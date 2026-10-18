package community

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func listingResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestAllForumThreadsIncludesActiveAndArchivedWithoutPrivateReads(t *testing.T) {
	c, fixture := syncSource(t)
	original := c.HTTP.Transport
	archivePages, activeReads := 0, 0
	c.HTTP.Transport = syncTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v10/guilds/33333/threads/active":
			activeReads++
			return listingResponse(`{"threads":[{"id":"22222","parent_id":"11111","guild_id":"33333","type":11},{"id":"99999","parent_id":"11111","type":12},{"id":"55555","parent_id":"66666","type":11}],"members":[{"private":"ignored"}]}`), nil
		case "/api/v10/channels/11111/threads/archived/public":
			archivePages++
			if r.URL.Query().Get("limit") != "100" {
				t.Fatal("unbounded archive listing")
			}
			if archivePages == 1 {
				if r.URL.Query().Get("before") != "" {
					t.Fatal("unexpected first cursor")
				}
				return listingResponse(`{"threads":[{"id":"44444","parent_id":"11111","type":11,"thread_metadata":{"archived":true,"archive_timestamp":"2026-10-17T10:00:00Z"}}],"has_more":true}`), nil
			}
			if r.URL.Query().Get("before") != "2026-10-17T10:00:00Z" {
				t.Fatal("wrong archive cursor")
			}
			return listingResponse(`{"threads":[{"id":"22222","parent_id":"11111","type":11,"thread_metadata":{"archived":true,"archive_timestamp":"2026-10-16T10:00:00Z"}}],"has_more":false}`), nil
		case "/api/v10/channels/44444":
			return listingResponse(`{"id":"44444","parent_id":"11111","guild_id":"33333","type":11,"name":"Archived proposal","thread_metadata":{"archived":true}}`), nil
		case "/api/v10/channels/99999", "/api/v10/channels/55555":
			t.Fatal("unselected or private thread fetched")
		}
		return original.RoundTrip(r)
	})
	m := syncManifest()
	m.Issues = nil
	m.Forums[0].ThreadIDs, m.Forums[0].AllThreads = nil, true
	p, err := c.Sync(t.Context(), m, now)
	if err != nil || len(p.Forums) != 2 || activeReads != 1 || archivePages != 2 {
		t.Fatalf("incomplete discovery: %+v %v, active=%d archives=%d", p, err, activeReads, archivePages)
	}
	if p.Forums[1].ThreadID != "44444" || !p.Forums[1].Archived {
		t.Fatal("archived state was lost")
	}
	body, _ := json.Marshal(p)
	if strings.Contains(string(body), "private") || strings.Contains(string(body), "99999") || strings.Contains(string(body), "55555") {
		t.Fatal("unselected content escaped")
	}
	// A later permission change withdraws the entire forum, before any discovery.
	fixture.roles = `[{"id":"33333","permissions":"0"}]`
	p, err = c.Sync(t.Context(), m, now)
	if err != nil || len(p.Forums) != 0 || activeReads != 1 {
		t.Fatal("a restricted forum was still discovered", err)
	}
}

func TestAllForumThreadsRejectsIncompleteAndUnboundedLists(t *testing.T) {
	for _, mode := range []string{"missing_active", "missing_has_more", "empty_more", "cursor_stalls", "private_archive", "too_many", "missing_archive"} {
		t.Run(mode, func(t *testing.T) {
			c, _ := syncSource(t)
			original := c.HTTP.Transport
			c.HTTP.Transport = syncTransport(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/api/v10/guilds/33333/threads/active":
					if mode == "missing_active" {
						return listingResponse(`{}`), nil
					}
					if mode == "too_many" {
						threads := []string{}
						for i := 0; i < 101; i++ {
							threads = append(threads, fmt.Sprintf(`{"id":"%d","parent_id":"11111","type":11}`, 10000+i))
						}
						return listingResponse(`{"threads":[` + strings.Join(threads, ",") + `]}`), nil
					}
					return listingResponse(`{"threads":[]}`), nil
				case "/api/v10/channels/11111/threads/archived/public":
					switch mode {
					case "missing_has_more":
						return listingResponse(`{"threads":[]}`), nil
					case "empty_more":
						return listingResponse(`{"threads":[],"has_more":true}`), nil
					case "cursor_stalls":
						return listingResponse(`{"threads":[{"id":"22222","parent_id":"11111","type":11,"thread_metadata":{"archived":true,"archive_timestamp":"2026-10-17T10:00:00Z"}}],"has_more":true}`), nil
					case "private_archive":
						return listingResponse(`{"threads":[{"id":"22222","parent_id":"11111","type":12}],"has_more":false}`), nil
					case "missing_archive":
						resp := listingResponse(`{}`)
						resp.StatusCode = 403
						return resp, nil
					}
				}
				return original.RoundTrip(r)
			})
			m := syncManifest()
			m.Issues = nil
			m.Forums[0].ThreadIDs, m.Forums[0].AllThreads = nil, true
			if _, err := c.Sync(t.Context(), m, now); err == nil {
				t.Fatal("incomplete list accepted")
			}
		})
	}
}

func TestAllThreadsSelectionCannotAlsoSelectIndividualIDs(t *testing.T) {
	m := syncManifest()
	m.Forums[0].AllThreads = true
	if m.Validate() == nil {
		t.Fatal("ambiguous selection accepted")
	}
}

func TestAllForumThreadsRoadmapLinksRequirePublishedThread(t *testing.T) {
	for _, mode := range []string{"published", "not discovered", "wrong parent", "removed before publication"} {
		t.Run(mode, func(t *testing.T) {
			c, fixture := syncSource(t)
			original := c.HTTP.Transport
			c.HTTP.Transport = syncTransport(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/api/v10/guilds/33333/threads/active":
					if mode == "not discovered" {
						return listingResponse(`{"threads":[]}`), nil
					}
					return listingResponse(`{"threads":[{"id":"22222","parent_id":"11111","type":11}]}`), nil
				case "/api/v10/channels/11111/threads/archived/public":
					return listingResponse(`{"threads":[],"has_more":false}`), nil
				case "/api/v10/channels/22222":
					if mode == "not discovered" {
						t.Fatal("roadmap reference caused an unselected thread read")
					}
				}
				return original.RoundTrip(r)
			})
			if mode == "wrong parent" {
				fixture.thread["parent_id"] = "99999"
			}
			fixture.before = func(path string) {
				if mode == "removed before publication" && path == "/repos/private/project/issues/1" {
					fixture.status["/api/v10/channels/22222"] = 404
				}
			}
			m := syncManifest()
			m.Forums[0].ThreadIDs, m.Forums[0].AllThreads = nil, true
			p, err := c.Sync(t.Context(), m, now)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(p)
			result, err := Decode(body, selectionFor(m), now)
			if err != nil || len(result.Milestones) != 1 {
				t.Fatalf("roadmap was lost: %+v %v", result, err)
			}
			want := ""
			if mode == "published" {
				want = "https://discord.com/channels/33333/22222"
			}
			if result.Milestones[0].DiscussionURL != want {
				t.Fatalf("discussion URL = %q, want %q", result.Milestones[0].DiscussionURL, want)
			}
		})
	}
}

func TestAllForumThreadsRoadmapRejectsMalformedThreadID(t *testing.T) {
	m := syncManifest()
	m.Forums[0].ThreadIDs, m.Forums[0].AllThreads = nil, true
	m.Issues[0].ThreadID = "../private"
	if m.Validate() == nil {
		t.Fatal("malformed roadmap thread ID accepted")
	}
	m = syncManifest()
	m.Issues[0].ThreadID = "99999"
	if m.Validate() == nil {
		t.Fatal("unselected thread accepted without forum-wide discovery")
	}
}
