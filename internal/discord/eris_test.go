package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErisSignedChatStaysPrivateAndDoesNotReadMediaAPI(t *testing.T) {
	b, r := signedInteraction(t, `{"id":"99999","application_id":"12345","guild_id":"23456","type":2,"token":"private-interaction-token","member":{"user":{"id":"11111"}},"data":{"name":"eris","options":[{"name":"message","type":3,"value":"Bonjour"}]}}`)
	b.Eris.URL, b.Eris.Model, b.Eris.Key = "http://model.test/v1", "eris-lfm2.5-v1", "private-key"
	var stages []string
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/callback"):
			stages = append(stages, "ack")
			return botResponse(204, ""), nil
		case r.URL.Host == "model.test":
			stages = append(stages, "model")
			if r.Header.Get("Authorization") != "Bearer private-key" {
				t.Error("wrong model credential")
			}
			return botResponse(200, `{"choices":[{"message":{"role":"assistant","content":"Bonjour @everyone."}}]}`), nil
		case strings.HasSuffix(r.URL.Path, "/messages/@original"):
			stages = append(stages, "reply")
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"flags":64`) || !strings.Contains(string(raw), `"parse":[]`) || !strings.Contains(string(raw), "Bonjour @everyone.") {
				t.Error("chat was public or allowed mentions")
			}
			return botResponse(200, `{}`), nil
		default:
			t.Error("chat accessed a media API or unexpected endpoint")
			return botResponse(500, `{}`), nil
		}
	})
	b.Eris.HTTP.Transport = b.Client.Transport
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, r)
	b.Wait()
	if w.Code != 202 || strings.Join(stages, ",") != "ack,model,reply" {
		t.Fatalf("chat flow: %d %v", w.Code, stages)
	}
}

func TestErisOptionsCannotRequestPublicReplyOrImpersonation(t *testing.T) {
	for _, tc := range []struct {
		options string
		valid   bool
	}{
		{`[{"name":"message","type":3,"value":"Salut"}]`, true},
		{`[{"name":"oublier","type":5,"value":true}]`, true},
		{`[{"name":"message","type":3,"value":"Salut"},{"name":"oublier","type":5,"value":true}]`, true},
		{`[]`, true}, {`[{"name":"oublier","type":5,"value":false}]`, true},
		{`[{"name":"message","type":3,"value":null}]`, false},
		{`[{"name":"message","type":3,"value":" "}]`, false},
		{`[{"name":"message","type":3,"value":"Salut"},{"name":"public","type":5,"value":true}]`, false},
		{`[{"name":"message","type":3,"value":"Salut"},{"name":"user","type":3,"value":"22222"}]`, false},
	} {
		var interaction Interaction
		_ = json.Unmarshal([]byte(`{"type":2,"data":{"name":"eris","options":`+tc.options+`}}`), &interaction)
		if _, err := CommandOptions(interaction); (err == nil) != tc.valid {
			t.Fatalf("wrong option validation: %s %v", tc.options, err)
		}
	}
}

func TestErisUsageWorksWithoutTheModelOrMediaAPI(t *testing.T) {
	b, r := signedCommand(t, "eris")
	b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("Eris usage must work without a remote service")
		return botResponse(500, `{}`), nil
	})
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"flags":64`) || !strings.Contains(w.Body.String(), "/eris message:") {
		t.Fatalf("missing private usage: %d %s", w.Code, w.Body)
	}
}

func TestIdentitySyncIsIdempotentAndOnlyChangesThisBot(t *testing.T) {
	for _, current := range []bool{false, true} {
		b, _ := signedCommand(t, "aide")
		b.Config.DiscordBotToken, b.Config.DiscordBotName = "private-token", "Eris"
		patches := 0
		b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") != "Bot private-token" {
				t.Error("wrong bot credential")
			}
			if r.Method == "PATCH" {
				patches++
				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)
				if len(body) != 1 || ((r.URL.Path != "/api/v10/users/@me" || body["username"] != "Eris") && (r.URL.Path != "/api/v10/guilds/23456/members/@me" || body["nick"] != "Eris")) {
					t.Error("identity changed unrelated fields")
				}
				return botResponse(200, `{}`), nil
			}
			if r.URL.Path == "/api/v10/users/@me" {
				name := "Le Concierge"
				if current {
					name = "Eris"
				}
				return botResponse(200, `{"id":"12345","username":"`+name+`","bot":true}`), nil
			}
			if r.URL.Path != "/api/v10/guilds/23456/members/12345" {
				t.Error("read another member")
			}
			if current {
				return botResponse(200, `{"nick":"Eris"}`), nil
			}
			return botResponse(200, `{"nick":null}`), nil
		})
		if err := b.SyncIdentity(context.Background()); err != nil || (current && patches != 0) || (!current && patches != 2) {
			t.Fatalf("identity result: %v %d", err, patches)
		}
	}
}

func TestIdentityRefusesAnotherBotToken(t *testing.T) {
	b, _ := signedCommand(t, "aide")
	b.Config.DiscordBotToken, b.Config.DiscordBotName = "private-token", "Eris"
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Path != "/api/v10/users/@me" {
			t.Error("updated another bot")
		}
		return botResponse(200, `{"id":"55555","username":"Other","bot":true}`), nil
	})
	if b.SyncIdentity(context.Background()) == nil {
		t.Fatal("wrong bot token accepted")
	}
}
