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
)

func subscriptionInteraction(t *testing.T, s *Server) func(id, user, data string) *httptest.ResponseRecorder {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s.Config.DiscordPublicKey = hex.EncodeToString(public)
	s.Config.BotAPISecret = strings.Repeat("s", 32)
	return func(id, user, data string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"id":%q,"application_id":"12345","guild_id":"23456","type":2,"member":{"user":{"id":%q}},"data":%s}`, id, user, data)
		stamp := strconv.FormatInt(time.Now().Unix(), 10)
		r := httptest.NewRequest("POST", "/internal/discord", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+s.Config.BotAPISecret)
		r.Header.Set("X-Signature-Timestamp", stamp)
		r.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(private, []byte(stamp+body))))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
}

const followFilmCommand = `{"name":"suivre","options":[{"name":"type","type":3,"value":"movie"},{"name":"tmdb-id","type":4,"value":12}]}`

func TestSubscriptionCommandsShareOwnerStateAndRemainPrivate(t *testing.T) {
	s, _, _ := subscriptionFixture(t)
	request := subscriptionInteraction(t, s)
	w := request("90001", "11111", followFilmCommand)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Suivi enregistré") {
		t.Fatalf("follow: %d %s", w.Code, w.Body)
	}
	var reply struct {
		Content string `json:"content"`
		Flags   int    `json:"flags"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || reply.Flags != 64 || !strings.Contains(w.Body.String(), `"parse":[]`) {
		t.Fatalf("non-private reply: %s %v", w.Body, err)
	}
	if strings.Contains(w.Body.String(), "Private requester") {
		t.Fatal("requester's identity escaped")
	}
	_, total, err := s.Store.Subscriptions(context.Background(), "11111", 0)
	if err != nil || total != 1 {
		t.Fatalf("Discord did not persist Hub follow: %d %v", total, err)
	}
	if w = request("90001", "11111", followFilmCommand); w.Code != 409 {
		t.Fatalf("interaction replay accepted: %d %s", w.Code, w.Body)
	}
}

func TestSubscriptionCommandsValidateOptionsAndAccess(t *testing.T) {
	for _, tc := range []struct {
		name, user, data   string
		expired, available bool
		want               int
	}{
		{"owner injection", "11111", `{"name":"suivre","options":[{"name":"type","type":3,"value":"movie"},{"name":"tmdb-id","type":4,"value":12},{"name":"user","type":3,"value":"22222"}]}`, false, false, 403},
		{"film season", "11111", `{"name":"suivre","options":[{"name":"type","type":3,"value":"movie"},{"name":"tmdb-id","type":4,"value":12},{"name":"season","type":4,"value":1}]}`, false, false, 403},
		{"expired during provider read", "11111", followFilmCommand, true, false, 200},
		{"unlinked Discord", "33333", followFilmCommand, false, false, 200},
		{"already available", "11111", followFilmCommand, false, true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, f := subscriptionFixture(t)
			f.available = tc.available
			if tc.expired {
				f.beforeDetail = func() { s.mu.Lock(); delete(s.grants, "11111"); s.mu.Unlock() }
			}
			w := subscriptionInteraction(t, s)("90001", tc.user, tc.data)
			if w.Code != tc.want {
				t.Fatalf("reply: %d %s", w.Code, w.Body)
			}
			_, total, err := s.Store.Subscriptions(context.Background(), "11111", 0)
			if err != nil || total != 0 {
				t.Fatalf("unsafe future follow: %d %v", total, err)
			}
			if tc.available {
				if !strings.Contains(w.Body.String(), "Déjà disponible") || !strings.Contains(w.Body.String(), "https://app.plex.tv") {
					t.Fatalf("availability reply: %s", w.Body)
				}
			} else if strings.Contains(w.Body.String(), "Shared film") {
				t.Fatalf("private title after access failure: %s", w.Body)
			}
		})
	}
}

func TestFollowingCommandsAutomaticallyListOwnSeerrRequests(t *testing.T) {
	for _, tc := range []struct {
		name, user, owner string
		wantTitle         bool
	}{
		{"own requests without saved follows", "11111", "7", true},
		{"another account", "22222", "7", false},
		{"upstream mixes request owners", "11111", "8", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t)
			cookie := seed(t, s, tc.user, true)
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" {
					t.Fatal("automatic following mutated upstream")
				}
				switch r.URL.Path {
				case "/api/v1/user":
					return response(200, `{"pageInfo":{"results":2},"results":[{"id":7,"plexId":11111},{"id":8,"plexId":22222}]}`), nil
				case "/api/v1/user/7/requests":
					if r.URL.Query().Get("skip") != "10" {
						t.Error("request page was not forwarded")
					}
					return response(200, fmt.Sprintf(`{"pageInfo":{"results":11},"results":[{"id":73,"type":"tv","status":2,"requestedBy":{"id":%s},"media":{"tmdbId":12,"status":4},"seasons":[{"seasonNumber":2}]}]}`, tc.owner)), nil
				case "/api/v1/user/8/requests":
					return response(200, `{"pageInfo":{"results":0},"results":[]}`), nil
				case "/api/v1/tv/12":
					return response(200, `{"id":12,"name":"My requested series"}`), nil
				default:
					t.Errorf("unexpected source: %s", r.URL.Path)
					return response(500, `{}`), nil
				}
			})
			w := subscriptionInteraction(t, s)("90001", tc.user, `{"name":"mes-suivis","options":[{"name":"page","type":4,"value":2}]}`)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"flags":64`) || strings.Contains(w.Body.String(), "My requested series") != tc.wantTitle {
				t.Fatalf("automatic list/privacy: %d %s", w.Code, w.Body)
			}
			if tc.wantTitle && (!strings.Contains(w.Body.String(), "Page 2") || !strings.Contains(w.Body.String(), "Partiellement disponible") || !strings.Contains(w.Body.String(), "saisons [2]")) {
				t.Fatalf("request status/seasons missing: %s", w.Body)
			}
			w = call(s, "GET", "/api/v1/requests?offset=10", cookie, "", "")
			if strings.Contains(w.Body.String(), "My requested series") != tc.wantTitle {
				t.Fatalf("Hub and bot disagree on ownership: %s", w.Body)
			}
			_, total, err := s.Store.Subscriptions(context.Background(), tc.user, 0)
			if err != nil || total != 0 {
				t.Fatalf("automatic requests created duplicate follows: %d %v", total, err)
			}
		})
	}
}
