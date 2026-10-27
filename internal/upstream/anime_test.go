package upstream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func animeResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

type animeRoundTrip func(*http.Request) (*http.Response, error)

func (f animeRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAnimeAliasesAndRecommendationsAreReadOnlyPublicQueries(t *testing.T) {
	c := New()
	calls := 0
	c.HTTP.Transport = animeRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		var p struct {
			Query     string
			Variables map[string]any
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		if r.Method != "POST" || r.URL.String() != "https://graphql.anilist.co" || r.Header.Get("Authorization") != "" || strings.Contains(p.Query, "mutation") || strings.Contains(p.Query, "mediaListEntry") || strings.Contains(p.Query, "streamingEpisodes") {
			t.Fatal("private/write/unexpected query", p.Query)
		}
		if calls == 1 {
			if p.Variables["search"] != "L’Attaque des Titans" || p.Variables["type"] != "MANGA" {
				t.Fatal(p)
			}
			return animeResponse(`{"data":{"Page":{"media":[{"id":1,"type":"MANGA","title":{"romaji":"Shingeki no Kyojin","english":"Attack on Titan"},"synonyms":["L’Attaque des Titans"],"format":"MANGA","startDate":{"year":2009}},{"id":2,"type":"MANGA","title":{"romaji":"Other"}}]}}}`), nil
		}
		if p.Variables["id"] != float64(1) || !strings.Contains(p.Query, "mediaRecommendation") {
			t.Fatal(p)
		}
		return animeResponse(`{"data":{"Media":{"id":1,"recommendations":{"nodes":[{"mediaRecommendation":{"id":3,"type":"MANGA","title":{"romaji":"Suggestion"}}},{"mediaRecommendation":{"id":3,"type":"MANGA","title":{"romaji":"Duplicate"}}},{"mediaRecommendation":{"id":4,"type":"ANIME","title":{"romaji":"Wrong type"}}}]}}}}`), nil
	})
	r, err := c.Anime(context.Background(), "L’Attaque des Titans", "manga", "", 2009, true)
	if err != nil || calls != 2 || r.Reference == nil || r.Reference.ID != 1 || len(r.Items) != 1 || r.Items[0].ID != 3 {
		t.Fatal(r, err, calls)
	}
}

func TestAnimeAmbiguityDateAndGraphQLErrorCannotBecomeFacts(t *testing.T) {
	for _, mode := range []string{"ambiguous", "error", "empty"} {
		c := New()
		c.HTTP.Transport = animeRoundTrip(func(r *http.Request) (*http.Response, error) {
			switch mode {
			case "ambiguous":
				return animeResponse(`{"data":{"Page":{"media":[{"id":1,"type":"ANIME","title":{"english":"Hunter x Hunter"},"startDate":{"year":1999}},{"id":2,"type":"ANIME","title":{"english":"Hunter x Hunter"},"startDate":{"year":2011}}]}}}`), nil
			case "error":
				return animeResponse(`{"errors":[{"message":"Not found"}],"data":{"Page":{"media":[{"id":1,"type":"ANIME"}]}}}`), nil
			default:
				return animeResponse(`{"data":{"Page":{"media":[]}}}`), nil
			}
		})
		r, err := c.Anime(context.Background(), "Hunter x Hunter", "anime", "", 0, false)
		if mode == "error" {
			if err == nil {
				t.Fatal("GraphQL error ignored")
			}
			continue
		}
		if err != nil || !r.Ambiguous {
			t.Fatal(mode, r, err)
		}
		if mode == "ambiguous" {
			r, err = c.Anime(context.Background(), "Hunter x Hunter", "anime", "", 2011, false)
			if err != nil || r.Ambiguous || len(r.Items) != 1 || r.Items[0].ID != 2 {
				t.Fatal("year ignored", r, err)
			}
		}
	}
}
