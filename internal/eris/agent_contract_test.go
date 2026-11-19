package eris

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEveryToolReturnsFactsToTheModelForItsOwnReply(t *testing.T) {
	cases := map[string]string{
		"lookup_media": `{"query":"Madagascar"}`, "request_media": `{"mediaId":953,"kind":"movie"}`,
		"movie_collection": `{"mediaId":953}`, "search_plex": `{"query":"Madagascar"}`,
		"recommend": `{"genre":"thriller"}`, "recent": `{}`, "status": `{}`, "monitoring": `{}`,
		"web": `{"query":"Madagascar 4 annonce"}`, "cinema": `{"query":"Madagascar"}`,
		"diagnostic": `{"requestId":411}`, "anime": `{"query":"Frieren","kind":"anime"}`, "gifts": `{}`,
		"follow": `{"mediaId":953,"kind":"movie","channel":"hub"}`, "follows": `{}`,
		"unfollow": `{"subscriptionId":17}`, "preferences": `{"setting":"dailyCap","value":"3"}`,
		"poll":  `{"text":"Quel film ?","options":["Shrek","Madagascar"]}`,
		"event": `{"text":"Cinéma","startAt":"2027-01-01T20:00:00+01:00","endAt":"2027-01-01T22:00:00+01:00"}`,
	}
	if len(cases) != len(agentTools()) {
		t.Fatal("a registered tool lacks a wire test")
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			for _, success := range []bool{true, false} {
				calls, executions := 0, 0
				facts := `{"status":"verified","marker":"opaque-tool-fact"}`
				if !success {
					facts = `{"error":"service_denied","marker":"opaque-tool-fact"}`
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					var request struct {
						Messages       []Message
						Tools          []toolDefinition
						ResponseFormat json.RawMessage `json:"response_format"`
					}
					if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Tools) != len(cases) || len(request.ResponseFormat) != 0 {
						t.Error("missing native function contract")
					}
					message := Message{Role: "assistant"}
					finish := "stop"
					if calls == 1 {
						call := ToolCall{ID: "native_call", Type: "function"}
						call.Function.Name, call.Function.Arguments = name, args
						message.ToolCalls, finish = []ToolCall{call}, "tool_calls"
					} else {
						last := request.Messages[len(request.Messages)-2]
						if last.Role != "tool" || last.ToolCallID != "native_call" || !strings.Contains(last.Content, "opaque-tool-fact") {
							t.Error("tool facts did not return to the model", last)
						}
						message.Content = `{"action":"chat","text":"Réponse rédigée par le modèle."}`
					}
					json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": finish, "message": message}}})
				}))
				c := New(server.URL, "", "any-model")
				c.V2 = true
				got, err := c.ReplyWithTools(context.Background(), "guild:member", "Une demande contextuelle", false, func(_ context.Context, plan Plan) (ToolReply, error) {
					executions++
					if plan.Action != agentActions[name] {
						t.Error("wrong action", plan)
					}
					return ToolReply{Content: facts}, nil
				})
				server.Close()
				if err != nil || got != "Réponse rédigée par le modèle." || calls != 2 || executions != 1 {
					t.Fatalf("success=%v reply=%q err=%v model=%d tools=%d", success, got, err, calls, executions)
				}
			}
		})
	}
}

func TestChatOnlyConversationAdvertisesNoToolsAndUsesModelReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		if len(body["tools"]) != 0 {
			t.Error("chat-only conversation exposed functions")
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"chat","text":"Je peux discuter avec toi ici."}`}}}})
	}))
	defer server.Close()
	c := New(server.URL, "", "model")
	c.V2 = true
	got, err := c.ReplyWithDiscordContext(context.Background(), "guild:member", "Tu peux demander un film ?", false, "", DiscordInput{ChatOnly: true}, func(context.Context, Plan) (ToolReply, error) {
		t.Fatal("restricted conversation executed a function")
		return ToolReply{}, nil
	})
	if err != nil || got != "Je peux discuter avec toi ici." {
		t.Fatal(got, err)
	}
}
