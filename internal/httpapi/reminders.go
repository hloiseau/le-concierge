package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/hloiseau/le-concierge/internal/departures"
	"github.com/hloiseau/le-concierge/internal/reminders"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) getReminderPreferences(w http.ResponseWriter, r *http.Request) {
	_, u, ok := s.user(w, r)
	if !ok {
		return
	}
	p, err := s.Store.GetReminderPreferences(r.Context(), u.ID)
	if err != nil {
		problem(w, 500, "storage_error", "Lecture des préférences impossible.")
		return
	}
	jsonOut(w, 200, p)
}

func (s *Server) putReminderPreferences(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	var input struct {
		Enabled        *bool `json:"enabled"`
		DaysBefore     []int `json:"daysBefore"`
		QuietStartHour *int  `json:"quietStartHour"`
		QuietEndHour   *int  `json:"quietEndHour"`
		DailyCap       *int  `json:"dailyCap"`
		AllUnwatched   *bool `json:"allUnwatched"`
		DiscordDM      *bool `json:"discordDmEnabled"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || input.Enabled == nil || input.QuietStartHour == nil || input.QuietEndHour == nil || input.DailyCap == nil || input.AllUnwatched == nil {
		problem(w, 400, "invalid_body", "Préférences de rappel invalides.")
		return
	}
	p := store.ReminderPreferences{Enabled: *input.Enabled, DaysBefore: input.DaysBefore, QuietStartHour: *input.QuietStartHour, QuietEndHour: *input.QuietEndHour, DailyCap: *input.DailyCap, AllUnwatched: *input.AllUnwatched}
	// Omission preserves the old Hub's inbox-only behavior; never infer DM consent.
	if input.DiscordDM != nil {
		p.DiscordDM = *input.DiscordDM
	}
	if p.DiscordDM && (!s.Config.DiscordDMsEnabled || u.DiscordID == "") {
		problem(w, 409, "discord_dm_unavailable", "Les DM nécessitent Discord lié et un service activé par l’administrateur.")
		return
	}
	if store.ValidateReminderPreferences(p) != nil {
		problem(w, 400, "invalid_body", "Préférences de rappel invalides.")
		return
	}
	select {
	case s.reminderGate <- struct{}{}:
		defer func() { <-s.reminderGate }()
	case <-r.Context().Done():
		problem(w, 503, "reminders_busy", "Réessayez dans un instant.")
		return
	}
	// The gate can wait behind a remote refresh. Consent must still belong to
	// the same live session and recipient when the mutation actually executes.
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	sess, sessionExists := s.sessions[key]
	g, grantExists := s.grants[u.ID]
	owner, err := s.Store.User(r.Context(), u.ID)
	if err != nil || !sessionExists || sess.UserID != u.ID || !now.Before(sess.Expires) || !grantExists || !now.Before(g.ExpiresAt) || g.PlexID != u.PlexID || owner.PlexID != u.PlexID || (p.DiscordDM && owner.DiscordID != u.DiscordID) {
		problem(w, 403, "authorization_changed", "Votre accès a changé. Reconnectez-vous avant de modifier vos préférences.")
		return
	}
	if err := s.Store.SaveReminderPreferences(r.Context(), u.ID, p); err != nil {
		problem(w, 500, "storage_error", "Enregistrement impossible.")
		return
	}
	jsonOut(w, 200, p)
}

type inboxItem struct {
	ID            int64     `json:"id"`
	Title         string    `json:"title"`
	Kind          string    `json:"type"`
	EstimatedAt   time.Time `json:"estimatedAt"`
	URL           string    `json:"url"`
	Reason        string    `json:"reason"`
	MilestoneDays int       `json:"milestoneDays"`
	SentAt        time.Time `json:"sentAt"`
}

func (s *Server) reminderInbox(w http.ResponseWriter, r *http.Request) {
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "Les rappels utilisent uniquement votre compte connecté.")
		return
	}
	if !s.departuresEnabled() {
		problem(w, 503, "not_configured", "Les sources des rappels ne sont pas configurées.")
		return
	}
	if s.rate("reminders:"+u.ID, 6) {
		problem(w, 429, "rate_limited", "Patientez avant d’actualiser les rappels.")
		return
	}
	page, err := s.refreshReminders(r.Context(), u)
	if err != nil {
		problem(w, 503, "reminders_unavailable", "Les rappels ne peuvent pas être vérifiés actuellement. Aucune ancienne liste n’est affichée.")
		return
	}
	history, err := s.Store.ReminderHistory(r.Context(), u.ID, 200)
	if err != nil {
		problem(w, 500, "storage_error", "Lecture des rappels impossible.")
		return
	}
	currentKey, current, ok := s.session(r)
	if !ok || currentKey != key || current.UserID != u.ID {
		problem(w, 401, "unauthenticated", "Reconnectez-vous avec Plex.")
		return
	}
	currentUser, err := s.Store.User(r.Context(), u.ID)
	if err != nil || currentUser.PlexID != u.PlexID {
		problem(w, 403, "plex_required", "Reconnectez-vous avec Plex.")
		return
	}
	if _, ok = s.requireGrant(w, r, currentUser); !ok {
		return
	}
	items := []inboxItem{}
	if currentUser.Notifications {
		for _, saved := range history {
			if saved.State != "sent" || saved.SentAt == nil {
				continue
			}
			for _, live := range page.Items {
				if saved.MediaKey == reminderMediaKey(live) && saved.DueAt.UnixMilli() == live.EstimatedAt.UnixMilli() && live.Watch == "in_progress" {
					items = append(items, inboxItem{saved.ID, live.Title, live.Kind, live.EstimatedAt, live.URL, saved.Reason, saved.MilestoneDays, *saved.SentAt})
					break
				}
			}
		}
	}
	jsonOut(w, 200, map[string]any{"items": items, "scope": "recorded_history", "updatedAt": page.UpdatedAt, "expiresAt": page.ExpiresAt})
}

func reminderMediaKey(item departures.Item) string { return item.ID + ":" + item.ActionID }

// Every delivery uses a complete fresh observation; persisted titles alone never
// establish current access or a removal risk after a restart or source failure.
func (s *Server) refreshReminders(ctx context.Context, u store.User) (departures.Page, error) {
	select {
	case s.reminderGate <- struct{}{}:
		defer func() { <-s.reminderGate }()
	case <-ctx.Done():
		return departures.Page{}, ctx.Err()
	}
	p, err := s.Store.GetReminderPreferences(ctx, u.ID)
	if err != nil {
		return departures.Page{}, err
	}
	if !p.Enabled {
		now := time.Now().UTC()
		return departures.Page{Items: []departures.Item{}, Scope: "recorded_history", UpdatedAt: now, ExpiresAt: now.Add(time.Minute)}, s.Store.CancelObsoleteReminders(ctx, u.ID, nil)
	}
	g, err := s.grant(ctx, u)
	if err != nil {
		return departures.Page{}, err
	}
	page, err := s.Departures.ListFresh(ctx, s.Config, g)
	if err != nil {
		return departures.Page{}, err
	}
	now := time.Now().UTC()
	queue := []store.Reminder{}
	keys := []string{}
	for _, item := range page.Items {
		decision := reminders.Decide(now, reminders.Candidate{
			UserID: u.ID, ServerID: s.Config.PlexServerID, MediaID: item.ID, ActionID: item.ActionID,
			Accessible: true, Watch: reminders.WatchState(item.Watch), Kind: item.Kind,
			RuleActive: true, DeletesFiles: true, LeavesAt: item.EstimatedAt, SourceUpdatedAt: page.UpdatedAt,
		}, reminders.Preferences{Enabled: p.Enabled, AllUnwatched: p.AllUnwatched, DaysBefore: p.DaysBefore,
			QuietStart: p.QuietStartHour, QuietEnd: p.QuietEndHour, Timezone: "Europe/Paris", MaxSourceAge: time.Minute})
		if decision.Notify {
			keys = append(keys, decision.Key)
			queue = append(queue, store.Reminder{DecisionKey: decision.Key, MediaKey: reminderMediaKey(item), Title: item.Title,
				PlexURL: item.URL, Reason: decision.Reason, MilestoneDays: decision.MilestoneDays, DueAt: item.EstimatedAt,
				NotBefore: decision.NotBefore, ExpiresAt: item.EstimatedAt})
		}
	}
	// Serialize the final local delivery with logout and grant replacement.
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.grants[u.ID]
	owner, err := s.Store.User(ctx, u.ID)
	if err != nil || !exists || owner.PlexID != u.PlexID || current.AccountToken != g.AccountToken || current.ServerToken != g.ServerToken || !now.Before(current.ExpiresAt) || !now.Before(page.ExpiresAt) {
		return departures.Page{}, upstream.ErrDenied
	}
	if err = s.Store.CancelObsoleteReminders(ctx, u.ID, keys); err == nil {
		err = s.Store.EnqueueReminders(ctx, u.ID, queue)
	}
	if err == nil {
		_, err = s.Store.DeliverRemindersWithDiscord(ctx, u.ID, now, s.Config.DiscordDMsEnabled)
	}
	return page, err
}

// RunReminders only processes current Plex grants. Tokens remain in memory; after
// a restart the durable queue resumes following the user's next Plex login.
func (s *Server) RunReminders(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := s.Store.PruneReminders(ctx, time.Now()); err != nil && ctx.Err() == nil {
			slog.Warn("Reminder retention cleanup deferred")
		}
		if err := s.Store.PruneExtensionRequests(ctx, time.Now()); err != nil && ctx.Err() == nil {
			slog.Warn("Extension request retention cleanup deferred")
		}
		if err := s.Store.PruneDiscordDeliveries(ctx, time.Now()); err != nil && ctx.Err() == nil {
			slog.Warn("Discord delivery retention cleanup deferred")
		}
		if !s.departuresEnabled() {
			continue
		}
		s.mu.Lock()
		ids := make([]string, 0, len(s.grants))
		for id, grant := range s.grants {
			if time.Now().Before(grant.ExpiresAt) {
				ids = append(ids, id)
			}
		}
		s.mu.Unlock()
		for _, id := range ids {
			if ctx.Err() != nil {
				return
			}
			u, err := s.Store.User(ctx, id)
			if err != nil || !u.Notifications {
				continue
			}
			_, err = s.refreshReminders(ctx, u)
			if err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("Reminder refresh deferred; fresh authorization and sources required")
			}
			if err == nil && s.Config.DiscordDMsEnabled {
				if err := s.deliverDiscordReminders(ctx, u); err != nil && ctx.Err() == nil {
					slog.Warn("Discord reminder delivery deferred")
				}
			}
		}
	}
}
