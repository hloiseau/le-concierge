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

func TestCinemaDisambiguatesYearAndKindAndRetainsOnlyPublicMetadata(t *testing.T) {
	for _, test := range []struct {
		kind string
		year int
		want string
	}{
		{"movie", 2021, "movie"}, {"show", 2021, "tv"}, {"", 2021, "ambiguous"}, {"movie", 0, "ambiguous"}, {"movie", 2000, "ambiguous"},
	} {
		t.Run(test.kind+test.want, func(t *testing.T) {
			reads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.Header.Get("X-Api-User") != "2" {
					t.Fatal("wrong member or media write")
				}
				switch r.URL.Path {
				case "/api/v1/auth/me":
					_, _ = w.Write([]byte(`{"id":2}`))
				case "/api/v1/search":
					_, _ = w.Write([]byte(`{"results":[{"id":1,"mediaType":"movie","title":"Dune","releaseDate":"1984-01-01"},{"id":2,"mediaType":"movie","title":"Dune","releaseDate":"2021-01-01"},{"id":3,"mediaType":"tv","name":"Dune","firstAirDate":"2021-01-01"}]}`))
				case "/api/v1/movie/2", "/api/v1/tv/3":
					reads++
					if r.URL.Query().Get("language") != "fr-FR" {
						t.Error("lost French metadata")
					}
					id := 2
					if strings.Contains(r.URL.Path, "/tv/") {
						id = 3
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "title": "Dune", "name": "Dune", "overview": "Synopsis vérifié", "runtime": 155, "episodeRunTime": []int{45}, "createdBy": []any{map[string]string{"name": "Créatrice"}}, "credits": map[string]any{"cast": []any{map[string]string{"name": "Acteur", "character": "Personnage"}}, "crew": []any{map[string]string{"name": "Réalisateur", "job": "Director"}, map[string]string{"name": "Scénariste", "job": "Writer"}}}, "relatedVideos": []any{map[string]string{"site": "Evil", "key": "AAAAAAAAAAA", "type": "Trailer"}, map[string]string{"site": "YouTube", "key": "not-a-valid/key", "type": "Trailer"}, map[string]string{"site": "YouTube", "key": "jNQXAC9IVRw", "type": "Trailer", "url": "http://internal/secret"}}, "watchProviders": []string{"FORBIDDEN-PROVIDER"}, "mediaInfo": map[string]string{"requests": "PRIVATE-OWNER"}})
				default:
					t.Fatal("unexpected endpoint", r.URL.Path)
				}
			}))
			defer server.Close()
			info, err := New().Cinema(context.Background(), server.URL, "key", 2, "Dune", test.kind, test.year)
			if test.want == "ambiguous" {
				if !errors.Is(err, ErrMediaReference) || reads != 0 {
					t.Fatal(info, err, reads)
				}
				return
			}
			if err != nil || reads != 1 || info.Reference.Kind != test.want || info.Overview != "Synopsis vérifié" || len(info.Cast) != 1 || info.TrailerURL != "https://www.youtube.com/watch?v=jNQXAC9IVRw" {
				t.Fatal(info, err, reads)
			}
			if (test.want == "movie" && (info.Creators[0] != "Réalisateur" || info.Minutes != 155)) || (test.want == "tv" && (info.Creators[0] != "Créatrice" || info.Minutes != 45)) {
				t.Fatal("incorrect credit or runtime", info)
			}
			raw, _ := json.Marshal(info)
			for _, forbidden := range []string{"FORBIDDEN-PROVIDER", "PRIVATE-OWNER", "internal/secret", "Scénariste"} {
				if strings.Contains(string(raw), forbidden) {
					t.Fatal("unwanted field retained", forbidden)
				}
			}
		})
	}
}

func TestCinemaMissingCreditTrailerAndWrongDetailIdentity(t *testing.T) {
	for _, id := range []int{4, 99} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/auth/me":
				_, _ = w.Write([]byte(`{"id":2}`))
			case "/api/v1/search":
				// The wanted title is beyond the legacy three request candidates.
				_, _ = w.Write([]byte(`{"results":[{"id":1,"mediaType":"movie","title":"Other 1"},{"id":2,"mediaType":"movie","title":"Other 2"},{"id":3,"mediaType":"movie","title":"Other 3"},{"id":4,"mediaType":"movie","title":"Wanted"}]}`))
			case "/api/v1/movie/4":
				_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "title": "Wanted"})
			default:
				t.Fatal("unexpected detail", r.URL.Path)
			}
		}))
		info, err := New().Cinema(context.Background(), server.URL, "key", 2, "Wanted", "movie", 0)
		server.Close()
		if id == 99 && !errors.Is(err, ErrIncomplete) {
			t.Fatal("wrong identity accepted", err)
		}
		if id == 4 && (err != nil || len(info.Cast) != 0 || len(info.Creators) != 0 || info.TrailerURL != "") {
			t.Fatal(info, err)
		}
	}
}
