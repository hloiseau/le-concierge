package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func TestCheckSourcesProducesOnlyAggregateDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/app/status":
			fmt.Fprint(w, `{"status":1,"version":"3.30.1","commitTag":"fixture"}`)
		case "/api/media-server/type":
			fmt.Fprint(w, `{"type":"plex"}`)
		case "/api/media-server":
			fmt.Fprint(w, `{"machineId":"private-server"}`)
		case "/api/rules":
			fmt.Fprint(w, `[{"id":5,"collectionId":1,"isActive":true,"notifications":[{"token":"private-notification"}]}]`)
		case "/api/rules/exclusion":
			fmt.Fprint(w, `[]`)
		case "/api/collections/overlay-data":
			fmt.Fprint(w, `[{"id":1,"title":"private-title","mediaServerType":"plex","libraryId":"1","isActive":true,"arrAction":0,"type":"movie","deleteAfterDays":null,"mediaCount":1,"media":[{"id":2,"collectionId":1,"mediaServerId":"42","addDate":"2026-10-01T00:00:00Z","isManual":false,"ruleEvaluationFailed":false}]}]`)
		case "/api/v2":
			if r.Header.Get("X-Api-Key") != "private-key" {
				t.Error("missing key")
			}
			data := `{"pms_identifier":"private-server","pms_ip":"private-host"}`
			if r.URL.Query().Get("cmd") == "get_tautulli_info" {
				data = `{"tautulli_version":"v2.18.2","tautulli_platform_device_name":"private-host"}`
			}
			fmt.Fprintf(w, `{"response":{"result":"success","data":%s}}`, data)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	var out bytes.Buffer
	c := config.Config{MaintainerrURL: server.URL, TautulliURL: server.URL, TautulliAPIKey: "private-key", PlexServerID: "private-server"}
	if err := checkSources(context.Background(), c, upstream.New(), "", &out); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"destructiveMemberships":1`, `"unknownDeadlines":1`, `"remindersEnabled":false`, `"version":"v2.18.2"`} {
		if !strings.Contains(out.String(), expected) {
			t.Errorf("missing %s", expected)
		}
	}
	if strings.Contains(out.String(), "private-") || strings.Contains(out.String(), server.URL) {
		t.Fatalf("personal details leaked: %s", &out)
	}
	out.Reset()
	if err := checkSources(context.Background(), config.Config{}, upstream.New(), "", &out); err == nil || out.Len() != 0 {
		t.Fatal("unconfigured sources accepted")
	}
}
