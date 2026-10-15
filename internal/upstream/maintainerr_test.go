package upstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const collectionFixture = `[{"id":1,"mediaServerId":"999","mediaServerType":"plex","libraryId":"2","type":"movie","isActive":true,"arrAction":0,"deleteAfterDays":7,"mediaCount":1,"media":[{"id":10,"collectionId":1,"mediaServerId":"42","addDate":"2026-10-24T22:00:00Z","isManual":false,"ruleEvaluationFailed":false}]}]`

func TestMaintainerrContractAndNormalization(t *testing.T) {
	for _, tc := range []struct {
		name, old, replacement string
		count                  int
		unknown                bool
		want                   error
	}{
		{name: "complete snapshot", count: 1},
		{name: "preview mismatch", old: `"mediaCount":1`, replacement: `"mediaCount":2`, want: ErrIncomplete},
		{name: "empty array", old: collectionFixture, replacement: `[]`},
		{name: "null is failure", old: collectionFixture, replacement: `null`, want: ErrIncomplete},
		{name: "empty body is failure", old: collectionFixture, replacement: ``, want: ErrUnavailable},
		{name: "null deadline", old: `"deleteAfterDays":7`, replacement: `"deleteAfterDays":null`, count: 1, unknown: true},
		{name: "zero deadline", old: `"deleteAfterDays":7`, replacement: `"deleteAfterDays":0`, count: 1, unknown: true},
		{name: "inactive", old: `"isActive":true`, replacement: `"isActive":false`},
		{name: "unmonitor only", old: `"arrAction":0`, replacement: `"arrAction":3`},
		{name: "do nothing", old: `"arrAction":0`, replacement: `"arrAction":4`},
		{name: "unmonitor if empty", old: `"arrAction":0`, replacement: `"arrAction":6`},
		{name: "quality change", old: `"arrAction":0`, replacement: `"arrAction":7`},
		{name: "unknown action", old: `"arrAction":0`, replacement: `"arrAction":99`},
		{name: "unmonitor and delete all", old: `"arrAction":0`, replacement: `"arrAction":1`, count: 1},
		{name: "unmonitor and delete existing", old: `"arrAction":0`, replacement: `"arrAction":2`, count: 1},
		{name: "action5 unsupported movie", old: `"arrAction":0`, replacement: `"arrAction":5`},
		{name: "different collection", old: `"collectionId":1`, replacement: `"collectionId":2`, want: ErrIncomplete},
		{name: "different media server type", old: `"mediaServerType":"plex"`, replacement: `"mediaServerType":"jellyfin"`, want: ErrDenied},
		{name: "missing active flag", old: `"isActive":true,`, replacement: ``, want: ErrIncomplete},
		{name: "unknown rule state", old: `"ruleEvaluationFailed":false`, replacement: `"ruleEvaluationFailed":null`, want: ErrIncomplete},
		{name: "unknown manual state", old: `"isManual":false,`, replacement: ``, want: ErrIncomplete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := collectionFixture
			if tc.old != "" {
				body = strings.Replace(body, tc.old, tc.replacement, 1)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Fatal("source mutated")
				}
				switch r.URL.Path {
				case "/api/app/status":
					fmt.Fprint(w, `{"status":1,"version":"3.30.1","commitTag":"fixture"}`)
				case "/api/media-server/type":
					fmt.Fprint(w, `{"type":"plex"}`)
				case "/api/media-server":
					fmt.Fprint(w, `{"machineId":"server-a"}`)
				case "/api/collections/overlay-data":
					fmt.Fprint(w, body)
				default:
					t.Errorf("unexpected route: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			snapshot, err := New().MaintainerrRemovals(context.Background(), server.URL, "server-a")
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if err != nil {
				if len(snapshot.Items) != 0 {
					t.Fatal("partial snapshot returned")
				}
				return
			}
			if len(snapshot.Items) != tc.count || time.Since(snapshot.UpdatedAt) > time.Second {
				t.Fatalf("invalid snapshot: %+v", snapshot)
			}
			if tc.count > 0 {
				item := snapshot.Items[0]
				if item.MediaID != "42" || item.CollectionID != 1 || item.LibraryID != "2" {
					t.Fatalf("identity lost: %+v", item)
				}
				if (item.EstimatedAt == nil) != tc.unknown {
					t.Fatal("unknown deadline presented as certain")
				}
				if item.EstimatedAt != nil && item.EstimatedAt.Format(time.RFC3339) != "2026-10-31T22:00:00Z" {
					t.Fatal("worker uses elapsed days across DST, not local calendar days")
				}
			}
		})
	}
}

func TestMaintainerrRejectsVersionServerMismatchAndOversizedSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		want             error
	}{
		{"new version", "/api/app/status", `{"status":1,"version":"3.31.0"}`, ErrUnsupportedVersion},
		{"failed status", "/api/app/status", `{"status":0,"version":"3.30.1"}`, ErrUnavailable},
		{"wrong server", "/api/media-server", `{"machineId":"server-b"}`, ErrDenied},
		{"wrong server type", "/api/media-server/type", `{"type":"jellyfin"}`, ErrDenied},
		{"too large", "/api/collections/overlay-data", strings.Repeat(" ", 2*1024*1024) + "[]", ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reachedSnapshot := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/collections/overlay-data" {
					reachedSnapshot = true
				}
				if r.URL.Path == tc.path {
					fmt.Fprint(w, tc.body)
					return
				}
				responses := map[string]string{"/api/app/status": `{"status":1,"version":"3.30.1"}`, "/api/media-server/type": `{"type":"plex"}`, "/api/media-server": `{"machineId":"server-a"}`, "/api/collections/overlay-data": collectionFixture}
				fmt.Fprint(w, responses[r.URL.Path])
			}))
			defer server.Close()
			if _, err := New().MaintainerrRemovals(context.Background(), server.URL, "server-a"); !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
			if tc.path != "/api/collections/overlay-data" && reachedSnapshot {
				t.Fatal("collection read despite unverified source")
			}
		})
	}
}
