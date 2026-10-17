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

const pageClick = `{"id":"99999","application_id":"12345","guild_id":"23456","type":3,"token":"private-interaction-token","member":{"user":{"id":"11111"}},"message":{"flags":64},"data":{"custom_id":"requests:11111:10","component_type":2}}`

func TestPaginationRejectsInvalidClicks(t *testing.T) {
	for _, tc := range []struct{ old, replacement string }{
		{"requests:11111:10", "requests:22222:10"},
		{"requests:11111:10", "requests:11111:-10"},
		{"requests:11111:10", "requests:11111:10010"},
		{"requests:11111:10", "requests:11111:11"},
		{"requests:11111:10", "requests:11111:010"},
		{"requests:11111:10", "requests:11111:+10"},
		{"requests:11111:10", "requests:11111:999999999999999999999999"},
		{"requests:11111:10", "nouveautes:11111:10"},
		{"requests:11111:10", "requests:11111:10:extra"},
		{"requests:11111:10", ""},
		{`"flags":64`, `"flags":0`},
		{`"component_type":2`, `"component_type":3`},
		{`"guild_id":"23456"`, `"guild_id":"34567"`},
	} {
		t.Run(tc.replacement, func(t *testing.T) {
			b, r := signedInteraction(t, strings.Replace(pageClick, tc.old, tc.replacement, 1))
			b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
				t.Error("invalid click reached upstream")
				return botResponse(500, `{}`), nil
			})
			w := httptest.NewRecorder()
			b.Handler().ServeHTTP(w, r)
			b.Wait()
			if w.Code != 403 {
				t.Fatalf("got %d: %s", w.Code, w.Body)
			}
		})
	}
}

func TestPaginationEditsPrivateMessageAndClearsOldButtonsOnError(t *testing.T) {
	for _, tc := range []struct {
		name, apiBody   string
		status, buttons int
	}{
		{"middle", `{"content":"Page 2","pagination":{"offset":10,"total":21}}`, 200, 2},
		{"last", `{"content":"Page 2","pagination":{"offset":10,"total":11}}`, 200, 1},
		{"empty after deletion", `{"content":"Page vide","pagination":{"offset":10,"total":0}}`, 200, 1},
		{"expired access", `{"content":"Reconnectez-vous"}`, 200, 0},
		{"API outage", `{}`, 503, 0},
		{"invalid metadata", `{"content":"Page 2","pagination":{"offset":-10,"total":21}}`, 200, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, r := signedInteraction(t, pageClick)
			var stages []string
			b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				raw, _ := io.ReadAll(r.Body)
				switch {
				case strings.HasSuffix(r.URL.Path, "/callback"):
					stages = append(stages, "ack")
					if string(raw) != `{"type":6}` {
						t.Errorf("not an update acknowledgement: %s", raw)
					}
					return botResponse(204, ""), nil
				case r.URL.Host == "api.test":
					stages = append(stages, "api")
					if string(raw) != pageClick {
						t.Error("signed component body changed")
					}
					return botResponse(tc.status, tc.apiBody), nil
				default:
					stages = append(stages, "edit")
					var reply struct {
						Flags      int `json:"flags"`
						Components []struct {
							Type    int `json:"type"`
							Buttons []struct {
								ID          string `json:"custom_id"`
								Type, Style int
							} `json:"components"`
						} `json:"components"`
					}
					if json.Unmarshal(raw, &reply) != nil || reply.Flags != 64 || reply.Components == nil || !strings.Contains(string(raw), `"parse":[]`) || r.Method != "PATCH" || !strings.HasSuffix(r.URL.Path, "/messages/@original") {
						t.Errorf("unsafe edit: %s", raw)
					}
					count := 0
					for _, row := range reply.Components {
						if row.Type != 1 {
							t.Error("not an action row")
						}
						for n, button := range row.Buttons {
							if button.ID != fmt.Sprintf("requests:11111:%d", n*20) || button.Type != 2 || button.Style != 2 {
								t.Errorf("unexpected button: %+v", button)
							}
							count++
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
			if w.Code != 202 || strings.Join(stages, ",") != "ack,api,edit" {
				t.Fatalf("invalid update sequence: %d %v", w.Code, stages)
			}
		})
	}
}

func TestRequestButtonsAtBoundaries(t *testing.T) {
	for _, tc := range []struct{ offset, total, buttons int }{
		{0, 0, 0}, {0, 10, 0}, {0, 11, 1}, {10, 20, 1}, {10, 21, 2}, {10000, 10020, 1},
	} {
		rows := (RequestPagination{Offset: tc.offset, Total: tc.total}).Components("11111")
		count := 0
		if len(rows) > 0 {
			count = len(rows[0]["components"].([]map[string]any))
		}
		if count != tc.buttons {
			t.Errorf("%+v: got %d buttons", tc, count)
		}
	}
}
