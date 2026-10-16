package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/store"
)

type reminderSources struct {
	addedAt         time.Time
	excluded        bool
	inactive        bool
	completed       bool
	failing         bool
	metadataStatus  int
	collectionReads int
	metadataReads   int
	beforeMetadata  func()
}

func reminderFixture(t *testing.T) (*Server, string, *reminderSources) {
	t.Helper()
	s := fixture(t)
	s.Config.SeerrURL, s.Config.SeerrAPIKey = "", ""
	s.Config.MaintainerrURL = "http://maintainerr.test"
	s.Config.TautulliURL = "http://tautulli.test"
	s.Config.TautulliAPIKey = "private-reminder-source-key"
	cookie := seed(t, s, "11111", true)
	p, err := s.Store.GetReminderPreferences(context.Background(), "11111")
	if err != nil {
		t.Fatal(err)
	}
	p.Enabled, p.QuietStartHour, p.QuietEndHour = true, 0, 0
	if err = s.Store.SaveReminderPreferences(context.Background(), "11111", p); err != nil {
		t.Fatal(err)
	}
	f := &reminderSources{addedAt: time.Now().UTC().Truncate(time.Second).Add(-6 * 24 * time.Hour), metadataStatus: 200}
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Errorf("reminder collection mutated upstream: %s", r.Method)
		}
		body := ""
		switch r.URL.Path {
		case "/identity":
			body = `{"MediaContainer":{"machineIdentifier":"server-a"}}`
		case "/api/app/status":
			body = `{"status":1,"version":"3.30.1"}`
		case "/api/media-server/type":
			body = `{"type":"plex"}`
		case "/api/media-server":
			body = `{"machineId":"server-a"}`
		case "/api/collections/overlay-data":
			f.collectionReads++
			if f.failing {
				return response(503, "private-upstream-error"), nil
			}
			body = fmt.Sprintf(`[{"id":1,"mediaServerType":"plex","libraryId":"2","type":"movie","isActive":true,"arrAction":0,"deleteAfterDays":7,"mediaCount":1,"media":[{"id":1,"collectionId":1,"mediaServerId":"42","addDate":%q,"isManual":false,"ruleEvaluationFailed":false}]}]`, f.addedAt.Format(time.RFC3339Nano))
		case "/api/rules":
			body = fmt.Sprintf(`[{"id":5,"collectionId":1,"isActive":%v}]`, !f.inactive)
		case "/api/rules/exclusion":
			body = `[]`
			if f.excluded {
				body = `[{"id":1,"mediaServerId":"42","ruleGroupId":5,"type":"movie"}]`
			}
		case "/api/v2":
			if r.Header.Get("X-Api-Key") != s.Config.TautulliAPIKey {
				t.Error("history source credential missing")
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
				rows := []any{}
				if r.URL.Query().Get("user_id") == "11111" {
					rows = append(rows, map[string]any{"row_id": 1, "user_id": 11111, "rating_key": 42, "media_type": "movie", "guid": "plex://movie/reminder", "percent_complete": 50, "watched_status": .5, "live": 0})
				}
				data = map[string]any{"recordsFiltered": len(rows), "data": rows}
			default:
				t.Errorf("unexpected history command: %s", r.URL.Query().Get("cmd"))
			}
			encoded, _ := json.Marshal(map[string]any{"response": map[string]any{"result": "success", "data": data}})
			body = string(encoded)
		case "/library/metadata/42":
			f.metadataReads++
			if r.Header.Get("X-Plex-Token") != "resource-11111" || r.URL.Query().Get("skipRefresh") != "1" {
				t.Error("metadata not read with personal Plex authorization")
			}
			if f.beforeMetadata != nil {
				f.beforeMetadata()
			}
			if f.metadataStatus != 200 {
				return response(f.metadataStatus, "private-media-error"), nil
			}
			views := 0
			if f.completed {
				views = 1
			}
			body = fmt.Sprintf(`{"MediaContainer":{"size":1,"librarySectionID":"2","Metadata":[{"ratingKey":"42","title":"Private reminder movie","type":"movie","guid":"plex://movie/reminder","viewCount":%d}]}}`, views)
		default:
			t.Errorf("unexpected reminder upstream route: %s", r.URL.Path)
			return response(500, "unexpected route"), nil
		}
		return response(200, body), nil
	})
	return s, cookie, f
}

func reminderItems(t *testing.T, w *httptest.ResponseRecorder) []inboxItem {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("reminder inbox: %d %s", w.Code, w.Body)
	}
	var page struct {
		Items     []inboxItem `json:"items"`
		Scope     string      `json:"scope"`
		UpdatedAt time.Time   `json:"updatedAt"`
		ExpiresAt time.Time   `json:"expiresAt"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Items == nil || page.Scope != "recorded_history" || !page.ExpiresAt.After(page.UpdatedAt) {
		t.Fatalf("invalid reminder page: %s", w.Body)
	}
	return page.Items
}

func TestReminderPreferenceBoundaryAndIsolation(t *testing.T) {
	s := fixture(t)
	a, b := seed(t, s, "11111", true), seed(t, s, "22222", true)
	const path = "/api/v1/reminders/preferences"
	const valid = `{"enabled":true,"daysBefore":[7,2],"quietStartHour":22,"quietEndHour":8,"dailyCap":10,"allUnwatched":false}`
	for _, path := range []string{path, "/api/v1/reminders"} {
		if w := call(s, "GET", path, "", "", ""); w.Code != 401 {
			t.Fatalf("anonymous reminder access: %s %d", path, w.Code)
		}
	}
	if w := call(s, "PUT", path, a, "https://attacker.example", valid); w.Code != 403 {
		t.Fatal("CSRF preference mutation accepted")
	}
	for _, invalid := range []string{
		`{}`,
		strings.Replace(valid, `"enabled":true,`, "", 1),
		strings.Replace(valid, `"enabled":true`, `"enabled":null`, 1),
		strings.Replace(valid, `,"allUnwatched":false`, "", 1),
		strings.Replace(valid, `"quietStartHour":22`, `"quietStartHour":null`, 1),
		strings.Replace(valid, "[7,2]", "[2,2]", 1),
		strings.Replace(valid, "[7,2]", "[31]", 1),
		strings.Replace(valid, `"dailyCap":10`, `"dailyCap":101`, 1),
		strings.Replace(valid, `"quietEndHour":8`, `"quietEndHour":24`, 1),
		strings.TrimSuffix(valid, "}") + `,"userId":"22222"}`,
		valid + `{}`,
	} {
		if w := call(s, "PUT", path, a, s.Config.PublicOrigin, invalid); w.Code != 400 {
			t.Fatalf("invalid preference body accepted: %s -> %d %s", invalid, w.Code, w.Body)
		}
	}
	if w := call(s, "PUT", path, a, s.Config.PublicOrigin, valid); w.Code != 200 {
		t.Fatalf("valid preference update failed: %d %s", w.Code, w.Body)
	}
	var other store.ReminderPreferences
	w := call(s, "GET", path, b, "", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &other) != nil || other.Enabled {
		t.Fatalf("preferences crossed accounts: %s", w.Body)
	}
}

func TestReminderInboxPrivateDurableAndDeduplicated(t *testing.T) {
	s, cookie, sources := reminderFixture(t)
	// Sub-millisecond source dates must survive storage precision when matched.
	sources.addedAt = sources.addedAt.Add(123456789 * time.Nanosecond)
	if w := call(s, "GET", "/api/v1/reminders?userId=22222", cookie, "", ""); w.Code != 400 || sources.collectionReads != 0 {
		t.Fatal("caller selected a reminder owner")
	}
	w := call(s, "GET", "/api/v1/reminders", cookie, "", "")
	items := reminderItems(t, w)
	if len(items) != 1 || items[0].Title != "Private reminder movie" || items[0].Reason != "unfinished_before_removal" || items[0].MilestoneDays != 2 || items[0].SentAt.IsZero() {
		t.Fatalf("missing personal reminder: %+v", items)
	}
	for _, secret := range []string{"11111", "22222", "private-reminder-source-key", "resource-", "decisionKey", "mediaKey"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("private implementation detail exposed: %s", secret)
		}
	}
	second := reminderItems(t, call(s, "GET", "/api/v1/reminders", cookie, "", ""))
	if len(second) != 1 || second[0].ID != items[0].ID || sources.collectionReads != 2 {
		t.Fatalf("reminder duplicated or sources reused: %+v, reads %d", second, sources.collectionReads)
	}
	history, err := s.Store.ReminderHistory(context.Background(), "11111", 100)
	if err != nil || len(history) != 1 || history[0].State != "sent" {
		t.Fatalf("delivery not durably recorded: %+v %v", history, err)
	}
	other := seed(t, s, "22222", true)
	if err = s.Store.Preferences(context.Background(), "22222", true); err != nil {
		t.Fatal(err)
	}
	if got := reminderItems(t, call(s, "GET", "/api/v1/reminders", other, "", "")); len(got) != 0 {
		t.Fatalf("another user's reminder exposed: %+v", got)
	}
	if sources.metadataReads != 2 {
		t.Fatal("metadata read for an unrelated user")
	}
}

func TestReminderFreshSourcesRemoveObsoleteInboxItems(t *testing.T) {
	for _, change := range []string{"excluded", "inactive", "postponed", "completed", "removed", "source error", "access denied"} {
		t.Run(change, func(t *testing.T) {
			s, cookie, sources := reminderFixture(t)
			if len(reminderItems(t, call(s, "GET", "/api/v1/reminders", cookie, "", ""))) != 1 {
				t.Fatal("initial reminder not delivered")
			}
			switch change {
			case "excluded":
				sources.excluded = true
			case "inactive":
				sources.inactive = true
			case "postponed":
				sources.addedAt = sources.addedAt.Add(3 * 24 * time.Hour)
			case "completed":
				sources.completed = true
			case "removed":
				sources.metadataStatus = 404
			case "source error":
				sources.failing = true
			case "access denied":
				sources.metadataStatus = 403
			}
			w := call(s, "GET", "/api/v1/reminders", cookie, "", "")
			if change == "source error" || change == "access denied" {
				if w.Code != 503 {
					t.Fatalf("unverifiable reminder accepted: %d %s", w.Code, w.Body)
				}
			} else if len(reminderItems(t, w)) != 0 {
				t.Fatalf("obsolete reminder still visible: %s", w.Body)
			}
			if strings.Contains(w.Body.String(), "Private reminder movie") || strings.Contains(w.Body.String(), "private-upstream-error") || strings.Contains(w.Body.String(), "private-media-error") {
				t.Fatal("stored private result leaked after source changed")
			}
			if sources.collectionReads != 2 {
				t.Fatal("delivery reused cached Maintainerr decision")
			}
		})
	}
}

func TestReminderQuietQueueIsCancelledAfterSourceChange(t *testing.T) {
	s, cookie, sources := reminderFixture(t)
	ctx := context.Background()
	p, _ := s.Store.GetReminderPreferences(ctx, "11111")
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	p.QuietStartHour = time.Now().In(loc).Hour()
	p.QuietEndHour = (p.QuietStartHour + 2) % 24
	if err = s.Store.SaveReminderPreferences(ctx, "11111", p); err != nil {
		t.Fatal(err)
	}
	if len(reminderItems(t, call(s, "GET", "/api/v1/reminders", cookie, "", ""))) != 0 {
		t.Fatal("reminder delivered during quiet hours")
	}
	history, err := s.Store.ReminderHistory(ctx, "11111", 100)
	if err != nil || len(history) != 1 || history[0].State != "pending" || !history[0].NotBefore.After(time.Now()) {
		t.Fatalf("quiet reminder not deferred durably: %+v %v", history, err)
	}
	sources.excluded = true
	if len(reminderItems(t, call(s, "GET", "/api/v1/reminders", cookie, "", ""))) != 0 {
		t.Fatal("excluded reminder delivered")
	}
	history, err = s.Store.ReminderHistory(ctx, "11111", 100)
	if err != nil || len(history) != 1 || history[0].State != "cancelled" {
		t.Fatalf("obsolete pending job survived: %+v %v", history, err)
	}
}

func TestReminderRevocationDuringCollectionPreventsDelivery(t *testing.T) {
	for _, change := range []string{"logout", "unlink", "revoked grant", "changed grant", "deleted account"} {
		t.Run(change, func(t *testing.T) {
			s, cookie, sources := reminderFixture(t)
			sources.beforeMetadata = func() {
				switch change {
				case "logout":
					if w := call(s, "POST", "/api/v1/auth/logout", cookie, s.Config.PublicOrigin, ""); w.Code != 200 {
						t.Fatal("logout failed")
					}
				case "unlink":
					if err := s.Store.Unlink(context.Background(), "11111"); err != nil {
						t.Fatal(err)
					}
				case "deleted account":
					if err := s.Store.Delete(context.Background(), "11111"); err != nil {
						t.Fatal(err)
					}
				case "revoked grant":
					s.mu.Lock()
					delete(s.grants, "11111")
					s.mu.Unlock()
				case "changed grant":
					s.mu.Lock()
					grant := s.grants["11111"]
					grant.ServerToken = "replacement-token"
					s.grants["11111"] = grant
					s.mu.Unlock()
				}
			}
			w := call(s, "GET", "/api/v1/reminders", cookie, "", "")
			if w.Code == 200 || strings.Contains(w.Body.String(), "Private reminder movie") {
				t.Fatalf("revoked identity received reminder: %d %s", w.Code, w.Body)
			}
			history, err := s.Store.ReminderHistory(context.Background(), "11111", 100)
			if err != nil || len(history) != 0 {
				t.Fatalf("revoked identity left durable delivery: %+v %v", history, err)
			}
		})
	}
}
