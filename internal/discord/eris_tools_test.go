package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/eris"
)

func TestErisToolForwardsSignedProofAndDisplaysPrivateChoices(t *testing.T) {
	body := `{"id":"99999","application_id":"12345","guild_id":"23456","type":2,"token":"private-interaction-token","member":{"user":{"id":"11111"}},"data":{"name":"eris","options":[{"name":"message","type":3,"value":"Demande Arrival"}]}}`
	b, r := signedInteraction(t, body)
	b.Eris.URL = "http://model.test/v1"
	b.Config.BotAPIURL = "http://api.test"
	b.Config.BotAPISecret = "service-secret"
	stages := []string{}
	b.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/callback"):
			stages = append(stages, "ack")
			return botResponse(204, ""), nil
		case req.URL.Host == "model.test":
			stages = append(stages, "model")
			return botResponse(200, `{"choices":[{"message":{"role":"assistant","content":"{\"action\":\"prepare\",\"query\":\"Arrival\",\"kind\":\"movie\"}"}}]}`), nil
		case req.URL.Host == "api.test":
			stages = append(stages, "api")
			var in eris.ToolRequest
			_ = json.NewDecoder(req.Body).Decode(&in)
			if req.URL.Path != "/internal/eris" || req.Header.Get("Authorization") != "Bearer service-secret" || string(in.Interaction) != body || in.Timestamp != r.Header.Get("X-Signature-Timestamp") || in.Signature != r.Header.Get("X-Signature-Ed25519") || in.Plan.Action != "prepare" || in.MessageID != "" {
				t.Error("lost transport proof")
			}
			return botResponse(200, `{"content":"Choisis le titre.","buttons":[{"id":"eris:choose:`+strings.Repeat("a", 64)+`","label":"Choisir 1"}]}`), nil
		case strings.HasSuffix(req.URL.Path, "/messages/@original"):
			stages = append(stages, "reply")
			raw, _ := io.ReadAll(req.Body)
			if !strings.Contains(string(raw), `"flags":64`) || !strings.Contains(string(raw), `"parse":[]`) || !strings.Contains(string(raw), "Choisir 1") {
				t.Error("choices missing or not private")
			}
			return botResponse(200, `{}`), nil
		default:
			t.Error("unexpected endpoint")
			return botResponse(500, `{}`), nil
		}
	})
	b.Eris.HTTP.Transport = b.Client.Transport
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, r)
	b.Wait()
	if w.Code != 202 || strings.Join(stages, ",") != "ack,model,api,reply" {
		t.Fatal(w.Code, stages)
	}
}

func TestErisConfirmationTransportFailureDoesNotInviteBlindRetry(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	b.Config.BotAPIURL = "http://api.test"
	calls := 0
	b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { calls++; return botResponse(502, `{}`), nil })
	result := b.erisTool(context.Background(), eris.ToolRequest{Interaction: json.RawMessage(`{"data":{"custom_id":"eris:confirm:` + strings.Repeat("a", 64) + `"}}`)})
	if calls != 1 || !strings.Contains(result.Content, "Vérifie /mes-demandes") || len(result.Buttons) > 0 {
		t.Fatal("unsafe retry after an ambiguous confirmation")
	}
}

func TestErisPublicHandoffCreatesNewEphemeralReply(t *testing.T) {
	body := `{"id":"99999","application_id":"12345","guild_id":"23456","type":3,"token":"interaction-token","member":{"user":{"id":"11111"}},"message":{"flags":0,"author":{"id":"12345"}},"data":{"custom_id":"eris:open:` + strings.Repeat("a", 64) + `","component_type":2}}`
	b, r := signedInteraction(t, body)
	b.Config.BotAPIURL = "http://api.test"
	calls := 0
	b.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(req.Body)
		switch {
		case strings.HasSuffix(req.URL.Path, "/callback"):
			if string(raw) != `{"type":5,"data":{"flags":64}}` {
				t.Error("handoff would edit public message", string(raw))
			}
		case req.URL.Host == "api.test":
			return botResponse(200, `{"content":"Choisis Hot Fuzz.","buttons":[{"id":"eris:choose:`+strings.Repeat("b", 64)+`","label":"Choisir"}]}`), nil
		case strings.HasSuffix(req.URL.Path, "/messages/@original"):
			if !strings.Contains(string(raw), `"flags":64`) || !strings.Contains(string(raw), "eris:choose:") {
				t.Error("private choices missing")
			}
		default:
			t.Error("unexpected model or message call", req.URL.Path)
		}
		return botResponse(204, ""), nil
	})
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, r)
	b.Wait()
	if w.Code != 202 || calls != 3 {
		t.Fatal(w.Code, calls)
	}
}

func TestErisConfirmationBypassesModelAndRejectsPublicButtons(t *testing.T) {
	for _, private := range []bool{true, false} {
		flags := 0
		if private {
			flags = 64
		}
		body, _ := json.Marshal(map[string]any{"id": "99999", "application_id": "12345", "guild_id": "23456", "type": 3, "token": "interaction-token", "member": map[string]any{"user": map[string]string{"id": "11111"}}, "message": map[string]any{"flags": flags, "author": map[string]string{"id": "12345"}}, "data": map[string]any{"custom_id": "eris:confirm:" + strings.Repeat("a", 64), "component_type": 2}})
		b, r := signedInteraction(t, string(body))
		b.Config.BotAPIURL = "http://api.test"
		b.Eris.URL = "http://model.test"
		calls := 0
		b.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if req.URL.Host == "model.test" {
				t.Error("model participated in confirmation")
			}
			if req.URL.Host == "api.test" {
				return botResponse(200, `{"content":"Demande créée."}`), nil
			}
			return botResponse(204, ""), nil
		})
		b.Eris.HTTP.Transport = b.Client.Transport
		w := httptest.NewRecorder()
		b.Handler().ServeHTTP(w, r)
		b.Wait()
		if private && (w.Code != 202 || calls != 3) {
			t.Fatal(w.Code, calls)
		}
		if !private && (w.Code != 403 || calls != 0) {
			t.Fatal("public confirmation accepted")
		}
	}
}
