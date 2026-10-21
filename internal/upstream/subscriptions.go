package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SubscriptionMedia contains no requesting user's identity or private request data.
type SubscriptionMedia struct {
	Title           string
	Available       bool
	URL             string
	RequestedBySelf bool
}

// CheckSubscription uses Seerr's user context for catalogue access and the user's
// Plex token for availability. A global Seerr status never proves playback access.
func (c *Client) CheckSubscription(ctx context.Context, seerrURL, key string, user int64, plexURL, serverID string, grant Grant, kind string, tmdbID int64, season *int) (SubscriptionMedia, error) {
	if user <= 0 || tmdbID <= 0 || (kind != "movie" && kind != "tv") || (season != nil && (kind != "tv" || *season < 0 || *season > 10000)) || grant.ServerToken == "" || (!grant.ExpiresAt.IsZero() && !grant.Active(time.Now())) {
		return SubscriptionMedia{}, ErrDenied
	}
	var detail struct {
		ID      int64  `json:"id"`
		Title   string `json:"title"`
		Name    string `json:"name"`
		Seasons []struct {
			Number int `json:"seasonNumber"`
		} `json:"seasons"`
		Media *struct {
			TMDBID      int64  `json:"tmdbId"`
			Type        string `json:"mediaType"`
			Status      int    `json:"status"`
			Status4K    int    `json:"status4k"`
			RatingKey   string `json:"ratingKey"`
			RatingKey4K string `json:"ratingKey4k"`
			Requests    []struct {
				Status      int `json:"status"`
				RequestedBy struct {
					ID int64 `json:"id"`
				} `json:"requestedBy"`
				Seasons []struct {
					Number int `json:"seasonNumber"`
				} `json:"seasons"`
			} `json:"requests"`
		} `json:"mediaInfo"`
	}
	headers := map[string]string{"X-Api-Key": key, "X-Api-User": strconv.FormatInt(user, 10)}
	var identity struct {
		ID int64 `json:"id"`
	}
	if err := c.JSON(ctx, "GET", seerrURL+"/api/v1/auth/me", headers, "", &identity); err != nil {
		return SubscriptionMedia{}, err
	}
	if identity.ID != user {
		return SubscriptionMedia{}, ErrDenied
	}
	if err := c.JSON(ctx, "GET", fmt.Sprintf("%s/api/v1/%s/%d", seerrURL, kind, tmdbID), headers, "", &detail); err != nil {
		return SubscriptionMedia{}, err
	}
	if detail.ID != tmdbID || detail.Media == nil || detail.Media.TMDBID != tmdbID || detail.Media.Type != kind {
		return SubscriptionMedia{}, ErrNotFound
	}
	out := SubscriptionMedia{Title: strings.TrimSpace(detail.Title)}
	if kind == "tv" {
		out.Title = strings.TrimSpace(detail.Name)
	}
	if out.Title == "" {
		return SubscriptionMedia{}, ErrIncomplete
	}
	if season != nil {
		found := false
		for _, candidate := range detail.Seasons {
			if candidate.Number == *season {
				found = true
			}
		}
		if !found {
			return SubscriptionMedia{}, ErrNotFound
		}
	}
	requested := false
	for _, request := range detail.Media.Requests {
		if request.Status != 1 && request.Status != 2 && request.Status != 5 {
			continue
		}
		matches := season == nil
		for _, candidate := range request.Seasons {
			if season != nil && candidate.Number == *season {
				matches = true
			}
		}
		if matches {
			if request.RequestedBy.ID <= 0 {
				return SubscriptionMedia{}, ErrIncomplete
			}
			// Completion still identifies Seerr's recipient, but is not a new pending request.
			requested = requested || request.Status != 5
			out.RequestedBySelf = out.RequestedBySelf || request.RequestedBy.ID == user
		}
	}
	if detail.Media.RatingKey != "" || detail.Media.RatingKey4K != "" {
		if err := c.PlexIdentity(ctx, plexURL, serverID); err != nil {
			return SubscriptionMedia{}, err
		}
		accessibleEdition := false
		seen := map[string]bool{}
		for _, ratingKey := range []string{detail.Media.RatingKey, detail.Media.RatingKey4K} {
			if ratingKey == "" || seen[ratingKey] {
				continue
			}
			seen[ratingKey] = true
			available, err := c.subscriptionPlexAvailable(ctx, plexURL, grant, ratingKey, kind, tmdbID, season)
			if errors.Is(err, ErrDenied) {
				continue
			}
			if err != nil {
				return SubscriptionMedia{}, err
			}
			accessibleEdition = true
			if available {
				out.Available = true
				out.URL = "https://app.plex.tv/desktop/#!/server/" + url.PathEscape(serverID) + "/details?key=" + url.QueryEscape("/library/metadata/"+ratingKey)
				return out, nil
			}
		}
		// Never turn inaccessible/already imported content into a new future alert.
		if !accessibleEdition || kind == "movie" || detail.Media.Status == 5 || detail.Media.Status4K == 5 {
			return SubscriptionMedia{}, ErrDenied
		}
	} else if detail.Media.Status == 4 || detail.Media.Status == 5 || detail.Media.Status4K == 4 || detail.Media.Status4K == 5 {
		return SubscriptionMedia{}, ErrIncomplete
	}
	if !requested {
		return SubscriptionMedia{}, ErrNotFound
	}
	return out, nil
}

type subscriptionPlexMetadata struct {
	ID            tautulliID `json:"ratingKey"`
	ParentID      tautulliID `json:"parentRatingKey"`
	GrandparentID tautulliID `json:"grandparentRatingKey"`
	Kind          string     `json:"type"`
	Index         *int       `json:"index"`
	GUID          string     `json:"guid"`
	GUIDs         []struct {
		ID string `json:"id"`
	} `json:"Guid"`
	Media []struct {
		Parts []struct {
			Key        string          `json:"key"`
			Exists     json.RawMessage `json:"exists"`
			Accessible json.RawMessage `json:"accessible"`
		} `json:"Part"`
	} `json:"Media"`
}

func (c *Client) subscriptionPlexPage(ctx context.Context, base, path string, grant Grant, start, limit int) ([]subscriptionPlexMetadata, int, error) {
	headers := plexHeaders(grant.ClientID, grant.ServerToken)
	headers["X-Plex-Container-Start"] = strconv.Itoa(start)
	headers["X-Plex-Container-Size"] = strconv.Itoa(limit)
	var result struct {
		Container *struct {
			Size     *int                       `json:"size"`
			Total    *int                       `json:"totalSize"`
			Offset   *int                       `json:"offset"`
			Metadata []subscriptionPlexMetadata `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	query := url.Values{"skipRefresh": {"1"}}
	// File checks are documented for item metadata, not descendant listings.
	if strings.Count(path, "/") == 3 {
		query.Set("checkFiles", "1")
		query.Set("checkFileAvailability", "1")
		query.Set("includeGuids", "1")
	}
	err := c.JSON(ctx, "GET", base+path+"?"+query.Encode(), headers, "", &result)
	if errors.Is(err, ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	r := result.Container
	if r == nil || r.Size == nil || *r.Size != len(r.Metadata) || len(r.Metadata) > limit || (r.Offset != nil && *r.Offset != start) {
		return nil, 0, ErrIncomplete
	}
	total := -1
	if r.Total != nil {
		total = *r.Total
	}
	if r.Total != nil && (total < start+len(r.Metadata) || (len(r.Metadata) == 0 && total > start)) {
		return nil, 0, ErrIncomplete
	}
	return r.Metadata, total, nil
}

func (c *Client) subscriptionPlexAvailable(ctx context.Context, base string, grant Grant, ratingKey, kind string, tmdbID int64, season *int) (bool, error) {
	n, err := strconv.ParseInt(ratingKey, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != ratingKey {
		return false, ErrIncomplete
	}
	items, _, err := c.subscriptionPlexPage(ctx, base, "/library/metadata/"+ratingKey, grant, 0, 1)
	if err != nil {
		return false, err
	}
	if len(items) == 0 {
		return false, ErrDenied
	}
	item := items[0]
	expectedKind := kind
	if kind == "tv" {
		expectedKind = "show"
	}
	if string(item.ID) != ratingKey || item.Kind != expectedKind {
		return false, ErrIncomplete
	}
	matched := false
	for _, guid := range item.GUIDs {
		if guid.ID == "tmdb://"+strconv.FormatInt(tmdbID, 10) {
			matched = true
		}
	}
	if !matched {
		return false, ErrIncomplete
	}
	if kind == "movie" {
		return playableSubscriptionItem(item), nil
	}
	parent := ratingKey
	path := "/library/metadata/" + ratingKey + "/grandchildren"
	if season != nil {
		parent = ""
		for offset := 0; offset < 1000; {
			children, total, err := c.subscriptionPlexPage(ctx, base, "/library/metadata/"+ratingKey+"/children", grant, offset, 100)
			if err != nil {
				return false, err
			}
			for _, child := range children {
				if child.Kind != "season" || string(child.ParentID) != ratingKey || child.Index == nil {
					return false, ErrIncomplete
				}
				if *child.Index == *season {
					parent = string(child.ID)
					break
				}
			}
			if parent != "" {
				break
			}
			if total < 0 {
				return false, ErrIncomplete
			}
			offset += len(children)
			if offset >= total {
				return false, nil
			}
			if offset >= 1000 {
				return false, ErrIncomplete
			}
		}
		if parent == "" {
			return false, ErrIncomplete
		}
		if n, err := strconv.ParseInt(parent, 10, 64); err != nil || n <= 0 || strconv.FormatInt(n, 10) != parent {
			return false, ErrIncomplete
		}
		path = "/library/metadata/" + parent + "/children"
	}
	episodeChecks := 0
	for offset := 0; offset < 1000; {
		episodes, total, err := c.subscriptionPlexPage(ctx, base, path, grant, offset, 20)
		if err != nil {
			return false, err
		}
		for _, episode := range episodes {
			if episode.Kind != "episode" || string(episode.GrandparentID) != ratingKey || (season != nil && string(episode.ParentID) != parent) {
				return false, ErrIncomplete
			}
			if episodeChecks >= 20 {
				return false, ErrIncomplete
			}
			episodeChecks++
			playable, err := c.subscriptionEpisodePlayable(ctx, base, grant, episode)
			if err != nil {
				return false, err
			}
			if playable {
				return true, nil
			}
		}
		if total < 0 {
			return false, ErrIncomplete
		}
		offset += len(episodes)
		if offset >= total {
			return false, nil
		}
	}
	return false, ErrIncomplete
}

func (c *Client) subscriptionEpisodePlayable(ctx context.Context, base string, grant Grant, listed subscriptionPlexMetadata) (bool, error) {
	id := string(listed.ID)
	if n, err := strconv.ParseInt(id, 10, 64); err != nil || n <= 0 || strconv.FormatInt(n, 10) != id {
		return false, ErrIncomplete
	}
	items, _, err := c.subscriptionPlexPage(ctx, base, "/library/metadata/"+id, grant, 0, 1)
	if errors.Is(err, ErrDenied) || (err == nil && len(items) == 0) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	item := items[0]
	if item.ID != listed.ID || item.Kind != "episode" || item.GrandparentID != listed.GrandparentID || (listed.ParentID != "" && item.ParentID != listed.ParentID) || (listed.GUID != "" && item.GUID != listed.GUID) {
		return false, ErrIncomplete
	}
	return playableSubscriptionItem(item), nil
}

func playableSubscriptionItem(item subscriptionPlexMetadata) bool {
	for _, media := range item.Media {
		playable := len(media.Parts) > 0
		for _, part := range media.Parts {
			if !strings.HasPrefix(part.Key, "/library/parts/") || !availablePlexPart(part.Exists) || !availablePlexPart(part.Accessible) {
				playable = false
				break
			}
		}
		if playable {
			return true
		}
	}
	return false
}

func availablePlexPart(value json.RawMessage) bool {
	return string(value) == "true" || string(value) == "1"
}
