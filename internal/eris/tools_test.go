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
		`{"action":"prepare","query":"Hot Fuzz","relation":"director"}`, `{"action":"recommend","query":"Hot Fuzz","relation":"actor"}`,
		`{"action":"recommend","source":"seerr"}`, `{"action":"recommend","query":"Hot Fuzz","relation":"director","source":"seerr","unwatched":true}`,
		`{"action":"recommend","query":"Hot Fuzz","relation":"similar","kind":"show"}`, `{"action":"recommend","query":"Hot Fuzz","relation":"similar","maxMinutes":90}`,
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
	if utf8.RuneCountInString(Persona+ToolInstructions) > 2200 {
		t.Fatal("runtime prompt limit exceeded")
	}
}

func TestRecommendationCanKeepReferenceRelationAndCatalogue(t *testing.T) {
	for _, raw := range []string{
		`{"action":"recommend","query":"Hot Fuzz","kind":"movie","relation":"similar"}`,
		`{"action":"recommend","query":"Hot Fuzz","kind":"movie","relation":"director","source":"seerr"}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: raw}}}})
		}))
		c := New(server.URL, "", "model")
		called := false
		reply, err := c.ReplyWithTools(context.Background(), "guild:alice", "Du même réal que Hot Fuzz qu'on pourrait demander sur Seerr ?", false, func(_ context.Context, p Plan) (ToolReply, error) {
			called = true
			if p.Action != "recommend" || p.Query != "Hot Fuzz" {
				t.Fatalf("lost recommendation: %+v", p)
			}
			return ToolReply{Content: "Films vérifiés du même réalisateur"}, nil
		})
		server.Close()
		if err != nil || !called || reply != "Films vérifiés du même réalisateur" {
			t.Fatalf("recommendation became a title clarification: %q %v", reply, err)
		}
	}
}

func TestAbsentFromPlexKeepsRecommendationInSeerr(t *testing.T) {
	for _, question := range []string{"Et qui seraient pas encore sur Plex ?", "Des films absents de Plex", "Hors Plex"} {
		p := (Plan{Action: "recommend", Query: "Hot Fuzz", Relation: "similar", Source: "plex", Kind: "movie"}).WithQuestionConstraints(question)
		if p.Source != "seerr" || p.Action != "recommend" || p.Validate() != nil {
			t.Fatal(question, p)
		}
	}
}

func TestToolFactsAreRenderedWithoutModelRewritingAndRemembered(t *testing.T) {
	var prompts [][]Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages       []Message
			ResponseFormat map[string]string `json:"response_format"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.ResponseFormat["type"] != "json_object" {
			t.Error("tool conversations must request constrained JSON")
		}
		prompts = append(prompts, body.Messages)
		answer := `{"action":"search","query":"Arrival","kind":"movie"}`
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
	for _, question := range []string{"Tu as Arrival ?", "Alors demande-le"} {
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

func TestButtonResultRemembersOnlyItsOwnConversation(t *testing.T) {
	c := New("http://must-not-call.test", "", "model")
	c.sessions["guild:alice:channel"] = &conversation{messages: []Message{{Role: "user", Content: "public history"}}, expires: time.Now().Add(time.Minute)}
	c.RememberRequest("guild:alice", ToolReply{Content: "Choisis Hot Fuzz (2007).", Buttons: []Button{{ID: "private-token", Label: "Choisir"}}})
	s := c.sessions["guild:alice"]
	if s == nil || !s.pending || len(s.messages) != 2 || strings.Contains(s.messages[1].Content, "private-token") || strings.Contains(s.messages[1].Content, "public history") {
		t.Fatal("button result lost or mixed context")
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

func TestRequestFollowupDoesNotTurnRequestStatusIntoPreparation(t *testing.T) {
	for _, question := range []string{"Ma demande le mercredi est-elle prête ?", "Cherche Arrival", "Demande le statut"} {
		if p := (Plan{Action: "search", Query: "Arrival"}).WithQuestionConstraints(question); p.Action != "search" {
			t.Fatal("invented request", question)
		}
	}
	if p := (Plan{Action: "status", Query: "Arrival"}).WithQuestionConstraints("Demande-le"); p.Action != "status" {
		t.Fatal("changed status action")
	}
}

func TestExplicitPronounRequestDoesNotBecomeAnotherRecommendation(t *testing.T) {
	for _, question := range []string{"Tu peux le demander pour moi dans Seerr ?", "Peux-tu le demander ?", "Demande-le"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"recommend","query":"Hot Fuzz","relation":"similar","source":"seerr","kind":"movie"}`}}}})
		}))
		c := New(server.URL, "", "model")
		c.sessions["owner"] = &conversation{expires: time.Now().Add(time.Minute), messages: []Message{{Role: "user", Content: "Mon film préf, c'est Hot Fuzz"}, {Role: "assistant", Content: "Bon choix."}}}
		_, err := c.ReplyWithTools(context.Background(), "owner", question, false, func(_ context.Context, p Plan) (ToolReply, error) {
			if p.Action != "prepare" || p.Query != "Hot Fuzz" || p.Relation != "" || p.Source != "" || p.Validate() != nil {
				t.Errorf("explicit request lost: %+v", p)
			}
			return ToolReply{Content: "Choisis le titre."}, nil
		})
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	p := (Plan{Action: "recommend", Query: "Hot Fuzz", Relation: "director", Source: "seerr"}).WithQuestionConstraints("Du même réalisateur que Hot Fuzz qu'on pourrait demander dans Seerr ?")
	if p.Action != "recommend" {
		t.Fatal("recommendation converted to request")
	}
}

func TestSameDirectorFollowupCannotRequestTheReferenceFilm(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"prepare","query":"Hot Fuzz","kind":"movie"}`}}}})
	}))
	defer server.Close()
	c := New(server.URL, "", "model")
	c.sessions["owner"] = &conversation{expires: time.Now().Add(time.Minute), messages: []Message{{Role: "user", Content: "Mon film préféré est Hot Fuzz"}, {Role: "assistant", Content: "Bon choix."}}}
	_, err := c.ReplyWithTools(context.Background(), "owner", "Un autre du même réalisateur à demander dans Seerr ?", false, func(_ context.Context, p Plan) (ToolReply, error) {
		if p.Action != "recommend" || p.Query != "Hot Fuzz" || p.Relation != "director" || p.Source != "seerr" {
			t.Errorf("reference requested instead of another director credit: %+v", p)
		}
		return ToolReply{Content: "Filmographie vérifiée."}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInventedUnwatchedFilterCannotForceAPrivateHandoff(t *testing.T) {
	for _, history := range []string{"", "Un film que je n'ai pas vu"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"recommend","genre":"thriller","kind":"movie","unwatched":true}`}}}})
		}))
		c := New(server.URL, "", "model")
		if history != "" {
			c.sessions["owner"] = &conversation{expires: time.Now().Add(time.Minute), messages: []Message{{Role: "user", Content: history}, {Role: "assistant", Content: "Voici les titres."}}}
		}
		_, err := c.ReplyWithTools(context.Background(), "owner", "Un thriller pour ce soir", false, func(_ context.Context, p Plan) (ToolReply, error) {
			if p.Unwatched != (history != "") {
				t.Errorf("invented or lost watch constraint: %+v history=%q", p, history)
			}
			return ToolReply{Content: "Titres vérifiés."}, nil
		})
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestRequestCannotInventTitleFromSystemExamples(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"prepare","query":"Arrival","kind":"movie"}`}}}})
	}))
	defer server.Close()
	c := New(server.URL, "", "model")
	called := false
	reply, err := c.ReplyWithTools(context.Background(), "guild:alice", "Tu peux demander le film dans seerr ?", false, func(context.Context, Plan) (ToolReply, error) {
		called = true
		return ToolReply{Content: "Choisir Arrival"}, nil
	})
	if err != nil || called || !strings.Contains(reply, "titre") {
		t.Fatalf("invented request: called=%v reply=%q err=%v", called, reply, err)
	}
}

func TestRequestTitleComesFromTheCurrentUnexpiredConversation(t *testing.T) {
	for _, tc := range []struct {
		name, question, title, owner, context string
		expired, allowed                      bool
	}{
		{"explicit", "Demande HOT FUZZ", "Hot Fuzz", "", "", false, true},
		{"preference", "Tu peux demander le film dans seerr ?", "Hot Fuzz", "guild:alice", "Mon film préf, c'est hot fuzz", false, true},
		{"another member", "Tu peux demander le film dans seerr ?", "Hot Fuzz", "guild:bob", "Mon film préféré est Hot Fuzz", false, false},
		{"expired", "Tu peux demander le film dans seerr ?", "Hot Fuzz", "guild:alice", "Mon film préféré est Hot Fuzz", true, false},
		{"different word", "Demande Aliens", "Alien", "", "", false, false},
		{"punctuation", "Demande Mission : Impossible", "Mission: Impossible", "", "", false, true},
		{"generic film", "Tu peux demander le film dans Seerr ?", "le film", "", "", false, false},
		{"generic series", "Demande cette série", "cette série", "", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				plan, _ := json.Marshal(Plan{Action: "prepare", Query: tc.title, Kind: "movie"})
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: string(plan)}}}})
			}))
			defer server.Close()
			c := New(server.URL, "", "model")
			if tc.owner != "" {
				expires := time.Now().Add(time.Minute)
				if tc.expired {
					expires = time.Now().Add(-time.Minute)
				}
				c.sessions[tc.owner] = &conversation{expires: expires, messages: []Message{{Role: "user", Content: tc.context}, {Role: "assistant", Content: "Noté."}}}
			}
			called := false
			reply, err := c.ReplyWithTools(context.Background(), "guild:alice", tc.question, false, func(_ context.Context, p Plan) (ToolReply, error) {
				called = true
				return ToolReply{Content: "Choix Seerr"}, nil
			})
			if err != nil || called != tc.allowed {
				t.Fatal(called, err)
			}
			if !tc.allowed && (!strings.Contains(reply, "titre") || !strings.Contains(c.sessions["guild:alice"].messages[1].Content, "titre")) {
				t.Fatal("clarification was not remembered", reply)
			}
		})
	}
}
