package eris

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func dollyCompletion(w http.ResponseWriter, model, finish, content string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"model": model, "choices": []any{map[string]any{
			"finish_reason": finish, "message": map[string]string{
				"role": "assistant", "content": content, "reasoning_content": strings.Repeat("ignored ", 3000),
			},
		}},
	})
}

func TestDollyChatCompletion(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "key")
	for _, test := range []struct{ model, finish, key, want string }{
		{"dolly2-security", "stop", "sk-" + strings.Repeat("a", 22), "Ouais."},
		{"dolly2-security", "length", "sk-" + strings.Repeat("b", 22), ""},
		{"glm-5.3", "stop", "sk-" + strings.Repeat("c", 22), ""},
		{"dolly2-security", "tool_calls", "sk-" + strings.Repeat("d", 22), ""},
	} {
		func() {
			if err := os.WriteFile(keyFile, []byte(test.key+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Model    string `json:"model"`
					Effort   string `json:"reasoning_effort"`
					Max      int    `json:"max_tokens"`
					Stream   bool   `json:"stream"`
					Response struct {
						Type string `json:"type"`
					} `json:"response_format"`
				}
				if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+test.key ||
					json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Model != "dolly2-security" ||
					payload.Effort != "low" || payload.Max != 1536 || payload.Stream || payload.Response.Type != "json_object" {
					t.Error("transport changed")
				}
				dollyCompletion(w, test.model, test.finish, "Ouais.")
			}))
			defer server.Close()
			chat := New(server.URL+"/v1", "", "dolly2-security")
			chat.KeyFile, chat.V2 = keyFile, true
			got, err := chat.complete(context.Background(), []Message{{Role: "user", Content: "Bonjour"}}, true)
			if got != test.want || (err != nil) != (test.want == "") {
				t.Errorf("%s with finish %s returned %q, %v", test.model, test.finish, got, err)
			}
		}()
	}
}

func TestDollyRepairsMissedMemoryCorrectionOnDelivery(t *testing.T) {
	const owner = "12345:23456:34567"
	input := DiscordInput{Speaker: Speaker{ID: "23456", Name: "Nora"}}
	first := `{"action":"chat","text":"Le jasmin, ça se défend.","memories":[{"kind":"preference","topic":"thé","quote":"Je préfère le thé au jasmin","summary":"Préfère le jasmin"}]}`
	missed := `{"action":"chat","text":"C’est corrigé."}`
	repaired := `{"action":"chat","text":"Place à la bergamote.","memories":[{"kind":"preference","topic":"the","quote":"Je préfère désormais le thé à la bergamote","summary":"Préfère la bergamote"}]}`
	replies := make(chan string, 5)
	for _, reply := range []string{first, missed, repaired, missed, repaired} {
		replies <- reply
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case reply := <-replies:
			dollyCompletion(w, "dolly2-security", "stop", reply)
		default:
			t.Error("unexpected extraction")
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	memory, err := OpenMemory(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	chat := New(server.URL, "sk-"+strings.Repeat("z", 22), "dolly2-security")
	chat.V2, chat.Memory = true, memory
	run := func(_ context.Context, _ Plan) (ToolReply, error) {
		t.Fatal("a memory reply ran a tool")
		return ToolReply{}, nil
	}
	call := func(id, question string) string {
		t.Helper()
		input.MessageID = id
		reply, err := chat.ReplyWithDiscordContext(context.Background(), owner, question, false, "", input, run)
		if err != nil {
			t.Fatal(err)
		}
		return reply
	}
	checkNote := func(word string) {
		t.Helper()
		var total int
		var quote string
		if err := memory.db.QueryRow("SELECT COUNT(*), MIN(quote) FROM eris_notes").Scan(&total, &quote); err != nil || total != 1 || !strings.Contains(quote, word) {
			t.Fatalf("want one note for %s, got %d %q, %v", word, total, quote, err)
		}
	}
	initial := call("45670", "Je préfère le thé au jasmin. Retiens cette préférence durable.")
	chat.Delivered(owner, "45700", initial)
	checkNote("jasmin")
	correction := "Je préfère désormais le thé à la bergamote. Corrige la préférence durable sur le même sujet."
	if reply := call("45671", correction); reply != "Place à la bergamote." {
		t.Fatalf("narrated a missing write: %q", reply)
	}
	checkNote("jasmin")
	// Drop the undelivered session, keeping only its durable state.
	chat = New(server.URL, "sk-"+strings.Repeat("z", 22), "dolly2-security")
	chat.V2, chat.Memory = true, memory
	chat.Delivered(owner, "45701", call("45672", correction))
	checkNote("bergamote")
	if len(replies) != 0 {
		t.Fatal("did not run the scoped extraction")
	}
}

func TestDollySeasonGroundingAndReceiptVoice(t *testing.T) {
	responses := make(chan string, 4)
	for _, reply := range []string{
		`{"action":"prepare","query":"Les Veilleurs d’Orbite","kind":"show","seasons":[2]}`,
		`{"opening":1}`,
		`{"action":"prepare","query":"Les Veilleurs d’Orbite","kind":"show","seasons":[2]}`,
		`{"opening":99}`,
	} {
		responses <- reply
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case reply := <-responses:
			dollyCompletion(w, "dolly2-security", "stop", reply)
		default:
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	chat := New(server.URL, "sk-"+strings.Repeat("z", 22), "dolly2-security")
	chat.V2 = true
	const owner = "12345:23456:34567"
	const facts = "Quelle saison veux-tu ?"
	chat.sessions[owner] = &conversation{messages: []Message{
		{Role: "user", Content: "Les Veilleurs d’Orbite sont disponibles sur Plex ?"},
		{Role: "assistant", Content: "La saison 2 est manquante.", toolResult: true},
	}}
	got, err := chat.ReplyWithTools(context.Background(), owner, "Demande la série Les Veilleurs d’Orbite dans Seerr.", false, func(_ context.Context, p Plan) (ToolReply, error) {
		if len(p.Seasons) != 0 {
			t.Errorf("assistant result authorized a season: %v", p.Seasons)
		}
		return ToolReply{Content: facts}, nil
	})
	if err != nil || got == facts || MediaReplyFacts(got) != facts {
		t.Fatalf("fact changed or voice missing: %q, %v", got, err)
	}
	chat.Delivered(owner, "45670", got)
	const receipt = "Demande #27 créée : Les Veilleurs d’Orbite, saison 2."
	got, err = chat.ReplyWithTools(context.Background(), owner, "Demande la saison 2 des Veilleurs d’Orbite.", false, func(_ context.Context, p Plan) (ToolReply, error) {
		if len(p.Seasons) != 1 || p.Seasons[0] != 2 {
			t.Errorf("explicit season dropped: %+v", p)
		}
		return ToolReply{Content: receipt}, nil
	})
	if err != nil || got != receipt {
		t.Fatalf("invalid narration changed receipt: %q, %v", got, err)
	}
}
