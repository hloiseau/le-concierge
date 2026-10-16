package upstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubscriptionOwnRequestDedupRespectsSeasonAndStatus(t *testing.T) {
	for _, tc := range []struct {
		name, requests string
		self           bool
		wantErr        error
	}{
		{"other season does not suppress", `[{"status":2,"requestedBy":{"id":7},"seasons":[{"seasonNumber":1}]},{"status":2,"requestedBy":{"id":8},"seasons":[{"seasonNumber":2}]}]`, false, nil},
		{"same season suppresses", `[{"status":2,"requestedBy":{"id":7},"seasons":[{"seasonNumber":1},{"seasonNumber":2}]},{"status":2,"requestedBy":{"id":8},"seasons":[{"seasonNumber":2}]}]`, true, nil},
		{"declined self does not suppress", `[{"status":3,"requestedBy":{"id":7},"seasons":[{"seasonNumber":2}]},{"status":1,"requestedBy":{"id":8},"seasons":[{"seasonNumber":2}]}]`, false, nil},
		{"unrequested season", `[{"status":2,"requestedBy":{"id":8},"seasons":[{"seasonNumber":1}]}]`, false, ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Api-User") != "7" {
					t.Error("missing personal context")
				}
				if r.URL.Path == "/api/v1/auth/me" {
					fmt.Fprint(w, `{"id":7}`)
					return
				}
				fmt.Fprintf(w, `{"id":12,"name":"Series","seasons":[{"seasonNumber":1},{"seasonNumber":2}],"mediaInfo":{"tmdbId":12,"mediaType":"tv","status":3,"requests":%s}}`, tc.requests)
			}))
			defer server.Close()
			season := 2
			media, err := New().CheckSubscription(context.Background(), server.URL, "key", 7, server.URL, "server-a", Grant{ServerToken: "personal"}, "tv", 12, &season)
			if !errors.Is(err, tc.wantErr) || media.RequestedBySelf != tc.self {
				t.Fatalf("media=%+v err=%v", media, err)
			}
		})
	}
}

func TestSubscriptionFindsPlayableEpisodeAfterOfflineEpisode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			fmt.Fprint(w, `{"id":7}`)
		case "/api/v1/tv/12":
			fmt.Fprint(w, `{"id":12,"name":"Series","mediaInfo":{"tmdbId":12,"mediaType":"tv","status":4,"ratingKey":"42","requests":[{"status":2,"requestedBy":{"id":8}}]}}`)
		case "/identity":
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"server-a"}}`)
		case "/library/metadata/42":
			fmt.Fprint(w, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"42","type":"show","guid":"plex://show/x","Guid":[{"id":"tmdb://12"}]}]}}`)
		case "/library/metadata/42/grandchildren":
			if r.Header.Get("X-Plex-Token") != "personal" {
				t.Error("episodes not personally authorized")
			}
			if r.Header.Get("X-Plex-Container-Start") == "0" {
				fmt.Fprint(w, `{"MediaContainer":{"size":1,"totalSize":2,"offset":0,"Metadata":[{"ratingKey":"43","type":"episode","grandparentRatingKey":"42","Media":[{"Part":[{"key":"/library/parts/1/file.mkv","exists":0}]}]}]}}`)
			} else {
				fmt.Fprint(w, `{"MediaContainer":{"size":1,"totalSize":2,"offset":1,"Metadata":[{"ratingKey":"44","type":"episode","grandparentRatingKey":"42","Media":[{"Part":[{"key":"/library/parts/2/file.mkv","exists":true,"accessible":1}]}]}]}}`)
			}
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	media, err := New().CheckSubscription(context.Background(), server.URL, "key", 7, server.URL, "server-a", Grant{ServerToken: "personal"}, "tv", 12, nil)
	if err != nil || !media.Available {
		t.Fatalf("playable second episode missed: %+v %v", media, err)
	}
}
