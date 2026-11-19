package httpapi

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/store"
)

func TestErisSeerrRetryAfterUncertainWriteAndRestart(t *testing.T) {
	s := fixture(t)
	path := filepath.Join(t.TempDir(), "concierge.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s.Store = db
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
			return response(200, `{"results":[{"id":123,"mediaType":"movie","title":"Veilleurs Orbitaux","releaseDate":"2024-01-01"}]}`), nil
		case "/api/v1/request":
			if r.Method != "POST" {
				t.Errorf("unexpected method: %s", r.Method)
			}
			posts++
			return nil, context.DeadlineExceeded
		case "/api/v1/user/2/requests":
			return response(200, `{"pageInfo":{"results":1},"results":[{"id":9,"type":"movie","is4k":false,"requestedBy":{"id":2},"media":{"tmdbId":123}}]}`), nil
		case "/api/v1/movie/123":
			return response(200, `{"id":123,"title":"Veilleurs Orbitaux"}`), nil
		default:
			t.Errorf("unexpected route: %s", r.URL.Path)
			return response(500, "{}"), nil
		}
	})
	body := strings.Replace(erisSlash, `"value":"Arrival"`, `"value":"Demande le film Veilleurs Orbitaux dans Seerr"`, 1)
	plan := eris.Plan{Action: "request", MediaID: 123, Kind: "movie"}
	w := callErisTool(s, signedErisTool(t, s, plan, body))
	if w.Code != 200 || posts != 1 || !strings.Contains(w.Body.String(), "outcome_unknown_do_not_retry") {
		t.Fatalf("first result did not represent an uncertain write: status=%d posts=%d", w.Code, posts)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s.Store = reopened
	second := strings.Replace(body, "99999", "99998", 1)
	w = callErisTool(s, signedErisTool(t, s, plan, second))
	if w.Code != 200 || posts != 1 || !strings.Contains(w.Body.String(), "already_created") {
		t.Fatalf("retry should read the matching receipt without a POST: status=%d posts=%d result=%s", w.Code, posts, w.Body)
	}
}
