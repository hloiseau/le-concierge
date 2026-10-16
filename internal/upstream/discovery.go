package upstream

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"math/big"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

const discoveryPageSize = 200
const discoveryMaxPages = 25
const discoveryPlaybackReads = 20

type discoveryMetadata struct {
	recentMetadata
	DeletedAt int64           `json:"deletedAt"`
	LeafCount *int            `json:"leafCount"`
	Media     json.RawMessage `json:"Media"`
}

type RecapResult struct {
	Items      []Media
	Start, End time.Time
}

// A bounded, complete read is required: a truncated catalogue would bias /surprise.
// Plex documents /library/all, media queries and paging at https://developer.plex.tv/pms/.
func (c *Client) discoveryItems(ctx context.Context, base, serverID string, g Grant, query url.Values) ([]discoveryMetadata, error) {
	if g.ServerToken == "" || serverID == "" || (!g.ExpiresAt.IsZero() && !time.Now().Before(g.ExpiresAt)) {
		return nil, ErrDenied
	}
	if err := c.PlexIdentity(ctx, base, serverID); err != nil {
		return nil, err
	}
	items := []discoveryMetadata{}
	total, offset := -1, 0
	for range discoveryMaxPages {
		var result struct {
			Container *struct {
				Offset    *int                `json:"offset"`
				Size      *int                `json:"size"`
				TotalSize *int                `json:"totalSize"`
				Metadata  []discoveryMetadata `json:"Metadata"`
			} `json:"MediaContainer"`
		}
		headers := plexHeaders(g.ClientID, g.ServerToken)
		headers["X-Plex-Container-Start"] = strconv.Itoa(offset)
		headers["X-Plex-Container-Size"] = strconv.Itoa(discoveryPageSize)
		if err := c.JSON(ctx, "GET", base+"/library/all?"+query.Encode(), headers, "", &result); err != nil {
			return nil, err
		}
		p := result.Container
		if p == nil || p.Size == nil || p.TotalSize == nil || *p.Size != len(p.Metadata) || len(p.Metadata) > discoveryPageSize || *p.TotalSize < 0 || *p.TotalSize > discoveryPageSize*discoveryMaxPages || (p.Offset != nil && *p.Offset != offset) || (offset > 0 && p.Offset == nil) || offset+len(p.Metadata) > *p.TotalSize || (total >= 0 && total != *p.TotalSize) {
			return nil, ErrIncomplete
		}
		total = *p.TotalSize
		for _, item := range p.Metadata {
			if item.Key == "" || item.Key == "0" || strings.TrimSpace(item.Title) == "" || item.AddedAt <= 0 {
				return nil, ErrIncomplete
			}
		}
		items = append(items, p.Metadata...)
		offset += len(p.Metadata)
		if offset == total {
			return items, nil
		}
		if len(p.Metadata) == 0 {
			return nil, ErrIncomplete
		}
	}
	return nil, ErrIncomplete
}

func (c *Client) Surprise(ctx context.Context, base, serverID string, g Grant, kind string) (Media, error) {
	return c.surprise(ctx, base, serverID, g, kind, rand.Reader)
}

func (c *Client) surprise(ctx context.Context, base, serverID string, g Grant, kind string, random io.Reader) (Media, error) {
	types := "1,2"
	switch kind {
	case "":
	case "movie":
		types = "1"
	case "show":
		types = "2"
	default:
		return Media{}, ErrDenied
	}
	items, err := c.discoveryItems(ctx, base, serverID, g, url.Values{"type": {types}, "sort": {"addedAt"}})
	if err != nil {
		return Media{}, err
	}
	candidates := []discoveryMetadata{}
	seen := map[string]bool{}
	for _, m := range items {
		if (m.Type != "movie" && m.Type != "show") || (kind != "" && m.Type != kind) {
			return Media{}, ErrIncomplete
		}
		if seen[string(m.Key)] || m.DeletedAt > 0 || (m.Type == "show" && m.LeafCount != nil && *m.LeafCount == 0) {
			continue
		}
		seen[string(m.Key)] = true
		candidates = append(candidates, m)
	}
	remainingReads := discoveryPlaybackReads
	for len(candidates) > 0 {
		index, err := rand.Int(random, big.NewInt(int64(len(candidates))))
		if err != nil {
			return Media{}, ErrUnavailable
		}
		chosen := candidates[index.Int64()]
		// Sampling without replacement preserves equal chances after absent files are excluded.
		candidates[index.Int64()] = candidates[len(candidates)-1]
		candidates = candidates[:len(candidates)-1]
		playable, err := c.discoveryPlayable(ctx, base, g, chosen, &remainingReads)
		if err != nil {
			return Media{}, err
		}
		if playable {
			return discoveryMedia(chosen.recentMetadata, serverID), nil
		}
	}
	return Media{}, nil
}

func (c *Client) discoveryPlayable(ctx context.Context, base string, g Grant, chosen discoveryMetadata, remaining *int) (bool, error) {
	if *remaining == 0 {
		return false, ErrIncomplete
	}
	*remaining -= 1
	id := string(chosen.Key)
	items, _, err := c.subscriptionPlexPage(ctx, base, "/library/metadata/"+id, g, 0, 1)
	if err != nil || len(items) == 0 {
		return false, err
	}
	if string(items[0].ID) != id || items[0].Kind != chosen.Type {
		return false, ErrIncomplete
	}
	if chosen.Type == "movie" {
		return playableSubscriptionItem(items[0]), nil
	}
	for offset := 0; ; {
		if *remaining == 0 {
			return false, ErrIncomplete
		}
		*remaining -= 1
		episodes, total, err := c.subscriptionPlexPage(ctx, base, "/library/metadata/"+id+"/grandchildren", g, offset, 20)
		if err != nil {
			return false, err
		}
		for _, episode := range episodes {
			if episode.ID == "" || episode.ID == "0" || episode.Kind != "episode" || string(episode.GrandparentID) != id {
				return false, ErrIncomplete
			}
			if playableSubscriptionItem(episode) {
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
}

// The seven completed Paris calendar days can contain 167, 168 or 169 hours.
func recapPeriod(now time.Time) (time.Time, time.Time, error) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return time.Time{}, time.Time{}, ErrUnavailable
	}
	local := now.In(paris)
	end := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, paris)
	return end.AddDate(0, 0, -7), end, nil
}

func (c *Client) Recap(ctx context.Context, base, serverID string, g Grant, now time.Time) (RecapResult, error) {
	start, end, err := recapPeriod(now)
	if err != nil {
		return RecapResult{}, err
	}
	query := url.Values{"type": {"1,4"}, "sort": {"addedAt:desc"}, "addedAt>>": {strconv.FormatInt(start.Unix()-1, 10)}, "addedAt<<": {strconv.FormatInt(end.Unix(), 10)}}
	items, err := c.discoveryItems(ctx, base, serverID, g, query)
	if err != nil {
		return RecapResult{}, err
	}
	result := RecapResult{Items: []Media{}, Start: start, End: end}
	seen, groups := map[string]bool{}, map[string]int{}
	var keys []string
	for _, m := range items {
		if m.Type != "movie" && m.Type != "episode" {
			return RecapResult{}, ErrIncomplete
		}
		if m.DeletedAt > 0 || m.AddedAt < start.Unix() || m.AddedAt >= end.Unix() || seen[string(m.Key)] {
			continue
		}
		if len(m.Media) > 0 {
			var playback subscriptionPlexMetadata
			if json.Unmarshal(m.Media, &playback.Media) != nil {
				return RecapResult{}, ErrIncomplete
			}
			if !playableSubscriptionItem(playback) {
				continue
			}
		}
		seen[string(m.Key)] = true
		keys = append(keys, string(m.Key))
		if m.Type == "episode" {
			if m.SeriesKey == "" || m.SeriesKey == "0" || strings.TrimSpace(m.Series) == "" {
				return RecapResult{}, ErrIncomplete
			}
			m.Key, m.Title, m.Type = m.SeriesKey, m.Series, "show"
		}
		media := discoveryMedia(m.recentMetadata, serverID)
		if index, exists := groups[media.ID]; exists {
			if media.AddedAt > result.Items[index].AddedAt {
				result.Items[index].AddedAt = media.AddedAt
			}
			continue
		}
		if media.Type == "show" {
			media.Subtitle = "Nouveaux épisodes"
		}
		groups[media.ID] = len(result.Items)
		result.Items = append(result.Items, media)
	}
	// Recheck the original episodes; access to the parent show alone is insufficient.
	for offset := 0; offset < len(keys); offset += discoveryPageSize {
		allowed, err := c.recentAccess(ctx, base, g, keys[offset:min(offset+discoveryPageSize, len(keys))])
		if err != nil {
			return RecapResult{}, err
		}
		if !allowed {
			return RecapResult{}, ErrDenied
		}
	}
	sort.Slice(result.Items, func(i, j int) bool {
		if result.Items[i].AddedAt == result.Items[j].AddedAt {
			return result.Items[i].ID < result.Items[j].ID
		}
		return result.Items[i].AddedAt > result.Items[j].AddedAt
	})
	return result, nil
}

func discoveryMedia(m recentMetadata, serverID string) Media {
	id := string(m.Key)
	return Media{ID: "plex:" + serverID + ":" + id, Title: m.Title, Type: m.Type, Year: m.Year, AddedAt: time.Unix(m.AddedAt, 0).UTC().Format(time.RFC3339), URL: "https://app.plex.tv/desktop/#!/server/" + url.PathEscape(serverID) + "/details?key=" + url.QueryEscape("/library/metadata/"+id)}
}
