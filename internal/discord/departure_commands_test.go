package discord

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDepartureCommandIsAlwaysDeliveredPrivately(t *testing.T) {
	b, r := signedCommand(t, "a-voir")
	stages := []string{}
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/callback"):
			stages = append(stages, "ack")
			var reply struct {
				Type int `json:"type"`
				Data struct {
					Flags int `json:"flags"`
				} `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&reply); err != nil || reply.Type != 5 || reply.Data.Flags != 64 {
				t.Fatal("departure command acknowledgement was not private")
			}
			return botResponse(204, ""), nil
		case r.URL.Host == "api.test":
			stages = append(stages, "api")
			return botResponse(200, `{"content":"À voir avant le départ","publicContent":"Must remain private"}`), nil
		default:
			stages = append(stages, "reply")
			var reply struct {
				Content  string `json:"content"`
				Flags    int    `json:"flags"`
				Mentions struct {
					Parse []string `json:"parse"`
				} `json:"allowed_mentions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&reply); err != nil || r.Method != "PATCH" || !strings.HasSuffix(r.URL.Path, "/messages/@original") || reply.Content != "À voir avant le départ" || reply.Flags != 64 || reply.Mentions.Parse == nil || len(reply.Mentions.Parse) != 0 {
				t.Fatalf("unsafe departure delivery: %s %s %+v", r.Method, r.URL.Path, reply)
			}
			return botResponse(200, `{}`), nil
		}
	})
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, r)
	b.Wait()
	if w.Code != 202 || strings.Join(stages, ",") != "ack,api,reply" {
		t.Fatalf("unexpected departure interactions: %d %v", w.Code, stages)
	}
}
