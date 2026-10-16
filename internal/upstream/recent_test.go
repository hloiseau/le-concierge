package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func recentMovies(count int) []map[string]any {
	items := make([]map[string]any, 0, count)
	for i := range count {
		items = append(items, map[string]any{"ratingKey": strconv.Itoa(i + 1), "title": "Movie " + strconv.Itoa(i+1), "type": "movie", "addedAt": 1000 - i})
	}
	return items
}

func recentTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/identity" {
			if r.Header.Get("X-Plex-Token") != "" {
				t.Error("token sent before identity verification")
			}
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"server-a"}}`)
			return
		}
		if r.Header.Get("X-Plex-Token") == "" {
			t.Error("media request missing user token")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRecentPaginationUsesRawLookaheadAndTotal(t *testing.T) {
	for _, tc := range []struct {
		name, total                string
		count, limit, offset, next int
	}{
		{name: "empty", count: 0, limit: 12, next: -1},
		{name: "exact full final page", count: 12, limit: 12, next: -1},
		{name: "lookahead", count: 13, limit: 12, next: 12},
		{name: "last partial page", count: 2, limit: 12, offset: 12, total: "14", next: -1},
		{name: "server caps page", count: 5, limit: 12, total: "20", next: 5},
		{name: "bot smaller page", count: 7, limit: 6, offset: 6, next: 12},
		{name: "API cursor boundary", count: 7, limit: 6, offset: 9996, next: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/library/recentlyAdded" || r.URL.Query().Get("X-Plex-Container-Start") != strconv.Itoa(tc.offset) || r.URL.Query().Get("X-Plex-Container-Size") != strconv.Itoa(tc.limit+1) {
					t.Errorf("unexpected pagination request %s", r.URL.RequestURI())
				}
				container := map[string]any{"Metadata": recentMovies(tc.count), "offset": tc.offset, "size": tc.count}
				if tc.total != "" {
					n, _ := strconv.Atoi(tc.total)
					container["totalSize"] = n
				}
				json.NewEncoder(w).Encode(map[string]any{"MediaContainer": container})
			})
			page, err := New().RecentPage(context.Background(), server.URL, "server-a", Grant{ServerToken: "user-a"}, tc.offset, tc.limit)
			if err != nil || len(page.Items) != min(tc.count, tc.limit) || page.Items == nil || page.UpdatedAt.IsZero() {
				t.Fatalf("unexpected page %+v, %v", page, err)
			}
			if (tc.next < 0 && page.NextOffset != nil) || (tc.next >= 0 && (page.NextOffset == nil || *page.NextOffset != tc.next)) {
				t.Fatalf("wrong next offset: %v, expected %d", page.NextOffset, tc.next)
			}
		})
	}
}

func TestRecentGroupsDistinctEpisodesAndPreservesRawCursor(t *testing.T) {
	server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"MediaContainer":{"totalSize":7,"Metadata":[
			{"ratingKey":"12","title":"Pilot","type":"episode","grandparentRatingKey":"10","grandparentTitle":"Series","parentTitle":"Season 1","addedAt":1000},
			{"ratingKey":"12","title":"Pilot","type":"episode","grandparentRatingKey":"10","grandparentTitle":"Series","parentTitle":"Season 1","addedAt":1000},
			{"ratingKey":"13","title":"Next","type":"episode","grandparentRatingKey":"10","grandparentTitle":"Series","parentTitle":"Season 1","addedAt":1002},
			{"ratingKey":"14","title":"Season 1","type":"season","parentRatingKey":"10","parentTitle":"Series","addedAt":999},
			{"ratingKey":"15","title":"Film","type":"movie","addedAt":1001,"Media":[{"Part":[{"file":"/private/films/movie.mkv"}]}]},
			{"ratingKey":"16","title":"Music","type":"album","addedAt":998},
			{"ratingKey":"17","title":"Next film","type":"movie","addedAt":997}
		]}}`)
	})
	page, err := New().RecentPage(context.Background(), server.URL, "server-a", Grant{ServerToken: "private-token"}, 0, 6)
	if err != nil || len(page.Items) != 2 || page.NextOffset == nil || *page.NextOffset != 6 {
		t.Fatalf("bad grouped page %+v %v", page, err)
	}
	show := page.Items[0]
	if show.ID != "plex:server-a:10" || show.Title != "Series" || show.Type != "show" || show.Subtitle != "2 épisodes ajoutés" || show.AddedAt != time.Unix(1002, 0).UTC().Format(time.RFC3339) || !strings.Contains(show.URL, "%2Flibrary%2Fmetadata%2F10") {
		t.Fatalf("wrong group %+v", show)
	}
	encoded, _ := json.Marshal(page.Items)
	for _, secret := range []string{"private-token", "/private/", server.URL} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("private field leaked: %s", encoded)
		}
	}
}

func TestRecentCacheRechecksOriginalItemsAndCopiesResult(t *testing.T) {
	recentCalls, accessCalls := 0, 0
	server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/library/recentlyAdded":
			recentCalls++
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"12","title":"Episode","type":"episode","grandparentRatingKey":"10","grandparentTitle":"Series","addedAt":1000},{"ratingKey":"13","title":"Film","type":"movie","addedAt":999}]}}`)
		case "/library/metadata/12":
			accessCalls++
			if r.URL.Query().Get("skipRefresh") != "1" || r.URL.Query().Get("includeFields") != "ratingKey" {
				t.Error("access check must avoid a metadata refresh")
			}
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"12"}]}}`)
		default:
			t.Errorf("unexpected check %s", r.URL.Path)
		}
	})
	c := New()
	g := Grant{PlexID: "1", ServerToken: "user-a"}
	first, err := c.RecentPage(context.Background(), server.URL, "server-a", g, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	first.Items[0].Title = "mutated"
	*first.NextOffset = 999
	second, err := c.RecentPage(context.Background(), server.URL, "server-a", g, 0, 1)
	if err != nil || recentCalls != 1 || accessCalls != 1 || second.Items[0].Title != "Series" || *second.NextOffset != 1 || !first.UpdatedAt.Equal(second.UpdatedAt) {
		t.Fatalf("bad cached page %+v %v, calls %d/%d", second, err, recentCalls, accessCalls)
	}
}

func TestRecentCacheFailsClosedOnRevocationAndOutage(t *testing.T) {
	for _, status := range []int{401, 403, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/library/metadata/") {
					w.WriteHeader(status)
					return
				}
				fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"1","title":"Movie","type":"movie","addedAt":1000}]}}`)
			})
			c := New()
			g := Grant{ServerToken: "user-a"}
			if _, err := c.RecentPage(context.Background(), server.URL, "server-a", g, 0, 12); err != nil {
				t.Fatal(err)
			}
			page, err := c.RecentPage(context.Background(), server.URL, "server-a", g, 0, 12)
			want := ErrDenied
			if status == 503 {
				want = ErrUnavailable
			}
			if !errors.Is(err, want) || len(page.Items) != 0 || len(c.recentCache) != 0 {
				t.Fatalf("cached content survived upstream failure: %+v %v", page, err)
			}
		})
	}
}

func TestRecentCacheDropsItemAfterLibraryRightsChange(t *testing.T) {
	recentCalls := 0
	server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/library/metadata/") {
			fmt.Fprint(w, `{"MediaContainer":{"size":0}}`)
			return
		}
		recentCalls++
		if recentCalls == 1 {
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"1","title":"Private film","type":"movie","addedAt":1000}]}}`)
		} else {
			fmt.Fprint(w, `{"MediaContainer":{"size":0}}`)
		}
	})
	c := New()
	g := Grant{ServerToken: "user-a"}
	if _, err := c.RecentPage(context.Background(), server.URL, "server-a", g, 0, 12); err != nil {
		t.Fatal(err)
	}
	page, err := c.RecentPage(context.Background(), server.URL, "server-a", g, 0, 12)
	if err != nil || len(page.Items) != 0 || recentCalls != 2 || len(c.recentCache) != 0 {
		t.Fatalf("revoked item still visible: %+v %v", page, err)
	}
}

func TestRecentCacheIsolatesUsersTokensAndPageSizes(t *testing.T) {
	recentCalls := 0
	server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/library/recentlyAdded" {
			t.Errorf("cross-user cache access check: %s", r.URL.Path)
		}
		recentCalls++
		fmt.Fprintf(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"1","title":%q,"type":"movie","addedAt":1000}]}}`, r.Header.Get("X-Plex-Token"))
	})
	c := New()
	for _, tc := range []struct {
		user, token string
		limit       int
	}{{"1", "a", 12}, {"2", "b", 12}, {"2", "rotated-b", 12}, {"3", "rotated-b", 12}, {"3", "rotated-b", 6}} {
		page, err := c.RecentPage(context.Background(), server.URL, "server-a", Grant{PlexID: tc.user, ServerToken: tc.token}, 0, tc.limit)
		if err != nil || page.Items[0].Title != tc.token {
			t.Fatalf("cache isolation failed: %+v %v", page, err)
		}
	}
	if recentCalls != 5 {
		t.Fatalf("only %d isolated fetches", recentCalls)
	}
}

func TestRecentCacheExpiryAndCapacity(t *testing.T) {
	recentCalls := 0
	server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/library/recentlyAdded" {
			t.Errorf("expired cache entry reused: %s", r.URL.Path)
		}
		recentCalls++
		fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"1","title":"Movie","type":"movie","addedAt":1000}]}}`)
	})
	c := New()
	g := Grant{ServerToken: "user-a"}
	first, err := c.RecentPage(context.Background(), server.URL, "server-a", g, 0, 12)
	if err != nil {
		t.Fatal(err)
	}
	for k, entry := range c.recentCache {
		entry.Page.UpdatedAt = time.Now().Add(-recentCacheTTL)
		c.recentCache[k] = entry
	}
	second, err := c.RecentPage(context.Background(), server.URL, "server-a", g, 0, 12)
	if err != nil || recentCalls != 2 || second.UpdatedAt.Before(first.UpdatedAt) {
		t.Fatalf("stale cache used: %+v %v", second, err)
	}
	for i := range recentCacheLimit + 1 {
		g.PlexID = strconv.Itoa(i)
		if _, err := c.RecentPage(context.Background(), server.URL, "server-a", g, 0, 12); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.recentCache) != recentCacheLimit {
		t.Fatalf("cache exceeded capacity: %d", len(c.recentCache))
	}
}

func TestRecentRejectsMalformedPage(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"MediaContainer":{}}`, `{"MediaContainer":{"size":1}}`,
		`{"MediaContainer":{"size":0,"totalSize":5}}`,
		`{"MediaContainer":{"offset":12,"size":0}}`,
		`{"MediaContainer":{"totalSize":-1,"size":0}}`,
		`{"MediaContainer":{"Metadata":[{"ratingKey":"/private/file","title":"Movie","type":"movie","addedAt":1000}]}}`,
		`{"MediaContainer":{"Metadata":[{"ratingKey":"1","title":"Movie","type":"movie"}]}}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			page, err := New().RecentPage(context.Background(), server.URL, "server-a", Grant{ServerToken: "user-a"}, 0, 12)
			if !errors.Is(err, ErrUnavailable) || len(page.Items) != 0 {
				t.Fatalf("malformed upstream page accepted: %+v %v", page, err)
			}
		})
	}
}
