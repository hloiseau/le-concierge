package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestArtworkAndRequesterUseValidatedUpstreamFields(t *testing.T) {
	for _, tc := range []struct{ name, path, want string }{
		{"TMDB image", "/abc_def-12.jpg", "https://image.tmdb.org/t/p/w342/abc_def-12.jpg"},
		{"missing", "", ""},
		{"absolute URL", "https://foreign.example/poster.jpg", ""},
		{"query credential", "/poster.jpg?token=private", ""},
		{"parent path", "/../poster.jpg", ""},
		{"SVG", "/poster.svg", ""},
		{"oversized path", "/" + strings.Repeat("a", 201) + ".jpg", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Api-Key") != "fixture-key" || r.URL.Query().Has("apikey") {
					t.Error("unsafe request credentials")
				}
				switch r.URL.Path {
				case "/api/v1/user/42/requests":
					fmt.Fprint(w, `{"pageInfo":{"results":1},"results":[{"id":1,"type":"movie","status":2,"requestedBy":{"id":42,"displayName":" Alice ","username":"secondary","plexUsername":"tertiary"},"media":{"tmdbId":100,"status":5}}]}`)
				case "/api/v1/movie/100":
					_ = json.NewEncoder(w).Encode(map[string]string{"title": "Fixture film", "posterPath": tc.path})
				default:
					t.Error("unexpected request path")
					w.WriteHeader(404)
				}
			}))
			defer source.Close()
			page, err := New().Requests(t.Context(), source.URL, "fixture-key", 42, 0)
			if err != nil || len(page.Items) != 1 || page.Items[0].PosterURL != tc.want || page.Items[0].RequesterName != "Alice" {
				t.Fatalf("request artwork/name: %+v %v", page, err)
			}
		})
	}
}

func TestSeerrPlexIdentityLookup(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		id         int64
		err        error
	}{
		{"exact identity", `{"pageInfo":{"results":2},"results":[{"id":7,"plexId":123,"username":"someone else"},{"id":8,"plexId":456,"username":"same name"}]}`, 7, nil},
		{"local account", `{"pageInfo":{"results":1},"results":[{"id":8,"plexId":null,"username":"123"}]}`, 0, ErrSeerrUserNotFound},
		{"missing permissions", `{"pageInfo":{"results":1},"results":[{"id":7,"username":"123"}]}`, 0, ErrUnavailable},
		{"duplicate Plex identity", `{"pageInfo":{"results":2},"results":[{"id":7,"plexId":123},{"id":8,"plexId":123}]}`, 0, ErrDenied},
		{"no matching identity", `{"pageInfo":{"results":1},"results":[{"id":7,"plexId":456}]}`, 0, ErrSeerrUserNotFound},
		{"empty directory", `{"pageInfo":{"results":0},"results":[]}`, 0, ErrSeerrUserNotFound},
		{"truncated directory", `{"pageInfo":{"results":2},"results":[]}`, 0, ErrUnavailable},
		{"missing pagination", `{"results":[]}`, 0, ErrUnavailable},
		{"excessive directory", `{"pageInfo":{"results":5001},"results":[]}`, 0, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/user" || r.Method != "GET" || r.Header.Get("X-Api-Key") != "key" {
					t.Error("unexpected lookup")
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			id, err := New().SeerrUser(context.Background(), server.URL, "key", "123")
			if id != tc.id || !errors.Is(err, tc.err) {
				t.Fatalf("got %d %v; want %d %v", id, err, tc.id, tc.err)
			}
		})
	}
}
func TestSeerrLookupScansAllPages(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprint(duplicate), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Query().Get("skip") == "0" {
					fmt.Fprint(w, `{"pageInfo":{"results":2},"results":[{"id":7,"plexId":123}]}`)
					return
				}
				if r.URL.Query().Get("skip") != "1" {
					t.Error("pagination not advanced")
				}
				plex := 456
				if duplicate {
					plex = 123
				}
				fmt.Fprintf(w, `{"pageInfo":{"results":2},"results":[{"id":8,"plexId":%d}]}`, plex)
			}))
			defer server.Close()
			id, err := New().SeerrUser(context.Background(), server.URL, "key", "123")
			if calls != 2 || (!duplicate && (id != 7 || err != nil)) || (duplicate && !errors.Is(err, ErrDenied)) {
				t.Fatalf("partial directory accepted: %d %v calls=%d", id, err, calls)
			}
		})
	}
}

func TestRequestDatesAreUTCOrUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
	}{
		{"UTC", `"2026-10-16T12:00:00Z"`, "2026-10-16T12:00:00Z"},
		{"offset with fractions", `"2026-10-16T01:30:00.123+02:00"`, "2026-10-15T23:30:00.123Z"},
		{"missing", "", ""},
		{"empty", `""`, ""},
		{"null", `null`, ""},
		{"no timezone", `"2026-10-16T12:00:00"`, ""},
		{"invalid date", `"2026-02-30T12:00:00Z"`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/user/42/requests" {
					field := ""
					if tc.source != "" {
						field = `,"createdAt":` + tc.source
					}
					fmt.Fprintf(w, `{"pageInfo":{"results":1},"results":[{"id":1,"type":"movie","status":999,"requestedBy":{"id":42},"media":{"tmdbId":100,"status":999}%s}]}`, field)
					return
				}
				if r.URL.Path != "/api/v1/movie/100" {
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()
			page, err := New().Requests(t.Context(), server.URL, "fixture-key", 42, 0)
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("request lost on incomplete upstream data: %+v %v", page, err)
			}
			item := page.Items[0]
			if item.CreatedAt != tc.want || item.Status != "unknown" || item.Title != "Film #100" || item.Seasons == nil {
				t.Fatalf("incorrect fallback or UTC date: %+v", item)
			}
		})
	}
}

func TestRequestStatusKeepsFailureAndDeclineDistinctFromMedia(t *testing.T) {
	for _, tc := range []struct {
		request, media int
		want           string
	}{
		{1, 2, "pending"}, {2, 2, "approved"}, {2, 3, "processing"},
		{2, 4, "partial"}, {5, 5, "available"}, {3, 5, "declined"},
		{4, 3, "error"}, {4, 5, "error"}, {5, 1, "unknown"}, {999, 999, "unknown"},
	} {
		t.Run(fmt.Sprintf("%d/%d", tc.request, tc.media), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/user/42/requests" {
					fmt.Fprintf(w, `{"pageInfo":{"results":1},"results":[{"id":1,"type":"movie","status":%d,"requestedBy":{"id":42},"media":{"tmdbId":100,"status":%d}}]}`, tc.request, tc.media)
					return
				}
				fmt.Fprint(w, `{"title":"A film"}`)
			}))
			defer server.Close()
			page, err := New().Requests(t.Context(), server.URL, "fixture-key", 42, 0)
			if err != nil || len(page.Items) != 1 || page.Items[0].Status != tc.want {
				t.Fatalf("status: %+v %v", page, err)
			}
		})
	}
}

func TestRequest4KStatusDoesNotUseHDAvailability(t *testing.T) {
	for _, tc := range []struct {
		status4k int
		want     string
	}{{0, "approved"}, {2, "approved"}, {3, "processing"}, {4, "partial"}, {5, "available"}} {
		t.Run(fmt.Sprint(tc.status4k), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/user/42/requests" {
					fmt.Fprintf(w, `{"pageInfo":{"results":1},"results":[{"id":1,"type":"movie","status":2,"is4k":true,"requestedBy":{"id":42},"media":{"tmdbId":100,"status":5,"status4k":%d}}]}`, tc.status4k)
					return
				}
				fmt.Fprint(w, `{"title":"A film"}`)
			}))
			defer server.Close()
			page, err := New().Requests(t.Context(), server.URL, "fixture-key", 42, 0)
			if err != nil || len(page.Items) != 1 || page.Items[0].Status != tc.want {
				t.Fatalf("HD availability used for 4K: %+v %v", page, err)
			}
		})
	}
}

func TestRequestPageReadBoundRejectsOversizedUpstreamBeforeEnrichment(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"pageInfo":{"results":11},"results":[`)
		for i := 1; i <= 11; i++ {
			if i > 1 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"id":%d,"type":"movie","status":2,"requestedBy":{"id":42},"media":{"tmdbId":100,"status":5}}`, i)
		}
		fmt.Fprint(w, "]}")
	}))
	defer server.Close()
	page, err := New().Requests(t.Context(), server.URL, "fixture-key", 42, 0)
	if !errors.Is(err, ErrUnavailable) || len(page.Items) != 0 || calls != 1 {
		t.Fatalf("unbounded page accepted: %+v %v calls=%d", page, err, calls)
	}
}
