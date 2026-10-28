package eris

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveSeriesSpellingCorrections(t *testing.T) {
	if os.Getenv("ERIS_SERIES_LIVE") != "1" {
		t.Skip("explicit synthetic model recipe only")
	}
	c := New("https://api.openai.com/v1", "", os.Getenv("ERIS_MODEL"))
	c.V2 = true
	c.Protocol = "responses"
	c.KeyFile = os.Getenv("ERIS_API_KEY_FILE")
	memory, err := OpenMemory(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	c.Memory = memory
	type turn struct {
		Question, Reply string
		Plan            *Plan
		Error           string
	}
	var report []turn
	defer func() { raw, _ := json.MarshalIndent(report, "", "  "); fmt.Println(string(raw)) }()
	owner := "10000:20000:30000"
	choice := "eris:choose:" + strings.Repeat("a", 64)
	for n, question := range []string{"est ce que tu peux mettre sur le plex la saison 1 de dective konan stp ?", "detective konan", "Detective Konnan", "detective Connan", "detective conan"} {
		if s := c.sessions[owner]; s != nil {
			s.next = time.Time{}
		}
		row := turn{Question: question}
		calls := 0
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		row.Reply, err = c.ReplyWithDiscordContext(ctx, owner, question, false, "", DiscordInput{MessageID: fmt.Sprint(40000 + n), Speaker: Speaker{ID: "20000", Name: "Testeur"}}, func(_ context.Context, p Plan) (ToolReply, error) {
			calls++
			copied := p
			row.Plan = &copied
			if p.Action == "continue" {
				if p.Query != choice {
					t.Error("unknown simulated choice")
				}
				return ToolReply{Content: "Choix de Détective Conan, saison 1 reçu : simulation, aucune demande réelle envoyée."}, nil
			}
			if p.Action != "prepare" || p.Kind != "show" || len(p.Seasons) != 1 || p.Seasons[0] != 1 {
				t.Error("series or original season lost", p)
			}
			return NaturalChoices(ToolReply{Content: "Catalogue simulé : 1. Détective Conan (1996), saison 1\n2. Détective Conan présente Apprenti criminel (2022), saison 1", Buttons: []Button{{ID: choice, Title: "Détective Conan", Year: "1996", Number: 1}, {ID: "eris:choose:" + strings.Repeat("b", 64), Title: "Détective Conan présente Apprenti criminel", Year: "2022", Number: 2}}}), nil
		})
		cancel()
		if err != nil {
			row.Error = "model_error"
			t.Error(err)
		}
		if calls != 1 {
			t.Errorf("turn %d remained before catalogue: %s", n, row.Reply)
		}
		report = append(report, row)
		if row.Reply != "" {
			c.Delivered(owner, fmt.Sprint(50000+n), row.Reply)
		}
	}
}
