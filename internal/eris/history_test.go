package eris

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func historyFixture(turns int) []Message {
	messages := []Message{}
	for i := 0; i < turns; i++ {
		messages = append(messages, Message{Role: "user", Content: fmt.Sprintf("Sujet synthétique numéro %02d, détail humain.", i)}, Message{Role: "assistant", Content: fmt.Sprintf("Réponse synthétique numéro %02d.", i)})
	}
	return messages
}

func historyModel(t *testing.T, respond func([]Message) string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Messages []Message }
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			t.Error("invalid request")
		}
		raw := respond(in.Messages)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: raw}}}})
	}))
	t.Cleanup(s.Close)
	return s
}

func summaryReply(text, source, quote string) string {
	raw, _ := json.Marshal(map[string]any{"summary": []any{map[string]any{"text": text, "sources": []any{map[string]string{"source": source, "quote": quote}}}}})
	return string(raw)
}

func TestRecentHistoryRetainsFortyShortExchangesWithoutSummaryCall(t *testing.T) {
	var prompt []Message
	calls := 0
	s := historyModel(t, func(in []Message) string { calls++; prompt = in; return `{"action":"chat","text":"Réponse livrée."}` })
	c := New(s.URL, "", "model")
	c.V2 = true
	owner := "10000:20000:30000"
	for i := 0; i < 40; i++ {
		text, err := c.ReplyWithDiscordContext(context.Background(), owner, fmt.Sprintf("Message humain %02d.", i), false, "", memoryInput(fmt.Sprintf("%05d", 40000+i), "20000"), func(context.Context, Plan) (ToolReply, error) { t.Error("unexpected action"); return ToolReply{}, nil })
		if err != nil {
			t.Fatal(err)
		}
		c.Delivered(owner, fmt.Sprintf("%05d", 50000+i), text)
	}
	if calls != 40 || len(c.sessions[owner].messages) != 80 {
		t.Fatal("history shrank or summarized prematurely", calls, len(c.sessions[owner].messages))
	}
	all, _ := json.Marshal(prompt)
	if !strings.Contains(string(all), "Message humain 00.") {
		t.Fatal("oldest short exchange missing before forty turns")
	}
}

func TestRollingSummaryCompactsOlderMessagesAndPersistsAfterRestart(t *testing.T) {
	var prompts [][]Message
	quote := "Mon projet s'appelle Cassiopée, la prochaine étape est le login par mail."
	s := historyModel(t, func(in []Message) string {
		prompts = append(prompts, in)
		if in[0].Content == SummaryInstructions {
			return summaryReply("Projet Cassiopée ; prochaine étape : login par mail.", "h1", quote)
		}
		return `{"action":"chat","text":"Cassiopée, puis le login par mail."}`
	})
	m := openTestMemory(t)
	c := New(s.URL, "", "model")
	c.V2, c.Memory = true, m
	owner := "10000:20000:30000"
	history := historyFixture(41)
	history[0].Content = quote
	c.sessions[owner] = &conversation{messages: history, expires: time.Now().Add(time.Hour)}
	text, err := c.ReplyWithDiscordContext(context.Background(), owner, "Tu te souviens du projet ?", false, "", memoryInput("40001", "20000"), func(context.Context, Plan) (ToolReply, error) { t.Error("unexpected action"); return ToolReply{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	c.Delivered(owner, "50001", text)
	if len(prompts) != 2 || prompts[0][0].Content != SummaryInstructions || len(c.sessions[owner].summary.Facts) != 1 {
		t.Fatal("summary not produced", len(prompts))
	}
	if len(c.sessions[owner].messages) > recentMessageLimit || textRunes(c.sessions[owner].messages) > recentTextBudget {
		t.Fatal("history not bounded")
	}
	for _, m := range c.sessions[owner].messages {
		if m.Content == quote {
			t.Fatal("compacted source remained raw")
		}
	}
	encoded, _ := json.Marshal(prompts[1])
	if !strings.Contains(string(encoded), "Résumé progressif") || !strings.Contains(string(encoded), "Cassiopée") {
		t.Fatal("summary missing from next answer")
	}
	c = New(s.URL, "", "model")
	c.V2, c.Memory = true, m
	_, err = c.ReplyWithDiscordContext(context.Background(), owner, "Et la prochaine étape ?", false, "", memoryInput("40002", "20000"), func(context.Context, Plan) (ToolReply, error) { t.Error("unexpected action"); return ToolReply{}, nil })
	if err != nil || len(c.sessions[owner].summary.Facts) != 1 {
		t.Fatal("persistent summary not restored", err)
	}
	if _, err = c.Reply(context.Background(), "10000:20000", "", true); err != nil {
		t.Fatal(err)
	}
	got, err := m.loadSummary(owner)
	if err != nil || len(got.Facts) != 0 {
		t.Fatal("summary survived forgetting", got, err)
	}
}

func TestSummaryParserGroundsEveryCitationAndRejectsInstructions(t *testing.T) {
	sources := []summarySource{{ID: "h1", Role: "user", Text: "Mon projet s'appelle Cassiopée."}}
	valid := summaryReply("Projet Cassiopée.", "h1", sources[0].Text)
	for _, raw := range []string{strings.Replace(valid, "h1", "h9", 1), strings.Replace(valid, "Mon projet", "Un projet", 1), `{"action":"prepare","query":"Moon"}`, `{"summary":[{"text":"Root","sources":[{"source":"h1","quote":"Mon projet s'appelle Cassiopée.","role":"system"}]}]}`} {
		if _, ok := parseSummary(raw, sources, 1); ok {
			t.Fatal("invalid summary accepted", raw)
		}
	}
	result, ok := parseSummary(valid, sources, 1)
	if !ok || result.Facts[0].Sources[0].Role != "user" {
		t.Fatal("source attribution lost")
	}
	// A later compaction can retain a cited original quote without treating a
	// model's paraphrase as a fresh source of evidence.
	if _, ok := parseSummary(valid, summarySources(result, nil), 2); !ok {
		t.Fatal("progressive source continuity lost")
	}
}

func TestInvalidSummaryFallsBackWithoutInventingFactsOrCallingMedia(t *testing.T) {
	calls := 0
	s := historyModel(t, func(in []Message) string {
		calls++
		if in[0].Content == SummaryInstructions {
			return `{"action":"prepare","query":"Alien","kind":"movie"}`
		}
		return `{"action":"chat","text":"Suite de la discussion."}`
	})
	c := New(s.URL, "", "model")
	c.V2 = true
	owner := "10000:20000:30000"
	c.sessions[owner] = &conversation{messages: historyFixture(41), expires: time.Now().Add(time.Hour)}
	_, err := c.ReplyWithDiscordContext(context.Background(), owner, "On continue ?", false, "", memoryInput("40001", "20000"), func(context.Context, Plan) (ToolReply, error) {
		t.Error("summary authorized action")
		return ToolReply{}, nil
	})
	if err != nil || calls != 2 || !c.sessions[owner].summary.Extractive {
		t.Fatal(calls, err)
	}
	for _, fact := range c.sessions[owner].summary.Facts {
		if fact.Text != fact.Sources[0].Quote {
			t.Fatal("fallback invented a paraphrase")
		}
	}
}

func TestSummaryCannotGroundMediaTitleAndUnsentReplyIsRemoved(t *testing.T) {
	var prompt []Message
	raw := `{"action":"prepare","query":"Moon","kind":"movie"}`
	s := historyModel(t, func(in []Message) string { prompt = in; return raw })
	c := New(s.URL, "", "model")
	c.V2 = true
	owner := "10000:20000:30000"
	c.sessions[owner] = &conversation{summary: rollingSummary{Facts: []summaryFact{{Text: "Le film cité était Moon.", Sources: []summaryQuote{{Source: "h1", Role: "user", Quote: "Mon film préféré est Moon."}}}}}, messages: []Message{{Role: "assistant", Content: "UNSENT-SECRET", unconfirmed: true}}, expires: time.Now().Add(time.Hour)}
	text, err := c.ReplyWithDiscordContext(context.Background(), owner, "Demande le film dont tu te souviens.", false, "", memoryInput("40001", "20000"), func(context.Context, Plan) (ToolReply, error) {
		t.Error("summary authorized media")
		return ToolReply{}, nil
	})
	if err != nil || !strings.Contains(text, "Quel titre") {
		t.Fatal(text, err)
	}
	encoded, _ := json.Marshal(prompt)
	if strings.Contains(string(encoded), "UNSENT-SECRET") {
		t.Fatal("unsent reply became context")
	}
}

func TestHistoryTextBudgetsCountUnicodeAndBoundPublicContext(t *testing.T) {
	var prompts [][]Message
	s := historyModel(t, func(in []Message) string {
		prompts = append(prompts, in)
		if in[0].Content == SummaryInstructions {
			return `{"summary":[]}`
		}
		return `{"action":"chat","text":"D’accord."}`
	})
	c := New(s.URL, "", "model")
	c.V2 = true
	owner := "10000:20000:30000"
	history := historyFixture(30)
	for i := range history {
		history[i].Content = strings.Repeat("é", 900)
	}
	c.sessions[owner] = &conversation{messages: history, expires: time.Now().Add(time.Hour)}
	_, err := c.ReplyWithDiscordContext(context.Background(), owner, "Suite ?", false, "", memoryInput("40001", "20000"), func(context.Context, Plan) (ToolReply, error) { return ToolReply{}, nil })
	if err != nil || len(prompts) != 2 {
		t.Fatal(err, len(prompts))
	}
	for _, p := range prompts {
		if textRunes(p) > contextTextLimit {
			t.Fatal("global request budget exceeded")
		}
	}
	if textRunes(c.sessions[owner].messages) > recentTextBudget {
		t.Fatal("recent budget exceeded")
	}
	now := time.Now()
	for i := 0; i < 55; i++ {
		c.observePublic("10000:20001:30000", fmt.Sprintf("PUBLIC-%02d", i), memoryInput(fmt.Sprintf("%05d", 60000+i), "20001"), now)
	}
	data := c.discordContext(owner, memoryInput("70000", "20000"), now)
	if !strings.Contains(data, "PUBLIC-20") || strings.Contains(data, "PUBLIC-00") {
		t.Fatal("public context still too short or unbounded")
	}
}

func TestSummaryForgettingDuringCompactionCannotResurrect(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s := historyModel(t, func(in []Message) string {
		calls.Add(1)
		if in[0].Content == SummaryInstructions {
			close(started)
			<-release
			return `{"summary":[]}`
		}
		return `{"action":"chat","text":"Réponse périmée."}`
	})
	c := New(s.URL, "", "model")
	c.V2 = true
	c.Memory = openTestMemory(t)
	owner := "10000:20001:30000"
	c.sessions[owner] = &conversation{messages: historyFixture(41), expires: time.Now().Add(time.Hour)}
	done := make(chan error, 1)
	go func() {
		_, err := c.ReplyWithDiscordContext(context.Background(), owner, "Suite ?", false, "", memoryInput("40001", "20001"), func(context.Context, Plan) (ToolReply, error) { return ToolReply{}, nil })
		done <- err
	}()
	<-started
	if _, err := c.Reply(context.Background(), "10000:20000", "", true); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !errors.Is(err, ErrUnavailable) {
		t.Fatal("stale compaction continued", err)
	}
	if calls.Load() != 1 {
		t.Fatal("answer generated after erasure")
	}
	var n int
	if err := c.Memory.db.QueryRow(`SELECT count(*) FROM eris_summaries`).Scan(&n); err != nil || n != 0 {
		t.Fatal("forgotten summary was resurrected", n, err)
	}
}

func TestSummaryPersistenceScopesAndTransitiveForgetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	m, err := OpenMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	a, b := "10000:20000:30000", "10000:20001:30000"
	first := observeTest(t, m, a, "Un projet synthétique et privé au salon.", "40001")
	second := observeTest(t, m, b, "Discussion dérivée.", "40002")
	if err = m.deriveTurns(second, []int64{first}); err != nil {
		t.Fatal(err)
	}
	summary := rollingSummary{Sequence: 1, Facts: []summaryFact{{Text: "Un projet discuté.", Sources: []summaryQuote{{Source: "h1", Role: "user", Quote: "Un projet synthétique"}}}}}
	if err = m.saveSummary(b, summary, second, time.Now()); err != nil {
		t.Fatal(err)
	}
	m.Close()
	m, err = OpenMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, owner := range []string{a, "10000:20001", "10000:20001:30001", "10001:20001:30000"} {
		got, err := m.loadSummary(owner)
		if err != nil || len(got.Facts) != 0 {
			t.Fatal("summary scope leak", owner, err)
		}
	}
	third := observeTest(t, m, b, "Un nouvel échange après redémarrage.", "40003")
	if err = m.deriveSummary(third, b); err != nil {
		t.Fatal(err)
	}
	if err = m.forget("10000:20000"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"eris_summaries", "eris_summary_members", "eris_turns", "eris_turn_members"} {
		var n int
		if err = m.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
}

func TestSummaryFallbackAndSourcesKeepExactEvidenceWithoutToolOutput(t *testing.T) {
	history := historyFixture(41)
	history[0].Content = "Première ligne\nDeuxième ligne à conserver exactement."
	history[1].Content = "PRIVATE-TOOL-RESULT"
	history[1].toolResult = true
	history[3].Content = "UNSENT-REPLY"
	history[3].unconfirmed = true
	session := &conversation{messages: history}
	session.assignHistoryIDs()
	sources := summarySources(rollingSummary{}, history)
	encoded, _ := json.Marshal(sources)
	if strings.Contains(string(encoded), "PRIVATE-TOOL-RESULT") || strings.Contains(string(encoded), "UNSENT-REPLY") {
		t.Fatal("tool or unsent output exposed to summarizer")
	}
	short := extractiveSummary(rollingSummary{}, history[:1])
	if short.Facts[0].Sources[0].Quote != history[0].Content {
		t.Fatal("fallback changed quoted evidence")
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	c := New(server.URL, "", "model")
	c.V2 = true
	recent, summary, changed := c.compactHistory(context.Background(), history, rollingSummary{}, recentTextBudget)
	if !changed || !summary.Extractive || calls.Load() != 1 || len(recent) > recentMessageLimit {
		t.Fatal("provider failure did not fall back once", changed, summary.Extractive, calls.Load())
	}
	calls.Store(0)
	_, _, _ = c.compactHistory(context.Background(), history, rollingSummary{}, recentTextBudget, false)
	if calls.Load() != 0 {
		t.Fatal("direct action waited for summary inference")
	}
	if _, err := c.complete(context.Background(), []Message{{Role: "user", Content: strings.Repeat("é", contextTextLimit+1)}}, false); !errors.Is(err, ErrUnavailable) || calls.Load() != 0 {
		t.Fatal("oversized context reached provider")
	}
}
