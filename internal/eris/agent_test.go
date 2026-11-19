package eris

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAgentRequestsMadagascarWithoutHumanChoices(t *testing.T) {
	rounds, tools := 0, 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rounds++
		var payload map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&payload)
		if len(payload["tools"]) == 0 {
			t.Error("native tools not advertised")
		}
		if len(payload["response_format"]) > 0 {
			t.Error("JSON-only response mode disables native calls on the configured provider")
		}
		if rounds == 3 {
			dollyCompletion(w, "dolly2-security", "stop", `{"action":"chat","text":"C’est demandé, Madagascar (2005)."}`)
			return
		}
		name, arguments := "lookup_media", `{"query":"Madagascar","kind":"movie"}`
		if rounds == 2 {
			name, arguments = "request_media", `{"mediaId":123,"kind":"movie"}`
			if !strings.Contains(string(payload["messages"]), `tool_call_id`) || !strings.Contains(string(payload["messages"]), `Madagascar`) {
				t.Error("tool result not passed back to the model")
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "dolly2-security", "choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "call_" + name, "type": "function", "function": map[string]string{"name": name, "arguments": arguments}}}}}}})
	}))
	defer model.Close()
	c := New(model.URL, "sk-"+strings.Repeat("z", 22), "dolly2-security")
	c.V2 = true
	reply, err := c.ReplyWithTools(context.Background(), "guild:member:channel", "Demande Madagascar stp", false, func(_ context.Context, p Plan) (ToolReply, error) {
		tools++
		if tools == 1 && p.Action != "lookup" || tools == 2 && p.Action != "request" {
			t.Fatalf("wrong tool: %+v", p)
		}
		if tools == 1 {
			return ToolReply{Content: `{"candidates":[{"id":123,"title":"Madagascar","year":"2005","kind":"movie"},{"id":456,"title":"Les Pingouins de Madagascar","year":"2014","kind":"movie"}]}`}, nil
		}
		return ToolReply{Content: `{"status":"created","requestId":411,"title":"Madagascar","year":"2005"}`}, nil
	})
	if err != nil || tools != 2 || rounds != 3 || reply != "C’est demandé, Madagascar (2005)." {
		t.Fatalf("reply=%q error=%v tools=%d rounds=%d", reply, err, tools, rounds)
	}
	if len(c.sessions["guild:member:channel"].choices) != 0 {
		t.Fatal("human choice state created")
	}
}

func TestNativeFollowupWaitHonorsCancellation(t *testing.T) {
	c := New("http://must-not-call.example", "", "dolly2-security")
	c.V2 = true
	c.sessions["member"] = &conversation{busy: true}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.ReplyWithTools(ctx, "member", "demande le aussi", false, nil)
	if !errors.Is(err, ErrUnavailable) || ctx.Err() == nil {
		t.Fatal("follow-up was rejected instead of queued until cancellation", err)
	}
}
