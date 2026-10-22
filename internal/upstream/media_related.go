package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

var ErrMediaReference = errors.New("movie reference missing or ambiguous")

type RelatedMovie struct {
	RequestCandidate
	Overview  string `json:"overview"`
	Job       string `json:"job"`
	Votes     int    `json:"voteCount"`
	MediaInfo *struct {
		Status   int `json:"status"`
		Status4K int `json:"status4k"`
	} `json:"mediaInfo"`
	ReleaseDate  string   `json:"releaseDate"`
	SharedGenres []string `json:"-"`
}

type MovieRecommendations struct {
	Reference RequestCandidate
	Director  string
	Genres    []string
	Items     []RelatedMovie
}

func movieTitle(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

// RelatedMovies resolves the reference and its credits through Seerr, never model knowledge.
// Only public catalogue metadata is retained; no request owners or watch history.
func (c *Client) RelatedMovies(ctx context.Context, base, key string, user int64, query, relation string) (MovieRecommendations, error) {
	out := MovieRecommendations{}
	if relation != "similar" && relation != "director" {
		return out, ErrDenied
	}
	matches, err := c.SearchRequestCandidates(ctx, base, key, user, query, "movie")
	if err != nil {
		return out, err
	}
	for _, m := range matches {
		if movieTitle(m.Title) == movieTitle(query) {
			if out.Reference.ID != 0 {
				return MovieRecommendations{}, ErrMediaReference
			}
			out.Reference = m
		}
	}
	if out.Reference.ID == 0 {
		return out, ErrMediaReference
	}
	headers, err := c.seerrMember(ctx, base, key, user)
	if err != nil {
		return out, err
	}
	if relation == "similar" {
		var detail struct {
			ID     int64 `json:"id"`
			Genres []struct {
				Name string `json:"name"`
			} `json:"genres"`
		}
		if err = c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/movie/%d?language=fr-FR", base, out.Reference.ID), headers, "", &detail); err != nil {
			return out, err
		}
		if detail.ID != out.Reference.ID {
			return out, ErrIncomplete
		}
		for _, g := range detail.Genres {
			out.Genres = append(out.Genres, g.Name)
		}
		var page struct {
			Results []RelatedMovie `json:"results"`
		}
		err = c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/movie/%d/recommendations?page=1&language=fr-FR", base, out.Reference.ID), headers, "", &page)
		if err != nil {
			return out, err
		}
		if len(page.Results) > 20 {
			return out, ErrIncomplete
		}
		out.Items = page.Results
	} else {
		var detail struct {
			ID      int64 `json:"id"`
			Credits struct {
				Crew []struct {
					ID   int64  `json:"id"`
					Name string `json:"name"`
					Job  string `json:"job"`
				} `json:"crew"`
			} `json:"credits"`
		}
		if err = c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/movie/%d?language=fr-FR", base, out.Reference.ID), headers, "", &detail); err != nil {
			return out, err
		}
		if detail.ID != out.Reference.ID {
			return out, ErrIncomplete
		}
		directors := map[int64]bool{}
		for _, person := range detail.Credits.Crew {
			if person.Job != "Director" || person.ID <= 0 || directors[person.ID] {
				continue
			}
			if len(directors) >= 3 {
				return out, ErrIncomplete
			}
			directors[person.ID] = true
			if out.Director != "" {
				out.Director += " / "
			}
			out.Director += person.Name
			var credits struct {
				Crew []RelatedMovie `json:"crew"`
			}
			if err = c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/person/%d/combined_credits?language=fr-FR", base, person.ID), headers, "", &credits); err != nil {
				return out, err
			}
			if len(credits.Crew) > 1000 {
				return out, ErrIncomplete
			}
			for _, m := range credits.Crew {
				if m.Job == "Director" {
					out.Items = append(out.Items, m)
				}
			}
		}
		if len(directors) == 0 {
			return out, ErrIncomplete
		}
		sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].Votes > out.Items[j].Votes })
	}
	items := []RelatedMovie{}
	seen := map[int64]bool{out.Reference.ID: true}
	for _, m := range out.Items {
		if m.Kind != "movie" || m.ID <= 0 || seen[m.ID] || strings.TrimSpace(m.Title) == "" {
			continue
		}
		seen[m.ID] = true
		if len(m.ReleaseDate) >= 4 {
			if n, e := strconv.Atoi(m.ReleaseDate[:4]); e == nil && n > 1800 && n < 3000 {
				m.Year = m.ReleaseDate[:4]
			}
		}
		items = append(items, m)
		if len(items) == 20 {
			break
		}
	}
	out.Items = items
	return out, nil
}

// RelatedPlayable intersects verified recommendations with the member's Plex library.
func (c *Client) RelatedPlayable(ctx context.Context, base, serverID string, g Grant, recommendations MovieRecommendations, unwatched bool) ([]RelatedMovie, error) {
	items, err := c.discoveryItems(ctx, base, serverID, g, url.Values{"type": {"1"}, "sort": {"titleSort"}, "includeGuids": {"1"}, "skipRefresh": {"1"}})
	if err != nil {
		return nil, err
	}
	result := []RelatedMovie{}
	reads := discoveryPlaybackReads
	movies := append([]RelatedMovie(nil), recommendations.Items...)
	// The first catalogue page can miss this small Plex library. Use at least two
	// verified shared genres as an explicit fallback, never a random comedy.
	if len(recommendations.Genres) >= 2 {
		fallback := []RelatedMovie{}
		for _, m := range items {
			var guids []struct {
				ID string `json:"id"`
			}
			if len(m.ExternalGUIDs) > 0 && json.Unmarshal(m.ExternalGUIDs, &guids) != nil {
				return nil, ErrIncomplete
			}
			id := int64(0)
			for _, guid := range guids {
				if strings.HasPrefix(guid.ID, "tmdb://") {
					id, _ = strconv.ParseInt(strings.TrimPrefix(guid.ID, "tmdb://"), 10, 64)
				}
			}
			if id <= 0 || id == recommendations.Reference.ID {
				continue
			}
			shared := []string{}
			seenGenres := map[string]bool{}
			for _, tag := range m.Genres {
				for _, genre := range recommendations.Genres {
					if genreName(tag.Tag) == genreName(genre) && !seenGenres[genreName(genre)] {
						shared = append(shared, tag.Tag)
						seenGenres[genreName(genre)] = true
					}
				}
			}
			if len(shared) >= 2 {
				fallback = append(fallback, RelatedMovie{RequestCandidate: RequestCandidate{ID: id, Kind: "movie", Title: m.Title, Year: strconv.Itoa(m.Year)}, Overview: m.Summary, SharedGenres: shared})
			}
		}
		score := func(m RelatedMovie) int {
			n := 0
			for i, genre := range recommendations.Genres {
				for _, shared := range m.SharedGenres {
					if genreName(shared) == genreName(genre) {
						n += len(recommendations.Genres) - i
					}
				}
			}
			return n
		}
		sort.SliceStable(fallback, func(i, j int) bool { return score(fallback[i]) > score(fallback[j]) })
		movies = append(movies, fallback...)
	}
	seenMovies := map[int64]bool{}
	for _, candidate := range movies {
		if seenMovies[candidate.ID] {
			continue
		}
		seenMovies[candidate.ID] = true
		for _, m := range items {
			if m.Type != "movie" || m.DeletedAt > 0 || !validPlexLibraryID(string(m.Key)) {
				continue
			}
			var guids []struct {
				ID string `json:"id"`
			}
			matched := false
			if len(m.ExternalGUIDs) > 0 && json.Unmarshal(m.ExternalGUIDs, &guids) != nil {
				return nil, ErrIncomplete
			}
			for _, guid := range guids {
				if guid.ID == "tmdb://"+strconv.FormatInt(candidate.ID, 10) {
					matched = true
				}
			}
			if !matched {
				continue
			}
			if unwatched && m.ViewCount != nil && *m.ViewCount > 0 {
				continue
			}
			ok, e := c.discoveryPlayable(ctx, base, g, m, &reads)
			if e != nil {
				return nil, e
			}
			if ok {
				result = append(result, candidate)
				break
			}
		}
		if len(result) == 3 {
			break
		}
	}
	return result, nil
}
