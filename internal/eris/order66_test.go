package eris

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestOrder66AsksNaturallyWhenThereIsNoIdentifiableTarget(t *testing.T) {
	server := featureModelServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []Message       `json:"messages"`
			Tools    json.RawMessage `json:"tools"`
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil || len(payload.Tools) != 0 {
			t.Error("roleplay gained external tools")
		}
		var facts struct {
			Result struct {
				Status           string `json:"status"`
				Missing          string `json:"missing"`
				InvokerProtected bool   `json:"invokerProtected"`
			} `json:"result"`
		}
		json.Unmarshal([]byte(payload.Messages[len(payload.Messages)-1].Content), &facts)
		if facts.Result.Status != "clarification_required" || facts.Result.Missing != "human_interlocutor" || !facts.Result.InvokerProtected {
			t.Error("model was still asked to choose a target when none exists")
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"text\":\"Qui est ma cible ?\"}"}}]}`))
	}))
	defer server.Close()
	c := New(server.URL, "", "model")
	c.V2 = true
	actor := Speaker{ID: "11111", Name: "Hugo"}
	reply, err := c.Order66(context.Background(), actor, []RoleplayTurn{{Author: actor, Text: "ordre66"}})
	if err != nil || reply != "Qui est ma cible ?" || len(c.sessions) != 0 {
		t.Fatal("ambiguous roleplay was not an isolated clarification", reply, err)
	}
}
