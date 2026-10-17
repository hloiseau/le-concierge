package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var reminderNow = time.Date(2026, time.October, 24, 9, 0, 0, 0, time.UTC)

func reminderStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reminders.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, id := range []string{"alice", "bob"} {
		if _, err = s.Upsert(context.Background(), id, id); err != nil {
			t.Fatal(err)
		}
		if err = s.Preferences(context.Background(), id, true); err != nil {
			t.Fatal(err)
		}
	}
	return s, path
}

func queuedReminder(key string) Reminder {
	return Reminder{DecisionKey: key, MediaKey: "media-" + key, Title: "Movie " + key, MilestoneDays: 2, DueAt: reminderNow.Add(48 * time.Hour), NotBefore: reminderNow}
}

func TestReminderDefaultsAndV2Migration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY); INSERT INTO schema_migrations VALUES(1),(2);
CREATE TABLE users(id TEXT PRIMARY KEY,discord_id TEXT UNIQUE,name TEXT NOT NULL,plex_id TEXT UNIQUE,seerr_id INTEGER UNIQUE,notifications INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
INSERT INTO users(id,discord_id,name,plex_id,notifications) VALUES('alice','123','Alice','42',1),('bob',NULL,'Bob','43',0);`)
	old.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	p, err := s.GetReminderPreferences(ctx, "alice")
	if err != nil || !p.Enabled || !reflect.DeepEqual(p.DaysBefore, []int{7, 2}) || p.QuietStartHour != 22 || p.QuietEndHour != 8 || p.DailyCap != 10 || p.AllUnwatched {
		t.Fatalf("migration defaults: %+v %v", p, err)
	}
	p, err = s.GetReminderPreferences(ctx, "bob")
	if err != nil || p.Enabled {
		t.Fatalf("migration enabled notifications without consent: %+v %v", p, err)
	}
	if _, err = s.GetReminderPreferences(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown preference owner: %v", err)
	}
}

func TestReminderRestartDeduplicationAndDailyDigest(t *testing.T) {
	s, path := reminderStore(t)
	ctx := context.Background()
	p, _ := s.GetReminderPreferences(ctx, "alice")
	p.DailyCap = 2
	if err := s.SaveReminderPreferences(ctx, "alice", p); err != nil {
		t.Fatal(err)
	}
	items := []Reminder{queuedReminder("one"), queuedReminder("two"), queuedReminder("three")}
	for range 2 {
		if err := s.EnqueueReminders(ctx, "alice", items); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	history, err := s.ReminderHistory(ctx, "alice", 100)
	if err != nil || len(history) != 3 {
		t.Fatalf("restart lost/doubled jobs: %+v %v", history, err)
	}
	start := make(chan struct{})
	type result struct {
		items []Reminder
		err   error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			items, err := s.DeliverReminders(ctx, "alice", reminderNow)
			results <- result{items, err}
		}()
	}
	close(start)
	total := 0
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		total += len(r.items)
		for _, item := range r.items {
			if item.State != "sent" || item.SentAt == nil {
				t.Fatalf("delivery not recorded: %+v", item)
			}
		}
	}
	if total != 2 {
		t.Fatalf("daily cap/digest race sent %d items", total)
	}
	if err = s.EnqueueReminders(ctx, "alice", items); err != nil {
		t.Fatal(err)
	}
	next, err := s.DeliverReminders(ctx, "alice", reminderNow.AddDate(0, 0, 1))
	if err != nil || len(next) != 1 {
		t.Fatalf("next day pending item: %+v %v", next, err)
	}
	none, err := s.DeliverReminders(ctx, "alice", reminderNow.AddDate(0, 0, 1).Add(time.Hour))
	if err != nil || len(none) != 0 {
		t.Fatalf("sent item redelivered: %+v %v", none, err)
	}
}

func TestReminderCancellationIsolationAndAccountDeletion(t *testing.T) {
	s, _ := reminderStore(t)
	ctx := context.Background()
	items := []Reminder{queuedReminder("same"), queuedReminder("obsolete")}
	for _, user := range []string{"alice", "bob"} {
		if err := s.EnqueueReminders(ctx, user, items); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CancelObsoleteReminders(ctx, "alice", []string{"same"}); err != nil {
		t.Fatal(err)
	}
	a, _ := s.ReminderHistory(ctx, "alice", 100)
	b, _ := s.ReminderHistory(ctx, "bob", 100)
	if a[0].State != "cancelled" || a[1].State != "pending" || b[0].State != "pending" || b[1].State != "pending" {
		t.Fatalf("cancellation leaked across users: %+v %+v", a, b)
	}
	if err := s.RetryReminder(ctx, "bob", a[1].ID, reminderNow, "temporary"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-user mutation accepted: %v", err)
	}
	if err := s.Preferences(ctx, "alice", false); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueReminders(ctx, "alice", []Reminder{queuedReminder("disabled")}); err != nil {
		t.Fatal(err)
	}
	a, _ = s.ReminderHistory(ctx, "alice", 100)
	if len(a) != 2 || a[1].State != "cancelled" {
		t.Fatalf("disabled preferences retained pending jobs: %+v", a)
	}
	p, _ := s.GetReminderPreferences(ctx, "bob")
	if err := s.SaveReminderPreferences(ctx, "bob", p); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueReminders(ctx, "bob", items); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeliverReminders(ctx, "bob", reminderNow); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"reminder_outbox", "reminder_preferences", "reminder_deliveries"} {
		var count int
		if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE user_id='bob'").Scan(&count); err != nil || count != 0 {
			t.Fatalf("account deletion left %s records: %d %v", table, count, err)
		}
	}
	a, _ = s.ReminderHistory(ctx, "alice", 100)
	if len(a) != 2 {
		t.Fatal("account deletion affected another owner")
	}
}

func TestReminderQuietHoursDSTAndExpiry(t *testing.T) {
	s, _ := reminderStore(t)
	ctx := context.Background()
	r := queuedReminder("dst")
	r.NotBefore = reminderNow.Add(-24 * time.Hour)
	if err := s.EnqueueReminders(ctx, "alice", []Reminder{r}); err != nil {
		t.Fatal(err)
	}
	// The repeated 02:30 in Paris remains quiet on both sides of the DST change.
	for _, raw := range []string{"2026-10-25T00:30:00Z", "2026-10-25T01:30:00Z", "2026-10-25T06:59:00Z"} {
		now, _ := time.Parse(time.RFC3339, raw)
		got, err := s.DeliverReminders(ctx, "alice", now)
		if err != nil || len(got) != 0 {
			t.Fatalf("sent during quiet hours at %s: %+v %v", raw, got, err)
		}
	}
	wake, _ := time.Parse(time.RFC3339, "2026-10-25T07:00:00Z")
	got, err := s.DeliverReminders(ctx, "alice", wake)
	if err != nil || len(got) != 1 {
		t.Fatalf("not sent after quiet hours: %+v %v", got, err)
	}
	expiring := queuedReminder("expired")
	expiring.DueAt = reminderNow.Add(time.Hour)
	if err = s.EnqueueReminders(ctx, "bob", []Reminder{expiring}); err != nil {
		t.Fatal(err)
	}
	got, err = s.DeliverReminders(ctx, "bob", expiring.DueAt)
	if err != nil || len(got) != 0 {
		t.Fatalf("expired deadline delivered: %+v %v", got, err)
	}
	history, _ := s.ReminderHistory(ctx, "bob", 100)
	if history[0].State != "cancelled" {
		t.Fatal("expired job not cancelled")
	}
	if err = s.PruneReminders(ctx, reminderNow.AddDate(0, 0, 94)); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"alice", "bob"} {
		history, _ = s.ReminderHistory(ctx, user, 100)
		if len(history) != 0 {
			t.Fatal("retention left personal history")
		}
	}
}

func TestReminderRetryPersistsAndStops(t *testing.T) {
	s, path := reminderStore(t)
	ctx := context.Background()
	r := queuedReminder("retry")
	if err := s.EnqueueReminders(ctx, "alice", []Reminder{r}); err != nil {
		t.Fatal(err)
	}
	history, _ := s.ReminderHistory(ctx, "alice", 100)
	id := history[0].ID
	if err := s.RetryReminder(ctx, "alice", id, reminderNow, "source_unavailable"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.EnqueueReminders(ctx, "alice", []Reminder{r}); err != nil {
		t.Fatal(err)
	}
	history, _ = s.ReminderHistory(ctx, "alice", 100)
	if history[0].Attempts != 1 || !history[0].NotBefore.Equal(reminderNow.Add(time.Minute)) {
		t.Fatalf("retry lost on restart/requeue: %+v", history[0])
	}
	got, err := s.DeliverReminders(ctx, "alice", reminderNow)
	if err != nil || len(got) != 0 {
		t.Fatalf("retry backoff ignored: %+v %v", got, err)
	}
	for attempt := 2; attempt <= 5; attempt++ {
		if err = s.RetryReminder(ctx, "alice", id, reminderNow, "source_unavailable"); err != nil {
			t.Fatal(err)
		}
	}
	history, _ = s.ReminderHistory(ctx, "alice", 100)
	if history[0].Attempts != 5 || history[0].State != "failed" {
		t.Fatalf("retry did not stop: %+v", history[0])
	}
	if err = s.EnqueueReminders(ctx, "alice", []Reminder{r}); err != nil {
		t.Fatal(err)
	}
	got, err = s.DeliverReminders(ctx, "alice", reminderNow.Add(time.Hour))
	if err != nil || len(got) != 0 {
		t.Fatalf("failed job unexpectedly restarted: %+v %v", got, err)
	}
}

func TestReminderBatchValidationAndPreferenceValidation(t *testing.T) {
	s, _ := reminderStore(t)
	ctx := context.Background()
	bad := queuedReminder("bad")
	bad.Title = ""
	if err := s.EnqueueReminders(ctx, "alice", []Reminder{queuedReminder("valid"), bad}); err == nil {
		t.Fatal("invalid batch accepted")
	}
	history, _ := s.ReminderHistory(ctx, "alice", 100)
	if len(history) != 0 {
		t.Fatal("invalid batch partially committed")
	}
	p, _ := s.GetReminderPreferences(ctx, "alice")
	for _, days := range [][]int{nil, {2, 2}, {0}, {31}, {1, 2, 3, 4, 5, 6}} {
		t.Run(fmt.Sprint(days), func(t *testing.T) {
			p.DaysBefore = days
			if err := s.SaveReminderPreferences(ctx, "alice", p); err == nil {
				t.Fatal("invalid milestones accepted")
			}
		})
	}
	p.DaysBefore = []int{7, 2}
	if err := s.SaveReminderPreferences(ctx, "missing", p); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown user accepted: %v", err)
	}
	if err := s.EnqueueReminders(ctx, "missing", []Reminder{queuedReminder("valid")}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown owner accepted: %v", err)
	}
	tooMany := make([]Reminder, 1001)
	if err := s.EnqueueReminders(ctx, "alice", tooMany); err == nil {
		t.Fatal("unbounded enqueue accepted")
	}
	if err := s.CancelObsoleteReminders(ctx, "alice", make([]string, 1001)); err == nil {
		t.Fatal("unbounded reconciliation accepted")
	}
	if _, err := s.ReminderHistory(ctx, "alice", 201); err == nil {
		t.Fatal("unbounded history accepted")
	}
	for _, change := range []func(*ReminderPreferences){
		func(p *ReminderPreferences) { p.QuietStartHour = -1 },
		func(p *ReminderPreferences) { p.QuietEndHour = 24 },
		func(p *ReminderPreferences) { p.DailyCap = 0 },
		func(p *ReminderPreferences) { p.DailyCap = 101 },
	} {
		invalid := p
		change(&invalid)
		if err := ValidateReminderPreferences(invalid); err == nil {
			t.Fatalf("invalid preferences accepted: %+v", invalid)
		}
	}
}

func TestLegacyAccountMergePreservesReminders(t *testing.T) {
	s, _ := reminderStore(t)
	ctx := context.Background()
	p, _ := s.GetReminderPreferences(ctx, "alice")
	p.DailyCap, p.DaysBefore = 3, []int{5, 1}
	if err := s.SaveReminderPreferences(ctx, "alice", p); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueReminders(ctx, "alice", []Reminder{queuedReminder("legacy")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeliverReminders(ctx, "alice", reminderNow); err != nil {
		t.Fatal(err)
	}
	user, err := s.LoginPlex(ctx, "42", "Alice Plex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.LinkDiscord(ctx, user.ID, "42", "alice"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetReminderPreferences(ctx, user.ID)
	if err != nil || !reflect.DeepEqual(got, p) {
		t.Fatalf("merge lost reminder preferences: %+v %v", got, err)
	}
	history, err := s.ReminderHistory(ctx, user.ID, 100)
	if err != nil || len(history) != 1 || history[0].State != "sent" {
		t.Fatalf("merge lost reminder history: %+v %v", history, err)
	}
	if err = s.EnqueueReminders(ctx, user.ID, []Reminder{queuedReminder("after-merge")}); err != nil {
		t.Fatal(err)
	}
	delivered, err := s.DeliverReminders(ctx, user.ID, reminderNow)
	if err != nil || len(delivered) != 0 {
		t.Fatalf("merge lost daily delivery limit: %+v %v", delivered, err)
	}
}
