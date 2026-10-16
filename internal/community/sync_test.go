package community

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type syncTransport func(*http.Request) (*http.Response, error)

func (f syncTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type syncFixture struct {
	parent map[string]any
	thread map[string]any
	issue  map[string]any
	roles  string
	status map[string]int
	calls  []string
	before func(string)
}

func syncManifest() Manifest {
	return Manifest{Version: 1, GuildID: "33333", Forums: []ForumSelection{{ID: "11111", Kind: "proposal", ThreadIDs: []string{"22222"}, DefaultStatus: "open", StatusTags: map[string]string{"88888": "in_progress", "77777": "resolved"}}}, Issues: []IssueSelection{{IssueID: "private/project#1", ID: "published-step", Title: "Approved summary title", Summary: "Approved public description", Dependencies: []string{}, ThreadID: "22222", OpenStatus: "planned", CompletedStatus: "delivered", LabelStatuses: map[string]string{"working": "in_progress"}}}}
}
func syncSource(t *testing.T) (SyncClient, *syncFixture) {
	t.Helper()
	f := &syncFixture{roles: `[{"id":"33333","permissions":"66560"}]`, status: map[string]int{}, parent: map[string]any{"id": "11111", "guild_id": "33333", "type": 15, "nsfw": false, "permission_overwrites": []any{}, "available_tags": []any{map[string]any{"id": "88888"}, map[string]any{"id": "77777"}}}, thread: map[string]any{"id": "22222", "guild_id": "33333", "parent_id": "11111", "type": 11, "name": "Public forum title", "applied_tags": []string{}, "thread_metadata": map[string]bool{"archived": false}}, issue: map[string]any{"number": 1, "state": "open", "state_reason": nil, "title": "PRIVATE TITLE NEVER EXPORTED", "body": "PRIVATE ADMIN BODY", "user": map[string]string{"login": "private-author"}, "labels": []any{}}}
	c := SyncClient{DiscordToken: "discord-read-secret", GitHubToken: "github-read-secret", HTTP: &http.Client{Transport: syncTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("exporter wrote remotely")
		}
		path := r.URL.Path
		f.calls = append(f.calls, path)
		if f.before != nil {
			f.before(path)
		}
		var body string
		if r.URL.Host == "discord.com" {
			if r.Header.Get("Authorization") != "Bot discord-read-secret" {
				t.Fatal("wrong Discord auth")
			}
		} else if r.URL.Host == "api.github.com" {
			if r.Header.Get("Authorization") != "Bearer github-read-secret" || r.Header.Get("X-GitHub-Api-Version") == "" {
				t.Fatal("wrong GitHub auth")
			}
		} else {
			t.Fatal("untrusted exporter host")
		}
		switch path {
		case "/api/v10/guilds/33333/roles":
			body = f.roles
		case "/api/v10/channels/11111":
			b, _ := json.Marshal(f.parent)
			body = string(b)
		case "/api/v10/channels/22222":
			b, _ := json.Marshal(f.thread)
			body = string(b)
		case "/repos/private/project/issues/1":
			b, _ := json.Marshal(f.issue)
			body = string(b)
		default:
			t.Fatalf("unselected or broad read: %s", path)
		}
		status := f.status[path]
		if status == 0 {
			status = 200
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"120"}, "Link": []string{`<https://evil.invalid/next>; rel="next"`}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}}
	return c, f
}

func TestSyncSelectedThreadsAndApprovedIssueSummariesRefresh(t *testing.T) {
	c, f := syncSource(t)
	m := syncManifest()
	p, err := c.Sync(t.Context(), m, now)
	if err != nil || len(p.Forums) != 1 || len(p.Roadmap) != 1 || p.Forums[0].Status != "open" || p.Roadmap[0].Status != "planned" || p.ExpiresAt.Sub(p.PublishedAt) != 5*time.Minute {
		t.Fatalf("sync %+v %v", p, err)
	}
	body, _ := json.Marshal(p)
	for _, private := range []string{"PRIVATE", "private-author", "read-secret"} {
		if strings.Contains(string(body), private) {
			t.Fatalf("upstream private fields escaped: %s", body)
		}
	}
	if len(f.calls) != 7 {
		t.Fatalf("unexpected listing/pagination/permission checks: %v", f.calls)
	}
	f.thread["name"] = "Edited public title"
	f.thread["applied_tags"] = []string{"88888"}
	f.thread["thread_metadata"] = map[string]bool{"archived": true}
	f.issue["state"] = "closed"
	f.issue["state_reason"] = "completed"
	p, err = c.Sync(t.Context(), m, now.Add(time.Minute))
	if err != nil || p.Forums[0].Title != "Edited public title" || p.Forums[0].Status != "in_progress" || !p.Forums[0].Archived || p.Roadmap[0].Status != "delivered" {
		t.Fatalf("stale refresh %+v %v", p, err)
	}
}

func TestSyncFailClosedDiscordPermissionsAndThreadIdentity(t *testing.T) {
	cases := map[string]func(*syncFixture){
		"everyone no view": func(f *syncFixture) { f.roles = `[{"id":"33333","permissions":"65536"}]` },
		"role deny": func(f *syncFixture) {
			f.parent["permission_overwrites"] = []any{map[string]any{"id": "55555", "type": 0, "allow": "0", "deny": "1024"}}
		},
		"member deny": func(f *syncFixture) {
			f.parent["permission_overwrites"] = []any{map[string]any{"id": "55555", "type": 1, "allow": "0", "deny": "65536"}}
		},
		"nsfw forum":         func(f *syncFixture) { f.parent["nsfw"] = true },
		"unknown nsfw":       func(f *syncFixture) { delete(f.parent, "nsfw") },
		"unknown overwrites": func(f *syncFixture) { delete(f.parent, "permission_overwrites") },
		"not forum":          func(f *syncFixture) { f.parent["type"] = 0 },
		"wrong guild":        func(f *syncFixture) { f.parent["guild_id"] = "99999" },
		"private thread":     func(f *syncFixture) { f.thread["type"] = 12 },
		"wrong parent":       func(f *syncFixture) { f.thread["parent_id"] = "99999" },
		"nsfw thread":        func(f *syncFixture) { f.thread["nsfw"] = true },
		"spoiler forum":      func(f *syncFixture) { f.parent["flags"] = 1 << 21 },
		"obfuscated thread":  func(f *syncFixture) { f.thread["flags"] = 1 << 17 },
		"conflicting tags":   func(f *syncFixture) { f.thread["applied_tags"] = []string{"88888", "77777"} },
		"removed mapped tag": func(f *syncFixture) {
			f.parent["available_tags"] = []any{}
			f.thread["applied_tags"] = []string{"88888"}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			c, f := syncSource(t)
			change(f)
			p, err := c.Sync(t.Context(), syncManifest(), now)
			if err != nil || len(p.Forums) != 0 {
				t.Fatalf("private forum published %+v %v", p, err)
			}
			b, _ := json.Marshal(p)
			result, err := Decode(b, selectionFor(syncManifest()), now)
			if err != nil || result.Milestones[0].DiscussionURL != "" {
				t.Fatal("private discussion link survived")
			}
		})
	}
}

func selectionFor(m Manifest) Selection {
	s := Selection{GuildID: m.GuildID}
	for _, f := range m.Forums {
		s.ForumIDs = append(s.ForumIDs, f.ID)
	}
	for _, i := range m.Issues {
		s.IssueIDs = append(s.IssueIDs, i.IssueID)
	}
	return s
}

func TestSyncRechecksPermissionsAfterRemoteReads(t *testing.T) {
	for _, mode := range []string{"role", "forum", "thread removed", "thread spoiler", "thread tag removed"} {
		t.Run(mode, func(t *testing.T) {
			c, f := syncSource(t)
			f.before = func(path string) {
				if path == "/repos/private/project/issues/1" {
					switch mode {
					case "role":
						f.roles = `[{"id":"33333","permissions":"0"}]`
					case "forum":
						f.parent["nsfw"] = true
					case "thread removed":
						f.status["/api/v10/channels/22222"] = 404
					case "thread spoiler":
						f.thread["flags"] = 1 << 21
					case "thread tag removed":
						f.thread["applied_tags"] = []string{"88888"}
						f.parent["available_tags"] = []any{}
					}
				}
			}
			p, err := c.Sync(t.Context(), syncManifest(), now)
			if err != nil || len(p.Forums) != 0 {
				t.Fatalf("mid-refresh permission change escaped: %+v %v", p, err)
			}
		})
	}
}

func TestSyncUsesFinalThreadTitleTagsAndArchive(t *testing.T) {
	c, f := syncSource(t)
	f.before = func(path string) {
		if path == "/repos/private/project/issues/1" {
			f.thread["name"] = "Changed during refresh"
			f.thread["applied_tags"] = []string{"77777"}
			f.thread["thread_metadata"] = map[string]bool{"archived": true}
		}
	}
	p, err := c.Sync(t.Context(), syncManifest(), now)
	if err != nil || len(p.Forums) != 1 || p.Forums[0].Title != "Changed during refresh" || p.Forums[0].Status != "resolved" || !p.Forums[0].Archived {
		t.Fatalf("stale thread fields %+v %v", p, err)
	}
}

func TestSyncRemovalsAndRateLimitWithdrawPublication(t *testing.T) {
	for _, status := range []int{403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c, f := syncSource(t)
			m := syncManifest()
			path := filepath.Join(t.TempDir(), "published.json")
			if err := c.SyncToFile(t.Context(), m, path, now); err != nil {
				t.Fatal(err)
			}
			f.status["/api/v10/channels/22222"] = status
			// 403 without rate-limit headers means authorization withdrawal.
			if status == 403 {
				inner := c.HTTP.Transport
				c.HTTP.Transport = syncTransport(func(r *http.Request) (*http.Response, error) {
					resp, err := inner.RoundTrip(r)
					resp.Header.Del("Retry-After")
					return resp, err
				})
			}
			err := c.SyncToFile(t.Context(), m, path, now.Add(time.Minute))
			if status == 403 || status == 404 {
				if err != nil {
					t.Fatal(err)
				}
				p, err := Load(path, selectionFor(m), now.Add(time.Minute))
				if err != nil || len(p.Threads) != 0 || p.Milestones[0].DiscussionURL != "" {
					t.Fatalf("removed item survived %+v %v", p, err)
				}
			} else {
				if err == nil {
					t.Fatal("failed source accepted")
				}
				if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatal("old publication was retained")
				}
				if status == 429 {
					var failure *SyncError
					if !errors.As(err, &failure) || failure.RetryAfter != 2*time.Minute {
						t.Fatalf("rate delay lost %v", err)
					}
				}
			}
		})
	}
}

func TestSyncIssueSelectionStatesAndFailures(t *testing.T) {
	for _, mode := range []string{"not planned", "missing", "labels", "ambiguous labels", "pull request"} {
		t.Run(mode, func(t *testing.T) {
			c, f := syncSource(t)
			m := syncManifest()
			want := 0
			wantErr := false
			switch mode {
			case "not planned":
				f.issue["state"] = "closed"
				f.issue["state_reason"] = "not_planned"
			case "missing":
				f.status["/repos/private/project/issues/1"] = 404
			case "labels":
				f.issue["labels"] = []any{map[string]string{"name": "working"}}
				want = 1
			case "ambiguous labels":
				m.Issues[0].LabelStatuses["planned"] = "planned"
				f.issue["labels"] = []any{map[string]string{"name": "working"}, map[string]string{"name": "planned"}}
				wantErr = true
			case "pull request":
				f.issue["pull_request"] = map[string]string{"url": "private"}
				wantErr = true
			}
			p, err := c.Sync(t.Context(), m, now)
			if (err != nil) != wantErr || (!wantErr && len(p.Roadmap) != want) {
				t.Fatalf("issue handling %+v %v", p, err)
			}
			if want == 1 && p.Roadmap[0].Status != "in_progress" {
				t.Fatal("source label not mapped")
			}
		})
	}
}

func TestSyncBoundsAndRedirectsNoUnexpectedRequests(t *testing.T) {
	c, f := syncSource(t)
	m := syncManifest()
	m.Forums[0].ThreadIDs = append(m.Forums[0].ThreadIDs, "22222")
	if _, err := c.Sync(t.Context(), m, now); err == nil || len(f.calls) != 0 {
		t.Fatal("invalid manifest read upstream")
	}
	m = syncManifest()
	m.Issues[0].IssueID = "private/../secret#1"
	if _, err := c.Sync(t.Context(), m, now); err == nil {
		t.Fatal("manifest path injection")
	}
	c.HTTP.Transport = syncTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.invalid"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, err := c.Sync(context.Background(), syncManifest(), now); err == nil {
		t.Fatal("redirect followed")
	}
}

func TestSyncCooldownSurvivesProcessReplacementAndNeverDeletesDirectories(t *testing.T) {
	c, f := syncSource(t)
	m := syncManifest()
	path := filepath.Join(t.TempDir(), "publication.json")
	f.status["/api/v10/channels/22222"] = 429
	if err := c.SyncToFile(t.Context(), m, path, now); err == nil {
		t.Fatal("rate limit accepted")
	}
	previous := len(f.calls)
	deadline, err := os.ReadFile(path + ".retry-after")
	if err != nil {
		t.Fatal(err)
	}
	f.status["/api/v10/channels/22222"] = 200
	resumed := c
	if err := resumed.SyncToFile(t.Context(), m, path, now.Add(time.Minute)); err == nil || len(f.calls) != previous {
		t.Fatal("restart ignored persisted retry deadline")
	}
	currentDeadline, _ := os.ReadFile(path + ".retry-after")
	if string(currentDeadline) != string(deadline) {
		t.Fatal("checking cooldown extended the deadline")
	}
	if err := resumed.SyncToFile(t.Context(), m, path, now.Add(2*time.Minute+2*time.Second)); err != nil {
		t.Fatalf("retry never resumed: %v", err)
	}
	if _, err := Load(path, selectionFor(m), now.Add(2*time.Minute+2*time.Second)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "keep")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := Withdraw(dir); err == nil {
		t.Fatal("directory was removed as publication")
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatal("directory lost")
	}
}

func TestSyncMalformedOrOversizedSourceWithdrawsPublication(t *testing.T) {
	for _, body := range []string{`{"number":`, strings.Repeat(" ", 1024*1024+1)} {
		c, _ := syncSource(t)
		path := filepath.Join(t.TempDir(), "publication.json")
		if err := c.SyncToFile(t.Context(), syncManifest(), path, now); err != nil {
			t.Fatal(err)
		}
		inner := c.HTTP.Transport
		c.HTTP.Transport = syncTransport(func(r *http.Request) (*http.Response, error) {
			resp, err := inner.RoundTrip(r)
			if r.URL.Host == "api.github.com" {
				resp.Body.Close()
				resp.Body = io.NopCloser(strings.NewReader(body))
			}
			return resp, err
		})
		if err := c.SyncToFile(t.Context(), syncManifest(), path, now.Add(time.Minute)); err == nil {
			t.Fatal("invalid source accepted")
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid source retained old publication")
		}
	}
}

func TestReadManifestStrictJSONAndBounds(t *testing.T) {
	for _, body := range []string{`{"version":1,"forums":[],"issues":[],"secret":"hidden"}`, `{"version":1,"forums":[],"issues":[]} {}`, strings.Repeat(" ", MaxBytes+1)} {
		path := filepath.Join(t.TempDir(), "manifest.json")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadManifest(path); err == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
}
