package reminders

import (
	"testing"
	"time"
)

func TestDecisions(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	base := Candidate{UserID: "1", ServerID: "server", MediaID: "film", ActionID: "rule-3", Accessible: true, Requested: true, Watch: Unstarted, Kind: "movie", RuleActive: true, DeletesFiles: true, LeavesAt: now.Add(24 * time.Hour), SourceUpdatedAt: now}
	prefs := Preferences{Enabled: true, DaysBefore: []int{7, 2}, QuietStart: 23, QuietEnd: 8}
	d := Decide(now, base, prefs)
	if !d.Notify || d.MilestoneDays != 2 || len(d.Key) != 64 {
		t.Fatalf("unexpected decision %+v", d)
	}
	cases := []struct {
		name, reason string
		change       func(*Candidate)
	}{
		{"watched", "already_completed", func(c *Candidate) { c.Watch = Completed }},
		{"unknown", "watch_unknown", func(c *Candidate) { c.Watch = Unknown }},
		{"revoked", "not_authorized_or_unidentified", func(c *Candidate) { c.Accessible = false }},
		{"inactive", "no_destructive_action", func(c *Candidate) { c.RuleActive = false }},
		{"keep files", "no_destructive_action", func(c *Candidate) { c.DeletesFiles = false }},
		{"no deadline", "deadline_unknown_or_elapsed", func(c *Candidate) { c.LeavesAt = time.Time{} }},
		{"stale", "source_stale", func(c *Candidate) { c.SourceUpdatedAt = now.Add(-time.Hour) }},
		{"not interested", "no_personal_interest", func(c *Candidate) { c.Requested = false }},
		{"season complete", "no_confirmed_remaining_episode", func(c *Candidate) { c.Kind = "season" }},
		{"future", "outside_reminder_window", func(c *Candidate) { c.LeavesAt = now.Add(8 * 24 * time.Hour) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			c := base
			test.change(&c)
			d := Decide(now, c, prefs)
			if d.Notify || d.Reason != test.reason {
				t.Fatalf("unexpected %+v", d)
			}
		})
	}
	c := base
	c.Requested = false
	c.Watch = InProgress
	if d = Decide(now, c, prefs); !d.Notify || d.Reason != "unfinished_before_removal" {
		t.Fatal(d)
	}
	c.Kind = "season"
	c.RemainingEpisodes = 3
	if d = Decide(now, c, prefs); !d.Notify || d.Reason != "episodes_remaining_before_removal" {
		t.Fatal(d)
	}
	c = base
	c.LeavesAt = c.LeavesAt.Add(time.Hour)
	if Decide(now, c, prefs).Key == Decide(now, base, prefs).Key {
		t.Fatal("postponement did not invalidate reminder")
	}
	if Decide(now, base, prefs).Key != Decide(now.Add(time.Minute), base, prefs).Key {
		t.Fatal("same milestone not deduplicable")
	}
}
func TestQuietHoursAcrossDST(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Paris")
	now := time.Date(2026, 10, 24, 23, 30, 0, 0, loc)
	c := Candidate{UserID: "1", ServerID: "s", MediaID: "m", ActionID: "r", Accessible: true, Following: true, Watch: Unstarted, Kind: "movie", RuleActive: true, DeletesFiles: true, LeavesAt: now.Add(48 * time.Hour), SourceUpdatedAt: now}
	p := Preferences{Enabled: true, QuietStart: 23, QuietEnd: 8}
	d := Decide(now, c, p)
	if !d.Notify || d.NotBefore.In(loc).Hour() != 8 || d.NotBefore.In(loc).Day() != 25 || d.NotBefore.Sub(now) != 9*time.Hour+30*time.Minute {
		t.Fatalf("DST reminder wrong: %+v", d)
	}
	c.LeavesAt = now.Add(time.Hour)
	if d = Decide(now, c, p); d.Notify || d.Reason != "deadline_during_quiet_hours" {
		t.Fatal("quiet hours overridden")
	}
}
