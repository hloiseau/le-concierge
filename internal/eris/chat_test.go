package eris

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestConversationIsolationAndForget(t *testing.T) {
	var prompts [][]Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer private-key" {
			t.Error("wrong inference target or credential")
		}
		var body struct {
			Model    string
			Messages []Message
			Stream   bool
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "eris-qwen3-4b-v1" || body.Stream {
			t.Error("invalid completion request")
		}
		if len(body.Messages) == 0 || body.Messages[0].Role != "system" || utf8.RuneCountInString(body.Messages[0].Content) > 2000 {
			t.Error("persona exceeds the inference protocol limit")
		}
		prompts = append(prompts, body.Messages)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Je veille sur les lieux."}}]}`))
	}))
	defer server.Close()
	c := New(server.URL+"/v1", "private-key", "eris-qwen3-4b-v1")
	for _, request := range []struct {
		owner, question string
		forget          bool
	}{
		{"guild:alice", "Bonjour", false}, {"guild:bob", "Salut", false},
		{"guild:alice", "Tu te souviens ?", false}, {"guild:alice", "Nouvelle conversation", true},
	} {
		if s := c.sessions[request.owner]; s != nil {
			s.next = time.Time{}
		}
		if _, err := c.Reply(context.Background(), request.owner, request.question, request.forget); err != nil {
			t.Fatal(err)
		}
	}
	if len(prompts) != 4 || len(prompts[0]) != 2 || len(prompts[1]) != 2 || len(prompts[2]) != 4 || len(prompts[3]) != 2 {
		t.Fatalf("wrong conversation boundaries: %+v", prompts)
	}
	if prompts[2][1].Content != "Bonjour" || prompts[2][3].Content != "Tu te souviens ?" || prompts[3][1].Content != "Nouvelle conversation" {
		t.Fatal("history lost, shared, or not forgotten")
	}
	c.URL = ""
	if _, err := c.Reply(context.Background(), "guild:alice", "", true); err != nil || c.sessions["guild:alice"] != nil {
		t.Fatal("offline forgetting failed")
	}
}

func TestConcurrencyAndCooldown(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Bonjour."}}]}`))
	}))
	defer server.Close()
	c := New(server.URL, "", "model")
	done := make(chan error, 1)
	go func() { _, err := c.Reply(context.Background(), "alice", "Bonjour", false); done <- err }()
	<-started
	if _, err := c.Reply(context.Background(), "alice", "Encore", false); !errors.Is(err, ErrBusy) {
		t.Fatal("concurrent turn accepted")
	}
	if _, err := c.Reply(context.Background(), "alice", "", true); !errors.Is(err, ErrBusy) {
		t.Fatal("forgot an active generation")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reply(context.Background(), "alice", "Encore", false); !errors.Is(err, ErrBusy) {
		t.Fatal("cooldown bypassed")
	}
}

func TestFailureDoesNotPoisonHistoryOrExposeProviderErrors(t *testing.T) {
	for _, body := range []string{`{"error":"secret-provider-details"}`, `{"choices":[]}`, `{"choices":[{"message":{"role":"user","content":"secret-provider-details"}}]}`, `{"choices":[{"message":{"role":"assistant","content":""}}]}`, strings.Repeat("x", 17000)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		c := New(server.URL, "", "model")
		if reply, err := c.Reply(context.Background(), "alice", "Bonjour", false); reply != "" || !errors.Is(err, ErrUnavailable) || len(c.sessions["alice"].messages) != 0 {
			t.Fatal("failed completion changed history or leaked response")
		}
		server.Close()
	}
}

func TestInferenceDoesNotFollowRedirects(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer server.Close()
	c := New(server.URL, "private-key", "model")
	if _, err := c.Reply(context.Background(), "alice", "Bonjour", false); err == nil || redirected {
		t.Fatal("inference followed redirect with private conversation")
	}
}

func TestForgetClearsPrivateAndChannelMemoryOnlyForThisMember(t *testing.T) {
	c := New("", "", "")
	for _, key := range []string{"guild:alice", "guild:alice:channel1", "guild:alice:channel2", "guild:alice2:channel1", "guild:bob:channel1"} {
		c.sessions[key] = &conversation{expires: time.Now().Add(time.Hour)}
	}
	c.sessions["guild:alice:channel1"].busy = true
	if _, err := c.Reply(context.Background(), "guild:alice", "", true); !errors.Is(err, ErrBusy) || len(c.sessions) != 5 {
		t.Fatal("active channel conversation was forgotten or partial deletion occurred")
	}
	c.sessions["guild:alice:channel1"].busy = false
	if _, err := c.Reply(context.Background(), "guild:alice", "", true); err != nil {
		t.Fatal(err)
	}
	if len(c.sessions) != 2 || c.sessions["guild:bob:channel1"] == nil || c.sessions["guild:alice2:channel1"] == nil {
		t.Fatal("wrong forgetting boundary")
	}
}
