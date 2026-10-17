package community

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func publication() Publication {
	return Publication{Version: 1, PublishedAt: now, ExpiresAt: now.Add(time.Hour),
		Forums:  []PublishedThread{{ForumID: "11111", ThreadID: "22222", Title: "A public summary", Kind: "proposal", Status: "open", UpdatedAt: now}},
		Roadmap: []PublishedIssue{{IssueID: "example/project#1", ID: "first-step", Title: "A selected milestone", Status: "planned", Dependencies: []string{}, ThreadID: "22222"}}}
}

func selection() Selection {
	return Selection{GuildID: "33333", ForumIDs: []string{"11111"}, IssueIDs: []string{"example/project#1"}}
}

func body(t *testing.T, p Publication) []byte {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPublicationExplicitSelectionAndNoPrivateReferences(t *testing.T) {
	p := publication()
	p.Forums = append(p.Forums, PublishedThread{ForumID: "44444", ThreadID: "55555", Title: "PRIVATE FORUM"})
	p.Roadmap = append(p.Roadmap, PublishedIssue{IssueID: "private/admin#2", ID: "secret", Title: "PRIVATE ISSUE"})
	p.Roadmap[0].Dependencies = []string{"secret", "first-step"}
	p.Roadmap[0].Date = "2030-01-01"
	s, err := Decode(body(t, p), selection(), now)
	if err != nil || len(s.Threads) != 1 || len(s.Milestones) != 1 {
		t.Fatalf("selection: %+v %v", s, err)
	}
	if s.Threads[0].URL != "https://discord.com/channels/33333/22222" || s.Milestones[0].DiscussionURL != s.Threads[0].URL || len(s.Milestones[0].Dependencies) != 0 || s.Milestones[0].ConfirmedDate != "" || s.Milestones[0].IssueURL != "" {
		t.Fatalf("unexpected publication: %+v", s)
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "example/project") || strings.Contains(string(b), "secret") {
		t.Fatalf("private source leaked: %s", b)
	}
}

func TestPublicationConfirmedDatesSelectedDependenciesAndExplicitLinks(t *testing.T) {
	p := publication()
	p.Roadmap[0].Date, p.Roadmap[0].DateConfirmed, p.Roadmap[0].LinkToIssue = "2030-01-01", true, true
	p.Roadmap[0].Dependencies = []string{"second-step", "second-step", "unknown"}
	p.Roadmap = append(p.Roadmap, PublishedIssue{IssueID: "example/project#2", ID: "second-step", Title: "Prerequisite", Status: "delivered"})
	sel := selection()
	sel.IssueIDs = append(sel.IssueIDs, "example/project#2")
	s, err := Decode(body(t, p), sel, now)
	if err != nil || len(s.Milestones[0].Dependencies) != 1 || s.Milestones[0].ConfirmedDate != "2030-01-01" || s.Milestones[0].IssueURL != "https://github.com/example/project/issues/1" {
		t.Fatalf("confirmed details: %+v %v", s, err)
	}
	sel.ForumIDs = nil
	s, err = Decode(body(t, p), sel, now)
	if err != nil || s.Milestones[0].DiscussionURL != "" {
		t.Fatalf("private discussion link leaked: %+v %v", s, err)
	}
}

func TestPublicationRejectsUnsafeOrAmbiguousInput(t *testing.T) {
	cases := map[string]func(*Publication){
		"expired":          func(p *Publication) { p.ExpiresAt = now },
		"future":           func(p *Publication) { p.PublishedAt = now.Add(2 * time.Minute) },
		"long ttl":         func(p *Publication) { p.ExpiresAt = now.Add(25 * time.Hour) },
		"version":          func(p *Publication) { p.Version = 2 },
		"duplicate thread": func(p *Publication) { p.Forums = append(p.Forums, p.Forums[0]) },
		"duplicate issue":  func(p *Publication) { p.Roadmap = append(p.Roadmap, p.Roadmap[0]) },
		"html":             func(p *Publication) { p.Forums[0].Title = "<script>" },
		"control":          func(p *Publication) { p.Forums[0].Title = "safe\u202eunsafe" },
		"bad status":       func(p *Publication) { p.Roadmap[0].Status = "secret" },
		"bad date":         func(p *Publication) { p.Roadmap[0].DateConfirmed = true; p.Roadmap[0].Date = "2030-02-30" },
		"bad id":           func(p *Publication) { p.Forums[0].ThreadID = "../private" },
		"too many":         func(p *Publication) { p.Forums = make([]PublishedThread, 101) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p := publication()
			change(&p)
			if _, err := Decode(body(t, p), selection(), now); err == nil {
				t.Fatal("unsafe publication accepted")
			}
		})
	}
	b := body(t, publication())
	for _, invalid := range [][]byte{append(b, []byte(`{}`)...), []byte(strings.Replace(string(b), `"version":1`, `"version":1,"adminSecret":"do-not-publish"`, 1)), []byte(strings.Repeat(" ", MaxBytes+1))} {
		if _, err := Decode(invalid, selection(), now); err == nil {
			t.Fatal("invalid JSON publication accepted")
		}
	}
	for _, id := range []string{"example/..#1", "example/project#0", "example/project#1?token=secret"} {
		if ValidIssueID(id) {
			t.Fatalf("unsafe issue ID accepted: %s", id)
		}
	}
}

func TestPublicationChangesArchiveRemovalExpiryAndDeletionNeverUseOldContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "publication.json")
	write := func(p Publication) {
		t.Helper()
		if err := os.WriteFile(path, body(t, p), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p := publication()
	write(p)
	if s, err := Load(path, selection(), now); err != nil || s.Threads[0].Archived {
		t.Fatalf("initial read: %+v %v", s, err)
	}
	p.Forums[0].Archived, p.Forums[0].Title = true, "Updated public title"
	write(p)
	if s, err := Load(path, selection(), now); err != nil || !s.Threads[0].Archived || s.Threads[0].Title != "Updated public title" {
		t.Fatalf("modified read: %+v %v", s, err)
	}
	p.Forums = nil
	write(p)
	if s, err := Load(path, selection(), now); err != nil || len(s.Threads) != 0 || s.Milestones[0].DiscussionURL != "" {
		t.Fatalf("removed forum survived: %+v %v", s, err)
	}
	if _, err := Load(path, selection(), now.Add(time.Hour)); err == nil {
		t.Fatal("expired content served")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if s, err := Load(path, selection(), now); err == nil || len(s.Milestones) != 0 {
		t.Fatal("deleted publication served old content")
	}
}
