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

func TestSubscriptionMultipartAvailabilityNeedsEveryPartInOneEdition(t *testing.T) {
	for _, tc := range []struct {
		name, media string
		playable    bool
	}{
		{"complete multipart", `[{"Part":[{"key":"/library/parts/1/file.mkv","exists":true,"accessible":1},{"key":"/library/parts/2/file.mkv","exists":1,"accessible":true}]}]`, true},
		{"missing second part", `[{"Part":[{"key":"/library/parts/1/file.mkv"},{"key":"/library/parts/2/file.mkv","exists":false}]}]`, false},
		{"inaccessible second part", `[{"Part":[{"key":"/library/parts/1/file.mkv"},{"key":"/library/parts/2/file.mkv","accessible":0}]}]`, false},
		{"complete alternate edition", `[{"Part":[{"key":"/library/parts/1/file.mkv","exists":false}]},{"Part":[{"key":"/library/parts/3/file.mkv","exists":true,"accessible":true}]}]`, true},
		{"unknown file", `[{"Part":[{"key":"/library/parts/1/file.mkv"}]}]`, false},
		{"unknown access", `[{"Part":[{"key":"/library/parts/1/file.mkv","exists":true}]}]`, false},
		{"unknown existence", `[{"Part":[{"key":"/library/parts/1/file.mkv","accessible":true}]}]`, false},
		{"empty parts", `[{"Part":[]}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var item subscriptionPlexMetadata
			if err := json.Unmarshal([]byte(`{"Media":`+tc.media+`}`), &item); err != nil {
				t.Fatal(err)
			}
			if got := playableSubscriptionItem(item); got != tc.playable {
				t.Fatalf("playable=%v want=%v", got, tc.playable)
			}
		})
	}
}

func TestSubscriptionAvailabilityAcrossPersonallyAuthorizedEditions(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		hdStatus, uhdStatus     int
		hdMismatch, wrongServer bool
		wantKey                 string
		wantErr                 error
		reads                   int
	}{
		{name: "hidden HD and accessible 4K", hdStatus: 404, uhdStatus: 200, wantKey: "142", reads: 2},
		{name: "denied HD and accessible 4K", hdStatus: 403, uhdStatus: 200, wantKey: "142", reads: 2},
		{name: "accessible HD and hidden 4K", hdStatus: 200, uhdStatus: 404, wantKey: "42", reads: 1},
		{name: "both editions hidden", hdStatus: 404, uhdStatus: 403, wantErr: ErrDenied, reads: 2},
		{name: "wrong HD identity cannot be ignored", hdStatus: 200, uhdStatus: 200, hdMismatch: true, wantErr: ErrIncomplete, reads: 1},
		{name: "wrong server is fatal before all credentials", hdStatus: 200, uhdStatus: 200, wrongServer: true, wantErr: ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/auth/me":
					fmt.Fprint(w, `{"id":7}`)
				case "/api/v1/movie/12":
					fmt.Fprint(w, `{"id":12,"title":"Shared movie","mediaInfo":{"tmdbId":12,"mediaType":"movie","status":5,"status4k":5,"ratingKey":"42","ratingKey4k":"142","requests":[{"status":2,"requestedBy":{"id":8}}]}}`)
				case "/identity":
					if r.Header.Get("X-Plex-Token") != "" {
						t.Error("credential sent before server identity check")
					}
					id := "server-a"
					if tc.wrongServer {
						id = "another-server"
					}
					fmt.Fprintf(w, `{"MediaContainer":{"machineIdentifier":%q}}`, id)
				default:
					reads++
					if r.Header.Get("X-Plex-Token") != "personal" {
						t.Error("edition check lacks personal token")
					}
					id := strings.TrimPrefix(r.URL.Path, "/library/metadata/")
					status, tmdb := tc.hdStatus, 12
					if id == "142" {
						status = tc.uhdStatus
					} else if tc.hdMismatch {
						tmdb = 13
					}
					w.WriteHeader(status)
					if status == 200 {
						fmt.Fprintf(w, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":%q,"type":"movie","guid":"plex://movie/x","Guid":[{"id":"tmdb://%d"}],"Media":[{"Part":[{"key":"/library/parts/1/file.mkv","exists":true,"accessible":true}]}]}]}}`, id, tmdb)
					}
				}
			}))
			defer server.Close()
			media, err := New().CheckSubscription(context.Background(), server.URL, "key", 7, server.URL, "server-a", Grant{ServerToken: "personal"}, "movie", 12, nil)
			if !errors.Is(err, tc.wantErr) || reads != tc.reads {
				t.Fatalf("media=%+v err=%v reads=%d", media, err, reads)
			}
			if tc.wantErr == nil && (!media.Available || !strings.HasSuffix(media.URL, "%2F"+tc.wantKey)) {
				t.Fatalf("wrong accessible edition: %+v", media)
			}
		})
	}
}
