package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/notifications"
	"github.com/hloiseau/le-concierge/internal/store"
)

type availableRequestSource struct {
	mode                 string
	addedAt, requestedAt time.Time
	beforeMetadata       func()
	firstStarted         bool
}

// A confirmed request, positive personal unwatched filter, real source addition
// and playable file. All upstream access is intercepted; no live sends occur.
func availableRequestFixture(t *testing.T) (*Server, string, *availableRequestSource) {
	t.Helper()
	s, cookie, _ := reminderFixture(t)
	s.Config.SeerrURL, s.Config.SeerrAPIKey = "http://seerr.test", "fixture-seerr-key"
	f := &availableRequestSource{addedAt: time.Now().UTC().Truncate(time.Second).Add(-40 * 24 * time.Hour), requestedAt: time.Now().UTC().Truncate(time.Second).Add(-55 * 24 * time.Hour)}
	p, _ := s.Store.GetReminderPreferences(context.Background(), "11111")
	p.AvailableUnwatched = true
	if err := s.Store.SaveReminderPreferences(context.Background(), "11111", p); err != nil {
		t.Fatal(err)
	}
	original := s.Upstream.HTTP.Transport
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		kind := "movie"
		if f.mode == "tv" || f.mode == "wrong_season" || f.mode == "parent_removal" {
			kind = "tv"
		}
		switch r.URL.Path {
		case "/api/v1/user":
			return response(200, `{"pageInfo":{"results":1},"results":[{"id":9,"plexId":11111}]}`), nil
		case "/api/v1/user/9/requests":
			if f.mode == "request_deleted" {
				return response(200, `{"pageInfo":{"results":0},"results":[]}`), nil
			}
			owner, status := 9, 5
			if f.mode == "other_owner" {
				owner = 10
			}
			if f.mode == "declined" {
				status = 3
			}
			created := any(f.requestedAt.Format(time.RFC3339))
			if f.mode == "missing_request_date" {
				created = nil
			}
			ratingKey := "84"
			if kind == "tv" {
				ratingKey = "79"
			}
			b, _ := json.Marshal(map[string]any{
				"pageInfo": map[string]int{"results": 1},
				"results":  []any{map[string]any{"id": 7, "type": kind, "status": status, "createdAt": created, "requestedBy": map[string]int{"id": owner}, "media": map[string]any{"tmdbId": 22, "ratingKey": ratingKey}, "seasons": []any{map[string]int{"seasonNumber": 1}}}},
			})
			return response(200, string(b)), nil
		case "/api/collections/overlay-data":
			if f.mode == "source_failure" {
				return response(503, "private failure"), nil
			}
			if f.mode == "planned" || f.mode == "unknown_deadline" || f.mode == "parent_removal" {
				id, removalKind := "84", "movie"
				if f.mode == "parent_removal" {
					id, removalKind = "79", "show"
				}
				delay := "365"
				if f.mode == "unknown_deadline" {
					delay = "null"
				}
				return response(200, fmt.Sprintf(`[{"id":1,"mediaServerType":"plex","libraryId":"2","type":%q,"isActive":true,"arrAction":0,"deleteAfterDays":%s,"mediaCount":1,"media":[{"id":1,"collectionId":1,"mediaServerId":%q,"addDate":%q,"isManual":false,"ruleEvaluationFailed":false}]}]`, removalKind, delay, id, time.Now().UTC().Format(time.RFC3339))), nil
			}
			return response(200, "[]"), nil
		case "/api/v2":
			if r.URL.Query().Get("cmd") == "get_history" {
				if f.mode == "tautulli_started" || f.mode == "tautulli_completed" {
					percent, watched := 50, .5
					if f.mode == "tautulli_completed" {
						percent, watched = 100, 1
					}
					return response(200, fmt.Sprintf(`{"response":{"result":"success","data":{"recordsFiltered":1,"data":[{"row_id":1,"user_id":11111,"rating_key":84,"media_type":"movie","guid":"plex://movie/available","percent_complete":%d,"watched_status":%g,"live":0}]}}}`, percent, watched)), nil
				}
				return response(200, `{"response":{"result":"success","data":{"recordsFiltered":0,"data":[]}}}`), nil
			}
		case "/library/all":
			if r.Header.Get("X-Plex-Token") != "resource-11111" || r.URL.Query().Get("unwatched") != "1" {
				t.Error("unwatched proof is not personal")
			}
			rows := []any{}
			if f.mode != "not_positive" {
				mediaType := "movie"
				if kind == "tv" {
					mediaType = "episode"
				}
				rows = append(rows, map[string]any{"ratingKey": "84", "title": "Available request", "type": mediaType, "addedAt": f.addedAt.Unix(), "guid": "plex://movie/available", "parentRatingKey": "80", "grandparentRatingKey": "79"})
				if kind == "tv" {
					rows = append(rows, map[string]any{"ratingKey": "85", "title": "Second episode", "type": "episode", "addedAt": f.addedAt.Unix(), "guid": "plex://episode/second", "parentRatingKey": "80", "grandparentRatingKey": "79"})
				}
			}
			if f.mode == "large_unrelated_library" {
				for index := 1000; index < 1150; index++ {
					rows = append(rows, map[string]any{"ratingKey": fmt.Sprint(index), "title": "Unrelated film", "type": "movie", "addedAt": f.addedAt.Unix(), "guid": fmt.Sprintf("plex://movie/%d", index)})
				}
			}
			b, _ := json.Marshal(map[string]any{"MediaContainer": map[string]any{"size": len(rows), "totalSize": len(rows), "Metadata": rows}})
			return response(200, string(b)), nil
		case "/library/metadata/84", "/library/metadata/85", "/library/metadata/80", "/library/metadata/79":
			if f.beforeMetadata != nil {
				f.beforeMetadata()
			}
			if f.mode == "revoked" {
				return response(403, "private denied"), nil
			}
			if r.Header.Get("X-Plex-Token") != "resource-11111" {
				t.Error("metadata is not personal")
			}
			id := strings.TrimPrefix(r.URL.Path, "/library/metadata/")
			m := map[string]any{"ratingKey": id, "librarySectionID": "2", "title": "Available request", "type": "movie", "guid": "plex://movie/available", "Guid": []any{map[string]string{"id": "tmdb://22"}}, "addedAt": f.addedAt.Unix(), "Media": []any{map[string]any{"Part": []any{map[string]any{"key": "/library/parts/1/file", "exists": true, "accessible": true}}}}}
			if f.firstStarted && id == "84" {
				m["viewOffset"] = 1000
			}
			switch f.mode {
			case "completed":
				m["viewCount"] = 1
			case "started":
				m["viewOffset"] = 1000
			case "replacement":
				m["guid"] = "plex://other"
			case "changed_added_at":
				m["addedAt"] = f.addedAt.Add(time.Hour).Unix()
			case "missing_added_at":
				delete(m, "addedAt")
			case "not_playable":
				delete(m, "Media")
			}
			if kind == "tv" {
				m["type"], m["parentRatingKey"], m["grandparentRatingKey"], m["parentTitle"], m["grandparentTitle"] = "episode", "80", "79", "Saison 1", "Series"
				if id == "85" {
					m["guid"], m["title"] = "plex://episode/second", "Second episode"
				}
				if id == "80" {
					m["type"], m["parentRatingKey"], m["index"] = "season", "79", 1
					if f.mode == "wrong_season" {
						m["index"] = 2
					}
				}
				if id == "79" {
					m["type"] = "show"
				}
			}
			b, _ := json.Marshal(map[string]any{"MediaContainer": map[string]any{"size": 1, "Metadata": []any{m}}})
			return response(200, string(b)), nil
		}
		return original.RoundTrip(r)
	})
	return s, cookie, f
}

func TestAvailableRequestRemindersRequireEveryPersonalProof(t *testing.T) {
	for _, mode := range []string{"available", "recent_request", "recent_addition", "too_old", "missing_request_date", "completed", "started", "tautulli_started", "tautulli_completed", "not_positive", "other_owner", "request_deleted", "declined", "source_failure", "revoked", "replacement", "missing_added_at", "changed_added_at", "not_playable", "planned", "unknown_deadline", "tv", "wrong_season", "parent_removal", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			s, _, f := availableRequestFixture(t)
			f.mode = mode
			if mode == "recent_request" {
				f.requestedAt = time.Now().Add(-29 * 24 * time.Hour)
			}
			if mode == "recent_addition" {
				f.addedAt = time.Now().Add(-29 * 24 * time.Hour)
			}
			if mode == "too_old" {
				f.addedAt, f.requestedAt = time.Now().Add(-91*24*time.Hour), time.Now().Add(-100*24*time.Hour)
			}
			if mode == "disabled" {
				p, _ := s.Store.GetReminderPreferences(context.Background(), "11111")
				p.AvailableUnwatched = false
				s.Store.SaveReminderPreferences(context.Background(), "11111", p)
			}
			u, _ := s.Store.User(context.Background(), "11111")
			_, err := s.refreshReminders(context.Background(), u)
			failure := mode == "other_owner" || mode == "source_failure" || mode == "revoked" || mode == "replacement" || mode == "missing_added_at" || mode == "changed_added_at"
			if (err != nil) != failure {
				t.Fatalf("unexpected source result: %v", err)
			}
			rows, err := s.Store.ReminderHistory(context.Background(), u.ID, 200)
			want := 0
			if mode == "available" || mode == "tv" {
				want = 1
			}
			if err != nil || len(rows) != want {
				t.Fatalf("rows=%+v error=%v want=%d", rows, err, want)
			}
			if want == 1 && (rows[0].AvailableAt == nil || !rows[0].AvailableAt.Equal(f.addedAt) || !rows[0].DueAt.IsZero() || rows[0].MilestoneDays != 0 || rows[0].Reason != "available_unwatched" || rows[0].State != "sent") {
				t.Fatalf("fabricated date/duplicate season: %+v", rows)
			}
		})
	}
}

func TestAvailableRequestInboxAndDMReuseDailyDigest(t *testing.T) {
	s, cookie, f := availableRequestFixture(t)
	s.Config.DiscordDMsEnabled = true
	s.Discord = notifications.NewDiscord("fixture-token")
	p, _ := s.Store.GetReminderPreferences(context.Background(), "11111")
	p.DiscordDM = true
	s.Store.SaveReminderPreferences(context.Background(), "11111", p)
	w := call(s, "GET", "/api/v1/reminders", cookie, "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "estimatedAt") || !strings.Contains(w.Body.String(), `"availableAt"`) {
		t.Fatalf("wrong date semantics: %d %s", w.Code, w.Body)
	}
	calls := 0
	s.Discord.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if strings.HasSuffix(r.URL.Path, "/users/@me/channels") {
			return response(200, `{"id":"456","type":1,"recipients":[{"id":"11111"}]}`), nil
		}
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		content, _ := payload["content"].(string)
		if !strings.Contains(content, "ajouté dans Plex") || strings.Contains(content, "départ estimé") {
			t.Fatalf("wrong DM wording: %s", content)
		}
		return response(200, `{"id":"789","channel_id":"456"}`), nil
	})
	u, _ := s.Store.User(context.Background(), "11111")
	for range 2 {
		if err := s.deliverDiscordReminders(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("duplicate DM: %d", calls)
	}
	f.mode = "tautulli_started"
	w = call(s, "GET", "/api/v1/reminders", cookie, "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "Available request") {
		t.Fatalf("started content retained: %d %s", w.Code, w.Body)
	}
}

func TestAvailableRequestLateLogoutDoesNotDeliver(t *testing.T) {
	s, _, f := availableRequestFixture(t)
	f.beforeMetadata = func() { s.mu.Lock(); delete(s.grants, "11111"); s.mu.Unlock() }
	u, _ := s.Store.User(context.Background(), "11111")
	if _, err := s.refreshReminders(context.Background(), u); err == nil {
		t.Fatal("late logout authorized a reminder")
	}
	rows, _ := s.Store.ReminderHistory(context.Background(), u.ID, 200)
	if len(rows) != 0 {
		t.Fatalf("late logout delivered content: %+v", rows)
	}
}

func TestAvailableRequestDMRevalidatesCurrentRequestAndViewingState(t *testing.T) {
	for _, mode := range []string{"completed", "tautulli_started", "request_deleted", "planned", "revoked", "opt_out"} {
		t.Run(mode, func(t *testing.T) {
			s, _, f := availableRequestFixture(t)
			s.Config.DiscordDMsEnabled = true
			s.Discord = notifications.NewDiscord("fixture-token")
			p, _ := s.Store.GetReminderPreferences(context.Background(), "11111")
			p.DiscordDM = true
			if err := s.Store.SaveReminderPreferences(context.Background(), "11111", p); err != nil {
				t.Fatal(err)
			}
			u, _ := s.Store.User(context.Background(), "11111")
			if _, err := s.refreshReminders(context.Background(), u); err != nil {
				t.Fatal(err)
			}
			f.mode = mode
			if mode == "opt_out" {
				p.AvailableUnwatched = false
				if err := s.Store.SaveReminderPreferences(context.Background(), u.ID, p); err != nil {
					t.Fatal(err)
				}
			}
			s.Discord.HTTP.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalidated request reminder contacted Discord")
				return nil, nil
			})
			if err := s.deliverDiscordReminders(context.Background(), u); err != nil {
				t.Fatal(err)
			}
			if discordState(t, s) != "failed" {
				t.Fatal("invalidated reminder remained sendable")
			}
		})
	}
}

func TestAvailableRequestCandidatesSkipUnrelatedLargeCatalogue(t *testing.T) {
	s, _, f := availableRequestFixture(t)
	f.mode = "large_unrelated_library"
	u, _ := s.Store.User(context.Background(), "11111")
	page, err := s.refreshReminders(context.Background(), u)
	if err != nil || len(page.Available) != 1 {
		t.Fatalf("unrelated library exhausted request budget: %+v %v", page.Available, err)
	}
}

func TestAvailableRequestPendingSeasonUpdatesItsProvenEpisode(t *testing.T) {
	s, _, f := availableRequestFixture(t)
	f.mode = "tv"
	u, _ := s.Store.User(context.Background(), "11111")
	g, err := s.grant(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := s.availableRequestReminders(context.Background(), u, g, time.Now())
	if err != nil || len(initial) != 1 || !strings.HasPrefix(initial[0].MediaKey, "available:84:") {
		t.Fatalf("initial: %+v %v", initial, err)
	}
	if err = s.Store.EnqueueReminders(context.Background(), u.ID, []store.Reminder{initial[0].Reminder}); err != nil {
		t.Fatal(err)
	}
	f.firstStarted = true
	if _, err = s.refreshReminders(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Store.ReminderHistory(context.Background(), u.ID, 200)
	if err != nil || len(rows) != 1 || rows[0].State != "sent" || !strings.HasPrefix(rows[0].MediaKey, "available:85:") {
		t.Fatalf("obsolete episode consumed request: %+v %v", rows, err)
	}
}

func TestAvailableRequestSourceChangesDuringDiscordChannelCreation(t *testing.T) {
	for _, mode := range []string{"revoked", "tautulli_started", "request_deleted", "source_failure", "logout_during_check"} {
		t.Run(mode, func(t *testing.T) {
			s, _, f := availableRequestFixture(t)
			s.Config.DiscordDMsEnabled = true
			s.Discord = notifications.NewDiscord("fixture-token")
			p, _ := s.Store.GetReminderPreferences(context.Background(), "11111")
			p.DiscordDM = true
			if err := s.Store.SaveReminderPreferences(context.Background(), "11111", p); err != nil {
				t.Fatal(err)
			}
			u, _ := s.Store.User(context.Background(), "11111")
			if _, err := s.refreshReminders(context.Background(), u); err != nil {
				t.Fatal(err)
			}
			calls := 0
			s.Discord.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if !strings.HasSuffix(r.URL.Path, "/users/@me/channels") {
					t.Fatal("stale private data sent after source changed")
				}
				f.mode = mode
				if mode == "logout_during_check" {
					f.beforeMetadata = func() { s.mu.Lock(); delete(s.grants, u.ID); s.mu.Unlock() }
				}
				return response(200, `{"id":"456","type":1,"recipients":[{"id":"11111"}]}`), nil
			})
			if err := s.deliverDiscordReminders(context.Background(), u); err != nil {
				t.Fatal(err)
			}
			state := "failed"
			if mode == "source_failure" || mode == "tautulli_started" || mode == "request_deleted" {
				state = "queued"
			}
			if calls != 1 || discordState(t, s) != state {
				t.Fatalf("calls=%d state=%s want=%s", calls, discordState(t, s), state)
			}
			if mode == "tautulli_started" || mode == "request_deleted" {
				if _, err := s.Store.DB.Exec(`UPDATE notification_deliveries SET not_before=0`); err != nil {
					t.Fatal(err)
				}
				if err := s.deliverDiscordReminders(context.Background(), u); err != nil || calls != 1 || discordState(t, s) != "failed" {
					t.Fatalf("empty rebuilt digest must stop without another Discord call: %v %d %s", err, calls, discordState(t, s))
				}
			}
		})
	}
}

func TestAvailableRequestLargeSeasonNeedsOnlyOneProofPerVerifiedGroup(t *testing.T) {
	for _, mode := range []string{"same_season", "second_season", "expired_siblings"} {
		t.Run(mode, func(t *testing.T) {
			secondSeason := mode == "second_season"
			s, _, f := availableRequestFixture(t)
			f.mode = "tv"
			if mode == "expired_siblings" {
				f.requestedAt = time.Now().Add(-120 * 24 * time.Hour)
			}
			original := s.Upstream.HTTP.Transport
			reads := 0
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				var data any
				if strings.HasPrefix(r.URL.Path, "/library/metadata/") {
					reads++
				}
				switch r.URL.Path {
				case "/api/v1/user/9/requests":
					seasons := []any{map[string]int{"seasonNumber": 1}}
					if secondSeason {
						seasons = append(seasons, map[string]int{"seasonNumber": 2})
					}
					data = map[string]any{"pageInfo": map[string]int{"results": 1}, "results": []any{map[string]any{"id": 7, "type": "tv", "status": 5, "createdAt": f.requestedAt, "requestedBy": map[string]int{"id": 9}, "media": map[string]any{"tmdbId": 22, "ratingKey": "79"}, "seasons": seasons}}}
				case "/library/all":
					rows := []any{map[string]any{"ratingKey": "84", "parentRatingKey": "80", "grandparentRatingKey": "79", "title": "First episode", "type": "episode", "addedAt": f.addedAt.Unix(), "guid": "plex://movie/available"}}
					if secondSeason {
						rows = append(rows, map[string]any{"ratingKey": "85", "parentRatingKey": "81", "grandparentRatingKey": "79", "title": "Another season", "type": "episode", "addedAt": f.addedAt.Unix(), "guid": "plex://episode/second"})
					}
					for id := 9000; id < 9110; id++ {
						added := f.addedAt
						if mode == "expired_siblings" {
							added = time.Now().Add(-100 * 24 * time.Hour)
						}
						rows = append(rows, map[string]any{"ratingKey": fmt.Sprint(id), "parentRatingKey": "80", "grandparentRatingKey": "79", "title": "Same season sibling", "type": "episode", "addedAt": added.Unix(), "guid": fmt.Sprintf("plex://episode/%d", id)})
					}
					data = map[string]any{"MediaContainer": map[string]any{"size": len(rows), "totalSize": len(rows), "Metadata": rows}}
				case "/library/metadata/85":
					episode := map[string]any{"ratingKey": "85", "parentRatingKey": "81", "grandparentRatingKey": "79", "librarySectionID": "2", "title": "Another season", "parentTitle": "Season 2", "grandparentTitle": "Series", "type": "episode", "guid": "plex://episode/second", "addedAt": f.addedAt.Unix()}
					episode["Media"] = []any{map[string]any{"Part": []any{map[string]any{"key": "/library/parts/2/file", "exists": true, "accessible": true}}}}
					data = map[string]any{"MediaContainer": map[string]any{"size": 1, "Metadata": []any{episode}}}
				case "/library/metadata/81":
					data = map[string]any{"MediaContainer": map[string]any{"size": 1, "Metadata": []any{map[string]any{"ratingKey": "81", "parentRatingKey": "79", "librarySectionID": "2", "title": "Season 2", "type": "season", "index": 2, "guid": "plex://season/second"}}}}
				}
				if data != nil {
					body, _ := json.Marshal(data)
					return response(200, string(body)), nil
				}
				return original.RoundTrip(r)
			})
			u, _ := s.Store.User(context.Background(), "11111")
			page, err := s.refreshReminders(context.Background(), u)
			want, expectedReads := 1, 3
			if secondSeason {
				want, expectedReads = 2, 5
			}
			if err != nil || len(page.Available) != want || reads != expectedReads {
				t.Fatalf("groups=%d reads=%d want=%d/%d err=%v", len(page.Available), reads, want, expectedReads, err)
			}
			if secondSeason && !strings.HasPrefix(page.Available[1].MediaKey, "available:85:") {
				t.Fatalf("unproven season skipped: %+v", page.Available)
			}
		})
	}
}
