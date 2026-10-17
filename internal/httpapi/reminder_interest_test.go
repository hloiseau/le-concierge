package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/store"
)

func TestUnstartedRemindersRequirePersonalIntentAndPositivePlexState(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		follow, requested, all, known, completed bool
		owner                                    int
		requestStatus                            int
		want                                     int
	}{
		{name: "default ignores unknown", known: true},
		{name: "followed non vu", follow: true, known: true, want: 1},
		{name: "absence is not non vu", follow: true},
		{name: "completed overrides filter", follow: true, known: true, completed: true},
		{name: "explicit all non vu", all: true, known: true, want: 1},
		{name: "own request", requested: true, known: true, owner: 9, want: 1},
		{name: "completed request still unwatched", requested: true, requestStatus: 5, known: true, owner: 9, want: 1},
		{name: "declined request excluded", requested: true, requestStatus: 3, known: true, owner: 9},
		{name: "another requester", requested: true, known: true, owner: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, f := reminderFixture(t)
			ctx := context.Background()
			u, _ := s.Store.User(ctx, "11111")
			if tc.follow {
				if _, e := s.Store.SaveSubscription(ctx, u.ID, store.SubscriptionTarget{Type: "movie", TMDBID: 22}, "hub"); e != nil {
					t.Fatal(e)
				}
			}
			p, _ := s.Store.GetReminderPreferences(ctx, u.ID)
			p.AllUnwatched = tc.all
			s.Store.SaveReminderPreferences(ctx, u.ID, p)
			if tc.requested {
				s.Config.SeerrURL = "http://seerr.test"
				s.Config.SeerrAPIKey = "test-key"
			}
			original := s.Upstream.HTTP.Transport
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/api/v1/user":
					return response(200, `{"pageInfo":{"results":1},"results":[{"id":9,"plexId":11111}]}`), nil
				case "/api/v1/user/9/requests":
					status := tc.requestStatus
					if status == 0 {
						status = 2
					}
					return response(200, fmt.Sprintf(`{"pageInfo":{"results":1},"results":[{"id":1,"type":"movie","status":%d,"requestedBy":{"id":%d},"media":{"tmdbId":22}}]}`, status, tc.owner)), nil
				case "/library/all":
					if r.URL.Query().Get("unwatched") != "1" || r.Header.Get("X-Plex-Token") != "resource-11111" {
						t.Fatal("not a personal unwatched query")
					}
					if tc.known {
						return response(200, `{"MediaContainer":{"size":1,"totalSize":1,"Metadata":[{"ratingKey":"42","title":"Movie","type":"movie","addedAt":1,"guid":"plex://movie/reminder"}]}}`), nil
					}
					return response(200, `{"MediaContainer":{"size":0,"totalSize":0,"Metadata":[]}}`), nil
				case "/library/metadata/42":
					count := 0
					if tc.completed {
						count = 1
					}
					return response(200, fmt.Sprintf(`{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"42","librarySectionID":"2","title":"Movie","type":"movie","guid":"plex://movie/reminder","Guid":[{"id":"tmdb://22"}],"viewCount":%d}]}}`, count)), nil
				case "/api/v2":
					if r.URL.Query().Get("cmd") == "get_history" {
						return response(200, `{"response":{"result":"success","data":{"recordsFiltered":0,"data":[]}}}`), nil
					}
				}
				return original.RoundTrip(r)
			})
			page, err := s.refreshReminders(ctx, u)
			if tc.owner == 10 {
				if err == nil {
					t.Fatal("another request owner accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			history, _ := s.Store.ReminderHistory(ctx, u.ID, 200)
			sent := 0
			for _, r := range history {
				if r.State == "sent" {
					sent++
					if r.Reason != "unstarted_before_removal" {
						t.Fatal(r.Reason)
					}
				}
			}
			if sent != tc.want {
				t.Fatalf("sent=%d want=%d page=%+v", sent, tc.want, page)
			}
			if tc.follow && tc.want == 1 {
				rows, _, _ := s.Store.Subscriptions(ctx, u.ID, 0)
				s.Store.DeleteSubscription(ctx, u.ID, rows[0].ID)
				page, err = s.refreshReminders(ctx, u)
				if err != nil || len(page.Items) != 0 {
					t.Fatalf("unsubscribe retained personal risk: %+v %v", page, err)
				}
			}
			_ = f
		})
	}
}

func TestReminderInboxUsesCurrentWatchReason(t *testing.T) {
	s, cookie, _ := reminderFixture(t)
	ctx := context.Background()
	u, err := s.Store.User(ctx, "11111")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.refreshReminders(ctx, u); err != nil {
		t.Fatal(err)
	}
	// This stored reason predates the current personal in-progress observation.
	if _, err = s.Store.DB.Exec(`UPDATE reminder_outbox SET reason='unstarted_before_removal' WHERE user_id=?`, u.ID); err != nil {
		t.Fatal(err)
	}
	w := call(s, "GET", "/api/v1/reminders", cookie, "", "")
	items := reminderItems(t, w)
	if len(items) != 1 || items[0].Reason != "unfinished_before_removal" {
		t.Fatalf("outdated unwatched claim: %s", w.Body)
	}
}

func TestSeasonReminderOnlyContainsUnwatchedEpisodesOfFollowedSeason(t *testing.T) {
	s, _, f := reminderFixture(t)
	ctx := context.Background()
	u, _ := s.Store.User(ctx, "11111")
	season := 1
	s.Store.SaveSubscription(ctx, u.ID, store.SubscriptionTarget{Type: "tv", TMDBID: 777, Season: &season}, "hub")
	original := s.Upstream.HTTP.Transport
	metadata := map[string]map[string]any{
		"400": {"type": "show", "title": "Series", "Guid": []any{map[string]any{"id": "tmdb://777"}}},
		"401": {"type": "season", "title": "Season 1", "parentRatingKey": "400", "index": 1},
		"411": {"type": "season", "title": "Season 2", "parentRatingKey": "400", "index": 2},
		"402": {"type": "episode", "title": "Episode 1", "parentRatingKey": "401", "grandparentRatingKey": "400"},
		"403": {"type": "episode", "title": "Episode 2", "parentRatingKey": "401", "grandparentRatingKey": "400", "viewCount": 1},
		"412": {"type": "episode", "title": "Episode 1", "parentRatingKey": "411", "grandparentRatingKey": "400"},
	}
	for id, m := range metadata {
		m["ratingKey"] = id
		m["librarySectionID"] = "2"
		m["guid"] = "plex://" + id
	}
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		var body any
		switch r.URL.Path {
		case "/api/collections/overlay-data":
			return response(200, fmt.Sprintf(`[{"id":1,"mediaServerType":"plex","libraryId":"2","type":"show","isActive":true,"arrAction":0,"deleteAfterDays":7,"mediaCount":1,"media":[{"id":1,"collectionId":1,"mediaServerId":"400","addDate":%q,"isManual":false,"ruleEvaluationFailed":false}]}]`, f.addedAt.Format(time.RFC3339Nano))), nil
		case "/api/v2":
			if r.URL.Query().Get("cmd") == "get_history" {
				return response(200, `{"response":{"result":"success","data":{"recordsFiltered":0,"data":[]}}}`), nil
			}
		case "/library/all":
			body = map[string]any{"MediaContainer": map[string]any{"size": 2, "totalSize": 2, "Metadata": []any{map[string]any{"ratingKey": "402", "type": "episode", "title": "one", "addedAt": 1, "guid": "plex://402"}, map[string]any{"ratingKey": "412", "type": "episode", "title": "two", "addedAt": 1, "guid": "plex://412"}}}}
		case "/library/metadata/400/grandchildren":
			body = map[string]any{"MediaContainer": map[string]any{"size": 3, "totalSize": 3, "Metadata": []any{metadata["402"], metadata["403"], metadata["412"]}}}
		default:
			for id, m := range metadata {
				if r.URL.Path == "/library/metadata/"+id {
					body = map[string]any{"MediaContainer": map[string]any{"size": 1, "Metadata": []any{m}}}
				}
			}
		}
		if body != nil {
			b, _ := json.Marshal(body)
			return response(200, string(b)), nil
		}
		return original.RoundTrip(r)
	})
	page, err := s.refreshReminders(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	history, _ := s.Store.ReminderHistory(ctx, u.ID, 200)
	if len(history) != 1 || history[0].MediaKey != "402:1:1:0" {
		t.Fatalf("wrong threatened episodes: %+v page=%+v", history, page)
	}
}
