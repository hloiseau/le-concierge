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
	page, err := s.personalReminders(ctx, u, g, p)
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
	availableToSend := map[int]store.Reminder{}
	var earliestExpiry time.Time
	for _, reminder := range saved {
		if live := currentAvailable(reminder, page, time.Now(), p); live != nil {
			availableToSend[len(items)] = reminder
			items = append(items, notifications.Message{Title: live.Title, URL: live.PlexURL, Description: fmt.Sprintf("demandé et toujours non vu — ajouté dans Plex le %s", live.AvailableAt.In(loc).Format("02/01/2006"))})
			if earliestExpiry.IsZero() || live.ExpiresAt.Before(earliestExpiry) {
				earliestExpiry = live.ExpiresAt
			}
			continue
		}
		for _, current := range page.Items {
			if reminder.MediaKey == reminderMediaKey(current) && reminder.DueAt.UnixMilli() == current.EstimatedAt.UnixMilli() && reminderDecision(time.Now(), u, s.Config.PlexServerID, current, page.Page, p).Notify {
				items = append(items, notifications.Message{Title: current.Title, URL: current.URL,
					Description: fmt.Sprintf("départ estimé le %s — %s", current.EstimatedAt.In(loc).Format("02/01 à 15:04"), reminderDescription(current.Watch))})
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
	validateLocal := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		now := time.Now().UTC()
		_, state, err := s.reminderSubscriptions(ctx, u.ID)
		if err != nil || state != page.FollowState {
			return upstream.ErrDenied
		}
		current, ok := s.grants[u.ID]
		owner, err := s.Store.User(ctx, u.ID)
		if err != nil || !ok || owner.PlexID != u.PlexID || owner.DiscordID != d.DiscordID || !owner.Notifications || current.AccountToken != g.AccountToken || current.ServerToken != g.ServerToken || !now.Before(current.ExpiresAt) || !now.Before(page.ExpiresAt) || !now.Before(earliestExpiry) {
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
		return nil
	}
	if err = validateLocal(); err != nil {
		return s.Store.CancelDiscordDelivery(ctx, *d, "authorization_changed")
	}
	// Channel creation is another remote boundary. Recheck only the optional
	// request nudges afterwards, without holding the logout/preferences mutex.
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var validationErr error
	validate := func() error {
		if err := validateLocal(); err != nil {
			return err
		}
		if len(availableToSend) > 0 {
			checkCtx, stop := context.WithTimeout(sendCtx, 4*time.Second)
			defer stop()
			fresh, err := s.availableRequestReminders(checkCtx, u, g, time.Now().UTC())
			if err != nil {
				validationErr = err
				return err
			}
			verified := page
			verified.Available = fresh
			for index, saved := range availableToSend {
				live := currentAvailable(saved, verified, time.Now(), p)
				if live == nil {
					return upstream.ErrDenied
				}
				items[index] = notifications.Message{Title: live.Title, URL: live.PlexURL, Description: fmt.Sprintf("demandé et toujours non vu — ajouté dans Plex le %s", live.AvailableAt.In(loc).Format("02/01/2006"))}
			}
		}
		if err := sendCtx.Err(); err != nil {
			validationErr = err
			return err
		}
		return validateLocal()
	}
	err = s.Discord.SendValidated(sendCtx, d.DiscordID, d.Nonce, items, validate)
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
