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
		{"other user's filtered button", "22222", "requests:11111:10:available", "invalid", 10, 21},
		{"unknown status", "11111", "requests:11111:10:bogus", "invalid", 10, 21},
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

func TestDiscordRequestFilterUsesGlobalListAndRevalidatesAccess(t *testing.T) {
	for _, tc := range []struct {
		name, status, state string
		offset, total       int
	}{
		{"first filtered page", "available", "", 0, 21},
		{"next filtered page", "available", "", 10, 21},
		{"last filtered page", "available", "", 20, 21},
		{"empty filter", "declined", "", 0, 0},
		{"page disappeared", "available", "empty", 10, 0},
		{"incomplete source", "available", "incomplete", 10, 0},
		{"foreign row before matching page", "available", "foreign", 10, 0},
		{"access revoked during read", "available", "revoked", 10, 0},
		{"identity removed during read", "available", "unlinked", 10, 0},
		{"source unavailable", "available", "outage", 10, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t)
			seed(t, s, "11111", true)
			listReads, detailReads := 0, 0
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case r.URL.Path == "/api/v1/user":
					return response(200, `{"pageInfo":{"results":2},"results":[{"id":42,"plexId":11111},{"id":99,"plexId":22222}]}`), nil
				case r.URL.Path == "/api/v1/user/42/requests":
					listReads++
					skip := (listReads - 1) * 100
					if r.URL.Query().Get("take") != "100" || r.URL.Query().Get("skip") != strconv.Itoa(skip) {
						t.Errorf("filter applied to a partial page: %s", r.URL.RawQuery)
					}
					switch tc.state {
					case "outage":
						return response(503, `{}`), nil
					case "incomplete":
						return response(200, `{"pageInfo":{"results":1001},"results":[]}`), nil
					case "empty":
						return response(200, `{"pageInfo":{"results":0},"results":[]}`), nil
					}
					rows := []string{}
					for id := skip + 1; id <= skip+100 && id <= 120; id++ {
						status, owner := 3, 42
						if id >= 91 && id <= 111 {
							status = 5
						}
						if id == 1 && tc.state == "foreign" {
							owner = 99
						}
						rows = append(rows, fmt.Sprintf(`{"id":%d,"type":"movie","status":2,"requestedBy":{"id":%d},"media":{"tmdbId":%d,"status":%d}}`, id, owner, id, status))
					}
					return response(200, `{"pageInfo":{"results":120},"results":[`+strings.Join(rows, ",")+`]}`), nil
				case strings.HasPrefix(r.URL.Path, "/api/v1/movie/"):
					detailReads++
					id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/v1/movie/"))
					if id < 91+tc.offset || id > 100+tc.offset || id > 111 {
						t.Errorf("enriched outside the selected filtered page: %d", id)
					}
					if tc.state == "revoked" {
						s.mu.Lock()
						delete(s.grants, "11111")
						s.mu.Unlock()
					}
					if tc.state == "unlinked" && detailReads == 1 {
						if err := s.Store.Delete(r.Context(), "11111"); err != nil {
							t.Fatal(err)
						}
					}
					return response(200, fmt.Sprintf(`{"title":"Private film %d"}`, id)), nil
				default:
					t.Errorf("wrong owner or route: %s", r.URL.Path)
					return response(500, `{}`), nil
				}
			})
			public, private, _ := ed25519.GenerateKey(rand.Reader)
			s.Config.DiscordPublicKey = hex.EncodeToString(public)
			s.Config.BotAPISecret = strings.Repeat("s", 32)
			kind, data := 3, fmt.Sprintf(`{"component_type":2,"custom_id":"requests:11111:%d:%s"}`, tc.offset, tc.status)
			if tc.offset == 0 {
				kind, data = 2, fmt.Sprintf(`{"name":"mes-demandes","options":[{"name":"etat","type":3,"value":%q}]}`, tc.status)
			}
			body := fmt.Sprintf(`{"id":"99999","application_id":"12345","guild_id":"23456","type":%d,"member":{"user":{"id":"11111"}},"message":{"flags":64},"data":%s}`, kind, data)
			stamp := strconv.FormatInt(time.Now().Unix(), 10)
			r := httptest.NewRequest("POST", "/internal/discord", strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+s.Config.BotAPISecret)
			r.Header.Set("X-Signature-Timestamp", stamp)
			r.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(private, []byte(stamp+body))))
			tampered := httptest.NewRequest("POST", "/internal/discord", strings.NewReader(strings.Replace(body, tc.status, "pending", 1)))
			tampered.Header = r.Header.Clone()
			denied := httptest.NewRecorder()
			s.Handler().ServeHTTP(denied, tampered)
			if denied.Code != 403 || listReads != 0 || detailReads != 0 {
				t.Fatal("filter changed without a matching signature")
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			var reply struct {
				Content    string                     `json:"content"`
				Flags      int                        `json:"flags"`
				Pagination *discord.RequestPagination `json:"pagination"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Flags != 64 || !strings.Contains(w.Body.String(), `"parse":[]`) {
				t.Fatalf("unsafe filtered reply: %d %s", w.Code, w.Body)
			}
			if tc.state != "" && tc.state != "empty" {
				if reply.Pagination != nil || strings.Contains(reply.Content, "Private film") {
					t.Fatalf("partial or revoked result retained: %s", w.Body)
				}
				if tc.state == "incomplete" && !strings.Contains(reply.Content, "liste complète") {
					t.Fatal("incomplete source looks like an empty filter")
				}
				return
			}
			if reply.Pagination == nil || reply.Pagination.Offset != tc.offset || reply.Pagination.Total != tc.total || reply.Pagination.Status != tc.status || !strings.Contains(reply.Content, "État : "+statusLabel(tc.status)) {
				t.Fatalf("filter or pagination lost: %s", w.Body)
			}
			if tc.state == "empty" {
				if !strings.Contains(reply.Content, "page précédente") || len(reply.Pagination.Components("11111")) != 1 {
					t.Fatal("no filtered page recovery")
				}
			} else if tc.total == 0 {
				if !strings.Contains(reply.Content, "Aucune demande dans cet état") || detailReads != 0 || listReads != 2 {
					t.Fatal("empty filter not checked globally")
				}
			} else if listReads != 2 || detailReads != min(10, 21-tc.offset) || !strings.Contains(reply.Content, fmt.Sprintf("Private film %d", 91+tc.offset)) {
				t.Fatalf("wrong filtered page: %s (%d lists, %d details)", reply.Content, listReads, detailReads)
			}
		})
	}
}
