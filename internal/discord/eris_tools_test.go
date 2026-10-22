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

func TestErisV2NaturalRequestsHideButtonsAndBypassModelForConsent(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	b.Eris.V2, b.Eris.URL, b.Config.BotAPIURL = true, "http://model.test", "http://api.test"
	modelCalls, apiCalls := 0, 0
	choose, confirm := "eris:choose:"+strings.Repeat("a", 64), "eris:confirm:"+strings.Repeat("b", 64)
	b.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "model.test" {
			modelCalls++
			return botResponse(200, `{"choices":[{"message":{"role":"assistant","content":"{\"action\":\"prepare\",\"query\":\"Bronson\",\"kind\":\"movie\"}"}}]}`), nil
		}
		apiCalls++
		var in eris.ToolRequest
		_ = json.NewDecoder(req.Body).Decode(&in)
		if in.ChannelID != "33333" || in.MessageID == "" {
			t.Error("lost message proof")
		}
		var result eris.ToolReply
		switch apiCalls {
		case 1:
			if in.Plan.Action != "prepare" || in.Choice != "" {
				t.Error(in)
			}
			result = eris.ToolReply{Content: "1. Bronson (2009)", Buttons: []eris.Button{{ID: choose, Label: "Choisir 1", Title: "Bronson", Number: 1}}}
		case 2:
			if in.Choice != choose || in.Plan.Action != "" {
				t.Error(in)
			}
			result = eris.ToolReply{Content: "Je demande Bronson (2009) ?", Buttons: []eris.Button{{ID: confirm, Label: "Confirmer"}}}
		case 3:
			if in.Choice != confirm || in.Plan.Action != "" {
				t.Error(in)
			}
			result = eris.ToolReply{Content: "Demande créée."}
		default:
			t.Error("extra API call")
		}
		raw, _ := json.Marshal(result)
		return botResponse(200, string(raw)), nil
	})
	b.Eris.HTTP.Transport = b.Client.Transport
	for _, q := range []string{"demande Bronson", "prends le premier", "oui, vas-y"} {
		result := b.erisMediaReply(context.Background(), "guild:alice:33333", q, false, eris.ToolRequest{ChannelID: "33333", MessageID: "99999"})
		if len(result.Buttons) != 0 {
			t.Fatal("v2 still displays buttons")
		}
		if apiCalls == 2 && !strings.Contains(result.Content, "oui, vas-y") {
			t.Fatal("missing natural consent hint")
		}
		b.Eris.Delivered("guild:alice:33333", "88888", result.Content)
	}
	if modelCalls != 1 || apiCalls != 3 {
		t.Fatal(modelCalls, apiCalls)
	}
}

func TestErisPublicSelectionCreatesPublicReplyWithoutOverwritingOriginal(t *testing.T) {
	body := `{"id":"99999","application_id":"12345","guild_id":"23456","channel_id":"33333","type":3,"token":"interaction-token","member":{"user":{"id":"11111"}},"message":{"flags":0,"author":{"id":"12345"}},"data":{"custom_id":"eris:choose:` + strings.Repeat("a", 64) + `","component_type":2}}`
	b, r := signedInteraction(t, body)
	b.Config.BotAPIURL = "http://api.test"
	calls := 0
	b.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(req.Body)
		switch {
		case strings.HasSuffix(req.URL.Path, "/callback"):
			if string(raw) != `{"type":5}` {
				t.Error("public selection must create a separate public reply", string(raw))
			}
		case req.URL.Host == "api.test":
			return botResponse(200, `{"content":"Confirmer Hot Fuzz ?","buttons":[{"id":"eris:confirm:`+strings.Repeat("b", 64)+`","label":"Confirmer"}]}`), nil
		case strings.HasSuffix(req.URL.Path, "/messages/@original"):
			if strings.Contains(string(raw), `"flags":64`) || !strings.Contains(string(raw), "eris:confirm:") || !strings.Contains(string(raw), `"parse":[]`) {
				t.Error("public confirmation missing or mentions enabled")
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

func TestErisConfirmationBypassesModelAndPreservesVisibility(t *testing.T) {
	for _, private := range []bool{true, false} {
		flags := 0
		if private {
			flags = 64
		}
		body, _ := json.Marshal(map[string]any{"id": "99999", "application_id": "12345", "guild_id": "23456", "channel_id": "33333", "type": 3, "token": "interaction-token", "member": map[string]any{"user": map[string]string{"id": "11111"}}, "message": map[string]any{"flags": flags, "author": map[string]string{"id": "12345"}}, "data": map[string]any{"custom_id": "eris:confirm:" + strings.Repeat("a", 64), "component_type": 2}})
		b, r := signedInteraction(t, string(body))
		b.Config.BotAPIURL = "http://api.test"
		b.Eris.URL = "http://model.test"
		calls := 0
		b.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if strings.HasSuffix(req.URL.Path, "/callback") {
				raw, _ := io.ReadAll(req.Body)
				want := `{"type":5}`
				if private {
					want = `{"type":6}`
				}
				if string(raw) != want {
					t.Error("wrong acknowledgement", string(raw))
				}
			}
			if strings.HasSuffix(req.URL.Path, "/messages/@original") {
				raw, _ := io.ReadAll(req.Body)
				if strings.Contains(string(raw), `"flags":64`) != private {
					t.Error("wrong visibility", string(raw))
				}
			}
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
		if w.Code != 202 || calls != 3 {
			t.Fatal(w.Code, calls)
		}
	}
}
