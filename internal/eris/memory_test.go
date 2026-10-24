package eris

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemoryPersistenceScopeEvolutionAndErasure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	m, err := OpenMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	w := &whim{Topic: "grille pain", Quote: "Je défends les grille-pain."}
	if err = m.commit("guild:alice:salon", "message-1", w, nil, now); err != nil {
		t.Fatal(err)
	}
	m.Close()
	m, err = OpenMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, owner := range []string{"guild:alice", "guild:bob", "guild:alice:ailleurs", "other:alice:salon"} {
		got, err := m.recall(owner, "grille pain", now)
		if err != nil || got != nil {
			t.Fatalf("scope leak %s %+v %v", owner, got, err)
		}
	}
	got, err := m.recall("guild:bob:salon", "grille pain", now)
	if err != nil || got == nil || got.Source != "message-1" {
		t.Fatal(got, err)
	}
	if unrelated, _ := m.recall("guild:bob:salon", "un film ce soir", now); unrelated != nil {
		t.Fatal("unrelated callback")
	}
	if cooled, _ := m.recall("guild:bob:salon", "un grille", now); cooled != nil {
		t.Fatal("cooldown ignored")
	}
	if cooled, _ := m.recall("guild:bob:salon", "un grille", now.Add(7*time.Hour)); cooled == nil {
		t.Fatal("cooldown never expires")
	}
	changed := &whim{Topic: w.Topic, Quote: "Je ne défends plus les grille-pain."}
	if err = m.commit("guild:bob:salon", "message-2", changed, got, now); err != nil {
		t.Fatal(err)
	}
	got, _ = m.recall("guild:bob:salon", "grille pain", now)
	if got == nil || got.Quote != changed.Quote {
		t.Fatal("opinion not replaced")
	}
	if err = m.forget("guild:alice"); err != nil {
		t.Fatal(err)
	}
	if got, _ = m.recall("guild:bob:salon", "grille pain", now); got != nil {
		t.Fatal("derived memory survived contributor forgetting")
	}
}

func TestMemoryRetirementExpiryAndCapacity(t *testing.T) {
	m, err := OpenMemory(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	now := time.Now()
	owner := "g:a:c"
	w := &whim{Topic: "cornichons", Quote: "Je protège les cornichons."}
	if err = m.commit(owner, "1", w, nil, now); err != nil {
		t.Fatal(err)
	}
	got, _ := m.recall(owner, "cornichons", now)
	if err = m.commit(owner, "2", &whim{Topic: w.Topic, Retire: true}, got, now); err != nil {
		t.Fatal(err)
	}
	if err = m.commit(owner, "3", w, nil, now); err != nil {
		t.Fatal(err)
	}
	if got, _ = m.recall(owner, "cornichons", now); got != nil {
		t.Fatal("retired whim revived")
	}
	for i := 0; i < 40; i++ {
		if err = m.commit(owner, "4", &whim{Topic: fmt.Sprintf("sujet %02d", i), Quote: w.Quote}, nil, now); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err = m.db.QueryRow(`SELECT count(*) FROM eris_whims`).Scan(&n); err != nil || n != 32 {
		t.Fatal(n, err)
	}
	if got, _ = m.recall(owner, "sujet 39", now.Add(31*24*time.Hour)); got != nil {
		t.Fatal("expired recall")
	}
}

func TestCharacterMetadataCannotBecomeAnAction(t *testing.T) {
	_, valid, err := parseCharacterPlan(`{"action":"chat","text":"Oui : les grille-pain ont une âme, bordel.","memory":{"topic":"grille-pain","quote":"Les grille-pain ont une âme."}}`, nil)
	if err != nil || valid == nil {
		t.Fatal("punctuation-only change rejected", err)
	}
	for _, raw := range []string{
		`{"action":"chat","text":"Bonjour","memory":{"topic":"grille pain","quote":"phrase jamais dite"}}`,
		`{"action":"prepare","query":"Moon","kind":"movie","memory":{"topic":"grille pain","quote":"Je défends les grille-pain."}}`,
		`{"action":"chat","text":"Bonjour","memory":{"topic":"grille pain","retire":true}}`,
	} {
		_, w, err := parseCharacterPlan(raw, nil)
		if err != nil || w != nil {
			t.Fatal(w, err)
		}
	}
	if _, _, err := parseCharacterPlan(`{"action":"chat","text":"Salut","shell":"rm -rf"}`, nil); err == nil {
		t.Fatal("unknown action field accepted")
	}
}

func TestPublicReferenceGroundsTitleWithoutImportingOtherHistory(t *testing.T) {
	var prompt []Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Messages []Message }
		json.NewDecoder(r.Body).Decode(&body)
		prompt = body.Messages
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"prepare","query":"Coherence","kind":"movie"}`}}}})
	}))
	defer server.Close()
	c := New(server.URL, "", "model")
	c.V2 = true
	c.sessions["guild:alice"] = &conversation{expires: time.Now().Add(time.Hour), messages: []Message{{Role: "user", Content: "private secret"}}}
	called := false
	_, err := c.ReplyWithContext(context.Background(), "guild:bob:salon", "demande celui-là", false, "Eris (message 88888) : Coherence est un bon choix.", func(_ context.Context, p Plan) (ToolReply, error) {
		called = true
		return ToolReply{Content: "Choisis Coherence."}, nil
	})
	if err != nil || !called {
		t.Fatal("reference title not grounded", err)
	}
	for _, msg := range prompt {
		if strings.Contains(msg.Content, "private secret") {
			t.Fatal("private history leaked")
		}
	}
}

func TestMemoryOnlyAfterDeliveryAndNotTitleGrounding(t *testing.T) {
	m, err := OpenMemory(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	raw := `{"action":"chat","text":"Je défends les grille-pain.","memory":{"topic":"grille pain","quote":"Je défends les grille-pain."}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: raw}}}})
	}))
	defer server.Close()
	c := New(server.URL, "", "model")
	c.V2 = true
	c.Memory = m
	run := func(context.Context, Plan) (ToolReply, error) {
		t.Fatal("unexpected media execution")
		return ToolReply{}, nil
	}
	text, err := c.ReplyWithTools(context.Background(), "g:a:c", "une lubie ?", false, run)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := m.recall("g:b:c", "grille pain", time.Now()); got != nil {
		t.Fatal("saved before delivery")
	}
	c.Delivered("g:a:c", "1", "different text")
	if got, _ := m.recall("g:b:c", "grille pain", time.Now()); got != nil {
		t.Fatal("wrong delivery")
	}
	c.Delivered("g:a:c", "1", text)
	if got, _ := m.recall("g:b:c", "grille pain", time.Now()); got == nil {
		t.Fatal("not saved after delivery")
	}
	raw = `{"action":"prepare","query":"grille pain","kind":"movie"}`
	text, err = c.ReplyWithTools(context.Background(), "g:b:c", "demande ça, grille pain", false, func(context.Context, Plan) (ToolReply, error) {
		return ToolReply{Content: "allowed explicit title"}, nil
	})
	if err != nil || text != "allowed explicit title" {
		t.Fatal(text, err)
	}
	// A fresh owner has no grounded title; memory is system context, not a title source.
	c.sessions["g:d:c"] = &conversation{expires: time.Now().Add(time.Hour)}
	raw = `{"action":"prepare","query":"Moon","kind":"movie"}`
	if err = m.commit("g:a:c", "2", &whim{Topic: "cornichons", Quote: "Je préfère Moon aux cornichons."}, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	text, err = c.ReplyWithTools(context.Background(), "g:d:c", "demande ça, ces cornichons", false, run)
	if err != nil || !strings.Contains(text, "Quel titre") {
		t.Fatal(text, err)
	}
}

func TestForgetCancelsPendingDeliveryAndReportsStorageFailure(t *testing.T) {
	m, err := OpenMemory(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	c := New("", "", "")
	c.Memory = m
	c.sessions["g:a:c"] = &conversation{expires: time.Now().Add(time.Hour), delivery: "reply", whim: &whim{Topic: "grille pain", Quote: "Je défends les grille-pain."}}
	if _, err = c.Reply(context.Background(), "g:a", "", true); err != nil {
		t.Fatal(err)
	}
	c.Delivered("g:a:c", "1", "reply")
	if got, _ := m.recall("g:a:c", "grille pain", time.Now()); got != nil {
		t.Fatal("resurrected after forgetting")
	}
	m.Close()
	text, err := c.Reply(context.Background(), "g:a", "", true)
	if err != nil || !strings.Contains(text, "ne peux pas confirmer") {
		t.Fatal("false erasure confirmation", text, err)
	}
}

func TestForgetDuringAnotherMembersGenerationCannotResurrectMemory(t *testing.T) {
	m, err := OpenMemory(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"chat","text":"Je défends les grille-pain.","memory":{"topic":"grille pain","quote":"Je défends les grille-pain."}}`}}}})
	}))
	defer server.Close()
	c := New(server.URL, "", "model")
	c.V2 = true
	c.Memory = m
	done := make(chan error, 1)
	go func() {
		text, err := c.ReplyWithTools(context.Background(), "g:b:c", "grille pain ?", false, func(context.Context, Plan) (ToolReply, error) { return ToolReply{}, nil })
		if err == nil {
			c.Delivered("g:b:c", "1", text)
		}
		done <- err
	}()
	<-started
	if _, err = c.Reply(context.Background(), "g:a", "", true); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if got, _ := m.recall("g:b:c", "grille pain", time.Now()); got != nil {
		t.Fatal("in-flight memory resurrected after forgetting")
	}
}
