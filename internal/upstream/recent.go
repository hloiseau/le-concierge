package upstream

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const recentPageSize = 12
const recentCacheTTL = 30 * time.Second
const recentCacheLimit = 128

type RecentResult struct {
	Items      []Media
	NextOffset *int
	UpdatedAt  time.Time
}

type recentCacheEntry struct {
	Page RecentResult
	Keys []string
}

type recentMetadata struct {
	Key       tautulliID `json:"ratingKey"`
	ParentKey tautulliID `json:"parentRatingKey"`
	SeriesKey tautulliID `json:"grandparentRatingKey"`
	Title     string     `json:"title"`
	Type      string     `json:"type"`
	Year      int        `json:"year"`
	AddedAt   int64      `json:"addedAt"`
	Series    string     `json:"grandparentTitle"`
	Parent    string     `json:"parentTitle"`
}

func (c *Client) RecentPage(ctx context.Context, base, serverID string, g Grant, offset, limit int) (RecentResult, error) {
	if g.ServerToken == "" || serverID == "" || (!g.ExpiresAt.IsZero() && !g.Active(time.Now())) || offset < 0 || offset > 10000 || limit < 1 || limit > recentPageSize {
		return RecentResult{}, ErrDenied
	}
	// Verify the configured server before sending it any user credential, even on a cache hit.
	if err := c.PlexIdentity(ctx, base, serverID); err != nil {
		return RecentResult{}, err
	}
	identity, _ := json.Marshal([]string{base, serverID, g.PlexID, g.ClientID, g.AccountToken, g.ServerToken, strconv.Itoa(offset), strconv.Itoa(limit)})
	key := sha256.Sum256(identity)
	c.recentMu.Lock()
	cached, hit := c.recentCache[key]
	for k, entry := range c.recentCache {
		if time.Since(entry.Page.UpdatedAt) >= recentCacheTTL {
			delete(c.recentCache, k)
		}
	}
	c.recentMu.Unlock()
	if hit && time.Since(cached.Page.UpdatedAt) < recentCacheTTL {
		allowed, err := c.recentAccess(ctx, base, g, cached.Keys)
		if err == nil && allowed {
			return copyRecent(cached.Page), nil
		}
		c.recentMu.Lock()
		delete(c.recentCache, key)
		c.recentMu.Unlock()
		if err != nil {
			return RecentResult{}, err
		}
	}
	page, keys, err := c.fetchRecent(ctx, base, serverID, g, offset, limit)
	if err != nil {
		return RecentResult{}, err
	}
	// Empty pages are cheap to refetch and cannot provide an item to recheck access against.
	if len(keys) > 0 {
		c.recentMu.Lock()
		if c.recentCache == nil {
			c.recentCache = make(map[[32]byte]recentCacheEntry)
		}
		if len(c.recentCache) >= recentCacheLimit {
			var oldestKey [32]byte
			var oldest time.Time
			for k, entry := range c.recentCache {
				if oldest.IsZero() || entry.Page.UpdatedAt.Before(oldest) {
					oldestKey, oldest = k, entry.Page.UpdatedAt
				}
			}
			delete(c.recentCache, oldestKey)
		}
		c.recentCache[key] = recentCacheEntry{Page: copyRecent(page), Keys: keys}
		c.recentMu.Unlock()
	}
	return page, nil
}

func copyRecent(p RecentResult) RecentResult {
	p.Items = append([]Media{}, p.Items...)
	if p.NextOffset != nil {
		next := *p.NextOffset
		p.NextOffset = &next
	}
	return p
}

func (c *Client) recentAccess(ctx context.Context, base string, g Grant, keys []string) (bool, error) {
	var result struct {
		Container struct {
			Metadata []struct {
				Key tautulliID `json:"ratingKey"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	// Recheck the original episodes, not only their show: a user's content restrictions can change.
	target := base + "/library/metadata/" + strings.Join(keys, ",") + "?skipRefresh=1&includeFields=ratingKey"
	err := c.JSON(ctx, "GET", target, plexHeaders(g.ClientID, g.ServerToken), "", &result)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	allowed := make(map[string]bool, len(result.Container.Metadata))
	for _, m := range result.Container.Metadata {
		allowed[string(m.Key)] = true
	}
	for _, key := range keys {
		if !allowed[key] {
			return false, nil
		}
	}
	return true, nil
}

func (c *Client) fetchRecent(ctx context.Context, base, serverID string, g Grant, offset, limit int) (RecentResult, []string, error) {
	var result struct {
		Container *struct {
			Offset    *int             `json:"offset"`
			Size      *int             `json:"size"`
			TotalSize *int             `json:"totalSize"`
			Metadata  []recentMetadata `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	q := url.Values{"X-Plex-Container-Start": {strconv.Itoa(offset)}, "X-Plex-Container-Size": {strconv.Itoa(limit + 1)}}
	// The extra raw entry proves that another page exists, even when totalSize is absent.
	err := c.JSON(ctx, "GET", base+"/library/recentlyAdded?"+q.Encode(), plexHeaders(g.ClientID, g.ServerToken), "", &result)
	if err != nil {
		return RecentResult{}, nil, err
	}
	r := result.Container
	if r == nil || (r.Metadata == nil && r.Size == nil) || len(r.Metadata) > limit+1 || (r.Offset != nil && *r.Offset != offset) || (r.Size != nil && *r.Size != len(r.Metadata)) || (r.TotalSize != nil && (*r.TotalSize < 0 || (len(r.Metadata) > 0 && *r.TotalSize < offset+len(r.Metadata)))) {
		return RecentResult{}, nil, ErrUnavailable
	}
	consumed := min(len(r.Metadata), limit)
	page := RecentResult{Items: []Media{}, UpdatedAt: time.Now().UTC()}
	if len(r.Metadata) > consumed || (r.TotalSize != nil && offset+consumed < *r.TotalSize) {
		if consumed == 0 {
			return RecentResult{}, nil, ErrUnavailable
		}
		next := offset + consumed
		if next <= 10000 {
			page.NextOffset = &next
		}
	}
	var keys []string
	seen := map[string]bool{}
	groups := map[string]int{}
	episodes := map[string]int{}
	for _, m := range r.Metadata[:consumed] {
		if m.Type != "movie" && m.Type != "show" && m.Type != "season" && m.Type != "episode" {
			continue
		}
		if m.Key == "" || m.Key == "0" || strings.TrimSpace(m.Title) == "" || m.AddedAt <= 0 {
			return RecentResult{}, nil, ErrUnavailable
		}
		if seen[string(m.Key)] {
			continue
		}
		seen[string(m.Key)] = true
		keys = append(keys, string(m.Key))
		id, title, kind, subtitle := string(m.Key), m.Title, m.Type, ""
		if m.Type == "episode" && m.SeriesKey != "" && m.SeriesKey != "0" && m.Series != "" {
			id, title, kind = string(m.SeriesKey), m.Series, "show"
			episodes[id]++
			subtitle = strings.Trim(strings.Join([]string{m.Parent, m.Title}, " · "), " ·")
		} else if m.Type == "season" && m.ParentKey != "" && m.ParentKey != "0" && m.Parent != "" {
			id, title, kind, subtitle = string(m.ParentKey), m.Parent, "show", m.Title
		}
		addedAt := time.Unix(m.AddedAt, 0).UTC().Format(time.RFC3339)
		if index, exists := groups[id]; exists {
			if addedAt > page.Items[index].AddedAt {
				page.Items[index].AddedAt = addedAt
			}
			if episodes[id] > 1 {
				page.Items[index].Subtitle = fmt.Sprintf("%d épisodes ajoutés", episodes[id])
			}
			continue
		}
		groups[id] = len(page.Items)
		link := "https://app.plex.tv/desktop/#!/server/" + url.PathEscape(serverID) + "/details?key=" + url.QueryEscape("/library/metadata/"+id)
		page.Items = append(page.Items, Media{ID: "plex:" + serverID + ":" + id, Title: title, Type: kind, Year: m.Year, AddedAt: addedAt, URL: link, Subtitle: subtitle, PosterURL: "/api/v1/posters/plex/" + id})
	}
	// shortcut: grouping is page-local; consumers merge stable IDs when appending subsequent pages.
	sort.SliceStable(page.Items, func(i, j int) bool {
		if page.Items[i].AddedAt == page.Items[j].AddedAt {
			return page.Items[i].ID < page.Items[j].ID
		}
		return page.Items[i].AddedAt > page.Items[j].AddedAt
	})
	return page, keys, nil
}
