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

func TestChannelReminderProgressiveBackoffPersistsAndHonorsRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		name       string
		retryAfter time.Duration
		waits      []time.Duration
	}{
		{"temporary failure", 0, []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute}},
		{"rate limit", 3 * time.Minute, []time.Duration{3 * time.Minute, 3 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "channel.db")
			db, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
			due := now.Add(24 * time.Hour)
			nonce := ""
			for attempt, wait := range tc.waits {
				currentNonce, claimed, err := db.ClaimChannelReminder(ctx, "123", "one-milestone", due, now)
				if err != nil || !claimed || currentNonce == "" {
					t.Fatalf("attempt %d: %v %v", attempt+1, claimed, err)
				}
				if nonce != "" && currentNonce != nonce {
					t.Fatal("retry changed deduplication nonce")
				}
				nonce = currentNonce
				if err = db.FinishChannelReminder(ctx, "123", "one-milestone", now, "temporary_failure", tc.retryAfter, false); err != nil {
					t.Fatal(err)
				}
				if err = db.Close(); err != nil {
					t.Fatal(err)
				}
				db, err = Open(path)
				if err != nil {
					t.Fatal(err)
				}
				var attempts, count int
				var nextAttempt int64
				if err = db.DB.QueryRow(`SELECT attempts,next_attempt,(SELECT COUNT(*) FROM channel_reminders) FROM channel_reminders WHERE channel_id='123' AND decision_key='one-milestone'`).Scan(&attempts, &nextAttempt, &count); err != nil {
					t.Fatal(err)
				}
				if attempts != attempt+1 || nextAttempt != now.Add(wait).UnixMilli() || count != 1 {
					t.Fatalf("persisted retry: attempts=%d next=%v rows=%d, want delay %v", attempts, time.UnixMilli(nextAttempt), count, wait)
				}
				early := now.Add(wait - time.Millisecond)
				if pending, err := db.ChannelReminderPending(ctx, "123", "one-milestone", early); err != nil || pending {
					t.Fatal("early work admitted after restart", pending, err)
				}
				if _, claimed, err := db.ClaimChannelReminder(ctx, "123", "one-milestone", due, early); err != nil || claimed {
					t.Fatal("backoff bypassed after restart", claimed, err)
				}
				now = now.Add(wait)
			}
			if pending, err := db.ChannelReminderPending(ctx, "123", "one-milestone", now); err != nil || pending {
				t.Fatal("exhausted reminder pending", pending, err)
			}
			if _, claimed, err := db.ClaimChannelReminder(ctx, "123", "one-milestone", due, now); err != nil || claimed {
				t.Fatal("sixth attempt accepted", claimed, err)
			}
		})
	}
}

func TestChannelReminderPermanentFailureStillStopsRetries(t *testing.T) {
	for _, tc := range []struct {
		name      string
		retry     time.Duration
		permanent bool
	}{
		{"permanent", 0, true},
		{"excessive retry-after", 25 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			ctx := context.Background()
			now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
			due := now.Add(24 * time.Hour)
			if _, claimed, err := db.ClaimChannelReminder(ctx, "123", "key", due, now); err != nil || !claimed {
				t.Fatal(claimed, err)
			}
			if err := db.FinishChannelReminder(ctx, "123", "key", now, "unavailable", tc.retry, tc.permanent); err != nil {
				t.Fatal(err)
			}
			if _, claimed, err := db.ClaimChannelReminder(ctx, "123", "key", due, now.Add(48*time.Hour)); err != nil || claimed {
				t.Fatal("permanent failure retried", claimed, err)
			}
		})
	}
}
