package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/discord"
)

type registrationTransport func(*http.Request) (*http.Response, error)

func (f registrationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCommandRegistrationOnStartup(t *testing.T) {
	for _, tc := range []struct {
		name, token, log string
		status           int
		networkFailure   bool
	}{
		{"token configured", "private-fixture-token", "Discord guild commands updated", 200, false},
		{"token absent", "", "registration skipped", 0, false},
		{"Discord rejects registration", "private-fixture-token", "registration failed", 403, false},
		{"network failure", "private-fixture-token", "registration failed", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var log bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			bot := discord.NewBot(config.Config{DiscordClientID: "12345", DiscordGuildID: "23456", DiscordBotToken: tc.token})
			calls := 0
			bot.Client.Transport = registrationTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 15*time.Second {
					t.Error("registration has no bounded startup deadline")
				}
				if tc.networkFailure {
					return nil, errors.New("transport leaked private-fixture-token")
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("private-fixture-token"))}, nil
			})
			registerOnStart(context.Background(), bot)
			if (tc.token == "" && calls != 0) || (tc.token != "" && calls != 1) {
				t.Fatalf("unexpected registration count %d", calls)
			}
			if !strings.Contains(log.String(), tc.log) || strings.Contains(log.String(), "private-fixture-token") {
				t.Fatalf("unsafe or misleading registration log: %s", log.String())
			}
			w := httptest.NewRecorder()
			bot.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
			if w.Code != 200 {
				t.Fatalf("registration failure disabled the bot: %d", w.Code)
			}
		})
	}
}

func TestCommandRegistrationDoesNotBlockServingAndStopsWithBot(t *testing.T) {
	bot := discord.NewBot(config.Config{DiscordClientID: "12345", DiscordGuildID: "23456", DiscordBotToken: "fixture-token"})
	started := make(chan struct{})
	bot.Client.Transport = registrationTransport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { registerOnStart(ctx, bot); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("registration did not start")
	}
	w := httptest.NewRecorder()
	bot.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 {
		t.Fatalf("registration blocked serving: %d", w.Code)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("registration survived bot shutdown")
	}
}
