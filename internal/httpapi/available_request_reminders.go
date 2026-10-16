package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"time"

	"github.com/hloiseau/le-concierge/internal/departures"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

type availableRequestReminder struct {
	store.Reminder
	Kind string
}

type personalReminderPage struct {
	departures.Page
	Available []availableRequestReminder
}

func (s *Server) personalReminders(ctx context.Context, u store.User, g upstream.Grant, p store.ReminderPreferences) (personalReminderPage, error) {
	page, err := s.personalDepartures(ctx, u, g, p, true)
	if err != nil {
		return personalReminderPage{}, err
	}
	out := personalReminderPage{Page: page}
	if !p.AvailableUnwatched {
		return out, nil
	}
	started := time.Now().UTC()
	out.Available, err = s.availableRequestReminders(ctx, u, g, started)
	if err != nil {
		return personalReminderPage{}, err
	}
	if started.Add(time.Minute).Before(out.ExpiresAt) {
		out.ExpiresAt = started.Add(time.Minute)
	}
	if !time.Now().Before(out.ExpiresAt) {
		return personalReminderPage{}, upstream.ErrIncomplete
	}
	return out, nil
}

// A request's createdAt establishes intent age only. Plex's addedAt is the
// source addition date, never a fabricated removal deadline or share date.
func (s *Server) availableRequestReminders(ctx context.Context, u store.User, g upstream.Grant, now time.Time) ([]availableRequestReminder, error) {
	out := []availableRequestReminder{}
	if !s.subscriptionsEnabled() {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	id, err := s.resolveSeerr(ctx, u)
	if errors.Is(err, upstream.ErrSeerrUserNotFound) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	requests, err := s.Upstream.RequestInterests(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, id)
	if err != nil {
		return nil, err
	}
	eligible := []upstream.InterestTarget{}
	wantsMovie, wantsTV := false, false
	for _, request := range requests {
		if request.RequestID > 0 && len(request.PlexKeys) > 0 && !request.RequestedAt.IsZero() && !request.RequestedAt.Add(30*24*time.Hour).After(now) {
			eligible = append(eligible, request)
			wantsMovie = wantsMovie || request.Type == "movie"
			wantsTV = wantsTV || request.Type == "tv"
		}
	}
	if len(eligible) == 0 {
		return out, nil
	}
	unwatched, err := s.Upstream.PlexUnwatchedItems(ctx, s.Config.PlexURL, s.Config.PlexServerID, g)
	if err != nil {
		return nil, err
	}
	history, err := s.Upstream.TautulliHistory(ctx, s.Config.TautulliURL, s.Config.TautulliAPIKey, s.Config.PlexServerID, g.PlexID)
	if err != nil {
		return nil, err
	}
	removals, err := s.Upstream.MaintainerrRemovals(ctx, s.Config.MaintainerrURL, s.Config.PlexServerID)
	if err != nil {
		return nil, err
	}
	// Unlike the departure page, this complete snapshot also excludes unknown
	// deadlines and removals beyond 30 days, including parent memberships.
	metadata := map[string]upstream.PlexItem{}
	reads := 0
	get := func(id string) (upstream.PlexItem, error) {
		if m, ok := metadata[id]; ok {
			return m, nil
		}
		if reads >= 100 {
			return upstream.PlexItem{}, upstream.ErrIncomplete
		}
		reads++
		m, err := s.Upstream.PlexMetadata(ctx, s.Config.PlexURL, id, g)
		if err == nil {
			metadata[id] = m
		}
		return m, err
	}
	// Deterministic one-episode evidence per request/season. A completed or
	// started sibling never gets described as an entirely unwatched season.
	sort.Slice(unwatched, func(i, j int) bool {
		if unwatched[i].AddedAt.Equal(unwatched[j].AddedAt) {
			return unwatched[i].ID < unwatched[j].ID
		}
		return unwatched[i].AddedAt.Before(unwatched[j].AddedAt)
	})
	seen := map[string]bool{}
	for _, row := range unwatched {
		if (row.Kind == "movie" && !wantsMovie) || (row.Kind == "episode" && !wantsTV) || row.AddedAt.Add(30*24*time.Hour).After(now) {
			continue
		}
		matched := []upstream.InterestTarget{}
		for _, request := range eligible {
			start := row.AddedAt
			if request.RequestedAt.After(start) {
				start = request.RequestedAt
			}
			if now.Before(start.Add(30*24*time.Hour)) || !now.Before(start.Add(90*24*time.Hour)) {
				continue
			}
			if (request.Type == "movie" && row.Kind == "movie" && slices.Contains(request.PlexKeys, row.ID)) || (request.Type == "tv" && row.Kind == "episode" && row.GrandparentID != "" && slices.Contains(request.PlexKeys, row.GrandparentID)) {
				if row.Kind == "movie" && seen[availableRequestKey(u.ID, s.Config.PlexServerID, request.RequestID, -1)] {
					continue
				}
				// Skip siblings only after this exact season and show were read
				// personally and one playable episode proved this request group.
				parent, parentKnown := metadata[row.ParentID]
				show, showKnown := metadata[row.GrandparentID]
				if row.Kind == "episode" && parentKnown && showKnown && parent.Kind == "season" && show.Kind == "show" && parent.ParentID == show.ID && parent.LibraryID == show.LibraryID && parent.Index != nil {
					if request.Season == nil || *parent.Index != *request.Season || show.TMDBID != request.TMDBID || seen[availableRequestKey(u.ID, s.Config.PlexServerID, request.RequestID, *parent.Index)] {
						continue
					}
				}
				matched = append(matched, request)
			}
		}
		if len(matched) == 0 {
			continue
		}
		m, err := get(row.ID)
		if errors.Is(err, upstream.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if m.Kind != row.Kind || m.GUID != row.GUID || m.AddedAt != row.AddedAt.Unix() || (m.Kind == "episode" && (m.ParentID != row.ParentID || m.GrandparentID != row.GrandparentID)) {
			return nil, upstream.ErrIncomplete
		}
		if m.ViewCount > 0 || m.ViewOffset > 0 || m.Edition != "" || !m.Playable {
			continue
		}
		played := false
		for _, play := range history.Items {
			if play.MediaID == m.ID && play.Kind == m.Kind && play.GUID == m.GUID {
				played = true
				break
			}
		}
		if played {
			continue
		}
		planned := false
		for _, removal := range removals.Items {
			if removal.LibraryID == m.LibraryID && (removal.MediaID == m.ID || removal.MediaID == m.ParentID || removal.MediaID == m.GrandparentID) {
				planned = true
				break
			}
		}
		if planned {
			continue
		}
		kind, tmdb := m.Kind, m.TMDBID
		var season *int
		if m.Kind == "episode" {
			parent, err := get(m.ParentID)
			if err != nil {
				return nil, err
			}
			show, err := get(m.GrandparentID)
			if err != nil {
				return nil, err
			}
			if parent.Kind != "season" || show.Kind != "show" || parent.ParentID != show.ID || parent.LibraryID != m.LibraryID || show.LibraryID != m.LibraryID || parent.Index == nil {
				return nil, upstream.ErrIncomplete
			}
			kind, tmdb, season = "tv", show.TMDBID, parent.Index
		}
		for _, request := range matched {
			if request.Type != kind || request.TMDBID != tmdb || tmdb <= 0 || (kind == "tv" && (request.Season == nil || season == nil || *request.Season != *season)) {
				continue
			}
			start := row.AddedAt
			if request.RequestedAt.After(start) {
				start = request.RequestedAt
			}
			notBefore, expires := start.Add(30*24*time.Hour), start.Add(90*24*time.Hour)
			if now.Before(notBefore) || !now.Before(expires) {
				continue
			}
			seasonKey := -1
			if season != nil {
				seasonKey = *season
			}
			key := availableRequestKey(u.ID, s.Config.PlexServerID, request.RequestID, seasonKey)
			if seen[key] {
				continue
			}
			seen[key] = true
			title := m.Title
			if m.Kind == "episode" {
				title = m.GrandparentTitle + " · " + m.ParentTitle + " · " + m.Title
			}
			added := row.AddedAt
			out = append(out, availableRequestReminder{Kind: m.Kind, Reminder: store.Reminder{
				DecisionKey: key, MediaKey: "available:" + m.ID + ":" + digest(m.GUID+":"+added.Format(time.RFC3339)), Title: title,
				PlexURL: "https://app.plex.tv/desktop/#!/server/" + url.PathEscape(s.Config.PlexServerID) + "/details?key=" + url.QueryEscape("/library/metadata/"+m.ID),
				Reason:  "available_unwatched", AvailableAt: &added, NotBefore: notBefore, ExpiresAt: expires,
			}})
		}
	}
	return out, ctx.Err()
}

func availableRequestKey(user, server string, request int64, season int) string {
	return "available:" + digest(fmt.Sprintf("%s:%s:%d:%d", user, server, request, season))
}

func currentAvailable(saved store.Reminder, page personalReminderPage, now time.Time, p store.ReminderPreferences) *availableRequestReminder {
	if !p.Enabled || !p.AvailableUnwatched || !now.Before(page.ExpiresAt) {
		return nil
	}
	for i := range page.Available {
		live := &page.Available[i]
		if saved.Reason == "available_unwatched" && saved.DecisionKey == live.DecisionKey && saved.MediaKey == live.MediaKey && saved.AvailableAt != nil && live.AvailableAt != nil && saved.AvailableAt.Equal(*live.AvailableAt) && !now.Before(live.NotBefore) && now.Before(live.ExpiresAt) {
			return live
		}
	}
	return nil
}
