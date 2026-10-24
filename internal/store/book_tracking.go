package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const bookTrackingSchema = `
CREATE TABLE book_request_matches(
 id INTEGER PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 source_scope TEXT NOT NULL,
 request_id TEXT NOT NULL,
 book_id TEXT NOT NULL,
 book_json TEXT NOT NULL,
 request_json TEXT NOT NULL,
 revision TEXT NOT NULL,
 confirmed_at INTEGER NOT NULL,
 verified_at INTEGER,
 checked_at INTEGER NOT NULL,
 request_page INTEGER NOT NULL CHECK(request_page BETWEEN 1 AND 1001),
 catalog_page INTEGER NOT NULL CHECK(catalog_page BETWEEN 1 AND 1001),
 active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
 UNIQUE(user_id,source_scope,request_id,book_id)
);
CREATE UNIQUE INDEX book_request_active_match ON book_request_matches(user_id,source_scope,request_id) WHERE active=1;
CREATE INDEX book_tracking_due ON book_request_matches(user_id,source_scope,active,checked_at);
CREATE TABLE book_availability_events(
 match_id INTEGER PRIMARY KEY REFERENCES book_request_matches(id) ON DELETE CASCADE,
 detected_at INTEGER NOT NULL,
 book_json TEXT NOT NULL,
 request_json TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('verified','cancelled'))
);
CREATE TRIGGER book_tracking_identity_changed AFTER UPDATE OF plex_id ON users
WHEN OLD.plex_id IS NOT NEW.plex_id
BEGIN DELETE FROM book_request_matches WHERE user_id=NEW.id; END;
`

var ErrBookMatchLimit = errors.New("too many active book matches")

// BookMatch contains only normalized catalogue metadata and source fingerprints.
// Provider credentials stay in the HTTP server's in-memory grants.
type BookMatch struct {
	ID                                         int64
	UserID, Scope, RequestID, BookID, Revision string
	BookJSON, RequestJSON                      []byte
	ConfirmedAt, CheckedAt                     time.Time
	VerifiedAt                                 *time.Time
	RequestPage, CatalogPage                   int
}

const bookMatchColumns = `id,user_id,source_scope,request_id,book_id,book_json,request_json,revision,confirmed_at,verified_at,checked_at,request_page,catalog_page`

func scanBookMatch(row interface{ Scan(...any) error }) (BookMatch, error) {
	var m BookMatch
	var confirmed, checked int64
	var verified sql.NullInt64
	err := row.Scan(&m.ID, &m.UserID, &m.Scope, &m.RequestID, &m.BookID, &m.BookJSON, &m.RequestJSON, &m.Revision, &confirmed, &verified, &checked, &m.RequestPage, &m.CatalogPage)
	m.ConfirmedAt, m.CheckedAt = time.UnixMilli(confirmed).UTC(), time.UnixMilli(checked).UTC()
	if verified.Valid {
		at := time.UnixMilli(verified.Int64).UTC()
		m.VerifiedAt = &at
	}
	return m, err
}

// ConfirmBookMatch records the explicit decision and its verified availability
// atomically. Repeated confirmations of the same pair do not emit another event.
func (s *Store) ConfirmBookMatch(ctx context.Context, m BookMatch, now time.Time) (BookMatch, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return BookMatch{}, err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM book_request_matches WHERE user_id=? AND source_scope=? AND active=1 AND request_id<>?`, m.UserID, m.Scope, m.RequestID).Scan(&count); err != nil {
		return BookMatch{}, err
	}
	if count >= 100 {
		return BookMatch{}, ErrBookMatchLimit
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM book_request_matches WHERE user_id=? AND NOT(source_scope=? AND request_id=? AND book_id=?)`, m.UserID, m.Scope, m.RequestID, m.BookID).Scan(&count); err != nil {
		return BookMatch{}, err
	}
	if count >= 1000 {
		return BookMatch{}, ErrBookMatchLimit
	}
	if _, err = tx.ExecContext(ctx, `UPDATE book_availability_events SET state='cancelled' WHERE match_id IN (SELECT id FROM book_request_matches WHERE user_id=? AND source_scope=? AND request_id=? AND book_id<>?)`, m.UserID, m.Scope, m.RequestID, m.BookID); err != nil {
		return BookMatch{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE book_request_matches SET active=0,checked_at=? WHERE user_id=? AND source_scope=? AND request_id=?`, now.UnixMilli(), m.UserID, m.Scope, m.RequestID); err != nil {
		return BookMatch{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO book_request_matches(user_id,source_scope,request_id,book_id,book_json,request_json,revision,confirmed_at,verified_at,checked_at,request_page,catalog_page)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(user_id,source_scope,request_id,book_id) DO UPDATE SET book_json=excluded.book_json,request_json=excluded.request_json,revision=excluded.revision,verified_at=excluded.verified_at,checked_at=excluded.checked_at,request_page=excluded.request_page,catalog_page=excluded.catalog_page,active=1`, m.UserID, m.Scope, m.RequestID, m.BookID, string(m.BookJSON), string(m.RequestJSON), m.Revision, now.UnixMilli(), now.UnixMilli(), now.UnixMilli(), m.RequestPage, m.CatalogPage)
	if err != nil {
		return BookMatch{}, err
	}
	m, err = scanBookMatch(tx.QueryRowContext(ctx, `SELECT `+bookMatchColumns+` FROM book_request_matches WHERE user_id=? AND source_scope=? AND request_id=? AND active=1`, m.UserID, m.Scope, m.RequestID))
	if err != nil {
		return BookMatch{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO book_availability_events(match_id,detected_at,book_json,request_json,state) VALUES(?,?,?,?,'verified')`, m.ID, now.UnixMilli(), string(m.BookJSON), string(m.RequestJSON)); err != nil {
		return BookMatch{}, err
	}
	// Reconfirmation reactivates the same historical event without replacing its
	// original evidence or identity; a notification consumer must deduplicate it.
	if _, err = tx.ExecContext(ctx, `UPDATE book_availability_events SET state='verified' WHERE match_id=?`, m.ID); err != nil {
		return BookMatch{}, err
	}
	return m, tx.Commit()
}

func (s *Store) BookMatches(ctx context.Context, user, scope string) ([]BookMatch, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+bookMatchColumns+` FROM book_request_matches WHERE user_id=? AND source_scope=? AND active=1 ORDER BY checked_at,id LIMIT 100`, user, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BookMatch{}
	for rows.Next() {
		m, err := scanBookMatch(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (s *Store) RemoveBookMatch(ctx context.Context, user, scope, request string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE book_availability_events SET state='cancelled' WHERE match_id IN (SELECT id FROM book_request_matches WHERE user_id=? AND source_scope=? AND request_id=?)`, user, scope, request); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE book_request_matches SET active=0,verified_at=NULL,checked_at=? WHERE user_id=? AND source_scope=? AND request_id=?`, time.Now().UnixMilli(), user, scope, request); err != nil {
		return err
	}
	return tx.Commit()
}

// ObserveBookMatch advances bounded page scans even when an item moved. A miss
// clears current availability; it never asserts that the item is absent.
func (s *Store) ObserveBookMatch(ctx context.Context, m BookMatch, verified bool, now time.Time) error {
	var at any
	if verified {
		at = now.UnixMilli()
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE book_request_matches SET verified_at=?,checked_at=?,request_page=?,catalog_page=?,book_json=?,request_json=? WHERE id=? AND user_id=? AND source_scope=? AND revision=? AND active=1`, at, now.UnixMilli(), m.RequestPage, m.CatalogPage, string(m.BookJSON), string(m.RequestJSON), m.ID, m.UserID, m.Scope, m.Revision)
	return err
}

func (s *Store) BeginBookMatchCheck(ctx context.Context, m BookMatch, now time.Time) (bool, error) {
	result, err := s.DB.ExecContext(ctx, `UPDATE book_request_matches SET checked_at=? WHERE id=? AND user_id=? AND source_scope=? AND revision=? AND active=1`, now.UnixMilli(), m.ID, m.UserID, m.Scope, m.Revision)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *Store) PruneBookTracking(ctx context.Context, now time.Time) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM book_request_matches WHERE active=0 AND checked_at<?`, now.Add(-90*24*time.Hour).UnixMilli())
	return err
}
