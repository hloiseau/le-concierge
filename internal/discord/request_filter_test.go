package discord

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestStatusOptionsAndButtons(t *testing.T) {
	for _, status := range []string{"all", "pending", "approved", "processing", "partial", "available", "declined", "error", "unknown"} {
		t.Run(status, func(t *testing.T) {
			var i Interaction
			_ = json.Unmarshal([]byte(`{"type":2,"data":{"name":"mes-demandes","options":[{"name":"etat","type":3,"value":"`+status+`"}]}}`), &i)
			want := status
			if want == "all" {
				want = ""
			}
			options, err := CommandOptions(i)
			command, offset, commandErr := Command(i)
			if err != nil || commandErr != nil || options.Status != want || command != "mes-demandes" || offset != 0 {
				t.Fatalf("status option not parsed: %+v %s %d %v %v", options, command, offset, err, commandErr)
			}
			rows := (RequestPagination{Offset: 10, Total: 21, Status: want}).Components("11111")
			if len(rows) != 1 {
				t.Fatalf("missing buttons: %v", rows)
			}
			buttons := rows[0]["components"].([]map[string]any)
			if len(buttons) != 2 {
				t.Fatalf("wrong buttons: %v", buttons)
			}
			for n, button := range buttons {
				id := button["custom_id"].(string)
				i = Interaction{}
				_ = json.Unmarshal([]byte(strings.Replace(pageClick, "requests:11111:10", id, 1)), &i)
				command, offset, err = Command(i)
				options, commandErr = CommandOptions(i)
				if command != "mes-demandes" || offset != n*20 || options.Status != want || err != nil || commandErr != nil {
					t.Fatalf("button lost filter: %s %+v %s %d %v %v", id, options, command, offset, err, commandErr)
				}
			}
		})
	}
}

func TestRequestFilterRejectsInvalidSignedInputs(t *testing.T) {
	inputs := []string{}
	for _, option := range []string{
		`{"name":"etat","type":3,"value":"Available"}`,
		`{"name":"etat","type":3,"value":""}`,
		`{"name":"etat","type":3,"value":null}`,
		`{"name":"etat","type":4,"value":"available"}`,
		`{"name":"etat","type":3,"value":"available"},{"name":"etat","type":3,"value":"pending"}`,
		`{"name":"etat","type":3,"value":"available"},{"name":"public","type":5,"value":true}`,
	} {
		inputs = append(inputs, `{"id":"99999","application_id":"12345","guild_id":"23456","type":2,"token":"private-interaction-token","member":{"user":{"id":"11111"}},"data":{"name":"mes-demandes","options":[`+option+`]}}`)
	}
	for _, id := range []string{"requests:22222:10:available", "requests:11111:10:Available", "requests:11111:10:", "requests:11111:10:all", "requests:11111:010:available", "requests:11111:11:available", "requests:11111:10:available:extra", "recent:11111:10:available"} {
		inputs = append(inputs, strings.Replace(pageClick, "requests:11111:10", id, 1))
	}
	filtered := strings.Replace(pageClick, "requests:11111:10", "requests:11111:10:available", 1)
	inputs = append(inputs, strings.Replace(filtered, `"flags":64`, `"flags":0`, 1))
	inputs = append(inputs, strings.Replace(filtered, `"component_type":2`, `"component_type":2,"options":[{"name":"public","type":5,"value":true}]`, 1))
	inputs = append(inputs, strings.Replace(strings.Replace(inputs[0], `"name":"mes-demandes"`, `"name":"nouveautes"`, 1), `"Available"`, `"available"`, 1))
	for _, body := range inputs {
		t.Run(body, func(t *testing.T) {
			b, r := signedInteraction(t, body)
			b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
				t.Error("invalid request reached remote service")
				return botResponse(500, `{}`), nil
			})
			w := httptest.NewRecorder()
			b.Handler().ServeHTTP(w, r)
			b.Wait()
			if w.Code != 403 {
				t.Fatalf("invalid request accepted: %d", w.Code)
			}
		})
	}
}

func TestRequestPaginationRequiresMatchingAPIFilter(t *testing.T) {
	for _, tc := range []struct {
		name, requested, returned string
		buttons                   int
	}{
		{"filtered", "available", "available", 2},
		{"missing filter", "available", "", 0},
		{"different filter", "available", "pending", 0},
		{"unexpected filter", "", "available", 0},
		{"invalid filter", "available", "bogus", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := pageClick
			if tc.requested != "" {
				body = strings.Replace(body, "requests:11111:10", "requests:11111:10:"+tc.requested, 1)
			}
			b, r := signedInteraction(t, body)
			edits := 0
			b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/callback"):
					return botResponse(204, ""), nil
				case r.URL.Host == "api.test":
					return botResponse(200, `{"content":"Demandes privées","pagination":{"offset":10,"total":21,"status":"`+tc.returned+`"}}`), nil
				default:
					edits++
					raw, _ := io.ReadAll(r.Body)
					var message struct {
						Flags      int `json:"flags"`
						Components []struct {
							Buttons []struct {
								ID string `json:"custom_id"`
							} `json:"components"`
						} `json:"components"`
					}
					if json.Unmarshal(raw, &message) != nil || message.Flags != 64 || r.Method != "PATCH" || !strings.Contains(string(raw), `"parse":[]`) {
						t.Fatalf("unsafe reply: %s", raw)
					}
					count := 0
					for _, row := range message.Components {
						for _, button := range row.Buttons {
							count++
							if !strings.HasSuffix(button.ID, ":"+tc.requested) {
								t.Errorf("filter changed: %s", button.ID)
							}
						}
					}
					if count != tc.buttons {
						t.Errorf("got %d buttons, want %d", count, tc.buttons)
					}
					return botResponse(200, `{}`), nil
				}
			})
			w := httptest.NewRecorder()
			b.Handler().ServeHTTP(w, r)
			b.Wait()
			if w.Code != 202 || edits != 1 {
				t.Fatalf("unexpected delivery: %d, edits %d", w.Code, edits)
			}
		})
	}
}
