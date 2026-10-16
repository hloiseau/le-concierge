package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hloiseau/le-concierge/internal/notifications"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

// deliverDiscordReminders resumes one durable daily digest, using current
// sources and access. A server enable never scans historical inbox deliveries.
func (s *Server) deliverDiscordReminders(ctx context.Context, u store.User) error {
	if !s.Config.DiscordDMsEnabled || s.Discord == nil || !s.departuresEnabled() {
		return nil
	}
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
	d, err := s.Store.ClaimDiscordDelivery(ctx, u.ID, time.Now().UTC())
	if err != nil || d == nil {
		return err
	}
	retry := func(code string) error {
		return s.Store.RetryDiscordDelivery(ctx, *d, time.Now().UTC(), 0, false, code)
	}
	g, err := s.grant(ctx, u)
	if err != nil {
		if errors.Is(err, upstream.ErrDenied) {
			return s.Store.CancelDiscordDelivery(ctx, *d, "authorization_changed")
		}
		return retry("source_unavailable")
	}
	page, err := s.Departures.ListFresh(ctx, s.Config, g)
	if err != nil {
		if errors.Is(err, upstream.ErrDenied) {
			return s.Store.CancelDiscordDelivery(ctx, *d, "authorization_changed")
		}
		return retry("source_unavailable")
	}
	saved, err := s.Store.DiscordDeliveryItems(ctx, *d)
	if err != nil {
		return retry("storage_unavailable")
	}
	items := []notifications.Message{}
	var earliestExpiry time.Time
	for _, reminder := range saved {
		for _, current := range page.Items {
			if reminder.MediaKey == reminderMediaKey(current) && reminder.DueAt.UnixMilli() == current.EstimatedAt.UnixMilli() && current.Watch == "in_progress" && current.EstimatedAt.After(time.Now()) {
				items = append(items, notifications.Message{Title: current.Title, URL: current.URL,
					Description: fmt.Sprintf("départ estimé le %s — visionnage en cours", current.EstimatedAt.In(loc).Format("02/01 à 15:04"))})
				if earliestExpiry.IsZero() || current.EstimatedAt.Before(earliestExpiry) {
					earliestExpiry = current.EstimatedAt
				}
				break
			}
		}
	}
	if len(items) == 0 {
		return s.Store.CancelDiscordDelivery(ctx, *d, "no_current_reminders")
	}
	validate := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		now := time.Now().UTC()
		current, ok := s.grants[u.ID]
		owner, err := s.Store.User(ctx, u.ID)
		if err != nil || !ok || owner.PlexID != u.PlexID || owner.DiscordID != d.DiscordID || !owner.Notifications || current.AccountToken != g.AccountToken || current.ServerToken != g.ServerToken || !now.Before(current.ExpiresAt) || !now.Before(page.ExpiresAt) || !now.Before(earliestExpiry) {
			return upstream.ErrDenied
		}
		prefs, err := s.Store.GetReminderPreferences(ctx, u.ID)
		if err != nil || !prefs.Enabled || !prefs.DiscordDM || reminderQuiet(prefs, now.In(loc).Hour()) {
			return upstream.ErrDenied
		}
		active, err := s.Store.DiscordDeliveryActive(ctx, *d, now)
		if err != nil || !active {
			return upstream.ErrDenied
		}
		return nil
	}
	if err = validate(); err != nil {
		return s.Store.CancelDiscordDelivery(ctx, *d, "authorization_changed")
	}
	err = s.Discord.SendValidated(ctx, d.DiscordID, d.Nonce, items, validate)
	if err == nil {
		return s.Store.CompleteDiscordDelivery(ctx, *d, time.Now().UTC())
	}
	var failure *notifications.DeliveryError
	if errors.As(err, &failure) {
		return s.Store.RetryDiscordDelivery(ctx, *d, time.Now().UTC(), failure.RetryAfter, failure.Permanent, failure.Code)
	}
	return retry("transport_unavailable")
}

func reminderQuiet(p store.ReminderPreferences, hour int) bool {
	return p.QuietStartHour < p.QuietEndHour && hour >= p.QuietStartHour && hour < p.QuietEndHour ||
		p.QuietStartHour > p.QuietEndHour && (hour >= p.QuietStartHour || hour < p.QuietEndHour)
}
