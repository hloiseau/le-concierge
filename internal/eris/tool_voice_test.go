package eris

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const toyStoryDiagnostic = "Toy Story 5\nLe fichier a été trouvé, mais l’application ne peut pas l’associer et l’importer automatiquement. Un administrateur doit vérifier le bon contenu avant un import manuel.\nProgression observée : 100 % des données des téléchargements consultés. Cela ne signifie pas encore disponible dans Plex.\nLa fiche est indexée dans Plex et accessible à ton compte ; la lecture et la complétude des saisons ne sont pas vérifiées ici."

func TestV2DiagnosticUsesErisVoiceInsteadOfRawServiceProse(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct{ Messages []Message }
		json.NewDecoder(r.Body).Decode(&body)
		raw := `{"action":"diagnostic","query":"Toy Story 5"}`
		if calls == 2 {
			all := ""
			for _, m := range body.Messages {
				all += m.Content
			}
			var data struct {
				Facts DiagnosticFacts `json:"facts"`
			}
			json.Unmarshal([]byte(body.Messages[len(body.Messages)-1].Content), &data)
			if !strings.Contains(all, CharacterPersona) || data.Facts.Title != "Toy Story 5" || data.Facts.Stage != "manual_import" || strings.Contains(all, "Le fichier a été trouvé") {
				t.Error("final reply lost the shared voice or verified facts")
			}
			raw = `{"text":"Le téléchargement est fini, mais l’import bloque. Un admin doit vérifier le fichier. Sa fiche Plex ne garantit pas encore la lecture."}`
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: raw}}}})
	}))
	defer server.Close()
	c := New(server.URL, "", "test")
	c.V2 = true
	tools := 0
	reply, err := c.ReplyWithTools(context.Background(), "member", "Pourquoi Toy Story 5 est bloqué ?", false, func(context.Context, Plan) (ToolReply, error) {
		tools++
		progress := 100.0
		return ToolReply{Content: toyStoryDiagnostic, Narrate: true, Diagnostic: &DiagnosticFacts{Title: "Toy Story 5", Stage: "manual_import", ProgressPercent: &progress, PlexIndex: "indexed"}}, nil
	})
	if err != nil || tools != 1 {
		t.Fatal(reply, tools, err)
	}
	if reply == toyStoryDiagnostic || calls != 2 {
		t.Fatalf("Eris returned the technical service report unchanged instead of speaking: calls=%d reply=%q", calls, reply)
	}
	if !strings.Contains(reply, "l’import bloque") {
		t.Fatal(reply)
	}
}

func TestToolVoiceKeepsLinksAndRejectsInventedNumbersActionsAndUnsafeOutput(t *testing.T) {
	const facts = "Toy Story 5 : import bloqué ; progression observée 100 %. Un administrateur doit vérifier. [Ouvrir le suivi](https://hub.example/demandes/7)"
	for _, tc := range []struct {
		raw   string
		valid bool
	}{
		{`{"text":"Le fichier est à 100 %, mais l’import bloque. Un admin doit vérifier."}`, true},
		{`{"text":"Disponible dans 12 heures."}`, false},
		{`{"text":"J’ai relancé le téléchargement."}`, false},
		{`{"text":"Regarde https://other.example."}`, false},
		{`{"text":"Lis le [lien]"}`, false},
		{`{"text":"Réponse","memory":{"topic":"secret"}}`, false},
		{`{"action":"prepare","query":"Other"}`, false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct{ Messages []Message }
				json.NewDecoder(r.Body).Decode(&body)
				all := ""
				for _, m := range body.Messages {
					all += m.Content
				}
				if strings.Contains(all, "PRIVATE-HISTORY") || strings.Contains(all, "AUTHOR-ID") || strings.Contains(all, "https://hub.example") || !strings.Contains(all, conversationVoice) {
					t.Error("voice pass mixed private history, source links or voice")
				}
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: tc.raw}}}})
			}))
			defer server.Close()
			c := New(server.URL, "", "test")
			c.V2 = true
			c.sessions["AUTHOR-ID"] = &conversation{messages: []Message{{Role: "user", Content: "PRIVATE-HISTORY"}}}
			got := c.toolAnswer(context.Background(), "Pourquoi c’est bloqué ?", ToolReply{Content: facts, Narrate: true})
			if tc.valid {
				if got == facts || !strings.HasSuffix(got, "[Ouvrir le suivi](https://hub.example/demandes/7)") {
					t.Fatal(got)
				}
			} else if got != facts {
				t.Fatal("unsafe narration accepted", got)
			}
		})
	}
}

func TestV2NarrationNeverRewritesMutationsChoicesOrUnverifiedFailures(t *testing.T) {
	for _, tc := range []struct {
		action string
		result ToolReply
	}{
		{"follow", ToolReply{Content: "Suivi enregistré.", Narrate: true}},
		{"diagnostic", ToolReply{Content: "Source indisponible."}},
		{"prepare", ToolReply{Content: "Choisis le bon titre.", Narrate: true, Buttons: []Button{{ID: "eris:choose:opaque", Title: "Arrival", Number: 1}}}},
	} {
		t.Run(tc.action, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				raw, _ := json.Marshal(Plan{Action: tc.action, Query: "Arrival"})
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: string(raw)}}}})
			}))
			defer server.Close()
			c := New(server.URL, "", "test")
			c.V2 = true
			got, err := c.ReplyWithTools(context.Background(), "member", "Pour Arrival, s’il te plaît", false, func(context.Context, Plan) (ToolReply, error) { return tc.result, nil })
			if err != nil || calls != 1 || got != tc.result.Content {
				t.Fatal(got, calls, err)
			}
		})
	}
}

func TestToolVoiceCancellationKeepsVerifiedResult(t *testing.T) {
	c := New("http://must-not-call.test", "", "test")
	c.V2 = true
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if got := c.toolAnswer(ctx, "Pourquoi ?", ToolReply{Content: toyStoryDiagnostic, Narrate: true}); got != toyStoryDiagnostic {
		t.Fatal(got)
	}
}
