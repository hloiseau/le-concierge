package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestPrivateDiscordDigestAndRevalidation(t *testing.T) {
	d := NewDiscord("test-token")
	calls, checks := 0, 0
	d.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "discord.com" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bot test-token" {
			t.Fatalf("invalid private transport request")
		}
		if _, ok := r.Context().Deadline(); !ok {
			t.Fatal("missing bounded request deadline")
		}
		if calls == 1 {
			if r.URL.Path != "/api/v10/users/@me/channels" {
				t.Fatal(r.URL.Path)
			}
			var payload map[string]string
			json.NewDecoder(r.Body).Decode(&payload)
			if payload["recipient_id"] != "123" || len(payload) != 1 {
				t.Fatalf("wrong recipient: %v", payload)
			}
			return response(200, `{"id":"456","type":1,"recipients":[{"id":"123"}]}`), nil
		}
		if checks != 1 || r.URL.Path != "/api/v10/channels/456/messages" {
			t.Fatal("message posted before final authorization or to wrong channel")
		}
		var payload struct {
			Content  string `json:"content"`
			Nonce    string `json:"nonce"`
			Enforce  bool   `json:"enforce_nonce"`
			Flags    int    `json:"flags"`
			Mentions struct {
				Parse []string `json:"parse"`
			} `json:"allowed_mentions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Nonce != "stable-digest-nonce" || !payload.Enforce || payload.Flags != 4 || payload.Mentions.Parse == nil || len(payload.Mentions.Parse) != 0 {
			t.Fatalf("unsafe message payload: %+v", payload)
		}
		if strings.Contains(payload.Content, "@everyone") || strings.Contains(payload.Content, "<@123>") || !strings.Contains(payload.Content, "Film deux") || !strings.Contains(payload.Content, "<https://app.plex.tv/>") {
			t.Fatal(payload.Content)
		}
		return response(200, `{"id":"789","channel_id":"456"}`), nil
	})
	err := d.SendValidated(context.Background(), "123", "stable-digest-nonce", []Message{{Title: "@everyone <@123>", URL: "https://app.plex.tv/", Description: "Demain"}, {Title: "Film deux"}}, func() error { checks++; return nil })
	if err != nil || calls != 2 {
		t.Fatalf("send: %v calls=%d", err, calls)
	}
}

func TestDiscordRefusesPublicGroupAndWrongRecipient(t *testing.T) {
	for _, body := range []string{
		`{"id":"456","type":0,"recipients":[{"id":"123"}]}`,
		`{"id":"456","type":3,"recipients":[{"id":"123"}]}`,
		`{"id":"456","type":1,"recipients":[{"id":"321"}]}`,
		`{"id":"456","type":1,"recipients":[{"id":"123"},{"id":"321"}]}`,
	} {
		d := NewDiscord("token")
		calls := 0
		d.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, body), nil })
		err := d.Send(context.Background(), "123", "nonce", []Message{{Title: "Private"}})
		var failure *DeliveryError
		if !errors.As(err, &failure) || !failure.Permanent || failure.Code != "invalid_private_channel" || calls != 1 {
			t.Fatalf("unsafe channel accepted: %v calls=%d", err, calls)
		}
	}
}

func TestDiscordRevokedAfterOpeningDM(t *testing.T) {
	d := NewDiscord("token")
	calls := 0
	d.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(200, `{"id":"456","type":1,"recipients":[{"id":"123"}]}`), nil
	})
	err := d.SendValidated(context.Background(), "123", "nonce", []Message{{Title: "Private"}}, func() error { return errors.New("revoked") })
	var failure *DeliveryError
	if !errors.As(err, &failure) || failure.Code != "authorization_changed" || !failure.Permanent || calls != 1 {
		t.Fatalf("revocation leaked message: %v calls=%d", err, calls)
	}
}

func TestDiscordFailureClassificationAndPrivacy(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		code      string
		permanent bool
		retry     time.Duration
	}{
		{"closed", 403, `{"code":50007,"message":"sensitive details"}`, "dm_unavailable", true, 0},
		{"closed_code", 400, `{"code":50007}`, "dm_unavailable", true, 0},
		{"token", 401, `{"message":"sensitive details"}`, "bot_unauthorized", true, 0},
		{"server", 503, `{"message":"sensitive details"}`, "upstream_failure", false, 0},
		{"rate", 429, `{"retry_after":64.57}`, "rate_limited", false, 64570 * time.Millisecond},
		{"invalid_rate", 429, `{"retry_after":-1}`, "invalid_rate_limit", true, 0},
		{"unbounded_rate", 429, `{"retry_after":86401}`, "invalid_rate_limit", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDiscord("token")
			calls := 0
			d.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(tc.status, tc.body), nil })
			err := d.Send(context.Background(), "123", "nonce", []Message{{Title: "Private"}})
			var failure *DeliveryError
			if !errors.As(err, &failure) || failure.Code != tc.code || failure.Permanent != tc.permanent || failure.RetryAfter != tc.retry || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("failure=%+v", err)
			}
			if tc.status == 429 || tc.status == 401 {
				d.Send(context.Background(), "124", "nonce2", []Message{{Title: "Private"}})
				if calls != 1 {
					t.Fatal("global cooldown/invalid token ignored")
				}
			}
		})
	}
}

func TestDiscordRateLimitHeaderAndTransportErrors(t *testing.T) {
	d := NewDiscord("token")
	d.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		r := response(429, `{"retry_after":1.5}`)
		r.Header.Set("Retry-After", "2.5")
		return r, nil
	})
	err := d.Send(context.Background(), "123", "nonce", []Message{{Title: "Private"}})
	var failure *DeliveryError
	if !errors.As(err, &failure) || failure.RetryAfter != 2500*time.Millisecond {
		t.Fatal(err)
	}
	d = NewDiscord("token")
	d.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private URL and secret token") })
	err = d.Send(context.Background(), "123", "nonce", []Message{{Title: "Private"}})
	if !errors.As(err, &failure) || failure.Permanent || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
}

func TestDiscordNoSendWhenDisabledOrInputInvalid(t *testing.T) {
	for _, token := range []string{"", "token"} {
		d := NewDiscord(token)
		d.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid delivery made network request")
			return nil, nil
		})
		id := "123"
		if token != "" {
			id = "bad/path"
		}
		if err := d.Send(context.Background(), id, "nonce", []Message{{Title: "Private"}}); err == nil {
			t.Fatal("missing validation")
		}
	}
}

func TestDiscordNeverFollowsRedirectsWithBotCredentials(t *testing.T) {
	d := NewDiscord("token")
	calls := 0
	d.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "discord.com" {
			t.Fatal("bot token sent to redirect target")
		}
		res := response(http.StatusFound, `{}`)
		res.Header.Set("Location", "https://untrusted.example.test/")
		return res, nil
	})
	err := d.Send(context.Background(), "123", "nonce", []Message{{Title: "Private"}})
	var failure *DeliveryError
	if !errors.As(err, &failure) || !failure.Permanent || calls != 1 {
		t.Fatalf("redirect accepted: %v calls=%d", err, calls)
	}
}

func TestDiscordDigestLengthAndUnsafeURLs(t *testing.T) {
	items := make([]Message, 100)
	for i := range items {
		items[i] = Message{Title: strings.Repeat("ðŸ˜€*", 250), URL: "javascript:alert(1)", Description: strings.Repeat("details\n", 100)}
	}
	body := digest(items)
	if len(utf16.Encode([]rune(body))) > 2000 || strings.Contains(body, "javascript:") || !strings.Contains(body, "dans le Hub") {
		t.Fatal("invalid bounded digest")
	}
	for _, u := range []string{"https://name:secret@example.com/", "https://example.com/> @everyone", "file:///private", "https://example.com/\nnext"} {
		if strings.Contains(digest([]Message{{Title: "Title", URL: u}}), u) {
			t.Fatalf("unsafe URL retained: %q", u)
		}
	}
}
