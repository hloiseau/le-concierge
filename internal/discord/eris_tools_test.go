package discord

import (
	"context"
	"encoding/json"

	"net/http"

	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/websearch"
)

func TestErisToolAcceptsGiftEvidenceOnlyForAnEvidenceAction(t *testing.T) {
	for _, action := range []string{"gifts", "status"} {
		t.Run(action, func(t *testing.T) {
			b, _ := signedCommand(t, "eris")
			b.Config.BotAPIURL = "http://api.test"
			evidence := websearch.Result{RetrievedAt: "2026-10-28T12:00:00Z", Sources: []websearch.Source{{Title: "Liste autorisée", URL: "https://gifts.example/lists/one", Text: "Un jeu coopératif.", Kind: "metadata"}}}
			b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				raw, _ := json.Marshal(eris.ToolReply{Content: `{"status":"evidence_found"}`, Web: &evidence})
				return botResponse(200, string(raw)), nil
			})
			got := b.erisTool(context.Background(), eris.ToolRequest{Plan: eris.Plan{Action: action}})
			if (got.Web != nil) != (action == "gifts") {
				t.Fatal("gift evidence rejected or unexpected evidence accepted", got)
			}
		})
	}
}

func TestErisDeliversModelDiagnosticAfterNativeToolResult(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	b.Eris.V2 = true
	b.Eris.URL = "http://model.test"
	b.Config.BotAPIURL = "http://api.test"
	modelCalls, apiCalls := 0, 0
	b.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "api.test" {
			apiCalls++
			return botResponse(200, `{"content":"{\"stage\":\"manual_import\"}","diagnostic":{"title":"Toy Story 5","stage":"manual_import","partial":false}}`), nil
		}
		modelCalls++
		if modelCalls == 1 {
			return botResponse(200, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"diag_1","type":"function","function":{"name":"diagnostic","arguments":"{\"query\":\"Toy Story 5\"}"}}]}}]}`), nil
		}
		raw := `{"action":"chat","text":"Ça bloque à l’import. Faut qu’un admin vérifie le fichier."}`
		if modelCalls == 2 {
			raw = `{"action":"chat","text":"Ça bloque à l’import. Faut qu’un admin vérifie le fichier."}`
		}
		body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": eris.Message{Role: "assistant", Content: raw}}}})
		return botResponse(200, string(body)), nil
	})
	b.Eris.HTTP.Transport = b.Client.Transport
	got := b.erisMediaReply(context.Background(), "23456:11111:33333", "Pourquoi Toy Story 5 est bloqué ?", false, eris.ToolRequest{ChannelID: "33333", MessageID: "44444"})
	if modelCalls != 2 || apiCalls != 1 || got.Content != "Ça bloque à l’import. Faut qu’un admin vérifie le fichier." {
		t.Fatal(got, modelCalls, apiCalls)
	}
}

func TestErisRequestTransportFailureDoesNotInviteBlindRetry(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	b.Config.BotAPIURL = "http://api.test"
	calls := 0
	b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { calls++; return botResponse(502, `{}`), nil })
	result := b.erisTool(context.Background(), eris.ToolRequest{Plan: eris.Plan{Action: "request", MediaID: 12, Kind: "movie"}, CallID: "tool-1"})
	if calls != 1 || !strings.Contains(result.Content, "request_outcome_unknown") {
		t.Fatal("unsafe retry after an ambiguous write", result)
	}
}
