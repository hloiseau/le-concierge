package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const extensionRequestsSchema = `
CREATE TABLE extension_requests (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 decision_key TEXT NOT NULL, media_id TEXT NOT NULL, title TEXT NOT NULL,
 kind TEXT NOT NULL, plex_url TEXT NOT NULL, estimated_at INTEGER NOT NULL,
 days INTEGER NOT NULL CHECK(days BETWEEN 1 AND 30), reason TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','reviewed','declined')),
 admin_note TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
 UNIQUE(user_id,decision_key)
);
CREATE INDEX extension_requests_pending ON extension_requests(status,created_at);
`

const extensionRequestsLifecycleSchema = `
CREATE TABLE extension_requests_v7 (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 decision_key TEXT NOT NULL, media_id TEXT NOT NULL, title TEXT NOT NULL,
 kind TEXT NOT NULL, plex_url TEXT NOT NULL, estimated_at INTEGER NOT NULL,
 days INTEGER NOT NULL CHECK(days BETWEEN 1 AND 30), reason TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','reviewed','declined','cancelled')),
 admin_note TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
 UNIQUE(user_id,decision_key)
);
INSERT INTO extension_requests_v7 SELECT * FROM extension_requests;
UPDATE sqlite_sequence SET seq=MAX(seq,COALESCE((SELECT seq FROM sqlite_sequence WHERE name='extension_requests'),0)) WHERE name='extension_requests_v7';
DROP TABLE extension_requests;
ALTER TABLE extension_requests_v7 RENAME TO extension_requests;
CREATE INDEX extension_requests_pending ON extension_requests(status,created_at);
`

var ErrExtensionLimit = errors.New("extension request limit reached")

type ExtensionRequest struct {
	ID          int64     `json:"id"`
	Key         string    `json:"-"`
	MediaID     string    `json:"mediaId"`
	Title       string    `json:"title"`
	Kind        string    `json:"type"`
	URL         string    `json:"url"`
	EstimatedAt time.Time `json:"estimatedAt"`
	Days        int       `json:"days"`
	Reason      string    `json:"reason"`
	Status      string    `json:"status"`
	AdminNote   string    `json:"adminNote"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Requester   string    `json:"requester,omitempty"`
}

const extensionColumns = `e.id,e.decision_key,e.media_id,e.title,e.kind,e.plex_url,e.estimated_at,e.days,e.reason,e.status,e.admin_note,e.created_at,e.updated_at`

func scanExtension(row interface{ Scan(...any) error }, admin bool, now time.Time) (ExtensionRequest, error) {
	var e ExtensionRequest
	var due, created, updated int64
	args := []any{&e.ID, &e.Key, &e.MediaID, &e.Title, &e.Kind, &e.URL, &due, &e.Days, &e.Reason, &e.Status, &e.AdminNote, &created, &updated}
	if admin {
		args = append(args, &e.Requester)
	}
	err := row.Scan(args...)
	e.EstimatedAt, e.CreatedAt, e.UpdatedAt = time.UnixMilli(due).UTC(), time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	if e.Status == "pending" && !e.EstimatedAt.After(now) {
		e.Status = "expired"
	}
	return e, err
}

// Only the server supplies media metadata, taken from a fresh authorized departure.
func (s *Store) CreateExtensionRequest(ctx context.Context, user string, input ExtensionRequest, now time.Time) (ExtensionRequest, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Key == "" || input.MediaID == "" || input.Days < 1 || input.Days > 30 || utf8.RuneCountInString(input.Reason) > 500 || !input.EstimatedAt.After(now) {
		return ExtensionRequest{}, errors.New("invalid extension request")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ExtensionRequest{}, err
	}
	defer tx.Rollback()
	existing, err := scanExtension(tx.QueryRowContext(ctx, `SELECT `+extensionColumns+` FROM extension_requests e WHERE user_id=? AND decision_key=?`, user, input.Key), false, now)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ExtensionRequest{}, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM extension_requests WHERE user_id=? AND ((status='pending' AND estimated_at>?) OR created_at>=?)`, user, now.UnixMilli(), now.Add(-24*time.Hour).UnixMilli()).Scan(&count); err != nil {
		return ExtensionRequest{}, err
	}
	if count >= 10 {
		return ExtensionRequest{}, ErrExtensionLimit
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO extension_requests(user_id,decision_key,media_id,title,kind,plex_url,estimated_at,days,reason,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, user, input.Key, input.MediaID, input.Title, input.Kind, input.URL, input.EstimatedAt.UnixMilli(), input.Days, input.Reason, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return ExtensionRequest{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return ExtensionRequest{}, err
	}
	created, err := scanExtension(tx.QueryRowContext(ctx, `SELECT `+extensionColumns+` FROM extension_requests e WHERE id=?`, id), false, now)
	if err != nil {
		return ExtensionRequest{}, err
	}
	return created, tx.Commit()
}

// An empty owner selects the admin queue; callers must enforce administrator authorization.
func (s *Store) ExtensionRequests(ctx context.Context, owner string, now time.Time) ([]ExtensionRequest, error) {
	query := `SELECT ` + extensionColumns + ` FROM extension_requests e WHERE user_id=? ORDER BY created_at DESC,id DESC LIMIT 100`
	args := []any{owner}
	if owner == "" {
		query = `SELECT ` + extensionColumns + `,u.name FROM extension_requests e JOIN users u ON u.id=e.user_id WHERE status='pending' AND estimated_at>? ORDER BY e.created_at,e.id LIMIT 100`
		args = []any{now.UnixMilli()}
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ExtensionRequest{}
	for rows.Next() {
		item, err := scanExtension(rows, owner == "", now)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// A review records an administrator's answer; it never changes Maintainerr.
func (s *Store) ReviewExtensionRequest(ctx context.Context, id int64, status, note string, now time.Time) error {
	note = strings.TrimSpace(note)
	if id <= 0 || (status != "reviewed" && status != "declined") || utf8.RuneCountInString(note) > 500 {
		return errors.New("invalid review")
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE extension_requests SET status=?,admin_note=?,updated_at=? WHERE id=? AND status='pending' AND estimated_at>?`, status, note, now.UnixMilli(), id, now.UnixMilli())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) CancelExtensionRequest(ctx context.Context, owner string, id int64, now time.Time) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE extension_requests SET status='cancelled',updated_at=? WHERE id=? AND user_id=? AND status='pending' AND estimated_at>?`, now.UnixMilli(), id, owner, now.UnixMilli())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) PruneExtensionRequests(ctx context.Context, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM extension_requests WHERE created_at<?`, now.Add(-90*24*time.Hour).UnixMilli())
	return err
}
