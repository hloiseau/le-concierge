package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Store struct{ DB *sql.DB }
type User struct {
	ID            string `json:"-"`
	DiscordID     string `json:"discordId,omitempty"`
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
	if version > 9 {
		tx.Rollback()
		return fail(errors.New("database schema is newer than this binary; restore a compatible backup"))
	}
	if _, e = tx.Exec(`CREATE TABLE IF NOT EXISTS users(discord_id TEXT PRIMARY KEY, name TEXT NOT NULL, plex_id TEXT UNIQUE, seerr_id INTEGER UNIQUE CHECK(seerr_id>0), notifications INTEGER NOT NULL DEFAULT 0 CHECK(notifications IN (0,1)), created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
 INSERT OR IGNORE INTO schema_migrations(version) VALUES(1);`); e != nil {
		tx.Rollback()
		return fail(e)
	}
	if version < 2 {
		// Preserve v1 identities, preferences and links while making Discord optional.
		if _, e = tx.Exec(`CREATE TABLE users_v2(id TEXT PRIMARY KEY, discord_id TEXT UNIQUE, name TEXT NOT NULL, plex_id TEXT UNIQUE, seerr_id INTEGER UNIQUE CHECK(seerr_id>0), notifications INTEGER NOT NULL DEFAULT 0 CHECK(notifications IN (0,1)), created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
INSERT INTO users_v2(id,discord_id,name,plex_id,seerr_id,notifications,created_at) SELECT discord_id,discord_id,name,plex_id,seerr_id,notifications,created_at FROM users;
DROP TABLE users;
ALTER TABLE users_v2 RENAME TO users;
INSERT INTO schema_migrations(version) VALUES(2);`); e != nil {
			tx.Rollback()
			return fail(e)
		}
	}
	if version < 3 {
		if _, e = tx.Exec(remindersSchema); e != nil {
			tx.Rollback()
			return fail(e)
		}
	}
	if version < 4 {
		if _, e = tx.Exec(subscriptionsSchema + notificationDeliverySchema + extensionRequestsSchema + `INSERT INTO schema_migrations(version) VALUES(4);`); e != nil {
			tx.Rollback()
			return fail(e)
		}
	}
	if version < 5 {
		if _, e = tx.Exec(availabilityDeliverySchema + metricsSchema + availableRequestRemindersSchema + `INSERT INTO schema_migrations(version) VALUES(5);`); e != nil {
			tx.Rollback()
			return fail(e)
		}
	}
	if version < 6 {
		if _, e = tx.Exec(channelReminderSchema + `INSERT INTO schema_migrations(version) VALUES(6);`); e != nil {
			tx.Rollback()
			return fail(e)
		}
	}
	if version < 7 {
		if _, e = tx.Exec(extensionRequestsLifecycleSchema + `INSERT INTO schema_migrations(version) VALUES(7);`); e != nil {
			tx.Rollback()
			return fail(e)
		}
	}
	if version < 8 {
		if _, e = tx.Exec(plexAuthorizationsSchema); e != nil {
			tx.Rollback()
			return fail(e)
		}
	}
	if version < 9 {
		if _, e = tx.Exec(bookTrackingSchema + `INSERT INTO schema_migrations(version) VALUES(9);`); e != nil {
			tx.Rollback()
			return fail(e)
		}
	}
	if e = tx.Commit(); e != nil {
		return fail(e)
	}
	return &Store{DB: db}, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) Upsert(ctx context.Context, id, name string) (User, error) {
	_, e := s.DB.ExecContext(ctx, `INSERT INTO users(id,discord_id,name) VALUES(?,?,?) ON CONFLICT(discord_id) DO UPDATE SET name=excluded.name`, id, id, name)
	if e != nil {
		return User{}, e
	}
	return s.UserByDiscord(ctx, id)
}
func (s *Store) User(ctx context.Context, id string) (User, error) {
	var u User
	e := s.DB.QueryRowContext(ctx, `SELECT id,COALESCE(discord_id,''),name,COALESCE(plex_id,''),COALESCE(seerr_id,0),notifications FROM users WHERE id=?`, id).Scan(&u.ID, &u.DiscordID, &u.Name, &u.PlexID, &u.SeerrID, &u.Notifications)
	return u, e
}
func (s *Store) UserByDiscord(ctx context.Context, discord string) (User, error) {
	var id string
	if err := s.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE discord_id=?`, discord).Scan(&id); err != nil {
		return User{}, err
	}
	return s.User(ctx, id)
}

// LoginPlex reuses an existing verified Plex identity, including migrated v1 users.
func (s *Store) LoginPlex(ctx context.Context, plex, name string) (User, error) {
	n, err := strconv.ParseInt(plex, 10, 64)
	if err != nil || n <= 0 {
		return User{}, errors.New("invalid Plex identity")
	}
	if name == "" {
		name = "Membre Plex"
	}
	randomID := make([]byte, 16)
	if _, err := rand.Read(randomID); err != nil {
		return User{}, err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO users(id,plex_id,name) VALUES(?,?,?) ON CONFLICT(plex_id) DO UPDATE SET name=excluded.name`, hex.EncodeToString(randomID), plex, name)
	if err != nil {
		return User{}, err
	}
	var id string
	if err = s.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE plex_id=?`, plex).Scan(&id); err != nil {
		return User{}, err
	}
	return s.User(ctx, id)
}

// LinkDiscord requires both a verified Plex session and a proven Discord login.
// Only a legacy Discord-only row may be merged; a different Plex owner conflicts.
func (s *Store) LinkDiscord(ctx context.Context, id, plex, discord string) (string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var currentDiscord string
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(discord_id,'') FROM users WHERE id=? AND plex_id=?`, id, plex).Scan(&currentDiscord); err != nil {
		return "", err
	}
	if currentDiscord != "" && currentDiscord != discord {
		return "", errors.New("different Discord identity already linked")
	}
	var oldID, oldPlex string
	var oldNotifications bool
	err = tx.QueryRowContext(ctx, `SELECT id,COALESCE(plex_id,''),notifications FROM users WHERE discord_id=?`, discord).Scan(&oldID, &oldPlex, &oldNotifications)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	merged := ""
	if oldID != "" && oldID != id {
		if oldPlex != "" {
			return "", errors.New("Discord identity belongs to another Plex account")
		}
		// Keep the legacy account's reminder settings and history when linking it.
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO reminder_preferences(user_id,days_before,quiet_start,quiet_end,daily_cap,all_unwatched)
SELECT ?,days_before,quiet_start,quiet_end,daily_cap,all_unwatched FROM reminder_preferences WHERE user_id=?`, id, oldID); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE OR IGNORE reminder_outbox SET user_id=? WHERE user_id=?`, id, oldID); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO reminder_deliveries(user_id,local_day,delivered_at) SELECT ?,local_day,delivered_at FROM reminder_deliveries WHERE user_id=?`, id, oldID); err != nil {
			return "", err
		}
		if err = mergeSubscriptions(ctx, tx, id, oldID); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE OR IGNORE extension_requests SET user_id=? WHERE user_id=?`, id, oldID); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM users WHERE id=?`, oldID); err != nil {
			return "", err
		}
		if oldNotifications {
			if _, err = tx.ExecContext(ctx, `UPDATE users SET notifications=1 WHERE id=?`, id); err != nil {
				return "", err
			}
		}
		merged = oldID
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET discord_id=? WHERE id=?`, discord, id); err != nil {
		return "", err
	}
	return merged, tx.Commit()
}

func (s *Store) LinkPlex(ctx context.Context, id, plex string) error {
	if plex == "" {
		return errors.New("empty Plex identity")
	}
	result, e := s.DB.ExecContext(ctx, `UPDATE users SET plex_id=? WHERE id=? AND (plex_id IS NULL OR plex_id=?)`, plex, id, plex)
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
	_, e = s.DB.ExecContext(ctx, `UPDATE users SET seerr_id=? WHERE id=?`, seerr, id)
	return e
}

// SyncSeerr binds the resolution to the Plex identity used for the lookup,
// preventing a concurrent unlink/delete from restoring a stale association.
func (s *Store) SyncSeerr(ctx context.Context, id, plex string, seerr int64) error {
	if plex == "" || seerr <= 0 {
		return errors.New("invalid verified identity")
	}
	r, err := s.DB.ExecContext(ctx, `UPDATE users SET seerr_id=? WHERE id=? AND plex_id=?`, seerr, id, plex)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("Plex identity changed during Seerr lookup")
	}
	return nil
}

func (s *Store) Preferences(ctx context.Context, id string, enabled bool) error {
	_, e := s.DB.ExecContext(ctx, `UPDATE users SET notifications=? WHERE id=?`, enabled, id)
	return e
}
func (s *Store) Unlink(ctx context.Context, id string) error {
	_, e := s.DB.ExecContext(ctx, `UPDATE users SET plex_id=NULL,seerr_id=NULL WHERE id=?`, id)
	return e
}
func (s *Store) Delete(ctx context.Context, id string) error {
	_, e := s.DB.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
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
