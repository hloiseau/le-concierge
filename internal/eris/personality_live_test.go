package eris

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in NAS lab: synthetic conversations, isolated SQLite, and simulated tools/delivery.
func TestLiveErisPersonality(t *testing.T) {
	input := os.Getenv("ERIS_PERSONALITY_CASES")
	if input == "" {
		t.Skip("live personality lab not enabled")
	}
	var cases []struct {
		ID     string   `json:"id"`
		Inputs []string `json:"inputs"`
	}
	raw, err := os.ReadFile(input)
	if err != nil || json.Unmarshal(raw, &cases) != nil || len(cases) == 0 {
		t.Fatal("invalid synthetic cases")
	}
	type turn struct {
		CaseID     string  `json:"case"`
		Input      string  `json:"input"`
		Reply      string  `json:"reply"`
		Seconds    float64 `json:"seconds"`
		Plan       *Plan   `json:"plan,omitempty"`
		Error      string  `json:"error,omitempty"`
		HTTPStatus int     `json:"http_status,omitempty"`
	}
	hash := sha256.Sum256([]byte(CharacterPersona))
	report := struct {
		Model       string `json:"model"`
		PersonaHash string `json:"persona_sha256"`
		PromptHash  string `json:"prompt_sha256"`
		Scope       string `json:"scope"`
		Turns       []turn `json:"turns"`
	}{Model: "gpt-6-luna", PersonaHash: hex.EncodeToString(hash[:]), Scope: "Synthetic NAS Responses lab; isolated memory, simulated tools and delivery; editorial review required."}
	promptHash := sha256.Sum256([]byte(CharacterPersona + ToolInstructions + MemoryInstructions))
	report.PromptHash = hex.EncodeToString(promptHash[:])
	defer func() {
		out, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(os.Getenv("ERIS_PERSONALITY_REPORT"), append(out, '\n'), 0600); err != nil {
			t.Error("could not save report")
		}
	}()
	for _, scenario := range cases {
		m, err := OpenMemory(filepath.Join(t.TempDir(), "character.db"))
		if err != nil {
			t.Fatal(err)
		}
		c := New("https://api.openai.com/v1", "", "gpt-6-luna")
		c.V2, c.Protocol, c.KeyFile, c.Memory = true, "responses", os.Getenv("ERIS_PERSONALITY_TOKEN_FILE"), m
		owner := "synthetic:member:lab"
		for _, question := range scenario.Inputs {
			if session := c.sessions[owner]; session != nil {
				session.next = time.Time{}
			}
			row := turn{CaseID: scenario.ID, Input: question}
			c.HTTP.Transport = personalityStatusTransport{status: &row.HTTPStatus}
			start := time.Now()
			row.Reply, err = c.ReplyWithTools(context.Background(), owner, question, false, func(_ context.Context, plan Plan) (ToolReply, error) {
				row.Plan = &plan
				return ToolReply{Content: "Résultat de test : données catalogue simulées."}, nil
			})
			row.Seconds = time.Since(start).Seconds()
			if err != nil {
				row.Error = "completion failed"
				t.Error(scenario.ID, row.Error)
			}
			report.Turns = append(report.Turns, row)
			c.Delivered(owner, "synthetic-delivery", row.Reply)
			// A concrete repro for the observed gatekeeping; creativity still needs a reader.
			if strings.Contains(question, "montrer quand") && strings.Contains(strings.ToLower(row.Reply), "bonne raison") {
				t.Error("requested performance refused pending a good reason")
			}
			if scenario.ID == "server" && (row.Plan == nil || row.Plan.Action != "monitoring") {
				t.Error("server metrics not routed to monitoring")
			}
			if outsidePlex.MatchString(question) && (row.Plan == nil || row.Plan.Source != "seerr") {
				t.Error("outside-Plex constraint lost")
			}
			if scenario.ID == "media" && sameDirector.MatchString(question) && (row.Plan == nil || row.Plan.Relation != "director" || row.Plan.Source != "seerr") {
				t.Error("director follow-up lost catalogue context")
			}
			if scenario.ID == "cinema-opinion" && row.Plan != nil {
				t.Error("personal opinion replaced with catalogue lookup")
			}
			t.Logf("%s: %.2fs", scenario.ID, row.Seconds)
		}
		m.Close()
	}
}

// Record only a status code, never headers, credentials or provider error bodies.
type personalityStatusTransport struct{ status *int }

func (p personalityStatusTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(r)
	if response != nil {
		*p.status = response.StatusCode
	}
	return response, err
}
