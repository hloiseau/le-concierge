package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/eris"
)

func participationMemory(t *testing.T, b *Bot) {
	t.Helper()
	m, err := eris.OpenMemory(filepath.Join(t.TempDir(), "eris.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	b.Eris.Memory = m
	b.Eris.V2, b.Config.DiscordMessageContent = true, true
}

func TestErisChannelCommandRequiresSignedAdministratorAndPersists(t *testing.T) {
	for _, tc := range []struct {
		permissions, action string
		want, invalid       bool
	}{
		{"8", "activer", true, false}, {"0", "activer", false, false}, {"16", "activer", false, false}, {"bad", "activer", false, false}, {"8", "desactiver", false, false}, {"8", "etat", false, false}, {"8", "unknown", false, true},
	} {
		t.Run(tc.permissions+tc.action, func(t *testing.T) {
			body := fmt.Sprintf(`{"id":"99999","application_id":"12345","guild_id":"23456","channel_id":"33333","type":2,"token":"private-interaction-token","member":{"permissions":%q,"user":{"id":"11111"}},"data":{"name":"eris-salon","options":[{"name":"participation","type":3,"value":%q}]}}`, tc.permissions, tc.action)
			b, r := signedInteraction(t, body)
			participationMemory(t, b)
			if tc.action == "desactiver" {
				b.Eris.Memory.SetParticipation("23456", "33333", true)
			}
			w := httptest.NewRecorder()
			b.Handler().ServeHTTP(w, r)
			if tc.invalid {
				if w.Code != 403 {
					t.Fatal(w.Code)
				}
				return
			}
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"flags":64`) {
				t.Fatal(w.Code, w.Body)
			}
			if got, err := b.Eris.Memory.Participation("23456", "33333"); err != nil || got != tc.want {
				t.Fatal(got, err, w.Body)
			}
		})
	}
}

func TestSpontaneousParticipationBudgetSilenceAndNoTools(t *testing.T) {
	for _, mode := range []string{"reply", "silence", "error", "quiet", "disabled", "intent disabled", "ordinary", "banter", "mention elsewhere", "private", "bot", "stale", "disabled during call", "new message during call"} {
		t.Run(mode, func(t *testing.T) {
			b, _ := signedCommand(t, "eris")
			participationMemory(t, b)
			b.Eris.URL = "http://model.test"
			if mode == "quiet" {
				b.Eris.HTTP.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
					return botResponse(200, `{"choices":[{"message":{"role":"assistant","content":"{\"action\":\"chat\",\"text\":\"Bonjour.\"}"}}]}`), nil
				})
				owner := "23456:11111:33333"
				_, err := b.Eris.ReplyWithDiscordContext(context.Background(), owner, "Bonjour", false, "", eris.DiscordInput{MessageID: "99990", Speaker: eris.Speaker{ID: "11111"}}, nil)
				if err != nil || !b.Eris.SkipPublicFollowup(owner, "tu peux te taire", false) {
					t.Fatal("could not enter quiet state", err)
				}
			}
			if mode != "disabled" {
				if err := b.Eris.Memory.SetParticipation("23456", "33333", true); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "intent disabled" {
				b.Config.DiscordMessageContent = false
			}
			calls, sends := 0, 0
			b.Eris.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if mode == "disabled during call" {
					b.Eris.Memory.SetParticipation("23456", "33333", false)
				}
				if mode == "new message during call" {
					b.invalidateErisParticipation("33333", "77777")
				}
				if mode == "error" {
					return botResponse(503, `{}`), nil
				}
				text := "Ça ressemble à Arrival."
				if mode == "silence" {
					text = ""
				}
				content, _ := json.Marshal(map[string]string{"text": text})
				raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": eris.Message{Role: "assistant", Content: string(content)}}}})
				return botResponse(200, string(raw)), nil
			})
			b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
				t.Error("spontaneous tool call")
				return botResponse(500, `{}`), nil
			})
			s, _ := discordgo.New("Bot fixture")
			s.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				sends++
				if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/messages") {
					t.Error("unexpected Discord call")
				}
				var msg discordgo.MessageSend
				json.NewDecoder(r.Body).Decode(&msg)
				if msg.AllowedMentions == nil || msg.AllowedMentions.RepliedUser || len(msg.AllowedMentions.Parse) > 0 || len(msg.Components) != 0 {
					t.Error("unexpected pings or actions")
				}
				return botResponse(200, `{"id":"88888"}`), nil
			})}
			m := mentionFixture()
			m.Content, m.Mentions, m.Timestamp = "Je cherche le nom du film avec la linguiste et les aliens", nil, time.Now()
			switch mode {
			case "ordinary", "banter":
				m.Content = "Mon chat vient encore de débrancher ma souris"
			case "mention elsewhere":
				m.Mentions = []*discordgo.User{{ID: "22222"}}
			case "private":
				m.Flags = discordgo.MessageFlagsEphemeral
			case "bot":
				m.Author.Bot = true
			case "stale":
				m.Timestamp = time.Now().Add(-10 * time.Minute)
			}
			count := 1
			if mode == "banter" {
				count = 8
			}
			for n := 0; n < count; n++ {
				m.ID = fmt.Sprint(99000 + n)
				if mode == "banter" {
					m.Author.ID = fmt.Sprint(11111 + n%2)
				}
				b.handleMention(context.Background(), s, m)
				b.Wait()
			}
			wantCall := mode == "reply" || mode == "silence" || mode == "error" || mode == "banter" || mode == "disabled during call" || mode == "new message during call"
			wantReply := mode == "reply" || mode == "banter"
			if (calls == 1) != wantCall || (sends == 1) != wantReply {
				t.Fatalf("calls=%d sends=%d", calls, sends)
			}
			// Simulate more discussion, outside the active conversation, without
			// waiting: a reply stays awaiting engagement even after its budget expires.
			delete(b.erisPublic, "33333")
			if state := b.erisAmbient["33333"]; state != nil && wantReply {
				state.NextCheck = time.Now().Add(-time.Second)
			}
			m.ID = "99998"
			b.handleMention(context.Background(), s, m)
			b.Wait()
			if wantCall && calls != 1 {
				t.Fatal("repeated unsolicited inference", calls)
			}
		})
	}
}
