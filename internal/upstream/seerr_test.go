package upstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
