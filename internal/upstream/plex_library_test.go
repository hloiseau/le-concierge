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

func TestDownloadLibrarySearchPersonalBoundedPage(t *testing.T) {
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/identity" {
			if r.Header.Get("X-Plex-Token") != "" {
				t.Error("identity leaked token")
			}
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"server-a"}}`)
			return
		}
		reads++
		q := r.URL.Query()
		if r.Method != "GET" || r.URL.Path != "/library/all" || q.Get("title") != "The film & friends" || q.Get("type") != "1,2" || q.Get("X-Plex-Container-Start") != "20" || q.Get("X-Plex-Container-Size") != "21" || r.Header.Get("X-Plex-Token") != "personal" || r.Header.Get("Cookie") != "" || strings.Contains(r.URL.RawQuery, "personal") {
			t.Errorf("wrong library read %s", r.URL.Path)
		}
		items := []any{}
		for i := 1; i <= 21; i++ {
			items = append(items, map[string]any{"ratingKey": strconv.Itoa(i), "title": "Film\n name", "type": "movie", "year": 2026, "file": "/private/film.mkv", "token": "private"})
		}
		json.NewEncoder(w).Encode(map[string]any{"MediaContainer": map[string]any{"offset": 20, "size": 21, "totalSize": 50, "Metadata": items}})
	}))
	defer server.Close()
	page, err := New().DownloadLibrary(t.Context(), server.URL, "server-a", Grant{ServerToken: "personal", ExpiresAt: time.Now().Add(time.Hour)}, "The film & friends", "", 20)
	if err != nil || len(page.Items) != 20 || page.NextOffset == nil || *page.NextOffset != 40 || reads != 1 {
		t.Fatalf("page %#v error %v reads %d", page, err, reads)
	}
	if page.Items[0].Title != "Film name" || page.ExpiresAt.Sub(page.CheckedAt) != 30*time.Second {
		t.Fatal("unsanitized or stale page")
	}
	encoded, _ := json.Marshal(page)
	if strings.Contains(string(encoded), "private") {
		t.Fatal("private metadata escaped")
	}
}

func TestDownloadLibraryChildrenAndAccess(t *testing.T) {
	for _, kind := range []string{"show", "season"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/identity":
					fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"server-a"}}`)
				case "/library/metadata/20":
					fmt.Fprintf(w, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"20","librarySectionID":"1","parentRatingKey":"10","type":%q,"title":"Parent","guid":"plex://show/a"}]}}`, kind)
				case "/library/metadata/20/children":
					if r.Header.Get("X-Plex-Token") != "personal" || r.URL.Query().Get("skipRefresh") != "1" {
						t.Error("missing read-only personal access")
					}
					child := "season"
					if kind == "season" {
						child = "episode"
					}
					fmt.Fprintf(w, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"21","parentRatingKey":"20","type":%q,"title":"Child","index":0}]}}`, child)
				default:
					t.Errorf("unexpected read %s", r.URL.Path)
				}
			}))
			defer server.Close()
			page, err := New().DownloadLibrary(t.Context(), server.URL, "server-a", Grant{ServerToken: "personal"}, "", "20", 0)
			if err != nil || len(page.Items) != 1 || page.Items[0].Index == nil || *page.Items[0].Index != 0 || page.Items[0].ParentTitle != "Parent" || page.NextOffset != nil {
				t.Fatalf("page %#v error %v", page, err)
			}
		})
	}
}

func TestDownloadLibraryRejectsInvalidSourceAndSelectors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		denied     bool
	}{
		{"wrong identity", `{"MediaContainer":{"machineIdentifier":"other"}}`, 200, true},
		{"revoked", "", 403, true},
		{"wrong offset", `{"MediaContainer":{"size":0,"offset":9}}`, 200, false},
		{"empty advancing page", `{"MediaContainer":{"size":0,"totalSize":1}}`, 200, false},
		{"unexpected kind", `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"1","title":"Music","type":"track"}]}}`, 200, false},
		{"unsafe key", `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"../secret","title":"Film","type":"movie"}]}}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/identity" && tc.name != "wrong identity" {
					fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"server-a"}}`)
					return
				}
				if tc.name == "wrong identity" && r.Header.Get("X-Plex-Token") != "" {
					t.Error("credential sent to wrong server")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			_, err := New().DownloadLibrary(t.Context(), server.URL, "server-a", Grant{ServerToken: "personal"}, "film", "", 0)
			if err == nil || (tc.denied && !errors.Is(err, ErrDenied)) {
				t.Fatalf("error %v", err)
			}
		})
	}
	for _, tc := range []struct {
		q, parent string
		offset    int
	}{{"", "", 0}, {"a", "", 0}, {"film", "1", 0}, {"", "../1", 0}, {"", "01", 0}, {"film", "", -1}, {strings.Repeat("x", 201), "", 0}, {"fi\nlm", "", 0}} {
		if ValidDownloadLibraryQuery(tc.q, tc.parent, tc.offset) {
			t.Fatalf("accepted invalid selectors %#v", tc)
		}
	}
}

func TestDownloadLibraryNeverFollowsRedirectOrReturnsExpiredGrant(t *testing.T) {
	called := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/identity" {
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"server-a"}}`)
			return
		}
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client := &Client{HTTP: &http.Client{}}
	if _, err := client.DownloadLibrary(context.Background(), source.URL, "server-a", Grant{ServerToken: "personal"}, "film", "", 0); err == nil || called {
		t.Fatal("redirect followed")
	}
	if _, err := client.DownloadLibrary(t.Context(), source.URL, "server-a", Grant{ServerToken: "personal", ExpiresAt: time.Now().Add(-time.Second)}, "film", "", 0); !errors.Is(err, ErrDenied) {
		t.Fatalf("expired grant %v", err)
	}
}
