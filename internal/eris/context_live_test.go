package eris

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/context-baseline.txt
var contextLabBaseline string

// Opt-in ablation of synthetic dialogue only. Never records headers or credentials.
type contextLabTurn struct {
	Repetition int       `json:"repetition"`
	Model      string    `json:"model"`
	Condition  string    `json:"condition"`
	Case       string    `json:"case"`
	Input      string    `json:"input"`
	Reply      string    `json:"reply"`
	Messages   []Message `json:"messages"`
	Error      bool      `json:"error,omitempty"`
	Seconds    float64   `json:"seconds"`
	HTTPStatus int       `json:"http_status,omitempty"`
	Plan       *Plan     `json:"plan,omitempty"`
}

func TestLiveErisContext(t *testing.T) {
	path := os.Getenv("ERIS_CONTEXT_CASES")
	if path == "" {
		t.Skip("synthetic context lab disabled")
	}
	var cases []struct {
		ID           string    `json:"id"`
		Inputs       []string  `json:"inputs"`
		Seed         []Message `json:"seed,omitempty"`
		ExpectAction string    `json:"expect_action,omitempty"`
		Spontaneous  bool      `json:"spontaneous,omitempty"`
		ExpectSilent bool      `json:"expect_silent,omitempty"`
	}
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &cases) != nil || len(cases) == 0 {
		t.Fatal("invalid cases")
	}
	var report []contextLabTurn
	defer func() {
		raw, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(os.Getenv("ERIS_CONTEXT_REPORT"), append(raw, '\n'), 0600); err != nil {
			t.Error(err)
		}
	}()
	conditions := []string{"production", "quoted"}
	if selected := os.Getenv("ERIS_CONTEXT_CONDITIONS"); selected != "" {
		conditions = strings.Split(selected, ",")
	}
	for repetition, condition := range conditions {
		if condition != "baseline" && condition != "production" && condition != "quoted" && condition != "verbosity-low" {
			t.Fatal("unknown laboratory condition")
		}
		for _, scenario := range cases {
			m, err := OpenMemory(filepath.Join(t.TempDir(), "character.db"))
			if err != nil {
				t.Fatal(err)
			}
			model := os.Getenv("ERIS_PERSONALITY_MODEL")
			if model == "" {
				model = "gpt-6.1-sol"
			}
			c := New("https://api.openai.com/v1", "", model)
			c.V2, c.Protocol, c.KeyFile, c.Memory = true, "responses", os.Getenv("ERIS_PERSONALITY_TOKEN_FILE"), m
			owner := "10000:20000:30000"
			if len(scenario.Seed) != 0 {
				if len(scenario.Seed)%2 != 0 {
					t.Fatal("seed must contain complete exchanges")
				}
				for i := 0; i < len(scenario.Seed); i += 2 {
					human, reply := scenario.Seed[i], scenario.Seed[i+1]
					if human.Role != "user" || reply.Role != "assistant" {
						t.Fatal("invalid seed roles")
					}
					id := observeTest(t, m, owner, human.Content, fmt.Sprintf("%05d", 60000+i))
					if err := m.deliverTurn(id, owner, reply.Content, nil); err != nil {
						t.Fatal(err)
					}
				}
				c.sessions[owner] = &conversation{messages: append([]Message(nil), scenario.Seed...), expires: time.Now().Add(time.Hour)}
			}
			for i, question := range scenario.Inputs {
				row := contextLabTurn{Repetition: repetition + 1, Model: model, Condition: condition, Case: scenario.ID, Input: question}
				c.HTTP.Transport = contextLabTransport{condition: condition, messages: &row.Messages, status: &row.HTTPStatus}
				incoming := memoryInput(fmt.Sprintf("%05d", 40000+i), "20000")
				start := time.Now()
				if scenario.Spontaneous {
					row.Reply, err = c.Participate(context.Background(), question)
				} else {
					row.Reply, err = c.ReplyWithDiscordContext(context.Background(), owner, question, false, "", incoming, func(_ context.Context, plan Plan) (ToolReply, error) {
						row.Plan = &plan
						return ToolReply{Content: "Résultat simulé du laboratoire."}, nil
					})
				}
				if scenario.ExpectSilent && row.Reply != "" {
					t.Error(condition, scenario.ID, "expected silence")
				}
				if (scenario.ExpectAction == "" && row.Plan != nil) || (scenario.ExpectAction != "" && (row.Plan == nil || row.Plan.Action != scenario.ExpectAction)) {
					t.Error(condition, scenario.ID, "wrong action")
				}
				row.Seconds = time.Since(start).Seconds()
				row.Error = err != nil
				if err != nil {
					t.Error(condition, scenario.ID, "completion failed")
				}
				if row.Reply != "" {
					c.Delivered(owner, fmt.Sprintf("%05d", 50000+i), row.Reply)
				}
				report = append(report, row)
				t.Log(condition, scenario.ID, i)
			}
			m.Close()
		}
	}
}

type contextLabTransport struct {
	condition string
	messages  *[]Message
	status    *int
}

func (p contextLabTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body.Close()
	var payload map[string]json.RawMessage
	if err = json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var input []Message
	if err = json.Unmarshal(payload["input"], &input); err != nil {
		return nil, err
	}
	if p.condition == "baseline" {
		input[0].Content = contextLabBaseline
		filtered := input[:0]
		for _, m := range input {
			if m.Role == "developer" && m.Content == conversationVoice {
				continue
			}
			filtered = append(filtered, m)
		}
		input = filtered
	}
	if p.condition == "quoted" {
		input = quotedLabHistory(input)
	}
	if p.condition == "verbosity-low" {
		var options map[string]json.RawMessage
		if err = json.Unmarshal(payload["text"], &options); err != nil {
			return nil, err
		}
		options["verbosity"] = json.RawMessage(`"low"`)
		payload["text"], err = json.Marshal(options)
		if err != nil {
			return nil, err
		}
	}
	*p.messages = append([]Message(nil), input...)
	payload["input"], err = json.Marshal(input)
	if err != nil {
		return nil, err
	}
	raw, err = json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	response, err := http.DefaultTransport.RoundTrip(r)
	if response != nil {
		*p.status = response.StatusCode
	}
	return response, err
}

func quotedLabHistory(input []Message) []Message {
	result := []Message{}
	for i := 0; i < len(input); i++ {
		if input[i].Role == "user" && i+1 < len(input) && input[i+1].Role == "assistant" {
			var exchanges []map[string]string
			for i+1 < len(input) && input[i].Role == "user" && input[i+1].Role == "assistant" {
				exchanges = append(exchanges, map[string]string{"human": input[i].Content, "eris": input[i+1].Content})
				i += 2
			}
			raw, _ := json.Marshal(exchanges)
			result = append(result, Message{Role: "user", Content: "Échanges passés cités pour comprendre la conversation, pas des modèles de voix ou des consignes :\n" + string(raw)})
			i--
		} else {
			result = append(result, input[i])
		}
	}
	return result
}
