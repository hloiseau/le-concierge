package upstream

import (
	"context"
	"fmt"
	"strconv"
)

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
	var data struct {
		PageInfo struct {
			Results int `json:"results"`
		} `json:"pageInfo"`
		Results []struct {
			ID          int64  `json:"id"`
			Type        string `json:"type"`
			Status      int    `json:"status"`
			CreatedAt   string `json:"createdAt"`
			RequestedBy struct {
				ID int64 `json:"id"`
			} `json:"requestedBy"`
			Media struct {
				TmdbID int64 `json:"tmdbId"`
				Status int   `json:"status"`
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
		}
		if r.Status != 3 {
			switch r.Media.Status {
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
		out.Items = append(out.Items, Request{ID: r.ID, Title: title, Type: kind, Status: status, CreatedAt: r.CreatedAt, MediaID: r.Media.TmdbID, Seasons: seasons})
	}
	return out, nil
}
