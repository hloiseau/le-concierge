package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

type diagnosticFixture struct {
	kind                     string
	request                  map[string]any
	items                    []map[string]any
	queue, history           []map[string]any
	queueTotal, historyTotal int
	wrongServer              bool
	status                   int
	reads                    int
}

func newDiagnosticFixture(t *testing.T, kind string) (*Client, DiagnosticSources, *diagnosticFixture) {
	t.Helper()
	f := &diagnosticFixture{kind: kind, status: 200, request: map[string]any{"id": 7, "type": kind, "status": 2, "serverId": 0, "is4k": false, "createdAt": "2026-10-15T10:00:00Z", "media": map[string]any{"tmdbId": 12, "tvdbId": 45, "mediaType": kind}, "seasons": []any{map[string]any{"seasonNumber": 2}}}, items: []map[string]any{{"id": 9, "tmdbId": 12, "tvdbId": 45, "hasFile": false}}, queue: []map[string]any{}, history: []map[string]any{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.reads++
		if r.Method != "GET" {
			t.Fatal("diagnostic wrote to remote")
		}
		if strings.HasPrefix(r.URL.Path, "/seerr/") && r.Header.Get("X-Api-Key") != "seerr-secret" {
			t.Fatal("wrong Seerr key")
		}
		if strings.HasPrefix(r.URL.Path, "/arr/") && r.Header.Get("X-Api-Key") != "arr-secret" {
			t.Fatal("wrong Arr key")
		}
		if f.status != 200 {
			w.WriteHeader(f.status)
			fmt.Fprint(w, "private failure /nas secret")
			return
		}
		var data any
		switch r.URL.Path {
		case "/seerr/api/v1/request/7":
			data = f.request
		case "/seerr/api/v1/settings/radarr", "/seerr/api/v1/settings/sonarr":
			u, _ := url.Parse("http://" + r.Host)
			port, _ := strconv.Atoi(u.Port())
			host := u.Hostname()
			if f.wrongServer {
				host = "other-instance.test"
			}
			data = []any{map[string]any{"id": 0, "hostname": host, "port": port, "baseUrl": "/arr", "useSsl": false, "isDefault": true, "is4k": false, "apiKey": "ignored-admin-key", "activeDirectory": "/private/nas"}}
		case "/arr/api/v3/system/status":
			source := "Radarr"
			if f.kind == "tv" {
				source = "Sonarr"
			}
			data = map[string]any{"appName": source, "version": "4.0.20.3014", "startupPath": "/private/nas", "instanceName": "private-host"}
		case "/arr/api/v3/movie":
			if r.URL.Query().Get("tmdbId") != "12" {
				t.Fatal("not correlated by TMDB")
			}
			data = f.items
		case "/arr/api/v3/series":
			if r.URL.Query().Get("tvdbId") != "45" {
				t.Fatal("not correlated by TVDB")
			}
			data = f.items
		case "/arr/api/v3/queue", "/arr/api/v3/history":
			filter := "movieIds"
			if f.kind == "tv" {
				filter = "seriesIds"
			}
			if r.URL.Query().Get(filter) != "9" {
				t.Fatal("unscoped queue/history")
			}
			size := 50
			records := f.queue
			total := len(records) + f.queueTotal
			if strings.HasSuffix(r.URL.Path, "history") {
				size = 30
				records = f.history
				total = len(records) + f.historyTotal
				if r.URL.Query().Get("sortKey") != "date" || r.URL.Query().Get("sortDirection") != "descending" {
					t.Fatal("history not latest first")
				}
			}
			data = map[string]any{"page": 1, "pageSize": size, "totalRecords": total, "records": records}
		case "/plex/identity":
			data = map[string]any{"MediaContainer": map[string]any{"machineIdentifier": "server-a"}}
		case "/plex/library/metadata/42":
			if r.Header.Get("X-Plex-Token") != "personal-plex" {
				t.Fatal("wrong Plex identity")
			}
			data = map[string]any{"MediaContainer": map[string]any{"size": 1, "Metadata": []any{map[string]any{"ratingKey": "42", "type": "movie", "Guid": []any{map[string]any{"id": "tmdb://12"}}}}}}
		default:
			t.Fatalf("unexpected route %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(data)
	}))
	t.Cleanup(server.Close)
	return New(), DiagnosticSources{SeerrURL: server.URL + "/seerr", SeerrKey: "seerr-secret", RadarrURL: server.URL + "/arr", RadarrKey: "arr-secret", SonarrURL: server.URL + "/arr", SonarrKey: "arr-secret", PlexURL: server.URL + "/plex", PlexServerID: "server-a"}, f
}
func TestDiagnosticManualImportIsReadOnlyCorrelatedAndRedacted(t *testing.T) {
	c, sources, f := newDiagnosticFixture(t, "movie")
	f.queue = []map[string]any{{"movieId": 9, "trackedDownloadState": "importBlocked", "downloadId": "private-download-id", "indexer": "private-tracker", "outputPath": "/private/nas", "statusMessages": []any{map[string]any{"title": "private-release-title", "messages": []string{"Found matching movie via grab history, but release was matched to movie by ID. Manual Import required. /private/nas https://tracker.test/?apikey=secret"}}}}}
	f.history = []map[string]any{{"movieId": 9, "eventType": "grabbed", "date": "2026-10-15T12:00:00Z", "downloadId": "private-download-id", "sourceTitle": "private-release-title", "data": map[string]any{"indexer": "private-tracker"}}}
	result, err := c.DiagnoseRequest(context.Background(), sources, Grant{}, 7)
	if err != nil || result.Stage != "manual_import" || result.GuideURL != manualImportGuide || f.reads != 6 || len(result.Evidence) != 3 {
		t.Fatalf("diagnostic %+v %v reads=%d", result, err, f.reads)
	}
	encoded, _ := json.Marshal(result)
	for _, secret := range []string{"private-", "/nas", "tracker.test", "apikey", "seerr-secret", "arr-secret", "ignored-admin-key"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("exposed %s", secret)
		}
	}
	if !strings.Contains(result.Summary, "matched to movie by ID") {
		t.Fatal("useful diagnostic lost")
	}
}
func TestDiagnosticFailureCategoriesAndBounds(t *testing.T) {
	for _, tc := range []struct{ message, stage string }{
		{"Not enough free space on /private/nas (1234 required)", "space"},
		{"Access to the path 'C:\\private\\secret' is denied", "permissions"},
		{"Permission denied: /mnt/media", "permissions"},
		{"Failed to rename /nas/old to /nas/new", "rename_import"},
		{"Unknown failure with token=secret and tracker PrivateTracker", "importing"},
	} {
		t.Run(tc.stage+tc.message[:4], func(t *testing.T) {
			c, sources, f := newDiagnosticFixture(t, "movie")
			f.queue = []map[string]any{{"movieId": 9, "trackedDownloadState": "importBlocked", "errorMessage": tc.message}}
			f.queueTotal = 100
			f.historyTotal = 100
			result, err := c.DiagnoseRequest(context.Background(), sources, Grant{}, 7)
			if err != nil || result.Stage != tc.stage || !result.QueueTruncated || !result.HistoryTruncated || f.reads != 6 {
				t.Fatalf("%+v %v", result, err)
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "PrivateTracker") || strings.Contains(string(encoded), "/nas") {
				t.Fatal("raw error escaped")
			}
		})
	}
}
func TestDiagnosticRejectsMismatchedIdentityAndScope(t *testing.T) {
	for _, state := range []string{"request", "media", "queue", "history", "instance", "outage"} {
		t.Run(state, func(t *testing.T) {
			c, sources, f := newDiagnosticFixture(t, "movie")
			switch state {
			case "request":
				f.request["id"] = 8
			case "media":
				f.items[0]["tmdbId"] = 13
			case "queue":
				f.queue = []map[string]any{{"movieId": 10, "errorMessage": "private"}}
			case "history":
				f.history = []map[string]any{{"movieId": 10}}
			case "instance":
				f.wrongServer = true
			case "outage":
				f.status = 503
			}
			if _, err := c.DiagnoseRequest(context.Background(), sources, Grant{}, 7); err == nil {
				t.Fatal("unverified source accepted")
			}
			if state == "instance" && f.reads != 2 {
				t.Fatalf("wrong instance reached: %d", f.reads)
			}
		})
	}
}
func TestDiagnosticTVKeepsRequestedSeasonsAndHistoricalCaveats(t *testing.T) {
	c, sources, f := newDiagnosticFixture(t, "tv")
	f.queue = []map[string]any{{"seriesId": 9, "seasonNumber": 1, "trackedDownloadState": "importBlocked", "errorMessage": "Not enough free space"}, {"seriesId": 9, "seasonNumber": 2, "trackedDownloadState": "downloading"}}
	f.history = []map[string]any{{"seriesId": 9, "eventType": "downloadFailed", "date": "2026-10-15T12:00:00Z", "episode": map[string]any{"seasonNumber": 1}}, {"seriesId": 9, "eventType": "downloadFailed", "date": "2026-10-14T12:00:00Z", "episode": map[string]any{"seasonNumber": 2}}, {"seriesId": 9, "eventType": "grabbed", "date": "2026-10-15T12:00:00Z", "episode": map[string]any{"seasonNumber": 2}}}
	result, err := c.DiagnoseRequest(context.Background(), sources, Grant{}, 7)
	if err != nil || result.Source != "sonarr" || result.Stage != "downloading" || len(result.Evidence) != 2 {
		t.Fatalf("wrong season/history %+v %v", result, err)
	}
}
func TestDiagnosticSeparatesFilesFromPlexIndexation(t *testing.T) {
	c, sources, f := newDiagnosticFixture(t, "movie")
	f.items[0]["hasFile"] = true
	result, err := c.DiagnoseRequest(context.Background(), sources, Grant{}, 7)
	if err != nil || result.Stage != "plex_indexing" || result.PlexStatus != "not_verified" {
		t.Fatalf("file presumed indexed %+v %v", result, err)
	}
	f.request["media"].(map[string]any)["ratingKey"] = "42"
	result, err = c.DiagnoseRequest(context.Background(), sources, Grant{ServerToken: "personal-plex"}, 7)
	if err != nil || result.PlexStatus != "indexed" || !strings.Contains(result.Summary, "lecture et la complétude") {
		t.Fatalf("indexation %+v %v", result, err)
	}
}
func TestDiagnosticAbsentPendingAndDeclinedAreExplicit(t *testing.T) {
	for _, tc := range []struct {
		status int
		stage  string
	}{{1, "approval"}, {3, "declined"}, {2, "not_found"}} {
		c, sources, f := newDiagnosticFixture(t, "movie")
		f.request["status"] = tc.status
		f.items = nil
		result, err := c.DiagnoseRequest(context.Background(), sources, Grant{}, 7)
		if err != nil || result.Stage != tc.stage {
			t.Fatalf("%+v %v", result, err)
		}
	}
}

func TestDiagnosticFailedAndCompletedRequestsStillReadArr(t *testing.T) {
	for _, status := range []int{4, 5} {
		c, sources, f := newDiagnosticFixture(t, "movie")
		f.request["status"] = status
		f.queue = []map[string]any{{"movieId": 9, "trackedDownloadState": "importBlocked", "errorMessage": "Not enough free space"}}
		result, err := c.DiagnoseRequest(context.Background(), sources, Grant{}, 7)
		if err != nil || result.Stage != "space" || f.reads != 6 {
			t.Fatalf("status %d: %+v %v reads=%d", status, result, err, f.reads)
		}
	}
}

func TestDiagnosticDefaultInstanceMustMatchRequestedQuality(t *testing.T) {
	c, sources, f := newDiagnosticFixture(t, "movie")
	delete(f.request, "serverId")
	f.request["is4k"] = true
	if _, err := c.DiagnoseRequest(context.Background(), sources, Grant{}, 7); err == nil || f.reads != 2 {
		t.Fatalf("HD instance used for 4K request: %v reads=%d", err, f.reads)
	}
	f.reads = 0
	f.request["is4k"] = false
	if _, err := c.DiagnoseRequest(context.Background(), sources, Grant{}, 7); err != nil || f.reads != 6 {
		t.Fatalf("default HD instance rejected: %v reads=%d", err, f.reads)
	}
}
