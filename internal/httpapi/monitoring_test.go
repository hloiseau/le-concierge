package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func activityTransport(t *testing.T, s *Server, wait func()) *atomic.Int32 {
	t.Helper()
	calls := &atomic.Int32{}
	s.Config.MonitoringEnabled = true
	s.Config.TautulliURL = "http://tautulli.test"
	s.Config.TautulliAPIKey = "secret"
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		data := "{}"
		switch r.URL.Query().Get("cmd") {
		case "get_tautulli_info":
			data = `{"tautulli_version":"v2.18.2"}`
		case "get_server_info":
			data = `{"pms_identifier":"server-a"}`
		case "get_activity":
			if wait != nil {
				wait()
			}
			data = `{"stream_count":1,"sessions":[{"friendly_name":"PRIVATE PERSON","full_title":"PRIVATE TITLE","state":"playing","transcode_decision":"transcode","stream_video_codec":"hevc","stream_audio_codec":"aac","bandwidth":"5000","transcode_hw_decoding":1,"transcode_hw_encoding":0,"ip_address":"PRIVATE ADDRESS","file":"PRIVATE PATH"}]}`
		default:
			t.Errorf("unexpected activity request %s", r.URL.Path)
		}
		return response(200, `{"response":{"result":"success","data":`+data+`}}`), nil
	})
	return calls
}
func TestMonitoringAuthorizationAndPrivacy(t *testing.T) {
	s := fixture(t)
	calls := activityTransport(t, s, nil)
	cookie := seed(t, s, "11111", true)
	unlinked := seed(t, s, "22222", false)
	for _, path := range []string{"/api/v1/monitoring", "/api/v1/monitoring/history", "/api/v1/admin/plex-activity"} {
		if w := call(s, "GET", path, "", "", ""); w.Code != 401 {
			t.Fatalf("anonymous %s %d", path, w.Code)
		}
		if w := call(s, "GET", path, unlinked, "", ""); w.Code != 403 {
			t.Fatalf("unlinked %s %d", path, w.Code)
		}
	}
	if w := call(s, "GET", "/api/v1/admin/plex-activity", cookie, "", ""); w.Code != 403 || calls.Load() != 0 {
		t.Fatal("nonadmin fetched named sessions")
	}
	w := call(s, "GET", "/api/v1/monitoring", cookie, "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "hevc") {
		t.Fatalf("unsafe public snapshot %d %s", w.Code, w.Body)
	}
	var current struct {
		HostStatus string `json:"hostStatus"`
		Activity   struct {
			Status   string `json:"status"`
			Sessions *int   `json:"sessions"`
		} `json:"activity"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &current); err != nil || current.HostStatus != "not_configured" || current.Activity.Status != "available" || current.Activity.Sessions == nil || *current.Activity.Sessions != 1 {
		t.Fatal("Plex activity hidden without a host collector", err)
	}
	if calls.Load() != 3 {
		t.Fatal("expected one verified activity fetch")
	}
	if err := s.Monitoring.Sample(context.Background(), s.Config); err != nil {
		t.Fatal(err)
	}
	w = call(s, "GET", "/api/v1/monitoring/history?range=1h", cookie, "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal("unsafe history")
	}
	var history struct {
		Items []json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &history)
	if len(history.Items) != 1 {
		t.Fatal("sample not persisted")
	}
	if calls.Load() != 3 {
		t.Fatal("cache did not coalesce fetches")
	}
	s.Config.AdminIDs = []string{"11111"}
	w = call(s, "GET", "/api/v1/admin/plex-activity", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "PRIVATE PERSON") || !strings.Contains(w.Body.String(), "hevc") || strings.Contains(w.Body.String(), "PRIVATE ADDRESS") || strings.Contains(w.Body.String(), "PRIVATE PATH") {
		t.Fatalf("wrong admin details %d %s", w.Code, w.Body)
	}
	for _, path := range []string{"/api/v1/monitoring?user=other", "/api/v1/admin/plex-activity?user=other", "/api/v1/monitoring/history?range=forever", "/api/v1/monitoring/history?range=1h&range=7d", "/api/v1/monitoring/history?user=other"} {
		if w = call(s, "GET", path, cookie, "", ""); w.Code != 400 {
			t.Fatalf("invalid query %s %d", path, w.Code)
		}
	}
	s.Config.MonitoringEnabled = false
	if w = call(s, "GET", "/api/v1/monitoring", cookie, "", ""); w.Code != 503 {
		t.Fatal("disabled monitoring exposed")
	}
}

func TestMonitoringRevalidatesAfterFetch(t *testing.T) {
	for _, mode := range []string{"logout", "admin_removed"} {
		t.Run(mode, func(t *testing.T) {
			s := fixture(t)
			cookie := seed(t, s, "11111", true)
			s.Config.AdminIDs = []string{"11111"}
			requested, release := make(chan struct{}), make(chan struct{})
			activityTransport(t, s, func() { close(requested); <-release })
			done := make(chan int, 1)
			go func() {
				w := call(s, "GET", "/api/v1/admin/plex-activity", cookie, "", "")
				if strings.Contains(w.Body.String(), "PRIVATE") {
					t.Error("late revoke exposed names")
				}
				done <- w.Code
			}()
			select {
			case <-requested:
			case <-time.After(3 * time.Second):
				close(release)
				t.Fatal("source not reached")
			}
			if mode == "logout" {
				if w := call(s, "POST", "/api/v1/auth/logout", cookie, s.Config.PublicOrigin, ""); w.Code != 200 {
					t.Fatal(w.Body)
				}
			} else {
				if _, err := s.Store.DB.Exec(`UPDATE users SET discord_id='33333' WHERE id='11111'`); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			select {
			case code := <-done:
				if code != 401 && code != 403 {
					t.Fatalf("revoked response %d", code)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("request stuck")
			}
		})
	}
}

func TestMonitoringDisabledStillPrunesHistory(t *testing.T) {
	s := fixture(t)
	past := time.Now().Add(-8 * 24 * time.Hour).UnixMilli()
	if _, err := s.Store.DB.Exec(`INSERT INTO metric_samples(at,payload) VALUES(?, '{}')`, past); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.RunMonitoring(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	for {
		var count int
		if err := s.Store.DB.QueryRow(`SELECT COUNT(*) FROM metric_samples`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("disabled collection prevented retention")
		case <-poll.C:
		}
	}
}
