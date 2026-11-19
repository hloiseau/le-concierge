package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/eris"
)

func TestOrder66AddressesErisWithoutASlashOrMention(t *testing.T) {
	for _, question := range []string{"ordre66", "Ordre 66 !", "<@12345> ordre66", "Eris, ordre66"} {
		m := mentionFixture().Message
		m.Content, m.Mentions = question, nil
		if _, ok := ErisQuestion(m, "12345"); !ok {
			t.Errorf("conversational invocation ignored: %q", question)
		}
	}
	for _, text := range []string{"Le film parle de l'ordre66", "ordre660", "ordre66bis"} {
		if order66Invocation(text, "12345") {
			t.Errorf("ordinary conversation triggered roleplay: %q", text)
		}
	}
}

func TestOrder66ContextUsesOnlyPublicVerifiedInterlocutors(t *testing.T) {
	base := func() *discordgo.Message {
		return &discordgo.Message{ID: "44444", GuildID: "23456", ChannelID: "33333", Timestamp: time.Now(), Author: &discordgo.User{ID: "11111", Username: "Hugo"}}
	}
	for name, change := range map[string]func(*discordgo.Message){
		"other channel":  func(m *discordgo.Message) { m.ChannelID = "77777" },
		"old":            func(m *discordgo.Message) { m.Timestamp = time.Now().Add(-11 * time.Minute) },
		"future":         func(m *discordgo.Message) { m.Timestamp = time.Now().Add(time.Minute) },
		"bot":            func(m *discordgo.Message) { m.Author.Bot = true },
		"webhook":        func(m *discordgo.Message) { m.WebhookID = "77777" },
		"private":        func(m *discordgo.Message) { m.Flags = 64 },
		"invalid author": func(m *discordgo.Message) { m.Author.ID = "fake" },
	} {
		t.Run(name, func(t *testing.T) {
			m := base()
			change(m)
			if _, ok := roleplayTurn(m, "33333", "12345"); ok {
				t.Fatal("untrusted context accepted")
			}
		})
	}
	m := base()
	m.Mentions = []*discordgo.User{{ID: "12345", Bot: true}, {ID: "fake"}, {ID: "22222", Username: "Keftcha"}}
	m.MessageReference = &discordgo.MessageReference{MessageID: "55555", ChannelID: "33333", GuildID: "23456"}
	m.ReferencedMessage = &discordgo.Message{ID: "55555", ChannelID: "33333", Author: &discordgo.User{ID: "22222", Username: "Keftcha"}}
	turn, ok := roleplayTurn(m, "33333", "12345")
	if !ok || len(turn.Mentions) != 1 || turn.Mentions[0].ID != "22222" || turn.ReplyTo == nil || turn.ReplyTo.ID != "22222" {
		t.Fatal("direct verified target was lost", turn)
	}
	m.MessageReference.GuildID = "77777"
	if turn, _ = roleplayTurn(m, "33333", "12345"); turn.ReplyTo != nil {
		t.Fatal("cross-guild reference accepted")
	}
}

func TestOrder66TargetsTheInterlocutorAndNeverItsInvoker(t *testing.T) {
	for _, target := range []string{"22222", "11111", "77777"} {
		t.Run(target, func(t *testing.T) {
			b, _ := signedCommand(t, "eris")
			b.Eris.V2 = true
			b.Eris.URL = "http://model.test"
			models, sends, toolCalls := 0, 0, 0
			b.Eris.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				models++
				var payload struct {
					Messages []eris.Message
					Tools    json.RawMessage
				}
				json.NewDecoder(r.Body).Decode(&payload)
				all, _ := json.Marshal(payload.Messages)
				if len(payload.Tools) != 0 || models == 1 && (!strings.Contains(string(all), "Keftcha") || !strings.Contains(string(all), "Hugo")) {
					t.Error("roleplay lost its real interlocutors or exposed external tools")
				}
				answer, _ := json.Marshal(map[string]string{"targetDiscordId": target, "text": "Je te plaque au sol."})
				if models == 2 {
					if !strings.Contains(string(all), "clarification_required") {
						t.Error("unsafe target did not lead to clarification")
					}
					answer = []byte(`{"text":"Qui veux-tu que je vise ?"}`)
				}
				raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": eris.Message{Role: "assistant", Content: string(answer)}}}})
				return botResponse(200, string(raw)), nil
			})
			b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { toolCalls++; return botResponse(500, `{}`), nil })
			s, _ := discordgo.New("Bot fixture")
			s.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == "GET" {
					messages := []*discordgo.Message{{ID: "99998", ChannelID: "33333", Timestamp: time.Now().Add(-time.Second), Content: "<@22222> tu parles trop, Keftcha.", Author: &discordgo.User{ID: "11111", Username: "Hugo"}, Mentions: []*discordgo.User{{ID: "22222", Username: "Keftcha"}}}}
					raw, _ := json.Marshal(messages)
					return botResponse(200, string(raw)), nil
				}
				if strings.HasSuffix(r.URL.Path, "/typing") {
					return botResponse(204, ""), nil
				}
				sends++
				raw, _ := io.ReadAll(r.Body)
				if target == "22222" && !strings.Contains(string(raw), "Je te plaque") {
					t.Error("valid scene was not delivered", string(raw))
				}
				var sent struct {
					Content string `json:"content"`
				}
				json.Unmarshal(raw, &sent)
				if target == "22222" && !strings.HasPrefix(sent.Content, "<@22222> ") {
					t.Error("second-person roleplay still appears addressed to the invoker", sent.Content)
				}
				if target != "22222" && strings.Contains(string(raw), "Je te plaque") {
					t.Error("unverified or self-targeted roleplay was published")
				}
				if target != "22222" && sent.Content != "Qui veux-tu que je vise ?" {
					t.Error("unsafe target became a technical error", sent.Content)
				}
				return botResponse(200, `{"id":"88888"}`), nil
			})}
			m := mentionFixture()
			m.Content, m.Mentions, m.Timestamp = "ordre66", nil, time.Now()
			m.Author.Username = "Hugo"
			b.handleMention(context.Background(), s, m)
			b.Wait()
			wantModels := 1
			if target != "22222" {
				wantModels = 2
			}
			if models != wantModels || sends != 1 || toolCalls != 0 {
				t.Fatal("order66 did not stay in the conversation", models, sends, toolCalls)
			}
		})
	}
}

func TestOrder66WithoutAHumanInterlocutorAsksInsteadOfFailing(t *testing.T) {
	for _, history := range []string{"empty", "author and bot", "expired peer"} {
		t.Run(history, func(t *testing.T) {
			b, _ := signedCommand(t, "eris")
			b.Eris.V2, b.Eris.URL = true, "http://model.test"
			models, reads, sends, tools := 0, 0, 0, 0
			b.Eris.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				models++
				var payload struct {
					Messages []eris.Message
					Tools    json.RawMessage
				}
				json.NewDecoder(r.Body).Decode(&payload)
				var facts struct {
					Result struct {
						Status   string `json:"status"`
						Eligible int    `json:"eligibleTargetCount"`
					} `json:"result"`
				}
				json.Unmarshal([]byte(payload.Messages[len(payload.Messages)-1].Content), &facts)
				if len(payload.Tools) != 0 || facts.Result.Status != "clarification_required" || facts.Result.Eligible != 0 {
					t.Error("model was allowed to invent an interlocutor")
				}
				return botResponse(200, `{"choices":[{"message":{"role":"assistant","content":"{\"text\":\"Qui veux-tu que je vise ?\"}"}}]}`), nil
			})
			b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { tools++; return botResponse(500, `{}`), nil })
			s, _ := discordgo.New("Bot fixture")
			s.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == "GET" {
					reads++
					messages := []*discordgo.Message{}
					if history == "author and bot" {
						messages = []*discordgo.Message{{ID: "99998", ChannelID: "33333", Timestamp: time.Now().Add(-time.Second), Author: &discordgo.User{ID: "11111"}}, {ID: "99997", ChannelID: "33333", Timestamp: time.Now().Add(-2 * time.Second), Author: &discordgo.User{ID: "12345", Bot: true}}}
					} else if history == "expired peer" {
						messages = []*discordgo.Message{{ID: "99998", ChannelID: "33333", Timestamp: time.Now().Add(-11 * time.Minute), Author: &discordgo.User{ID: "22222"}}}
					}
					raw, _ := json.Marshal(messages)
					return botResponse(200, string(raw)), nil
				}
				if strings.HasSuffix(r.URL.Path, "/typing") {
					return botResponse(204, ""), nil
				}
				sends++
				var sent struct {
					Content string `json:"content"`
				}
				json.NewDecoder(r.Body).Decode(&sent)
				if sent.Content != "Qui veux-tu que je vise ?" {
					t.Error("missing interlocutor became a technical error", sent.Content)
				}
				return botResponse(200, `{"id":"88888"}`), nil
			})}
			m := mentionFixture()
			m.Content, m.Timestamp = "<@12345> Ordre 66", time.Now()
			b.handleMention(context.Background(), s, m)
			b.Wait()
			if models != 1 || reads != 1 || sends != 1 || tools != 0 {
				t.Fatal("missing-target handling did not stay in one conversation turn", models, reads, sends, tools)
			}
		})
	}
}
