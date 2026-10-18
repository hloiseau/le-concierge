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
	deliveryCtx, stop := context.WithTimeout(ctx, 45*time.Second)
	defer stop()
	g, err := s.grant(deliveryCtx, u)
	if err != nil {
		if errors.Is(err, upstream.ErrDenied) {
			return s.Store.CancelDiscordDelivery(ctx, *d, "authorization_changed")
		}
		return retry("source_unavailable")
	}
	page, err := s.personalReminders(deliveryCtx, u, g, p)
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
	messageFor := func(reminder store.Reminder, page personalReminderPage) (notifications.Message, time.Time, bool) {
		if live := currentAvailable(reminder, page, time.Now(), p); live != nil {
			return notifications.Message{Title: live.Title, URL: live.PlexURL, Description: fmt.Sprintf("demandé et toujours non vu — ajouté dans Plex le %s", live.AvailableAt.In(loc).Format("02/01/2006"))}, live.ExpiresAt, true
		}
		for _, current := range page.Items {
			if reminder.MediaKey == reminderMediaKey(current) && reminder.DueAt.UnixMilli() == current.EstimatedAt.UnixMilli() && reminderDecision(time.Now(), u, s.Config.PlexServerID, current, page.Page, p).Notify {
				return notifications.Message{Title: current.Title, URL: current.URL,
					Description: fmt.Sprintf("départ estimé le %s — %s", current.EstimatedAt.In(loc).Format("02/01 à 15:04"), reminderDescription(current.Watch))}, current.EstimatedAt, true
			}
		}
		return notifications.Message{}, time.Time{}, false
	}
	items := []notifications.Message{}
	selected := []store.Reminder{}
	var earliestExpiry time.Time
	for _, reminder := range saved {
		message, expires, ok := messageFor(reminder, page)
		if !ok {
			continue
		}
		items = append(items, message)
		selected = append(selected, reminder)
		if earliestExpiry.IsZero() || expires.Before(earliestExpiry) {
			earliestExpiry = expires
		}
	}
	if len(items) == 0 {
		return s.Store.CancelDiscordDelivery(ctx, *d, "no_current_reminders")
	}
	validateLocal := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		now := time.Now().UTC()
		current, ok := s.grants[u.ID]
		owner, err := s.Store.User(ctx, u.ID)
		if err != nil || !ok || owner.PlexID != u.PlexID || owner.DiscordID != d.DiscordID || !owner.Notifications || current.AccountToken != g.AccountToken || current.ServerToken != g.ServerToken || !now.Before(current.ExpiresAt) {
			return upstream.ErrDenied
		}
		prefs, err := s.Store.GetReminderPreferences(ctx, u.ID)
		if err != nil || !prefs.Enabled || !prefs.DiscordDM || prefs.AvailableUnwatched != p.AvailableUnwatched || reminderQuiet(prefs, now.In(loc).Hour()) {
			return upstream.ErrDenied
		}
		active, err := s.Store.DiscordDeliveryActive(ctx, *d, now)
		if err != nil || !active {
			return upstream.ErrDenied
		}
		if !now.Before(page.ExpiresAt) || !now.Before(earliestExpiry) {
			return upstream.ErrIncomplete
		}
		_, state, err := s.reminderSubscriptions(ctx, u.ID)
		if err != nil {
			return err
		}
		if state != page.FollowState {
			return upstream.ErrIncomplete
		}
		return nil
	}
	if err = validateLocal(); err != nil {
		if !errors.Is(err, upstream.ErrDenied) {
			return retry("source_unavailable")
		}
		return s.Store.CancelDiscordDelivery(ctx, *d, "authorization_changed")
	}
	// Opening a DM can outlive a removal or watch-state change. Re-read every
	// selected reminder without holding the logout/preferences mutex.
	var validationErr error
	validate := func() error {
		if err := validateLocal(); err != nil {
			validationErr = err
			return err
		}
		fresh, err := s.personalReminders(deliveryCtx, u, g, p)
		if err != nil {
			validationErr = err
			return err
		}
		if fresh.FollowState != page.FollowState {
			validationErr = upstream.ErrIncomplete
			return validationErr
		}
		for index, saved := range selected {
			message, expires, ok := messageFor(saved, fresh)
			if !ok {
				// Rebuild the remaining digest on retry instead of losing valid siblings.
				validationErr = upstream.ErrIncomplete
				return validationErr
			}
			items[index] = message
			if index == 0 || expires.Before(earliestExpiry) {
				earliestExpiry = expires
			}
		}
		page = fresh
		if err := deliveryCtx.Err(); err != nil {
			validationErr = err
			return err
		}
		validationErr = validateLocal()
		return validationErr
	}
	err = s.Discord.SendValidated(deliveryCtx, d.DiscordID, d.Nonce, items, validate)
	if err == nil {
		return s.Store.CompleteDiscordDelivery(ctx, *d, time.Now().UTC())
	}
	if validationErr != nil && !errors.Is(validationErr, upstream.ErrDenied) && !errors.Is(validationErr, upstream.ErrNotFound) {
		return retry("source_unavailable")
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
