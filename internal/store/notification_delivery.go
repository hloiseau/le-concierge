package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

const notificationDeliverySchema = `
ALTER TABLE reminder_preferences ADD COLUMN discord_dm INTEGER NOT NULL DEFAULT 0 CHECK(discord_dm IN (0,1));
CREATE TABLE notification_deliveries(
 id INTEGER PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 discord_id TEXT NOT NULL,
 digest_at INTEGER NOT NULL,
 nonce TEXT NOT NULL UNIQUE,
 state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','claimed','sent','failed')),
 attempts INTEGER NOT NULL DEFAULT 0,
 not_before INTEGER NOT NULL,
 lease_until INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '',
 sent_at INTEGER,
 UNIQUE(user_id,digest_at)
);
CREATE INDEX notification_deliveries_pending ON notification_deliveries(user_id,state,not_before);
CREATE TRIGGER discord_reminders_disabled AFTER UPDATE OF notifications,plex_id,discord_id ON users
WHEN NEW.notifications=0 OR NEW.plex_id IS NULL OR NEW.discord_id IS NULL OR OLD.plex_id IS NOT NEW.plex_id OR OLD.discord_id IS NOT NEW.discord_id
BEGIN UPDATE notification_deliveries SET state='failed',last_error='authorization_changed' WHERE user_id=NEW.id AND state IN ('queued','claimed'); END;
CREATE TRIGGER discord_preferences_changed AFTER UPDATE ON reminder_preferences
BEGIN UPDATE notification_deliveries SET state='failed',last_error='preferences_changed' WHERE user_id=NEW.user_id AND state IN ('queued','claimed'); END;`

// NotificationDelivery contains no credential or copied private message content.
type NotificationDelivery struct {
	ID         int64
	UserID     string
	DiscordID  string
	DigestAt   time.Time
	Nonce      string
	State      string
	Attempts   int
	LeaseUntil time.Time
}

// queueDiscordDelivery must run inside the transaction creating a NEW inbox
// digest, only while the server channel is enabled. It never backfills history.
func queueDiscordDelivery(ctx context.Context, tx *sql.Tx, user string, digestAt time.Time) error {
	var discordID string
	err := tx.QueryRowContext(ctx, `SELECT u.discord_id FROM users u JOIN reminder_preferences p ON p.user_id=u.id WHERE u.id=? AND u.notifications=1 AND p.discord_dm=1 AND u.plex_id IS NOT NULL AND u.discord_id IS NOT NULL`, user).Scan(&discordID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO notification_deliveries(user_id,discord_id,digest_at,nonce,not_before) VALUES(?,?,?,?,?) ON CONFLICT(user_id,digest_at) DO NOTHING`, user, discordID, digestAt.UnixMilli(), hex.EncodeToString(nonce[:]), digestAt.UnixMilli())
	return err
}

// ClaimDiscordDelivery atomically leases one digest. Claim attempts, including
// interrupted workers, are bounded; the lease exceeds the transport timeout.
func (s *Store) ClaimDiscordDelivery(ctx context.Context, user string, now time.Time) (*NotificationDelivery, error) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return nil, err
	}
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE notification_deliveries SET state='failed',last_error='expired_or_revoked' WHERE user_id=? AND state IN ('queued','claimed') AND (
 NOT EXISTS(SELECT 1 FROM users u JOIN reminder_preferences p ON p.user_id=u.id WHERE u.id=? AND u.notifications=1 AND u.plex_id IS NOT NULL AND u.discord_id=notification_deliveries.discord_id AND p.discord_dm=1)
 OR NOT EXISTS(SELECT 1 FROM reminder_outbox r WHERE r.user_id=notification_deliveries.user_id AND r.sent_at=notification_deliveries.digest_at AND r.state='sent' AND r.expires_at>?)
 OR digest_at<? OR (attempts>=5 AND lease_until<=?))`, user, user, now.UnixMilli(), start.UnixMilli(), now.UnixMilli()); err != nil {
		return nil, err
	}
	var d NotificationDelivery
	var digest, lease int64
	err = tx.QueryRowContext(ctx, `UPDATE notification_deliveries SET state='claimed',attempts=attempts+1,lease_until=? WHERE id=(SELECT id FROM notification_deliveries WHERE user_id=? AND attempts<5 AND not_before<=? AND (state='queued' OR (state='claimed' AND lease_until<=?)) ORDER BY digest_at,id LIMIT 1) RETURNING id,user_id,discord_id,digest_at,nonce,state,attempts,lease_until`, now.Add(time.Minute).UnixMilli(), user, now.UnixMilli(), now.UnixMilli()).Scan(&d.ID, &d.UserID, &d.DiscordID, &digest, &d.Nonce, &d.State, &d.Attempts, &lease)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	d.DigestAt, d.LeaseUntil = time.UnixMilli(digest).UTC(), time.UnixMilli(lease).UTC()
	return &d, tx.Commit()
}

// DiscordDeliveryItems is only a decision-key lookup. Its stored content must be
// replaced with a complete freshly authorized observation before sending.
func (s *Store) DiscordDeliveryItems(ctx context.Context, d NotificationDelivery) ([]Reminder, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+reminderColumns+` FROM reminder_outbox WHERE user_id=? AND sent_at=? AND state='sent' ORDER BY due_at,id`, d.UserID, d.DigestAt.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Reminder{}
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return items, rows.Err()
}

func (s *Store) CompleteDiscordDelivery(ctx context.Context, d NotificationDelivery, now time.Time) error {
	return s.finishDiscordDelivery(ctx, d, "sent", "", now, now)
}

// DiscordDeliveryActive is the final local check after source revalidation and
// DM channel creation. An expired or superseded claim must never send.
func (s *Store) DiscordDeliveryActive(ctx context.Context, d NotificationDelivery, now time.Time) (bool, error) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return false, err
	}
	if d.DigestAt.In(loc).Format("2006-01-02") != now.In(loc).Format("2006-01-02") {
		return false, nil
	}
	var active bool
	err = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_deliveries n JOIN users u ON u.id=n.user_id JOIN reminder_preferences p ON p.user_id=u.id WHERE n.id=? AND n.user_id=? AND n.discord_id=? AND u.discord_id=n.discord_id AND u.plex_id IS NOT NULL AND u.notifications=1 AND p.discord_dm=1 AND n.state='claimed' AND n.attempts=? AND n.lease_until=? AND n.lease_until>?)`, d.ID, d.UserID, d.DiscordID, d.Attempts, d.LeaseUntil.UnixMilli(), now.UnixMilli()).Scan(&active)
	return active, err
}

func (s *Store) CancelDiscordDelivery(ctx context.Context, d NotificationDelivery, reason string) error {
	return s.RetryDiscordDelivery(ctx, d, time.Now().UTC(), 0, true, reason)
}

// RetryDiscordDelivery persists bounded retries. Only sanitized error codes may
// be passed; upstream response bodies and URLs must never enter this table.
func (s *Store) RetryDiscordDelivery(ctx context.Context, d NotificationDelivery, now time.Time, retryAfter time.Duration, permanent bool, reason string) error {
	if len(reason) > 64 || reason == "" {
		return errors.New("invalid notification failure code")
	}
	for _, c := range reason {
		if (c < 'a' || c > 'z') && c != '_' {
			return errors.New("invalid notification failure code")
		}
	}
	delay := time.Minute * time.Duration(1<<min(max(d.Attempts-1, 0), 4))
	if retryAfter > 24*time.Hour {
		permanent = true // Never retry sooner than an excessive upstream delay.
	} else if retryAfter > delay {
		delay = retryAfter
	}
	state := "queued"
	if permanent || d.Attempts >= 5 {
		state = "failed"
	}
	return s.finishDiscordDelivery(ctx, d, state, reason, now, now.Add(delay))
}

func (s *Store) finishDiscordDelivery(ctx context.Context, d NotificationDelivery, state, reason string, now, next time.Time) error {
	r, err := s.DB.ExecContext(ctx, `UPDATE notification_deliveries SET state=?,last_error=?,not_before=?,lease_until=0,sent_at=CASE WHEN ?='sent' THEN ? ELSE NULL END WHERE id=? AND user_id=? AND state='claimed' AND attempts=? AND lease_until=?`, state, reason, next.UnixMilli(), state, now.UnixMilli(), d.ID, d.UserID, d.Attempts, d.LeaseUntil.UnixMilli())
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("notification claim expired or revoked")
	}
	return nil
}

func (s *Store) PruneDiscordDeliveries(ctx context.Context, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM notification_deliveries WHERE digest_at<?`, now.AddDate(0, 0, -90).UnixMilli())
	return err
}
