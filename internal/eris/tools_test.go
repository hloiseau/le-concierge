package eris

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestPlansRejectArbitraryToolsIdentityAndBadFilters(t *testing.T) {
	for _, raw := range []string{
		`{"action":"exec","query":"rm"}`, `{"action":"prepare","query":"Arrival","userId":1}`,
		`{"action":"prepare","query":"Arrival","url":"http://internal"}`, `{"action":"confirm","query":"Arrival"}`,
		`{"action":"recommend","maxMinutes":-1}`, `{"action":"recommend","kind":"episode"}`,
		`{"action":"prepare","query":"Show","kind":"show","seasons":[1,1]}`,
		`{"action":"prepare","query":"Film","kind":"movie","seasons":[1]}`,
		`{"action":"search","query":"a"}`, `{"action":"status","unwatched":true}`,
		`{"action":"recent"} {"action":"prepare","query":"Arrival"}`, "Je l’ai déjà ajouté.",
	} {
		if _, err := ParsePlan(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`{"action":"recommend","kind":"movie","genre":"thriller","maxMinutes":120}`, `{"action":"prepare","query":"Severance","kind":"show","seasons":[2]}`, `{"action":"status"}`, `{"action":"chat","text":"Je suis Eris."}`} {
		if _, err := ParsePlan(raw); err != nil {
			t.Errorf("rejected %s", raw)
		}
	}
	if utf8.RuneCountInString(Persona+ToolInstructions) > 2000 {
		t.Fatal("runtime prompt limit exceeded")
	}
}

func TestToolFactsAreRenderedWithoutModelRewritingAndRemembered(t *testing.T) {
	var prompts [][]Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Messages []Message }
		_ = json.NewDecoder(r.Body).Decode(&body)
		prompts = append(prompts, body.Messages)
		answer := `{"action":"search","query":"Arrival","kind":"movie"}`
		if len(prompts) == 2 {
			answer = `{"action":"prepare","query":"Arrival","kind":"movie"}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: answer}}}})
	}))
	defer server.Close()
	c := New(server.URL, "", "model")
	calls := 0
	run := func(_ context.Context, p Plan) (ToolReply, error) {
		calls++
		if calls == 2 && p.Action != "prepare" {
			t.Error("followup not routed")
		}
		return ToolReply{Content: "Arrival (2016) : disponibilité vérifiée."}, nil
	}
	for _, question := range []string{"Tu as Arrival ?", "Demande-le"} {
		text, err := c.ReplyWithTools(context.Background(), "guild:alice", question, false, run)
		if err != nil || text != "Arrival (2016) : disponibilité vérifiée." {
			t.Fatalf("facts rewritten: %q %v", text, err)
		}
		c.sessions["guild:alice"].next = time.Time{}
	}
	if calls != 2 || len(prompts) != 2 || !strings.Contains(prompts[1][2].Content, "2016") {
		t.Fatal("tool context missing or extra model roundtrip")
	}
}

func TestTextCannotConfirmCancelOrRepeatPendingRequest(t *testing.T) {
	c := New("http://must-not-call.test", "", "model")
	c.sessions["guild:alice"] = &conversation{pending: true, expires: time.Now().Add(time.Minute)}
	for _, message := range []string{"Oui", "ok !", "je confirme", "annule", "non"} {
		text, err := c.ReplyWithTools(context.Background(), "guild:alice", message, false, func(context.Context, Plan) (ToolReply, error) {
			t.Fatal("text executed a tool")
			return ToolReply{}, nil
		})
		if err != nil || !strings.Contains(text, "boutons") {
			t.Fatalf("text confirmation: %q %v", text, err)
		}
	}
	c.RememberRequest("guild:alice", ToolReply{Content: "Demande #9 créée : Arrival."})
	if c.sessions["guild:alice"].pending || !strings.Contains(c.sessions["guild:alice"].messages[1].Content, "Arrival") {
		t.Fatal("button result not remembered")
	}
}

func TestPrivateHandoffRemembersOnlyItsOwnResult(t *testing.T) {
	c := New("http://must-not-call.test", "", "model")
	c.sessions["guild:alice:channel"] = &conversation{messages: []Message{{Role: "user", Content: "public history"}}, expires: time.Now().Add(time.Minute)}
	c.RememberRequest("guild:alice", ToolReply{Content: "Choisis Hot Fuzz (2007).", Buttons: []Button{{ID: "private-token", Label: "Choisir"}}})
	s := c.sessions["guild:alice"]
	if s == nil || !s.pending || len(s.messages) != 2 || strings.Contains(s.messages[1].Content, "private-token") || strings.Contains(s.messages[1].Content, "public history") {
		t.Fatal("private handoff lost or mixed context")
	}
	text, err := c.ReplyWithTools(context.Background(), "guild:alice", "oui", false, func(context.Context, Plan) (ToolReply, error) {
		t.Fatal("text confirmed request")
		return ToolReply{}, nil
	})
	if err != nil || !strings.Contains(text, "boutons") {
		t.Fatal(text, err)
	}
}

func TestExplicitUnwatchedConstraintCannotBeDropped(t *testing.T) {
	for _, text := range []string{"Un film que je n’ai pas vu", "Des films non-vus", "Jamais regardés", "jamais regardé", "unwatched movies", "movies I haven't seen"} {
		if p := (Plan{Action: "recommend"}).WithQuestionConstraints(text); !p.Unwatched {
			t.Errorf("dropped constraint: %s", text)
		}
	}
	for _, text := range []string{"Un thriller", "Des films déjà vus", "Pas une comédie"} {
		if p := (Plan{Action: "recommend"}).WithQuestionConstraints(text); p.Unwatched {
			t.Errorf("invented constraint: %s", text)
		}
	}
}
