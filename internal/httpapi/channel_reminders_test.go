package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hloiseau/le-concierge/internal/notifications"
	"github.com/hloiseau/le-concierge/internal/upstream"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestChannelRemindersOnlyMentionVerifiedUnwatchedRequesters(t *testing.T) {
	for _, mode := range []string{"seerr identity", "linked identity", "no Discord identity", "excluded", "inactive", "declined", "wrong key", "wrong quality", "deleted first requester", "temporary requester error", "wrong Seerr server", "changed source", "quiet hours", "quiet boundary source", "quiet boundary channel", "season", "season long title", "episode", "episode long title", "episode missing index", "episode zero index", "episode other season", "late exclusion", "late inactive rule", "late postponed", "late removed", "late non-destructive action", "late source error", "late request cancelled", "late recipient changed", "late local identity removed", "late title changed", "completed", "history completed", "unknown watch", "no Plex session", "expired Plex session", "late completed", "late logout", "two requesters", "metadata completed"} {
		t.Run(mode, func(t *testing.T) {
			s := fixture(t)
			s.Config.DiscordNotificationChannelID = "456"
			s.Config.DiscordGuildID = "123"
			s.Config.MaintainerrURL = "https://maintainerr.example"
			s.Config.SeerrURL = "https://seerr.example"
			s.Config.SeerrAPIKey = "fixture-key"
			s.Config.PlexServerID = "server-a"
			s.Config.TautulliURL, s.Config.TautulliAPIKey = "https://tautulli.example", "fixture-key"
			user, err := s.Store.LoginPlex(context.Background(), "777", "Fixture")
			if err != nil {
				t.Fatal(err)
			}
			if mode != "no Plex session" {
				expires := time.Now().Add(time.Hour)
				if mode == "expired Plex session" {
					expires = time.Now().Add(-time.Second)
				}
				s.grants[user.ID] = upstream.Grant{PlexID: "777", AccountToken: "personal-account", ServerToken: "personal-server", VerifiedAt: time.Now(), ExpiresAt: expires}
			}
			if mode == "two requesters" {
				second, err := s.Store.LoginPlex(context.Background(), "888", "Second")
				if err != nil {
					t.Fatal(err)
				}
				s.grants[second.ID] = upstream.Grant{PlexID: "888", AccountToken: "second-account", ServerToken: "second-server", VerifiedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
			}
			kind, requestedKind, requestedKey, title := "movie", "movie", "42", "Film demandé"
			if strings.HasPrefix(mode, "season") || strings.HasPrefix(mode, "episode") {
				s.Config.TautulliURL = "https://tautulli.example"
				s.Config.TautulliAPIKey = "fixture-key"
				kind, requestedKind, requestedKey, title = "season", "tv", "38", "Série demandée — saison 3"
				if strings.HasSuffix(mode, "long title") {
					title = " — saison 3"
				}
				if strings.HasPrefix(mode, "episode") {
					kind = "episode"
					title += ", épisode 2"
					if mode == "episode missing index" {
						title = "Série demandée — saison 3, un épisode (numéro indisponible)"
					} else if mode == "episode zero index" {
						title = "Série demandée — saison 3, épisode 0"
					}
				}
			}
			now := time.Now().UTC()
			loc, _ := time.LoadLocation("Europe/Paris")
			local := now.In(loc)
			now = time.Date(local.Year(), local.Month(), local.Day(), 12, 0, 0, 0, loc).UTC()
			if mode == "quiet hours" {
				now = now.Add(-8 * time.Hour)
			} else if strings.HasPrefix(mode, "quiet boundary") {
				now = time.Date(local.Year(), local.Month(), local.Day(), 22, 0, 0, 0, loc).Add(-10 * time.Millisecond).UTC()
			}
			waitForQuietHours := func(r *http.Request) (*http.Response, error) {
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 10*time.Millisecond {
					t.Fatal("request deadline extends beyond quiet hours")
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			}
			added := now.Add(-6 * 24 * time.Hour)
			posts := 0
			reads := 0
			changed := false
			changeDuringLookup := true
			if mode == "linked identity" || mode == "late local identity removed" {
				u, err := s.Store.LoginPlex(context.Background(), "777", "Fixture")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.Store.LinkDiscord(context.Background(), u.ID, "777", "789"); err != nil {
					t.Fatal(err)
				}
			}
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" {
					t.Fatal("mutated upstream")
				}
				body := ""
				switch r.URL.Path {
				case "/identity":
					body = `{"MediaContainer":{"machineIdentifier":"server-a"}}`
				case "/library/all":
					if (r.Header.Get("X-Plex-Token") != "personal-server" && r.Header.Get("X-Plex-Token") != "second-server") || r.URL.Query().Get("unwatched") != "1" {
						t.Fatal("unwatched not checked for the requester")
					}
					if (mode == "two requesters" && r.Header.Get("X-Plex-Token") == "personal-server") || mode == "unknown watch" || mode == "completed" || (changed && mode == "late completed") {
						body = `{"MediaContainer":{"size":0,"totalSize":0,"Metadata":[]}}`
						break
					}
					rowKind, rowID, parent := "movie", "42", "40"
					if kind != "movie" {
						rowKind = "episode"
					}
					if kind == "season" {
						rowID, parent = "43", "42"
					}
					body = fmt.Sprintf(`{"MediaContainer":{"size":1,"totalSize":1,"Metadata":[{"ratingKey":%q,"type":%q,"title":"Fixture","guid":"plex://fixture","addedAt":1,"parentRatingKey":%q,"grandparentRatingKey":"38"}]}}`, rowID, rowKind, parent)
				case "/library/metadata/42", "/library/metadata/43", "/library/metadata/40":
					rowKind, parent, id, index := "movie", "40", "42", 2
					if kind != "movie" {
						rowKind = "episode"
					}
					if kind == "season" {
						parent, id = "42", "43"
					}
					if (kind == "season" && r.URL.Path == "/library/metadata/42") || r.URL.Path == "/library/metadata/40" {
						rowKind, parent, id, index = "season", "38", strings.TrimPrefix(r.URL.Path, "/library/metadata/"), 3
					}
					body = fmt.Sprintf(`{"MediaContainer":{"size":1,"librarySectionID":"2","Metadata":[{"ratingKey":%q,"type":%q,"title":"Fixture","guid":"plex://fixture","parentRatingKey":%q,"grandparentRatingKey":"38","index":%d,"Guid":[{"id":"tmdb://99"}]}]}}`, id, rowKind, parent, index)
					if mode == "metadata completed" {
						body = strings.Replace(body, `"guid":"plex://fixture"`, `"guid":"plex://fixture","viewCount":1`, 1)
					}
				case "/library/metadata/38":
					body = `{"MediaContainer":{"size":1,"librarySectionID":"2","Metadata":[{"ratingKey":"38","type":"show","title":"Fixture","guid":"plex://show","Guid":[{"id":"tmdb://99"}]}]}}`
				case "/api/v1/settings/plex":
					if mode == "quiet boundary source" {
						return waitForQuietHours(r)
					}
					body = `{"machineId":"server-a"}`
					if mode == "wrong Seerr server" {
						body = `{"machineId":"server-b"}`
					}
				case "/api/app/status":
					body = `{"status":1,"version":"3.30.1"}`
				case "/api/media-server/type":
					body = `{"type":"plex"}`
				case "/api/media-server":
					body = `{"machineId":"server-a"}`
				case "/api/collections/overlay-data":
					reads++
					if mode == "changed source" || (changed && mode == "late source error") {
						return response(503, "private error"), nil
					}
					if changed && mode == "late removed" {
						body = `[]`
						break
					}
					body = fmt.Sprintf(`[{"id":1,"mediaServerType":"plex","libraryId":"2","type":%q,"isActive":true,"arrAction":0,"deleteAfterDays":7,"mediaCount":1,"media":[{"id":1,"collectionId":1,"mediaServerId":"42","addDate":%q,"isManual":false,"ruleEvaluationFailed":false}]}]`, kind, added.Format(time.RFC3339))
					if changed && mode == "late postponed" {
						body = strings.Replace(body, `"deleteAfterDays":7`, `"deleteAfterDays":8`, 1)
					} else if changed && mode == "late non-destructive action" {
						body = strings.Replace(body, `"arrAction":0`, `"arrAction":3`, 1)
					}
				case "/api/rules":
					body = fmt.Sprintf(`[{"id":5,"collectionId":1,"isActive":%v}]`, mode != "inactive" && !(changed && mode == "late inactive rule"))
				case "/api/rules/exclusion":
					body = `[]`
					if mode == "excluded" || (changed && mode == "late exclusion") {
						body = `[{"id":1,"mediaServerId":"42","ruleGroupId":5,"type":"movie"}]`
					}
				case "/api/v1/request":
					if mode == "deleted first requester" || mode == "temporary requester error" || mode == "two requesters" {
						body = `{"pageInfo":{"results":2},"results":[{"id":2,"type":"movie","status":2,"requestedBy":{"id":8},"media":{"tmdbId":99,"ratingKey":"42"}},{"id":1,"type":"movie","status":2,"requestedBy":{"id":7},"media":{"tmdbId":99,"ratingKey":"42"}}]}`
						break
					}
					status := 2
					key := requestedKey
					if mode == "declined" || (changed && mode == "late request cancelled") {
						status = 3
					}
					if mode == "wrong key" {
						key = "999"
					}
					season := 3
					if mode == "episode other season" {
						season = 4
					}
					body = fmt.Sprintf(`{"pageInfo":{"results":1},"results":[{"id":1,"type":%q,"status":%d,"is4k":%v,"requestedBy":{"id":7},"media":{"tmdbId":99,"ratingKey":%q,"ratingKey4k":"43"},"seasons":[{"seasonNumber":%d}]}]}`, requestedKind, status, mode == "wrong quality", key, season)
				case "/api/v1/user/8/settings/notifications":
					body = `{"discordIds":["999"]}`
				case "/api/v1/user/8":
					if mode == "two requesters" {
						return response(200, `{"id":8,"plexId":888}`), nil
					}
					if mode == "temporary requester error" {
						return response(503, "unavailable"), nil
					}
					return response(404, "not found"), nil
				case "/api/v1/user/7":
					body = `{"id":7,"plexId":777,"displayName":"@everyone"}`
				case "/api/v1/user/7/settings/notifications":
					body = `{"discordIds":["789"]}`
					if mode == "no Discord identity" || mode == "linked identity" || mode == "late local identity removed" {
						body = `{"discordIds":[]}`
					} else if changed && mode == "late recipient changed" {
						body = `{"discordIds":["790"]}`
					}
				case "/api/v1/movie/99":
					body = `{"title":"Film demandé @everyone"}`
					if changed && mode == "late title changed" {
						body = `{"title":"Titre actuel"}`
					}
				case "/api/v1/tv/99":
					body = `{"name":"Série demandée"}`
					if strings.HasSuffix(mode, "long title") {
						body = fmt.Sprintf(`{"name":%q}`, strings.Repeat("é", 240))
					}
				case "/api/v2":
					data := ""
					switch r.URL.Query().Get("cmd") {
					case "get_tautulli_info":
						data = `{"tautulli_version":"v2.18.2"}`
					case "get_server_info":
						data = `{"pms_identifier":"server-a"}`
					case "get_users":
						data = `[{"user_id":777,"is_active":1,"keep_history":1},{"user_id":888,"is_active":1,"keep_history":1}]`
					case "get_history":
						data = `{"recordsFiltered":0,"data":[]}`
						if mode == "history completed" {
							data = `{"recordsFiltered":1,"data":[{"row_id":1,"user_id":777,"rating_key":42,"media_type":"movie","guid":"plex://fixture","percent_complete":100,"watched_status":1,"live":0}]}`
						}
					case "get_metadata":
						index, parent := "3", "38"
						if kind == "episode" {
							index, parent = "2", "40"
						}
						if mode == "episode missing index" {
							index = ""
						} else if mode == "episode zero index" {
							index = "0"
						}
						data = fmt.Sprintf(`{"rating_key":"42","media_type":%q,"section_id":"2","parent_rating_key":%q,"grandparent_rating_key":"38","media_index":%q,"parent_media_index":"3"}`, kind, parent, index)
					default:
						t.Fatal("unexpected Tautulli query")
					}
					body = `{"response":{"result":"success","data":` + data + `}}`
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					return response(404, ""), nil
				}
				return response(200, body), nil
			})
			s.Discord = notifications.NewDiscord("fixture-token")
			s.Discord.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == "GET" {
					if mode == "quiet boundary channel" {
						return waitForQuietHours(r)
					}
					changed = changeDuringLookup
					if mode == "late logout" {
						delete(s.grants, user.ID)
					}
					if mode == "late local identity removed" {
						if _, err := s.Store.DB.Exec(`UPDATE users SET discord_id=NULL WHERE plex_id='777'`); err != nil {
							t.Fatal(err)
						}
					}
					return response(200, `{"id":"456","type":0,"guild_id":"123"}`), nil
				}
				posts++
				var body struct {
					Content  string `json:"content"`
					Mentions struct {
						Users []string `json:"users"`
						Parse []string `json:"parse"`
					} `json:"allowed_mentions"`
				}
				json.NewDecoder(r.Body).Decode(&body)
				expectedRecipient := "789"
				if mode == "two requesters" {
					expectedRecipient = "999"
				}
				if len(body.Mentions.Users) != 1 || body.Mentions.Users[0] != expectedRecipient || len(body.Mentions.Parse) != 0 || strings.Contains(body.Content, "@everyone") || !strings.Contains(body.Content, title) {
					t.Fatalf("incorrect channel reminder: %+v", body)
				}
				if strings.HasSuffix(mode, "long title") && !strings.Contains(body.Content, strings.Repeat("é", 200)) {
					t.Fatal("long title was lost", body.Content)
				}
				if strings.HasPrefix(mode, "season") && strings.Contains(body.Content, "épisode") {
					t.Fatal("season reminder narrowed to an episode", body.Content)
				}
				return response(200, `{"id":"111","channel_id":"456"}`), nil
			})
			cycles := 2
			if strings.HasPrefix(mode, "late ") || strings.HasPrefix(mode, "quiet boundary") {
				cycles = 1
			}
			for i := 0; i < cycles; i++ {
				err := s.deliverChannelReminders(context.Background(), now)
				if (strings.HasPrefix(mode, "late ") || strings.HasPrefix(mode, "quiet boundary")) && err == nil {
					t.Fatal("late change was not revalidated")
				}
				if mode == "changed source" || mode == "wrong Seerr server" || mode == "temporary requester error" || (mode == "deleted first requester" && i == 0) {
					if err == nil {
						t.Fatal("source failure ignored")
					}
				} else if err != nil && !strings.HasPrefix(mode, "late ") && !strings.HasPrefix(mode, "quiet boundary") {
					t.Fatal(err)
				}
			}
			want := 0
			if mode == "two requesters" || mode == "seerr identity" || mode == "linked identity" || mode == "deleted first requester" || strings.HasPrefix(mode, "season") || mode == "episode" || mode == "episode long title" || mode == "episode missing index" || mode == "episode zero index" {
				want = 1
			}
			if posts != want {
				t.Fatalf("posts=%d want=%d (grants=%d)", posts, want, len(s.grants))
			}
			if mode == "late source error" {
				var attempts int
				var next int64
				var code string
				if err := s.Store.DB.QueryRow(`SELECT attempts,next_attempt,last_error FROM channel_reminders`).Scan(&attempts, &next, &code); err != nil || attempts != 1 || code != "source_unavailable" || next <= time.Now().UnixMilli() {
					t.Fatal("transient validation failure was not scheduled for retry", attempts, next, code, err)
				}
				changed, changeDuringLookup = false, false
				// Align the persisted retry with this fixture's midday clock.
				if _, err := s.Store.DB.Exec(`UPDATE channel_reminders SET next_attempt=?`, now.Add(time.Minute).UnixMilli()); err != nil {
					t.Fatal(err)
				}
				if err := s.deliverChannelReminders(context.Background(), now.Add(2*time.Minute)); err != nil || posts != 1 {
					t.Fatal("recovered source did not deliver once", posts, err)
				}
			}
			if mode == "quiet hours" && reads != 0 {
				t.Fatal("sources queried during quiet hours")
			}
		})
	}
}
