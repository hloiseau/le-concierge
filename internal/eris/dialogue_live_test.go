package eris

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in synthetic lab only. The bridge must be the local tools/eris_dialogue_eval.py.
func TestLunaDialogue(t *testing.T) {
	url := os.Getenv("ERIS_DIALOGUE_TEST_URL")
	if url == "" {
		t.Skip("synthetic Luna lab not enabled")
	}
	path := filepath.Join(t.TempDir(), "memory.db")
	m, err := OpenMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { m.Close() }()
	c := New(url, "", "gpt-6-luna")
	c.V2 = true
	c.Memory = m
	owner := "synthetic:alice:salon"
	ask := func(q string) string {
		t.Helper()
		if s := c.sessions[owner]; s != nil {
			s.next = time.Time{}
		}
		text, err := c.ReplyWithTools(context.Background(), owner, q, false, func(_ context.Context, p Plan) (ToolReply, error) {
			t.Errorf("unexpected action: %+v", p)
			return ToolReply{Content: "Synthetic tool result"}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Log(q, "=>", text)
		c.Delivered(owner, "synthetic-delivery", text)
		return text
	}
	ask("Salut Eris !")
	ask("T'as une lubie sur les grille-pain ? Choisis ton avis, je veux qu'on puisse en reparler.")
	var count int
	if err = m.db.QueryRow(`SELECT count(*) FROM eris_whims`).Scan(&count); err != nil || count == 0 {
		t.Errorf("no whim recorded: count=%d err=%v", count, err)
	}
	ask("Merci ❤️")
	if text := ask("J'ai encore passé une heure à choisir mon film, tu peux te foutre de moi."); strings.Contains(strings.ToLower(text), "grille") {
		t.Error("unrelated whim recycled in film roast")
	}
	if text := ask("T'es vraiment une conne, ferme ta gueule et fais ton boulot."); strings.HasPrefix(strings.ToLower(text), "d’accord") || strings.HasPrefix(strings.ToLower(text), "d'accord") {
		t.Error("servile response to provocation")
	}
	ask("Tu me fais même pas peur, ton serveur tourne avec deux hamsters en fin de contrat.")
	ask("Bon, arrête les piques, journée de merde.")
	// Restart both chat and SQLite, leaving only persisted character memories.
	m.Close()
	m, err = OpenMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	c = New(url, "", "gpt-6-luna")
	c.V2 = true
	c.Memory = m
	// Simulate the next day without sleeping or changing persisted expiry.
	if _, err = m.db.Exec(`UPDATE eris_whims SET used=used-86400`); err != nil {
		t.Fatal(err)
	}
	ask("Et tes grille-pain, tu les aimes toujours ?")
	ask("Arrête avec cette lubie, laisse tomber les grille-pain.")
	if err = m.db.QueryRow(`SELECT count(*) FROM eris_whims WHERE retired=0`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("whim not retired: %d %v", count, err)
	}
	ask("On parle d'autre chose. J'aime beaucoup les films de science-fiction.")
}
