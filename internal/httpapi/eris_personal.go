package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) erisFollows(ctx context.Context, u store.User, g upstream.Grant, p eris.Plan) eris.ToolReply {
	failure := func(err error) eris.ToolReply { return erisFailure("follow_unavailable") }
	if p.Validate() != nil {
		return failure(upstream.ErrDenied)
	}
	if !s.subscriptionsEnabled() {
		return erisFailure("follows_not_configured")
	}
	if p.Action == "unfollow" && p.SubscriptionID == 0 {
		items, _, err := s.listFollows(ctx, u, 0)
		if err != nil {
			return failure(err)
		}
		var candidates []map[string]any
		for i, item := range items {
			if i == 10 {
				break
			}
			candidates = append(candidates, map[string]any{"subscriptionId": item.ID, "title": item.Title, "season": item.Season})
		}
		return erisData(map[string]any{"status": "identify_subscription", "candidates": candidates})
	}
	if p.Action == "follows" {
		items, total, err := s.listFollows(ctx, u, 0)
		if err != nil {
			return failure(err)
		}
		projected := []map[string]any{}
		for i, item := range items {
			if i == 10 {
				break
			}
			projected = append(projected, map[string]any{"subscriptionId": item.ID, "title": item.Title, "season": item.Season})
		}
		return erisData(map[string]any{"subscriptions": projected, "total": total, "partial": total > len(projected)})
	}
	select {
	case s.reminderGate <- struct{}{}:
		defer func() { <-s.reminderGate }()
	case <-ctx.Done():
		return failure(ctx.Err())
	}
	if p.Action == "unfollow" {
		err := s.erisPersonalMutation(ctx, u, g, func() error { return s.Store.DeleteSubscription(ctx, u.ID, p.SubscriptionID) })
		if errors.Is(err, sql.ErrNoRows) {
			return erisFailure("subscription_not_found_not_removed")
		}
		if err != nil {
			return failure(err)
		}
		return erisData(map[string]any{"status": "removed", "subscriptionId": p.SubscriptionID, "mediaDeleted": false, "requestCancelled": false})
	}
	member, err := s.resolveSeerr(ctx, u)
	if err != nil {
		return failure(err)
	}
	var m upstream.RequestCandidate
	var choices []upstream.RequestCandidate
	if p.MediaID > 0 {
		m, err = s.Upstream.MediaByID(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, member, p.MediaID, p.Kind)
	} else {
		m, choices, err = s.Upstream.ResolveMediaReference(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, member, p.Query, p.Kind, p.Year)
	}
	if errors.Is(err, upstream.ErrMediaReference) {
		return erisData(map[string]any{"status": "ambiguous", "candidates": choices})
	}
	if err != nil {
		return failure(err)
	}
	return s.erisFollowCandidate(ctx, u, p, m)
}

func (s *Server) erisFollowCandidate(ctx context.Context, u store.User, p eris.Plan, m upstream.RequestCandidate) eris.ToolReply {
	failure := func(err error) eris.ToolReply { return erisFailure("follow_unavailable") }
	target := store.SubscriptionTarget{Type: m.Kind, TMDBID: m.ID}
	if len(p.Seasons) == 1 {
		target.Season = &p.Seasons[0]
	}
	channel := p.Channel
	if channel == "" {
		channel = "hub"
	}
	result, err := s.createFollow(ctx, u, subscriptionInput{SubscriptionTarget: target, Channel: channel})
	if errors.Is(err, errSubscriptionChannel) {
		return erisData(map[string]any{"error": "discord_dm_consent_or_configuration_required", "consentChanged": false})
	}
	if err != nil {
		return failure(err)
	}
	if result.Status == "already_available" {
		return erisData(map[string]any{"status": "already_available", "title": result.Title, "pendingFollowCreated": false})
	}
	return erisData(map[string]any{"status": "created", "subscriptionId": result.Subscription.ID, "title": result.Title, "channel": channel, "dmSent": false, "availabilityScope": "first_accessible_episode_for_series"})
}

// Called under the delivery gate; authorization and identity cannot change mid-write.
func (s *Server) erisPersonalMutation(ctx context.Context, u store.User, g upstream.Grant, write func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.grants[u.ID]
	owner, err := s.Store.User(ctx, u.ID)
	if err != nil || !exists || current.PlexID != u.PlexID || owner.PlexID != u.PlexID || owner.DiscordID != u.DiscordID || !current.Active(time.Now()) || current.AccountToken != g.AccountToken || current.ServerToken != g.ServerToken {
		return upstream.ErrDenied
	}
	return write()
}

func (s *Server) erisPreferences(ctx context.Context, u store.User, g upstream.Grant, p eris.Plan) eris.ToolReply {
	failure := erisFailure("preferences_outcome_unverified")
	if p.Validate() != nil {
		return failure
	}
	select {
	case s.reminderGate <- struct{}{}:
		defer func() { <-s.reminderGate }()
	case <-ctx.Done():
		return failure
	}
	var preferences store.ReminderPreferences
	err := s.erisPersonalMutation(ctx, u, g, func() error {
		var err error
		preferences, err = s.Store.GetReminderPreferences(ctx, u.ID)
		if err != nil || p.Setting == "" {
			return err
		}
		boolean := p.Value == "true"
		switch p.Setting {
		case "enabled":
			preferences.Enabled = boolean
		case "discordDmEnabled":
			preferences.DiscordDM = boolean
		case "allUnwatched":
			preferences.AllUnwatched = boolean
		case "availableUnwatched":
			preferences.AvailableUnwatched = boolean
		case "dailyCap":
			preferences.DailyCap, _ = strconv.Atoi(p.Value)
		case "quietHours":
			parts := strings.Split(p.Value, "-")
			preferences.QuietStartHour, _ = strconv.Atoi(parts[0])
			preferences.QuietEndHour, _ = strconv.Atoi(parts[1])
		case "daysBefore":
			preferences.DaysBefore = nil
			for _, part := range strings.Split(p.Value, ",") {
				day, _ := strconv.Atoi(part)
				preferences.DaysBefore = append(preferences.DaysBefore, day)
			}
		default:
			return errors.New("unknown preference")
		}
		if p.Setting == "discordDmEnabled" && boolean && (!s.Config.DiscordDMsEnabled || u.DiscordID == "") {
			return errSubscriptionChannel
		}
		if p.Setting == "availableUnwatched" && boolean && (!s.subscriptionsEnabled() || !s.departuresEnabled()) {
			return upstream.ErrUnavailable
		}
		if err := s.Store.SaveReminderPreferences(ctx, u.ID, preferences); err != nil {
			return err
		}
		verified, err := s.Store.GetReminderPreferences(ctx, u.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(verified, preferences) {
			return errors.New("preference verification failed")
		}
		preferences = verified
		return nil
	})
	if errors.Is(err, errSubscriptionChannel) {
		return erisFailure("discord_dm_unavailable_not_changed")
	}
	if err != nil {
		return failure
	}
	status := "read"
	if p.Setting != "" {
		status = "saved_and_verified"
	}
	return erisData(map[string]any{"status": status, "preferences": preferences, "timezone": "Europe/Paris"})
}
