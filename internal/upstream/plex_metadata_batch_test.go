package upstream

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlexMetadataBatchValidatesEveryPersonalItem(t *testing.T) {
	for _, mode := range []string{"valid", "duplicate", "unexpected key", "invalid item", "wrong count", "partial", "denied"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/library/metadata/41,42" || r.Header.Get("X-Plex-Token") != "personal" || r.URL.Query().Get("skipRefresh") != "1" || r.URL.Query().Get("checkFileAvailability") != "1" {
					t.Error("incorrect personal batch read")
				}
				if mode == "denied" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				id, title, size := "42", "Second", 2
				switch mode {
				case "duplicate":
					id = "41"
				case "unexpected key":
					id = "99"
				case "invalid item":
					title = ""
				case "wrong count":
					size = 3
				}
				first := `{"ratingKey":"41","title":"First","guid":"plex://movie/first","type":"movie"}`
				if mode == "partial" {
					fmt.Fprintf(w, `{"MediaContainer":{"size":1,"librarySectionID":"2","Metadata":[%s]}}`, first)
					return
				}
				fmt.Fprintf(w, `{"MediaContainer":{"size":%d,"librarySectionID":"2","Metadata":[%s,{"ratingKey":%q,"title":%q,"guid":"plex://movie/second","type":"movie","Guid":[{"id":"tmdb://22"}]}]}}`, size, first, id, title)
			}))
			defer server.Close()
			items, err := New().PlexMetadataBatch(context.Background(), server.URL, []string{"41", "42"}, Grant{ServerToken: "personal"})
			if mode != "valid" && mode != "partial" {
				if err == nil || len(items) != 0 {
					t.Fatal("unsafe batch accepted")
				}
				return
			}
			if err != nil || items["41"].LibraryID != "2" {
				t.Fatalf("valid metadata rejected: %v", err)
			}
			if mode == "valid" && (len(items) != 2 || items["42"].TMDBID != 22 || items["42"].GUID != "plex://movie/second") {
				t.Fatal("batch metadata lost")
			}
			if mode == "partial" && len(items) != 1 {
				t.Fatal("missing metadata invented")
			}
		})
	}
}
