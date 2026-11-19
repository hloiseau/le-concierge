package eris

import (
	"context"
	"encoding/json"
	"net/http"

	"strings"
	"testing"
	"time"
)

func TestPublicClosuresSilenceAndExplicitReentry(t *testing.T) {
	c := New("", "", "")
	c.V2 = true
	owner := "10000:20000:30000"
	c.observePublic(owner, "salut", memoryInput("40001", "20000"), time.Now())
	for _, text := range []string{"oui", "ouai, c'est ça", "👍", "jpp", "merci", "ah pas mal"} {
		if c.SkipPublicFollowup(owner, text, false) {
			t.Fatal("model did not receive contextual followup", text)
		}
	}
	for _, text := range []string{"oui, demande-le", "mais pourquoi ?", "?", "demande le deuxième", "non je voulais la série"} {
		if c.SkipPublicFollowup(owner, text, false) {
			t.Fatal("real followup suppressed", text)
		}
	}
	if c.SkipPublicFollowup(owner, "merci", true) {
		t.Fatal("explicit mention suppressed")
	}
	if c.SkipPublicFollowup(owner, "oui", false) {
		t.Fatal("short followup suppressed")
	}
	if !c.SkipPublicFollowup(owner, "tu peux fermer ta gueule ?", false) || !c.SkipPublicFollowup(owner, "encore une chose", false) {
		t.Fatal("quiet state ignored")
	}
	if c.SkipPublicFollowup("10000:20001:30000", "nouvelle question", true) || c.SkipPublicFollowup(owner, "autre question", false) {
		t.Fatal("explicit reentry failed")
	}
}

func TestTeasingReassuranceIsNotUnconditionallySilenced(t *testing.T) {
	c := New("", "", "")
	c.V2 = true
	owner := "10000:20000:30000"
	c.observePublic(owner, "Je vais te rayer de mon grimoire 😏", memoryInput("40001", "20000"), time.Now())
	c.publicReply(owner, memoryInput("40001", "20000"), "Essaie un peu.")
	for _, text := range []string{"tkt", "tkt pas", "tkt pas 😏"} {
		if c.SkipPublicFollowup(owner, text, false) {
			t.Fatalf("contextual teasing response %q was hard-silenced before the model could read the exchange", text)
		}
	}
	if c.SkipPublicFollowup(owner, "ok", false) {
		t.Fatal("closure was decided without the model")
	}
}

func TestAmbientContextIsTransientActiveRoomOnlyAndNeverAnAction(t *testing.T) {
	c := New("", "", "")
	c.V2 = true
	owner := "10000:20000:30000"
	c.ObserveAmbient(owner, "Pas de salle active", memoryInput("40000", "20000"))
	if len(c.publicRooms) != 0 {
		t.Fatal("ambient message activated room")
	}
	c.observePublic(owner, "bonjour", memoryInput("40001", "20000"), time.Now())
	c.ObserveAmbient("10000:20001:30000", "Le sigle MTC désigne Mon Très Cher ordinateur.", memoryInput("40002", "20001"))
	c.ObserveAmbient(owner, "Je parlais aussi de mon ordinateur personnel.", memoryInput("40007", "20000"))
	data := c.discordContext(owner, memoryInput("40003", "20000"), time.Now())
	if !strings.Contains(data, "Mon Très Cher") {
		t.Fatal("missing ambient reference")
	}
	if !strings.Contains(data, "ordinateur personnel") {
		t.Fatal("current author's ambient context was excluded")
	}
	c.ObserveAmbient("10000:20001:30001", "Autre salon secret", memoryInput("40004", "20001"))
	if len(c.publicRooms) != 1 {
		t.Fatal("ambient context crossed rooms")
	}
	c.publicRooms[publicScope(owner)].at = time.Now().Add(-31 * time.Minute)
	c.ObserveAmbient(owner, "Trop tard", memoryInput("40005", "20000"))
	if strings.Contains(c.discordContext(owner, memoryInput("40006", "20000"), time.Now()), "Trop tard") {
		t.Fatal("expired room observed")
	}
}

func TestModelSilenceDoesNotExecuteToolOrBlockNextTurn(t *testing.T) {
	server := featureModelServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"silent"}`}}}})
	}))
	defer server.Close()
	c := New(server.URL, "", "model")
	c.V2 = true
	for i, owner := range []string{"10000:20000:30000", "10000:20000"} {
		text, err := c.ReplyWithDiscordContext(context.Background(), owner, "oui", false, "", memoryInput("40001", "20000"), func(context.Context, Plan) (ToolReply, error) {
			t.Fatal("silent invoked tool")
			return ToolReply{}, nil
		})
		if (i == 0 && (err != nil || text != "")) || (i == 1 && (err != ErrUnavailable || text != "")) {
			t.Fatal(text, err)
		}
	}
}
