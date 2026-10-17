package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/hloiseau/le-concierge/internal/departures"
	"github.com/hloiseau/le-concierge/internal/reminders"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) reminderSubscriptions(ctx context.Context, user string) ([]upstream.InterestTarget, string, error) {
	targets := []upstream.InterestTarget{}
	saved := []store.Subscription{}
	total := -1
	for offset := 0; offset < 100; offset += 10 {
		rows, count, err := s.Store.Subscriptions(ctx, user, offset)
		if err != nil {
			return nil, "", err
		}
		if count > 100 || (total >= 0 && total != count) {
			return nil, "", upstream.ErrIncomplete
		}
		total = count
		saved = append(saved, rows...)
		for _, r := range rows {
			targets = append(targets, upstream.InterestTarget{Type: r.Type, TMDBID: r.TMDBID, Season: r.Season, Following: true})
		}
		if offset+len(rows) == total {
			encoded, _ := json.Marshal(saved)
			return targets, digest(string(encoded)), nil
		}
		if len(rows) == 0 {
			return nil, "", upstream.ErrIncomplete
		}
	}
	return nil, "", upstream.ErrIncomplete
}

func (s *Server) personalDepartures(ctx context.Context, u store.User, g upstream.Grant, p store.ReminderPreferences, fresh bool) (departures.Page, error) {
	targets, state, err := s.reminderSubscriptions(ctx, u.ID)
	if err != nil {
		return departures.Page{}, err
	}
	if s.Config.SeerrURL != "" {
		id, e := s.resolveSeerr(ctx, u)
		if e != nil && !errors.Is(e, upstream.ErrSeerrUserNotFound) {
			return departures.Page{}, e
		}
		if e == nil {
			requests, e := s.Upstream.RequestInterests(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, id)
			if e != nil {
				return departures.Page{}, e
			}
			targets = append(targets, requests...)
		}
	}
	page, err := s.Departures.ListInterested(ctx, s.Config, g, targets, p.AllUnwatched, fresh)
	page.FollowState = state
	return page, err
}

func reminderDecision(now time.Time, u store.User, server string, item departures.Item, page departures.Page, p store.ReminderPreferences) reminders.Decision {
	return reminders.Decide(now, reminders.Candidate{UserID: u.ID, ServerID: server, MediaID: item.ID, ActionID: item.ActionID,
		Accessible: true, Requested: item.Requested, Following: item.Following, Watch: reminders.WatchState(item.Watch), Kind: item.Kind,
		RuleActive: true, DeletesFiles: true, LeavesAt: item.EstimatedAt, SourceUpdatedAt: page.UpdatedAt}, reminders.Preferences{
		Enabled: p.Enabled, AllUnwatched: p.AllUnwatched, DaysBefore: p.DaysBefore, QuietStart: p.QuietStartHour, QuietEnd: p.QuietEndHour, Timezone: "Europe/Paris", MaxSourceAge: time.Minute})
}

func reminderDescription(watch string) string {
	if watch == "unstarted" {
		return "marqué non vu dans Plex"
	}
	return "visionnage en cours"
}
