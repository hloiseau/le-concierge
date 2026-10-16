package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

var ErrSeerrUserNotFound = errors.New("no Seerr user for verified Plex identity")

type Request struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	MediaID   int64  `json:"mediaId"`
	Seasons   []int  `json:"seasons"`
}
type RequestPage struct {
	Items  []Request `json:"items"`
	Total  int       `json:"total"`
	Offset int       `json:"offset"`
	Limit  int       `json:"limit"`
}

func (c *Client) Requests(ctx context.Context, base, key string, user int64, offset int) (RequestPage, error) {
	out := RequestPage{Items: []Request{}, Offset: offset, Limit: 10}
	if user <= 0 {
		return out, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	var data struct {
		PageInfo struct {
			Results int `json:"results"`
		} `json:"pageInfo"`
		Results []struct {
			ID          int64  `json:"id"`
			Type        string `json:"type"`
			Status      int    `json:"status"`
			Is4k        bool   `json:"is4k"`
			CreatedAt   string `json:"createdAt"`
			RequestedBy struct {
				ID int64 `json:"id"`
			} `json:"requestedBy"`
			Media struct {
				TmdbID   int64 `json:"tmdbId"`
				Status   int   `json:"status"`
				Status4k int   `json:"status4k"`
			} `json:"media"`
			Seasons []struct {
				Number int `json:"seasonNumber"`
			} `json:"seasons"`
		} `json:"results"`
	}
	headers := map[string]string{"X-Api-Key": key}
	endpoint := fmt.Sprintf("%s/api/v1/user/%d/requests?take=10&skip=%d", base, user, offset)
	if e := c.JSON(ctx, "GET", endpoint, headers, "", &data); e != nil {
		return out, e
	}
	if len(data.Results) > out.Limit || data.PageInfo.Results < 0 {
		return RequestPage{}, ErrUnavailable
	}
	out.Total = data.PageInfo.Results
	for _, r := range data.Results {
		// Defense in depth: never trust an upstream list to enforce ownership alone.
		if r.RequestedBy.ID != user {
			return RequestPage{}, ErrDenied
		}
		kind := r.Type
		if kind != "movie" && kind != "tv" {
			continue
		}
		title := "Film #" + strconv.FormatInt(r.Media.TmdbID, 10)
		if kind == "tv" {
			title = "Série #" + strconv.FormatInt(r.Media.TmdbID, 10)
		}
		var detail struct {
			Title string `json:"title"`
			Name  string `json:"name"`
		}
		if e := c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/%s/%d", base, kind, r.Media.TmdbID), headers, "", &detail); e == nil {
			if detail.Title != "" {
				title = detail.Title
			} else if detail.Name != "" {
				title = detail.Name
			}
		}
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
		seasons := []int{}
		for _, s := range r.Seasons {
			seasons = append(seasons, s.Number)
		}
		createdAt := ""
		if date, err := time.Parse(time.RFC3339Nano, r.CreatedAt); err == nil {
			createdAt = date.UTC().Format(time.RFC3339Nano)
		}
		out.Items = append(out.Items, Request{ID: r.ID, Title: title, Type: kind, Status: status, CreatedAt: createdAt, MediaID: r.Media.TmdbID, Seasons: seasons})
	}
	return out, nil
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
