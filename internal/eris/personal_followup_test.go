package eris

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUnfollowChoiceBypassesModelAndMediaRemovalGuardOnlyForUnfollow(t *testing.T) {
	for _, operation := range []string{"unfollow", ""} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"unfollow"}`}}}})
		}))
		c := New(server.URL, "", "test")
		c.V2 = true
		choice := "eris:choose:abc"
		c.sessions["member"] = &conversation{expires: time.Now().Add(time.Minute), choices: []Button{{ID: choice, Title: "Arrival", Number: 1, Operation: operation}}, choiceSource: "source", choiceUntil: time.Now().Add(time.Minute)}
		tools := 0
		text, err := c.ReplyWithTools(context.Background(), "member", "retire le premier", false, func(_ context.Context, p Plan) (ToolReply, error) {
			tools++
			if p.Action != "continue" || p.Query != choice {
				t.Fatal(p)
			}
			return ToolReply{Content: "Suivi retiré."}, nil
		})
		server.Close()
		if err != nil || calls != 0 {
			t.Fatal(text, err, calls)
		}
		if operation == "unfollow" && (tools != 1 || text != "Suivi retiré.") {
			t.Fatal(text, tools)
		}
		if operation == "" && (tools != 0 || text != RemovalExplanation) {
			t.Fatal("media removal guard bypassed", text, tools)
		}
	}
}
