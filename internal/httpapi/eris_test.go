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

	"github.com/bwmarrin/discordgo"
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
	for _, plan := range []eris.Plan{{Action: "status"}, {Action: "recommend", Unwatched: true}} {
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

func TestErisMissingSeasonAsksBeforeReadingOrCreatingRequest(t *testing.T) {
	s := fixture(t)
	seed(t, s, "11111", true)
	u, _ := s.Store.User(context.Background(), "11111")
	s.Upstream.HTTP.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("missing season reached upstream")
		return nil, nil
	})
	reply := s.runErisTool(context.Background(), u, s.grants[u.ID], u.DiscordID, eris.Plan{Action: "prepare", Query: "Severance", Kind: "show"}, true)
	if !strings.Contains(reply.Content, "Quelle saison") || len(reply.Buttons) > 0 {
		t.Fatal("invented season")
	}
}

func TestErisPublicRequestOffersChoicesWithoutPrivateHandoff(t *testing.T) {
	s := fixture(t)
	s.Config.BotAPISecret = strings.Repeat("s", 32)
	seed(t, s, "11111", true)
	seed(t, s, "22222", true)
	u, _ := s.Store.User(context.Background(), "11111")
	reads := 0
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		reads++
		if r.Method != "GET" {
			t.Fatal("request created before confirmation")
		}
		switch r.URL.Path {
		case "/api/v1/user":
			return response(200, `{"pageInfo":{"results":1},"results":[{"id":2,"plexId":11111}]}`), nil
		case "/api/v1/auth/me":
			return response(200, `{"id":2}`), nil
		case "/api/v1/search":
			if r.URL.Query().Get("query") != "Hot Fuzz" {
				t.Error("lost conversation title")
			}
			return response(200, `{"results":[{"id":4638,"mediaType":"movie","title":"Hot Fuzz","releaseDate":"2007-02-14"}]}`), nil
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			return response(500, `{}`), nil
		}
	})
	reply := s.runErisTool(context.Background(), u, s.grants[u.ID], "11111", eris.Plan{Action: "prepare", Query: "Hot Fuzz", Kind: "movie"}, false)
	if reads != 3 || len(reply.Buttons) != 1 || !strings.HasPrefix(reply.Buttons[0].ID, "eris:choose:") || !strings.Contains(reply.Content, "Hot Fuzz") {
		t.Fatalf("public request did not search Seerr directly: %+v reads=%d", reply, reads)
	}
	button := reply.Buttons[0].ID
	for index, owner := range []string{"22222", "11111", "11111"} {
		body := fmt.Sprintf(`{"id":"%d","application_id":"12345","guild_id":"23456","channel_id":"33333","type":3,"member":{"user":{"id":"%s"}},"message":{"flags":0,"author":{"id":"12345"}},"data":{"custom_id":%q,"component_type":2}}`, 99990+index, owner, button)
		w := callErisTool(s, signedErisTool(t, s, eris.Plan{}, body))
		if owner == "22222" {
			if reads != 3 || strings.Contains(w.Body.String(), "Hot Fuzz") {
				t.Fatal("another member used the owner's choice")
			}
			continue
		}
		if index == 1 {
			if reads != 3 || !strings.Contains(w.Body.String(), "Hot Fuzz") || !strings.Contains(w.Body.String(), "eris:confirm:") {
				t.Fatalf("public selection failed %d %s", w.Code, w.Body)
			}
		} else if reads != 3 || strings.Contains(w.Body.String(), "Hot Fuzz") {
			t.Fatal("selection replay read media again")
		}
	}
}

func TestErisAPIRevalidatesReplyTargetFromDiscord(t *testing.T) {
	for _, state := range []string{"valid", "other author", "other channel", "missing reference", "hidden content"} {
		s := fixture(t)
		s.Config.DiscordBotToken = "fixture"
		s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != "GET" || r.URL.Host != "discord.com" {
				t.Fatal("unexpected upstream")
			}
			if r.URL.Path == "/api/v10/channels/33333" {
				return response(200, `{"id":"33333","guild_id":"23456"}`), nil
			}
			m := discordgo.Message{ID: "99999", ChannelID: "33333", Timestamp: time.Now(), Type: discordgo.MessageTypeReply, Content: "Tu peux le demander ?", Author: &discordgo.User{ID: "11111"}, MessageReference: &discordgo.MessageReference{MessageID: "88888", ChannelID: "33333", GuildID: "23456"}, ReferencedMessage: &discordgo.Message{ID: "88888", ChannelID: "33333", Author: &discordgo.User{ID: "12345", Bot: true}}}
			switch state {
			case "other author":
				m.ReferencedMessage.Author.ID = "77777"
			case "other channel":
				m.ReferencedMessage.ChannelID = "44444"
			case "missing reference":
				m.ReferencedMessage = nil
			case "hidden content":
				m.Content = ""
			}
			raw, _ := json.Marshal(m)
			return response(200, string(raw)), nil
		})
		id, private, _, err := s.erisCaller(context.Background(), eris.ToolRequest{ChannelID: "33333", MessageID: "99999"})
		if state == "valid" {
			if err != nil || private || id != "11111" {
				t.Fatal(id, private, err)
			}
		} else if err == nil {
			t.Fatalf("accepted %s", state)
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

func TestErisConfirmationIsOwnerBoundExpiringAndExactlyOnce(t *testing.T) {
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
			chosen := s.erisConfirmation(context.Background(), u, "11111", "eris:choose:"+key, false)
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
				go func() {
					defer wg.Done()
					results <- s.erisConfirmation(context.Background(), u, u.DiscordID, confirm, false)
				}()
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

func TestErisSignedConfirmationKeepsItsVisibilityAndOwner(t *testing.T) {
	for _, private := range []bool{false, true} {
		s := fixture(t)
		s.Config.BotAPISecret = strings.Repeat("s", 32)
		seed(t, s, "11111", true)
		seed(t, s, "22222", true)
		u, _ := s.Store.User(context.Background(), "11111")
		posts := 0
		s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			switch r.URL.Path {
			case "/api/v1/user":
				return response(200, `{"pageInfo":{"results":1},"results":[{"id":2,"plexId":11111}]}`), nil
			case "/api/v1/auth/me":
				return response(200, `{"id":2}`), nil
			case "/api/v1/request":
				if r.Method != "POST" || r.Header.Get("X-Api-User") != "2" {
					t.Fatal("wrong request owner")
				}
				posts++
				return response(201, `{"id":9,"type":"movie","requestedBy":{"id":2},"media":{"tmdbId":4638}}`), nil
			default:
				t.Fatal("unexpected upstream", r.URL.Path)
				return nil, nil
			}
		})
		key := s.saveErisPending(erisPending{UserID: u.ID, PlexID: u.PlexID, DiscordID: "11111", GuildID: s.Config.DiscordGuildID, SeerrID: 2, Candidate: upstream.RequestCandidate{ID: 4638, Title: "Hot Fuzz", Kind: "movie", Year: "2007"}, Stage: "confirm", Private: private, Expires: time.Now().Add(time.Minute)})
		for index, attempt := range []struct {
			owner   string
			private bool
			created bool
		}{
			{"22222", private, false}, {"11111", !private, false}, {"11111", private, true}, {"11111", private, false},
		} {
			flags := 0
			if attempt.private {
				flags = 64
			}
			body := fmt.Sprintf(`{"id":"%d","application_id":"12345","guild_id":"23456","channel_id":"33333","type":3,"member":{"user":{"id":%q}},"message":{"flags":%d,"author":{"id":"12345"}},"data":{"custom_id":%q,"component_type":2}}`, 99990+index, attempt.owner, flags, "eris:confirm:"+key)
			w := callErisTool(s, signedErisTool(t, s, eris.Plan{}, body))
			if w.Code != 200 || strings.Contains(w.Body.String(), "créée") != attempt.created {
				t.Fatalf("private=%v attempt=%+v: %d %s", private, attempt, w.Code, w.Body)
			}
			if index < 2 && (posts != 0 || strings.Contains(w.Body.String(), "Hot Fuzz")) {
				t.Fatal("wrong owner or visibility used pending request")
			}
		}
		if posts != 1 {
			t.Fatal("request not exactly once", posts)
		}
	}
}
