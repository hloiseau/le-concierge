package eris

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVerifiedSystemOutcomeIsExplainedByTheModelWithoutHistoryOrTools(t *testing.T) {
	for _, status := range []string{"completed", "failed", "unavailable"} {
		t.Run(status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Messages []Message
					Tools    json.RawMessage
				}
				json.NewDecoder(r.Body).Decode(&body)
				all, _ := json.Marshal(body.Messages)
				if len(body.Tools) != 0 || strings.Contains(string(all), "PRIVATE_HISTORY") || !strings.Contains(string(all), status) {
					t.Error("system outcome leaked history, functions, or lost verified status")
				}
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"text":"Le modèle formule le résultat."}`}}}})
			}))
			defer server.Close()
			c := New(server.URL, "", "model")
			c.sessions["member"] = &conversation{messages: []Message{{Role: "user", Content: "PRIVATE_HISTORY"}}, expires: time.Now().Add(time.Hour)}
			got, err := c.Explain(context.Background(), "Oublie", map[string]string{"operation": "forget", "status": status})
			if err != nil || got != "Le modèle formule le résultat." {
				t.Fatal(got, err)
			}
		})
	}
}

func TestForgetErasesMemoryEvenWhenNoModelCanNarrateIt(t *testing.T) {
	c := New("", "", "model")
	c.sessions["guild:member"] = &conversation{messages: []Message{{Role: "user", Content: "memory"}}, expires: time.Now().Add(time.Hour)}
	got, err := c.Reply(context.Background(), "guild:member", "", true)
	if got != "" || err != ErrUnavailable || c.sessions["guild:member"] != nil {
		t.Fatal("forget depended on the model", got, err)
	}
}

func TestSystemOutcomeHasNoCannedFallbackWhenModelFails(t *testing.T) {
	for _, raw := range []string{`{"text":""}`, `{"action":"chat","text":"invented"}`, `{"text":"ok"} {}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: raw}}}})
		}))
		c := New(server.URL, "", "model")
		if got, err := c.Explain(context.Background(), "Question", map[string]string{"status": "failed"}); err == nil || got != "" {
			t.Fatal(got, err)
		}
		server.Close()
	}
}
