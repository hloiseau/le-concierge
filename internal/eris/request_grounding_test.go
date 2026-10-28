package eris

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSeriesPrepareAcceptsAccentsAndReportedSpellingVariants(t *testing.T) {
	for _, question := range []string{
		"Mets sur Plex la saison 1 de dective konan stp",
		"Demande la saison 1 de detective konan",
		"Demande la saison 1 de Detective Konnan",
		"Demande la saison 1 de detective Connan",
		"Demande la saison 1 de detective conan",
	} {
		t.Run(question, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"prepare","query":"Détective Conan","kind":"show","seasons":[1]}`}}}})
			}))
			defer server.Close()
			c := New(server.URL, "", "test")
			c.V2 = true
			calls := 0
			reply, err := c.ReplyWithTools(context.Background(), "member", question, false, func(_ context.Context, p Plan) (ToolReply, error) {
				calls++
				if p.Action != "prepare" || p.Kind != "show" || len(p.Seasons) != 1 || p.Seasons[0] != 1 {
					t.Fatal("series or season lost", p)
				}
				return ToolReply{Content: "Choix catalogue vérifiés pour la saison 1."}, nil
			})
			if err != nil || calls != 1 {
				t.Fatalf("request stuck before catalog search: calls=%d reply=%q err=%v", calls, reply, err)
			}
		})
	}
}

func TestSpellingGroundingStillRejectsInventedTitlesAndDifferentSequels(t *testing.T) {
	for _, tc := range []struct{ title, text string }{
		{"Détective Conan", "Ajoute Breaking Bad saison 1"},
		{"Alien", "Ajoute Aliens"},
		{"Breaking Bad", "Ajoute Breaking Dad saison 1"},
		{"Toy Story 5", "Ajoute Toy Story 4"},
		{"Conan le Barbare", "Ajoute Détective Conan"},
		{"un film", "Ajoute un truc sympa"},
		{"cette série", "Ajoute cette série"},
	} {
		if prepareTitleMentioned(tc.title, []Message{{Role: "user", Content: tc.text}}) {
			t.Error("unrelated title accepted", tc)
		}
	}
	if prepareTitleMentioned("Détective Conan", []Message{{Role: "system", Content: "Détective Conan"}, {Role: "user", Content: "Demande le titre de ton choix"}}) {
		t.Fatal("system example grounded a request")
	}
	if requestTitleMentioned("Détective Conan", []Message{{Role: "user", Content: "dective konan"}}) {
		t.Fatal("search spelling tolerance leaked into strict title matching")
	}
	if ExplicitRequest("Demande la saison 1 de detective konan", "Détective Conan", "1996", "tv", []int{1}) {
		t.Fatal("typo became direct submission consent")
	}
}

func TestSeriesTitleCorrectionsKeepSeasonAndCanChooseWithoutAccents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Messages []Message }
		json.NewDecoder(r.Body).Decode(&in)
		all := ""
		for _, m := range in.Messages {
			all += m.Content
		}
		if !strings.Contains(all, "saison 1") {
			t.Error("original season disappeared from conversation")
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: `{"action":"prepare","query":"Détective Conan","kind":"show","seasons":[1]}`}}}})
	}))
	defer server.Close()
	c := New(server.URL, "", "test")
	c.V2 = true
	owner := "10000:20000:30000"
	choice := "eris:choose:" + strings.Repeat("a", 64)
	for turn, question := range []string{"Mets sur Plex la saison 1 de dective konan stp", "detective konan", "Detective Konnan", "detective Connan", "detective conan"} {
		if turn > 0 {
			c.sessions[owner].next = time.Time{}
		}
		calls := 0
		text, err := c.ReplyWithDiscordContext(context.Background(), owner, question, false, "", DiscordInput{MessageID: fmt.Sprint(40000 + turn), Speaker: Speaker{ID: "20000", Name: "Testeur"}}, func(_ context.Context, p Plan) (ToolReply, error) {
			calls++
			if turn == 4 {
				if p.Action != "continue" || p.Query != choice {
					t.Fatal("accent-free selection lost", p)
				}
				return ToolReply{Content: "Choix de la saison 1 reçu (simulation)."}, nil
			}
			if p.Action != "prepare" || p.Kind != "show" || len(p.Seasons) != 1 || p.Seasons[0] != 1 {
				t.Fatal("season lost on corrected title", p)
			}
			return NaturalChoices(ToolReply{Content: "1. Détective Conan (1996), saison 1", Buttons: []Button{{ID: choice, Title: "Détective Conan", Year: "1996", Number: 1}}}), nil
		})
		if err != nil || calls != 1 {
			t.Fatal(turn, text, calls, err)
		}
		c.Delivered(owner, fmt.Sprint(50000+turn), text)
	}
}

func TestAccentInsensitiveChoiceStillRequiresWholeUnambiguousTitle(t *testing.T) {
	choices := []Button{{ID: "eris:choose:a", Title: "Détective Conan", Year: "1996", Number: 1}}
	if got, ok := MatchChoice("detective conan", choices); !ok || got != choices[0].ID {
		t.Fatal(got, ok)
	}
	for _, text := range []string{"detective konan", "pas detective conan", "demande detective conan mais pas maintenant"} {
		if got, _ := MatchChoice(text, choices); got != "" {
			t.Fatal("fuzzy or partial consent", text, got)
		}
	}
	choices = append(choices, Button{ID: "eris:choose:b", Title: "Detective Conan", Year: "2000", Number: 2})
	if got, _ := MatchChoice("detective conan", choices); got != "" {
		t.Fatal("ambiguous title silently selected", got)
	}
}
