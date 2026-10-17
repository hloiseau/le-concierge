// Package community reads an explicitly published, sanitized community feed.
// It does not extract messages or infer publication consent from upstream access.
package community

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const MaxBytes = 256 * 1024

var snowflake = regexp.MustCompile(`^[1-9][0-9]{4,19}$`)
var issueKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9][A-Za-z0-9_.-]{0,99}#[1-9][0-9]{0,9}$`)
var publicID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func ValidForumID(id string) bool {
	_, err := strconv.ParseUint(id, 10, 64)
	return snowflake.MatchString(id) && err == nil
}
func ValidIssueID(id string) bool { return issueKey.MatchString(id) }

type Selection struct {
	GuildID  string
	ForumIDs []string
	IssueIDs []string
}

// Publication is a public summary authored or exported by a trusted publisher.
// Keep private bodies, author identities, credentials and admin fields out.
type Publication struct {
	Version     int               `json:"version"`
	PublishedAt time.Time         `json:"publishedAt"`
	ExpiresAt   time.Time         `json:"expiresAt"`
	Forums      []PublishedThread `json:"forums"`
	Roadmap     []PublishedIssue  `json:"roadmap"`
}

type PublishedThread struct {
	ForumID   string    `json:"forumId"`
	ThreadID  string    `json:"threadId"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	Archived  bool      `json:"archived"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type PublishedIssue struct {
	IssueID       string   `json:"issueId"`
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary,omitempty"`
	Status        string   `json:"status"`
	Dependencies  []string `json:"dependencies"`
	Date          string   `json:"date,omitempty"`
	DateConfirmed bool     `json:"dateConfirmed,omitempty"`
	ThreadID      string   `json:"threadId,omitempty"`
	LinkToIssue   bool     `json:"linkToIssue,omitempty"`
}

type Thread struct {
	ID        string    `json:"id"`
	ForumID   string    `json:"forumId"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	Archived  bool      `json:"archived"`
	UpdatedAt time.Time `json:"updatedAt"`
	URL       string    `json:"url"`
}

type Milestone struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary,omitempty"`
	Status        string   `json:"status"`
	Dependencies  []string `json:"dependencies"`
	ConfirmedDate string   `json:"confirmedDate,omitempty"`
	DiscussionURL string   `json:"discussionUrl,omitempty"`
	IssueURL      string   `json:"issueUrl,omitempty"`
}

type Snapshot struct {
	Threads     []Thread
	Milestones  []Milestone
	PublishedAt time.Time
	ExpiresAt   time.Time
}

func Load(path string, selection Selection, now time.Time) (Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return Snapshot{}, errors.New("community publication unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return Snapshot{}, errors.New("invalid community publication file")
	}
	body, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil || len(body) > MaxBytes {
		return Snapshot{}, errors.New("community publication too large or unreadable")
	}
	return Decode(body, selection, now)
}

func Decode(body []byte, selection Selection, now time.Time) (Snapshot, error) {
	invalid := errors.New("invalid or expired community publication")
	if len(body) > MaxBytes || len(selection.ForumIDs) > 100 || len(selection.IssueIDs) > 100 {
		return Snapshot{}, invalid
	}
	forums, issues := map[string]bool{}, map[string]bool{}
	for _, id := range selection.ForumIDs {
		if !ValidForumID(id) || forums[id] || !ValidForumID(selection.GuildID) {
			return Snapshot{}, invalid
		}
		forums[id] = true
	}
	for _, id := range selection.IssueIDs {
		if !ValidIssueID(id) || issues[strings.ToLower(id)] {
			return Snapshot{}, invalid
		}
		issues[strings.ToLower(id)] = true
	}
	var p Publication
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Version != 1 || p.PublishedAt.IsZero() || p.ExpiresAt.IsZero() || p.PublishedAt.After(now.Add(time.Minute)) || !p.ExpiresAt.After(now) || !p.ExpiresAt.After(p.PublishedAt) || p.ExpiresAt.Sub(p.PublishedAt) > 24*time.Hour || len(p.Forums) > 100 || len(p.Roadmap) > 100 {
		return Snapshot{}, invalid
	}
	s := Snapshot{Threads: []Thread{}, Milestones: []Milestone{}, PublishedAt: p.PublishedAt.UTC(), ExpiresAt: p.ExpiresAt.UTC()}
	threadURLs, milestoneIDs, seenIssues := map[string]string{}, map[string]bool{}, map[string]bool{}
	for _, t := range p.Forums {
		if !forums[t.ForumID] {
			continue
		}
		if !ValidForumID(t.ThreadID) || threadURLs[t.ThreadID] != "" || !plain(t.Title, 200, false) || (t.Kind != "proposal" && t.Kind != "problem") || !oneOf(t.Status, "open", "planned", "in_progress", "resolved", "declined") || t.UpdatedAt.IsZero() || t.UpdatedAt.After(p.PublishedAt.Add(time.Minute)) {
			return Snapshot{}, invalid
		}
		url := "https://discord.com/channels/" + selection.GuildID + "/" + t.ThreadID
		threadURLs[t.ThreadID] = url
		s.Threads = append(s.Threads, Thread{t.ThreadID, t.ForumID, t.Title, t.Kind, t.Status, t.Archived, t.UpdatedAt.UTC(), url})
	}
	for _, item := range p.Roadmap {
		if !issues[strings.ToLower(item.IssueID)] {
			continue
		}
		key := strings.ToLower(item.IssueID)
		if !publicID.MatchString(item.ID) || milestoneIDs[item.ID] || seenIssues[key] || !plain(item.Title, 200, false) || !plain(item.Summary, 1000, true) || !oneOf(item.Status, "planned", "in_progress", "delivered") || len(item.Dependencies) > 100 {
			return Snapshot{}, invalid
		}
		milestoneIDs[item.ID], seenIssues[key] = true, true
		m := Milestone{ID: item.ID, Title: item.Title, Summary: item.Summary, Status: item.Status, Dependencies: []string{}, DiscussionURL: threadURLs[item.ThreadID]}
		if item.DateConfirmed {
			date, err := time.Parse("2006-01-02", item.Date)
			if err != nil || date.Format("2006-01-02") != item.Date {
				return Snapshot{}, invalid
			}
			m.ConfirmedDate = item.Date
		}
		if item.LinkToIssue {
			m.IssueURL = "https://github.com/" + strings.Replace(item.IssueID, "#", "/issues/", 1)
		}
		s.Milestones = append(s.Milestones, m)
	}
	for i := range s.Milestones {
		for _, source := range p.Roadmap {
			if source.ID != s.Milestones[i].ID || !issues[strings.ToLower(source.IssueID)] {
				continue
			}
			seen := map[string]bool{}
			for _, id := range source.Dependencies {
				if milestoneIDs[id] && id != source.ID && !seen[id] {
					s.Milestones[i].Dependencies = append(s.Milestones[i].Dependencies, id)
					seen[id] = true
				}
			}
		}
	}
	return s, nil
}

func oneOf(value string, allowed ...string) bool {
	for _, v := range allowed {
		if value == v {
			return true
		}
	}
	return false
}

func plain(value string, limit int, optional bool) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit || strings.TrimSpace(value) != value || (!optional && value == "") || strings.ContainsAny(value, "<>") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}
