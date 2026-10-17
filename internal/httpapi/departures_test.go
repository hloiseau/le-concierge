package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/departures"
)

func TestPersonalDepartures(t *testing.T) {
	for _, mode := range []string{"success", "other user", "no history", "missing media", "excluded", "parent excluded", "child exclusion", "inactive rule", "missing rule", "changed library", "changed guid", "edition", "completed", "seasons unknown", "source error", "malformed rules", "wrong server", "denied media", "malformed exclusions", "logout", "unlinked", "history failure after success"} {
		t.Run(mode, func(t *testing.T) {
			s := fixture(t)
			s.Config.SeerrURL, s.Config.SeerrAPIKey = "", ""
			s.Config.MaintainerrURL = "http://maintainerr.test"
			s.Config.TautulliURL = "http://tautulli.test"
			s.Config.TautulliAPIKey = "private-key"
			cookie := seed(t, s, "11111", true)
			other := seed(t, s, "22222", true)
			calls, metadataReads, historyReads := 0, 0, 0
			kind, key := "movie", "42"
			if mode == "parent excluded" || mode == "child exclusion" || mode == "seasons unknown" {
				kind, key = "season", "44"
			}
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" {
					t.Errorf("mutation %s", r.Method)
				}
				body := ""
				switch r.URL.Path {
				case "/identity":
					body = `{"MediaContainer":{"machineIdentifier":"server-a"}}`
					if mode == "wrong server" {
						body = `{"MediaContainer":{"machineIdentifier":"server-b"}}`
					}
				case "/api/app/status":
					body = `{"status":1,"version":"3.30.1"}`
				case "/api/media-server/type":
					body = `{"type":"plex"}`
				case "/api/media-server":
					body = `{"machineId":"server-a"}`
				case "/api/collections/overlay-data":
					if mode == "source error" {
						return response(500, `private source error`), nil
					}
					body = fmt.Sprintf(`[{"id":1,"mediaServerType":"plex","libraryId":"2","type":%q,"isActive":true,"arrAction":0,"deleteAfterDays":7,"mediaCount":1,"media":[{"id":1,"collectionId":1,"mediaServerId":%q,"addDate":%q,"isManual":false,"ruleEvaluationFailed":false}]}]`, kind, key, time.Now().UTC().Format(time.RFC3339))
				case "/api/rules":
					body = fmt.Sprintf(`[{"id":5,"collectionId":1,"isActive":%v,"notifications":[{"token":"private-notification-secret"}]}]`, mode != "inactive rule")
					if mode == "missing rule" {
						body = `[]`
					}
					if mode == "malformed rules" {
						body = `[{"id":5,"collectionId":1}]`
					}
				case "/api/rules/exclusion":
					if r.URL.Query().Get("rulegroupId") != "5" {
						t.Error("missing exclusion scope")
					}
					body = `[]`
					if mode == "malformed exclusions" {
						body = `[{"id":1,"mediaServerId":null,"ruleGroupId":null}]`
					}
					if mode == "excluded" {
						body = `[{"id":1,"mediaServerId":"42","ruleGroupId":null,"type":"movie"}]`
					}
					if mode == "parent excluded" {
						body = `[{"id":1,"mediaServerId":"45","ruleGroupId":null,"type":"show"}]`
					}
					if mode == "child exclusion" {
						body = `[{"id":1,"mediaServerId":"43","parent":"44","ruleGroupId":5,"type":"episode"}]`
					}
				case "/api/v2":
					if r.Header.Get("X-Api-Key") != "private-key" {
						t.Error("wrong source key")
					}
					var data any
					switch r.URL.Query().Get("cmd") {
					case "get_tautulli_info":
						data = map[string]any{"tautulli_version": "v2.18.2"}
					case "get_server_info":
						data = map[string]any{"pms_identifier": "server-a"}
					case "get_users":
						data = []any{map[string]any{"user_id": 11111, "is_active": 1, "keep_history": 1}, map[string]any{"user_id": 22222, "is_active": 1, "keep_history": 1}}
					case "get_history":
						historyReads++
						if mode == "history failure after success" && historyReads > 1 {
							return response(500, `private-history`), nil
						}
						uid := r.URL.Query().Get("user_id")
						rows := []any{}
						if uid == "11111" && mode != "no history" {
							rows = append(rows, map[string]any{"row_id": 1, "user_id": 11111, "rating_key": 42, "parent_rating_key": 44, "grandparent_rating_key": 45, "media_type": "movie", "guid": "plex://movie/fixture", "percent_complete": 50, "watched_status": .5, "live": 0})
						}
						if kind == "season" && len(rows) > 0 {
							rows[0].(map[string]any)["media_type"] = "episode"
						}
						data = map[string]any{"recordsFiltered": len(rows), "data": rows}
					default:
						t.Errorf("unexpected Tautulli command %s", r.URL.Query().Get("cmd"))
					}
					b, _ := json.Marshal(map[string]any{"response": map[string]any{"result": "success", "data": data}})
					body = string(b)
				default:
					if !strings.HasPrefix(r.URL.Path, "/library/metadata/") {
						t.Errorf("unexpected route %s", r.URL.Path)
						return response(500, ``), nil
					}
					metadataReads++
					if r.Header.Get("X-Plex-Token") != "resource-11111" || r.URL.Query().Get("skipRefresh") != "1" {
						t.Error("metadata not restricted to actual user")
					}
					if mode == "missing media" {
						return response(404, ``), nil
					}
					if mode == "denied media" {
						return response(403, `private Plex detail`), nil
					}
					if mode == "logout" {
						call(s, "POST", "/api/v1/auth/logout", cookie, s.Config.PublicOrigin, "")
					}
					if mode == "unlinked" {
						if err := s.Store.Unlink(context.Background(), "11111"); err != nil {
							t.Fatal(err)
						}
					}
					library, guid, edition, views := "2", "plex://movie/fixture", "", 0
					if mode == "changed library" {
						library = "3"
					}
					if mode == "changed guid" {
						guid = "plex://movie/reimport"
					}
					if mode == "edition" {
						edition = "Director cut"
					}
					if mode == "completed" {
						views = 1
					}
					body = fmt.Sprintf(`{"MediaContainer":{"size":1,"librarySectionID":%q,"Metadata":[{"ratingKey":%q,"parentRatingKey":"45","title":"Private title","parentTitle":"Series","type":%q,"guid":%q,"editionTitle":%q,"viewCount":%d}]}}`, library, key, kind, guid, edition, views)
				}
				return response(200, body), nil
			})
			if w := call(s, "GET", "/api/v1/departures", "", "", ""); w.Code != 401 || calls != 0 {
				t.Fatal("anonymous upstream access")
			}
			if w := call(s, "GET", "/api/v1/departures?user_id=22222", cookie, "", ""); w.Code != 400 || calls != 0 {
				t.Fatal("client selected another identity")
			}
			if mode == "other user" {
				cookie = other
			}
			w := call(s, "GET", "/api/v1/departures", cookie, "", "")
			if mode == "logout" || mode == "unlinked" || mode == "changed library" || mode == "source error" || mode == "malformed rules" || mode == "wrong server" || mode == "denied media" || mode == "malformed exclusions" {
				if w.Code == 200 || strings.Contains(w.Body.String(), "Private title") {
					t.Fatalf("unsafe response %d %s", w.Code, w.Body)
				}
				if mode == "wrong server" && calls != 1 {
					t.Fatal("read personal sources for another server")
				}
				return
			}
			if w.Code != 200 {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			var page departures.Page
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if mode == "other user" || mode == "no history" || mode == "missing media" || mode == "excluded" || mode == "parent excluded" || mode == "inactive rule" || mode == "missing rule" {
				if len(page.Items) != 0 {
					t.Fatal("unexpected personal item")
				}
				if (mode == "other user" || mode == "no history") && metadataReads != 0 {
					t.Fatal("unrelated metadata read")
				}
				return
			}
			if len(page.Items) != 1 || page.Scope != "recorded_history" || !page.ExpiresAt.After(page.UpdatedAt) {
				t.Fatalf("invalid page %+v", page)
			}
			watch := "in_progress"
			if mode == "edition" || mode == "changed guid" || kind == "season" {
				watch = "unknown"
			}
			if mode == "completed" {
				watch = "completed"
			}
			if page.Items[0].Watch != watch {
				t.Fatalf("watch %s, want %s", page.Items[0].Watch, watch)
			}
			for _, private := range []string{"private-key", "private-notification", "resource-11111", "11111", "22222"} {
				if strings.Contains(w.Body.String(), private) {
					t.Fatal("private detail exposed")
				}
			}
			if mode == "history failure after success" {
				w = call(s, "GET", "/api/v1/departures", cookie, "", "")
				if w.Code != 503 || strings.Contains(w.Body.String(), "Private title") {
					t.Fatal("stale result survived source failure")
				}
			}
		})
	}
}
