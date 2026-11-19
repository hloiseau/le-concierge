package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func TestErisAdminDelegatesMediaToTheActualRecipient(t *testing.T) {
	for _, action := range []string{"request", "status"} {
		t.Run(action, func(t *testing.T) {
			s := fixture(t)
			seed(t, s, "11111", true)
			seed(t, s, "22222", true)
			s.Config.AdminIDs = []string{"11111"}
			s.Config.BotAPISecret = strings.Repeat("s", 32)
			posts, reads := 0, 0
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/api/v1/user":
					return response(200, `{"pageInfo":{"results":2},"results":[{"id":2,"plexId":11111},{"id":3,"plexId":22222}]}`), nil
				case "/api/v1/auth/me":
					return response(200, `{"id":3}`), nil
				case "/api/v1/movie/270946":
					return response(200, `{"id":270946,"title":"Les Pingouins de Madagascar"}`), nil
				case "/api/v1/user/3/requests":
					reads++
					return response(200, `{"pageInfo":{"results":1},"results":[{"id":415,"type":"movie","status":2,"requestedBy":{"id":3},"media":{"tmdbId":270946}}]}`), nil
				case "/api/v1/request":
					posts++
					if r.Header.Get("X-Api-User") != "3" {
						t.Error("request still belongs to Hugo")
					}
					return response(201, `{"id":415,"type":"movie","requestedBy":{"id":3},"media":{"tmdbId":270946}}`), nil
				default:
					t.Error("unexpected route", r.URL.Path)
					return response(500, `{}`), nil
				}
			})
			p := eris.Plan{Action: action}
			if action == "request" {
				p.MediaID, p.Kind = 270946, "movie"
			}
			body := strings.Replace(erisSlash, "Arrival", "Demande les Pingouins pour <@22222>", 1)
			in := signedErisTool(t, s, p, body)
			in.CallID = "tool-1"
			raw, _ := json.Marshal(in)
			var envelope map[string]any
			json.Unmarshal(raw, &envelope)
			envelope["plan"].(map[string]any)["targetDiscordId"] = "22222"
			raw, _ = json.Marshal(envelope)
			r := httptest.NewRequest("POST", "/internal/eris", strings.NewReader(string(raw)))
			r.Header.Set("Authorization", "Bearer "+s.Config.BotAPISecret)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != 200 || action == "request" && posts != 1 || action == "status" && reads != 1 {
				t.Fatalf("delegated %s failed: http=%d posts=%d reads=%d body=%s", action, w.Code, posts, reads, w.Body)
			}
			var reply eris.ToolReply
			if json.Unmarshal(w.Body.Bytes(), &reply) != nil {
				t.Fatal("invalid tool envelope", w.Body)
			}
			var facts struct {
				RequestOwner struct {
					DiscordID string `json:"discordId"`
				} `json:"requestOwner"`
				ActedBy struct {
					DiscordID string `json:"discordId"`
				} `json:"actedBy"`
				Delegated bool `json:"delegated"`
				Verified  bool `json:"requestOwnerVerified"`
			}
			if json.Unmarshal([]byte(reply.Content), &facts) != nil || facts.RequestOwner.DiscordID != "22222" || facts.ActedBy.DiscordID != "11111" || !facts.Delegated || !facts.Verified {
				t.Fatal("model did not receive the actual verified owner", reply.Content)
			}
		})
	}
}

func TestErisDelegationNeverFallsBackToTheCallerOrExposesRevokedData(t *testing.T) {
	for _, action := range []string{"request", "status"} {
		for _, state := range []string{"not admin", "unknown recipient", "recipient unlinked", "actor revoked", "admin revoked", "recipient removed"} {
			t.Run(action+"/"+state, func(t *testing.T) {
				s := fixture(t)
				seed(t, s, "11111", true)
				seed(t, s, "22222", true)
				s.Config.AdminIDs = []string{"11111"}
				s.Config.BotAPISecret = strings.Repeat("s", 32)
				if state == "not admin" {
					s.Config.AdminIDs = nil
				}
				if state == "recipient unlinked" {
					s.Store.Delete(context.Background(), "22222")
				}
				reads, posts := 0, 0
				revoke := func() {
					switch state {
					case "actor revoked":
						s.mu.Lock()
						delete(s.grants, "11111")
						s.mu.Unlock()
					case "admin revoked":
						s.Config.AdminIDs = nil
					case "recipient removed":
						s.Store.Delete(context.Background(), "22222")
					}
				}
				s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
					reads++
					switch r.URL.Path {
					case "/api/v1/user":
						return response(200, `{"pageInfo":{"results":2},"results":[{"id":2,"plexId":11111},{"id":3,"plexId":22222}]}`), nil
					case "/api/v1/auth/me":
						return response(200, `{"id":3}`), nil
					case "/api/v1/movie/270946":
						revoke()
						return response(200, `{"id":270946,"title":"HIDDEN_RESULT"}`), nil
					case "/api/v1/user/3/requests":
						revoke()
						return response(200, `{"pageInfo":{"results":1},"results":[{"id":415,"type":"movie","requestedBy":{"id":3},"media":{"tmdbId":270946}}]}`), nil
					case "/api/v1/request":
						posts++
						return response(500, `{}`), nil
					default:
						t.Error("wrong identity or endpoint", r.URL.Path)
						return response(500, `{}`), nil
					}
				})
				p := eris.Plan{Action: action, TargetDiscordID: "22222"}
				if action == "request" {
					p.MediaID, p.Kind = 270946, "movie"
				}
				if state == "unknown recipient" {
					p.TargetDiscordID = "77777"
				}
				w := callErisTool(s, signedErisTool(t, s, p, erisSlash))
				if w.Code != 200 || posts != 0 || strings.Contains(w.Body.String(), "HIDDEN_RESULT") {
					t.Fatal(state, w.Code, w.Body, posts)
				}
				if (state == "not admin" || state == "unknown recipient" || state == "recipient unlinked") && reads != 0 {
					t.Fatal("read before delegation was authorized", reads)
				}
			})
		}
	}
}

func TestConfirmedRequestReceiptCannotClaimAnOwnerAfterReassignment(t *testing.T) {
	s := fixture(t)
	posts, checks := 0, 0
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			return response(200, `{"id":2}`), nil
		case "/api/v1/request":
			posts++
			return response(201, `{"id":415,"type":"movie","requestedBy":{"id":2},"media":{"tmdbId":270946}}`), nil
		case "/api/v1/request/415":
			checks++
			return response(200, `{"id":415,"type":"movie","requestedBy":{"id":3},"media":{"tmdbId":270946}}`), nil
		default:
			t.Error(r.URL.Path)
			return response(500, `{}`), nil
		}
	})
	p := erisMediaRequest{UserID: "11111", SeerrID: 2, Candidate: upstream.RequestCandidate{ID: 270946, Kind: "movie", Title: "Les Pingouins de Madagascar"}}
	first := s.submitErisRequest(context.Background(), p)
	second := s.submitErisRequest(context.Background(), p)
	if !strings.Contains(first.Content, `"status":"created"`) || !strings.Contains(second.Content, "stored_request_owner_unverified") || posts != 1 || checks != 1 {
		t.Fatal(first, second, posts, checks)
	}
}

func TestErisSignedInteractionPreservesWhitespaceAndMentionBytes(t *testing.T) {
	s := fixture(t)
	s.Config.BotAPISecret = strings.Repeat("s", 32)
	var pretty bytes.Buffer
	json.Indent(&pretty, []byte(strings.Replace(erisSlash, "Arrival", "<@22222> & films", 1)), "", "  ")
	in := signedErisTool(t, s, eris.Plan{Action: "monitoring"}, pretty.String())
	w := callErisTool(s, in)
	if w.Code != 200 {
		t.Fatal("signed interaction bytes were rewritten in transit", w.Body)
	}
}
