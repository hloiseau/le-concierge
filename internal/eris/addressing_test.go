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

func TestAddressingIsStrictAndDoesNotBorrowPrivateHistory(t *testing.T) {
	for _, tc := range []struct {
		raw               string
		status            int
		addressed, failed bool
	}{
		{`{"addressed":true}`, 200, true, false},
		{`{"addressed":false}`, 200, false, false},
		{`{}`, 200, false, true},
		{`{"addressed":"true"}`, 200, false, true},
		{`{"addressed":true,"action":"prepare"}`, 200, false, true},
		{`{"addressed":true} {}`, 200, false, true},
		{`{"addressed":true}`, 503, false, true},
	} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct{ Messages []Message }
				_ = json.NewDecoder(r.Body).Decode(&body)
				raw, _ := json.Marshal(body)
				if len(body.Messages) != 2 || strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), CharacterPersona) {
					t.Error("classification borrowed conversation history/personality")
				}
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: tc.raw}}}})
			}))
			defer server.Close()
			c := New(server.URL, "", "test")
			c.sessions["private"] = &conversation{messages: []Message{{Role: "user", Content: "PRIVATE"}}}
			got, err := c.Addressed(context.Background(), "Salut", "Et toi, ça roule ?", "Nickel", true)
			if got != tc.addressed || (err != nil) != tc.failed || len(c.sessions) != 1 || len(c.sessions["private"].messages) != 1 {
				t.Fatalf("got=%v err=%v sessions=%d", got, err, len(c.sessions))
			}
		})
	}
}

func TestAddressingSharesInferenceCapacity(t *testing.T) {
	c := New("http://unused.test", "", "test")
	c.slots <- struct{}{}
	c.slots <- struct{}{}
	if ok, err := c.Addressed(context.Background(), "Salut", "Ça va ?", "Oui", true); ok || err != ErrBusy {
		t.Fatal(ok, err)
	}
}
