package eris

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestErisRepairsLoreReplyWithCurrentDiscordContext(t *testing.T) {
	const question = "Raconte ton histoire à Sam."
	calls := 0
	server := historyModel(t, func(messages []Message) string {
		calls++
		if calls == 1 {
			return `{"action":"chat","text":"` + strings.Repeat("x", 1001) + `"}`
		}
		if len(messages) == 0 || !strings.Contains(messages[0].Content, CharacterCanon) || strings.Contains(messages[0].Content, "<actions>") {
			t.Error("recovery dropped the canon or exposed tools")
			return ""
		}
		body, _ := json.Marshal(messages)
		if !strings.Contains(string(body), "Tu as déjà vu Arrival") || !strings.Contains(string(body), `\"mentions\"`) || messages[len(messages)-1].Content != question {
			t.Error("recovery lost the conversation, recipient or current question")
		}
		return `{"action":"chat","text":"Sam, j’ai commencé comme une silhouette dans HLOISEAU_OS. Ils m’ont appelée ERIS-01 en me confinant."}`
	})
	c := New(server.URL, "", "fixture")
	c.V2 = true
	owner := "34567:11111:33333"
	incoming := DiscordInput{MessageID: "50003", Speaker: Speaker{ID: "11111", Name: "Alex"}, Mentions: []Speaker{{ID: "22222", Name: "Sam"}}}
	// Open the public window before recording the interleaved speaker.
	c.mu.Lock()
	c.observePublic(owner, "On parlait de ton passé.", DiscordInput{MessageID: "50001", Speaker: incoming.Speaker}, time.Now())
	c.mu.Unlock()
	c.ObserveAmbient("34567:22222:33333", "Tu as déjà vu Arrival ?", DiscordInput{MessageID: "50002", Speaker: Speaker{ID: "22222", Name: "Sam"}})
	reply, err := c.ReplyWithDiscordContext(context.Background(), owner, question, false, "", incoming, func(context.Context, Plan) (ToolReply, error) {
		t.Error("lore called a tool")
		return ToolReply{}, nil
	})
	if err != nil || calls != 2 || !strings.Contains(reply, "ERIS-01") {
		t.Fatalf("reply=%q err=%v calls=%d", reply, err, calls)
	}
}

func TestStalledReplyIsDeliveredAndTheNextTurnKeepsTheRequest(t *testing.T) {
	calls := 0
	server := historyModel(t, func(messages []Message) string {
		calls++
		if calls < 3 {
			return `{"action":"unsupported"}`
		}
		body, _ := json.Marshal(messages)
		if len(messages) == 0 || !strings.Contains(messages[0].Content, stalledTurnInstructions) || !strings.Contains(string(body), "Fais des propals") || !strings.Contains(string(body), dialogueFallback) {
			t.Error("follow-up did not receive the failed turn")
		}
		return `{"action":"chat","text":"Je dirais Dark si tu veux de la SF qui prend son temps."}`
	})
	c := New(server.URL, "", "fixture")
	c.V2 = true
	owner := "34567:11111:33333"
	run := func(context.Context, Plan) (ToolReply, error) {
		t.Error("invalid plan called a tool")
		return ToolReply{}, nil
	}
	first, err := c.ReplyWithTools(context.Background(), owner, "Fais des propals", false, run)
	if err != nil || first != dialogueFallback || calls != 2 {
		t.Fatalf("first reply=%q err=%v calls=%d", first, err, calls)
	}
	c.Delivered(owner, "50001", first)
	reply, err := c.ReplyWithTools(context.Background(), owner, "Des séries de SF", false, run)
	if err != nil || !strings.Contains(reply, "Dark") || calls != 3 {
		t.Fatalf("follow-up=%q err=%v calls=%d", reply, err, calls)
	}
}

func TestRepeatedQuestionIsRepairedWithTheAnswerAlreadyGiven(t *testing.T) {
	calls := 0
	server := historyModel(t, func([]Message) string {
		calls++
		if calls == 3 {
			return `{"action":"chat","text":"Essaie Dark pour une série de SF."}`
		}
		return `{"action":"chat","text":"Tu veux un film ou une série ?"}`
	})
	c := New(server.URL, "", "fixture")
	c.V2 = true
	owner := "34567:11111:33333"
	run := func(context.Context, Plan) (ToolReply, error) {
		t.Error("clarification called a tool")
		return ToolReply{}, nil
	}
	first, err := c.ReplyWithTools(context.Background(), owner, "Fais des propals", false, run)
	if err != nil {
		t.Fatal(err)
	}
	c.Delivered(owner, "50001", first)
	reply, err := c.ReplyWithTools(context.Background(), owner, "Une série de SF", false, run)
	if err != nil || !strings.Contains(reply, "Dark") || calls != 3 {
		t.Fatalf("reply=%q err=%v calls=%d", reply, err, calls)
	}
}

func TestAccountAcknowledgementResumesReadsOnlyOnce(t *testing.T) {
	for _, action := range []string{"search", "prepare"} {
		t.Run(action, func(t *testing.T) {
			calls, tools := 0, 0
			server := historyModel(t, func([]Message) string {
				calls++
				if calls == 1 {
					return `{"action":"` + action + `","query":"Arrival","kind":"movie"}`
				}
				return `{"action":"chat","text":"Il reste un blocage côté Hub."}`
			})
			c := New(server.URL, "", "fixture")
			c.V2 = true
			owner := "34567:11111:33333"
			run := func(_ context.Context, p Plan) (ToolReply, error) {
				tools++
				if p.Action != action || p.Query != "Arrival" {
					t.Errorf("wrong plan: %+v", p)
				}
				return ToolReply{Issue: ToolPlexAccessRequired, Content: "Discord est lié. Reconnecte-toi avec Plex."}, nil
			}
			first, err := c.ReplyWithTools(context.Background(), owner, "Arrival sur Plex", false, run)
			if err != nil || tools != 1 {
				t.Fatal(first, err, tools)
			}
			c.Delivered(owner, "50001", first)
			reply, err := c.ReplyWithContext(context.Background(), owner, "C’est fait cheffe", false, "Eris (message 50001) : "+first, run)
			if action == "search" {
				if err != nil || calls != 1 || tools != 2 || !strings.Contains(reply, "Discord est bien lié") || strings.Contains(reply, "Reconnecte-toi") {
					t.Fatalf("read was not rechecked: %q %v model=%d tool=%d", reply, err, calls, tools)
				}
				c.Delivered(owner, "50002", reply)
				_, err = c.ReplyWithTools(context.Background(), owner, "C’est fait", false, run)
				if err != nil || calls != 2 || tools != 2 {
					t.Fatalf("read retried indefinitely: %v model=%d tool=%d", err, calls, tools)
				}
			} else if err != nil || calls != 2 || tools != 1 {
				t.Fatalf("write plan resumed: %q %v model=%d tool=%d", reply, err, calls, tools)
			}
		})
	}
}
