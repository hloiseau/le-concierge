package eris

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParticipationUpgradeKeepsDurableMemoryAndForgettingKeepsSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eris.db")
	m, err := OpenMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := "10000:20000:30000"
	id := observeTest(t, m, owner, "Mon projet s'appelle Vesper.", "40001")
	if err := m.deliverTurn(id, owner, "Vesper, compris.", nil); err != nil {
		t.Fatal(err)
	}
	// A database from main already has durable memory but no channel settings.
	if _, err := m.db.Exec(`DROP TABLE eris_participation`); err != nil {
		t.Fatal(err)
	}
	m.Close()
	m, err = OpenMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if got, err := m.recallDurable(owner, "Vesper", memoryInput("40002", "20000"), 0); err != nil || len(got.Episodes) != 1 {
		t.Fatal("upgrade lost durable episode", got, err)
	}
	if err := m.SetParticipation("10000", "30000", true); err != nil {
		t.Fatal(err)
	}
	if err := m.forget(owner); err != nil {
		t.Fatal(err)
	}
	if enabled, err := m.Participation("10000", "30000"); err != nil || !enabled {
		t.Fatal("forgetting changed administrator settings", enabled, err)
	}
	if got, err := m.recallDurable(owner, "Vesper", memoryInput("40003", "20000"), 0); err != nil || len(got.Episodes) != 0 {
		t.Fatal("forgotten episode survived", got, err)
	}
	if _, err := m.recall(owner, "Vesper", time.Now()); err != nil {
		t.Fatal("legacy notebook unavailable", err)
	}
}

func TestParticipationSettingsPersistAndStayScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eris.db")
	m, err := OpenMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.SetParticipation("guild", "cinema", true); err != nil {
		t.Fatal(err)
	}
	m.Close()
	m, err = OpenMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, tc := range []struct {
		guild, channel string
		want           bool
	}{{"guild", "cinema", true}, {"guild", "logs", false}, {"other", "cinema", false}} {
		if got, err := m.Participation(tc.guild, tc.channel); err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	if err = m.SetParticipation("guild", "cinema", false); err != nil {
		t.Fatal(err)
	}
	if got, err := m.Participation("guild", "cinema"); err != nil || got {
		t.Fatal(got, err)
	}
}

func TestParticipationUsesOneCallAndCannotProduceTools(t *testing.T) {
	for _, raw := range []string{`{"text":""}`, `{"text":"Ça ressemble à Arrival."}`, `{"action":"prepare","query":"Arrival"}`, `{"text":"ok","action":"search"}`, `{"text":null}`, `{"text":"ok"} {}`} {
		t.Run(raw, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var payload struct{ Messages []Message }
				json.NewDecoder(r.Body).Decode(&payload)
				encoded, _ := json.Marshal(payload)
				if len(payload.Messages) != 3 || strings.Contains(string(encoded), "PRIVATE") || strings.Contains(payload.Messages[0].Content, ToolInstructions) {
					t.Error("borrowed private context/tools")
				}
				if !strings.Contains(payload.Messages[0].Content, CharacterCanon) {
					t.Error("spontaneous response lost ERIS-01 canon")
				}
				if len(payload.Messages) != 3 || payload.Messages[1].Content != conversationVoice {
					t.Error("spontaneous reply lost the shared conversation voice")
				}
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: raw}}}})
			}))
			defer s.Close()
			c := New(s.URL, "", "test")
			c.V2 = true
			c.sessions["private"] = &conversation{messages: []Message{{Role: "user", Content: "PRIVATE"}}}
			text, err := c.Participate(context.Background(), "alice: Quel film avec une linguiste et des aliens ?")
			valid := raw == `{"text":""}` || raw == `{"text":"Ça ressemble à Arrival."}`
			if (err == nil) != valid || calls != 1 || (!valid && text != "") {
				t.Fatal(text, err, calls)
			}
		})
	}
}
