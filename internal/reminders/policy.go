// Package reminders decides whether a personal pre-removal reminder is useful.
// It performs no network request, notification delivery, or Maintainerr mutation.
package reminders

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"time"
)

type WatchState string

const (
	Unstarted  WatchState = "unstarted"
	InProgress WatchState = "in_progress"
	Completed  WatchState = "completed"
	Unknown    WatchState = "unknown"
)

type Candidate struct {
	UserID, ServerID, MediaID, ActionID string
	Accessible, Requested, Following    bool
	Watch                               WatchState
	RemainingEpisodes                   int
	Kind                                string // movie, episode, season, show
	RuleActive, DeletesFiles            bool
	LeavesAt, SourceUpdatedAt           time.Time
}
type Preferences struct {
	Enabled, AllUnwatched bool
	DaysBefore            []int
	QuietStart, QuietEnd  int
	Timezone              string
	MaxSourceAge          time.Duration
}
type Decision struct {
	Notify        bool
	Reason        string
	MilestoneDays int
	Key           string
	NotBefore     time.Time
}

func Decide(now time.Time, c Candidate, p Preferences) Decision {
	skip := func(reason string) Decision { return Decision{Reason: reason} }
	if !p.Enabled {
		return skip("disabled")
	}
	if !c.Accessible || c.UserID == "" || c.ServerID == "" || c.MediaID == "" || c.ActionID == "" {
		return skip("not_authorized_or_unidentified")
	}
	if !c.RuleActive || !c.DeletesFiles {
		return skip("no_destructive_action")
	}
	if c.LeavesAt.IsZero() || !c.LeavesAt.After(now) {
		return skip("deadline_unknown_or_elapsed")
	}
	age := p.MaxSourceAge
	if age <= 0 {
		age = 15 * time.Minute
	}
	if c.SourceUpdatedAt.IsZero() || now.Sub(c.SourceUpdatedAt) > age || c.SourceUpdatedAt.After(now.Add(time.Minute)) {
		return skip("source_stale")
	}
	if c.Watch == Unknown || c.Watch == "" {
		return skip("watch_unknown")
	}
	if c.Watch == Completed {
		return skip("already_completed")
	}
	if c.Watch != Unstarted && c.Watch != InProgress {
		return skip("watch_unknown")
	}
	if c.Kind != "movie" && c.Kind != "episode" && c.Kind != "season" && c.Kind != "show" {
		return skip("unsupported_kind")
	}
	if (c.Kind == "season" || c.Kind == "show") && c.RemainingEpisodes <= 0 {
		return skip("no_confirmed_remaining_episode")
	}
	if !c.Requested && !c.Following && c.Watch != InProgress && !p.AllUnwatched {
		return skip("no_personal_interest")
	}
	days := slices.Clone(p.DaysBefore)
	if len(days) == 0 {
		days = []int{2, 7}
	}
	slices.Sort(days)
	milestone := -1
	for _, d := range days {
		if d > 0 && d <= 365 && c.LeavesAt.Sub(now) <= time.Duration(d)*24*time.Hour {
			milestone = d
			break
		}
	}
	if milestone < 0 {
		return skip("outside_reminder_window")
	}
	reason := "unstarted_before_removal"
	if c.Watch == InProgress {
		reason = "unfinished_before_removal"
	}
	if c.Kind == "season" || c.Kind == "show" {
		reason = "episodes_remaining_before_removal"
	}
	zone := p.Timezone
	if zone == "" {
		zone = "Europe/Paris"
	}
	loc, e := time.LoadLocation(zone)
	if e != nil {
		return skip("invalid_timezone")
	}
	if p.QuietStart < 0 || p.QuietStart > 23 || p.QuietEnd < 0 || p.QuietEnd > 23 {
		return skip("invalid_quiet_hours")
	}
	local := now.In(loc)
	hour := local.Hour()
	quiet := false
	if p.QuietStart < p.QuietEnd {
		quiet = hour >= p.QuietStart && hour < p.QuietEnd
	} else if p.QuietStart > p.QuietEnd {
		quiet = hour >= p.QuietStart || hour < p.QuietEnd
	}
	notBefore := now
	if quiet {
		day := local.Day()
		if p.QuietStart > p.QuietEnd && hour >= p.QuietStart {
			day++
		}
		notBefore = time.Date(local.Year(), local.Month(), day, p.QuietEnd, 0, 0, 0, loc)
		if !notBefore.Before(c.LeavesAt) {
			return skip("deadline_during_quiet_hours")
		}
	}
	// Include the actual deadline and action so postponement invalidates old reminders.
	key := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%d", c.UserID, c.ServerID, c.MediaID, c.ActionID, c.LeavesAt.UTC().Format(time.RFC3339), milestone)
	hash := sha256.Sum256([]byte(key))
	return Decision{Notify: true, Reason: reason, MilestoneDays: milestone, Key: fmt.Sprintf("%x", hash), NotBefore: notBefore}
}
