package eris

import "testing"

func TestDiagnosticPlanCannotSelectIdentityOrRepair(t *testing.T) {
	for _, raw := range []string{`{"action":"diagnostic","query":"Arrival"}`, `{"action":"diagnostic","requestId":7}`} {
		if _, err := ParsePlan(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{`{"action":"diagnostic"}`, `{"action":"diagnostic","requestId":-1}`, `{"action":"diagnostic","requestId":7,"query":"Arrival"}`, `{"action":"diagnostic","requestId":7,"userId":2}`, `{"action":"diagnostic","query":"Arrival","relation":"repair"}`, `{"action":"search","query":"Arrival","requestId":7}`} {
		if _, err := ParsePlan(raw); err == nil {
			t.Fatal("invalid plan accepted", raw)
		}
	}
}
