package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDiscordV3MigrationPreservesInboxWithoutConsent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY);
INSERT INTO schema_migrations VALUES(1),(2);
CREATE TABLE users(id TEXT PRIMARY KEY,discord_id TEXT UNIQUE,name TEXT NOT NULL,plex_id TEXT UNIQUE,seerr_id INTEGER UNIQUE,notifications INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
INSERT INTO users(id,discord_id,name,plex_id,notifications) VALUES('legacy','123','Legacy','42',1);` + remindersSchema + `
INSERT INTO reminder_preferences(user_id,days_before,quiet_start,quiet_end,daily_cap,all_unwatched) VALUES('legacy','[5,1]',20,9,3,0);
INSERT INTO reminder_outbox(user_id,decision_key,media_key,title,plex_url,reason,milestone_days,due_at,not_before,expires_at,created_at,state,sent_at)
VALUES('legacy','pending-key','media-pending','Pending title','','unfinished',1,9999999999999,1,9999999999999,1,'pending',NULL),
('legacy','sent-key','media-sent','Sent title','','unfinished',1,9999999999999,1,9999999999999,1,'sent',2);
INSERT INTO reminder_deliveries(user_id,local_day,delivered_at) VALUES('legacy','2026-10-15',2);`)
	old.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	check := func(db *Store) {
		t.Helper()
		p, err := db.GetReminderPreferences(ctx, "legacy")
		if err != nil || !p.Enabled || p.DiscordDM || p.DailyCap != 3 || p.QuietStartHour != 20 || len(p.DaysBefore) != 2 || p.DaysBefore[0] != 5 {
			t.Fatalf("migrated preferences=%+v %v", p, err)
		}
		history, err := db.ReminderHistory(ctx, "legacy", 10)
		if err != nil || len(history) != 2 || history[0].State != "sent" || history[1].State != "pending" {
			t.Fatalf("migrated history=%+v %v", history, err)
		}
		if deliveryCount(t, db) != 0 {
			t.Fatal("migration backfilled DMs")
		}
	}
	check(s)
	backup := filepath.Join(t.TempDir(), "backup.db")
	if err := s.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	check(s)
	s.Close()
	restored, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	check(restored)
	if _, err = restored.DB.Exec(`INSERT INTO schema_migrations(version) VALUES(999)`); err != nil {
		t.Fatal(err)
	}
	restored.Close()
	if unsupported, err := Open(backup); err == nil {
		unsupported.Close()
		t.Fatal("future schema accepted")
	}
}

func discordDeliveryStore(t *testing.T) (*Store, string) {
	t.Helper()
	s, path := reminderStore(t)
	ctx := context.Background()
	if err := s.LinkPlex(ctx, "alice", "42"); err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetReminderPreferences(ctx, "alice")
	p.DiscordDM = true
	if err := s.SaveReminderPreferences(ctx, "alice", p); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueReminders(ctx, "alice", []Reminder{queuedReminder("first"), queuedReminder("second")}); err != nil {
		t.Fatal(err)
	}
	items, err := s.DeliverRemindersWithDiscord(ctx, "alice", reminderNow, true)
	if err != nil || len(items) != 2 {
		t.Fatalf("deliver=%d %v", len(items), err)
	}
	return s, path
}

func deliveryCount(t *testing.T, s *Store) int {
	t.Helper()
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM notification_deliveries`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestDiscordDeliveryRestartClaimDedupAndLease(t *testing.T) {
	s, path := discordDeliveryStore(t)
	ctx := context.Background()
	if _, err := s.DeliverRemindersWithDiscord(ctx, "alice", reminderNow.Add(time.Minute), true); err != nil {
		t.Fatal(err)
	}
	if deliveryCount(t, s) != 1 {
		t.Fatal("digest queued more than once")
	}
	s.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	claimed := make(chan *NotificationDelivery, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, e := s.ClaimDiscordDelivery(ctx, "alice", reminderNow)
			if e != nil {
				t.Error(e)
			}
			if d != nil {
				claimed <- d
			}
		}()
	}
	wg.Wait()
	close(claimed)
	var first *NotificationDelivery
	for d := range claimed {
		if first != nil {
			t.Fatal("concurrent double claim")
		}
		first = d
	}
	if first == nil || first.Attempts != 1 || first.DiscordID != "alice" || len(first.Nonce) != 24 {
		t.Fatalf("claim=%+v", first)
	}
	items, err := s.DiscordDeliveryItems(ctx, *first)
	if err != nil || len(items) != 2 {
		t.Fatalf("lost digest contents %v %v", items, err)
	}
	second, err := s.ClaimDiscordDelivery(ctx, "alice", reminderNow.Add(2*time.Minute))
	if err != nil || second == nil || second.Nonce != first.Nonce || second.Attempts != 2 {
		t.Fatalf("lease restart=%+v %v", second, err)
	}
	if err = s.CompleteDiscordDelivery(ctx, *first, reminderNow.Add(2*time.Minute)); err == nil {
		t.Fatal("superseded worker finished")
	}
	active, err := s.DiscordDeliveryActive(ctx, *second, reminderNow.Add(2*time.Minute))
	if err != nil || !active {
		t.Fatalf("active=%v %v", active, err)
	}
	if err = s.CompleteDiscordDelivery(ctx, *second, reminderNow.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	third, err := s.ClaimDiscordDelivery(ctx, "alice", reminderNow.Add(time.Hour))
	if err != nil || third != nil {
		t.Fatalf("sent digest duplicated %+v %v", third, err)
	}
}

func TestDiscordDeliveryExplicitConsentAndServerOffNoBackfill(t *testing.T) {
	for _, mode := range []string{"notifications_only", "server_off", "no_discord", "no_plex"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := reminderStore(t)
			ctx := context.Background()
			if mode != "no_plex" {
				if err := s.LinkPlex(ctx, "alice", "42"); err != nil {
					t.Fatal(err)
				}
			}
			p, _ := s.GetReminderPreferences(ctx, "alice")
			if p.DiscordDM {
				t.Fatal("migration silently opted in")
			}
			p.DiscordDM = mode != "notifications_only"
			if err := s.SaveReminderPreferences(ctx, "alice", p); err != nil {
				t.Fatal(err)
			}
			if mode == "no_discord" {
				if _, err := s.DB.Exec(`UPDATE users SET discord_id=NULL WHERE id='alice'`); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.EnqueueReminders(ctx, "alice", []Reminder{queuedReminder("first")}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DeliverRemindersWithDiscord(ctx, "alice", reminderNow, mode != "server_off"); err != nil {
				t.Fatal(err)
			}
			if deliveryCount(t, s) != 0 {
				t.Fatal("DM queued without separate enabled channel+identity+consent")
			}
			if _, err := s.DeliverRemindersWithDiscord(ctx, "alice", reminderNow.Add(time.Minute), true); err != nil {
				t.Fatal(err)
			}
			if deliveryCount(t, s) != 0 {
				t.Fatal("enabled channel backfilled old inbox")
			}
		})
	}
}

func TestDiscordDeliveryRevocationAndClaimExpiry(t *testing.T) {
	for _, mode := range []string{"opt_out", "disable_notifications", "unlink", "changed_discord", "delete", "expired_lease"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := discordDeliveryStore(t)
			ctx := context.Background()
			d, err := s.ClaimDiscordDelivery(ctx, "alice", reminderNow)
			if err != nil || d == nil {
				t.Fatalf("claim %v", err)
			}
			now := reminderNow
			switch mode {
			case "opt_out":
				p, _ := s.GetReminderPreferences(ctx, "alice")
				p.DiscordDM = false
				err = s.SaveReminderPreferences(ctx, "alice", p)
			case "disable_notifications":
				err = s.Preferences(ctx, "alice", false)
			case "unlink":
				err = s.Unlink(ctx, "alice")
			case "changed_discord":
				_, err = s.DB.Exec(`UPDATE users SET discord_id='new_discord' WHERE id='alice'`)
			case "delete":
				err = s.Delete(ctx, "alice")
			case "expired_lease":
				now = now.Add(2 * time.Minute)
			}
			if err != nil {
				t.Fatal(err)
			}
			active, err := s.DiscordDeliveryActive(ctx, *d, now)
			if err != nil || active {
				t.Fatalf("revoked delivery active=%v %v", active, err)
			}
		})
	}
}

func TestDiscordDeliveryBoundedDurableRetries(t *testing.T) {
	s, path := discordDeliveryStore(t)
	ctx := context.Background()
	now := reminderNow
	for attempt := 1; attempt <= 5; attempt++ {
		d, err := s.ClaimDiscordDelivery(ctx, "alice", now)
		if err != nil || d == nil || d.Attempts != attempt {
			t.Fatalf("attempt %d: %+v %v", attempt, d, err)
		}
		if err = s.RetryDiscordDelivery(ctx, *d, now, 90*time.Second, false, "rate_limited"); err != nil {
			t.Fatal(err)
		}
		if next, err := s.ClaimDiscordDelivery(ctx, "alice", now.Add(30*time.Second)); err != nil || next != nil {
			t.Fatalf("backoff skipped %+v %v", next, err)
		}
		if attempt == 1 {
			s.Close()
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
		}
		now = now.Add(20 * time.Minute)
	}
	if d, err := s.ClaimDiscordDelivery(ctx, "alice", now); err != nil || d != nil {
		t.Fatalf("unbounded retries: %+v %v", d, err)
	}
	var state string
	if err := s.DB.QueryRow(`SELECT state FROM notification_deliveries`).Scan(&state); err != nil || state != "failed" {
		t.Fatalf("state %s %v", state, err)
	}
}

func TestDiscordDeliveryPermanentFailureAndSanitizedReasons(t *testing.T) {
	s, _ := discordDeliveryStore(t)
	ctx := context.Background()
	d, _ := s.ClaimDiscordDelivery(ctx, "alice", reminderNow)
	if err := s.RetryDiscordDelivery(ctx, *d, reminderNow, 0, false, "https://private?token=secret"); err == nil {
		t.Fatal("stored private failure text")
	}
	if err := s.RetryDiscordDelivery(ctx, *d, reminderNow, 0, true, "dm_unavailable"); err != nil {
		t.Fatal(err)
	}
	if next, err := s.ClaimDiscordDelivery(ctx, "alice", reminderNow.Add(time.Hour)); err != nil || next != nil {
		t.Fatalf("closed DM retried %+v %v", next, err)
	}
}

func TestDiscordDeliveryRetryNeverCrossesParisDigestDay(t *testing.T) {
	s, _ := discordDeliveryStore(t)
	ctx := context.Background()
	// Paris is still on UTC+2 here, the evening before its DST transition.
	beforeMidnight := time.Date(2026, time.October, 24, 21, 59, 30, 0, time.UTC)
	afterMidnight := beforeMidnight.Add(40 * time.Second)
	d, err := s.ClaimDiscordDelivery(ctx, "alice", beforeMidnight)
	if err != nil || d == nil {
		t.Fatalf("claim=%+v %v", d, err)
	}
	if active, err := s.DiscordDeliveryActive(ctx, *d, beforeMidnight); err != nil || !active {
		t.Fatalf("same day active=%v %v", active, err)
	}
	if active, err := s.DiscordDeliveryActive(ctx, *d, afterMidnight); err != nil || active {
		t.Fatalf("old digest active after Paris midnight: %v %v", active, err)
	}
	if next, err := s.ClaimDiscordDelivery(ctx, "alice", afterMidnight); err != nil || next != nil {
		t.Fatalf("old digest reclaimed: %+v %v", next, err)
	}
	var state string
	if err := s.DB.QueryRow(`SELECT state FROM notification_deliveries`).Scan(&state); err != nil || state != "failed" {
		t.Fatalf("old digest state=%s %v", state, err)
	}
	newDay := reminderNow.Add(24 * time.Hour)
	reminder := queuedReminder("new-day")
	reminder.NotBefore = newDay
	if err := s.EnqueueReminders(ctx, "alice", []Reminder{reminder}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeliverRemindersWithDiscord(ctx, "alice", newDay, true); err != nil {
		t.Fatal(err)
	}
	next, err := s.ClaimDiscordDelivery(ctx, "alice", newDay)
	if err != nil || next == nil || next.ID == d.ID {
		t.Fatalf("new daily digest unavailable: %+v %v", next, err)
	}
}
