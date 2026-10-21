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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func signedErisTool(t *testing.T, s *Server, plan eris.Plan, body string) eris.ToolRequest {
	t.Helper()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	s.Config.DiscordPublicKey = hex.EncodeToString(pub)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	return eris.ToolRequest{Plan: plan, Interaction: json.RawMessage(body), Timestamp: ts, Signature: hex.EncodeToString(ed25519.Sign(key, []byte(ts+body)))}
}

func callErisTool(s *Server, in eris.ToolRequest) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(in)
	r := httptest.NewRequest("POST", "/internal/eris", strings.NewReader(string(raw)))
	r.Header.Set("Authorization", "Bearer "+s.Config.BotAPISecret)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

const erisSlash = `{"id":"99999","application_id":"12345","guild_id":"23456","type":2,"member":{"user":{"id":"11111"}},"data":{"name":"eris","options":[{"name":"message","type":3,"value":"Arrival"}]}}`

func TestErisSignedIdentityAndRevocation(t *testing.T) {
	for _, state := range []string{"linked", "unlinked", "revoked during read", "forged", "wrong guild", "no service auth", "replay"} {
		t.Run(state, func(t *testing.T) {
			s := fixture(t)
			s.Config.BotAPISecret = strings.Repeat("s", 32)
			if state != "unlinked" {
				seed(t, s, "11111", true)
			}
			reads := 0
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				reads++
				if r.Method != "GET" || r.URL.Host != "plex.test" {
					t.Error("unexpected write or source")
				}
				if r.URL.Path == "/identity" {
					return response(200, `{"MediaContainer":{"machineIdentifier":"server-a"}}`), nil
				}
				if r.URL.Path == "/library/all" {
					if r.Header.Get("X-Plex-Token") != "resource-11111" {
						t.Error("wrong Plex scope")
					}
					if state == "revoked during read" {
						_ = s.Store.Delete(context.Background(), "11111")
					}
					return response(200, `{"MediaContainer":{"size":1,"totalSize":1,"Metadata":[{"ratingKey":"1","title":"Arrival","type":"movie","addedAt":1,"duration":6000000,"summary":"Private synopsis"}]}}`), nil
				}
				return response(200, `{"MediaContainer":{"size":1,"totalSize":1,"Metadata":[{"ratingKey":"1","type":"movie","Media":[{"Part":[{"key":"/library/parts/1/file","exists":true,"accessible":true}]}]}]}}`), nil
			})
			body := erisSlash
			if state == "wrong guild" {
				body = strings.Replace(body, "23456", "34567", 1)
			}
			in := signedErisTool(t, s, eris.Plan{Action: "search", Query: "Arrival"}, body)
			if state == "forged" {
				in.Signature = strings.Repeat("0", 128)
			}
			if state == "no service auth" {
				s.Config.BotAPISecret = ""
			}
			w := callErisTool(s, in)
			if state == "linked" || state == "replay" {
				if w.Code != 200 || !strings.Contains(w.Body.String(), "Private synopsis") {
					t.Fatalf("missing result %d %s", w.Code, w.Body)
				}
			} else if strings.Contains(w.Body.String(), "Private synopsis") {
				t.Fatal("disclosed revoked data")
			}
			if state == "forged" || state == "wrong guild" || state == "no service auth" || state == "unlinked" {
				if reads != 0 {
					t.Fatal("read before authorization")
				}
			}
			if state == "replay" {
				before := reads
				w = callErisTool(s, in)
				if w.Code != 403 || reads != before {
					t.Fatal("replayed proof accepted")
				}
			}
		})
	}
}

func TestErisPublicPersonalActionsNeverReadMedia(t *testing.T) {
	for _, plan := range []eris.Plan{{Action: "status"}, {Action: "prepare", Query: "Arrival", Kind: "movie"}, {Action: "recommend", Unwatched: true}} {
		s := fixture(t)
		s.Config.BotAPISecret = strings.Repeat("s", 32)
		s.Config.DiscordBotToken = "fixture"
		s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host != "discord.com" || r.Method != "GET" {
				t.Fatal("public personal action accessed media")
			}
			if r.URL.Path == "/api/v10/channels/33333" {
				return response(200, `{"id":"33333","guild_id":"23456"}`), nil
			}
			return response(200, fmt.Sprintf(`{"id":"99999","channel_id":"33333","timestamp":%q,"content":"<@12345> question","author":{"id":"11111"},"mentions":[{"id":"12345"}]}`, time.Now().Format(time.RFC3339))), nil
		})
		w := callErisTool(s, eris.ToolRequest{Plan: plan, ChannelID: "33333", MessageID: "99999"})
		if w.Code != 200 || !strings.Contains(w.Body.String(), "en privé") {
			t.Fatalf("privacy boundary %d %s", w.Code, w.Body)
		}
	}
}

func TestErisGatewayProofRejectsOtherGuildBotMissingMentionAndStaleMessages(t *testing.T) {
	for _, state := range []string{"other guild", "bot", "webhook", "missing mention", "stale", "wrong channel"} {
		s := fixture(t)
		s.Config.BotAPISecret = strings.Repeat("s", 32)
		s.Config.DiscordBotToken = "fixture"
		s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host != "discord.com" {
				t.Fatal("untrusted gateway accessed media")
			}
			if r.URL.Path == "/api/v10/channels/33333" {
				guild := "23456"
				if state == "other guild" {
					guild = "34567"
				}
				return response(200, `{"id":"33333","guild_id":"`+guild+`"}`), nil
			}
			stamp := time.Now()
			if state == "stale" {
				stamp = stamp.Add(-time.Hour)
			}
			m := map[string]any{"id": "99999", "channel_id": "33333", "timestamp": stamp.Format(time.RFC3339), "content": "<@12345> question", "author": map[string]any{"id": "11111", "bot": state == "bot"}, "mentions": []any{map[string]string{"id": "12345"}}}
			if state == "webhook" {
				m["webhook_id"] = "88888"
			}
			if state == "missing mention" {
				m["content"] = "hello"
			}
			if state == "wrong channel" {
				m["channel_id"] = "44444"
			}
			raw, _ := json.Marshal(m)
			return response(200, string(raw)), nil
		})
		if w := callErisTool(s, eris.ToolRequest{Plan: eris.Plan{Action: "recent"}, ChannelID: "33333", MessageID: "99999"}); w.Code != 403 {
			t.Fatalf("accepted %s", state)
		}
	}
}

func TestErisConfirmationIsPrivateOwnerBoundExpiringAndExactlyOnce(t *testing.T) {
	for _, state := range []string{"created", "other owner", "expired", "cancel", "changed account", "uncertain"} {
		t.Run(state, func(t *testing.T) {
			s := fixture(t)
			seed(t, s, "11111", true)
			seed(t, s, "22222", true)
			u, _ := s.Store.User(context.Background(), "11111")
			var posts atomic.Int32
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/api/v1/user" {
					return response(200, `{"pageInfo":{"results":1},"results":[{"id":2,"plexId":11111}]}`), nil
				}
				if r.URL.Path == "/api/v1/auth/me" {
					return response(200, `{"id":2}`), nil
				}
				if r.Method != "POST" || r.URL.Path != "/api/v1/request" || r.Header.Get("X-Api-User") != "2" {
					t.Errorf("wrong request scope %s", r.URL.Path)
					return response(500, `{}`), nil
				}
				posts.Add(1)
				if state == "uncertain" {
					return response(500, `{}`), nil
				}
				return response(201, `{"id":9,"type":"movie","requestedBy":{"id":2},"media":{"tmdbId":123}}`), nil
			})
			p := erisPending{UserID: u.ID, PlexID: u.PlexID, DiscordID: "11111", GuildID: s.Config.DiscordGuildID, SeerrID: 2, Candidate: upstream.RequestCandidate{ID: 123, Title: "Arrival", Kind: "movie", Year: "2016"}, Stage: "choose", Expires: time.Now().Add(time.Minute)}
			key := s.saveErisPending(p)
			chosen := s.erisConfirmation(context.Background(), u, "11111", "eris:choose:"+key)
			if posts.Load() != 0 || len(chosen.Buttons) != 2 || !strings.Contains(chosen.Content, "Confirmer") {
				t.Fatal("selection wrote without confirmation")
			}
			confirm := chosen.Buttons[0].ID
			switch state {
			case "other owner":
				u, _ = s.Store.User(context.Background(), "22222")
			case "expired":
				key = strings.Split(confirm, ":")[2]
				entry := s.erisPending[key]
				entry.Expires = time.Now().Add(-time.Minute)
				s.erisPending[key] = entry
			case "cancel":
				confirm = chosen.Buttons[1].ID
			case "changed account":
				u.PlexID = "77777"
			}
			var wg sync.WaitGroup
			results := make(chan eris.ToolReply, 2)
			for range 2 {
				wg.Add(1)
				go func() { defer wg.Done(); results <- s.erisConfirmation(context.Background(), u, u.DiscordID, confirm) }()
			}
			wg.Wait()
			close(results)
			want := int32(0)
			if state == "created" || state == "uncertain" {
				want = 1
			}
			if posts.Load() != want {
				t.Fatalf("posts %d want %d", posts.Load(), want)
			}
			for r := range results {
				if state != "created" && strings.Contains(r.Content, "créée") {
					t.Fatal("false success")
				}
			}
		})
	}
}
