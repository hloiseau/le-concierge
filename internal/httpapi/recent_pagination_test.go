package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/discord"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func TestDiscordRecentPagination(t *testing.T) {
	for _, mode := range []string{"first", "next", "empty", "outage", "foreign button", "logout", "unlinked"} {
		t.Run(mode, func(t *testing.T) {
			s := fixture(t)
			cookie := seed(t, s, "11111", true)
			seed(t, s, "22222", true)
			offset := 6
			if mode == "first" {
				offset = 0
			}
			calls := 0
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path == "/identity" {
					return response(200, `{"MediaContainer":{"machineIdentifier":"server-a"}}`), nil
				}
				if r.URL.Path != "/library/recentlyAdded" || r.Header.Get("X-Plex-Token") != "resource-11111" {
					t.Fatalf("unexpected personal upstream %s", r.URL.Path)
				}
				if r.URL.Query().Get("X-Plex-Container-Start") != strconv.Itoa(offset) || r.URL.Query().Get("X-Plex-Container-Size") != "7" {
					t.Errorf("wrong pagination %s", r.URL.RawQuery)
				}
				if mode == "outage" {
					return response(503, `{}`), nil
				}
				if mode == "logout" {
					call(s, "POST", "/api/v1/auth/logout", cookie, s.Config.PublicOrigin, "")
				}
				if mode == "unlinked" {
					if err := s.Store.Unlink(context.Background(), "11111"); err != nil {
						t.Fatal(err)
					}
				}
				items := []map[string]any{}
				if mode != "empty" {
					for i := range 7 {
						items = append(items, map[string]any{"ratingKey": strconv.Itoa(offset + i + 1), "title": "Film @everyone **test**", "type": "movie", "addedAt": 1704067200})
					}
				}
				body, _ := json.Marshal(map[string]any{"MediaContainer": map[string]any{"Metadata": items}})
				return response(200, string(body)), nil
			})
			public, private, _ := ed25519.GenerateKey(rand.Reader)
			s.Config.DiscordPublicKey = hex.EncodeToString(public)
			s.Config.BotAPISecret = strings.Repeat("s", 32)
			caller := "11111"
			if mode == "foreign button" {
				caller = "22222"
			}
			kind, data := 3, fmt.Sprintf(`{"component_type":2,"custom_id":"recent:11111:%d"}`, offset)
			if mode == "first" {
				kind, data = 2, `{"name":"nouveautes"}`
			}
			body := fmt.Sprintf(`{"id":"99999","application_id":"12345","guild_id":"23456","type":%d,"member":{"user":{"id":%q}},"message":{"flags":64},"data":%s}`, kind, caller, data)
			stamp := strconv.FormatInt(time.Now().Unix(), 10)
			r := httptest.NewRequest("POST", "/internal/discord", strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+s.Config.BotAPISecret)
			r.Header.Set("X-Signature-Timestamp", stamp)
			r.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(private, []byte(stamp+body))))
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if mode == "foreign button" {
				if w.Code != 403 || calls != 0 {
					t.Fatalf("foreign button accepted %d (%d calls)", w.Code, calls)
				}
				return
			}
			var reply struct {
				Content    string                    `json:"content"`
				Pagination *discord.RecentPagination `json:"recentPagination"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil {
				t.Fatalf("reply %d %s", w.Code, w.Body)
			}
			if mode == "outage" || mode == "logout" || mode == "unlinked" {
				if reply.Pagination != nil || strings.Contains(reply.Content, "test") {
					t.Fatalf("stale private message survived %s", w.Body)
				}
				return
			}
			if reply.Pagination == nil || reply.Pagination.Offset != offset || len(reply.Content) > 2000 {
				t.Fatalf("invalid pagination %s", w.Body)
			}
			if mode == "empty" {
				if reply.Pagination.NextOffset != nil || !strings.Contains(reply.Content, "Aucun ajout") {
					t.Fatal("empty page not terminal")
				}
			} else if reply.Pagination.NextOffset == nil || *reply.Pagination.NextOffset != offset+6 || !strings.Contains(reply.Content, "01/01/2024") || !strings.Contains(reply.Content, "https://app.plex.tv") || strings.Contains(reply.Content, "@everyone") {
				t.Fatalf("missing dates/links or unsafe text %s", w.Body)
			}
		})
	}
}

func TestRecentMessageBounded(t *testing.T) {
	items := make([]upstream.Media, 6)
	for i := range items {
		items[i] = upstream.Media{Title: strings.Repeat("🌒", 200), Type: "movie", AddedAt: "2026-10-16T12:00:00Z", URL: "https://app.plex.tv/" + strings.Repeat("x", 1000)}
	}
	if message := recentMessage(items, "https://hub.example"); len(message) > 2000 || strings.Count(message, "•") != 6 {
		t.Fatal("message limit lost an item or was exceeded")
	}
}

func TestRecentBrowserRevocationDuringFetch(t *testing.T) {
	for _, revoke := range []string{"logout", "unlink"} {
		t.Run(revoke, func(t *testing.T) {
			s := fixture(t)
			cookie := seed(t, s, "11111", true)
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/identity" {
					return response(200, `{"MediaContainer":{"machineIdentifier":"server-a"}}`), nil
				}
				if revoke == "logout" {
					call(s, "POST", "/api/v1/auth/logout", cookie, s.Config.PublicOrigin, "")
				} else if err := s.Store.Unlink(context.Background(), "11111"); err != nil {
					t.Fatal(err)
				}
				return response(200, `{"MediaContainer":{"Metadata":[{"ratingKey":"42","title":"Private title","type":"movie","addedAt":1704067200}]}}`), nil
			})
			w := call(s, "GET", "/api/v1/recent", cookie, "", "")
			if w.Code == 200 || strings.Contains(w.Body.String(), "Private title") {
				t.Fatalf("revoked response %d %s", w.Code, w.Body)
			}
		})
	}
}
