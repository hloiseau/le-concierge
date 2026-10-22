package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/store"
)

func TestErisMetricsNeverInventMissingOrStaleMeasurements(t *testing.T) {
	v := 42.0
	got := erisMetrics(map[string]store.MetricValue{
		"cpu_percent":       {Status: "available", Value: &v},
		"cpu_temperature":   {Status: "stale", Value: &v},
		"memory_used_bytes": {Status: "available"},
		"private_metric":    {Status: "available", Value: &v},
	})
	if !strings.Contains(got, "CPU : 42 % · indisponible") || !strings.Contains(got, "RAM : indisponible / indisponible") || strings.Contains(got, "private_metric") {
		t.Fatal(got)
	}
}

func TestErisNaturalRequestVerifiesDiscordAndCreatesAtMostOnce(t *testing.T) {
	for _, state := range []string{"valid", "other member", "other channel", "negative", "conditional", "unrelated reply", "expired", "private mismatch", "forged plan", "cancel"} {
		t.Run(state, func(t *testing.T) {
			s := fixture(t)
			s.Config.BotAPISecret = strings.Repeat("s", 32)
			s.Config.DiscordBotToken = "fixture"
			seed(t, s, "11111", true)
			seed(t, s, "22222", true)
			var posts atomic.Int32
			question, member, channel := "demande Arrival", "11111", "33333"
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "discord.com" {
					if !strings.Contains(r.URL.Path, "/messages/") {
						return response(200, fmt.Sprintf(`{"id":%q,"guild_id":"23456"}`, channel)), nil
					}
					parts := strings.Split(r.URL.Path, "/")
					m := map[string]any{"id": parts[len(parts)-1], "channel_id": channel, "timestamp": time.Now(), "content": "<@12345> " + question, "author": map[string]string{"id": member}, "mentions": []any{map[string]string{"id": "12345"}}}
					if state == "unrelated reply" && strings.HasPrefix(question, "oui") {
						m["referenced_message"] = map[string]string{"content": "Tu aimes le popcorn ?"}
					}
					raw, _ := json.Marshal(m)
					return response(200, string(raw)), nil
				}
				switch r.URL.Path {
				case "/api/v1/user":
					return response(200, `{"pageInfo":{"results":1},"results":[{"id":2,"plexId":11111}]}`), nil
				case "/api/v1/auth/me":
					return response(200, `{"id":2}`), nil
				case "/api/v1/search":
					return response(200, `{"results":[{"id":123,"mediaType":"movie","title":"Arrival","releaseDate":"2016-01-01"},{"id":456,"mediaType":"movie","title":"Bronson","releaseDate":"2009-01-01"}]}`), nil
				case "/api/v1/request":
					if r.Method != "POST" {
						t.Error("unexpected request method")
					}
					posts.Add(1)
					return response(201, `{"id":9,"type":"movie","requestedBy":{"id":2},"media":{"tmdbId":456}}`), nil
				default:
					t.Error("unexpected route", r.URL.Path)
					return response(500, `{}`), nil
				}
			})
			decode := func(in eris.ToolRequest) eris.ToolReply {
				t.Helper()
				w := callErisTool(s, in)
				var result eris.ToolReply
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
					t.Fatalf("%d %s", w.Code, w.Body)
				}
				return result
			}
			prepared := decode(eris.ToolRequest{Plan: eris.Plan{Action: "prepare", Query: "Arrival", Kind: "movie"}, ChannelID: channel, MessageID: "90001"})
			if len(prepared.Buttons) != 2 {
				t.Fatal(prepared)
			}
			question = "prends le deuxième"
			chosen := decode(eris.ToolRequest{Choice: prepared.Buttons[1].ID, ChannelID: channel, MessageID: "90002"})
			if len(chosen.Buttons) != 2 || !strings.Contains(chosen.Content, "Bronson (2009)") || posts.Load() != 0 {
				t.Fatal("choice bypassed title confirmation", chosen)
			}
			choice := chosen.Buttons[0].ID
			question = "oui, vas-y"
			switch state {
			case "other member":
				member = "22222"
			case "other channel":
				channel = "44444"
			case "negative":
				question = "non, ne confirme pas"
			case "conditional":
				question = "oui mais pas maintenant"
			case "cancel":
				question = "annule"
				choice = chosen.Buttons[1].ID
			case "expired", "private mismatch":
				key := strings.Split(choice, ":")[2]
				p := s.erisPending[key]
				if state == "expired" {
					p.Expires = time.Now().Add(-time.Minute)
				} else {
					p.Private = true
				}
				s.erisPending[key] = p
			}
			var wg sync.WaitGroup
			for index := 0; index < 2; index++ {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					in := eris.ToolRequest{Choice: choice, ChannelID: channel, MessageID: fmt.Sprint(90003 + index)}
					if state == "forged plan" {
						in.Plan = eris.Plan{Action: "prepare", Query: "Arrival"}
					}
					w := callErisTool(s, in)
					if state != "valid" && strings.Contains(w.Body.String(), "créée") {
						t.Error("false success", w.Body)
					}
				}(index)
			}
			wg.Wait()
			want := int32(0)
			if state == "valid" {
				want = 1
			}
			if posts.Load() != want {
				t.Fatalf("posts=%d want=%d", posts.Load(), want)
			}
		})
	}
}
