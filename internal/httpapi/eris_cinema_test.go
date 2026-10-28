package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/eris"
)

func TestCinemaRepliesCreditAbsenceAndNeverSuggestsStreamingPlatforms(t *testing.T) {
	s := fixture(t)
	seed(t, s, "11111", true)
	u, _ := s.Store.User(context.Background(), "11111")
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("cinema changed state")
		}
		switch r.URL.Path {
		case "/api/v1/user":
			return response(200, `{"pageInfo":{"results":1},"results":[{"id":2,"plexId":11111}]}`), nil
		case "/api/v1/auth/me":
			return response(200, `{"id":2}`), nil
		case "/api/v1/search":
			return response(200, `{"results":[{"id":4,"mediaType":"movie","title":"Test","releaseDate":"2021-01-01"}]}`), nil
		case "/api/v1/movie/4":
			return response(200, `{"id":4,"title":"Test","overview":"Synopsis","watchProviders":["Netflix","Prime"]}`), nil
		default:
			t.Fatal("unwanted cinema endpoint", r.URL.Path)
			return nil, nil
		}
	})
	for _, focus := range []string{"", "cast", "director", "overview", "trailer"} {
		reply := s.erisCinema(context.Background(), u, eris.Plan{Action: "cinema", Query: "Test", Kind: "movie", Year: 2021, Relation: focus})
		if !reply.Narrate || !strings.Contains(reply.Content, "TMDB via Seerr") || strings.Contains(reply.Content, "Netflix") || strings.Contains(reply.Content, "Prime") {
			t.Fatal(reply)
		}
		if focus == "cast" && !strings.Contains(reply.Content, "Distribution non renseignée") {
			t.Fatal(reply)
		}
		if focus == "trailer" && !strings.Contains(reply.Content, "Aucune bande-annonce") {
			t.Fatal(reply)
		}
	}
}
