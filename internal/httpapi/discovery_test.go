package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/discord"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func TestDiscoveryMessagesAreBoundedAndDoNotMentionAnyone(t *testing.T) {
	paris, _ := time.LoadLocation("Europe/Paris")
	end := time.Date(2026, 10, 16, 0, 0, 0, 0, paris)
	recap := upstream.RecapResult{Start: end.AddDate(0, 0, -7), End: end}
	for index := range 5000 {
		recap.Items = append(recap.Items, upstream.Media{ID: strconv.Itoa(index), Title: strings.Repeat("📽", 200) + "@everyone <@12345> [link](https://malicious.test)", Type: "movie", URL: "https://app.plex.tv/desktop/#!/server/server-a/details?key=%2Flibrary%2Fmetadata%2F1"})
	}
	text := recapMessage(recap, true)
	if len(text) > 2000 || !strings.Contains(text, "09/10/2026 au 15/10/2026 inclus · Europe/Paris") || !strings.Contains(text, "autre(s) titre(s)") || strings.Contains(text, "@everyone") || strings.Contains(text, "<@") {
		t.Fatalf("bad recap rendering (%d bytes): %s", len(text), text)
	}
	text = discoveryLine(upstream.Media{Title: "@everyone <@12345> **Title**", Type: "show", URL: "https://app.plex.tv/desktop/#!/server/server-a/details?key=%2Flibrary%2Fmetadata%2F1"}, true)
	if strings.Contains(text, "@everyone") || strings.Contains(text, "<@") || strings.Contains(text, "**Title**") || !strings.Contains(text, "Série") {
		t.Fatalf("unsafe title %s", text)
	}
	reply := discord.Message(text)
	if reply["flags"] != 64 {
		t.Fatal("catalogue response is not private")
	}
}

func TestDiscoveryMessageHandlesEmptyIncompleteAndDenied(t *testing.T) {
	for _, command := range []string{"surprise", "recap"} {
		for _, state := range []string{"empty", "incomplete", "denied", "outage"} {
			t.Run(command+"/"+state, func(t *testing.T) {
				s := fixture(t)
				s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
					if r.Method != "GET" || r.URL.Host != "plex.test" {
						t.Fatal("command sent a message or contacted another service")
					}
					if r.URL.Path == "/identity" {
						return response(200, `{"MediaContainer":{"machineIdentifier":"server-a"}}`), nil
					}
					switch state {
					case "incomplete":
						return response(200, `{"MediaContainer":{"size":0,"totalSize":8000}}`), nil
					case "denied":
						return response(403, `{}`), nil
					case "outage":
						return response(503, `{}`), nil
					default:
						return response(200, `{"MediaContainer":{"size":0,"totalSize":0}}`), nil
					}
				})
				text, publicText := s.discoveryMessage(context.Background(), upstream.Grant{ServerToken: "private-token"}, command, "", time.Now())
				if state != "empty" && publicText != "" {
					t.Fatal("source failure produced a public message")
				}
				want := map[string]string{"empty": "Aucun", "incomplete": "catalogue complet", "denied": "accès Plex", "outage": "indisponible"}[state]
				if !strings.Contains(text, want) || strings.Contains(text, "private-token") || strings.Contains(text, "plex.test") {
					t.Fatalf("bad failure/empty message: %s", text)
				}
			})
		}
	}
}

func TestDiscoverySignedCommandsRecheckIdentity(t *testing.T) {
	for _, command := range []string{"surprise", "recap"} {
		for _, state := range []string{"linked", "unlinked", "revoked", "deleted during read"} {
			t.Run(command+"/"+state, func(t *testing.T) {
				s := fixture(t)
				if state != "unlinked" {
					seed(t, s, "11111", true)
				}
				calls := 0
				s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Method != "GET" || r.URL.Host != "plex.test" {
						t.Fatalf("discovery contacted a messaging service: %s %s", r.Method, r.URL.Host)
					}
					switch r.URL.Path {
					case "/identity":
						return response(200, `{"MediaContainer":{"machineIdentifier":"server-a"}}`), nil
					case "/library/all":
						if r.Header.Get("X-Plex-Token") != "resource-11111" {
							t.Error("wrong caller credential")
						}
						if state == "deleted during read" {
							if err := s.Store.Delete(context.Background(), "11111"); err != nil {
								t.Fatal(err)
							}
						}
						return response(200, fmt.Sprintf(`{"MediaContainer":{"size":1,"totalSize":1,"Metadata":[{"ratingKey":"1","title":"Private title","type":"movie","addedAt":%d}]}}`, time.Now().Add(-48*time.Hour).Unix())), nil
					case "/library/metadata/1":
						if state == "revoked" {
							return response(403, `{}`), nil
						}
						if command == "surprise" && (r.URL.Query().Get("checkFiles") != "1" || r.URL.Query().Get("checkFileAvailability") != "1") {
							t.Fatal("surprise omitted fresh playback checks")
						}
						return response(200, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"1","type":"movie","Media":[{"Part":[{"key":"/library/parts/1/file.mkv","exists":true,"accessible":true}]}]}]}}`), nil
					default:
						t.Fatalf("unexpected discovery request: %s", r.URL.Path)
						return nil, nil
					}
				})
				public, private, _ := ed25519.GenerateKey(rand.Reader)
				s.Config.DiscordPublicKey = hex.EncodeToString(public)
				s.Config.BotAPISecret = strings.Repeat("s", 32)
				body := fmt.Sprintf(`{"id":"99999","application_id":"12345","guild_id":"23456","type":2,"member":{"user":{"id":"11111"}},"data":{"name":%q}}`, command)
				stamp := strconv.FormatInt(time.Now().Unix(), 10)
				call := func(payload string) *httptest.ResponseRecorder {
					r := httptest.NewRequest("POST", "/internal/discord", strings.NewReader(payload))
					r.Header.Set("Authorization", "Bearer "+s.Config.BotAPISecret)
					r.Header.Set("X-Signature-Timestamp", stamp)
					r.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(private, []byte(stamp+body))))
					w := httptest.NewRecorder()
					s.Handler().ServeHTTP(w, r)
					return w
				}
				if w := call(body + " "); w.Code != 403 || calls != 0 {
					t.Fatal("unsigned payload reached catalogue")
				}
				w := call(body)
				var reply struct {
					Content string `json:"content"`
					Flags   int    `json:"flags"`
				}
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Flags != 64 || !strings.Contains(w.Body.String(), `"parse":[]`) {
					t.Fatalf("unsafe command reply: %d %s", w.Code, w.Body)
				}
				if strings.Contains(reply.Content, "Private title") != (state == "linked") {
					t.Fatalf("access boundary broken: %s", reply.Content)
				}
				if state == "unlinked" && calls != 0 {
					t.Fatal("unlinked account reached Plex")
				}
				if w := call(body); w.Code != 409 {
					t.Fatal("signed command replay accepted")
				}
			})
		}
	}
}
