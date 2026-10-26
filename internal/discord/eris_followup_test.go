package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/eris"
)

func publicFollowupFixture() (*discordgo.Message, *discordgo.Message) {
	m := mentionFixture().Message
	m.ID, m.Content, m.Mentions, m.Timestamp = "99999", "Nickel", nil, time.Now().Add(-time.Second)
	prior := &discordgo.Message{ID: "88888", GuildID: m.GuildID, ChannelID: m.ChannelID, Content: "Et toi, ça va ?", Timestamp: m.Timestamp.Add(-time.Second), Author: &discordgo.User{ID: "12345", Bot: true}}
	return m, prior
}

func TestErisContextNeverCrossesNewerReplyOrWindow(t *testing.T) {
	m, anchor := publicFollowupFixture()
	human := &discordgo.Message{ID: "88889", ChannelID: m.ChannelID, Timestamp: anchor.Timestamp.Add(time.Millisecond), Content: "Autre échange", Author: &discordgo.User{ID: "22222"}}
	for _, tc := range []struct {
		name   string
		recent []*discordgo.Message
		valid  bool
	}{
		{"five interleaved", []*discordgo.Message{human, human, human, human, human, anchor}, true},
		{"six interleaved", []*discordgo.Message{human, human, human, human, human, human, anchor}, false},
		{"newer Eris reply", []*discordgo.Message{{ID: "88890", ChannelID: m.ChannelID, Timestamp: human.Timestamp, Author: anchor.Author}, anchor}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, between := ErisContext(m, tc.recent, "12345", anchor.ID, time.Now())
			if (got != nil) != tc.valid || (tc.valid && !strings.Contains(between, "22222: Autre échange")) {
				t.Fatal(got, between)
			}
		})
	}
}

func TestImplicitQuestionRequiresRecentPublicContextAndNoOtherAddressee(t *testing.T) {
	for _, state := range []string{"valid", "another member", "old", "future", "older than bot", "other channel", "other guild", "other author", "private", "webhook", "bot input", "mention", "role", "everyone", "raw mention", "reply elsewhere", "empty", "too long", "system"} {
		t.Run(state, func(t *testing.T) {
			m, prior := publicFollowupFixture()
			switch state {
			case "another member":
				m.Author.ID = "22222"
			case "old":
				prior.Timestamp = time.Now().Add(-3 * time.Minute)
			case "future":
				prior.Timestamp = time.Now().Add(time.Minute)
			case "older than bot":
				m.Timestamp = prior.Timestamp.Add(-time.Second)
			case "other channel":
				prior.ChannelID = "44444"
			case "other guild":
				prior.GuildID = "55555"
			case "other author":
				prior.Author.ID = "55555"
			case "private":
				prior.Flags = discordgo.MessageFlagsEphemeral
			case "webhook":
				prior.WebhookID = "55555"
			case "bot input":
				m.Author.Bot = true
			case "mention":
				m.Mentions = []*discordgo.User{{ID: "22222"}}
			case "role":
				m.MentionRoles = []string{"22222"}
			case "everyone":
				m.MentionEveryone = true
			case "raw mention":
				m.Content = "<@22222> oui"
			case "reply elsewhere":
				m.MessageReference = &discordgo.MessageReference{MessageID: "77777"}
			case "empty":
				m.Content = " "
			case "too long":
				m.Content = strings.Repeat("é", 501)
			case "system":
				m.Type = discordgo.MessageTypeChannelPinnedMessage
			}
			_, ok := ImplicitErisQuestion(m, prior, "12345", time.Now())
			if ok != (state == "valid" || state == "another member") {
				t.Fatal(state, ok)
			}
		})
	}
}

func TestPlainNameMustAddressErisNotDiscussHer(t *testing.T) {
	for text, want := range map[string]bool{"Eris bonjour": true, "Eris, tu vas bien ?": true, "Salut Eris !": true, "Ça va, Eris ?": true, "Eris est un bot": false, "On parlait d'Eris hier": false, "Éris tu connais ce film ?": true} {
		m := mentionFixture().Message
		m.Content, m.Mentions = text, nil
		_, ok := ErisQuestion(m, "12345")
		if ok != want {
			t.Errorf("%q: %v", text, ok)
		}
	}
	for text, want := range map[string]string{"Eris, demande Arrival": "demande Arrival", "prends le deuxième, Eris": "prends le deuxième", "Salut Eris !": "Salut", "Éris tu connais ce film ?": "tu connais ce film ?"} {
		m := mentionFixture().Message
		m.Content, m.Mentions = text, nil
		got, ok := ErisQuestion(m, "12345")
		if !ok || got != want {
			t.Errorf("%q: got %q, want %q", text, got, want)
		}
	}
}

func TestFollowupUsesDeliveredContextAndCurrentAuthorWithoutPing(t *testing.T) {
	for _, mode := range []string{"same member", "interleaved", "another member", "not addressed", "classifier fails", "deleted context", "missed intervening message", "interrupted classification", "interrupted generation", "failed delivery", "disabled intent", "legacy", "expired", "forgotten", "tool"} {
		t.Run(mode, func(t *testing.T) {
			b, _ := signedCommand(t, "eris")
			b.Eris.V2, b.Config.DiscordMessageContent = true, true
			b.Eris.URL, b.Config.BotAPIURL = "http://model.test", "http://api.test"
			var generation, classifications, sends, tools atomic.Int32
			first := mentionFixture()
			b.Eris.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				var payload struct{ Messages []eris.Message }
				_ = json.NewDecoder(r.Body).Decode(&payload)
				raw := `{"action":"chat","text":"Et toi, ça va ?"}`
				if strings.Contains(payload.Messages[0].Content, `"addressed"`) {
					classifications.Add(1)
					if mode == "classifier fails" {
						return botResponse(503, `{}`), nil
					}
					if mode == "interrupted classification" {
						other := mentionFixture()
						other.ID, other.Content = "99998", "<@22222> salut"
						other.Mentions = []*discordgo.User{{ID: "22222"}}
						b.handleMention(context.Background(), nil, other)
					}
					raw = fmt.Sprintf(`{"addressed":%t}`, mode != "not addressed")
				} else if generation.Add(1) > 1 {
					all, _ := json.Marshal(payload.Messages)
					if !strings.Contains(string(all), "Eris (message 88888)") || !strings.Contains(payload.Messages[len(payload.Messages)-1].Content, "Nickel") {
						t.Error("lost public context or actual input")
					}
					if mode == "another member" && !strings.Contains(string(all), "23456:22222:33333") {
						t.Error("borrowed first member identity")
					}
					raw = `{"action":"chat","text":"Tant mieux !"}`
					if mode == "interrupted generation" {
						other := mentionFixture()
						other.ID, other.Content = "99998", "<@22222> salut"
						other.Mentions = []*discordgo.User{{ID: "22222"}}
						b.handleMention(context.Background(), nil, other)
					}
					if mode == "tool" || mode == "interrupted generation" {
						raw = `{"action":"monitoring"}`
					}
				}
				body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": eris.Message{Role: "assistant", Content: raw}}}})
				return botResponse(200, string(body)), nil
			})
			b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				tools.Add(1)
				var proof eris.ToolRequest
				_ = json.NewDecoder(r.Body).Decode(&proof)
				if proof.ContextMessageID != "88888" || proof.MessageID != "99990" || proof.Plan.Action != "monitoring" {
					t.Error("missing independent context proof", proof)
				}
				return botResponse(200, `{"content":"CPU : 10 %"}`), nil
			})
			s, _ := discordgo.New("Bot fixture")
			s.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/typing") {
					return botResponse(204, ""), nil
				}
				if r.Method == "GET" {
					if r.URL.Query().Get("before") != "99990" || r.URL.Query().Get("limit") != "6" {
						t.Error("unbounded/wrong context lookup")
					}
					if mode == "deleted context" {
						return botResponse(200, `[]`), nil
					}
					_, prior := publicFollowupFixture()
					if mode == "missed intervening message" {
						prior.ID = "77777"
					}
					recent := []*discordgo.Message{prior}
					if mode == "interleaved" {
						recent = append([]*discordgo.Message{{ID: "88889", ChannelID: prior.ChannelID, Timestamp: prior.Timestamp.Add(time.Millisecond), Content: "Je reviens", Author: &discordgo.User{ID: "22222"}}}, recent...)
					}
					body, _ := json.Marshal(recent)
					return botResponse(200, string(body)), nil
				}
				sends.Add(1)
				if mode == "failed delivery" {
					return botResponse(400, `{}`), nil
				}
				var body struct {
					Allowed *discordgo.MessageAllowedMentions `json:"allowed_mentions"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body.Allowed == nil || body.Allowed.RepliedUser || len(body.Allowed.Parse) > 0 {
					t.Error("unexpected ping")
				}
				return botResponse(200, `{"id":"88888"}`), nil
			})}
			b.handleMention(context.Background(), s, first)
			b.Wait()
			if mode == "disabled intent" {
				b.Config.DiscordMessageContent = false
			}
			if mode == "legacy" {
				b.Eris.V2 = false
			}
			if mode == "expired" {
				b.erisPublic["33333"].Expires = time.Now().Add(-time.Second)
			}
			if mode == "forgotten" {
				b.clearPublicErisTurns("11111")
			}
			if mode == "interleaved" {
				other := mentionFixture()
				other.ID, other.Content = "88889", "<@22222> Je reviens"
				other.Mentions = []*discordgo.User{{ID: "22222"}}
				b.handleMention(context.Background(), s, other)
			}
			m := mentionFixture()
			m.ID, m.Content, m.Mentions, m.Timestamp = "99990", "Nickel", nil, time.Now()
			if mode == "another member" {
				m.Author.ID = "22222"
			}
			b.handleMention(context.Background(), s, m)
			b.Wait()
			b.handleMention(context.Background(), s, m)
			b.Wait()
			wantReply := mode == "same member" || mode == "interleaved" || mode == "another member" || mode == "tool"
			if (sends.Load() == 2) != wantReply {
				t.Fatalf("sends=%d classifications=%d generations=%d", sends.Load(), classifications.Load(), generation.Load())
			}
			if !wantReply && tools.Load() != 0 {
				t.Fatal("ignored message called tool")
			}
			if mode == "tool" && tools.Load() != 1 {
				t.Fatal("tool not called")
			}
		})
	}
}
