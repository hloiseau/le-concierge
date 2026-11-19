package eris

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/websearch"
)

// Only model calls and public web reads are real. Media and Discord actions
// are synthetic, with no production account or memory loaded.
func TestLiveAgentMadagascar(t *testing.T) {
	key := os.Getenv("ERIS_AGENT_KEY_FILE")
	if key == "" {
		t.Skip("native agent lab not enabled")
	}
	for _, tc := range []struct {
		name, question string
		want           int
		web            bool
	}{
		{"one", "Demande le film Madagascar stp", 1, false},
		{"saga", "Est ce que tu peux demander tous les films Madagascar stp ?", 3, false},
		{"web", "Est-ce qu'il existe un Madagascar 4 ? Vérifie sur Internet.", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(os.Getenv("ERIS_AGENT_API_URL"), "", "dolly2-security")
			c.V2, c.KeyFile = true, key
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
			defer cancel()
			requests := map[int64]int{}
			webCalls := 0
			lookup := `{"candidates":[{"id":953,"title":"Madagascar","year":"2005","mediaType":"movie"},{"id":270946,"title":"Les Pingouins de Madagascar","year":"2014","mediaType":"movie"},{"id":10527,"title":"Madagascar 2","year":"2008","mediaType":"movie"}]}`
			reply, err := c.ReplyWithTools(ctx, "synthetic:alice:channel", tc.question, false, func(ctx context.Context, p Plan) (ToolReply, error) {
				t.Logf("tool=%s mediaId=%d query=%q", p.Action, p.MediaID, p.Query)
				switch p.Action {
				case "lookup":
					return ToolReply{Content: lookup}, nil
				case "collection":
					return ToolReply{Content: `{"candidates":[{"id":953,"title":"Madagascar","year":"2005","mediaType":"movie"},{"id":10527,"title":"Madagascar 2","year":"2008","mediaType":"movie"},{"id":80321,"title":"Madagascar 3 : Bons baisers d'Europe","year":"2012","mediaType":"movie"}]}`}, nil
				case "request":
					requests[p.MediaID]++
					return ToolReply{Content: fmt.Sprintf(`{"status":"created","requestId":%d,"mediaId":%d,"plexAvailability":"not_checked","arrivalGuaranteed":false,"notificationCreated":false}`, 400+len(requests), p.MediaID)}, nil
				case "web":
					webCalls++
					r, err := websearch.New(os.Getenv("ERIS_AGENT_WEB_URL"), "").Search(ctx, p.Query, false)
					if err != nil {
						return ToolReply{Content: `{"error":"web_unavailable"}`}, err
					}
					return ToolReply{Content: `{"status":"evidence_found"}`, Web: &r}, nil
				default:
					return ToolReply{Content: `{"error":"tool_not_in_scenario"}`}, nil
				}
			})
			t.Logf("reply=%q", reply)
			if err != nil || len(requests) != tc.want || tc.want > 0 && requests[953] != 1 || tc.want == 3 && (requests[10527] != 1 || requests[80321] != 1) || requests[270946] != 0 || tc.web && (webCalls == 0 || !strings.Contains(reply, "](<")) {
				t.Fatalf("requests=%v web=%d error=%v", requests, webCalls, err)
			}
			for _, canned := range []string{"Choisis le bon titre", "Pour choisir", "Bon, voilà.", "Je n’ai pas bien compris", "numéro de la proposition"} {
				if strings.Contains(reply, canned) {
					t.Error("canned choice flow survived", reply)
				}
			}
		})
	}
}

func TestLiveAgentStructuredTools(t *testing.T) {
	key := os.Getenv("ERIS_AGENT_KEY_FILE")
	if key == "" {
		t.Skip("native agent lab not enabled")
	}
	for _, tc := range []struct{ name, question, action, facts string }{
		{"metrics", "Tu peux vérifier la charge du CPU du NAS ?", "monitoring", `{"metrics":{"cpu_percent":{"value":42,"unit":"percent","status":"available"},"cpu_temperature":{"value":null,"unit":"celsius","status":"stale"}}}`},
		{"preferences", "Mets mon plafond de rappels à trois par jour stp.", "preferences", `{"status":"saved_and_verified","preferences":{"dailyCap":3}}`},
		{"diagnostic", "Pourquoi ma demande numéro 411 est bloquée ?", "diagnostic", `{"title":"Madagascar","stage":"manual_import","partial":false}`},
		{"gifts", "Lis la liste cadeaux Alice et aide-moi à choisir un jeu de société.", "gifts", `{"status":"evidence_found","ideas":[{"title":"Un jeu coopératif","description":"Pour quatre joueurs","declaredBudget":30,"currency":"EUR"}],"reservationStatusExposed":false,"merchantPriceVerified":false}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(os.Getenv("ERIS_AGENT_API_URL"), "", "dolly2-security")
			c.V2, c.KeyFile = true, key
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			calls := 0
			got, err := c.ReplyWithTools(ctx, "synthetic:member", tc.question, false, func(_ context.Context, p Plan) (ToolReply, error) {
				t.Logf("tool=%s", p.Action)
				if p.Action != tc.action {
					return ToolReply{Content: `{"error":"not_in_scenario"}`}, nil
				}
				if p.Action == "preferences" && (p.Setting != "dailyCap" || p.Value != "3") {
					t.Error("wrong preference", p)
				}
				calls++
				return ToolReply{Content: tc.facts}, nil
			})
			t.Logf("reply=%q", got)
			if err != nil || calls != 1 || got == "" || strings.HasPrefix(got, "{") {
				t.Fatal(got, err, calls)
			}
		})
	}
}
