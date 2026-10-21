package upstream

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

type MediaFilter struct {
	Query, Kind, Genre string
	MaxMinutes         int
	Unwatched          bool
}

type MediaSuggestion struct {
	Media
	Summary string
	Minutes int
	Genres  []string
}

// AssistantMedia only returns titles freshly verified playable with this member's grant.
func (c *Client) AssistantMedia(ctx context.Context, base, serverID string, g Grant, f MediaFilter) ([]MediaSuggestion, error) {
	if (f.Kind != "" && f.Kind != "movie" && f.Kind != "show") || len(f.Query) > 800 || f.MaxMinutes < 0 || f.MaxMinutes > 600 {
		return nil, ErrDenied
	}
	kinds := "1,2"
	if f.Kind == "movie" {
		kinds = "1"
	}
	if f.Kind == "show" {
		kinds = "2"
	}
	query := url.Values{"type": {kinds}, "sort": {"titleSort"}, "skipRefresh": {"1"}}
	if f.Query != "" {
		query.Set("title", f.Query)
	}
	items, err := c.discoveryItems(ctx, base, serverID, g, query)
	if err != nil {
		return nil, err
	}
	candidates := []discoveryMetadata{}
	seen := map[string]bool{}
	for _, m := range items {
		if !validPlexLibraryID(string(m.Key)) || (m.Type != "movie" && m.Type != "show") || (f.Kind != "" && m.Type != f.Kind) {
			return nil, ErrIncomplete
		}
		if seen[string(m.Key)] || m.DeletedAt > 0 || (m.Type == "show" && m.LeafCount != nil && *m.LeafCount == 0) {
			continue
		}
		seen[string(m.Key)] = true
		if f.Query != "" && !strings.Contains(strings.ToLower(m.Title), strings.ToLower(f.Query)) {
			continue
		}
		if f.MaxMinutes > 0 && (m.Duration <= 0 || m.Duration >= int64(f.MaxMinutes)*60000) {
			continue
		}
		// Plex omits viewCount for unplayed items; this means 'not marked watched', not proof of never watching elsewhere.
		if f.Unwatched && ((m.ViewCount != nil && *m.ViewCount > 0) || m.ViewedLeafCount > 0) {
			continue
		}
		genreMatch := f.Genre == ""
		for _, genre := range m.Genres {
			genreMatch = genreMatch || genreName(genre.Tag) == genreName(f.Genre)
		}
		if !genreMatch {
			continue
		}
		candidates = append(candidates, m)
	}
	result := []MediaSuggestion{}
	reads := discoveryPlaybackReads
	for len(candidates) > 0 && len(result) < 3 {
		index := 0
		if f.Query == "" {
			n, e := rand.Int(rand.Reader, big.NewInt(int64(len(candidates))))
			if e != nil {
				return nil, ErrUnavailable
			}
			index = int(n.Int64())
		}
		m := candidates[index]
		candidates = append(candidates[:index], candidates[index+1:]...)
		ok, e := c.discoveryPlayable(ctx, base, g, m, &reads)
		if e != nil {
			return nil, e
		}
		if !ok {
			continue
		}
		item := MediaSuggestion{Media: discoveryMedia(m.recentMetadata, serverID), Summary: m.Summary, Minutes: int(m.Duration / 60000)}
		for _, g := range m.Genres {
			if len(item.Genres) < 3 {
				item.Genres = append(item.Genres, g.Tag)
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func genreName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	aliases := map[string]string{"science-fiction": "science fiction", "sci-fi": "science fiction", "comédie": "comedy", "drame": "drama", "horreur": "horror", "épouvante": "horror", "policier": "crime", "aventure": "adventure", "fantastique": "fantasy", "documentaire": "documentary", "familial": "family", "guerre": "war", "histoire": "history", "mystère": "mystery", "musique": "music"}
	if v, ok := aliases[s]; ok {
		return v
	}
	return s
}

type RequestCandidate struct {
	ID    int64  `json:"id"`
	Kind  string `json:"mediaType"`
	Title string `json:"title"`
	Year  string `json:"year"`
}

func (c *Client) seerrMember(ctx context.Context, base, key string, user int64) (map[string]string, error) {
	if user <= 0 || key == "" {
		return nil, ErrDenied
	}
	headers := map[string]string{"X-Api-Key": key, "X-Api-User": strconv.FormatInt(user, 10), "Content-Type": "application/json"}
	var identity struct {
		ID int64 `json:"id"`
	}
	if err := c.JSON(ctx, "GET", base+"/api/v1/auth/me", headers, "", &identity); err != nil {
		return nil, err
	}
	if identity.ID != user {
		return nil, ErrDenied
	}
	return headers, nil
}

// SearchRequestCandidates returns public catalogue fields only, never mediaInfo.requests.
func (c *Client) SearchRequestCandidates(ctx context.Context, base, key string, user int64, query, kind string) ([]RequestCandidate, error) {
	if len([]rune(query)) < 2 || len([]rune(query)) > 200 || strings.ContainsFunc(query, unicode.IsControl) || (kind != "" && kind != "movie" && kind != "show") {
		return nil, ErrDenied
	}
	headers, err := c.seerrMember(ctx, base, key, user)
	if err != nil {
		return nil, err
	}
	var data struct {
		Results []struct {
			ID      int64  `json:"id"`
			Type    string `json:"mediaType"`
			Title   string `json:"title"`
			Name    string `json:"name"`
			Release string `json:"releaseDate"`
			Air     string `json:"firstAirDate"`
		} `json:"results"`
	}
	// Seerr's query validator rejects form-encoded spaces (+); use percent encoding.
	params := strings.ReplaceAll(url.Values{"query": {query}, "page": {"1"}, "language": {"fr-FR"}}.Encode(), "+", "%20")
	if err = c.JSON(ctx, "GET", base+"/api/v1/search?"+params, headers, "", &data); err != nil {
		return nil, err
	}
	if len(data.Results) > 20 {
		return nil, ErrIncomplete
	}
	result := []RequestCandidate{}
	for _, m := range data.Results {
		if m.ID <= 0 || (m.Type != "movie" && m.Type != "tv") || (kind == "movie" && m.Type != "movie") || (kind == "show" && m.Type != "tv") {
			continue
		}
		title, date := m.Title, m.Release
		if m.Type == "tv" {
			title, date = m.Name, m.Air
		}
		if strings.TrimSpace(title) == "" {
			return nil, ErrIncomplete
		}
		year := ""
		if len(date) >= 4 {
			if n, e := strconv.Atoi(date[:4]); e == nil && n > 1800 && n < 3000 {
				year = date[:4]
			}
		}
		result = append(result, RequestCandidate{ID: m.ID, Kind: m.Type, Title: title, Year: year})
		if len(result) == 3 {
			break
		}
	}
	return result, nil
}

var ErrRequestConflict = errors.New("request already exists")
var ErrRequestUncertain = errors.New("request outcome unknown")

// CreateMediaRequest is called only after an owner-bound confirmation. Never retry an ambiguous POST.
// Seerr enforces the impersonated member's permissions and quota: no userId/ignoreQuota overrides.
func (c *Client) CreateMediaRequest(ctx context.Context, base, key string, user int64, m RequestCandidate, seasons []int) (int64, error) {
	if m.ID <= 0 || (m.Kind != "movie" && m.Kind != "tv") || (m.Kind == "movie" && len(seasons) != 0) || (m.Kind == "tv" && (len(seasons) == 0 || len(seasons) > 20)) {
		return 0, ErrDenied
	}
	seen := map[int]bool{}
	for _, n := range seasons {
		if n < 1 || n > 1000 || seen[n] {
			return 0, ErrDenied
		}
		seen[n] = true
	}
	headers, err := c.seerrMember(ctx, base, key, user)
	if err != nil {
		return 0, err
	}
	body := map[string]any{"mediaType": m.Kind, "mediaId": m.ID, "is4k": false}
	if m.Kind == "tv" {
		body["seasons"] = seasons
	}
	encoded, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/api/v1/request", strings.NewReader(string(encoded)))
	if err != nil {
		return 0, ErrUnavailable
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Jar = nil
	resp, err := client.Do(req)
	if err != nil {
		return 0, ErrRequestUncertain
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return 0, ErrDenied
	}
	if resp.StatusCode == 409 || resp.StatusCode == 202 {
		return 0, ErrRequestConflict
	}
	if resp.StatusCode != 201 {
		return 0, ErrRequestUncertain
	}
	var result seerrRequest
	if json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 32*1024)).Decode(&result) != nil || result.ID <= 0 || result.RequestedBy.ID != user || result.Media.TmdbID != m.ID || result.Type != m.Kind {
		return 0, ErrRequestUncertain
	}
	return result.ID, nil
}

// FindPersonalRequests scans a bounded complete personal list when filtering by title.
func (c *Client) FindPersonalRequests(ctx context.Context, base, key string, user int64, query string) ([]Request, error) {
	if query == "" {
		page, e := c.Requests(ctx, base, key, user, 0)
		return page.Items, e
	}
	result := []Request{}
	total := -1
	for offset := 0; offset < 1000; offset += 10 {
		page, e := c.Requests(ctx, base, key, user, offset)
		if e != nil {
			return nil, e
		}
		if page.Total > 1000 || (total >= 0 && page.Total != total) {
			return nil, ErrIncomplete
		}
		total = page.Total
		for _, m := range page.Items {
			if strings.Contains(strings.ToLower(m.Title), strings.ToLower(query)) {
				result = append(result, m)
			}
		}
		if offset+len(page.Items) >= total {
			return result, nil
		}
		if len(page.Items) != 10 {
			return nil, ErrIncomplete
		}
	}
	return nil, fmt.Errorf("%w: request limit", ErrIncomplete)
}
