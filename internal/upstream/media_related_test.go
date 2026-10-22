package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRelatedMoviesUsesVerifiedDirectorCreditsNotCastOrWriting(t *testing.T) {
	for _, relation := range []string{"director", "similar"} {
		t.Run(relation, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.Header.Get("X-Api-User") != "2" {
					t.Error("recommendation changed state or identity")
				}
				var body string
				switch r.URL.Path {
				case "/api/v1/auth/me":
					body = `{"id":2}`
				case "/api/v1/search":
					body = `{"results":[{"id":4638,"mediaType":"movie","title":"Hot Fuzz","releaseDate":"2007-01-01"}]}`
				case "/api/v1/movie/4638":
					body = `{"id":4638,"credits":{"crew":[{"id":1,"name":"Edgar Wright","job":"Director"},{"id":2,"name":"Writer","job":"Writer"}]}}`
				case "/api/v1/person/1/combined_credits":
					body = `{"cast":[{"id":44,"title":"Cameo"}],"crew":[{"id":4638,"mediaType":"movie","title":"Hot Fuzz","job":"Director","voteCount":9999},{"id":2,"mediaType":"movie","title":"Shaun","releaseDate":"2004-01-01","job":"Director","voteCount":100,"mediaInfo":{"status":5,"requests":[{"requestedBy":{"username":"secret"}}]}},{"id":3,"mediaType":"movie","title":"Baby Driver","releaseDate":"2017-01-01","job":"Director","voteCount":900},{"id":4,"mediaType":"movie","title":"Only written","job":"Writer"},{"id":3,"mediaType":"movie","title":"Duplicate","job":"Director"},{"id":5,"mediaType":"tv","title":"Series","job":"Director"}]}`
				case "/api/v1/movie/4638/recommendations":
					body = `{"results":[{"id":3,"mediaType":"movie","title":"Baby Driver","releaseDate":"2017-01-01"},{"id":4638,"mediaType":"movie","title":"Hot Fuzz"}]}`
				default:
					t.Errorf("unexpected route: %s", r.URL.Path)
					w.WriteHeader(500)
					return
				}
				_, _ = w.Write([]byte(body))
			}))
			defer s.Close()
			result, err := New().RelatedMovies(context.Background(), s.URL, "key", 2, "hotfuzz", relation)
			if err != nil || len(result.Items) == 0 || result.Items[0].Title != "Baby Driver" || result.Items[0].Year != "2017" {
				t.Fatalf("bad recommendation: %+v %v", result, err)
			}
			if relation == "director" && (result.Director != "Edgar Wright" || len(result.Items) != 2) {
				t.Fatalf("credits lost: %+v", result)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "Only written") {
				t.Fatal("leaked or invented recommendation")
			}
		})
	}
}

func TestRelatedMoviesRefusesAmbiguousReference(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			_, _ = w.Write([]byte(`{"id":2}`))
		case "/api/v1/search":
			_, _ = w.Write([]byte(`{"results":[{"id":1,"mediaType":"movie","title":"Dune"},{"id":2,"mediaType":"movie","title":"Dune"}]}`))
		default:
			t.Error("guessed reference")
			w.WriteHeader(500)
		}
	}))
	defer s.Close()
	_, err := New().RelatedMovies(context.Background(), s.URL, "key", 2, "Dune", "director")
	if !errors.Is(err, ErrMediaReference) {
		t.Fatal(err)
	}
}

func TestRelatedPlexUsesTMDBIdentityAndPersonalPlayback(t *testing.T) {
	server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/library/metadata/") {
			if r.URL.Path != "/library/metadata/1" {
				t.Fatal("selected wrong or watched film")
			}
			discoveryPlayableResponse(w, r, "movie")
			return
		}
		items := recentMovies(3)
		items[0]["Guid"] = []map[string]string{{"id": "tmdb://3"}}
		items[1]["Guid"] = []map[string]string{{"id": "tmdb://4"}}
		items[1]["viewCount"] = 1
		items[2]["Guid"] = []map[string]string{{"id": "tmdb://5"}}
		discoveryPage(w, 0, len(items), items)
	})
	result, err := New().RelatedPlayable(context.Background(), server.URL, "server-a", Grant{ServerToken: "user-a"}, MovieRecommendations{Items: []RelatedMovie{{RequestCandidate: RequestCandidate{ID: 4, Title: "Watched"}}, {RequestCandidate: RequestCandidate{ID: 3, Title: "Different translated title"}}}}, true)
	if err != nil || len(result) != 1 || result[0].ID != 3 {
		t.Fatal(result, err)
	}
}

func TestRelatedPlexUsesMultipleSharedGenresWhenCataloguePageMissesLibrary(t *testing.T) {
	server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/library/metadata/") {
			if r.URL.Path != "/library/metadata/2" {
				t.Fatal("recommended reference or unrelated comedy")
			}
			discoveryPlayableResponse(w, r, "movie")
			return
		}
		items := recentMovies(3)
		for i, m := range items {
			m["Guid"] = []map[string]string{{"id": []string{"tmdb://4638", "tmdb://290250", "tmdb://9"}[i]}}
			m["Genre"] = []map[string]string{{"tag": "Comédie"}}
		}
		items[0]["Genre"] = []map[string]string{{"tag": "Comédie"}, {"tag": "Crime"}}
		items[1]["Genre"] = []map[string]string{{"tag": "Comédie"}, {"tag": "Crime"}}
		discoveryPage(w, 0, 3, items)
	})
	recs := MovieRecommendations{Reference: RequestCandidate{ID: 4638}, Genres: []string{"Comedy", "Crime", "Action"}}
	result, err := New().RelatedPlayable(context.Background(), server.URL, "server-a", Grant{ServerToken: "user-a"}, recs, false)
	if err != nil || len(result) != 1 || result[0].ID != 290250 || len(result[0].SharedGenres) != 2 {
		t.Fatal(result, err)
	}
}
