package store

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestChannelReminderDedupRetryAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	due := now.Add(24 * time.Hour)
	nonce, ok, err := db.ClaimChannelReminder(ctx, "123", "key", due, now)
	if err != nil || !ok || nonce == "" {
		t.Fatal(nonce, ok, err)
	}
	if _, ok, err = db.ClaimChannelReminder(ctx, "123", "key", due, now); err != nil || ok {
		t.Fatal("claim lease ignored", err)
	}
	if err = db.FinishChannelReminder(ctx, "123", "key", now, "rate_limited", 2*time.Minute, false); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = db.ClaimChannelReminder(ctx, "123", "key", due, now.Add(time.Minute)); err != nil || ok {
		t.Fatal("retry delay ignored", err)
	}
	if n, claimed, e := db.ClaimChannelReminder(ctx, "123", "key", due, now.Add(2*time.Minute)); e != nil || !claimed || n != nonce {
		t.Fatal("retry nonce not stable", e)
	}
	if err = db.FinishChannelReminder(ctx, "123", "key", now, "", 0, false); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, ok, err = db.ClaimChannelReminder(ctx, "123", "key", due, now.Add(time.Hour)); err != nil || ok {
		t.Fatal("sent reminder replayed after restart", err)
	}
	if _, ok, err = db.ClaimChannelReminder(ctx, "123", "changed-deadline", due.Add(time.Hour), now); err != nil || !ok {
		t.Fatal("new deadline lost", err)
	}
}

func TestChannelReminderClaimSerializesAndCapsDailyDelivery(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	due := now.Add(time.Hour)
	var claims atomic.Int32
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, ok, err := db.ClaimChannelReminder(ctx, "123", "same", due, now)
			if err != nil {
				t.Error(err)
			}
			if ok {
				claims.Add(1)
			}
		}()
	}
	workers.Wait()
	if claims.Load() != 1 {
		t.Fatal("concurrent duplicate claim", claims.Load())
	}
	for i := 0; i < 9; i++ {
		_, ok, err := db.ClaimChannelReminder(ctx, "123", string(rune('a'+i)), due, now)
		if err != nil || !ok {
			t.Fatal("unexpected cap", err)
		}
	}
	if _, ok, err := db.ClaimChannelReminder(ctx, "123", "over-cap", due, now); err != nil || ok {
		t.Fatal("daily cap ignored", err)
	}
}
