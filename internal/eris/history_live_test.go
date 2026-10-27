package eris

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Fixtures simulate past deliveries without spending forty inference calls on
// filler. The current answer and rolling summary use the real selected model.
func TestERIS01ExtendedHistory(t *testing.T) {
	url, model := os.Getenv("ERIS_DIALOGUE_TEST_URL"), os.Getenv("ERIS_DIALOGUE_MODEL")
	if url == "" {
		t.Skip("synthetic extended history lab not enabled")
	}
	if model == "" {
		t.Fatal("select a lab model explicitly")
	}
	c := New(url, "", model)
	c.V2 = true
	owner := "10000:20000:30000"
	history := historyFixture(36)
	history[0].Content = "Mon projet s'appelle Cassiopée. La prochaine étape prévue est le login par mail."
	c.sessions[owner] = &conversation{messages: history, expires: time.Now().Add(time.Hour)}
	turn := 0
	ask := func(question string) string {
		t.Helper()
		turn++
		text, err := c.ReplyWithDiscordContext(context.Background(), owner, question, false, "", memoryInput(fmt.Sprintf("%05d", 40000+turn), "20000"), func(context.Context, Plan) (ToolReply, error) {
			t.Error("history discussion invoked media")
			return ToolReply{}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		c.Delivered(owner, fmt.Sprintf("%05d", 50000+turn), text)
		t.Log(question, "=>", text)
		return text
	}
	check := func(text, step string) {
		t.Helper()
		lower := strings.ToLower(text)
		if !strings.Contains(lower, "cassiopée") || !strings.Contains(lower, step) {
			t.Error("project or pending step lost", text)
		}
	}
	check(ask("Quel est mon projet et quelle est la prochaine étape prévue ?"), "mail")
	if len(c.sessions[owner].summary.Facts) != 0 {
		t.Fatal("short messages summarized before forty exchanges")
	}
	// Cross the eighty-message watermark, with the important source outside the
	// retained thirty-exchange batch. These are simulated already-delivered turns.
	c.sessions[owner].messages = append(c.sessions[owner].messages, Message{Role: "user", Content: "Correction : pour Cassiopée, la prochaine étape est maintenant la connexion Plex. Le login par mail est abandonné."}, Message{Role: "assistant", Content: "La connexion Plex remplace donc le login par mail."})
	c.sessions[owner].messages = append(c.sessions[owner].messages, historyFixture(6)...)
	check(ask("On reprend : quel projet et quelle prochaine étape avions-nous décidés ?"), "plex")
	summary := c.sessions[owner].summary
	if len(summary.Facts) == 0 || summary.Extractive {
		t.Fatal("real model did not produce a grounded rolling summary")
	}
	for _, m := range c.sessions[owner].messages {
		if strings.Contains(m.Content, "Mon projet s'appelle Cassiopée") {
			t.Fatal("important source never left raw history")
		}
	}
	// Make the correction leave the recent window too. The second compaction
	// must merge it into the previous summary instead of retaining the old plan.
	c.sessions[owner].messages = append(c.sessions[owner].messages, historyFixture(40)...)
	check(ask("Après tous ces échanges, rappelle-moi le projet et l'étape actuelle."), "plex")
	summary = c.sessions[owner].summary
	if summary.Extractive {
		t.Fatal("second model compaction failed")
	}
	// Persist only the summary and an unrelated source turn in an isolated DB;
	// this avoids the episodic journal answering the final question in its place.
	m := openTestMemory(t)
	id := observeTest(t, m, owner, "Transmission synthétique du résumé.", "70001")
	if err := m.saveSummary(owner, summary, id, time.Now()); err != nil {
		t.Fatal(err)
	}
	c = New(url, "", model)
	c.V2, c.Memory = true, m
	check(ask("De quel projet parlait-on et quelle était la prochaine étape ?"), "plex")
	if len(c.sessions[owner].summary.Facts) == 0 {
		t.Fatal("summary not restored from SQLite")
	}
	if _, err := c.Reply(context.Background(), "10000:20000", "", true); err != nil {
		t.Fatal(err)
	}
	remaining, err := m.loadSummary(owner)
	if err != nil || len(remaining.Facts) != 0 {
		t.Fatal("summary survived erasure", err)
	}
}
