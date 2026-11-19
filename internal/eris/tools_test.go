package eris

import (
	"context"
	"encoding/json"
	"net/http"

	"testing"
)

func TestPlansRejectArbitraryToolsIdentityAndBadFilters(t *testing.T) {
	for _, raw := range []string{
		`{"action":"exec","query":"rm"}`, `{"action":"prepare","query":"Arrival","userId":1}`,
		`{"action":"prepare","query":"Arrival","url":"http://internal"}`, `{"action":"confirm","query":"Arrival"}`,
		`{"action":"recommend","maxMinutes":-1}`, `{"action":"recommend","kind":"episode"}`,
		`{"action":"prepare","query":"Show","kind":"show","seasons":[1,1]}`,
		`{"action":"prepare","query":"Film","kind":"movie","seasons":[1]}`,
		`{"action":"search","query":"a"}`, `{"action":"status","unwatched":true}`,
		`{"action":"prepare","query":"Hot Fuzz","relation":"director"}`, `{"action":"recommend","query":"Hot Fuzz","relation":"actor"}`,
		`{"action":"recommend","source":"seerr"}`, `{"action":"recommend","query":"Hot Fuzz","relation":"director","source":"seerr","unwatched":true}`,
		`{"action":"recommend","query":"Hot Fuzz","relation":"similar","kind":"show"}`, `{"action":"recommend","query":"Hot Fuzz","relation":"similar","maxMinutes":90}`,
		`{"action":"recent"} {"action":"prepare","query":"Arrival"}`, "Je l’ai déjà ajouté.",
	} {
		if _, err := ParsePlan(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`{"action":"recommend","kind":"movie","genre":"thriller","maxMinutes":120}`, `{"action":"request","mediaId":100,"kind":"show","seasons":[2]}`, `{"action":"status"}`, `{"action":"chat","text":"Je suis Eris."}`} {
		if _, err := ParsePlan(raw); err != nil {
			t.Errorf("rejected %s", raw)
		}
	}
}

func TestRecommendationCanKeepReferenceRelationAndCatalogue(t *testing.T) {
	for _, raw := range []string{
		`{"action":"recommend","query":"Hot Fuzz","kind":"movie","relation":"similar"}`,
		`{"action":"recommend","query":"Hot Fuzz","kind":"movie","relation":"director","source":"seerr"}`,
	} {
		server := featureModelServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: raw}}}})
		}))
		c := New(server.URL, "", "model")
		called := false
		reply, err := c.ReplyWithTools(context.Background(), "guild:alice", "Du même réal que Hot Fuzz qu'on pourrait demander sur Seerr ?", false, func(_ context.Context, p Plan) (ToolReply, error) {
			called = true
			if p.Action != "recommend" || p.Query != "Hot Fuzz" {
				t.Fatalf("lost recommendation: %+v", p)
			}
			return ToolReply{Content: "Films vérifiés du même réalisateur"}, nil
		})
		server.Close()
		if err != nil || !called || reply != "Films vérifiés du même réalisateur" {
			t.Fatalf("recommendation became a title clarification: %q %v", reply, err)
		}
	}
}
