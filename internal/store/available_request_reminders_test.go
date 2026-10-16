package store

import (
	"context"
	"testing"
	"time"
)

func availableQueuedReminder() Reminder {
	added := reminderNow.Add(-40 * 24 * time.Hour)
	return Reminder{DecisionKey: "available:request", MediaKey: "available:42:source", Title: "Requested movie", Reason: "available_unwatched", AvailableAt: &added, NotBefore: added.Add(30 * 24 * time.Hour), ExpiresAt: added.Add(90 * 24 * time.Hour)}
}

func TestAvailableRequestRemindersShareDigestCapAndDurableDedup(t *testing.T) {
	s, path := reminderStore(t)
	ctx := context.Background()
	p, _ := s.GetReminderPreferences(ctx, "alice")
	if p.AvailableUnwatched {
		t.Fatal("legacy/global preference enabled old requests")
	}
	available := availableQueuedReminder()
	if err := s.EnqueueReminders(ctx, "alice", []Reminder{available}); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.ReminderHistory(ctx, "alice", 200)
	if len(rows) != 0 {
		t.Fatal("missing explicit opt in")
	}
	p.AvailableUnwatched, p.DailyCap = true, 1
	if err := s.SaveReminderPreferences(ctx, "alice", p); err != nil {
		t.Fatal(err)
	}
	if err := s.EnqueueReminders(ctx, "alice", []Reminder{available, queuedReminder("urgent")}); err != nil {
		t.Fatal(err)
	}
	sent, err := s.DeliverReminders(ctx, "alice", reminderNow)
	if err != nil || len(sent) != 1 || sent[0].DecisionKey != "urgent" {
		t.Fatalf("removal did not keep shared budget priority: %+v %v", sent, err)
	}
	sent, err = s.DeliverReminders(ctx, "alice", reminderNow.Add(time.Hour))
	if err != nil || len(sent) != 0 {
		t.Fatalf("second digest: %+v %v", sent, err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sent, err = s.DeliverReminders(ctx, "alice", reminderNow.Add(24*time.Hour))
	if err != nil || len(sent) != 1 || sent[0].AvailableAt == nil || !sent[0].DueAt.IsZero() {
		t.Fatalf("restart lost added fact: %+v %v", sent, err)
	}
	if err = s.EnqueueReminders(ctx, "alice", []Reminder{available}); err != nil {
		t.Fatal(err)
	}
	sent, err = s.DeliverReminders(ctx, "alice", reminderNow.Add(48*time.Hour))
	if err != nil || len(sent) != 0 {
		t.Fatalf("request reminder repeated: %+v %v", sent, err)
	}
}

func TestAvailableRequestReminderCannotInventRemovalDatesOrEscapeWindow(t *testing.T) {
	s, _ := reminderStore(t)
	ctx := context.Background()
	for _, mode := range []string{"fake_deadline", "fake_milestone", "missing_added", "too_early", "too_long"} {
		r := availableQueuedReminder()
		switch mode {
		case "fake_deadline":
			r.DueAt = reminderNow
		case "fake_milestone":
			r.MilestoneDays = 7
		case "missing_added":
			r.AvailableAt = nil
		case "too_early":
			r.NotBefore = r.AvailableAt.Add(time.Hour)
		case "too_long":
			r.ExpiresAt = r.NotBefore.Add(61 * 24 * time.Hour)
		}
		if err := s.EnqueueReminders(ctx, "alice", []Reminder{r}); err == nil {
			t.Fatalf("accepted %s", mode)
		}
	}
}

func TestAvailableRequestPendingRefreshPreservesRetryBackoff(t *testing.T) {
	s, _ := reminderStore(t)
	ctx := context.Background()
	p, _ := s.GetReminderPreferences(ctx, "alice")
	p.AvailableUnwatched = true
	s.SaveReminderPreferences(ctx, "alice", p)
	r := availableQueuedReminder()
	s.EnqueueReminders(ctx, "alice", []Reminder{r})
	rows, _ := s.ReminderHistory(ctx, "alice", 200)
	if err := s.RetryReminder(ctx, "alice", rows[0].ID, reminderNow, "source_unavailable"); err != nil {
		t.Fatal(err)
	}
	r.MediaKey = "available:43:replacement"
	if err := s.EnqueueReminders(ctx, "alice", []Reminder{r}); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.ReminderHistory(ctx, "alice", 200)
	if len(rows) != 1 || rows[0].MediaKey != r.MediaKey || rows[0].Attempts != 1 || rows[0].LastError != "source_unavailable" || !rows[0].NotBefore.Equal(reminderNow.Add(time.Minute)) {
		t.Fatalf("refresh reset backoff: %+v", rows)
	}
}
