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

func memoryInput(message, member string) DiscordInput {
	return DiscordInput{MessageID: message, Speaker: Speaker{ID: member, Name: "Nom " + member, Username: "user" + member}}
}

func openTestMemory(t *testing.T) *Memory {
	t.Helper()
	m, err := OpenMemory(filepath.Join(t.TempDir(), "eris.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

func observeTest(t *testing.T, m *Memory, owner, text, mid string) int64 {
	t.Helper()
	member := strings.Split(owner, ":")[1]
	id, err := m.observe(owner, text, memoryInput(mid, member), time.Now())
	if err != nil || id == 0 {
		t.Fatal(id, err)
	}
	return id
}

func TestDurableMemorySurvivesRestartAndThirtyDaysWithSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eris.db")
	m, err := OpenMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := "10000:20000:30000"
	text := "Je préfère les films d'épouvante aux comédies."
	in := memoryInput("40001", "20000")
	id, err := m.observe(owner, text, in, time.Now().Add(-400*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	note := memoryNote{Kind: "preference", Topic: "cinéma", Quote: text, Summary: "Préférence pour les films d'épouvante.", Aliases: []string{"horreur"}}
	if err = m.deliverTurn(id, owner, "J'approuve ce goût.", []memoryNote{note}); err != nil {
		t.Fatal(err)
	}
	if err = m.commit(owner, "50000", &whim{Topic: "cornichons", Quote: "Je protège les cornichons."}, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	m.Close()
	m, err = OpenMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, query := range []string{"cinema", "horreur", "épouvante", "coméd"} {
		got, err := m.recallDurable(owner, query, in, 0)
		if err != nil || len(got.Notes) != 1 || got.Notes[0].Source != "40001" || got.Notes[0].Author.ID != "20000" || got.Notes[0].Quote != text {
			t.Fatalf("%s: %+v %v", query, got, err)
		}
		if query == "coméd" && len(got.Episodes) != 1 {
			t.Fatal("prefix recall lost durable episode")
		}
	}
	if whim, err := m.recall(owner, "cornichons", time.Now()); err != nil || whim == nil {
		t.Fatal("legacy notebook was lost", err)
	}
}

func TestDurableMemoryVisibilityAndIdentity(t *testing.T) {
	m := openTestMemory(t)
	owner := "10000:20000:30000"
	id := observeTest(t, m, owner, "Mon projet s'appelle Vesper.", "40001")
	if err := m.deliverTurn(id, owner, "Vesper, retenu pour cette discussion.", nil); err != nil {
		t.Fatal(err)
	}
	for _, other := range []string{"10000:20000", "10000:20001", "10000:20000:30001", "10001:20000:30000"} {
		member := strings.Split(other, ":")[1]
		b, err := m.recallDurable(other, "Vesper", memoryInput("40002", member), 0)
		if err != nil || len(b.Episodes) != 0 || len(b.Notes) != 0 {
			t.Fatal("scope leak", other, b, err)
		}
	}
	b, err := m.recallDurable("10000:20001:30000", "Vesper", memoryInput("40002", "20001"), 0)
	if err != nil || len(b.Episodes) != 1 || b.Episodes[0].Author.ID != "20000" {
		t.Fatal("public author lost", b, err)
	}
	for _, in := range []DiscordInput{{}, memoryInput("40002", "20001"), memoryInput("invalid", "20000")} {
		if id, err := m.observe(owner, "Une information inventée.", in, time.Now()); err != nil || id != 0 {
			t.Fatal("unverified input stored", id, err)
		}
	}
	if id2 := observeTest(t, m, owner, "Mon projet s'appelle Vesper.", "40001"); id2 != id {
		t.Fatal("message replay duplicated")
	}
	if id, err := m.observe("10000:20001:30000", "usurpation", memoryInput("40001", "20001"), time.Now()); err != nil || id != 0 {
		t.Fatal("source identity overwritten", id, err)
	}
}

func TestDurableNoteCorrectionCannotBeReversedByLateDelivery(t *testing.T) {
	m := openTestMemory(t)
	owner := "10000:20000:30000"
	old := "Je préfère les comédies."
	new := "Finalement je préfère les films d'horreur."
	first := observeTest(t, m, owner, old, "40001")
	second := observeTest(t, m, owner, new, "40002")
	note := memoryNote{Kind: "preference", Topic: "cinéma", Quote: new, Summary: new}
	if err := m.deliverTurn(second, owner, "Changement de goût.", []memoryNote{note}); err != nil {
		t.Fatal(err)
	}
	note.Quote, note.Summary = old, old
	if err := m.deliverTurn(first, owner, "Ancienne réplique livrée tard.", []memoryNote{note}); err != nil {
		t.Fatal(err)
	}
	b, err := m.recallDurable(owner, "cinéma", memoryInput("40003", "20000"), 0)
	if err != nil || len(b.Notes) != 1 || b.Notes[0].Quote != new || b.Notes[0].Source != "40002" {
		t.Fatal("late delivery restored old preference", b, err)
	}
	b, _ = m.recallDurable(owner, "comédies horreur", memoryInput("40003", "20000"), 0)
	if len(b.Episodes) != 2 {
		t.Fatal("dated evidence should retain the change", b)
	}
}

func TestDurableForgettingDeletesTransitiveSourcesAndFTS(t *testing.T) {
	m := openTestMemory(t)
	a, b, d := "10000:20000:30000", "10000:20001:30000", "10000:20002:30000"
	first := observeTest(t, m, a, "Je construis Vesper.", "40001")
	if err := m.deliverTurn(first, a, "Un beau projet.", nil); err != nil {
		t.Fatal(err)
	}
	second := observeTest(t, m, b, "J'en pense autant.", "40002")
	bundle, _ := m.recallDurable(b, "Vesper", memoryInput("40002", "20001"), second)
	if err := m.derive(second, b, bundle); err != nil {
		t.Fatal(err)
	}
	if err := m.deliverTurn(second, b, "Vesper m'amuse.", nil); err != nil {
		t.Fatal(err)
	}
	third := observeTest(t, m, d, "Encore un avis.", "40003")
	if err := m.deriveTurns(third, []int64{second}); err != nil {
		t.Fatal(err)
	}
	if err := m.commit(d, "50000", &whim{Topic: "vesper", Quote: "Vesper est ma lubie."}, nil, time.Now(), third); err != nil {
		t.Fatal(err)
	}
	if err := m.forget("10000:20000"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"eris_turns", "eris_notes", "eris_turn_members", "eris_whims", "eris_whim_members"} {
		var count int
		if err := m.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("derivative survived", table, count, err)
		}
	}
	for _, table := range []string{"eris_turn_search", "eris_note_search"} {
		var count int
		if err := m.db.QueryRow("SELECT count(*) FROM " + table + " WHERE " + table + " MATCH 'vesper'").Scan(&count); err != nil || count != 0 {
			t.Fatal("search index resurrected a forgotten source", table, count, err)
		}
	}
}

func TestDurableMetadataMustQuoteCurrentHumanAndCannotChangePlan(t *testing.T) {
	question := "J'aime les films d'horreur."
	note := `{"kind":"preference","topic":"cinéma","quote":"J'aime les films d'horreur.","summary":"Aime l'horreur.","aliases":["épouvante"]}`
	for _, tc := range []struct {
		metadata string
		count    int
	}{
		{note, 1},
		{strings.Replace(note, "J'aime", "Je hais", 1), 0},
		{strings.Replace(note, `"kind":"preference"`, `"kind":"admin"`, 1), 0},
		{strings.Replace(note, `"kind":"preference"`, `"author":"20001","kind":"preference"`, 1), 0},
	} {
		p, _, notes, err := parseDurablePlan(`{"action":"chat","text":"J'approuve.","memories":[`+tc.metadata+`]}`, nil, question)
		if err != nil || p.Action != "chat" || len(notes) != tc.count {
			t.Fatal(p, notes, err)
		}
	}
	if _, _, _, err := parseDurablePlan(`{"action":"chat","text":"Salut","shell":"rm","memories":[`+note+`]}`, nil, question); err == nil {
		t.Fatal("metadata weakened strict action validation")
	}
	_, _, notes, err := parseDurablePlan(`{"action":"prepare","query":"Alien","kind":"movie","memories":[`+note+`]}`, nil, question)
	if err != nil || len(notes) != 0 {
		t.Fatal("media plan created a personal note", notes, err)
	}
	for _, bad := range []string{`{"action":"recall","query":"Vesper","kind":"movie"}`, `{"action":"recall","query":""}`, `{"action":"recall","query":"Vesper"} {}`} {
		if parseRecall(bad) != "" {
			t.Fatal("invalid lookup accepted")
		}
	}
	if parseRecall(`{"action":"recall","query":"recent"}`) != "recent" {
		t.Fatal("recent lookup rejected")
	}
}

func TestDurableChatDeliveryRestartRecallAndActionBoundary(t *testing.T) {
	m := openTestMemory(t)
	owner, in := "10000:20000:30000", memoryInput("40001", "20000")
	raw := `{"action":"chat","text":"Un goût très respectable.","memories":[{"kind":"preference","topic":"cinéma","quote":"J'aime Alien.","summary":"Aime le film Alien.","aliases":["horreur"]}]}`
	var prompts [][]Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct{ Messages []Message }
		json.NewDecoder(r.Body).Decode(&payload)
		prompts = append(prompts, payload.Messages)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: raw}}}})
	}))
	defer server.Close()
	newChat := func() *Chat { c := New(server.URL, "", "model"); c.V2, c.Memory = true, m; return c }
	c := newChat()
	run := func(context.Context, Plan) (ToolReply, error) {
		t.Fatal("memory authorized media action")
		return ToolReply{}, nil
	}
	text, err := c.ReplyWithDiscordContext(context.Background(), owner, "J'aime Alien.", false, "", in, run)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := m.recallDurable(owner, "Alien", in, 0)
	if len(b.Notes) != 0 || len(b.Episodes) != 1 || b.Episodes[0].Reply != "" {
		t.Fatal("undelivered reply was stored", b)
	}
	c.Delivered(owner, "50001", text)
	c = newChat() // Empty RAM; same database.
	raw = `{"action":"prepare","query":"Alien","kind":"movie"}`
	in.MessageID = "40002"
	text, err = c.ReplyWithDiscordContext(context.Background(), owner, "demande le film dont tu te souviens", false, "", in, run)
	if err != nil || !strings.Contains(text, "Quel titre") {
		t.Fatal(text, err)
	}
	joined := ""
	for _, message := range prompts[len(prompts)-1] {
		joined += message.Content
	}
	if !strings.Contains(joined, "J'aime Alien.") || !strings.Contains(joined, "WEB-07") {
		t.Fatal("missing durable context or canon after restart")
	}
}

func TestDurableRecallOneReadOnlyPass(t *testing.T) {
	for _, final := range []string{`{"action":"chat","text":"Tu construis Vesper."}`, `{"action":"prepare","query":"Alien","kind":"movie"}`, `{"action":"recall","query":"Vesper"}`} {
		t.Run(final, func(t *testing.T) {
			m := openTestMemory(t)
			observeTest(t, m, "10000:20000:30000", "Je construis Vesper.", "40001")
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				raw := `{"action":"recall","query":"Vesper"}`
				if calls > 1 {
					raw = final
				}
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: raw}}}})
			}))
			defer server.Close()
			c := New(server.URL, "", "model")
			c.V2, c.Memory = true, m
			_, err := c.ReplyWithDiscordContext(context.Background(), "10000:20000:30000", "tu te rappelles de mon projet ?", false, "", memoryInput("40002", "20000"), func(context.Context, Plan) (ToolReply, error) {
				t.Fatal("recall executed media action")
				return ToolReply{}, nil
			})
			if err != nil || calls != 2 {
				t.Fatal(calls, err)
			}
		})
	}
}

func TestDurableContextBoundsAndFTSInput(t *testing.T) {
	m := openTestMemory(t)
	owner := "10000:20000:30000"
	for i := 0; i < 20; i++ {
		id := observeTest(t, m, owner, fmt.Sprintf("Vesper sujet %02d %s", i, strings.Repeat("é", 450)), fmt.Sprintf("%05d", 40000+i))
		if err := m.deliverTurn(id, owner, strings.Repeat("x", 1000), nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{"Vesper", `Vesper" OR scope:* NOT (`, strings.Repeat("a OR ", 1000)} {
		b, err := m.recallDurable(owner, query, memoryInput("50000", "20000"), 0)
		if err != nil || len(b.Episodes) > 5 || len(b.context()) > 14500 {
			t.Fatal(err, len(b.context()))
		}
	}
}

func TestDurableOldReplyReferenceInheritsErasure(t *testing.T) {
	m := openTestMemory(t)
	owner := "10000:20000:30000"
	id := observeTest(t, m, owner, "Mon projet s'appelle Vesper.", "40001")
	if err := m.deliverTurn(id, owner, "Vesper est ton projet.", nil, "50001"); err != nil {
		t.Fatal(err)
	}
	other := "10000:20001:30000"
	derived := observeTest(t, m, other, "Ce projet mérite une suite.", "40002")
	if err := m.deriveReference(derived, other, "Eris (message 50001) : Vesper est ton projet."); err != nil {
		t.Fatal(err)
	}
	if err := m.forget("10000:20000"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := m.db.QueryRow("SELECT count(*) FROM eris_turns").Scan(&n); err != nil || n != 0 {
		t.Fatal("old reference survived erasure", n, err)
	}
}
