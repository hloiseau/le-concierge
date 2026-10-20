package departures

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func TestPersonalDeparturesAboveOneHundredMetadata(t *testing.T) {
	const count = 120
	members, history := []any{}, []any{}
	for id := 1; id <= count; id++ {
		members = append(members, map[string]any{"id": id, "collectionId": 1, "mediaServerId": strconv.Itoa(id), "addDate": time.Now().UTC().Format(time.RFC3339), "isManual": false, "ruleEvaluationFailed": false})
		history = append(history, map[string]any{"row_id": id, "user_id": 123, "rating_key": id, "media_type": "movie", "guid": fmt.Sprintf("plex://movie/%d", id), "percent_complete": 50, "watched_status": .5, "live": 0})
	}
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body any
		switch r.URL.Path {
		case "/identity":
			body = map[string]any{"MediaContainer": map[string]any{"machineIdentifier": "server-a"}}
		case "/api/app/status":
			body = map[string]any{"status": 1, "version": "3.30.1"}
		case "/api/media-server/type":
			body = map[string]any{"type": "plex"}
		case "/api/media-server":
			body = map[string]any{"machineId": "server-a"}
		case "/api/collections/overlay-data":
			body = []any{map[string]any{"id": 1, "mediaServerType": "plex", "libraryId": "2", "type": "movie", "isActive": true, "arrAction": 0, "deleteAfterDays": 7, "mediaCount": count, "media": members}}
		case "/api/rules":
			body = []any{map[string]any{"id": 5, "collectionId": 1, "isActive": true}}
		case "/api/rules/exclusion":
			body = []any{}
		case "/api/v2":
			var data any
			switch r.URL.Query().Get("cmd") {
			case "get_tautulli_info":
				data = map[string]any{"tautulli_version": "v2.18.2"}
			case "get_server_info":
				data = map[string]any{"pms_identifier": "server-a"}
			case "get_users":
				data = []any{map[string]any{"user_id": 123, "is_active": 1, "keep_history": 1}}
			case "get_history":
				start, _ := strconv.Atoi(r.URL.Query().Get("start"))
				length, _ := strconv.Atoi(r.URL.Query().Get("length"))
				data = map[string]any{"recordsFiltered": count, "data": history[start:min(start+length, count)]}
			default:
				t.Errorf("unexpected history command")
			}
			body = map[string]any{"response": map[string]any{"result": "success", "data": data}}
		default:
			reads++
			if !strings.HasPrefix(r.URL.Path, "/library/metadata/") || r.Header.Get("X-Plex-Token") != "personal-token" {
				t.Errorf("unexpected or non-personal read")
			}
			items := []any{}
			for _, id := range strings.Split(strings.TrimPrefix(r.URL.Path, "/library/metadata/"), ",") {
				items = append(items, map[string]any{"ratingKey": id, "title": "Fixture", "type": "movie", "guid": "plex://movie/" + id})
			}
			body = map[string]any{"MediaContainer": map[string]any{"size": len(items), "librarySectionID": "2", "Metadata": items}}
		}
		json.NewEncoder(w).Encode(body)
	}))
	defer server.Close()
	cfg := config.Config{PlexURL: server.URL, PlexServerID: "server-a", MaintainerrURL: server.URL, TautulliURL: server.URL, TautulliAPIKey: "fixture"}
	page, err := New(upstream.New()).List(context.Background(), cfg, upstream.Grant{PlexID: "123", ServerToken: "personal-token", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil || len(page.Items) != count || reads != 3 {
		t.Fatalf("large complete snapshot rejected: count=%d reads=%d err=%v", len(page.Items), reads, err)
	}
}
