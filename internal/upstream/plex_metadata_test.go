package upstream

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlexMetadataDistinguishesCanonicalGUIDFromExternalGuids(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "user-token" || r.URL.Query().Get("skipRefresh") != "1" || r.URL.Query().Get("checkFiles") != "1" || r.URL.Query().Get("checkFileAvailability") != "1" {
			t.Error("missing private token or refresh suppression")
		}
		fmt.Fprint(w, `{"MediaContainer":{"size":1,"librarySectionID":2,"Metadata":[{"ratingKey":"42","title":"Fixture","type":"movie","guid":"plex://movie/fixture","Guid":[{"id":"tmdb://123"}],"viewCount":1}]}}`)
	}))
	defer server.Close()
	item, err := New().PlexMetadata(context.Background(), server.URL, "42", Grant{ServerToken: "user-token"})
	if err != nil {
		t.Fatal(err)
	}
	if item.GUID != "plex://movie/fixture" || item.LibraryID != "2" || item.ViewCount != 1 {
		t.Fatalf("incorrect normalized metadata: %+v", item)
	}
}

func TestPlexMetadataRequiresPositiveCurrentFileAndAccessFlags(t *testing.T) {
	for _, tc := range []struct {
		name, flags string
		playable    bool
	}{
		{"confirmed", `,"exists":true,"accessible":1`, true},
		{"file absent", `,"exists":false,"accessible":true`, false},
		{"file denied", `,"exists":true,"accessible":false`, false},
		{"flags absent", ``, false},
		{"access unknown", `,"exists":true`, false},
		{"existence unknown", `,"accessible":true`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Plex-Token") != "user-token" || r.URL.Query().Get("skipRefresh") != "1" || r.URL.Query().Get("checkFiles") != "1" || r.URL.Query().Get("checkFileAvailability") != "1" {
					t.Error("personal synchronous file check missing")
				}
				fmt.Fprintf(w, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"42","librarySectionID":"2","title":"Fixture","type":"movie","guid":"plex://movie/fixture","Media":[{"Part":[{"key":"/library/parts/1/file"%s}]}]}]}}`, tc.flags)
			}))
			defer server.Close()
			item, err := New().PlexMetadata(context.Background(), server.URL, "42", Grant{ServerToken: "user-token"})
			if err != nil || item.Playable != tc.playable {
				t.Fatalf("playability %+v %v", item, err)
			}
		})
	}
}
