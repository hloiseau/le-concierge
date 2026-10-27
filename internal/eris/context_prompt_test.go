package eris

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDurablePromptBudgetCoversBoundedAndDeduplicatedData(t *testing.T) {
	for _, text := range []string{"court", strings.Repeat("x", 6000), strings.Repeat("漢字😭", 2500)} {
		bundle := memoryBundle{Notes: []durableNote{{memoryNote: memoryNote{Topic: "test", Summary: strings.Repeat("préférence ", 200)}}}}
		for i := 0; i < 5; i++ {
			bundle.Episodes = append(bundle.Episodes, memoryTurn{Author: Speaker{ID: "20000"}, Question: text, Reply: text})
		}
		budget := bundle.promptBudget()
		for _, rendered := range []memoryBundle{bundle, bundle.withoutRecent("20000", []Message{{Role: "user", Content: text}, {Role: "assistant", Content: text}})} {
			if utf8.RuneCountInString(rendered.context()) > budget {
				t.Fatal("quoted memory exceeded the reserved prompt budget")
			}
		}
	}
	if (memoryBundle{}).promptBudget() != 0 {
		t.Fatal("empty memory reserved context")
	}
}

func TestDialogueVoiceKeepsCurrentMessageAndTransientImages(t *testing.T) {
	messages := []Message{{Role: "system", Content: "action contract"}, {Role: "user", Content: "ancienne question"}, {Role: "assistant", Content: "ancienne réponse"}, {Role: "user", Content: "et cette image ?", Images: []string{"data:image/jpeg;base64,test"}}}
	copyBefore := append([]Message(nil), messages...)
	c := &Chat{V2: true}
	got := c.dialoguePrompt(messages)
	if len(got) != len(messages)+1 || got[len(got)-2].Content != conversationVoice || !reflect.DeepEqual(got[len(got)-1], messages[len(messages)-1]) || !reflect.DeepEqual(messages, copyBefore) {
		t.Fatal("voice changed message order, current media or caller history")
	}
	c.V2 = false
	if !reflect.DeepEqual(c.dialoguePrompt(messages), messages) {
		t.Fatal("legacy conversation changed")
	}
}

func TestQuotedLabHistoryKeepsLatestRequestAndPastFacts(t *testing.T) {
	messages := []Message{{Role: "developer", Content: "rules"}, {Role: "user", Content: "Je choisis Alien."}, {Role: "assistant", Content: "Alien, noté."}, {Role: "developer", Content: conversationVoice}, {Role: "user", Content: "Demande-le."}}
	got := quotedLabHistory(messages)
	if len(got) != 4 || got[3].Content != "Demande-le." || got[3].Role != "user" || got[2].Content != conversationVoice || !strings.Contains(got[1].Content, "Je choisis Alien.") || !strings.Contains(got[1].Content, "Alien, noté.") {
		t.Fatal("ablation lost order, current request or cited facts", got)
	}
}

func TestRecentDialogueNotRepeatedAsMemory(t *testing.T) {
	var prompts [][]Message
	const previous = "Le réglage Vesper fonctionne mieux."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Messages []Message }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		prompts = append(prompts, body.Messages)
		raw, _ := json.Marshal(Plan{Action: "chat", Text: previous})
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: string(raw)}}}})
	}))
	defer server.Close()
	c := New(server.URL, "", "test")
	c.V2, c.Memory = true, openTestMemory(t)
	const owner = "10000:20000:30000"
	for i, question := range []string{"Le réglage Vesper marche ?", "Il fonctionne mieux ?"} {
		incoming := memoryInput([]string{"40000", "40001"}[i], "20000")
		reply, err := c.ReplyWithDiscordContext(context.Background(), owner, question, false, "", incoming, func(context.Context, Plan) (ToolReply, error) {
			t.Error("unexpected tool")
			return ToolReply{}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		c.Delivered(owner, []string{"50000", "50001"}[i], reply)
	}
	count := 0
	for _, m := range prompts[1] {
		if strings.Contains(m.Content, previous) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("previous assistant reply appears in %d prompt messages; want one chronological occurrence", count)
	}
}

func TestDurableEpisodeDedupKeepsOtherAuthorsAndMissingContext(t *testing.T) {
	bundle := memoryBundle{Episodes: []memoryTurn{{Author: Speaker{ID: "20000"}, Question: "Vesper ?", Reply: "Oui."}, {Author: Speaker{ID: "20001"}, Question: "Vesper ?", Reply: "Oui."}}}
	for _, tc := range []struct {
		name    string
		history []Message
		want    int
	}{
		{"same author complete exchange", []Message{{Role: "user", Content: "Vesper ?"}, {Role: "assistant", Content: "Oui."}}, 1},
		{"after restart", nil, 2},
		{"question only", []Message{{Role: "user", Content: "Vesper ?"}}, 2},
		{"tool result", []Message{{Role: "user", Content: "Vesper ?"}, {Role: "assistant", Content: "Oui.", toolResult: true}}, 2},
		{"undelivered reply", []Message{{Role: "user", Content: "Vesper ?"}, {Role: "assistant", Content: "Oui.", unconfirmed: true}}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := bundle.withoutRecent("20000", tc.history)
			if len(got.Episodes) != tc.want || len(bundle.Episodes) != 2 {
				t.Fatal("lost source or changed original bundle", got)
			}
		})
	}
}
