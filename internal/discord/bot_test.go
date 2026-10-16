package discord

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func botResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func signedCommand(t *testing.T, name string) (*Bot, *http.Request) {
	t.Helper()
	return signedInteraction(t, fmt.Sprintf(`{"id":"99999","application_id":"12345","guild_id":"23456","type":2,"token":"private-interaction-token","member":{"user":{"id":"11111"}},"data":{"name":%q}}`, name))
}
func signedInteraction(t *testing.T, body string) (*Bot, *http.Request) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bot := NewBot(config.Config{DiscordClientID: "12345", DiscordGuildID: "23456", DiscordPublicKey: hex.EncodeToString(public), PublicOrigin: "https://hub.example", BotAPIURL: "http://api.test", BotAPISecret: strings.Repeat("s", 32)})
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	r := httptest.NewRequest("POST", "/interactions", strings.NewReader(body))
	r.Header.Set("X-Signature-Timestamp", stamp)
	r.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(private, []byte(stamp+body))))
	return bot, r
}
func TestHelpRepliesInlineWithoutUpstream(t *testing.T) {
	b, r := signedCommand(t, "aide")
	b.Config.Services = []config.Service{{ID: "plex", URL: "https://plex.example"}, {ID: "grimmory"}}
	b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("help must not need the API or a webhook")
		return botResponse(503, `{}`), nil
	})
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, r)
	b.Wait()
	var reply struct {
		Type int `json:"type"`
		Data struct {
			Flags   int    `json:"flags"`
			Content string `json:"content"`
		} `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &reply) != nil || w.Code != 200 || reply.Type != 4 || reply.Data.Flags != 64 || !strings.Contains(reply.Data.Content, "https://plex.example") || !strings.Contains(reply.Data.Content, "https://hub.example/aide") || !strings.Contains(reply.Data.Content, "https://hub.example/compte") {
		t.Fatalf("invalid help: %d %s", w.Code, w.Body)
	}
}

func TestHelpUsesOnlyConfiguredPublicServiceLinksWithinDiscordLimit(t *testing.T) {
	text := Help("https://hub.example", []config.Service{
		{ID: "plex", URL: "https://plex.example/path(with)"},
		{ID: "seerr", URL: "https://user:secret@seerr.example"},
		{ID: "downloadarr", URL: "javascript:alert(1)"},
		{ID: "shelfmark", URL: "https://shelfmark.example/" + strings.Repeat("x", 2000)},
		{ID: "unknown", Name: "@everyone", URL: "https://unknown.example"},
		{ID: "plex", URL: "https://duplicate.example"},
	})
	if len(text) > 2000 || !strings.Contains(text, "https://plex.example/path%28with%29") || !strings.Contains(text, "https://hub.example/aide") {
		t.Fatalf("invalid help links or bounds: %s", text)
	}
	for _, excluded := range []string{"secret", "javascript", "shelfmark.example", "unknown.example", "@everyone", "duplicate.example"} {
		if strings.Contains(text, excluded) {
			t.Fatalf("unsafe help contained %q", excluded)
		}
	}
	if len(Help("https://"+strings.Repeat("x", 3000)+".example", nil)) > 2000 {
		t.Fatal("long configured origin exceeded Discord limit")
	}
}
func TestDeferredReplyWaitsForAcknowledgement(t *testing.T) {
	for _, failAck := range []bool{false, true} {
		t.Run(strconv.FormatBool(failAck), func(t *testing.T) {
			b, r := signedCommand(t, "mes-demandes")
			var stages []string
			b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/callback"):
					stages = append(stages, "ack")
					raw, _ := io.ReadAll(r.Body)
					if r.Method != "POST" || string(raw) != `{"type":5,"data":{"flags":64}}` {
						t.Error("invalid deferred private callback")
					}
					deadline, ok := r.Context().Deadline()
					if !ok || time.Until(deadline) > 2500*time.Millisecond {
						t.Error("ack has no short deadline")
					}
					if failAck {
						return botResponse(404, `{"code":10062}`), nil
					}
					return botResponse(204, ""), nil
				case r.URL.Host == "api.test":
					stages = append(stages, "api")
					raw, _ := io.ReadAll(r.Body)
					if _, err := Verify(b.Config.DiscordPublicKey, r.Header.Get("X-Signature-Timestamp"), r.Header.Get("X-Signature-Ed25519"), raw, time.Now()); err != nil {
						t.Error("original signature lost")
					}
					return botResponse(200, `{"content":"Vos demandes"}`), nil
				default:
					stages = append(stages, "reply")
					raw, _ := io.ReadAll(r.Body)
					if r.Method != "PATCH" || !strings.HasSuffix(r.URL.Path, "/messages/@original") || !strings.Contains(string(raw), `"flags":64`) || !strings.Contains(string(raw), `"parse":[]`) {
						t.Error("invalid private reply")
					}
					return botResponse(200, `{}`), nil
				}
			})
			w := httptest.NewRecorder()
			b.Handler().ServeHTTP(w, r)
			b.Wait()
			expected := "ack,api,reply"
			if failAck {
				expected = "ack"
			}
			if w.Code != 202 || w.Body.Len() != 0 || strings.Join(stages, ",") != expected {
				t.Fatalf("unsafe callback order: %d %s %v", w.Code, w.Body, stages)
			}
		})
	}
}
func TestWebhook404IsLoggedWithoutSecretsAndNotRetried(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(old)
	b, r := signedCommand(t, "nouveautes")
	patches := 0
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/callback") {
			return botResponse(204, ""), nil
		}
		if r.URL.Host == "api.test" {
			return botResponse(503, `{}`), nil
		}
		patches++
		return botResponse(404, `{"code":10015,"message":"private-interaction-token"}`), nil
	})
	b.Handler().ServeHTTP(httptest.NewRecorder(), r)
	b.Wait()
	if patches != 1 || !strings.Contains(logs.String(), `"discord_code":10015`) || strings.Contains(logs.String(), "private-interaction-token") {
		t.Fatalf("unsafe webhook error handling: %d %s", patches, &logs)
	}
}
