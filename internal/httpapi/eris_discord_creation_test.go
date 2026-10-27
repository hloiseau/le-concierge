package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/eris"
)

func pollPlan() eris.Plan {
	return eris.Plan{Action: "poll", Text: "Quel film ?", Options: []string{"Alien", "Arrival"}}
}

func TestPollVerifiesCallerAndPayloadAndDeduplicatesConfirmedOrUncertain(t *testing.T) {
	for _, state := range []string{"success", "uncertain", "refused", "missing intent", "unprovided choice", "wrong guild"} {
		s := fixture(t)
		s.Config.DiscordBotToken = "test-bot"
		posts := 0
		s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host != "discord.com" {
				t.Fatal("community tool reached private media services")
			}
			if r.Method == "GET" {
				guild := "23456"
				if state == "wrong guild" {
					guild = "99999"
				}
				return response(200, fmt.Sprintf(`{"id":"33333","guild_id":%q}`, guild)), nil
			}
			posts++
			var payload struct {
				Nonce    string `json:"nonce"`
				Enforce  bool   `json:"enforce_nonce"`
				Mentions struct {
					Parse []string `json:"parse"`
				} `json:"allowed_mentions"`
				Poll struct {
					Question struct {
						Text string `json:"text"`
					} `json:"question"`
					Answers []struct {
						Media struct {
							Text string `json:"text"`
						} `json:"poll_media"`
					} `json:"answers"`
					Duration int `json:"duration"`
				} `json:"poll"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if !payload.Enforce || len(payload.Nonce) != 24 || len(payload.Mentions.Parse) != 0 || payload.Poll.Duration != 24 || len(payload.Poll.Answers) != 2 {
				t.Fatal("unbounded or non-idempotent poll", payload)
			}
			if state == "uncertain" {
				return nil, errors.New("response lost")
			}
			if state == "refused" {
				return response(403, `{"message":"Missing Permissions"}`), nil
			}
			return response(200, fmt.Sprintf(`{"id":"55555","channel_id":"33333","author":{"id":"12345"},"poll":{"question":{"text":%q},"answers":[{"poll_media":{"text":"Alien"}},{"poll_media":{"text":"Arrival"}}]}}`, payload.Poll.Question.Text)), nil
		})
		caller := erisCallerContext{UserID: "11111", ChannelID: "33333", Question: "Crée un sondage : Quel film ? Alien ou Arrival"}
		if state == "missing intent" {
			caller.Question = "J’aime les sondages Alien ou Arrival"
		}
		if state == "unprovided choice" {
			caller.Question = "Crée un sondage : Alien ou Predator"
		}
		r := s.erisDiscordCreation(context.Background(), caller, pollPlan())
		if state == "missing intent" || state == "unprovided choice" || state == "wrong guild" {
			if posts != 0 {
				t.Fatal("unverified publication", state, posts)
			}
			continue
		}
		if state == "success" && !strings.Contains(r.Content, "/55555") || state == "uncertain" && !strings.Contains(r.Content, "pas confirmer") || state == "refused" && !strings.Contains(r.Content, "refusé") {
			t.Fatal(state, r)
		}
		if state != "refused" {
			s.erisDiscordCreation(context.Background(), caller, pollPlan())
			if posts != 1 {
				t.Fatal("duplicate after repeat", state, posts)
			}
		}
	}
}

func TestEventUsesVerifiedChannelAndKnownFutureTimesAndExistingEvent(t *testing.T) {
	start := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	end := start.Add(2 * time.Hour)
	for _, state := range []string{"create", "existing", "past", "private", "no everyone view"} {
		s := fixture(t)
		s.Config.DiscordBotToken = "test-bot"
		p := eris.Plan{Action: "event", Text: "Soirée Alien", StartAt: start.Format(time.RFC3339), EndAt: end.Format(time.RFC3339)}
		if state == "past" {
			p.StartAt = time.Now().Add(-time.Hour).Format(time.RFC3339)
			p.EndAt = time.Now().Format(time.RFC3339)
		}
		posts := 0
		event := map[string]any{"id": "66666", "guild_id": "23456", "creator_id": "12345", "name": p.Text, "entity_type": 3, "scheduled_start_time": p.StartAt, "scheduled_end_time": p.EndAt, "entity_metadata": map[string]string{"location": "https://discord.com/channels/23456/33333"}}
		s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/api/v10/guilds/23456/roles" {
				permissions := "1024"
				if state == "no everyone view" {
					permissions = "0"
				}
				return response(200, `[{"id":"23456","permissions":"`+permissions+`"}]`), nil
			}
			if r.URL.Path == "/api/v10/channels/33333" {
				if state == "private" {
					return response(200, `{"id":"33333","guild_id":"23456","permission_overwrites":[{"id":"23456","type":0,"deny":"1024","allow":"0"}]}`), nil
				}
				return response(200, `{"id":"33333","guild_id":"23456"}`), nil
			}
			if r.URL.Path != "/api/v10/guilds/23456/scheduled-events" {
				t.Fatal("wrong event target", r.URL)
			}
			if r.Method == "GET" {
				if state == "existing" {
					raw, _ := json.Marshal([]any{event})
					return response(200, string(raw)), nil
				}
				return response(200, `[]`), nil
			}
			posts++
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["entity_type"] != float64(3) || payload["privacy_level"] != float64(2) {
				t.Fatal(payload)
			}
			raw, _ := json.Marshal(event)
			return response(200, string(raw)), nil
		})
		r := s.erisDiscordCreation(context.Background(), erisCallerContext{UserID: "11111", ChannelID: "33333", Question: "Programme une soirée Soirée Alien"}, p)
		if state == "create" && (posts != 1 || !strings.Contains(r.Content, "/66666") || !strings.Contains(r.Content, "Europe/Paris")) {
			t.Fatal(state, r, posts)
		}
		if state != "create" && posts != 0 {
			t.Fatal("unexpected duplicate/past/private event", state, r, posts)
		}
		if state == "existing" && !strings.Contains(r.Content, "existe déjà") {
			t.Fatal(r)
		}
	}
}

func TestPollSignedProofAllowsCommunityActionWithoutPlexAccount(t *testing.T) {
	s := fixture(t)
	s.Config.BotAPISecret = strings.Repeat("s", 32)
	s.Config.DiscordBotToken = "test-bot"
	posts := 0
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "discord.com" {
			t.Fatal("unrelated private service read")
		}
		if r.Method == "GET" {
			return response(200, `{"id":"33333","guild_id":"23456"}`), nil
		}
		posts++
		return response(200, `{"id":"55555","channel_id":"33333","author":{"id":"12345"},"poll":{"question":{"text":"Quel film ?"},"answers":[{"poll_media":{"text":"Alien"}},{"poll_media":{"text":"Arrival"}}]}}`), nil
	})
	body := `{"id":"99001","application_id":"12345","guild_id":"23456","channel_id":"33333","type":2,"member":{"user":{"id":"11111"}},"data":{"name":"eris","options":[{"name":"message","type":3,"value":"Crée un sondage : Quel film ? Alien ou Arrival"}]}}`
	w := callErisTool(s, signedErisTool(t, s, pollPlan(), body))
	if w.Code != 200 || posts != 1 || !strings.Contains(w.Body.String(), "/55555") {
		t.Fatal(w.Code, posts, w.Body.String())
	}
}
