package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type discordCreationTransport func(*http.Request) (*http.Response, error)

func (f discordCreationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDiscordCreationNeverRetriesAmbiguousPostOrFollowsRedirect(t *testing.T) {
	for _, status := range []int{201, 400, 403, 429, 500, 302, 0} {
		c := New()
		calls := 0
		c.HTTP.Transport = discordCreationTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.Method != "POST" || r.URL.String() != "https://discord.com/api/v10/channels/33333/messages" || r.Header.Get("Authorization") != "Bot test-token" {
				t.Fatal("unexpected write target")
			}
			if status == 0 {
				return nil, errors.New("synthetic network loss")
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://untrusted.example/"}}, Body: io.NopCloser(strings.NewReader(`{"id":"55555","channel_id":"33333","poll":{"question":{"text":"Question"}}}`))}, nil
		})
		_, err := c.CreateDiscord(context.Background(), "test-token", "23456", "33333", "poll", map[string]any{})
		if calls != 1 {
			t.Fatal("automatic retry or redirect", status, calls)
		}
		if status == 201 && err != nil || status >= 400 && status < 500 && !errors.Is(err, ErrDenied) || (status == 500 || status == 302 || status == 0) && !errors.Is(err, ErrDiscordUncertain) {
			t.Fatal(status, err)
		}
	}
}
