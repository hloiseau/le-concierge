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
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"targetDiscordId\":\"\",\"text\":\"Qui est ma cible ?\"}"}}]}`))
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
