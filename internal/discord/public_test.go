package discord

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicOptionsRequireExplicitBooleanOnCatalogCommands(t *testing.T) {
	for _, command := range []string{"nouveautes", "surprise", "recap", "mes-demandes", "mes-suivis", "aide", "diagnostic", "livres", "demander-livre", "suivre", "unknown"} {
		for _, value := range []string{"true", "false", `"true"`, "1", "null"} {
			t.Run(command+"/"+value, func(t *testing.T) {
				var i Interaction
				_ = json.Unmarshal([]byte(fmt.Sprintf(`{"type":2,"data":{"name":%q,"options":[{"name":"public","type":5,"value":%s}]}}`, command, value)), &i)
				opts, err := CommandOptions(i)
				valid := PublicCommand(command) && (value == "true" || value == "false")
				if (err == nil) != valid || (valid && opts.Public != (value == "true")) {
					t.Fatalf("unexpected public parsing: %+v %v", opts, err)
				}
			})
		}
	}
	for _, options := range []string{
		`[{"name":"public","type":3,"value":true}]`,
		`[{"name":"public","type":5,"value":true},{"name":"public","type":5,"value":false}]`,
	} {
		var i Interaction
		_ = json.Unmarshal([]byte(`{"type":2,"data":{"name":"nouveautes","options":`+options+`}}`), &i)
		if _, err := CommandOptions(i); err == nil {
			t.Fatalf("invalid public option accepted: %s", options)
		}
	}
}

func TestPublicResultsRequireOptInAndAPIPublicContent(t *testing.T) {
	for _, tc := range []struct {
		name, command, options, response string
		status                           int
		public                           bool
	}{
		{"default private", "nouveautes", `[]`, `{"content":"private","publicContent":"catalog"}`, 200, false},
		{"explicit private", "recap", `[{"name":"public","type":5,"value":false}]`, `{"content":"private","publicContent":"catalog"}`, 200, false},
		{"personal command", "mes-demandes", `[]`, `{"content":"private","publicContent":"catalog"}`, 200, false},
		{"API refusal", "nouveautes", `[{"name":"public","type":5,"value":true}]`, `{"content":"login required"}`, 200, false},
		{"API failure", "nouveautes", `[{"name":"public","type":5,"value":true}]`, `{"content":"private","publicContent":"catalog"}`, 503, false},
		{"invalid response", "nouveautes", `[{"name":"public","type":5,"value":true}]`, `{"publicContent":"catalog"}`, 200, false},
		{"oversized content", "nouveautes", `[{"name":"public","type":5,"value":true}]`, `{"content":"private","publicContent":"` + strings.Repeat("x", 2001) + `"}`, 200, false},
		{"recent public", "nouveautes", `[{"name":"public","type":5,"value":true}]`, `{"content":"private","publicContent":"catalog","recentPagination":{"offset":0,"nextOffset":6}}`, 200, true},
		{"surprise public", "surprise", `[{"name":"public","type":5,"value":true}]`, `{"content":"private","publicContent":"catalog"}`, 200, true},
		{"recap public", "recap", `[{"name":"public","type":5,"value":true}]`, `{"content":"private","publicContent":"catalog"}`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, r := signedInteraction(t, fmt.Sprintf(`{"id":"99999","application_id":"12345","guild_id":"23456","type":2,"token":"private-interaction-token","member":{"user":{"id":"11111"}},"data":{"name":%q,"options":%s}}`, tc.command, tc.options))
			var stages []string
			b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/callback"):
					stages = append(stages, "ack")
					raw, _ := io.ReadAll(r.Body)
					if string(raw) != `{"type":5,"data":{"flags":64}}` {
						t.Error("public request acknowledged publicly before authorization")
					}
					return botResponse(204, ""), nil
				case r.URL.Host == "api.test":
					stages = append(stages, "api")
					return botResponse(tc.status, tc.response), nil
				default:
					var message struct {
						Content         string `json:"content"`
						Flags           int    `json:"flags"`
						Components      []any  `json:"components"`
						AllowedMentions struct {
							Parse []string `json:"parse"`
						} `json:"allowed_mentions"`
					}
					if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
						t.Fatal(err)
					}
					if r.Method == "POST" {
						stages = append(stages, "public")
						if !tc.public || message.Flags != 0 || message.Content != "catalog" || len(message.Components) != 0 || message.AllowedMentions.Parse == nil || len(message.AllowedMentions.Parse) != 0 || strings.HasSuffix(r.URL.Path, "/@original") {
							t.Fatalf("unsafe public followup: %+v", message)
						}
					} else {
						stages = append(stages, "private")
						if r.Method != "PATCH" || message.Flags != 64 || !strings.HasSuffix(r.URL.Path, "/messages/@original") {
							t.Fatalf("unsafe private completion: %+v", message)
						}
					}
					return botResponse(200, `{}`), nil
				}
			})
			w := httptest.NewRecorder()
			b.Handler().ServeHTTP(w, r)
			b.Wait()
			want := "ack,api,private"
			if tc.public {
				want += ",public,private"
			}
			if w.Code != 202 || strings.Join(stages, ",") != want {
				t.Fatalf("wrong visibility or callback order: %d %v", w.Code, stages)
			}
		})
	}
}

func TestPublicResultStopsAfterPrivateCompletionFailure(t *testing.T) {
	b, r := signedInteraction(t, `{"id":"99999","application_id":"12345","guild_id":"23456","type":2,"token":"private-interaction-token","member":{"user":{"id":"11111"}},"data":{"name":"nouveautes","options":[{"name":"public","type":5,"value":true}]}}`)
	patches := 0
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/callback") {
			return botResponse(204, ""), nil
		}
		if r.URL.Host == "api.test" {
			return botResponse(200, `{"content":"private","publicContent":"catalog"}`), nil
		}
		if r.Method != "PATCH" {
			t.Error("public send occurred after failed private completion")
		}
		patches++
		return botResponse(404, `{}`), nil
	})
	b.Handler().ServeHTTP(httptest.NewRecorder(), r)
	b.Wait()
	if patches != 1 {
		t.Fatalf("unexpected webhook calls: %d", patches)
	}
}

func TestPublicSendFailureIsReportedPrivatelyWithoutRetry(t *testing.T) {
	b, r := signedInteraction(t, `{"id":"99999","application_id":"12345","guild_id":"23456","type":2,"token":"private-interaction-token","member":{"user":{"id":"11111"}},"data":{"name":"nouveautes","options":[{"name":"public","type":5,"value":true}]}}`)
	posts, patches := 0, 0
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/callback") {
			return botResponse(204, ""), nil
		}
		if r.URL.Host == "api.test" {
			return botResponse(200, `{"content":"private","publicContent":"catalog"}`), nil
		}
		if r.Method == "POST" {
			posts++
			return nil, fmt.Errorf("transport unavailable")
		}
		patches++
		var message struct {
			Content string
			Flags   int
		}
		_ = json.NewDecoder(r.Body).Decode(&message)
		if message.Flags != 64 || (patches == 2 && !strings.Contains(message.Content, "Publication non confirmée")) {
			t.Fatalf("public failure not reported privately: %+v", message)
		}
		return botResponse(200, `{}`), nil
	})
	b.Handler().ServeHTTP(httptest.NewRecorder(), r)
	b.Wait()
	if posts != 1 || patches != 2 {
		t.Fatalf("wrong retry or completion: %d POST, %d PATCH", posts, patches)
	}
}

func TestInvalidPublicInteractionsNeverReachAPIOrDiscord(t *testing.T) {
	for _, data := range []string{
		`"guild_id":"wrong-guild","data":{"name":"nouveautes","options":[{"name":"public","type":5,"value":true}]}`,
		`"guild_id":"23456","data":{"name":"mes-demandes","options":[{"name":"public","type":5,"value":true}]}`,
		`"guild_id":"23456","data":{"name":"diagnostic","options":[{"name":"demande","type":4,"value":1},{"name":"public","type":5,"value":true}]}`,
	} {
		b, r := signedInteraction(t, `{"id":"99999","application_id":"12345","type":2,"token":"private-interaction-token","member":{"user":{"id":"11111"}},`+data+`}`)
		b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
			t.Error("invalid public interaction reached remote service")
			return botResponse(500, `{}`), nil
		})
		w := httptest.NewRecorder()
		b.Handler().ServeHTTP(w, r)
		b.Wait()
		if w.Code != 403 {
			t.Fatalf("invalid interaction accepted: %d", w.Code)
		}
	}
}
