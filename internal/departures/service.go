// Package departures builds a private observation list, never a notification queue.
package departures

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

type Item struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Kind        string    `json:"type"`
	Watch       string    `json:"watch"`
	EstimatedAt time.Time `json:"estimatedAt"`
	URL         string    `json:"url"`
}
type Page struct {
	Items     []Item    `json:"items"`
	Scope     string    `json:"scope"`
	UpdatedAt time.Time `json:"updatedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type Service struct {
	client   *upstream.Client
	gate     chan struct{}
	key      string
	snapshot upstream.RemovalSnapshot
	rules    map[int64]upstream.RemovalRule
}

func New(client *upstream.Client) *Service {
	return &Service{client: client, gate: make(chan struct{}, 1)}
}

// Only the bounded, non-personal Maintainerr snapshot is cached, under 30 seconds.
// History and Plex access are read for every request; no stale fallback on error.
func (s *Service) sources(ctx context.Context, c config.Config) (upstream.RemovalSnapshot, map[int64]upstream.RemovalRule, error) {
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return upstream.RemovalSnapshot{}, nil, ctx.Err()
	}
	defer func() { <-s.gate }()
	key := c.MaintainerrURL + "\x00" + c.PlexServerID
	if key == s.key && time.Since(s.snapshot.UpdatedAt) < 25*time.Second {
		return s.snapshot, s.rules, nil
	}
	s.snapshot, s.rules = upstream.RemovalSnapshot{}, nil
	snap, err := s.client.MaintainerrRemovals(ctx, c.MaintainerrURL, c.PlexServerID)
	if err != nil {
		return snap, nil, err
	}
	rules, err := s.client.MaintainerrRules(ctx, c.MaintainerrURL)
	if err != nil {
		return upstream.RemovalSnapshot{}, nil, err
	}
	s.key, s.snapshot, s.rules = key, snap, rules
	return snap, rules, nil
}

func (s *Service) List(ctx context.Context, c config.Config, g upstream.Grant) (Page, error) {
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	if g.PlexID == "" || g.ServerToken == "" || !time.Now().Before(g.ExpiresAt) {
		return Page{}, upstream.ErrDenied
	}
	if err := s.client.PlexIdentity(ctx, c.PlexURL, c.PlexServerID); err != nil {
		return Page{}, err
	}
	snapshot, rules, err := s.sources(ctx, c)
	if err != nil {
		return Page{}, err
	}
	history, err := s.client.TautulliHistory(ctx, c.TautulliURL, c.TautulliAPIKey, c.PlexServerID, g.PlexID)
	if err != nil {
		return Page{}, err
	}
	interest := map[string]bool{}
	plays := map[string][]upstream.Playback{}
	for _, p := range history.Items {
		interest[p.Kind+":"+p.MediaID] = true
		plays[p.MediaID] = append(plays[p.MediaID], p)
		if p.Kind == "episode" {
			if p.ParentID != "" {
				interest["season:"+p.ParentID] = true
			}
			if p.GrandparentID != "" {
				interest["show:"+p.GrandparentID] = true
			}
		}
	}
	now := time.Now().UTC()
	page := Page{Items: []Item{}, Scope: "recorded_history", UpdatedAt: snapshot.UpdatedAt, ExpiresAt: snapshot.UpdatedAt.Add(time.Minute)}
	if history.UpdatedAt.Before(page.UpdatedAt) {
		page.UpdatedAt = history.UpdatedAt
	}
	metadata := map[string]upstream.PlexItem{}
	missing := map[string]bool{}
	items := map[string]Item{}
	for _, r := range snapshot.Items {
		if !interest[r.Kind+":"+r.MediaID] || r.EstimatedAt == nil || !r.EstimatedAt.After(now) || r.EstimatedAt.After(now.Add(30*24*time.Hour)) || r.RuleEvaluationFailed {
			continue
		}
		rule, ok := rules[r.CollectionID]
		// Collections without an active verified rule group remain unconfirmed.
		if !ok || !rule.Active || missing[r.MediaID] {
			continue
		}
		m, ok := metadata[r.MediaID]
		if !ok {
			if len(metadata)+len(missing) >= 100 {
				return Page{}, upstream.ErrIncomplete
			}
			m, err = s.client.PlexMetadata(ctx, c.PlexURL, r.MediaID, g)
			if errors.Is(err, upstream.ErrNotFound) {
				missing[r.MediaID] = true
				continue
			}
			if err != nil {
				return Page{}, err
			}
			metadata[r.MediaID] = m
		}
		if m.Kind != r.Kind || m.LibraryID != r.LibraryID {
			return Page{}, upstream.ErrIncomplete
		}
		if rule.Excludes(m) {
			continue
		}
		watch := Watch(m, plays[m.ID])
		title := m.Title
		if m.Kind == "season" && m.ParentTitle != "" {
			title = m.ParentTitle + " · " + m.Title
		}
		if m.Kind == "episode" && m.GrandparentTitle != "" {
			title = m.GrandparentTitle + " · " + m.ParentTitle + " · " + m.Title
		}
		item := Item{ID: m.ID, Title: title, Kind: m.Kind, Watch: watch, EstimatedAt: *r.EstimatedAt, URL: "https://app.plex.tv/desktop/#!/server/" + url.PathEscape(c.PlexServerID) + "/details?key=" + url.QueryEscape("/library/metadata/"+m.ID)}
		if old, exists := items[m.ID]; !exists || item.EstimatedAt.Before(old.EstimatedAt) {
			items[m.ID] = item
		}
	}
	if ctx.Err() != nil || !time.Now().Before(page.ExpiresAt) {
		return Page{}, upstream.ErrIncomplete
	}
	for _, item := range items {
		page.Items = append(page.Items, item)
	}
	sort.Slice(page.Items, func(i, j int) bool {
		if page.Items[i].EstimatedAt.Equal(page.Items[j].EstimatedAt) {
			return page.Items[i].ID < page.Items[j].ID
		}
		return page.Items[i].EstimatedAt.Before(page.Items[j].EstimatedAt)
	})
	return page, nil
}

func Watch(m upstream.PlexItem, plays []upstream.Playback) string {
	// A child history never establishes completion of a whole season/show.
	if m.Kind != "movie" && m.Kind != "episode" {
		return "unknown"
	}
	if m.ViewCount > 0 {
		return "completed"
	}
	state := "unknown"
	for _, p := range plays {
		// Do not transfer history across changed keys, GUIDs or ambiguous editions.
		if p.MediaID != m.ID || p.Kind != m.Kind || p.GUID == "" || p.GUID != m.GUID || m.Edition != "" {
			continue
		}
		if p.Completed {
			return "completed"
		}
		state = "in_progress"
	}
	if m.ViewOffset > 0 {
		return "in_progress"
	}
	return state
}
