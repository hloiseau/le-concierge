package eris

import "testing"

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
