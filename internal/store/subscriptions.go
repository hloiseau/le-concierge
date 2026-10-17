package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const subscriptionsSchema = `CREATE TABLE subscriptions(
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 media_type TEXT NOT NULL CHECK(media_type IN ('movie','tv')),
 tmdb_id INTEGER NOT NULL CHECK(tmdb_id>0),
 season INTEGER NOT NULL DEFAULT -1 CHECK(season>=-1 AND season<=10000 AND (media_type='tv' OR season=-1)),
 channel TEXT NOT NULL CHECK(channel IN ('hub','discord')),
 created_at TEXT NOT NULL,
 UNIQUE(user_id,media_type,tmdb_id,season)
);
CREATE INDEX subscriptions_owner ON subscriptions(user_id,id);
CREATE TABLE subscription_availability(
 subscription_id INTEGER PRIMARY KEY REFERENCES subscriptions(id) ON DELETE CASCADE,
 available_at TEXT NOT NULL
);`

var ErrSubscriptionLimit = errors.New("subscription limit reached")

type SubscriptionTarget struct {
	Type   string `json:"type"`
	TMDBID int64  `json:"tmdbId"`
	Season *int   `json:"season,omitempty"`
}

type Subscription struct {
	ID int64 `json:"id"`
	SubscriptionTarget
	Channel   string    `json:"channel"`
	CreatedAt time.Time `json:"createdAt"`
}

func ValidateSubscription(target SubscriptionTarget, channel string) error {
	if (target.Type != "movie" && target.Type != "tv") || target.TMDBID <= 0 || target.TMDBID > 2147483647 ||
		(target.Season != nil && (target.Type != "tv" || *target.Season < 0 || *target.Season > 10000)) ||
		(channel != "hub" && channel != "discord") {
		return errors.New("invalid subscription")
	}
	return nil
}

func subscriptionSeason(target SubscriptionTarget) int {
	if target.Season == nil {
		return -1
	}
	return *target.Season
}

// SaveSubscription is idempotent for a user's stable target and updates its channel.
func (s *Store) SaveSubscription(ctx context.Context, userID string, target SubscriptionTarget, channel string) (Subscription, error) {
	if err := ValidateSubscription(target, channel); err != nil {
		return Subscription{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Subscription{}, err
	}
	defer tx.Rollback()
	season := subscriptionSeason(target)
	var count, existing int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MAX(media_type=? AND tmdb_id=? AND season=?),0) FROM subscriptions WHERE user_id=?`, target.Type, target.TMDBID, season, userID).Scan(&count, &existing); err != nil {
		return Subscription{}, err
	}
	if count >= 100 && existing == 0 {
		return Subscription{}, ErrSubscriptionLimit
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO subscriptions(user_id,media_type,tmdb_id,season,channel,created_at) VALUES(?,?,?,?,?,?)
 ON CONFLICT(user_id,media_type,tmdb_id,season) DO UPDATE SET channel=excluded.channel`, userID, target.Type, target.TMDBID, season, channel, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return Subscription{}, err
	}
	item, err := scanSubscription(tx.QueryRowContext(ctx, `SELECT id,media_type,tmdb_id,season,channel,created_at FROM subscriptions WHERE user_id=? AND media_type=? AND tmdb_id=? AND season=?`, userID, target.Type, target.TMDBID, season))
	if err != nil {
		return Subscription{}, err
	}
	return item, tx.Commit()
}

func scanSubscription(row interface{ Scan(...any) error }) (Subscription, error) {
	var item Subscription
	var season int
	var created string
	if err := row.Scan(&item.ID, &item.Type, &item.TMDBID, &season, &item.Channel, &created); err != nil {
		return item, err
	}
	if season >= 0 {
		item.Season = &season
	}
	var err error
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	return item, err
}

func (s *Store) Subscriptions(ctx context.Context, userID string, offset int) ([]Subscription, int, error) {
	if offset < 0 || offset > 10000 {
		return nil, 0, errors.New("invalid offset")
	}
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE user_id=?`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,media_type,tmdb_id,season,channel,created_at FROM subscriptions WHERE user_id=? ORDER BY id DESC LIMIT 10 OFFSET ?`, userID, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Subscription{}
	for rows.Next() {
		item, err := scanSubscription(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Store) DeleteSubscription(ctx context.Context, userID string, id int64) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM subscriptions WHERE user_id=? AND id=?`, userID, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (s *Store) DeleteSubscriptionTarget(ctx context.Context, userID string, target SubscriptionTarget) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM subscriptions WHERE user_id=? AND media_type=? AND tmdb_id=? AND season=?`, userID, target.Type, target.TMDBID, subscriptionSeason(target))
	return err
}

func (s *Store) UpdateSubscriptionChannel(ctx context.Context, userID string, id int64, channel string) error {
	if channel != "hub" && channel != "discord" {
		return errors.New("invalid subscription channel")
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE subscriptions SET channel=? WHERE id=? AND user_id=?`, channel, id, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return sql.ErrNoRows
	}
	return err
}

func mergeSubscriptions(ctx context.Context, tx *sql.Tx, userID, oldID string) error {
	// A merged identity must re-consent; pending Discord messages keep their original owner.
	if _, err := tx.ExecContext(ctx, `UPDATE availability_discord_deliveries SET state='failed',last_error='authorization_changed' WHERE user_id=? AND state IN ('queued','claimed')`, oldID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO availability_digests(user_id,local_day,delivered_at,item_count) SELECT ?,local_day,delivered_at,item_count FROM availability_digests WHERE user_id=? ON CONFLICT(user_id,local_day) DO UPDATE SET item_count=item_count+excluded.item_count`, userID, oldID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE OR IGNORE subscriptions SET user_id=? WHERE user_id=?`, userID, oldID)
	return err
}

// MarkSubscriptionAvailable records one observation, after fresh personal access
// checks, without creating a second event when a poll or process restarts.
func (s *Store) MarkSubscriptionAvailable(ctx context.Context, userID string, id int64, at time.Time) (bool, error) {
	if at.IsZero() {
		return false, errors.New("missing availability observation time")
	}
	result, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO subscription_availability(subscription_id,available_at)
SELECT id,? FROM subscriptions WHERE id=? AND user_id=?`, at.UTC().Format(time.RFC3339Nano), id, userID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
