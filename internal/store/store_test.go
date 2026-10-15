package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityConstraintsAndBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ctx := context.Background()
	for _, id := range []string{"12345", "67890"} {
		if _, e = s.Upsert(ctx, id, "User"); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.LinkSeerr(ctx, "12345", 10); e == nil {
		t.Fatal("unverified user linked to Seerr")
	}
	if e = s.LinkPlex(ctx, "12345", "100"); e != nil {
		t.Fatal(e)
	}
	if e = s.LinkPlex(ctx, "67890", "100"); e == nil {
		t.Fatal("Plex identity shared across users")
	}
	if e = s.LinkPlex(ctx, "12345", "200"); e == nil {
		t.Fatal("Plex identity silently replaced")
	}
	if e = s.LinkPlex(ctx, "67890", "200"); e != nil {
		t.Fatal(e)
	}
	if e = s.LinkSeerr(ctx, "12345", 10); e != nil {
		t.Fatal(e)
	}
	if e = s.LinkSeerr(ctx, "67890", 10); e == nil {
		t.Fatal("Seerr identity shared across users")
	}
	if e = s.Preferences(ctx, "12345", true); e != nil {
		t.Fatal(e)
	}
	backup := filepath.Join(t.TempDir(), "backup.db")
	if e = s.Backup(ctx, backup); e != nil {
		t.Fatal(e)
	}
	restored, e := Open(backup)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	u, e := restored.User(ctx, "12345")
	if e != nil || u.SeerrID != 10 || !u.Notifications {
		t.Fatalf("backup lost data: %+v %v", u, e)
	}
	if e = s.Backup(ctx, backup); e == nil {
		t.Fatal("existing backup overwritten")
	}
	info, _ := os.Stat(backup)
	if info.Mode().Perm() != 0600 {
		t.Fatal("backup not private")
	}
	if e = s.Unlink(ctx, "12345"); e != nil {
		t.Fatal(e)
	}
	u, _ = s.User(ctx, "12345")
	if u.PlexID != "" || u.SeerrID != 0 {
		t.Fatal("unlink left identity")
	}
	if e = s.Delete(ctx, "67890"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.User(ctx, "67890"); e == nil {
		t.Fatal("account still present")
	}
}

func TestConcurrentPlexLinkCannotReplaceIdentity(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err := s.Upsert(ctx, "12345", "User"); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, plex := range []string{"100", "200"} {
		go func(plex string) { <-start; results <- s.LinkPlex(ctx, "12345", plex) }(plex)
	}
	close(start)
	successes := 0
	for range 2 {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("got %d successful links, want exactly one", successes)
	}
	u, err := s.User(ctx, "12345")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LinkPlex(ctx, "12345", u.PlexID); err != nil {
		t.Fatal("same identity must remain idempotent:", err)
	}
	if err := s.LinkPlex(ctx, "missing", "300"); err == nil {
		t.Fatal("missing user accepted")
	}
}

func TestAutomaticSeerrSyncCannotRestoreUnlinkedIdentity(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err = s.Upsert(ctx, "11111", "User"); err != nil {
		t.Fatal(err)
	}
	if err = s.LinkPlex(ctx, "11111", "123"); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncSeerr(ctx, "11111", "999", 42); err == nil {
		t.Fatal("different Plex identity accepted")
	}
	if err = s.SyncSeerr(ctx, "11111", "123", 42); err != nil {
		t.Fatal(err)
	}
	if err = s.Unlink(ctx, "11111"); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncSeerr(ctx, "11111", "123", 42); err == nil {
		t.Fatal("stale lookup restored an unlinked account")
	}
}

func TestV1MigrationPreservesAccountsAndPlexLogin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY); INSERT INTO schema_migrations VALUES(1);
 CREATE TABLE users(discord_id TEXT PRIMARY KEY,name TEXT NOT NULL,plex_id TEXT UNIQUE,seerr_id INTEGER UNIQUE,notifications INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
 INSERT INTO users(discord_id,name,plex_id,seerr_id,notifications) VALUES('11111','Alice','77',42,1),('22222','Legacy',NULL,NULL,1);`)
	legacy.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	u, err := s.LoginPlex(ctx, "77", "Alice Plex")
	if err != nil || u.ID != "11111" || u.DiscordID != "11111" || u.SeerrID != 42 || !u.Notifications {
		t.Fatalf("migration lost identity/preferences: %+v %v", u, err)
	}
	fresh, err := s.LoginPlex(ctx, "88", "Bob")
	if err != nil || fresh.DiscordID != "" {
		t.Fatalf("Plex requires Discord: %+v %v", fresh, err)
	}
	merged, err := s.LinkDiscord(ctx, fresh.ID, "88", "22222")
	if err != nil || merged != "22222" {
		t.Fatalf("legacy merge failed: %s %v", merged, err)
	}
	linked, err := s.UserByDiscord(ctx, "22222")
	if err != nil || linked.ID != fresh.ID || !linked.Notifications {
		t.Fatal("legacy preferences/identity lost")
	}
	if _, err = s.LinkDiscord(ctx, fresh.ID, "88", "11111"); err == nil {
		t.Fatal("Discord moved between Plex owners")
	}
	other, err := s.LoginPlex(ctx, "99", "Other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.LinkDiscord(ctx, other.ID, "99", "11111"); err == nil {
		t.Fatal("existing Discord account stolen")
	}
	if _, err = s.LinkDiscord(ctx, other.ID, "777", "33333"); err == nil {
		t.Fatal("unverified Plex identity linked")
	}
	s.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	u, err = reopened.LoginPlex(ctx, "77", "Alice")
	if err != nil || u.ID != "11111" || !u.Notifications {
		t.Fatal("reopen reapplied migration")
	}
}
