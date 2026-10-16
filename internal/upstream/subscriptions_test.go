package upstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubscriptionSeerrContextAndActiveRequest(t *testing.T) {
	for _, tc := range []struct {
		name, identity, detail string
		wantErr                error
		self                   bool
	}{
		{"others request", `{"id":7}`, `{"id":12,"title":"Shared movie","mediaInfo":{"tmdbId":12,"mediaType":"movie","status":2,"requests":[{"status":1,"requestedBy":{"id":8,"email":"private@example.invalid"}}]}}`, nil, false},
		{"own request", `{"id":7}`, `{"id":12,"title":"Shared movie","mediaInfo":{"tmdbId":12,"mediaType":"movie","status":3,"requests":[{"status":2,"requestedBy":{"id":7}}]}}`, nil, true},
		{"requester omitted", `{"id":7}`, `{"id":12,"title":"Shared movie","mediaInfo":{"tmdbId":12,"mediaType":"movie","status":3,"requests":[{"status":2}]}}`, ErrIncomplete, false},
		{"impersonation ignored", `{"id":1}`, `{}`, ErrDenied, false},
		{"unrequested catalogue", `{"id":7}`, `{"id":12,"title":"Catalogue"}`, ErrNotFound, false},
		{"declined", `{"id":7}`, `{"id":12,"title":"Declined","mediaInfo":{"tmdbId":12,"mediaType":"movie","status":1,"requests":[{"status":3}]}}`, ErrNotFound, false},
		{"mismatched identity", `{"id":7}`, `{"id":13,"title":"Wrong","mediaInfo":{"tmdbId":13,"mediaType":"movie","status":2,"requests":[{"status":1}]}}`, ErrNotFound, false},
		{"import without matching Plex key", `{"id":7}`, `{"id":12,"title":"Imported","mediaInfo":{"tmdbId":12,"mediaType":"movie","status":5,"requests":[{"status":2}]}}`, ErrIncomplete, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.Header.Get("X-Api-Key") != "key" || r.Header.Get("X-Api-User") != "7" {
					t.Error("request did not use authenticated user context")
				}
				switch r.URL.Path {
				case "/api/v1/auth/me":
					fmt.Fprint(w, tc.identity)
				case "/api/v1/movie/12":
					fmt.Fprint(w, tc.detail)
				default:
					t.Errorf("unexpected read: %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			media, err := New().CheckSubscription(context.Background(), server.URL, "key", 7, server.URL, "server-a", Grant{ServerToken: "personal"}, "movie", 12, nil)
			if !errors.Is(err, tc.wantErr) || media.RequestedBySelf != tc.self || media.Available {
				t.Fatalf("media=%+v error=%v", media, err)
			}
		})
	}
}

func TestSubscriptionAvailabilityRequiresPersonalPlexMatch(t *testing.T) {
	for _, tc := range []struct {
		name, kind, guid string
		season           *int
		deny             bool
		offline          bool
		wantErr          error
	}{
		{name: "movie", kind: "movie", guid: "tmdb://12"},
		{name: "wrong movie", kind: "movie", guid: "tmdb://13", wantErr: ErrIncomplete},
		{name: "hidden movie", kind: "movie", guid: "tmdb://12", deny: true, wantErr: ErrDenied},
		{name: "offline movie", kind: "movie", guid: "tmdb://12", offline: true, wantErr: ErrDenied},
		{name: "series first episode", kind: "tv", guid: "tmdb://12"},
		{name: "season first episode", kind: "tv", guid: "tmdb://12", season: new(2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plexReads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Fatal("unexpected mutation")
				}
				switch {
				case r.URL.Path == "/api/v1/auth/me":
					fmt.Fprint(w, `{"id":7}`)
				case strings.HasPrefix(r.URL.Path, "/api/v1/"):
					fmt.Fprintf(w, `{"id":12,"title":"Film","name":"Series","seasons":[{"seasonNumber":2}],"mediaInfo":{"tmdbId":12,"mediaType":%q,"status":5,"ratingKey":"42","requests":[{"status":2,"requestedBy":{"id":8},"seasons":[{"seasonNumber":2}]}]}}`, tc.kind)
				case r.URL.Path == "/identity":
					if r.Header.Get("X-Plex-Token") != "" {
						t.Error("credential sent before server check")
					}
					fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"server-a"}}`)
				default:
					plexReads++
					if r.Header.Get("X-Plex-Token") != "personal" || r.URL.Query().Get("skipRefresh") != "1" {
						t.Error("availability not using personal non-refresh read")
					}
					if tc.deny {
						w.WriteHeader(404)
						return
					}
					kind := tc.kind
					if kind == "tv" {
						kind = "show"
					}
					switch r.URL.Path {
					case "/library/metadata/42":
						fmt.Fprintf(w, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"42","type":%q,"guid":"plex://movie/local","Guid":[{"id":%q}],"Media":[{"Part":[{"key":"/library/parts/1/file.mkv","exists":%v}]}]}]}}`, kind, tc.guid, !tc.offline)
					case "/library/metadata/42/children":
						fmt.Fprint(w, `{"MediaContainer":{"size":1,"totalSize":1,"Metadata":[{"ratingKey":"43","type":"season","parentRatingKey":"42","index":2}]}}`)
					case "/library/metadata/42/grandchildren", "/library/metadata/43/children":
						fmt.Fprint(w, `{"MediaContainer":{"size":1,"totalSize":1,"Metadata":[{"ratingKey":"44","type":"episode","parentRatingKey":"43","grandparentRatingKey":"42","Media":[{"Part":[{"key":"/library/parts/2/file.mkv"}]}]}]}}`)
					default:
						t.Errorf("unexpected Plex route %s", r.URL.Path)
						w.WriteHeader(500)
					}
				}
			}))
			defer server.Close()
			media, err := New().CheckSubscription(context.Background(), server.URL, "key", 7, server.URL, "server-a", Grant{ServerToken: "personal"}, tc.kind, 12, tc.season)
			if !errors.Is(err, tc.wantErr) || media.Available != (tc.wantErr == nil) || plexReads == 0 {
				t.Fatalf("media=%+v err=%v reads=%d", media, err, plexReads)
			}
			if err == nil && !strings.HasPrefix(media.URL, "https://app.plex.tv/") {
				t.Fatalf("unexpected public link %s", media.URL)
			}
		})
	}
}
