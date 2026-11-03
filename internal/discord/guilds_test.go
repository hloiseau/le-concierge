package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/eris"
)

func TestChatGuildRejectsMediaCommandsAndComponents(t *testing.T) {
	for _, name := range []string{"mes-demandes", "nouveautes", "surprise", "recap", "livres", "diagnostic", "salons"} {
		t.Run(name, func(t *testing.T) {
			body := fmt.Sprintf(`{"id":"99999","application_id":"12345","guild_id":"34567","type":2,"token":"interaction-token","member":{"user":{"id":"11111"}},"data":{"name":%q}}`, name)
			b, r := signedInteraction(t, body)
			b.Config.DiscordChatGuildIDs = []string{"34567"}
			b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
				t.Error("forbidden command reached a remote service")
				return botResponse(500, "{}"), nil
			})
			w := httptest.NewRecorder()
			b.Handler().ServeHTTP(w, r)
			b.Wait()
			if w.Code != 403 {
				t.Fatal(name, w.Code)
			}
		})
	}
	body := `{"id":"99999","application_id":"12345","guild_id":"34567","channel_id":"33333","type":3,"token":"interaction-token","member":{"user":{"id":"11111"}},"message":{"flags":0,"author":{"id":"12345"}},"data":{"custom_id":"eris:confirm:` + strings.Repeat("a", 64) + `","component_type":2}}`
	b, r := signedInteraction(t, body)
	b.Config.DiscordChatGuildIDs = []string{"34567"}
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("old media button accepted", w.Code)
	}
}

func TestChatGuildHelpAndUsageHideHub(t *testing.T) {
	for _, command := range []string{"aide", "eris", "eris-salon"} {
		for _, guild := range []string{"34567", "45678", ""} {
			body := fmt.Sprintf(`{"id":"99999","application_id":"12345","guild_id":%q,"channel_id":"33333","type":2,"token":"interaction-token","member":{"permissions":"8","user":{"id":"11111"}},"data":{"name":%q}}`, guild, command)
			b, r := signedInteraction(t, body)
			b.Config.DiscordChatGuildIDs = []string{"34567"}
			w := httptest.NewRecorder()
			b.Handler().ServeHTTP(w, r)
			b.Wait()
			if guild != "34567" {
				if w.Code != 403 {
					t.Fatal("unconfigured guild accepted", guild, w.Code)
				}
				continue
			}
			if w.Code != 200 {
				t.Fatal(command, w.Code, w.Body)
			}
			for _, hidden := range []string{"hub.example", "/mes-demandes", "/nouveautes", "/salons", "/compte", "/livres"} {
				if strings.Contains(w.Body.String(), hidden) {
					t.Fatal("help exposed media capability", hidden, w.Body)
				}
			}
		}
	}
}

func TestChatGuildSignedConversationCannotInvokeTools(t *testing.T) {
	for _, v2 := range []bool{false, true} {
		body := `{"id":"99999","application_id":"12345","guild_id":"34567","type":2,"token":"interaction-token","member":{"user":{"id":"11111"}},"data":{"name":"eris","options":[{"name":"message","type":3,"value":"Demande Bronson"}]}}`
		b, r := signedInteraction(t, body)
		b.Config.DiscordChatGuildIDs = []string{"34567"}
		b.Eris.URL, b.Eris.V2 = "http://model.test/v1", v2
		var stages []string
		b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.URL.Host == "model.test":
				stages = append(stages, "model")
				raw, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(raw), "discussion_seule") || strings.Contains(string(raw), "<actions>") {
					t.Error("model received media tool instructions")
				}
				// A model ignoring the restricted prompt still cannot run an action.
				reply, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": `{"action":"prepare","query":"Bronson","kind":"movie"}`}}}})
				return botResponse(200, string(reply)), nil
			case strings.HasSuffix(r.URL.Path, "/callback"):
				stages = append(stages, "ack")
				return botResponse(204, ""), nil
			case strings.HasSuffix(r.URL.Path, "/messages/@original"):
				stages = append(stages, "reply")
				raw, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(raw), eris.ChatOnlyExplanation) || !strings.Contains(string(raw), `"flags":64`) {
					t.Error("missing private capability refusal", string(raw))
				}
				return botResponse(200, "{}"), nil
			default:
				t.Error("chat-only guild called an external tool", r.URL.Path)
				return botResponse(500, "{}"), nil
			}
		})
		b.Eris.HTTP.Transport = b.Client.Transport
		w := httptest.NewRecorder()
		b.Handler().ServeHTTP(w, r)
		b.Wait()
		if w.Code != 202 || strings.Join(stages, ",") != "ack,model,reply" {
			t.Fatal(v2, w.Code, stages)
		}
	}
}

func TestChatGuildGatewayRepliesWithoutHub(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	b.Config.DiscordChatGuildIDs = []string{"34567"}
	b.Eris.URL = "http://model.test/v1"
	calls := 0
	b.Eris.HTTP.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return botResponse(200, `{"choices":[{"message":{"role":"assistant","content":"{\"action\":\"chat\",\"text\":\"Salut, deuxième serveur.\"}"}}]}`), nil
	})
	s, _ := discordgo.New("Bot fixture-token")
	sent := 0
	s.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/typing") {
			return botResponse(204, ""), nil
		}
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/messages") {
			sent++
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), "Salut, deuxième serveur.") {
				t.Error(string(raw))
			}
			return botResponse(200, `{"id":"88888","channel_id":"33333"}`), nil
		}
		t.Error("unexpected Discord call", r.Method, r.URL.Path)
		return botResponse(500, "{}"), nil
	})}
	b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("public chat contacted Hub")
		return botResponse(500, "{}"), nil
	})
	for _, guild := range []string{"45678", "", "34567"} {
		m := mentionFixture()
		m.GuildID = guild
		b.handleMention(context.Background(), s, m)
		b.Wait()
	}
	if calls != 1 || sent != 1 {
		t.Fatal(calls, sent)
	}
}

func TestGuildRegistrationUsesSeparateCommandCatalogs(t *testing.T) {
	for _, failExtra := range []bool{false, true} {
		b, _ := signedCommand(t, "eris")
		b.Config.DiscordBotToken = "fixture-token"
		b.Config.DiscordChatGuildIDs = []string{"34567", "45678"}
		seen := map[string][]string{}
		b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			var commands []struct{ Name string }
			if r.Method != "PUT" || json.NewDecoder(r.Body).Decode(&commands) != nil {
				t.Error("invalid registration")
			}
			for _, command := range commands {
				seen[r.URL.Path] = append(seen[r.URL.Path], command.Name)
			}
			if failExtra && strings.Contains(r.URL.Path, "/34567/") {
				return botResponse(403, "{}"), nil
			}
			return botResponse(200, "[]"), nil
		})
		err := b.Register(context.Background())
		if (err != nil) != failExtra || len(seen) != 3 {
			t.Fatal("registration failed to attempt every guild", err, seen)
		}
		for _, guild := range []string{"34567", "45678"} {
			if !reflect.DeepEqual(seen["/api/v10/applications/12345/guilds/"+guild+"/commands"], []string{"eris-salon", "eris", "aide"}) {
				t.Fatal("media commands registered in chat guild", seen)
			}
		}
		if !strings.Contains(strings.Join(seen["/api/v10/applications/12345/guilds/23456/commands"], ","), "mes-demandes") {
			t.Fatal("primary guild lost media commands")
		}
	}
}

func TestForgetPublicTurnsStaysWithinGuild(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	b.erisPublic = map[string]*erisPublicTurn{
		"33333": {GuildID: "23456", AuthorID: "11111"},
		"44444": {GuildID: "34567", AuthorID: "11111"},
	}
	b.erisAmbient = map[string]*erisAmbientTurn{
		"33333": {GuildID: "23456", Messages: []erisAmbientMessage{{Author: "11111"}}},
		"44444": {GuildID: "34567", Messages: []erisAmbientMessage{{Author: "11111"}}},
	}
	b.clearPublicErisGuildTurns("34567", "11111")
	if b.erisPublic["33333"] == nil || b.erisAmbient["33333"] == nil || b.erisPublic["44444"] != nil || b.erisAmbient["44444"] != nil {
		t.Fatal("forget crossed guild boundary")
	}
}
