package eris

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestResponsesStreamingContractAndTokenRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	token := "token-one"
	os.WriteFile(path, []byte(token), 0600)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("wrong target or stale credential")
		}
		var body map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Fatal("bad JSON")
		}
		if string(body["stream"]) != "true" || string(body["store"]) != "false" || string(body["model"]) != `"gpt-6-luna"` {
			t.Error("wrong Responses contract")
		}
		for _, k := range []string{"tools", "temperature", "max_output_tokens", "previous_response_id"} {
			if _, ok := body[k]; ok {
				t.Error("unexpected field", k)
			}
		}
		var input []Message
		json.Unmarshal(body["input"], &input)
		if input[0].Role != "developer" {
			t.Error("system role not converted")
		}
		fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"{\\\"action\\\":\\\"chat\\\",\\\"text\\\":\\\"Salut\\\"}\"}]}]}}\n\n")
	}))
	defer server.Close()
	c := New(server.URL, "", "gpt-6-luna")
	c.Protocol = "responses"
	c.KeyFile = path
	for i := 0; i < 2; i++ {
		reply, err := c.complete(context.Background(), []Message{{Role: "system", Content: "JSON"}, {Role: "user", Content: "Salut"}}, true)
		if err != nil || reply != `{"action":"chat","text":"Salut"}` {
			t.Fatal(reply, err)
		}
		token = "token-two"
		os.WriteFile(path, []byte(token), 0600)
	}
}

func TestResponsesRejectsIncompleteRefusalAndUnexpectedTool(t *testing.T) {
	for _, body := range []string{
		`data: {"type":"response.output_text.delta","delta":"partial"}`,
		`data: {"type":"response.failed","response":{"error":"secret"}}`,
		`data: {"type":"response.completed","response":{"status":"incomplete","output":[]}}`,
		`data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call"}]}}`,
		`data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"refusal","text":"secret"}]}]}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, body) }))
		c := New(server.URL, "key", "gpt-6-luna")
		c.Protocol = "responses"
		if text, err := c.complete(context.Background(), nil, true); err == nil || text != "" {
			t.Fatal("accepted incomplete output", text, err)
		}
		server.Close()
	}
}
