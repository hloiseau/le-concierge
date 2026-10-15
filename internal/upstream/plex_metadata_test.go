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
		if r.Header.Get("X-Plex-Token") != "user-token" || r.URL.Query().Get("skipRefresh") != "1" {
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
