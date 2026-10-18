package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"
)

const channelReminderSchema = `CREATE TABLE channel_reminders (
 channel_id TEXT NOT NULL, decision_key TEXT NOT NULL, nonce TEXT NOT NULL,
 due_at INTEGER NOT NULL, local_day TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 1,
 next_attempt INTEGER NOT NULL, sent_at INTEGER, last_error TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(channel_id,decision_key));`

// Avoid remote enrichment for already delivered, leased, exhausted or capped
// work. ClaimChannelReminder repeats these checks transactionally before send.
func (s *Store) ChannelReminderPending(ctx context.Context, channel, key string, now time.Time) (bool, error) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return false, err
	}
	var pending bool
	err = s.DB.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM channel_reminders WHERE channel_id=? AND decision_key=? AND (sent_at IS NOT NULL OR attempts>=5 OR next_attempt>?)) AND (SELECT COUNT(*) FROM channel_reminders WHERE channel_id=? AND local_day=? AND (sent_at IS NOT NULL OR next_attempt>?))<10`, channel, key, now.UnixMilli(), channel, now.In(loc).Format("2006-01-02"), now.UnixMilli()).Scan(&pending)
	return pending, err
}

// ClaimChannelReminder persists the retry lease before contacting Discord. A
// completed milestone cannot be replayed after restart or a preference change.
func (s *Store) ClaimChannelReminder(ctx context.Context, channel, key string, due, now time.Time) (string, bool, error) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return "", false, err
	}
	day := now.In(loc).Format("2006-01-02")
	nonce := fmt.Sprintf("%x", sha256.Sum256([]byte(channel+":"+key)))[:24]
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM channel_reminders WHERE channel_id=? AND local_day=? AND (sent_at IS NOT NULL OR next_attempt>?)`, channel, day, now.UnixMilli()).Scan(&count); err != nil {
		return "", false, err
	}
	if count >= 10 {
		return "", false, nil
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO channel_reminders(channel_id,decision_key,nonce,due_at,local_day,next_attempt) VALUES(?,?,?,?,?,?) ON CONFLICT(channel_id,decision_key) DO UPDATE SET attempts=attempts+1,next_attempt=excluded.next_attempt,local_day=excluded.local_day WHERE sent_at IS NULL AND attempts<5 AND next_attempt<=?`, channel, key, nonce, due.UnixMilli(), day, now.Add(time.Minute).UnixMilli(), now.UnixMilli())
	if err != nil {
		return "", false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return "", false, err
	}
	return nonce, n == 1, tx.Commit()
}

func (s *Store) FinishChannelReminder(ctx context.Context, channel, key string, now time.Time, code string, retry time.Duration, permanent bool) error {
	if code == "" {
		_, err := s.DB.ExecContext(ctx, `UPDATE channel_reminders SET sent_at=?,last_error='' WHERE channel_id=? AND decision_key=? AND sent_at IS NULL`, now.UnixMilli(), channel, key)
		return err
	}
	if retry > 24*time.Hour {
		permanent = true
	}
	// Compute from the persisted attempt in the same update: 1/2/4/8/16 minutes,
	// never sooner than Discord's Retry-After.
	_, err := s.DB.ExecContext(ctx, `UPDATE channel_reminders SET last_error=?,next_attempt=?+MAX(?,?*(1<<MIN(MAX(attempts-1,0),4))),attempts=CASE WHEN ? THEN 5 ELSE attempts END WHERE channel_id=? AND decision_key=? AND sent_at IS NULL`, code, now.UnixMilli(), retry.Milliseconds(), time.Minute.Milliseconds(), permanent, channel, key)
	return err
}

func (s *Store) LinkedDiscordForPlex(ctx context.Context, plex string) (string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(discord_id),'') FROM users WHERE plex_id=?`, plex).Scan(&id)
	return id, err
}

func (s *Store) PruneChannelReminders(ctx context.Context, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM channel_reminders WHERE due_at<?`, now.AddDate(0, 0, -90).UnixMilli())
	return err
}
