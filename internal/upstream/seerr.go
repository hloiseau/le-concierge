package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrSeerrUserNotFound = errors.New("no Seerr user for verified Plex identity")

type Request struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	Type          string `json:"type"`
	Status        string `json:"status"`
	CreatedAt     string `json:"createdAt"`
	MediaID       int64  `json:"mediaId"`
	Seasons       []int  `json:"seasons"`
	PosterURL     string `json:"posterUrl,omitempty"`
	RequesterName string `json:"requesterName,omitempty"`
}
type RequestPage struct {
	Items  []Request `json:"items"`
	Total  int       `json:"total"`
	Offset int       `json:"offset"`
	Limit  int       `json:"limit"`
}

type seerrRequest struct {
	ID          int64  `json:"id"`
	Type        string `json:"type"`
	Status      int    `json:"status"`
	Is4k        bool   `json:"is4k"`
	CreatedAt   string `json:"createdAt"`
	RequestedBy struct {
		ID           int64  `json:"id"`
		DisplayName  string `json:"displayName"`
		Username     string `json:"username"`
		PlexUsername string `json:"plexUsername"`
	} `json:"requestedBy"`
	Media struct {
		TmdbID   int64 `json:"tmdbId"`
		Status   int   `json:"status"`
		Status4k int   `json:"status4k"`
	} `json:"media"`
	Seasons []struct {
		Number int `json:"seasonNumber"`
	} `json:"seasons"`
}

type seerrRequestPage struct {
	PageInfo struct {
		Results *int `json:"results"`
	} `json:"pageInfo"`
	Results []seerrRequest `json:"results"`
}

func ValidRequestStatus(status string) bool {
	switch status {
	case "pending", "approved", "processing", "partial", "available", "declined", "error", "unknown":
		return true
	default:
		return false
	}
}

func (r seerrRequest) normalizedStatus() string {
	status := "unknown"
	switch r.Status {
	case 1:
		status = "pending"
	case 2:
		status = "approved"
	case 3:
		status = "declined"
	case 4:
		status = "error"
	}
	if r.Status != 3 && r.Status != 4 {
		mediaStatus := r.Media.Status
		if r.Is4k {
			mediaStatus = r.Media.Status4k
		}
		switch mediaStatus {
		case 3:
			status = "processing"
		case 4:
			status = "partial"
		case 5:
			status = "available"
		}
	}
	return status
}

func (c *Client) Requests(ctx context.Context, base, key string, user int64, offset int) (RequestPage, error) {
	out := RequestPage{Items: []Request{}, Offset: offset, Limit: 10}
	if user <= 0 {
		return out, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	var data seerrRequestPage
	headers := map[string]string{"X-Api-Key": key}
	endpoint := fmt.Sprintf("%s/api/v1/user/%d/requests?take=10&skip=%d", base, user, offset)
	if e := c.JSON(ctx, "GET", endpoint, headers, "", &data); e != nil {
		return out, e
	}
	if len(data.Results) > out.Limit || data.PageInfo.Results == nil || *data.PageInfo.Results < 0 {
		return RequestPage{}, ErrUnavailable
	}
	out.Total = *data.PageInfo.Results
	for _, r := range data.Results {
		// Defense in depth: never trust an upstream list to enforce ownership alone.
		if r.RequestedBy.ID != user {
			return RequestPage{}, ErrDenied
		}
		kind := r.Type
		if kind != "movie" && kind != "tv" {
			continue
		}
		out.Items = append(out.Items, c.enrichRequest(ctx, base, key, r))
	}
	return out, nil
}

// RequestsByStatus filters the complete personal list before applying pagination.
func (c *Client) RequestsByStatus(ctx context.Context, base, key string, user int64, offset int, status string) (RequestPage, error) {
	if status == "" || status == "all" {
		return c.Requests(ctx, base, key, user, offset)
	}
	out := RequestPage{Items: []Request{}, Offset: offset, Limit: 10}
	if user <= 0 {
		return out, ErrDenied
	}
	if offset < 0 || offset > 10000 || !ValidRequestStatus(status) {
		return out, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	total, skip := -1, 0
	seen := map[int64]bool{}
	selected := []seerrRequest{}
	// shortcut: at most 1,000 personal requests; use a native normalized filter if Seerr provides one.
	for page := 0; page < 10; page++ {
		var data seerrRequestPage
		endpoint := fmt.Sprintf("%s/api/v1/user/%d/requests?take=100&skip=%d", base, user, skip)
		if err := c.JSON(ctx, "GET", endpoint, map[string]string{"X-Api-Key": key}, "", &data); err != nil {
			return RequestPage{}, err
		}
		if data.PageInfo.Results == nil {
			return RequestPage{}, ErrIncomplete
		}
		count := *data.PageInfo.Results
		if count < 0 || count > 1000 || (total >= 0 && count != total) || len(data.Results) > 100 || skip+len(data.Results) > count {
			return RequestPage{}, ErrIncomplete
		}
		total = count
		for _, r := range data.Results {
			if r.RequestedBy.ID != user {
				return RequestPage{}, ErrDenied
			}
			if r.ID <= 0 || seen[r.ID] {
				return RequestPage{}, ErrIncomplete
			}
			seen[r.ID] = true
			if (r.Type != "movie" && r.Type != "tv") || r.normalizedStatus() != status {
				continue
			}
			if out.Total >= offset && len(selected) < out.Limit {
				selected = append(selected, r)
			}
			out.Total++
		}
		skip += len(data.Results)
		if skip == total {
			for _, r := range selected {
				out.Items = append(out.Items, c.enrichRequest(ctx, base, key, r))
			}
			return out, nil
		}
		if len(data.Results) == 0 {
			return RequestPage{}, ErrIncomplete
		}
	}
	return RequestPage{}, ErrIncomplete
}

func (c *Client) enrichRequest(ctx context.Context, base, key string, r seerrRequest) Request {
	title := "Film #" + strconv.FormatInt(r.Media.TmdbID, 10)
	if r.Type == "tv" {
		title = "Série #" + strconv.FormatInt(r.Media.TmdbID, 10)
	}
	var detail struct {
		Title      string `json:"title"`
		Name       string `json:"name"`
		PosterPath string `json:"posterPath"`
	}
	if e := c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/%s/%d", base, r.Type, r.Media.TmdbID), map[string]string{"X-Api-Key": key}, "", &detail); e == nil {
		if detail.Title != "" {
			title = detail.Title
		} else if detail.Name != "" {
			title = detail.Name
		}
	}
	seasons := []int{}
	for _, s := range r.Seasons {
		seasons = append(seasons, s.Number)
	}
	createdAt := ""
	if date, err := time.Parse(time.RFC3339Nano, r.CreatedAt); err == nil {
		createdAt = date.UTC().Format(time.RFC3339Nano)
	}
	requester := strings.TrimSpace(r.RequestedBy.DisplayName)
	if requester == "" {
		requester = strings.TrimSpace(r.RequestedBy.Username)
	}
	if requester == "" {
		requester = strings.TrimSpace(r.RequestedBy.PlexUsername)
	}
	return Request{ID: r.ID, Title: title, Type: r.Type, Status: r.normalizedStatus(), CreatedAt: createdAt, MediaID: r.Media.TmdbID, Seasons: seasons, PosterURL: seerrPosterURL(detail.PosterPath), RequesterName: requester}
}

var seerrPosterPath = regexp.MustCompile(`^/[A-Za-z0-9_-]+\.(jpg|jpeg|png|webp)$`)

func seerrPosterURL(path string) string {
	if len(path) > 200 || !seerrPosterPath.MatchString(path) {
		return ""
	}
	return "https://image.tmdb.org/t/p/w342" + path
}

// SeerrUser resolves only the stable identity already verified by Plex. The
// server API key must be allowed to manage users so Seerr exposes plexId.
// Never fall back to display names, email addresses or a previously saved ID.
func (c *Client) SeerrUser(ctx context.Context, base, key, plexID string) (int64, error) {
	plex, err := strconv.ParseInt(plexID, 10, 64)
	if err != nil || plex <= 0 {
		return 0, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var match int64
	total := -1
	seen := map[int64]bool{}
	for skip := 0; skip < 5000; {
		var page struct {
			PageInfo struct {
				Results *int `json:"results"`
			} `json:"pageInfo"`
			Results []struct {
				ID     int64           `json:"id"`
				PlexID json.RawMessage `json:"plexId"`
			} `json:"results"`
		}
		target := fmt.Sprintf("%s/api/v1/user?take=100&skip=%d", base, skip)
		if err := c.JSON(ctx, "GET", target, map[string]string{"X-Api-Key": key}, "", &page); err != nil {
			return 0, err
		}
		if page.PageInfo.Results == nil {
			return 0, ErrUnavailable
		}
		count := *page.PageInfo.Results
		if count < 0 || count > 5000 || (total >= 0 && count != total) || skip+len(page.Results) > count {
			return 0, ErrUnavailable
		}
		total = count
		for _, user := range page.Results {
			if user.ID <= 0 || seen[user.ID] || len(user.PlexID) == 0 {
				return 0, ErrUnavailable
			}
			seen[user.ID] = true
			if string(user.PlexID) == "null" {
				continue
			}
			var number json.Number
			if json.Unmarshal(user.PlexID, &number) != nil {
				return 0, ErrUnavailable
			}
			id, err := number.Int64()
			if err != nil || id <= 0 {
				return 0, ErrUnavailable
			}
			if id == plex {
				if match != 0 {
					return 0, ErrDenied
				}
				match = user.ID
			}
		}
		skip += len(page.Results)
		if skip == total {
			if match == 0 {
				return 0, ErrSeerrUserNotFound
			}
			return match, nil
		}
		if len(page.Results) == 0 {
			return 0, ErrUnavailable
		}
	}
	return 0, ErrUnavailable
}
