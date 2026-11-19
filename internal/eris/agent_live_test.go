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
					return ToolReply{Content: fmt.Sprintf(`{"status":"created","requestId":%d,"mediaId":%d}`, 400+len(requests), p.MediaID)}, nil
				case "web":
					webCalls++
					r, err := websearch.New(os.Getenv("ERIS_AGENT_WEB_URL"), "").Search(ctx, p.Query, false)
					if err != nil {
						return ToolReply{Content: `{"error":"web_unavailable"}`}, err
					}
					return ToolReply{Content: r.Fallback(), Web: &r}, nil
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
