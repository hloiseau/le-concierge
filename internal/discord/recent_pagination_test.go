package discord

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecentButtonsUseSignedOwnerAndClearOnFailure(t *testing.T) {
	for _, tc := range []struct {
		body    string
		buttons int
	}{
		{`{"content":"Nouveautés","recentPagination":{"offset":6,"nextOffset":12}}`, 2},
		{`{"content":"Nouveautés","recentPagination":{"offset":6,"nextOffset":null}}`, 1},
		{`{"content":"Indisponible"}`, 0},
		{`{"content":"Nouveautés","recentPagination":{"offset":12,"nextOffset":18}}`, 0},
	} {
		b, request := signedInteraction(t, strings.Replace(pageClick, "requests:11111:10", "recent:11111:6", 1))
		patches := 0
		b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(r.Body)
			switch {
			case strings.HasSuffix(r.URL.Path, "/callback"):
				if string(body) != `{"type":6}` {
					t.Fatal("wrong update acknowledgement")
				}
				return botResponse(204, ""), nil
			case r.URL.Path == "/internal/discord":
				if !strings.Contains(string(body), "recent:11111:6") || r.Header.Get("X-Signature-Ed25519") == "" {
					t.Fatal("original signature/body lost")
				}
				return botResponse(200, tc.body), nil
			default:
				patches++
				var message struct {
					Flags      int `json:"flags"`
					Components []struct {
						Components []struct {
							CustomID string `json:"custom_id"`
						} `json:"components"`
					} `json:"components"`
				}
				if json.Unmarshal(body, &message) != nil || message.Flags != 64 {
					t.Fatal("not a private reply")
				}
				buttons := 0
				for _, row := range message.Components {
					for _, button := range row.Components {
						buttons++
						if !strings.HasPrefix(button.CustomID, "recent:11111:") {
							t.Fatal("wrong button owner")
						}
					}
				}
				if buttons != tc.buttons {
					t.Fatalf("got %d buttons, want %d", buttons, tc.buttons)
				}
				return botResponse(200, `{}`), nil
			}
		})
		w := httptest.NewRecorder()
		b.Handler().ServeHTTP(w, request)
		b.Wait()
		if w.Code != 202 || patches != 1 {
			t.Fatalf("delivery %d (%d patches)", w.Code, patches)
		}
	}
	for _, bad := range []string{"recent:22222:6", "recent:11111:-1", "recent:11111:10001", "recent:11111:06"} {
		var i Interaction
		_ = json.Unmarshal([]byte(strings.Replace(pageClick, "requests:11111:10", bad, 1)), &i)
		if _, _, err := Command(i); err == nil {
			t.Fatalf("invalid click %s accepted", bad)
		}
	}
}
