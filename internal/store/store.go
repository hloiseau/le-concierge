package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
)

type Store struct{ DB *sql.DB }
type User struct {
	DiscordID     string `json:"discordId"`
	Name          string `json:"name"`
	PlexID        string `json:"plexId,omitempty"`
	SeerrID       int64  `json:"seerrId,omitempty"`
	Notifications bool   `json:"notifications"`
}

func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		f.Close()
		if err = os.Chmod(path, 0600); err != nil {
			return nil, err
		}
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*Store, error) { db.Close(); return nil, e }
	if _, e = db.Exec(`PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;`); e != nil {
		return fail(e)
	}
	tx, e := db.Begin()
	if e != nil {
		return fail(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY)`); e != nil {
		tx.Rollback()
		return fail(e)
	}
	var version int
	if e = tx.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); e != nil {
		tx.Rollback()
		return fail(e)
	}
	if version > 1 {
		tx.Rollback()
		return fail(errors.New("database schema is newer than this binary; restore a compatible backup"))
	}
	if _, e = tx.Exec(`CREATE TABLE IF NOT EXISTS users(discord_id TEXT PRIMARY KEY, name TEXT NOT NULL, plex_id TEXT UNIQUE, seerr_id INTEGER UNIQUE CHECK(seerr_id>0), notifications INTEGER NOT NULL DEFAULT 0 CHECK(notifications IN (0,1)), created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
 INSERT OR IGNORE INTO schema_migrations(version) VALUES(1);`); e != nil {
		tx.Rollback()
		return fail(e)
	}
	if e = tx.Commit(); e != nil {
		return fail(e)
	}
	return &Store{DB: db}, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) Upsert(ctx context.Context, id, name string) (User, error) {
	_, e := s.DB.ExecContext(ctx, `INSERT INTO users(discord_id,name) VALUES(?,?) ON CONFLICT(discord_id) DO UPDATE SET name=excluded.name`, id, name)
	if e != nil {
		return User{}, e
	}
	return s.User(ctx, id)
}
func (s *Store) User(ctx context.Context, id string) (User, error) {
	var u User
	e := s.DB.QueryRowContext(ctx, `SELECT discord_id,name,COALESCE(plex_id,''),COALESCE(seerr_id,0),notifications FROM users WHERE discord_id=?`, id).Scan(&u.DiscordID, &u.Name, &u.PlexID, &u.SeerrID, &u.Notifications)
	return u, e
}
func (s *Store) LinkPlex(ctx context.Context, id, plex string) error {
	if plex == "" {
		return errors.New("empty Plex identity")
	}
	result, e := s.DB.ExecContext(ctx, `UPDATE users SET plex_id=? WHERE discord_id=? AND (plex_id IS NULL OR plex_id=?)`, plex, id, plex)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return errors.New("user missing or different Plex identity already linked; administrator must reset it")
	}
	return nil
}
func (s *Store) LinkSeerr(ctx context.Context, id string, seerr int64) error {
	u, e := s.User(ctx, id)
	if e != nil {
		return e
	}
	if u.PlexID == "" {
		return errors.New("user must first prove their Plex identity")
	}
	if seerr <= 0 {
		return errors.New("invalid Seerr ID")
	}
	_, e = s.DB.ExecContext(ctx, `UPDATE users SET seerr_id=? WHERE discord_id=?`, seerr, id)
	return e
}
func (s *Store) Preferences(ctx context.Context, id string, enabled bool) error {
	_, e := s.DB.ExecContext(ctx, `UPDATE users SET notifications=? WHERE discord_id=?`, enabled, id)
	return e
}
func (s *Store) Unlink(ctx context.Context, id string) error {
	_, e := s.DB.ExecContext(ctx, `UPDATE users SET plex_id=NULL,seerr_id=NULL WHERE discord_id=?`, id)
	return e
}
func (s *Store) Delete(ctx context.Context, id string) error {
	_, e := s.DB.ExecContext(ctx, `DELETE FROM users WHERE discord_id=?`, id)
	return e
}
func (s *Store) Backup(ctx context.Context, path string) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	f.Close()
	_, e = s.DB.ExecContext(ctx, `VACUUM INTO '`+strings.ReplaceAll(path, `'`, `''`)+`'`)
	if e != nil {
		os.Remove(path)
		return fmt.Errorf("backup failed: %w", e)
	}
	return nil
}
