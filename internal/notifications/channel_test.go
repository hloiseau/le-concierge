package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
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
