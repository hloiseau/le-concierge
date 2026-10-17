package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const availabilityDeliverySchema = `
ALTER TABLE subscriptions ADD COLUMN availability_checked_at INTEGER NOT NULL DEFAULT 0;
CREATE TABLE availability_events(
 subscription_id INTEGER PRIMARY KEY REFERENCES subscriptions(id) ON DELETE CASCADE,
 detected_at INTEGER NOT NULL,
 verified_at INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','delivered','cancelled')),
 delivered_at INTEGER
);
CREATE TABLE availability_digests(
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 local_day TEXT NOT NULL,
 delivered_at INTEGER NOT NULL,
 item_count INTEGER NOT NULL CHECK(item_count>0),
 PRIMARY KEY(user_id,local_day)
);
CREATE TABLE availability_discord_deliveries(
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
CREATE TABLE availability_discord_items(
 delivery_id INTEGER NOT NULL REFERENCES availability_discord_deliveries(id) ON DELETE CASCADE,
 subscription_id INTEGER NOT NULL UNIQUE REFERENCES availability_events(subscription_id) ON DELETE CASCADE,
 PRIMARY KEY(delivery_id,subscription_id)
);
CREATE INDEX availability_pending ON availability_discord_deliveries(user_id,state,not_before);
CREATE TRIGGER availability_authorization_changed AFTER UPDATE OF notifications,plex_id,discord_id ON users
WHEN NEW.notifications=0 OR NEW.plex_id IS NULL OR OLD.plex_id IS NOT NEW.plex_id OR OLD.discord_id IS NOT NEW.discord_id
BEGIN
 UPDATE availability_events SET state='cancelled' WHERE state='pending' AND subscription_id IN (SELECT id FROM subscriptions WHERE user_id=NEW.id);
 UPDATE availability_discord_deliveries SET state='failed',last_error='authorization_changed' WHERE user_id=NEW.id AND state IN ('queued','claimed');
END;
CREATE TRIGGER availability_preferences_changed AFTER UPDATE ON reminder_preferences
BEGIN UPDATE availability_discord_deliveries SET state='failed',last_error='preferences_changed' WHERE user_id=NEW.user_id AND state IN ('queued','claimed'); END;
CREATE TRIGGER availability_channel_changed AFTER UPDATE OF channel ON subscriptions WHEN OLD.channel IS NOT NEW.channel
BEGIN UPDATE availability_discord_deliveries SET state='failed',last_error='channel_changed' WHERE state IN ('queued','claimed') AND id IN (SELECT delivery_id FROM availability_discord_items WHERE subscription_id=NEW.id); END;
CREATE TRIGGER availability_unsubscribed BEFORE DELETE ON subscriptions
BEGIN UPDATE availability_discord_deliveries SET state='failed',last_error='unsubscribed' WHERE state IN ('queued','claimed') AND id IN (SELECT delivery_id FROM availability_discord_items WHERE subscription_id=OLD.id); END;`

type AvailabilityEvent struct {
	Subscription
	AvailableAt time.Time `json:"availableAt"`
	DeliveredAt time.Time `json:"deliveredAt"`
}

func (s *Store) AvailabilityCandidates(ctx context.Context, user string, now time.Time) ([]Subscription, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT s.id,s.media_type,s.tmdb_id,s.season,s.channel,s.created_at FROM subscriptions s
LEFT JOIN subscription_availability a ON a.subscription_id=s.id LEFT JOIN availability_events e ON e.subscription_id=s.id
WHERE s.user_id=? AND (a.subscription_id IS NULL OR e.state='pending') ORDER BY s.availability_checked_at,s.id LIMIT 10`, user)
	if err != nil {
		return nil, err
	}
	items := []Subscription{}
	for rows.Next() {
		item, err := scanSubscription(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if _, err = s.DB.ExecContext(ctx, `UPDATE subscriptions SET availability_checked_at=? WHERE user_id=? AND id=?`, now.UnixMilli(), user, item.ID); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// Historical v4 observations never create new alerts. The first verified v5
// observation records consent and suppresses the user's own Seerr request once.
func (s *Store) ObserveAvailability(ctx context.Context, user string, id int64, now time.Time, requestedBySelf bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := reminderPreferences(ctx, tx, user)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_availability(subscription_id,available_at) SELECT id,? FROM subscriptions WHERE id=? AND user_id=?`, now.UTC().Format(time.RFC3339Nano), id, user)
	if err != nil {
		return err
	}
	fresh, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if fresh == 1 && p.Enabled && !requestedBySelf {
		if _, err = tx.ExecContext(ctx, `INSERT INTO availability_events(subscription_id,detected_at,verified_at,state) VALUES(?,?,?,'pending')`, id, now.UnixMilli(), now.UnixMilli()); err != nil {
			return err
		}
	}
	if requestedBySelf {
		_, err = tx.ExecContext(ctx, `UPDATE availability_events SET state='cancelled' WHERE subscription_id=? AND state='pending' AND EXISTS(SELECT 1 FROM subscriptions WHERE id=? AND user_id=?)`, id, id, user)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE availability_events SET verified_at=? WHERE subscription_id=? AND state='pending' AND EXISTS(SELECT 1 FROM subscriptions WHERE id=? AND user_id=?)`, now.UnixMilli(), id, id, user)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CancelAvailability(ctx context.Context, user string, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE availability_events SET state='cancelled' WHERE subscription_id=? AND state='pending' AND EXISTS(SELECT 1 FROM subscriptions WHERE id=? AND user_id=?)`, id, id, user)
	return err
}

func notificationItemsDeliveredToday(ctx context.Context, tx *sql.Tx, user string, now time.Time) (int, error) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return 0, err
	}
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	var count int
	err = tx.QueryRowContext(ctx, `SELECT
(SELECT COUNT(*) FROM reminder_outbox WHERE user_id=? AND state='sent' AND sent_at>=? AND sent_at<?)+
(SELECT COALESCE(SUM(item_count),0) FROM availability_digests WHERE user_id=? AND delivered_at>=? AND delivered_at<?)`, user, start.UnixMilli(), end.UnixMilli(), user, start.UnixMilli(), end.UnixMilli()).Scan(&count)
	return count, err
}

func (s *Store) DeliverAvailability(ctx context.Context, user string, verifiedIDs []int64, now time.Time, discordEnabled bool) error {
	if len(verifiedIDs) == 0 {
		return nil
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
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return err
	}
	local := now.In(loc)
	hour := local.Hour()
	quiet := p.QuietStartHour < p.QuietEndHour && hour >= p.QuietStartHour && hour < p.QuietEndHour || p.QuietStartHour > p.QuietEndHour && (hour >= p.QuietStartHour || hour < p.QuietEndHour)
	if !p.Enabled || quiet {
		return tx.Commit()
	}
	day := local.Format("2006-01-02")
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM availability_digests WHERE user_id=? AND local_day=?)`, user, day).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return tx.Commit()
	}
	used, err := notificationItemsDeliveredToday(ctx, tx, user, now)
	if err != nil {
		return err
	}
	remaining := max(0, p.DailyCap-used)
	if remaining == 0 {
		return tx.Commit()
	}
	args := []any{user, now.Add(-time.Minute).UnixMilli(), now.Add(-7 * 24 * time.Hour).UnixMilli()}
	for _, id := range verifiedIDs {
		args = append(args, id)
	}
	args = append(args, remaining)
	query := `SELECT s.id,s.channel FROM subscriptions s JOIN availability_events e ON e.subscription_id=s.id WHERE s.user_id=? AND e.state='pending' AND e.verified_at>=? AND e.detected_at>=? AND s.id IN (` + strings.TrimRight(strings.Repeat("?,", len(verifiedIDs)), ",") + `) ORDER BY e.detected_at,s.id LIMIT ?`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	ids, discordIDs := []int64{}, []int64{}
	for rows.Next() {
		var id int64
		var channel string
		if err = rows.Scan(&id, &channel); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
		if channel == "discord" {
			discordIDs = append(discordIDs, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO availability_digests(user_id,local_day,delivered_at,item_count) VALUES(?,?,?,?)`, user, day, now.UnixMilli(), len(ids)); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, `UPDATE availability_events SET state='delivered',delivered_at=? WHERE subscription_id=? AND state='pending'`, now.UnixMilli(), id); err != nil {
			return err
		}
	}
	if discordEnabled && p.DiscordDM && len(discordIDs) > 0 {
		var discordID string
		err = tx.QueryRowContext(ctx, `SELECT discord_id FROM users WHERE id=? AND notifications=1 AND plex_id IS NOT NULL AND discord_id IS NOT NULL`, user).Scan(&discordID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if discordID != "" {
			var nonce [12]byte
			if _, err = rand.Read(nonce[:]); err != nil {
				return err
			}
			result, err := tx.ExecContext(ctx, `INSERT INTO availability_discord_deliveries(user_id,discord_id,digest_at,nonce,not_before) VALUES(?,?,?,?,?)`, user, discordID, now.UnixMilli(), hex.EncodeToString(nonce[:]), now.UnixMilli())
			if err != nil {
				return err
			}
			id, err := result.LastInsertId()
			if err != nil {
				return err
			}
			for _, subscriptionID := range discordIDs {
				if _, err = tx.ExecContext(ctx, `INSERT INTO availability_discord_items(delivery_id,subscription_id) VALUES(?,?)`, id, subscriptionID); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

func (s *Store) AvailabilityInbox(ctx context.Context, user string) ([]AvailabilityEvent, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT s.id,s.media_type,s.tmdb_id,s.season,s.channel,s.created_at,e.detected_at,e.delivered_at FROM subscriptions s JOIN availability_events e ON e.subscription_id=s.id WHERE s.user_id=? AND e.state='delivered' ORDER BY e.delivered_at DESC,s.id LIMIT 20`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AvailabilityEvent{}
	for rows.Next() {
		var item AvailabilityEvent
		var season int
		var created string
		var detected, delivered int64
		if err = rows.Scan(&item.ID, &item.Type, &item.TMDBID, &season, &item.Channel, &created, &detected, &delivered); err != nil {
			return nil, err
		}
		if season >= 0 {
			item.Season = &season
		}
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		item.AvailableAt = time.UnixMilli(detected).UTC()
		item.DeliveredAt = time.UnixMilli(delivered).UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ClaimAvailabilityDiscord(ctx context.Context, user string, now time.Time) (*NotificationDelivery, error) {
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
	if _, err = tx.ExecContext(ctx, `UPDATE availability_discord_deliveries SET state='failed',last_error='expired_or_revoked' WHERE user_id=? AND state IN ('queued','claimed') AND (digest_at<? OR (attempts>=5 AND lease_until<=?) OR NOT EXISTS(SELECT 1 FROM users u JOIN reminder_preferences p ON p.user_id=u.id WHERE u.id=? AND u.notifications=1 AND u.plex_id IS NOT NULL AND u.discord_id=availability_discord_deliveries.discord_id AND p.discord_dm=1))`, user, start.UnixMilli(), now.UnixMilli(), user); err != nil {
		return nil, err
	}
	var d NotificationDelivery
	var digest, lease int64
	err = tx.QueryRowContext(ctx, `UPDATE availability_discord_deliveries SET state='claimed',attempts=attempts+1,lease_until=? WHERE id=(SELECT id FROM availability_discord_deliveries WHERE user_id=? AND attempts<5 AND not_before<=? AND (state='queued' OR (state='claimed' AND lease_until<=?)) ORDER BY digest_at,id LIMIT 1) RETURNING id,user_id,discord_id,digest_at,nonce,state,attempts,lease_until`, now.Add(time.Minute).UnixMilli(), user, now.UnixMilli(), now.UnixMilli()).Scan(&d.ID, &d.UserID, &d.DiscordID, &digest, &d.Nonce, &d.State, &d.Attempts, &lease)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	d.DigestAt = time.UnixMilli(digest).UTC()
	d.LeaseUntil = time.UnixMilli(lease).UTC()
	return &d, tx.Commit()
}

func (s *Store) AvailabilityDiscordItems(ctx context.Context, d NotificationDelivery) ([]Subscription, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT s.id,s.media_type,s.tmdb_id,s.season,s.channel,s.created_at FROM subscriptions s JOIN availability_discord_items i ON i.subscription_id=s.id JOIN availability_events e ON e.subscription_id=s.id WHERE i.delivery_id=? AND s.user_id=? AND s.channel='discord' AND e.state='delivered' ORDER BY s.id`, d.ID, d.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Subscription{}
	for rows.Next() {
		item, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) AvailabilityDiscordActive(ctx context.Context, d NotificationDelivery, now time.Time) (bool, error) {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return false, err
	}
	if d.DigestAt.In(loc).Format("2006-01-02") != now.In(loc).Format("2006-01-02") {
		return false, nil
	}
	var active bool
	err = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM availability_discord_deliveries n JOIN users u ON u.id=n.user_id JOIN reminder_preferences p ON p.user_id=u.id WHERE n.id=? AND n.user_id=? AND n.discord_id=? AND u.discord_id=n.discord_id AND u.notifications=1 AND u.plex_id IS NOT NULL AND p.discord_dm=1 AND n.state='claimed' AND n.attempts=? AND n.lease_until=? AND n.lease_until>?)`, d.ID, d.UserID, d.DiscordID, d.Attempts, d.LeaseUntil.UnixMilli(), now.UnixMilli()).Scan(&active)
	return active, err
}

func (s *Store) FinishAvailabilityDiscord(ctx context.Context, d NotificationDelivery, now time.Time, code string, retryAfter time.Duration, permanent bool) error {
	for _, c := range code {
		if (c < 'a' || c > 'z') && c != '_' {
			return errors.New("invalid availability failure code")
		}
	}
	if len(code) > 64 {
		return errors.New("invalid availability failure code")
	}
	state, next := "sent", now
	if code != "" {
		state = "queued"
		delay := time.Minute * time.Duration(1<<min(max(d.Attempts-1, 0), 4))
		if retryAfter > 24*time.Hour {
			permanent = true
		} else if retryAfter > delay {
			delay = retryAfter
		}
		next = now.Add(delay)
		if permanent || d.Attempts >= 5 {
			state = "failed"
		}
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE availability_discord_deliveries SET state=?,last_error=?,not_before=?,lease_until=0,sent_at=CASE WHEN ?='sent' THEN ? ELSE NULL END WHERE id=? AND user_id=? AND state='claimed' AND attempts=? AND lease_until=?`, state, code, next.UnixMilli(), state, now.UnixMilli(), d.ID, d.UserID, d.Attempts, d.LeaseUntil.UnixMilli())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return fmt.Errorf("availability claim expired or revoked")
	}
	return err
}

func (s *Store) PruneAvailability(ctx context.Context, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE availability_events SET state='cancelled' WHERE state='pending' AND detected_at<?;
DELETE FROM availability_events WHERE detected_at<?;
DELETE FROM availability_discord_deliveries WHERE digest_at<?;
DELETE FROM availability_digests WHERE delivered_at<?`, now.Add(-7*24*time.Hour).UnixMilli(), now.AddDate(0, 0, -90).UnixMilli(), now.AddDate(0, 0, -90).UnixMilli(), now.AddDate(0, 0, -90).UnixMilli())
	return err
}
