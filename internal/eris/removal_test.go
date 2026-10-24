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

func TestRemovalExplainsLimitsWithoutOfferingAnAddition(t *testing.T) {
	for _, raw := range []string{
		`{"action":"prepare","query":"Paddington 2","kind":"movie"}`,
		`{"action":"status","query":"Paddington 2"}`,
		`{"action":"chat","text":"Donne-moi le titre ?"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: raw}}}})
			}))
			defer server.Close()
			for _, question := range []string{
				"est ce que tu peux supprimer un film que j'ai demandé ?",
				"Donc, supprime paddington 2",
				"C'est celui là, supprime le",
				"Supprime le de Plex",
			} {
				c := New(server.URL, "", "model")
				c.V2 = true
				reply, err := c.ReplyWithTools(context.Background(), "guild:member:channel", question, false, func(_ context.Context, p Plan) (ToolReply, error) {
					t.Errorf("removal invoked %s instead of explaining its limits", p.Action)
					return ToolReply{Content: "Choisis le bon titre"}, nil
				})
				if err != nil || !strings.Contains(reply, "Seerr") || !strings.Contains(reply, "Plex") || strings.Contains(reply, "?") {
					t.Errorf("%s: expected capability explanation without a title/private handoff, got %q (%v)", question, reply, err)
				}
			}
		})
	}
}

func TestRemovalFollowupKeepsIntentAndClearsPendingAddition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"prepare","query":"Paddington 2","kind":"movie"}`}}}})
	}))
	defer server.Close()
	c := New(server.URL, "", "model")
	c.V2 = true
	owner := "guild:member:channel"
	c.sessions[owner] = &conversation{expires: time.Now().Add(time.Minute), choiceUntil: time.Now().Add(time.Minute), choiceSource: "old-offer", choices: []Button{{ID: "eris:choose:old", Title: "Paddington 2", Number: 1}}}
	calls := 0
	run := func(_ context.Context, p Plan) (ToolReply, error) {
		calls++
		return ToolReply{Content: "Demande créée : Paddington 2"}, nil
	}
	for _, question := range []string{"Tu peux supprimer un film que j'ai demandé ?", "Paddington 2", "Donc, supprime paddington 2", "C'est celui là, supprime le", "Supprime le de Plex"} {
		reply, err := c.ReplyWithTools(context.Background(), owner, question, false, run)
		if err != nil || reply != RemovalExplanation || calls != 0 || len(c.sessions[owner].choices) != 0 {
			t.Fatalf("removal lost intent or retained an old add choice: %q %v calls=%d", reply, err, calls)
		}
		c.Delivered(owner, "test-reply", reply)
	}
	if reply, err := c.ReplyWithContext(context.Background(), owner, "Paddington 2", false, "Eris (message test-reply) : "+RemovalExplanation, run); err != nil || reply != RemovalExplanation || calls != 0 {
		t.Fatalf("quoted explanation lost removal intent: %q %v", reply, err)
	} else {
		c.Delivered(owner, "test-reply", reply)
	}
	if _, err := c.ReplyWithTools(context.Background(), owner, "Demande Paddington 2", false, run); err != nil || calls != 1 {
		t.Fatalf("an explicit change to an add intent must still work: %v calls=%d", err, calls)
	}
}

func TestMediaRemovalDoesNotConsumeOtherIntents(t *testing.T) {
	for _, question := range []string{"Annule ma demande de Paddington 2", "Retire Paddington 2 de Plex", "Peux-tu le supprimer de Plex ?", "Comment supprimer un film ?", "Efface le film", "Enlève Paddington 2", "Je voudrais supprimer Paddington 2", "Supprime-le"} {
		if !IsMediaRemoval(question) {
			t.Errorf("missed media removal: %s", question)
		}
	}
	for _, question := range []string{"Annule", "Annule stp", "Non", "Supprime tes souvenirs", "Efface ma mémoire", "Supprime ce message", "Oublie ta lubie", "Demande Paddington 2", "Ne supprime pas Paddington 2", "Demande le film Effacer l'historique", "Tu penses quoi du film Supprimer ?", "Supprimer est un titre de film"} {
		if IsMediaRemoval(question) {
			t.Errorf("intercepted another intent: %s", question)
		}
	}
}
