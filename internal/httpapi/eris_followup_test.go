package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/eris"
)

func TestErisAPIIndependentlyVerifiesImplicitContext(t *testing.T) {
	for _, state := range []string{"valid", "interleaved", "another member", "missing proof", "malformed proof", "nonadjacent", "deleted", "null", "other author", "other channel", "private", "webhook", "old", "current before context", "hidden content", "reply elsewhere", "mention elsewhere", "mixed direct proof"} {
		t.Run(state, func(t *testing.T) {
			s := fixture(t)
			s.Config.DiscordBotToken = "fixture"
			in := eris.ToolRequest{ChannelID: "33333", MessageID: "99999", ContextMessageID: "88888"}
			m := discordgo.Message{ID: in.MessageID, ChannelID: in.ChannelID, Timestamp: time.Now().Add(-time.Second), Content: "Le deuxième", Author: &discordgo.User{ID: "11111"}}
			previous := &discordgo.Message{ID: in.ContextMessageID, ChannelID: in.ChannelID, Timestamp: time.Now().Add(-2 * time.Second), Content: "Quel film préfères-tu ?", Author: &discordgo.User{ID: "12345", Bot: true}}
			switch state {
			case "another member":
				m.Author.ID = "22222"
			case "missing proof":
				in.ContextMessageID = ""
			case "malformed proof":
				in.ContextMessageID = "../88888"
			case "nonadjacent":
				previous.ID = "77777"
			case "other author":
				previous.Author.ID = "44444"
			case "other channel":
				previous.ChannelID = "44444"
			case "private":
				previous.Flags = discordgo.MessageFlagsEphemeral
			case "webhook":
				previous.WebhookID = "55555"
			case "old":
				previous.Timestamp = time.Now().Add(-3 * time.Minute)
			case "current before context":
				m.Timestamp = previous.Timestamp.Add(-time.Second)
			case "hidden content":
				previous.Content = ""
			case "reply elsewhere":
				m.Type, m.MessageReference = discordgo.MessageTypeReply, &discordgo.MessageReference{MessageID: "77777", ChannelID: m.ChannelID}
			case "mention elsewhere":
				m.Mentions = []*discordgo.User{{ID: "44444"}}
			case "mixed direct proof":
				m.Content = "Eris, le deuxième"
			}
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.URL.Host != "discord.com" {
					t.Fatal("unexpected upstream")
				}
				var data any
				switch r.URL.Path {
				case "/api/v10/channels/33333":
					data = discordgo.Channel{ID: "33333", GuildID: "23456"}
				case "/api/v10/channels/33333/messages/99999":
					data = m
				case "/api/v10/channels/33333/messages":
					if r.URL.Query().Get("before") != m.ID || r.URL.Query().Get("limit") != "6" {
						t.Fatal("adjacency not checked")
					}
					data = []*discordgo.Message{previous}
					if state == "interleaved" {
						data = []*discordgo.Message{{ID: "88889", ChannelID: m.ChannelID, Timestamp: previous.Timestamp.Add(time.Millisecond), Content: "Je reviens", Author: &discordgo.User{ID: "22222"}}, previous}
					}
					if state == "deleted" {
						data = []*discordgo.Message{}
					} else if state == "null" {
						data = []*discordgo.Message{nil}
					}
				default:
					t.Fatal("unexpected route", r.URL.Path)
				}
				raw, _ := json.Marshal(data)
				return response(200, string(raw)), nil
			})
			caller, err := s.erisCaller(context.Background(), in)
			if state == "valid" || state == "interleaved" || state == "another member" {
				if err != nil || caller.Private || caller.UserID != m.Author.ID || caller.Reference != previous.Content || caller.Question != m.Content {
					t.Fatal(caller, err)
				}
				if _, err := s.erisCaller(context.Background(), in); err == nil {
					t.Fatal("replayed context accepted")
				}
			} else if err == nil {
				t.Fatal("invalid context accepted")
			}
		})
	}
}

func TestErisImplicitWatchFiltersStayPrivate(t *testing.T) {
	for _, plan := range []eris.Plan{{Action: "recommend", Unwatched: true}} {
		s := fixture(t)
		s.Config.BotAPISecret, s.Config.DiscordBotToken = strings.Repeat("s", 32), "fixture"
		s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host != "discord.com" || r.Method != "GET" {
				t.Fatal("public personal follow-up accessed media")
			}
			var data any = discordgo.Message{ID: "99999", ChannelID: "33333", Timestamp: time.Now(), Content: "Et les films que je n'ai pas vus ?", Author: &discordgo.User{ID: "11111"}}
			if r.URL.Path == "/api/v10/channels/33333" {
				data = discordgo.Channel{ID: "33333", GuildID: "23456"}
			} else if strings.HasSuffix(r.URL.Path, "/messages") {
				data = []discordgo.Message{{ID: "88888", ChannelID: "33333", Timestamp: time.Now().Add(-time.Second), Content: "Qu'est-ce que tu veux voir ?", Author: &discordgo.User{ID: "12345", Bot: true}}}
			}
			raw, _ := json.Marshal(data)
			return response(200, string(raw)), nil
		})
		w := callErisTool(s, eris.ToolRequest{Plan: plan, ChannelID: "33333", MessageID: "99999", ContextMessageID: "88888"})
		if w.Code != 200 || !strings.Contains(w.Body.String(), "en privé") {
			t.Fatalf("privacy boundary %d %s", w.Code, w.Body)
		}
	}
}
