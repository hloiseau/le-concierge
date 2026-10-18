package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestChannelMentionsOnlyExplicitRequestersInConfiguredGuild(t *testing.T) {
	for _, wrongGuild := range []bool{false, true} {
		t.Run(map[bool]string{false: "allowed", true: "wrong guild"}[wrongGuild], func(t *testing.T) {
			d := NewDiscord("fixture-token")
			posts := 0
			checks := 0
			d.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == "GET" {
					guild := "123"
					if wrongGuild {
						guild = "999"
					}
					return response(200, `{"id":"456","guild_id":"`+guild+`","type":0}`), nil
				}
				posts++
				if r.URL.Path != "/api/v10/channels/456/messages" || checks != 1 {
					t.Fatal("wrong destination or validation order")
				}
				var body struct {
					Content  string `json:"content"`
					Mentions struct {
						Parse []string `json:"parse"`
						Users []string `json:"users"`
					} `json:"allowed_mentions"`
					Nonce   string `json:"nonce"`
					Enforce bool   `json:"enforce_nonce"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if len(body.Mentions.Parse) != 0 || len(body.Mentions.Users) != 1 || body.Mentions.Users[0] != "789" || !body.Enforce || body.Nonce != "stable-nonce" {
					t.Fatalf("unsafe payload: %+v", body)
				}
				if !strings.Contains(body.Content, "<@789>") || strings.Contains(body.Content, "<@111>") || strings.Contains(body.Content, "@everyone") {
					t.Fatal(body.Content)
				}
				return response(200, `{"id":"111","channel_id":"456"}`), nil
			})
			err := d.SendChannelValidated(context.Background(), "123", "456", "stable-nonce", Message{Title: "@everyone <@111> film", Description: "départ demain"}, []string{"789", "789"}, func() error { checks++; return nil })
			if wrongGuild {
				if err == nil || posts != 0 {
					t.Fatal("wrong guild accepted")
				}
			} else if err != nil || posts != 1 {
				t.Fatalf("%v, posts=%d", err, posts)
			}
		})
	}
}

func TestChannelValidationKeepsRetryabilityAndSeparateHTTPDeadlines(t *testing.T) {
	for _, mode := range []string{"available", "source unavailable", "denied", "caller cancelled"} {
		t.Run(mode, func(t *testing.T) {
			d := NewDiscord("fixture-token")
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			var lookup context.Context
			var lookupDeadline time.Time
			posts := 0
			d.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 5*time.Second || r.Context().Err() != nil {
					t.Fatal("Discord request is not bounded to five seconds")
				}
				if r.Method == "GET" {
					lookup, lookupDeadline = r.Context(), deadline
					return response(200, `{"id":"456","guild_id":"123","type":0}`), nil
				}
				posts++
				if deadline.Before(lookupDeadline) || r.Context() == lookup {
					t.Fatal("source validation consumed the POST's timeout")
				}
				return response(200, `{"id":"111","channel_id":"456"}`), nil
			})
			err := d.SendChannelValidated(ctx, "123", "456", "nonce", Message{Title: "Film"}, []string{"789"}, func() error {
				if lookup == nil || lookup.Err() != context.Canceled || ctx.Err() != nil {
					t.Fatal("source validation must use the live caller context after closing the lookup")
				}
				switch mode {
				case "source unavailable":
					return &DeliveryError{Code: "source_unavailable", RetryAfter: time.Minute}
				case "denied":
					return errors.New("revoked")
				case "caller cancelled":
					cancel()
				}
				return nil
			})
			if mode == "available" {
				if err != nil || posts != 1 {
					t.Fatal("valid reminder was not posted once", posts, err)
				}
				return
			}
			var failure *DeliveryError
			if posts != 0 || !errors.As(err, &failure) || failure.Permanent != (mode == "denied") {
				t.Fatal("failed revalidation posted or lost retryability", posts, err)
			}
			if mode == "source unavailable" && (failure.Code != "source_unavailable" || failure.RetryAfter != time.Minute) {
				t.Fatal("transient validation error was replaced", failure)
			}
		})
	}
}
