package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/eris"
)

func TestErisRequestStatusDoesNotRequirePrivateConversation(t *testing.T) {
	for _, mode := range []string{"mention", "followup", "slash"} {
		for _, query := range []string{"Billions", ""} {
			t.Run(mode+"/"+query, func(t *testing.T) {
				s := fixture(t)
				seed(t, s, "11111", true)
				s.Config.BotAPISecret, s.Config.DiscordBotToken = strings.Repeat("s", 32), "fixture"
				s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
					if r.Method != "GET" {
						t.Fatal("status attempted a write")
					}
					switch r.URL.Path {
					case "/api/v10/channels/33333":
						return response(200, `{"id":"33333","guild_id":"23456"}`), nil
					case "/api/v10/channels/33333/messages":
						data := []discordgo.Message{{ID: "88888", ChannelID: "33333", Timestamp: time.Now().Add(-time.Second), Content: "Quel titre ?", Author: &discordgo.User{ID: "12345", Bot: true}}}
						raw, _ := json.Marshal(data)
						return response(200, string(raw)), nil
					case "/api/v10/channels/33333/messages/99999":
						question := "t'as des infos sur la demande pour Billions ?"
						m := discordgo.Message{ID: "99999", ChannelID: "33333", Timestamp: time.Now(), Content: question, Author: &discordgo.User{ID: "11111"}}
						if mode == "mention" {
							m.Content = "<@12345> " + question
							m.Mentions = []*discordgo.User{{ID: "12345"}}
						}
						raw, _ := json.Marshal(m)
						return response(200, string(raw)), nil
					case "/api/v1/user":
						return response(200, `{"pageInfo":{"results":1},"results":[{"id":2,"plexId":11111}]}`), nil
					case "/api/v1/user/2/requests":
						return response(200, `{"pageInfo":{"results":1},"results":[{"id":7,"type":"tv","status":2,"requestedBy":{"id":2,"displayName":"secret-account"},"media":{"tmdbId":22,"status":3},"seasons":[{"seasonNumber":1}]}]}`), nil
					case "/api/v1/tv/22":
						return response(200, `{"id":22,"name":"Billions"}`), nil
					default:
						t.Errorf("unexpected route %s", r.URL.Path)
						return response(500, `{}`), nil
					}
				})
				in := eris.ToolRequest{Plan: eris.Plan{Action: "status", Query: query}, ChannelID: "33333", MessageID: "99999"}
				if mode == "followup" {
					in.ContextMessageID = "88888"
				} else if mode == "slash" {
					in = signedErisTool(t, s, in.Plan, erisSlash)
				}
				w := callErisTool(s, in)
				var reply eris.ToolReply
				if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
					t.Fatal(err)
				}
				if w.Code != 200 || !strings.Contains(reply.Content, "Billions") || !strings.Contains(reply.Content, statusLabel("processing")) || !strings.Contains(reply.Content, "saisons [1]") || strings.Contains(reply.Content, "privé") || strings.Contains(reply.Content, "secret-account") {
					t.Fatalf("request status hidden or unsafe: %d %s", w.Code, w.Body)
				}
			})
		}
	}
}
