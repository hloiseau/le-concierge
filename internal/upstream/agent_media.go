package upstream

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Agent candidates expose only catalogue identity, never another member's data.
func (c *Client) LookupMedia(ctx context.Context, base, key string, user int64, query, kind string, year int) ([]RequestCandidate, error) {
	items, err := c.searchRequestCandidates(ctx, base, key, user, query, kind, 20)
	if err != nil {
		return nil, err
	}
	result := []RequestCandidate{}
	for _, item := range items {
		if year == 0 || item.Year == strconv.Itoa(year) {
			result = append(result, item)
		}
	}
	return result, nil
}

func (c *Client) MediaByID(ctx context.Context, base, key string, user, id int64, kind string) (RequestCandidate, error) {
	if id <= 0 || id > 2147483647 || (kind != "movie" && kind != "show") {
		return RequestCandidate{}, ErrDenied
	}
	headers, err := c.seerrMember(ctx, base, key, user)
	if err != nil {
		return RequestCandidate{}, err
	}
	mediaType := kind
	if kind == "show" {
		mediaType = "tv"
	}
	var detail struct {
		ID           int64
		Title, Name  string
		ReleaseDate  string `json:"releaseDate"`
		FirstAirDate string `json:"firstAirDate"`
	}
	if err = c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/%s/%d?language=fr-FR", base, mediaType, id), headers, "", &detail); err != nil {
		return RequestCandidate{}, err
	}
	title, date := detail.Title, detail.ReleaseDate
	if kind == "show" {
		title, date = detail.Name, detail.FirstAirDate
	}
	if detail.ID != id || strings.TrimSpace(title) == "" {
		return RequestCandidate{}, ErrIncomplete
	}
	year := ""
	if len(date) >= 4 {
		if n, e := strconv.Atoi(date[:4]); e == nil && n > 1800 && n < 3000 {
			year = date[:4]
		}
	}
	return RequestCandidate{ID: id, Kind: mediaType, Title: cleanActivity(title, 140), Year: year}, nil
}

func (c *Client) MovieCollection(ctx context.Context, base, key string, user, id int64) ([]RequestCandidate, error) {
	if id <= 0 || id > 2147483647 {
		return nil, ErrDenied
	}
	headers, err := c.seerrMember(ctx, base, key, user)
	if err != nil {
		return nil, err
	}
	var detail struct {
		ID         int64
		Collection *struct{ ID int64 }
	}
	if err = c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/movie/%d?language=fr-FR", base, id), headers, "", &detail); err != nil {
		return nil, err
	}
	if detail.ID != id {
		return nil, ErrIncomplete
	}
	if detail.Collection == nil {
		return []RequestCandidate{}, nil
	}
	if detail.Collection.ID <= 0 {
		return nil, ErrIncomplete
	}
	var collection struct {
		ID    int64
		Parts []struct {
			ID          int64
			Title       string
			ReleaseDate string `json:"releaseDate"`
		}
	}
	if err = c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/collection/%d?language=fr-FR", base, detail.Collection.ID), headers, "", &collection); err != nil {
		return nil, err
	}
	if collection.ID != detail.Collection.ID || len(collection.Parts) > 30 {
		return nil, ErrIncomplete
	}
	items := []RequestCandidate{}
	seen := map[int64]bool{}
	for _, part := range collection.Parts {
		if part.ID <= 0 || strings.TrimSpace(part.Title) == "" || seen[part.ID] {
			return nil, ErrIncomplete
		}
		seen[part.ID] = true
		year := ""
		if len(part.ReleaseDate) >= 4 {
			if n, e := strconv.Atoi(part.ReleaseDate[:4]); e == nil && n > 1800 && n < 3000 {
				year = part.ReleaseDate[:4]
			}
		}
		items = append(items, RequestCandidate{ID: part.ID, Kind: "movie", Title: cleanActivity(part.Title, 140), Year: year})
	}
	return items, nil
}
