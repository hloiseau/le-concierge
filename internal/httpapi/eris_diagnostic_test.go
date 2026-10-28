package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/eris"
)

func TestErisDiagnosticAllowsMemberAndStopsForeignRequestBeforeArr(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		s := fixture(t)
		seed(t, s, "11111", true)
		u, _ := s.Store.User(context.Background(), "11111")
		s.Config.AdminIDs = nil
		s.Config.RadarrURL = "http://radarr.test:7878"
		s.Config.RadarrAPIKey = "admin-secret"
		arrReads := 0
		s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != "GET" {
				t.Fatal("diagnostic performed write")
			}
			if r.URL.Host == "radarr.test:7878" {
				arrReads++
			}
			switch r.URL.Path {
			case "/api/v1/user":
				return response(200, `{"pageInfo":{"results":1},"results":[{"id":2,"plexId":11111}]}`), nil
			case "/api/v1/request/7":
				owner := "2"
				if foreign {
					owner = "3"
				}
				return response(200, `{"id":7,"type":"movie","status":2,"serverId":0,"requestedBy":{"id":`+owner+`},"createdAt":"2026-10-15T10:00:00Z","media":{"tmdbId":12}}`), nil
			case "/api/v1/settings/radarr":
				return response(200, `[{"id":0,"hostname":"radarr.test","port":7878,"isDefault":true,"is4k":false}]`), nil
			case "/api/v3/system/status":
				return response(200, `{"appName":"Radarr","version":"6.4.4.10685"}`), nil
			case "/api/v3/movie":
				return response(200, `[{"id":9,"tmdbId":12}]`), nil
			case "/api/v3/queue":
				return response(200, `{"page":1,"pageSize":50,"totalRecords":1,"records":[{"movieId":9,"status":"downloading","size":1000,"sizeleft":250,"outputPath":"/private/nas","indexer":"secret-indexer"}]}`), nil
			case "/api/v3/history":
				return response(200, `{"page":1,"pageSize":30,"totalRecords":0,"records":[]}`), nil
			default:
				t.Fatal("unexpected diagnostic endpoint", r.URL.Path)
				return nil, nil
			}
		})
		reply := s.erisDiagnostic(context.Background(), u, s.grants[u.ID], eris.Plan{Action: "diagnostic", RequestID: 7})
		if foreign {
			if arrReads != 0 || !strings.Contains(reply.Content, "pas pu vérifier") || reply.Narrate || reply.Diagnostic != nil {
				t.Fatal("foreign request disclosed", arrReads, reply)
			}
		} else if arrReads == 0 || !strings.Contains(reply.Content, "75 %") || !strings.Contains(reply.Content, "indexation") || !reply.Narrate || reply.Diagnostic == nil || !reply.Diagnostic.Valid() || reply.Diagnostic.ProgressPercent == nil || *reply.Diagnostic.ProgressPercent != 75 {
			t.Fatal("member diagnosis missing", reply)
		}
		if strings.Contains(reply.Content, "private") || strings.Contains(reply.Content, "secret") || strings.Contains(reply.Content, "6.4.4") || strings.Contains(reply.Content, "admin-secret") {
			t.Fatal("technical private fields exposed", reply)
		}
		raw, _ := json.Marshal(reply.Diagnostic)
		if strings.Contains(string(raw), "private") || strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "outputPath") || strings.Contains(string(raw), "indexer") {
			t.Fatal("private evidence reached narrator", string(raw))
		}
	}
}
