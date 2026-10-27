package eris

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The same lab can run at the recovered commit and main. All participants,
// conversations, tools and deliveries are synthetic; no Discord requests.
func TestLiveErisReconciliation(t *testing.T) {
	path := os.Getenv("ERIS_RECONCILIATION_CASES")
	if path == "" {
		t.Skip("synthetic reconciliation lab not enabled")
	}
	model := os.Getenv("ERIS_RECONCILIATION_MODEL")
	if model == "" {
		t.Fatal("select a model explicitly")
	}
	var cases []struct {
		ID, Review string
		Turns      []struct {
			Member, Name, Input, Action string
			Restart, Forget, Silent     bool
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &cases) != nil || len(cases) == 0 {
		t.Fatal("invalid synthetic cases")
	}
	type turn struct {
		Case, Member, Name, Input, Reply, Action string
		Restart, Forget                          bool
		Seconds                                  float64
	}
	report := struct {
		Model string
		Turns []turn
	}{Model: model}
	defer func() {
		raw, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(os.Getenv("ERIS_RECONCILIATION_REPORT"), append(raw, '\n'), 0600); err != nil {
			t.Error("cannot save private report")
		}
	}()
	for _, scenario := range cases {
		t.Run(scenario.ID, func(t *testing.T) {
			database := filepath.Join(t.TempDir(), "memory.db")
			var c *Chat
			var memory *Memory
			defer func() {
				if memory != nil {
					memory.Close()
				}
			}()
			for i, input := range scenario.Turns {
				if c == nil || input.Restart {
					if memory != nil {
						memory.Close()
					}
					memory, err = OpenMemory(database)
					if err != nil {
						t.Fatal(err)
					}
					c = New("https://api.openai.com/v1", "", model)
					c.V2, c.Protocol, c.KeyFile, c.Memory = true, "responses", os.Getenv("ERIS_RECONCILIATION_TOKEN_FILE"), memory
				}
				owner := "10000:" + input.Member + ":30000"
				row := turn{Case: scenario.ID, Member: input.Member, Name: input.Name, Input: input.Input, Restart: input.Restart, Forget: input.Forget}
				if input.Forget {
					_, err = c.Reply(context.Background(), owner, "", true)
				} else {
					if session := c.sessions[owner]; session != nil {
						session.next = time.Time{}
					}
					incoming := DiscordInput{MessageID: fmt.Sprintf("%05d", 40000+i), Speaker: Speaker{ID: input.Member, Name: input.Name, Username: "synthetic_" + input.Member}}
					start := time.Now()
					row.Reply, err = c.ReplyWithDiscordContext(context.Background(), owner, input.Input, false, "", incoming, func(_ context.Context, plan Plan) (ToolReply, error) {
						row.Action = plan.Action
						return ToolReply{Content: "Résultat synthétique : choix ou mesure simulés ; aucune action réelle."}, nil
					})
					row.Seconds = time.Since(start).Seconds()
					if row.Reply != "" {
						c.Delivered(owner, fmt.Sprintf("%05d", 50000+i), row.Reply)
					}
					if row.Action != input.Action {
						t.Errorf("turn %d: action %q, expected %q", i, row.Action, input.Action)
					}
					if input.Silent && row.Reply != "" {
						t.Errorf("turn %d: expected silence", i)
					}
				}
				report.Turns = append(report.Turns, row)
				if err != nil {
					t.Fatal("completion or memory operation failed")
				}
			}
		})
	}
}
