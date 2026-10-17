package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPublicCatalogueCommandsRequireConsentAndCurrentAccess(t *testing.T) {
	for _, command := range []string{"nouveautes", "surprise", "recap"} {
		for _, state := range []string{"private default", "explicit false", "public", "unlinked", "expired", "revoked", "outage", "deleted during read", "rate limited"} {
			t.Run(command+"/"+state, func(t *testing.T) {
				s := fixture(t)
				if state != "unlinked" {
					seed(t, s, "11111", true)
				}
				if state == "expired" {
					delete(s.grants, "11111")
				}
				if state == "rate limited" {
					for range 30 {
						s.rate("discord-personal:11111", 30)
					}
				}
				calls := 0
				s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Method != "GET" || r.URL.Host != "plex.test" || (r.URL.Path != "/identity" && r.Header.Get("X-Plex-Token") != "resource-11111") {
						t.Fatal("public catalogue did not use the caller's read-only Plex access")
					}
					if state == "outage" {
						return response(503, `{}`), nil
					}
					if state == "revoked" {
						return response(403, `{}`), nil
					}
					switch r.URL.Path {
					case "/identity":
						return response(200, `{"MediaContainer":{"machineIdentifier":"server-a"}}`), nil
					case "/library/all", "/library/recentlyAdded":
						if state == "deleted during read" {
							if err := s.Store.Delete(context.Background(), "11111"); err != nil {
								t.Fatal(err)
							}
						}
						return response(200, fmt.Sprintf(`{"MediaContainer":{"size":1,"totalSize":1,"Metadata":[{"ratingKey":"1","title":"Shared film @everyone","type":"movie","addedAt":%d}]}}`, time.Now().Add(-48*time.Hour).Unix())), nil
					case "/library/metadata/1":
						return response(200, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"1","type":"movie","Media":[{"Part":[{"key":"/library/parts/1/file.mkv","exists":true,"accessible":true}]}]}]}}`), nil
					default:
						t.Fatalf("unexpected catalogue endpoint: %s", r.URL.Path)
						return nil, nil
					}
				})
				options := `,"options":[{"name":"public","type":5,"value":true}]`
				if state == "private default" {
					options = ""
				} else if state == "explicit false" {
					options = strings.Replace(options, "true", "false", 1)
				}
				w := subscriptionInteraction(t, s)("99999", "11111", fmt.Sprintf(`{"name":%q%s}`, command, options))
				var reply struct {
					Content       string `json:"content"`
					PublicContent string `json:"publicContent"`
					Flags         int    `json:"flags"`
				}
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Flags != 64 {
					t.Fatalf("private original reply lost: %d %s", w.Code, w.Body)
				}
				if (reply.PublicContent != "") != (state == "public") {
					t.Fatalf("public result did not honor consent/access: %s", w.Body)
				}
				if state == "public" {
					if !strings.Contains(reply.PublicContent, "Shared film") || len(reply.PublicContent) > 2000 {
						t.Fatalf("missing or oversized public result: %s", reply.PublicContent)
					}
					for _, forbidden := range []string{"@everyone", "<@", "app.plex.tv", "server-a", "resource-11111", "plex.test", "11111"} {
						if strings.Contains(reply.PublicContent, forbidden) {
							t.Fatalf("personal link, identity or mention in public result: %s", reply.PublicContent)
						}
					}
				}
				if state == "unlinked" || state == "expired" || state == "rate limited" {
					if calls != 0 {
						t.Fatal("denied command reached the catalogue")
					}
				}
			})
		}
	}
}

func TestPersonalCommandsRejectPublicOptionAtAPIBoundary(t *testing.T) {
	for _, command := range []string{"mes-demandes", "mes-suivis", "suivre", "livres", "demander-livre", "diagnostic", "aide"} {
		t.Run(command, func(t *testing.T) {
			s := fixture(t)
			seed(t, s, "11111", true)
			w := subscriptionInteraction(t, s)("99999", "11111", fmt.Sprintf(`{"name":%q,"options":[{"name":"public","type":5,"value":true}]}`, command))
			if w.Code != 403 || strings.Contains(w.Body.String(), "publicContent") {
				t.Fatalf("personal command accepted public option: %d %s", w.Code, w.Body)
			}
		})
	}
}
