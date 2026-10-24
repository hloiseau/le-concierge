package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/eris"
)

func TestErisRemovalUsesVerifiedTextBeforeAccountOrPrivacyHandoff(t *testing.T) {
	for _, action := range []string{"prepare", "status", "choice"} {
		t.Run(action, func(t *testing.T) {
			s := fixture(t)
			s.Config.BotAPISecret = strings.Repeat("s", 32)
			s.Config.DiscordBotToken = "fixture"
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "discord.com" {
					t.Fatalf("removal reached media service: %s", r.URL.Path)
				}
				if !strings.Contains(r.URL.Path, "/messages/") {
					return response(200, `{"id":"33333","guild_id":"23456"}`), nil
				}
				raw, _ := json.Marshal(map[string]any{"id": "90001", "channel_id": "33333", "timestamp": time.Now(), "content": "<@12345> Donc, supprime Paddington 2", "author": map[string]string{"id": "11111"}, "mentions": []any{map[string]string{"id": "12345"}}})
				return response(200, string(raw)), nil
			})
			in := eris.ToolRequest{Plan: eris.Plan{Action: action, Query: "Paddington 2"}, ChannelID: "33333", MessageID: "90001"}
			if action == "choice" {
				in.Plan, in.Choice = eris.Plan{}, "eris:choose:old"
			}
			w := callErisTool(s, in)
			var reply eris.ToolReply
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Content != eris.RemovalExplanation || len(reply.Buttons) != 0 || len(s.erisPending) != 0 {
				t.Fatalf("removal became a private/account/add flow: %d %s", w.Code, w.Body)
			}
			if replay := callErisTool(s, in); replay.Code != 403 {
				t.Fatal("accepted a replayed Discord proof")
			}
		})
	}
}
