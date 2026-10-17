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
)

func TestDiscordRequestPaginationRechecksIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, user, customID, state string
		offset, total               int
	}{
		{"first page", "11111", "", "", 0, 21},
		{"next page", "11111", "requests:11111:10", "", 10, 21},
		{"last page", "11111", "requests:11111:20", "", 20, 21},
		{"previous page", "11111", "requests:11111:0", "", 0, 21},
		{"list emptied", "11111", "requests:11111:10", "empty", 10, 0},
		{"other user's button", "22222", "requests:11111:10", "invalid", 10, 21},
		{"invalid offset", "11111", "requests:11111:11", "invalid", 11, 21},
		{"unlinked account", "33333", "requests:33333:10", "unlinked", 10, 21},
		{"expired grant", "11111", "requests:11111:10", "expired", 10, 21},
		{"Seerr outage", "11111", "requests:11111:10", "outage", 10, 21},
		{"foreign request", "11111", "requests:11111:10", "foreign", 10, 21},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t)
			seed(t, s, "11111", true)
			seed(t, s, "22222", true)
			// Old manual links must not bypass the current Plex -> Seerr lookup.
			if err := s.Store.LinkSeerr(context.Background(), "11111", 99); err != nil {
				t.Fatal(err)
			}
			if tc.state == "expired" {
				g := s.grants["11111"]
				g.ExpiresAt = time.Now().Add(-time.Minute)
				s.grants["11111"] = g
			}
			calls := 0
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				switch r.URL.Path {
				case "/api/v1/user":
					if tc.state == "outage" {
						return response(503, `{}`), nil
					}
					return response(200, `{"pageInfo":{"results":2},"results":[{"id":42,"plexId":11111},{"id":99,"plexId":22222}]}`), nil
				case "/api/v1/user/42/requests":
					if r.URL.Query().Get("skip") != strconv.Itoa(tc.offset) || r.URL.Query().Get("take") != "10" {
						t.Errorf("wrong pagination: %s", r.URL.RawQuery)
					}
					if tc.state == "empty" {
						return response(200, `{"pageInfo":{"results":0},"results":[]}`), nil
					}
					owner := 42
					if tc.state == "foreign" {
						owner = 99
					}
					return response(200, fmt.Sprintf(`{"pageInfo":{"results":%d},"results":[{"id":1,"type":"movie","status":2,"requestedBy":{"id":%d},"media":{"tmdbId":100,"status":5}}]}`, tc.total, owner)), nil
				case "/api/v1/movie/100":
					return response(200, `{"title":"Film @everyone <@12345> **test**"}`), nil
				default:
					t.Errorf("wrong identity or route accessed: %s", r.URL.Path)
					return response(500, `{}`), nil
				}
			})
			public, private, _ := ed25519.GenerateKey(rand.Reader)
			s.Config.DiscordPublicKey = hex.EncodeToString(public)
			s.Config.BotAPISecret = strings.Repeat("s", 32)
			kind, data := 3, fmt.Sprintf(`{"component_type":2,"custom_id":%q}`, tc.customID)
			if tc.customID == "" {
				kind, data = 2, `{"name":"mes-demandes"}`
			}
			body := fmt.Sprintf(`{"id":"99999","application_id":"12345","guild_id":"23456","type":%d,"member":{"user":{"id":%q}},"message":{"flags":64},"data":%s}`, kind, tc.user, data)
			stamp := strconv.FormatInt(time.Now().Unix(), 10)
			request := func(payload string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "/internal/discord", strings.NewReader(payload))
				r.Header.Set("Authorization", "Bearer "+s.Config.BotAPISecret)
				r.Header.Set("X-Signature-Timestamp", stamp)
				r.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(private, []byte(stamp+body))))
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				return w
			}
			if w := request(body + " "); w.Code != 403 {
				t.Fatal("tampered component accepted")
			}
			w := request(body)
			if tc.state == "invalid" {
				if w.Code != 403 || calls != 0 {
					t.Fatalf("invalid click reached requests: %d %d", w.Code, calls)
				}
				return
			}
			var reply struct {
				Content    string                     `json:"content"`
				Flags      int                        `json:"flags"`
				Pagination *discord.RequestPagination `json:"pagination"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Flags != 64 || !strings.Contains(w.Body.String(), `"parse":[]`) {
				t.Fatalf("unsafe reply: %d %s", w.Code, w.Body)
			}
			if tc.state == "" || tc.state == "empty" {
				if reply.Pagination == nil || reply.Pagination.Offset != tc.offset || reply.Pagination.Total != tc.total {
					t.Fatalf("wrong pagination: %s", w.Body)
				}
				if tc.state == "empty" {
					if !strings.Contains(reply.Content, "page précédente") {
						t.Error("no recovery from emptied page")
					}
				} else if !strings.Contains(reply.Content, "Disponible") || strings.Contains(reply.Content, "@everyone") || strings.Contains(reply.Content, "<@") {
					t.Errorf("unsafe or missing request: %s", reply.Content)
				}
			} else {
				if reply.Pagination != nil || strings.Contains(reply.Content, "Film") {
					t.Errorf("private results retained after access failure: %s", reply.Content)
				}
				if (tc.state == "expired" || tc.state == "unlinked") && calls != 0 {
					t.Error("requests accessed without current grant")
				}
			}
			if w := request(body); w.Code != 409 {
				t.Fatal("component replay accepted")
			}
		})
	}
}
