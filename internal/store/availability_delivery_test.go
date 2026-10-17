package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

var availabilityNow = time.Date(2026, time.October, 16, 10, 0, 0, 0, time.UTC)

func availabilityStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "availability.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err = s.Upsert(t.Context(), "11111", "Alice"); err != nil {
		t.Fatal(err)
	}
	if err = s.LinkPlex(t.Context(), "11111", "11111"); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetReminderPreferences(t.Context(), "11111")
	if err != nil {
		t.Fatal(err)
	}
	p.Enabled = true
	p.DiscordDM = true
	p.QuietStartHour = 0
	p.QuietEndHour = 0
	if err = s.SaveReminderPreferences(t.Context(), "11111", p); err != nil {
		t.Fatal(err)
	}
	return s, path
}

func availabilityFollow(t *testing.T, s *Store, id int64, channel string) Subscription {
	t.Helper()
	item, err := s.SaveSubscription(t.Context(), "11111", SubscriptionTarget{Type: "movie", TMDBID: id}, channel)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func observeAvailability(t *testing.T, s *Store, item Subscription, now time.Time) {
	t.Helper()
	if err := s.ObserveAvailability(t.Context(), "11111", item.ID, now, false); err != nil {
		t.Fatal(err)
	}
}

func TestAvailabilityOutboxDedupRestartAndBoundedRetry(t *testing.T) {
	s, path := availabilityStore(t)
	item := availabilityFollow(t, s, 12, "discord")
	now := availabilityNow
	for range 2 {
		observeAvailability(t, s, item, now)
		if err := s.DeliverAvailability(t.Context(), "11111", []int64{item.ID}, now, true); err != nil {
			t.Fatal(err)
		}
	}
	inbox, err := s.AvailabilityInbox(t.Context(), "11111")
	if err != nil || len(inbox) != 1 {
		t.Fatalf("duplicate/missing inbox: %+v %v", inbox, err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	d, err := s.ClaimAvailabilityDiscord(t.Context(), "11111", now)
	if err != nil || d == nil || d.Attempts != 1 {
		t.Fatalf("restart lost job: %+v %v", d, err)
	}
	if other, err := s.ClaimAvailabilityDiscord(t.Context(), "11111", now); err != nil || other != nil {
		t.Fatalf("concurrent claim: %+v %v", other, err)
	}
	nonce := d.Nonce
	for attempt := 1; attempt <= 5; attempt++ {
		if d.Attempts != attempt || d.Nonce != nonce {
			t.Fatalf("retry identity: %+v", d)
		}
		if err = s.FinishAvailabilityDiscord(t.Context(), *d, now, "rate_limited", 3*time.Minute, false); err != nil {
			t.Fatal(err)
		}
		if premature, err := s.ClaimAvailabilityDiscord(t.Context(), "11111", now.Add(time.Second)); err != nil || premature != nil {
			t.Fatalf("backoff ignored: %+v %v", premature, err)
		}
		now = now.Add(time.Hour)
		d, err = s.ClaimAvailabilityDiscord(t.Context(), "11111", now)
		if err != nil {
			t.Fatal(err)
		}
		if attempt < 5 && d == nil {
			t.Fatal("retry stopped too early")
		}
	}
	if d != nil {
		t.Fatal("unbounded retry")
	}
	var jobs int
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM availability_discord_deliveries`).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("duplicate jobs: %d %v", jobs, err)
	}
}

func TestAvailabilityNoHistoricalBackfillAndNoSelfRequestDuplicate(t *testing.T) {
	for _, mode := range []string{"self", "historical", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := availabilityStore(t)
			item := availabilityFollow(t, s, 12, "discord")
			if mode == "historical" {
				if _, err := s.MarkSubscriptionAvailable(t.Context(), "11111", item.ID, availabilityNow.Add(-time.Hour)); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "disabled" {
				if err := s.Preferences(t.Context(), "11111", false); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.ObserveAvailability(t.Context(), "11111", item.ID, availabilityNow, mode == "self"); err != nil {
				t.Fatal(err)
			}
			if err := s.Preferences(t.Context(), "11111", true); err != nil {
				t.Fatal(err)
			}
			observeAvailability(t, s, item, availabilityNow.Add(time.Minute))
			if err := s.DeliverAvailability(t.Context(), "11111", []int64{item.ID}, availabilityNow.Add(time.Minute), true); err != nil {
				t.Fatal(err)
			}
			items, err := s.AvailabilityInbox(t.Context(), "11111")
			if err != nil || len(items) != 0 {
				t.Fatalf("unexpected old/self alert: %+v %v", items, err)
			}
		})
	}
}

func TestAvailabilityQuietHoursAndSharedDailyBudget(t *testing.T) {
	s, _ := availabilityStore(t)
	p, _ := s.GetReminderPreferences(t.Context(), "11111")
	p.DailyCap = 2
	p.QuietStartHour = 22
	p.QuietEndHour = 8
	if err := s.SaveReminderPreferences(t.Context(), "11111", p); err != nil {
		t.Fatal(err)
	}
	first := availabilityFollow(t, s, 12, "discord")
	second := availabilityFollow(t, s, 13, "discord")
	quiet := time.Date(2026, 10, 15, 21, 0, 0, 0, time.UTC)
	observeAvailability(t, s, first, quiet)
	observeAvailability(t, s, second, quiet)
	if err := s.DeliverAvailability(t.Context(), "11111", []int64{first.ID, second.ID}, quiet, true); err != nil {
		t.Fatal(err)
	}
	items, _ := s.AvailabilityInbox(t.Context(), "11111")
	if len(items) != 0 {
		t.Fatal("quiet hours ignored")
	}
	r := queuedReminder("budget")
	r.NotBefore = availabilityNow
	r.DueAt = availabilityNow.Add(time.Hour)
	r.ExpiresAt = r.DueAt
	if err := s.EnqueueReminders(t.Context(), "11111", []Reminder{r}); err != nil {
		t.Fatal(err)
	}
	if delivered, err := s.DeliverRemindersWithDiscord(t.Context(), "11111", availabilityNow, true); err != nil || len(delivered) != 1 {
		t.Fatalf("reminder delivery: %+v %v", delivered, err)
	}
	observeAvailability(t, s, first, availabilityNow)
	observeAvailability(t, s, second, availabilityNow)
	if err := s.DeliverAvailability(t.Context(), "11111", []int64{first.ID, second.ID}, availabilityNow, true); err != nil {
		t.Fatal(err)
	}
	items, _ = s.AvailabilityInbox(t.Context(), "11111")
	if len(items) != 1 {
		t.Fatalf("global budget ignored: %d", len(items))
	}
	if err := s.DeleteSubscription(t.Context(), "11111", items[0].ID); err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	used, err := notificationItemsDeliveredToday(t.Context(), tx, "11111", availabilityNow)
	tx.Rollback()
	if err != nil || used != 2 {
		t.Fatalf("unsubscribe reset budget: %d %v", used, err)
	}
	observeAvailability(t, s, second, availabilityNow.AddDate(0, 0, 1))
	if err := s.DeliverAvailability(t.Context(), "11111", []int64{second.ID}, availabilityNow.AddDate(0, 0, 1), true); err != nil {
		t.Fatal(err)
	}
	items, _ = s.AvailabilityInbox(t.Context(), "11111")
	if len(items) != 1 {
		t.Fatalf("next-day queued event missing: %d", len(items))
	}
}

func TestAvailabilityClaimRevocationUnsubscribeAndPreferenceChange(t *testing.T) {
	for _, mode := range []string{"unsubscribe", "channel", "preferences", "plex", "discord"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := availabilityStore(t)
			item := availabilityFollow(t, s, 12, "discord")
			observeAvailability(t, s, item, availabilityNow)
			if err := s.DeliverAvailability(t.Context(), "11111", []int64{item.ID}, availabilityNow, true); err != nil {
				t.Fatal(err)
			}
			d, err := s.ClaimAvailabilityDiscord(t.Context(), "11111", availabilityNow)
			if err != nil || d == nil {
				t.Fatalf("claim %+v %v", d, err)
			}
			switch mode {
			case "unsubscribe":
				err = s.DeleteSubscription(t.Context(), "11111", item.ID)
			case "channel":
				err = s.UpdateSubscriptionChannel(t.Context(), "11111", item.ID, "hub")
			case "preferences":
				err = s.Preferences(t.Context(), "11111", false)
			case "plex":
				err = s.Unlink(t.Context(), "11111")
			case "discord":
				_, err = s.DB.Exec(`UPDATE users SET discord_id='22222' WHERE id='11111'`)
			}
			if err != nil {
				t.Fatal(err)
			}
			active, err := s.AvailabilityDiscordActive(t.Context(), *d, availabilityNow)
			if err != nil || active {
				t.Fatalf("revoked job active: %v %v", active, err)
			}
			if err = s.FinishAvailabilityDiscord(t.Context(), *d, availabilityNow, "", 0, false); err == nil {
				t.Fatal("revoked claim completed")
			}
		})
	}
}

func TestAvailabilityBudgetAlsoLimitsFollowingReminderDigest(t *testing.T) {
	s, _ := availabilityStore(t)
	p, _ := s.GetReminderPreferences(t.Context(), "11111")
	p.DailyCap = 2
	if err := s.SaveReminderPreferences(t.Context(), "11111", p); err != nil {
		t.Fatal(err)
	}
	item := availabilityFollow(t, s, 12, "hub")
	observeAvailability(t, s, item, availabilityNow)
	if err := s.DeliverAvailability(t.Context(), "11111", []int64{item.ID}, availabilityNow, false); err != nil {
		t.Fatal(err)
	}
	reminders := []Reminder{queuedReminder("one"), queuedReminder("two")}
	for i := range reminders {
		reminders[i].NotBefore = availabilityNow
		reminders[i].DueAt = availabilityNow.Add(time.Hour)
		reminders[i].ExpiresAt = reminders[i].DueAt
	}
	if err := s.EnqueueReminders(t.Context(), "11111", reminders); err != nil {
		t.Fatal(err)
	}
	delivered, err := s.DeliverRemindersWithDiscord(t.Context(), "11111", availabilityNow, true)
	if err != nil || len(delivered) != 1 {
		t.Fatalf("reminders ignored availability budget: %+v %v", delivered, err)
	}
}

func TestAvailabilityPruneAndForeignOwner(t *testing.T) {
	s, _ := availabilityStore(t)
	item := availabilityFollow(t, s, 12, "hub")
	observeAvailability(t, s, item, availabilityNow)
	if err := s.DeliverAvailability(t.Context(), "11111", []int64{item.ID}, availabilityNow, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert(context.Background(), "22222", "Bob"); err != nil {
		t.Fatal(err)
	}
	if err := s.ObserveAvailability(t.Context(), "22222", item.ID, availabilityNow, false); err != nil {
		t.Fatal(err)
	}
	items, err := s.AvailabilityInbox(t.Context(), "22222")
	if err != nil || len(items) != 0 {
		t.Fatalf("foreign inbox: %+v %v", items, err)
	}
	if err = s.PruneAvailability(t.Context(), availabilityNow.AddDate(0, 0, 91)); err != nil {
		t.Fatal(err)
	}
	items, err = s.AvailabilityInbox(t.Context(), "11111")
	if err != nil || len(items) != 0 {
		t.Fatalf("retention: %+v %v", items, err)
	}
	observeAvailability(t, s, item, availabilityNow.AddDate(0, 0, 92))
	if err = s.DeliverAvailability(t.Context(), "11111", []int64{item.ID}, availabilityNow.AddDate(0, 0, 92), false); err != nil {
		t.Fatal(err)
	}
	items, err = s.AvailabilityInbox(t.Context(), "11111")
	if err != nil || len(items) != 0 {
		t.Fatalf("prune regenerated alert: %+v %v", items, err)
	}
}
