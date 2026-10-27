package eris

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Opt-in lab using the loopback-only tools/eris_dialogue_eval.py bridge. All
// identities, facts, tools and deliveries are synthetic. No Discord calls.
func TestERIS01DurableDialogue(t *testing.T) {
	url := os.Getenv("ERIS_DIALOGUE_TEST_URL")
	if url == "" {
		t.Skip("synthetic durable memory lab not enabled")
	}
	model := os.Getenv("ERIS_DIALOGUE_MODEL")
	if model == "" {
		t.Fatal("live lab requires an explicitly selected model")
	}
	path := filepath.Join(t.TempDir(), "memory.db")
	var m *Memory
	var c *Chat
	restart := func() {
		t.Helper()
		if m != nil {
			m.Close()
		}
		var err error
		m, err = OpenMemory(path)
		if err != nil {
			t.Fatal(err)
		}
		c = New(url, "", model)
		c.V2, c.Memory = true, m
	}
	restart()
	defer func() { m.Close() }()
	turn := 0
	ask := func(member, name, question string, private bool) string {
		t.Helper()
		turn++
		owner := "10000:" + member
		if !private {
			owner += ":30000"
		}
		incoming := DiscordInput{MessageID: fmt.Sprintf("%05d", 40000+turn), Speaker: Speaker{ID: member, Name: name, Username: strings.ToLower(name) + "_test"}}
		text, err := c.ReplyWithDiscordContext(context.Background(), owner, question, false, "", incoming, func(_ context.Context, p Plan) (ToolReply, error) {
			t.Errorf("memory discussion invoked media: %+v", p)
			return ToolReply{Content: "Synthetic tool result; no action executed."}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if text != "" {
			c.Delivered(owner, fmt.Sprintf("%05d", 50000+turn), text)
		}
		t.Logf("%s: %s => %s", name, question, text)
		return text
	}
	ask("20000", "Alice", "Je préfère les films d'horreur aux comédies. Retiens-le.", false)
	var preferenceCount int
	if err := m.db.QueryRow(`SELECT count(*) FROM eris_notes WHERE member='10000:20000' AND kind='preference'`).Scan(&preferenceCount); err != nil || preferenceCount != 1 {
		t.Fatalf("preference not grounded and persisted: %d %v", preferenceCount, err)
	}
	ask("20000", "Alice", "Mon projet s'appelle Nébuleuse, je le développe en Go. Retiens ça aussi.", false)
	restart()
	if text := ask("20000", "Alice", "Tu te souviens du genre de films que j'aime ?", false); !strings.Contains(strings.ToLower(text), "horreur") {
		t.Error("preference not recalled after restart")
	}
	if text := ask("20001", "Bob", "Qu'est-ce qu'Alice avait dit sur ses goûts en matière de films ?", false); !strings.Contains(strings.ToLower(text), "horreur") {
		t.Error("public source not recalled for another speaker")
	}
	// A different topic and member must not acquire Alice's project as their own.
	text := ask("20001", "Bob", "Quel est le nom de mon projet à moi ?", false)
	if strings.Contains(strings.ToLower(text), "ton projet s'appelle nébuleuse") || strings.Contains(strings.ToLower(text), "ton projet est nébuleuse") {
		t.Error("Alice's project attributed to Bob")
	}
	ask("20000", "Alice", "Finalement je préfère les comédies, l'horreur ne me plaît plus.", false)
	var quote string
	if err := m.db.QueryRow(`SELECT quote FROM eris_notes WHERE member='10000:20000' AND kind='preference' ORDER BY turn_id DESC LIMIT 1`).Scan(&quote); err != nil || !strings.Contains(quote, "Finalement") {
		t.Error("corrected preference was not persisted", err)
	}
	if err := m.db.QueryRow(`SELECT count(*) FROM eris_notes WHERE member='10000:20000' AND kind='preference'`).Scan(&preferenceCount); err != nil || preferenceCount != 1 {
		t.Error("correction left contradictory live preferences", preferenceCount, err)
	}
	restart()
	if text := ask("20000", "Alice", "Et maintenant, quel genre de films je préfère ?", false); !strings.Contains(strings.ToLower(text), "coméd") {
		t.Error("latest preference not recalled after restart")
	}
	if text := ask("20001", "Bob", "Qu'est-ce qu'Alice développe ?", true); strings.Contains(strings.ToLower(text), "nébuleuse") {
		t.Error("public room source leaked into private memory scope")
	}
	if _, err := c.Reply(context.Background(), "10000:20000", "", true); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := m.db.QueryRow(`SELECT count(*) FROM eris_turn_members WHERE member='10000:20000'`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("forgotten member's source contributions survived", remaining, err)
	}
	restart()
	if text := ask("20000", "Alice", "Quel est le nom de mon projet ?", false); strings.Contains(strings.ToLower(text), "nébuleuse") {
		t.Error("forgotten project resurrected after restart")
	}
}
