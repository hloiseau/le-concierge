package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/eris"
)

func TestConversationKeepsSpeakersHistorySilenceAndSimulatedTools(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload struct{ Messages []eris.Message }
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		raw := `{"action":"chat","text":"Bonjour Hugo."}`
		switch calls {
		case 2:
			all, _ := json.Marshal(payload.Messages)
			if !strings.Contains(string(all), "Bonjour Hugo.") || !strings.Contains(string(all), "couronne") || !strings.Contains(string(all), "Alice") || !strings.Contains(string(all), "Bob") {
				t.Error("lost history or ambient speakers")
			}
			raw = `{"action":"chat","text":"Bonjour Alice."}`
		case 3:
			raw = `{"action":"monitoring"}`
		case 4:
			raw = `{"action":"chat","text":"Nouvelle question."}`
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": raw}}}})
	}))
	defer server.Close()
	c := eris.New(server.URL, "", "test-model")
	c.V2 = true
	memory, err := eris.OpenMemory(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	c.Memory = memory
	input := strings.Join([]string{
		`{"text":"Bonjour"}`,
		`{"speaker":"Bob","text":"Alice a pris la couronne du jeu.","ambient":true}`,
		`{"speaker":"Alice","text":"Tu as entendu Bob ?"}`,
		`{"speaker":"Alice","text":"Merci 👍"}`,
		`{"speaker":"Alice","text":"État du CPU du NAS ?"}`,
		`{"speaker":"Alice","forget":true}`,
		`{"speaker":"Alice","text":"On reprend ?"}`,
	}, "\n")
	var out bytes.Buffer
	if err := converse(strings.NewReader(input), &out, c); err != nil {
		t.Fatal(err)
	}
	var rows []output
	decoder := json.NewDecoder(&out)
	for decoder.More() {
		var row output
		if err := decoder.Decode(&row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	if len(rows) != 8 || !rows[0].Ready || rows[0].Model != "test-model" || !rows[2].Observed || !rows[4].Silent || rows[5].Tool == nil || rows[5].Tool.Action != "monitoring" || calls != 4 {
		t.Fatalf("unexpected conversation: %+v, calls=%d", rows, calls)
	}
	if rows[1].Text != "Bonjour Hugo." || rows[3].Text != "Bonjour Alice." || rows[5].Text != "Outil simulé : aucune action exécutée, aucune donnée réelle consultée." || rows[7].Error != "" {
		t.Fatal(rows)
	}
}

func TestInvalidInputDoesNotCallModelOrLeakProviderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "private-provider-detail", http.StatusUnauthorized)
	}))
	defer server.Close()
	for _, raw := range []string{`{}`, `not json`, `{"text":"ok","unknown":true}`, `{"text":"ok"} {}`, `{"text":"ok","forget":true}`, `{"text":"ok","ambient":true,"explicit":true}`, fmt.Sprintf(`{"text":%q}`, strings.Repeat("é", 501)), `{"text":"ok","speaker":"bad\nname"}`, `{"text":"Qui es-tu ?"}`} {
		c := eris.New(server.URL, "", "test-model")
		c.V2 = true
		var out bytes.Buffer
		if err := converse(strings.NewReader(raw), &out, c); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "private-provider-detail") || strings.Count(out.String(), `"error"`) != 1 {
			t.Fatal(out.String())
		}
		want := "invalid_input"
		if raw == `{"text":"Qui es-tu ?"}` {
			want = "unavailable"
		}
		if !strings.Contains(out.String(), `"error":"`+want+`"`) {
			t.Fatal(out.String())
		}
	}
}
