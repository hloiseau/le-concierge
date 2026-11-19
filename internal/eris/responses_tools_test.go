package eris

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponsesNativeToolLoopReplaysReasoningAndFunctionResult(t *testing.T) {
	calls, tools := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		var definitions []map[string]json.RawMessage
		json.Unmarshal(body["tools"], &definitions)
		if len(definitions) != len(agentTools()) || len(definitions[0]["parameters"]) == 0 || len(definitions[0]["function"]) != 0 || len(body["text"]) != 0 || !strings.Contains(string(body["include"]), "reasoning.encrypted_content") {
			t.Error("Responses tools were not advertised with the native contract")
		}
		output := `[{"type":"reasoning","id":"rs_1","encrypted_content":"opaque_reasoning","summary":[]},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"monitoring","arguments":"{}","status":"completed"}]`
		if calls == 2 {
			if !strings.Contains(string(body["input"]), "opaque_reasoning") || !strings.Contains(string(body["input"]), `"type":"function_call_output"`) || !strings.Contains(string(body["input"]), `"call_id":"call_1"`) || !strings.Contains(string(body["input"]), "verified_cpu_42") {
				t.Error("Responses reasoning or function output was lost", string(body["input"]))
			}
			text, _ := json.Marshal(`{"action":"chat","text":"Le CPU est à 42 %."}`)
			output = fmt.Sprintf(`[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":%s}]}]`, text)
		}
		fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":%s}}\n\n", output)
	}))
	defer server.Close()
	c := New(server.URL, "test-key", "responses-model")
	c.Protocol = "responses"
	c.V2 = true
	got, err := c.ReplyWithTools(context.Background(), "member", "État du CPU ?", false, func(_ context.Context, p Plan) (ToolReply, error) {
		tools++
		if p.Action != "monitoring" {
			t.Error(p)
		}
		return ToolReply{Content: `{"cpu":42,"marker":"verified_cpu_42"}`}, nil
	})
	if err != nil || got != "Le CPU est à 42 %." || calls != 2 || tools != 1 {
		t.Fatal(got, err, calls, tools)
	}
}

func TestResponsesDoesNotExecuteUnfinishedOrHostedTools(t *testing.T) {
	item := `{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"monitoring","arguments":"{}","status":"completed"}}`
	for _, events := range []string{
		"data: " + item + "\n\n",
		"data: " + item + "\n\ndata: {\"type\":\"response.incomplete\"}\n\n",
		`data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"web_search_call","status":"completed"}]}}` + "\n\n",
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, events) }))
		c := New(server.URL, "test-key", "responses-model")
		c.Protocol = "responses"
		_, err := c.ReplyWithTools(context.Background(), "member", "État du CPU ?", false, func(context.Context, Plan) (ToolReply, error) {
			t.Fatal("executed an unverified tool")
			return ToolReply{}, nil
		})
		server.Close()
		if err == nil {
			t.Fatal("accepted incomplete or hosted output")
		}
	}
}
