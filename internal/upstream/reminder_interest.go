package upstream

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// InterestTarget describes a personal intent, never an authorization to a file.
type InterestTarget struct {
	Type                 string
	TMDBID               int64
	Season               *int
	Requested, Following bool
	RequestID            int64
	RequestedAt          time.Time
	PlexKeys             []string
}

// RequestInterests reads the verified owner's complete request list without
// fetching titles. A changing or truncated list must not create an alert.
func (c *Client) RequestInterests(ctx context.Context, base, key string, user int64) ([]InterestTarget, error) {
	if user <= 0 {
		return nil, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	items := []InterestTarget{}
	seen := map[int64]bool{}
	total := -1
	for offset := 0; offset < 1000; {
		var page struct {
			PageInfo struct {
				Results *int `json:"results"`
			} `json:"pageInfo"`
			Results []struct {
				ID          int64     `json:"id"`
				Type        string    `json:"type"`
				Status      int       `json:"status"`
				CreatedAt   time.Time `json:"createdAt"`
				RequestedBy struct {
					ID int64 `json:"id"`
				} `json:"requestedBy"`
				Media struct {
					TMDBID      int64  `json:"tmdbId"`
					RatingKey   string `json:"ratingKey"`
					RatingKey4K string `json:"ratingKey4k"`
				} `json:"media"`
				Seasons []struct {
					Number int `json:"seasonNumber"`
				} `json:"seasons"`
			} `json:"results"`
		}
		if err := c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/user/%d/requests?take=100&skip=%d", base, user, offset), map[string]string{"X-Api-Key": key}, "", &page); err != nil {
			return nil, err
		}
		if page.PageInfo.Results == nil || *page.PageInfo.Results < 0 || *page.PageInfo.Results > 1000 || len(page.Results) > 100 || offset+len(page.Results) > *page.PageInfo.Results || (total >= 0 && total != *page.PageInfo.Results) {
			return nil, ErrIncomplete
		}
		total = *page.PageInfo.Results
		for _, row := range page.Results {
			if row.RequestedBy.ID != user {
				return nil, ErrDenied
			}
			if row.ID <= 0 || seen[row.ID] || row.Media.TMDBID <= 0 || (row.Type != "movie" && row.Type != "tv") {
				return nil, ErrIncomplete
			}
			seen[row.ID] = true
			// A completed Seerr request can still be personally unwatched in Plex.
			if row.Status != 1 && row.Status != 2 && row.Status != 5 {
				continue
			}
			keys := []string{}
			for _, key := range []string{row.Media.RatingKey, row.Media.RatingKey4K} {
				if key == "" {
					continue
				}
				n, err := strconv.ParseInt(key, 10, 64)
				if err != nil || n <= 0 || strconv.FormatInt(n, 10) != key {
					return nil, ErrIncomplete
				}
				keys = append(keys, key)
			}
			if row.Type == "movie" {
				items = append(items, InterestTarget{Type: row.Type, TMDBID: row.Media.TMDBID, Requested: true, RequestID: row.ID, RequestedAt: row.CreatedAt, PlexKeys: keys})
				continue
			}
			// A series request without an explicit season list is ambiguous.
			for _, season := range row.Seasons {
				if season.Number < 0 || season.Number > 10000 {
					return nil, ErrIncomplete
				}
				n := season.Number
				items = append(items, InterestTarget{Type: "tv", TMDBID: row.Media.TMDBID, Season: &n, Requested: true, RequestID: row.ID, RequestedAt: row.CreatedAt, PlexKeys: keys})
			}
		}
		offset += len(page.Results)
		if offset == total {
			return items, nil
		}
		if len(page.Results) == 0 {
			return nil, ErrIncomplete
		}
	}
	return nil, ErrIncomplete
}

// Plex's positive unwatched filter establishes personal state. Missing history
// or an omitted viewCount alone never does. GUIDs prevent transferring state to
// a replacement rating key. See https://developer.plex.tv/pms/ library queries.
func (c *Client) PlexUnwatched(ctx context.Context, base, serverID string, g Grant) (map[string]string, error) {
	rows, err := c.PlexUnwatchedItems(ctx, base, serverID, g)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, row := range rows {
		out[row.ID] = row.GUID
	}
	return out, nil
}

type UnwatchedItem struct {
	ID, GUID, Kind, ParentID, GrandparentID string
	AddedAt                                 time.Time
}

// AddedAt is Plex's actual source addition timestamp, not the request date or
// the time the item became shared with this user.
func (c *Client) PlexUnwatchedItems(ctx context.Context, base, serverID string, g Grant) ([]UnwatchedItem, error) {
	rows, err := c.discoveryItems(ctx, base, serverID, g, url.Values{"type": {"1,4"}, "unwatched": {"1"}, "sort": {"addedAt"}})
	if err != nil {
		return nil, err
	}
	out := []UnwatchedItem{}
	seen := map[string]bool{}
	for _, row := range rows {
		if (row.Type != "movie" && row.Type != "episode") || row.CanonicalGUID == "" || (row.ViewCount != nil && *row.ViewCount != 0) {
			return nil, ErrIncomplete
		}
		id := string(row.Key)
		if seen[id] {
			return nil, ErrIncomplete
		}
		seen[id] = true
		if row.DeletedAt == 0 {
			out = append(out, UnwatchedItem{ID: id, GUID: row.CanonicalGUID, Kind: row.Type, ParentID: string(row.ParentKey), GrandparentID: string(row.SeriesKey), AddedAt: time.Unix(row.AddedAt, 0).UTC()})
		}
	}
	return out, nil
}

// PlexEpisodeIDs enumerates only descendants of the threatened season/show.
// Callers read metadata with the same personal grant before using any item.
func (c *Client) PlexEpisodeIDs(ctx context.Context, base string, g Grant, parent PlexItem) ([]string, error) {
	path := "/library/metadata/" + parent.ID + "/children"
	if parent.Kind == "show" {
		path = "/library/metadata/" + parent.ID + "/grandchildren"
	} else if parent.Kind != "season" {
		return nil, ErrIncomplete
	}
	ids := []string{}
	seen := map[string]bool{}
	expected := -1
	for offset := 0; offset < 1000; {
		rows, total, err := c.subscriptionPlexPage(ctx, base, path, g, offset, 100)
		if err != nil {
			return nil, err
		}
		if total < 0 || total > 1000 || (expected >= 0 && total != expected) {
			return nil, ErrIncomplete
		}
		expected = total
		for _, row := range rows {
			id := string(row.ID)
			if id == "" || id == "0" || row.Kind != "episode" || seen[id] || (parent.Kind == "show" && string(row.GrandparentID) != parent.ID) || (parent.Kind == "season" && string(row.ParentID) != parent.ID) {
				return nil, ErrIncomplete
			}
			seen[id] = true
			ids = append(ids, id)
		}
		offset += len(rows)
		if offset == total {
			return ids, nil
		}
		if len(rows) == 0 {
			return nil, ErrIncomplete
		}
	}
	return nil, ErrIncomplete
}
