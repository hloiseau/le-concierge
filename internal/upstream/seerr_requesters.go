package upstream

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// SeerrRequestedMedia is read only by the server's configured notification
// channel worker, never exposed through a member's personal request endpoint.
type SeerrRequestedMedia struct {
	ID, UserID, TMDBID int64
	Type               string
	PlexKeys           []string
	Seasons            []int
}

// Plex rating keys are scoped to a server. The administrative settings response
// also contains credentials, so decode only its machine identifier.
func (c *Client) SeerrPlexServer(ctx context.Context, base, key, serverID string) error {
	if serverID == "" {
		return ErrDenied
	}
	var settings struct {
		MachineID string `json:"machineId"`
	}
	if err := c.JSON(ctx, "GET", base+"/api/v1/settings/plex", map[string]string{"X-Api-Key": key}, "", &settings); err != nil {
		return err
	}
	if settings.MachineID != serverID {
		return ErrDenied
	}
	return nil
}

func (c *Client) SeerrRequestedMedia(ctx context.Context, base, key string) ([]SeerrRequestedMedia, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out := []SeerrRequestedMedia{}
	seen := map[int64]bool{}
	total := -1
	for offset := 0; offset < 5000; {
		var page struct {
			PageInfo struct {
				Results *int `json:"results"`
			} `json:"pageInfo"`
			Results []struct {
				ID          int64  `json:"id"`
				Type        string `json:"type"`
				Status      int    `json:"status"`
				Is4K        bool   `json:"is4k"`
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
		if err := c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/request?take=100&skip=%d&sort=added", base, offset), map[string]string{"X-Api-Key": key}, "", &page); err != nil {
			return nil, err
		}
		if page.PageInfo.Results == nil || *page.PageInfo.Results < 0 || *page.PageInfo.Results > 5000 || len(page.Results) > 100 || offset+len(page.Results) > *page.PageInfo.Results || (total >= 0 && total != *page.PageInfo.Results) {
			return nil, ErrIncomplete
		}
		total = *page.PageInfo.Results
		for _, row := range page.Results {
			if row.ID <= 0 || row.RequestedBy.ID <= 0 || seen[row.ID] || row.Media.TMDBID <= 0 || (row.Type != "movie" && row.Type != "tv") {
				return nil, ErrIncomplete
			}
			seen[row.ID] = true
			if row.Status != 1 && row.Status != 2 && row.Status != 5 {
				continue
			}
			item := SeerrRequestedMedia{ID: row.ID, UserID: row.RequestedBy.ID, Type: row.Type, TMDBID: row.Media.TMDBID}
			selectedKey := row.Media.RatingKey
			if row.Is4K {
				selectedKey = row.Media.RatingKey4K
			}
			for _, key := range []string{selectedKey} {
				if key == "" {
					continue
				}
				n, err := strconv.ParseInt(key, 10, 64)
				if err != nil || n <= 0 || strconv.FormatInt(n, 10) != key {
					return nil, ErrIncomplete
				}
				item.PlexKeys = append(item.PlexKeys, key)
			}
			for _, season := range row.Seasons {
				if season.Number < 0 || season.Number > 10000 {
					return nil, ErrIncomplete
				}
				item.Seasons = append(item.Seasons, season.Number)
			}
			out = append(out, item)
		}
		offset += len(page.Results)
		if offset == total {
			return out, nil
		}
		if len(page.Results) == 0 {
			return nil, ErrIncomplete
		}
	}
	return nil, ErrIncomplete
}

type SeerrRequester struct {
	ID           int64
	Name, PlexID string
	DiscordIDs   []string
}

func (c *Client) SeerrRequester(ctx context.Context, base, key string, id int64) (SeerrRequester, error) {
	if id <= 0 {
		return SeerrRequester{}, ErrDenied
	}
	headers := map[string]string{"X-Api-Key": key}
	var user struct {
		ID          int64  `json:"id"`
		PlexID      *int64 `json:"plexId"`
		DisplayName string `json:"displayName"`
	}
	if err := c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/user/%d", base, id), headers, "", &user); err != nil {
		return SeerrRequester{}, err
	}
	if user.ID != id {
		return SeerrRequester{}, ErrDenied
	}
	out := SeerrRequester{ID: id, Name: user.DisplayName}
	if user.PlexID != nil && *user.PlexID > 0 {
		out.PlexID = strconv.FormatInt(*user.PlexID, 10)
	}
	// Seerr's user-settings route is authoritative; never match a Discord pseudo.
	var settings struct {
		IDs      []string `json:"discordIds"`
		LegacyID string   `json:"discordId"`
	}
	if err := c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/user/%d/settings/notifications", base, id), headers, "", &settings); err != nil {
		return SeerrRequester{}, err
	}
	if settings.LegacyID != "" {
		settings.IDs = append(settings.IDs, settings.LegacyID)
	}
	if len(settings.IDs) > 25 {
		return SeerrRequester{}, ErrIncomplete
	}
	seen := map[string]bool{}
	for _, discordID := range settings.IDs {
		n, err := strconv.ParseUint(discordID, 10, 64)
		if err != nil || n == 0 || strconv.FormatUint(n, 10) != discordID {
			return SeerrRequester{}, ErrIncomplete
		}
		if !seen[discordID] {
			out.DiscordIDs = append(out.DiscordIDs, discordID)
			seen[discordID] = true
		}
	}
	return out, nil
}

func (c *Client) SeerrMediaTitle(ctx context.Context, base, key, kind string, id int64) (string, error) {
	if (kind != "movie" && kind != "tv") || id <= 0 {
		return "", ErrDenied
	}
	var detail struct {
		Title string `json:"title"`
		Name  string `json:"name"`
	}
	if err := c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/%s/%d", base, kind, id), map[string]string{"X-Api-Key": key}, "", &detail); err != nil {
		return "", err
	}
	if detail.Title != "" {
		return detail.Title, nil
	}
	if detail.Name != "" {
		return detail.Name, nil
	}
	return "", ErrIncomplete
}
