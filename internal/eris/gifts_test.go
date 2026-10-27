package eris

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGiftPlansStayReadOnlyAndBounded(t *testing.T) {
	for _, raw := range []string{`{"action":"gifts"}`, `{"action":"gifts","query":"Alice","text":"jeu"}`} {
		if _, err := ParsePlan(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{`{"action":"gifts","url":"https://other.test"}`, `{"action":"gifts","kind":"reserve"}`, `{"action":"gifts","year":2026}`, `{"action":"gifts","query":"Alice\n"}`, `{"action":"gifts","userId":"bob"}`} {
		if _, err := ParsePlan(raw); err == nil {
			t.Fatal(raw)
		}
	}
}

func TestGiftsSynthesizeOnlyProjectedEvidence(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw := `{"action":"gifts","query":"Alice"}`
		if calls == 2 {
			raw = `{"text":"Le jeu coopératif correspond à ta recherche [1].","sources":[1]}`
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: raw}}}})
	}))
	defer server.Close()
	c := New(server.URL, "", "test")
	c.V2 = true
	text, err := c.ReplyWithTools(context.Background(), "member", "Quel cadeau choisir dans la liste Alice ?", false, func(ctx context.Context, p Plan) (ToolReply, error) {
		if p.Action != "gifts" {
			t.Fatal(p)
		}
		evidence := webEvidence()
		evidence.Sources = evidence.Sources[:1]
		evidence.Sources[0].Text = "Un jeu coopératif"
		evidence.Sources[0].Kind = "metadata"
		return ToolReply{Content: evidence.Fallback(), Web: &evidence}, nil
	})
	if err != nil || calls != 2 || !strings.Contains(text, "jeu coopératif") {
		t.Fatal(text, calls, err)
	}
}
