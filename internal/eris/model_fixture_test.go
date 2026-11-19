package eris

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
)

// Existing feature fixtures describe a desired action. Adapt those fixtures to
// native provider frames; agent_test exercises the wire loop independently.
func featureModelServer(handler http.Handler) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var request struct {
			Model    string
			Messages []Message
			Tools    []json.RawMessage
		}
		json.Unmarshal(raw, &request)
		// Feature tests use a separate synthetic reply for application outcomes;
		// system_reply_test checks the real narration request independently.
		if len(request.Messages) > 0 {
			var outcome struct{ Result struct{ Operation string } }
			if json.Unmarshal([]byte(request.Messages[len(request.Messages)-1].Content), &outcome) == nil && outcome.Result.Operation == "forget" {
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"text":"La mémoire a été effacée."}`}}}})
				return
			}
		}
		lastTool := -1
		for j, message := range request.Messages {
			if message.Role == "tool" {
				lastTool = j
			}
		}
		if len(request.Tools) > 0 && lastTool >= 0 {
			var result struct{ Facts json.RawMessage }
			json.Unmarshal([]byte(request.Messages[lastTool].Content), &result)
			text := "Résultat vérifié."
			var facts string
			if json.Unmarshal(result.Facts, &facts) == nil && facts != "" {
				text = facts
			}
			answer, _ := json.Marshal(Plan{Action: "chat", Text: text})
			json.NewEncoder(w).Encode(map[string]any{"model": request.Model, "choices": []any{map[string]any{"finish_reason": "stop", "message": Message{Role: "assistant", Content: string(answer)}}}})
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		var response struct{ Choices []struct{ Message Message } }
		if len(request.Tools) > 0 && recorder.Code == 200 && json.Unmarshal(recorder.Body.Bytes(), &response) == nil && len(response.Choices) == 1 {
			var fields map[string]json.RawMessage
			if json.Unmarshal([]byte(response.Choices[0].Message.Content), &fields) == nil {
				var action string
				json.Unmarshal(fields["action"], &action)
				for name, target := range agentActions {
					if target == action {
						delete(fields, "action")
						if action == "request" && fields["targetDiscordId"] == nil {
							fields["targetDiscordId"] = json.RawMessage(`"self"`)
						}
						arguments, _ := json.Marshal(fields)
						call := ToolCall{ID: "fixture_call", Type: "function"}
						call.Function.Name = name
						call.Function.Arguments = string(arguments)
						message := Message{Role: "assistant", ToolCalls: []ToolCall{call}}
						json.NewEncoder(w).Encode(map[string]any{"model": request.Model, "choices": []any{map[string]any{"finish_reason": "tool_calls", "message": message}}})
						return
					}
				}
			}
		}
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		w.Write(recorder.Body.Bytes())
	}))
}
