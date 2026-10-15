package store

import (
	"context"
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
