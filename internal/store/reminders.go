package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"
)

const remindersSchema = `
CREATE TABLE reminder_preferences(
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 days_before TEXT NOT NULL DEFAULT '[7,2]',
 quiet_start INTEGER NOT NULL DEFAULT 22 CHECK(quiet_start BETWEEN 0 AND 23),
 quiet_end INTEGER NOT NULL DEFAULT 8 CHECK(quiet_end BETWEEN 0 AND 23),
 daily_cap INTEGER NOT NULL DEFAULT 10 CHECK(daily_cap BETWEEN 1 AND 100),
 all_unwatched INTEGER NOT NULL DEFAULT 0 CHECK(all_unwatched IN (0,1))
);
CREATE TABLE reminder_outbox(
 id INTEGER PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 decision_key TEXT NOT NULL,
 media_key TEXT NOT NULL,
 title TEXT NOT NULL,
 plex_url TEXT NOT NULL,
 reason TEXT NOT NULL,
 milestone_days INTEGER NOT NULL,
 due_at INTEGER NOT NULL,
 not_before INTEGER NOT NULL,
 expires_at INTEGER NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','sent','cancelled','failed')),
 attempts INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL,
 sent_at INTEGER,
 UNIQUE(user_id,decision_key)
);
CREATE INDEX reminder_outbox_pending ON reminder_outbox(user_id,state,not_before);
CREATE TABLE reminder_deliveries(
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 local_day TEXT NOT NULL,
 delivered_at INTEGER NOT NULL,
 PRIMARY KEY(user_id,local_day)
);
CREATE TRIGGER reminders_disabled AFTER UPDATE OF notifications ON users WHEN NEW.notifications=0
BEGIN UPDATE reminder_outbox SET state='cancelled' WHERE user_id=NEW.id AND state='pending'; END;
CREATE TRIGGER reminders_unlinked AFTER UPDATE OF plex_id ON users WHEN NEW.plex_id IS NULL
BEGIN UPDATE reminder_outbox SET state='cancelled' WHERE user_id=NEW.id AND state='pending'; END;
INSERT INTO schema_migrations(version) VALUES(3);`

// DailyCap limits items in the single daily inbox digest, in Europe/Paris.
type ReminderPreferences struct {
	Enabled            bool  `json:"enabled"`
	DiscordDM          bool  `json:"discordDmEnabled"`
	DaysBefore         []int `json:"daysBefore"`
	QuietStartHour     int   `json:"quietStartHour"`
	QuietEndHour       int   `json:"quietEndHour"`
	DailyCap           int   `json:"dailyCap"`
	AllUnwatched       bool  `json:"allUnwatched"`
	AvailableUnwatched bool  `json:"availableUnwatched"`
}

type Reminder struct {
	ID            int64      `json:"id"`
	DecisionKey   string     `json:"decisionKey"`
	MediaKey      string     `json:"mediaKey"`
	Title         string     `json:"title"`
	PlexURL       string     `json:"plexUrl"`
	Reason        string     `json:"reason"`
	MilestoneDays int        `json:"milestoneDays"`
	DueAt         time.Time  `json:"dueAt"`
	AvailableAt   *time.Time `json:"availableAt,omitempty"`
	NotBefore     time.Time  `json:"notBefore"`
	ExpiresAt     time.Time  `json:"expiresAt"`
	State         string     `json:"state"`
	Attempts      int        `json:"attempts"`
	LastError     string     `json:"lastError,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	SentAt        *time.Time `json:"sentAt,omitempty"`
}

type reminderQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func reminderPreferences(ctx context.Context, db reminderQuerier, user string) (ReminderPreferences, error) {
	var p ReminderPreferences
	var days string
	err := db.QueryRowContext(ctx, `SELECT u.notifications,COALESCE(p.days_before,'[7,2]'),COALESCE(p.quiet_start,22),COALESCE(p.quiet_end,8),COALESCE(p.daily_cap,10),COALESCE(p.all_unwatched,0),COALESCE(p.discord_dm,0),COALESCE(p.available_unwatched,0)
	FROM users u LEFT JOIN reminder_preferences p ON p.user_id=u.id WHERE u.id=?`, user).Scan(&p.Enabled, &days, &p.QuietStartHour, &p.QuietEndHour, &p.DailyCap, &p.AllUnwatched, &p.DiscordDM, &p.AvailableUnwatched)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(days), &p.DaysBefore)
	return p, err
}

func (s *Store) GetReminderPreferences(ctx context.Context, user string) (ReminderPreferences, error) {
	return reminderPreferences(ctx, s.DB, user)
}

func ValidateReminderPreferences(p ReminderPreferences) error {
	if len(p.DaysBefore) == 0 || len(p.DaysBefore) > 5 || p.QuietStartHour < 0 || p.QuietStartHour > 23 || p.QuietEndHour < 0 || p.QuietEndHour > 23 || p.DailyCap < 1 || p.DailyCap > 100 {
		return errors.New("invalid reminder preferences")
	}
	seen := map[int]bool{}
	for _, day := range p.DaysBefore {
		if day < 1 || day > 30 || seen[day] {
			return errors.New("reminder milestones must be unique days from 1 to 30")
		}
		seen[day] = true
	}
	return nil
}

func (s *Store) SaveReminderPreferences(ctx context.Context, user string, p ReminderPreferences) error {
	if err := ValidateReminderPreferences(p); err != nil {
		return err
	}
	days, err := json.Marshal(p.DaysBefore)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = reminderPreferences(ctx, tx, user); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET notifications=? WHERE id=?`, p.Enabled, user); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO reminder_preferences(user_id,days_before,quiet_start,quiet_end,daily_cap,all_unwatched,discord_dm,available_unwatched) VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(user_id) DO UPDATE SET days_before=excluded.days_before,quiet_start=excluded.quiet_start,quiet_end=excluded.quiet_end,daily_cap=excluded.daily_cap,all_unwatched=excluded.all_unwatched,discord_dm=excluded.discord_dm,available_unwatched=excluded.available_unwatched`, user, string(days), p.QuietStartHour, p.QuietEndHour, p.DailyCap, p.AllUnwatched, p.DiscordDM, p.AvailableUnwatched); err != nil {
		return err
	}
	// Revalidate queued decisions against the new preferences before delivery.
	if _, err = tx.ExecContext(ctx, `UPDATE reminder_outbox SET state='cancelled' WHERE user_id=? AND state='pending'`, user); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) EnqueueReminders(ctx context.Context, user string, reminders []Reminder) error {
	if len(reminders) > 1000 {
		return errors.New("too many reminders")
	}
	for _, r := range reminders {
		if r.DecisionKey == "" || len(r.DecisionKey) > 256 || r.MediaKey == "" || len(r.MediaKey) > 256 || strings.TrimSpace(r.Title) == "" || len(r.Title) > 1000 || len(r.PlexURL) > 4096 || len(r.Reason) > 256 {
			return errors.New("invalid reminder")
		}
		if r.Reason == "available_unwatched" {
			if r.AvailableAt == nil || r.AvailableAt.IsZero() || !r.DueAt.IsZero() || r.MilestoneDays != 0 || r.NotBefore.Before(r.AvailableAt.Add(30*24*time.Hour)) || !r.ExpiresAt.After(r.NotBefore) || r.ExpiresAt.After(r.NotBefore.Add(60*24*time.Hour)) {
				return errors.New("invalid available request reminder")
			}
		} else if r.AvailableAt != nil || r.DueAt.IsZero() || r.MilestoneDays < 1 || r.MilestoneDays > 30 {
			return errors.New("invalid removal reminder")
		} else if !r.ExpiresAt.IsZero() && r.ExpiresAt.After(r.DueAt) {
			return errors.New("reminder cannot expire after its removal deadline")
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := reminderPreferences(ctx, tx, user)
	if err != nil {
		return err
	}
	if !p.Enabled {
		return nil
	}
	for _, r := range reminders {
		if r.Reason == "available_unwatched" && !p.AvailableUnwatched {
			continue
		}
		if r.ExpiresAt.IsZero() {
			r.ExpiresAt = r.DueAt
		}
		if r.NotBefore.IsZero() {
			r.NotBefore = time.Now()
		}
		var added any
		due := r.DueAt.UnixMilli()
		if r.AvailableAt != nil {
			added, due = r.AvailableAt.UnixMilli(), 0
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO reminder_outbox(user_id,decision_key,media_key,title,plex_url,reason,milestone_days,due_at,not_before,expires_at,created_at,available_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(user_id,decision_key) DO UPDATE SET media_key=excluded.media_key,title=excluded.title,plex_url=excluded.plex_url,reason=excluded.reason,due_at=excluded.due_at,available_at=excluded.available_at,not_before=CASE WHEN reminder_outbox.state='pending' THEN MAX(reminder_outbox.not_before,excluded.not_before) ELSE excluded.not_before END,expires_at=excluded.expires_at,state='pending',attempts=CASE WHEN reminder_outbox.state='cancelled' THEN 0 ELSE reminder_outbox.attempts END,last_error=CASE WHEN reminder_outbox.state='cancelled' THEN '' ELSE reminder_outbox.last_error END
WHERE reminder_outbox.state='cancelled' OR (reminder_outbox.state='pending' AND excluded.reason='available_unwatched')`, user, r.DecisionKey, r.MediaKey, r.Title, r.PlexURL, r.Reason, r.MilestoneDays, due, r.NotBefore.UnixMilli(), r.ExpiresAt.UnixMilli(), time.Now().UnixMilli(), added); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CancelObsoleteReminders requires a complete, freshly authorized candidate list.
func (s *Store) CancelObsoleteReminders(ctx context.Context, user string, validDecisionKeys []string) error {
	if len(validDecisionKeys) > 1000 {
		return errors.New("too many reminder decisions")
	}
	query := `UPDATE reminder_outbox SET state='cancelled' WHERE user_id=? AND state='pending'`
	args := []any{user}
	if len(validDecisionKeys) > 0 {
		query += ` AND decision_key NOT IN (` + strings.TrimRight(strings.Repeat("?,", len(validDecisionKeys)), ",") + `)`
		for _, key := range validDecisionKeys {
			args = append(args, key)
		}
	}
	_, err := s.DB.ExecContext(ctx, query, args...)
	return err
}

const reminderColumns = `id,decision_key,media_key,title,plex_url,reason,milestone_days,due_at,not_before,expires_at,state,attempts,last_error,created_at,sent_at,available_at`

func scanReminder(rows *sql.Rows) (Reminder, error) {
	var r Reminder
	var due, notBefore, expires, created int64
	var sent sql.NullInt64
	var added sql.NullInt64
	err := rows.Scan(&r.ID, &r.DecisionKey, &r.MediaKey, &r.Title, &r.PlexURL, &r.Reason, &r.MilestoneDays, &due, &notBefore, &expires, &r.State, &r.Attempts, &r.LastError, &created, &sent, &added)
	r.DueAt, r.NotBefore, r.ExpiresAt, r.CreatedAt = time.UnixMilli(due).UTC(), time.UnixMilli(notBefore).UTC(), time.UnixMilli(expires).UTC(), time.UnixMilli(created).UTC()
	if added.Valid {
		value := time.UnixMilli(added.Int64).UTC()
		r.AvailableAt, r.DueAt = &value, time.Time{}
	}
	if sent.Valid {
		value := time.UnixMilli(sent.Int64).UTC()
		r.SentAt = &value
	}
	return r, err
}

// DeliverReminders records the inbox digest and its items in one transaction.
// Callers must revalidate sources and access immediately before calling it.
func (s *Store) DeliverReminders(ctx context.Context, user string, now time.Time) ([]Reminder, error) {
	return s.DeliverRemindersWithDiscord(ctx, user, now, false)
}

// DeliverRemindersWithDiscord queues a DM atomically with a new inbox digest
// only when the server channel and the user's explicit DM preference are on.
func (s *Store) DeliverRemindersWithDiscord(ctx context.Context, user string, now time.Time, discordEnabled bool) ([]Reminder, error) {
	delivered := []Reminder{}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := reminderPreferences(ctx, tx, user)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE reminder_outbox SET state='cancelled' WHERE user_id=? AND state='pending' AND expires_at<=?`, user, now.UnixMilli()); err != nil {
		return nil, err
	}
	if !p.Enabled {
		return delivered, tx.Commit()
	}
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return nil, err
	}
	local := now.In(loc)
	hour := local.Hour()
	quiet := p.QuietStartHour < p.QuietEndHour && hour >= p.QuietStartHour && hour < p.QuietEndHour || p.QuietStartHour > p.QuietEndHour && (hour >= p.QuietStartHour || hour < p.QuietEndHour)
	if quiet {
		return delivered, tx.Commit()
	}
	day := local.Format("2006-01-02")
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM reminder_deliveries WHERE user_id=? AND local_day=?)`, user, day).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return delivered, tx.Commit()
	}
	used, err := notificationItemsDeliveredToday(ctx, tx, user, now)
	if err != nil {
		return nil, err
	}
	limit := max(0, p.DailyCap-used)
	rows, err := tx.QueryContext(ctx, `SELECT `+reminderColumns+` FROM reminder_outbox WHERE user_id=? AND state='pending' AND not_before<=? AND expires_at>? AND (available_at IS NULL OR ?=1) ORDER BY available_at IS NOT NULL,due_at,id LIMIT ?`, user, now.UnixMilli(), now.UnixMilli(), p.AvailableUnwatched, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		delivered = append(delivered, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(delivered) == 0 {
		return delivered, tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO reminder_deliveries(user_id,local_day,delivered_at) VALUES(?,?,?)`, user, day, now.UnixMilli()); err != nil {
		return nil, err
	}
	for i := range delivered {
		if _, err = tx.ExecContext(ctx, `UPDATE reminder_outbox SET state='sent',sent_at=? WHERE id=? AND user_id=? AND state='pending'`, now.UnixMilli(), delivered[i].ID, user); err != nil {
			return nil, err
		}
		delivered[i].State = "sent"
		sent := now.UTC()
		delivered[i].SentAt = &sent
	}
	if discordEnabled {
		if err = queueDiscordDelivery(ctx, tx, user, now); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return delivered, nil
}

func (s *Store) ReminderHistory(ctx context.Context, user string, limit int) ([]Reminder, error) {
	if limit < 1 || limit > 200 {
		return nil, errors.New("reminder history limit must be from 1 to 200")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+reminderColumns+` FROM reminder_outbox WHERE user_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, user, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Reminder{}
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// RetryReminder preserves a bounded backoff across restarts; five failures stop it.
func (s *Store) RetryReminder(ctx context.Context, user string, id int64, now time.Time, reason string) error {
	if len(reason) > 512 {
		return errors.New("reminder failure reason too long")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var attempts int
	var expires int64
	if err = tx.QueryRowContext(ctx, `SELECT attempts,expires_at FROM reminder_outbox WHERE id=? AND user_id=? AND state='pending'`, id, user).Scan(&attempts, &expires); err != nil {
		return err
	}
	attempts++
	next := now.Add(time.Minute * time.Duration(1<<min(attempts-1, 4)))
	state := "pending"
	if expires <= now.UnixMilli() {
		state = "cancelled"
	} else if attempts >= 5 || next.UnixMilli() >= expires {
		state = "failed"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE reminder_outbox SET attempts=?,not_before=?,state=?,last_error=? WHERE id=? AND user_id=?`, attempts, next.UnixMilli(), state, reason, id, user); err != nil {
		return fmt.Errorf("record reminder retry: %w", err)
	}
	return tx.Commit()
}

// Retain decision keys for 90 days after expiry to prevent duplicate deliveries.
func (s *Store) PruneReminders(ctx context.Context, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE reminder_outbox SET state='cancelled' WHERE state='pending' AND expires_at<=?`, now.UnixMilli()); err != nil {
		return err
	}
	cutoff := now.AddDate(0, 0, -90).UnixMilli()
	if _, err = tx.ExecContext(ctx, `DELETE FROM reminder_outbox WHERE expires_at<?`, cutoff); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM reminder_deliveries WHERE delivered_at<?`, cutoff); err != nil {
		return err
	}
	return tx.Commit()
}
