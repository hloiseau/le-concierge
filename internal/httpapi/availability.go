package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/hloiseau/le-concierge/internal/notifications"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

// RunAvailability visits at most ten old checks per active owner per cycle.
// Restored authorizations are verified with Plex before polling resumes.
func (s *Server) RunAvailability(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	lastUser := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := s.Store.PruneAvailability(ctx, time.Now()); err != nil && ctx.Err() == nil {
			slog.Warn("Availability retention cleanup deferred")
		}
		if !s.subscriptionsEnabled() {
			continue
		}
		s.mu.Lock()
		ids := []string{}
		for id, g := range s.grants {
			if g.Active(time.Now()) {
				ids = append(ids, id)
			}
		}
		s.mu.Unlock()
		sort.Strings(ids)
		split := sort.SearchStrings(ids, lastUser)
		for split < len(ids) && ids[split] <= lastUser {
			split++
		}
		ids = append(ids[split:], ids[:split]...)
		cycle, cancel := context.WithTimeout(ctx, 45*time.Second)
		for _, id := range ids {
			if cycle.Err() != nil {
				break
			}
			lastUser = id
			u, err := s.Store.User(cycle, id)
			if err != nil {
				continue
			}
			if err = s.refreshAvailability(cycle, u, time.Now().UTC()); err != nil && cycle.Err() == nil {
				slog.Warn("Availability refresh deferred; fresh authorization required")
			}
			if err = s.deliverAvailabilityDiscord(cycle, u); err != nil && cycle.Err() == nil {
				slog.Warn("Availability Discord delivery deferred")
			}
		}
		cancel()
	}
}

func (s *Server) availabilityOwner(ctx context.Context, u store.User, g upstream.Grant) error {
	current, exists := s.grants[u.ID]
	owner, err := s.Store.User(ctx, u.ID)
	if err != nil || !exists || owner.PlexID != u.PlexID || current.AccountToken != g.AccountToken || current.ServerToken != g.ServerToken || !current.Active(time.Now()) {
		return upstream.ErrDenied
	}
	return nil
}

func (s *Server) refreshAvailability(ctx context.Context, u store.User, now time.Time) error {
	if !s.subscriptionsEnabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	g, err := s.grant(ctx, u)
	if err != nil {
		return err
	}
	candidates, err := s.Store.AvailabilityCandidates(ctx, u.ID, now)
	if err != nil || len(candidates) == 0 {
		return err
	}
	seerrID, err := s.resolveSeerr(ctx, u)
	if err != nil {
		return err
	}
	verified := []int64{}
	var failure error
	for _, item := range candidates {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		media, err := s.Upstream.CheckSubscription(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, seerrID, s.Config.PlexURL, s.Config.PlexServerID, g, item.Type, item.TMDBID, item.Season)
		if err != nil && !errors.Is(err, upstream.ErrDenied) && !errors.Is(err, upstream.ErrNotFound) {
			failure = err
			continue
		}
		s.mu.Lock()
		if ownerErr := s.availabilityOwner(ctx, u, g); ownerErr != nil {
			s.mu.Unlock()
			return ownerErr
		}
		if err != nil || !media.Available {
			err = s.Store.CancelAvailability(ctx, u.ID, item.ID)
		} else {
			err = s.Store.ObserveAvailability(ctx, u.ID, item.ID, now, media.RequestedBySelf)
			if err == nil && !media.RequestedBySelf {
				verified = append(verified, item.ID)
			}
		}
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
	select {
	case s.reminderGate <- struct{}{}:
		defer func() { <-s.reminderGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.availabilityOwner(ctx, u, g); err != nil {
		return err
	}
	if err = s.Store.DeliverAvailability(ctx, u.ID, verified, now, s.Config.DiscordDMsEnabled); err != nil {
		return err
	}
	return failure
}

type availabilityItem struct {
	SubscriptionID int64 `json:"subscriptionId"`
	store.SubscriptionTarget
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	AvailableAt time.Time `json:"availableAt"`
	DeliveredAt time.Time `json:"deliveredAt"`
}

func (s *Server) availabilityInbox(w http.ResponseWriter, r *http.Request) {
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "Les notifications utilisent uniquement votre compte connecté.")
		return
	}
	if !s.subscriptionsEnabled() {
		problem(w, 503, "not_configured", "Les sources des suivis ne sont pas configurées.")
		return
	}
	if s.limited(w, r, "availability", 30) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	p, err := s.Store.GetReminderPreferences(ctx, u.ID)
	if err != nil {
		subscriptionError(w, err)
		return
	}
	items := []availabilityItem{}
	if p.Enabled {
		if err = s.refreshAvailability(ctx, u, time.Now().UTC()); err != nil {
			subscriptionError(w, err)
			return
		}
		events, err := s.Store.AvailabilityInbox(ctx, u.ID)
		if err != nil {
			subscriptionError(w, err)
			return
		}
		if len(events) > 0 {
			g, err := s.grant(ctx, u)
			if err != nil {
				subscriptionError(w, err)
				return
			}
			seerrID, err := s.resolveSeerr(ctx, u)
			if err != nil {
				subscriptionError(w, err)
				return
			}
			for _, event := range events {
				media, err := s.Upstream.CheckSubscription(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, seerrID, s.Config.PlexURL, s.Config.PlexServerID, g, event.Type, event.TMDBID, event.Season)
				if errors.Is(err, upstream.ErrDenied) || errors.Is(err, upstream.ErrNotFound) {
					continue
				}
				if err != nil {
					subscriptionError(w, err)
					return
				}
				if media.Available && !media.RequestedBySelf {
					items = append(items, availabilityItem{event.ID, event.SubscriptionTarget, media.Title, media.URL, event.AvailableAt, event.DeliveredAt})
				}
			}
		}
	}
	if !s.revalidateUser(w, r, key, u) {
		return
	}
	// A preference change or unsubscribe can happen during the upstream checks.
	s.mu.Lock()
	p, err = s.Store.GetReminderPreferences(ctx, u.ID)
	if err == nil && !p.Enabled {
		items = []availabilityItem{}
	} else if err == nil {
		var current []store.AvailabilityEvent
		current, err = s.Store.AvailabilityInbox(ctx, u.ID)
		owned := make(map[int64]bool, len(current))
		for _, event := range current {
			owned[event.ID] = true
		}
		retained := items[:0]
		for _, item := range items {
			if owned[item.SubscriptionID] {
				retained = append(retained, item)
			}
		}
		items = retained
	}
	s.mu.Unlock()
	if err != nil {
		subscriptionError(w, err)
		return
	}
	now := time.Now().UTC()
	jsonOut(w, 200, map[string]any{"items": items, "updatedAt": now, "expiresAt": now.Add(time.Minute)})
}

func (s *Server) deliverAvailabilityDiscord(ctx context.Context, u store.User) error {
	if !s.subscriptionsEnabled() || !s.Config.DiscordDMsEnabled || s.Discord == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	select {
	case s.reminderGate <- struct{}{}:
		defer func() { <-s.reminderGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	p, err := s.Store.GetReminderPreferences(ctx, u.ID)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return err
	}
	if !p.Enabled || !p.DiscordDM || reminderQuiet(p, time.Now().In(loc).Hour()) {
		return nil
	}
	d, err := s.Store.ClaimAvailabilityDiscord(ctx, u.ID, time.Now().UTC())
	if err != nil || d == nil {
		return err
	}
	finish := func(code string, wait time.Duration, permanent bool) error {
		return s.Store.FinishAvailabilityDiscord(ctx, *d, time.Now().UTC(), code, wait, permanent)
	}
	g, err := s.grant(ctx, u)
	if err != nil {
		return finish("authorization_unavailable", 0, errors.Is(err, upstream.ErrDenied))
	}
	saved, err := s.Store.AvailabilityDiscordItems(ctx, *d)
	if err != nil {
		return finish("storage_unavailable", 0, false)
	}
	seerrID, err := s.resolveSeerr(ctx, u)
	if err != nil {
		return finish("source_unavailable", 0, false)
	}
	items := []notifications.Message{}
	targets := []store.Subscription{}
	for _, item := range saved {
		media, err := s.Upstream.CheckSubscription(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, seerrID, s.Config.PlexURL, s.Config.PlexServerID, g, item.Type, item.TMDBID, item.Season)
		if errors.Is(err, upstream.ErrDenied) || errors.Is(err, upstream.ErrNotFound) {
			continue
		}
		if err != nil {
			return finish("source_unavailable", 0, false)
		}
		if media.Available && !media.RequestedBySelf {
			items = append(items, notifications.Message{Title: media.Title, URL: media.URL, Description: "Disponible dans votre Plex"})
			targets = append(targets, item)
		}
	}
	if len(items) == 0 {
		return finish("no_accessible_media", 0, true)
	}
	validateLocal := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.availabilityOwner(ctx, u, g); err != nil {
			return err
		}
		owner, err := s.Store.User(ctx, u.ID)
		if err != nil || owner.DiscordID != d.DiscordID || !owner.Notifications {
			return upstream.ErrDenied
		}
		prefs, err := s.Store.GetReminderPreferences(ctx, u.ID)
		if err != nil || !prefs.Enabled || !prefs.DiscordDM || reminderQuiet(prefs, time.Now().In(loc).Hour()) {
			return upstream.ErrDenied
		}
		active, err := s.Store.AvailabilityDiscordActive(ctx, *d, time.Now())
		if err != nil || !active {
			return upstream.ErrDenied
		}
		return nil
	}
	if err = validateLocal(); err != nil {
		return finish("authorization_changed", 0, true)
	}
	var validationErr error
	beforeSend := func() error {
		check, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		if err := validateLocal(); err != nil {
			validationErr = err
			return err
		}
		for index, item := range targets {
			media, err := s.Upstream.CheckSubscription(check, s.Config.SeerrURL, s.Config.SeerrAPIKey, seerrID, s.Config.PlexURL, s.Config.PlexServerID, g, item.Type, item.TMDBID, item.Season)
			if err != nil {
				validationErr = err
				return err
			}
			if !media.Available || media.RequestedBySelf {
				validationErr = upstream.ErrDenied
				return validationErr
			}
			items[index].Title, items[index].URL = media.Title, media.URL
		}
		validationErr = validateLocal()
		return validationErr
	}
	err = s.Discord.SendValidated(ctx, d.DiscordID, d.Nonce, items, beforeSend)
	if err == nil {
		return finish("", 0, false)
	}
	if validationErr != nil && !errors.Is(validationErr, upstream.ErrDenied) && !errors.Is(validationErr, upstream.ErrNotFound) {
		return finish("source_unavailable", 0, false)
	}
	var failure *notifications.DeliveryError
	if errors.As(err, &failure) {
		return finish(failure.Code, failure.RetryAfter, failure.Permanent)
	}
	return finish("transport_unavailable", 0, false)
}
