package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/eris"
)

func TestErisRejectsOldChoicesAndUntrackedCallsBeforeAnyUpstream(t *testing.T) {
	s := fixture(t)
	s.Config.BotAPISecret = strings.Repeat("s", 32)
	s.Upstream.HTTP.Transport = transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("invalid call reached upstream"); return nil, nil })
	for _, body := range []string{
		`{"plan":{"action":"lookup","query":"Madagascar"}}`,
		`{"plan":{"action":"lookup","query":"Madagascar"},"callId":"tool-0"}`,
		`{"plan":{"action":"lookup","query":"Madagascar"},"callId":"tool-100"}`,
		`{"plan":{"action":"prepare","query":"Madagascar"},"callId":"tool-1"}`,
		`{"choice":"eris:choose:old","callId":"tool-1"}`,
	} {
		r := httptest.NewRequest("POST", "/internal/eris", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+s.Config.BotAPISecret)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("old or untracked call accepted", w.Code, w.Body)
		}
	}
}

func TestNativeMediaToolsReadResolveAndRequestWithoutChoices(t *testing.T) {
	s := fixture(t)
	s.Config.BotAPISecret = strings.Repeat("s", 32)
	seed(t, s, "11111", true)
	posts := 0
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v1/user":
			return response(200, `{"pageInfo":{"results":1},"results":[{"id":2,"plexId":11111}]}`), nil
		case "/api/v1/auth/me":
			return response(200, `{"id":2}`), nil
		case "/api/v1/search":
			return response(200, `{"results":[{"id":953,"mediaType":"movie","title":"Madagascar","releaseDate":"2005-01-01"},{"id":270946,"mediaType":"movie","title":"Les Pingouins de Madagascar","releaseDate":"2014-01-01"}]}`), nil
		case "/api/v1/movie/953":
			return response(200, `{"id":953,"title":"Madagascar","releaseDate":"2005-01-01","collection":{"id":14740}}`), nil
		case "/api/v1/collection/14740":
			return response(200, `{"id":14740,"parts":[{"id":953,"title":"Madagascar","releaseDate":"2005-01-01"},{"id":10527,"title":"Madagascar 2","releaseDate":"2008-01-01"},{"id":80321,"title":"Madagascar 3","releaseDate":"2012-01-01"}]}`), nil
		case "/api/v1/request/411":
			return response(200, `{"id":411,"type":"movie","requestedBy":{"id":2},"media":{"tmdbId":953}}`), nil
		case "/api/v1/request":
			posts++
			if r.Method != "POST" || r.Header.Get("X-Api-User") != "2" {
				t.Error("request escaped the member's identity")
			}
			var body struct {
				MediaID int64 `json:"mediaId"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.MediaID != 953 {
				t.Fatal("wrong film", body)
			}
			return response(201, `{"id":411,"type":"movie","requestedBy":{"id":2},"media":{"tmdbId":953}}`), nil
		default:
			t.Error("unexpected route", r.URL.Path)
			return response(500, `{}`), nil
		}
	})
	body := strings.Replace(erisSlash, `"value":"Arrival"`, `"value":"Demande Madagascar"`, 1)
	plans := []eris.Plan{{Action: "lookup", Query: "Madagascar", Kind: "movie"}, {Action: "collection", MediaID: 953, Kind: "movie"}, {Action: "request", MediaID: 953, Kind: "movie"}, {Action: "request", MediaID: 953, Kind: "movie"}}
	for index, plan := range plans {
		in := signedErisTool(t, s, plan, body)
		in.CallID = fmt.Sprintf("tool-%d", index+1)
		w := callErisTool(s, in)
		if w.Code != 200 {
			t.Fatalf("tool %d: %d %s", index, w.Code, w.Body)
		}
		var reply eris.ToolReply
		if json.Unmarshal(w.Body.Bytes(), &reply) != nil {
			t.Fatal("human choice state created", w.Body)
		}
		if index == 0 && !strings.Contains(reply.Content, `"id":953`) {
			t.Fatal("missing catalogue identity", reply)
		}
		if index == 1 && (!strings.Contains(reply.Content, `"id":80321`) || strings.Contains(reply.Content, "Pingouins")) {
			t.Fatal("wrong collection", reply)
		}
		if index == 2 && !strings.Contains(reply.Content, `"requestId":411`) {
			t.Fatal("missing receipt", reply)
		}
		if again := callErisTool(s, in); again.Code != 403 {
			t.Fatal("same native call was replayed", again.Code)
		}
	}
	if posts != 1 {
		t.Fatalf("expected one POST, got %d", posts)
	}
}
