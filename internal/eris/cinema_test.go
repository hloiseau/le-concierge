package eris

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCinemaPlansAndRoutingPreserveExplicitYearAndFocus(t *testing.T) {
	for _, raw := range []string{`{"action":"cinema","query":"Dune","year":2021,"kind":"movie","relation":"cast"}`, `{"action":"cinema","query":"Dark","kind":"show"}`} {
		if _, err := ParsePlan(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{`{"action":"cinema","query":"Dune","year":1}`, `{"action":"cinema","query":"Dune","relation":"similar"}`, `{"action":"cinema","query":"Dune","unwatched":true}`, `{"action":"cinema","query":"Dune","kind":"youtube"}`, `{"action":"search","query":"Dune","year":2021}`} {
		if _, err := ParsePlan(raw); err == nil {
			t.Fatal("invalid plan accepted", raw)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"cinema","query":"Dune","year":2021,"kind":"movie","relation":"director"}`}}}})
	}))
	defer server.Close()
	c := New(server.URL, "", "test")
	c.V2 = true
	tools := 0
	text, err := c.ReplyWithTools(context.Background(), "member", "Qui réalise Dune de 2021 ?", false, func(_ context.Context, p Plan) (ToolReply, error) {
		tools++
		if p.Action != "cinema" || p.Query != "Dune" || p.Year != 2021 || p.Relation != "director" {
			t.Fatal(p)
		}
		return ToolReply{Content: "Réalisation vérifiée."}, nil
	})
	if err != nil || text != "Réalisation vérifiée." || tools != 1 {
		t.Fatal(text, err, tools)
	}
}
