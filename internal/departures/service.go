// Package departures builds a private observation list, never a notification queue.
package departures

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

type Item struct {
	ActionID    string    `json:"-"`
	Requested   bool      `json:"-"`
	Following   bool      `json:"-"`
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Kind        string    `json:"type"`
	Watch       string    `json:"watch"`
	EstimatedAt time.Time `json:"estimatedAt"`
	URL         string    `json:"url"`
}
type Page struct {
	FollowState string    `json:"-"`
	Items       []Item    `json:"items"`
	Scope       string    `json:"scope"`
	UpdatedAt   time.Time `json:"updatedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
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
func (s *Service) sources(ctx context.Context, c config.Config, fresh bool) (upstream.RemovalSnapshot, map[int64]upstream.RemovalRule, error) {
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return upstream.RemovalSnapshot{}, nil, ctx.Err()
	}
	defer func() { <-s.gate }()
	key := c.MaintainerrURL + "\x00" + c.PlexServerID
	if !fresh && key == s.key && time.Since(s.snapshot.UpdatedAt) < 25*time.Second {
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
	return s.list(ctx, c, g, false, nil, false)
}

// ListFresh bypasses the shared snapshot before a reminder is delivered.
func (s *Service) ListFresh(ctx context.Context, c config.Config, g upstream.Grant) (Page, error) {
	return s.list(ctx, c, g, true, nil, false)
}

func (s *Service) ListInterested(ctx context.Context, c config.Config, g upstream.Grant, targets []upstream.InterestTarget, allUnwatched, fresh bool) (Page, error) {
	return s.list(ctx, c, g, fresh, targets, allUnwatched)
}

func (s *Service) list(ctx context.Context, c config.Config, g upstream.Grant, fresh bool, targets []upstream.InterestTarget, allUnwatched bool) (Page, error) {
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	if g.PlexID == "" || g.ServerToken == "" || !time.Now().Before(g.ExpiresAt) {
		return Page{}, upstream.ErrDenied
	}
	if err := s.client.PlexIdentity(ctx, c.PlexURL, c.PlexServerID); err != nil {
		return Page{}, err
	}
	snapshot, rules, err := s.sources(ctx, c, fresh)
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
	unwatched := map[string]string{}
	if allUnwatched || len(targets) > 0 {
		page.Scope = "personal_interest"
		unwatched, err = s.client.PlexUnwatched(ctx, c.PlexURL, c.PlexServerID, g)
		if err != nil {
			return Page{}, err
		}
	}
	metadata := map[string]upstream.PlexItem{}
	missing := map[string]bool{}
	get := func(id string) (upstream.PlexItem, error) {
		if m, ok := metadata[id]; ok {
			return m, nil
		}
		if missing[id] {
			return upstream.PlexItem{}, upstream.ErrNotFound
		}
		// A single threatened series can contain hundreds of episodes. Keep a
		// bounded cache while the request deadline also bounds the source work.
		if len(metadata)+len(missing) >= 5000 {
			return upstream.PlexItem{}, fmt.Errorf("departure metadata limit: %w", upstream.ErrIncomplete)
		}
		m, err := s.client.PlexMetadata(ctx, c.PlexURL, id, g)
		if errors.Is(err, upstream.ErrNotFound) {
			missing[id] = true
			return m, err
		}
		if err == nil {
			metadata[id] = m
		}
		if err != nil {
			return m, fmt.Errorf("departure Plex metadata: %w", err)
		}
		return m, nil
	}
	prefetch := func(ids []string) error {
		pending := []string{}
		for _, id := range ids {
			if _, ok := metadata[id]; !ok && !missing[id] {
				pending = append(pending, id)
			}
		}
		if len(metadata)+len(missing)+len(pending) > 5000 {
			return fmt.Errorf("departure metadata limit: %w", upstream.ErrIncomplete)
		}
		for start := 0; start < len(pending); start += 50 {
			batch := pending[start:min(start+50, len(pending))]
			items, err := s.client.PlexMetadataBatch(ctx, c.PlexURL, batch, g)
			if errors.Is(err, upstream.ErrNotFound) {
				continue // Resolve missing keys individually to preserve access checks.
			}
			if err != nil {
				return fmt.Errorf("departure Plex metadata batch: %w", err)
			}
			for id, item := range items {
				metadata[id] = item
			}
		}
		return nil
	}
	rootIDs := []string{}
	rootSeen := map[string]bool{}
	for _, r := range snapshot.Items {
		rule, ok := rules[r.CollectionID]
		if rootSeen[r.MediaID] || (!interest[r.Kind+":"+r.MediaID] && len(targets) == 0 && !allUnwatched) ||
			r.EstimatedAt == nil || !r.EstimatedAt.After(now) || r.EstimatedAt.After(now.Add(30*24*time.Hour)) ||
			r.RuleEvaluationFailed || !ok || !rule.Active {
			continue
		}
		rootSeen[r.MediaID] = true
		rootIDs = append(rootIDs, r.MediaID)
	}
	if len(rootIDs) > 1 {
		if err := prefetch(rootIDs); err != nil {
			return Page{}, err
		}
	}
	items := map[string]Item{}
	for _, r := range snapshot.Items {
		if (!interest[r.Kind+":"+r.MediaID] && len(targets) == 0 && !allUnwatched) || r.EstimatedAt == nil || !r.EstimatedAt.After(now) || r.EstimatedAt.After(now.Add(30*24*time.Hour)) || r.RuleEvaluationFailed {
			continue
		}
		rule, ok := rules[r.CollectionID]
		// Collections without an active verified rule group remain unconfirmed.
		if !ok || !rule.Active || missing[r.MediaID] {
			continue
		}
		m, err := get(r.MediaID)
		if errors.Is(err, upstream.ErrNotFound) {
			missing[r.MediaID] = true
			continue
		}
		if err != nil {
			return Page{}, err
		}
		if m.Kind != r.Kind || m.LibraryID != r.LibraryID {
			return Page{}, fmt.Errorf("departure collection metadata mismatch: %w", upstream.ErrIncomplete)
		}
		if rule.Excludes(m) {
			continue
		}
		candidates := []upstream.PlexItem{m}
		if (len(targets) > 0 || allUnwatched) && (m.Kind == "show" || m.Kind == "season") {
			root := m
			if m.Kind == "season" {
				root, err = get(m.ParentID)
				if err != nil {
					return Page{}, err
				}
				if root.Kind != "show" || root.LibraryID != m.LibraryID {
					return Page{}, upstream.ErrIncomplete
				}
			}
			relevant := allUnwatched || interest[m.Kind+":"+m.ID]
			for _, target := range targets {
				if target.Type == "tv" && target.TMDBID == root.TMDBID && root.TMDBID > 0 &&
					(target.Following || (target.Requested && slices.Contains(target.PlexKeys, root.ID))) &&
					(m.Kind != "season" || target.Season == nil || (m.Index != nil && *m.Index == *target.Season)) {
					relevant = true
				}
			}
			if !relevant {
				continue
			}
			ids, e := s.client.PlexEpisodeIDs(ctx, c.PlexURL, g, m)
			if e != nil {
				return Page{}, fmt.Errorf("departure episode enumeration: %w", e)
			}
			ids = slices.DeleteFunc(ids, func(id string) bool { return unwatched[id] == "" && !interest["episode:"+id] })
			if err := prefetch(ids); err != nil {
				return Page{}, err
			}
			candidates = nil
			for _, id := range ids {
				child, e := get(id)
				if errors.Is(e, upstream.ErrNotFound) {
					continue
				}
				if e != nil {
					return Page{}, e
				}
				if child.Kind != "episode" || child.LibraryID != m.LibraryID || (m.Kind == "show" && child.GrandparentID != m.ID) || (m.Kind == "season" && child.ParentID != m.ID) {
					return Page{}, fmt.Errorf("departure episode parent mismatch: %w", upstream.ErrIncomplete)
				}
				if !rule.Excludes(child) {
					candidates = append(candidates, child)
				}
			}
		}
		for _, m := range candidates {
			requested, following, e := matchInterest(m, targets, get)
			if e != nil {
				return Page{}, fmt.Errorf("departure interest matching: %w", e)
			}
			watch := Watch(m, plays[m.ID])
			if watch == "unknown" && unwatched[m.ID] == m.GUID && m.GUID != "" {
				watch = "unstarted"
			}
			if !interest[m.Kind+":"+m.ID] && !requested && !following && !allUnwatched {
				continue
			}
			title := m.Title
			if m.Kind == "season" && m.ParentTitle != "" {
				title = m.ParentTitle + " · " + m.Title
			}
			if m.Kind == "episode" && m.GrandparentTitle != "" {
				title = m.GrandparentTitle + " · " + m.ParentTitle + " · " + m.Title
			}
			item := Item{ID: m.ID, Title: title, Kind: m.Kind, Watch: watch, EstimatedAt: *r.EstimatedAt, URL: "https://app.plex.tv/desktop/#!/server/" + url.PathEscape(c.PlexServerID) + "/details?key=" + url.QueryEscape("/library/metadata/"+m.ID)}
			item.ActionID = fmt.Sprintf("%d:%d:%d", r.CollectionID, r.MembershipID, r.Action)
			item.Requested, item.Following = requested, following
			if old, exists := items[m.ID]; !exists || item.EstimatedAt.Before(old.EstimatedAt) {
				items[m.ID] = item
			}
		}
	}
	if ctx.Err() != nil || !time.Now().Before(page.ExpiresAt) {
		return Page{}, fmt.Errorf("departure snapshot expired: %w", upstream.ErrIncomplete)
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

func matchInterest(m upstream.PlexItem, targets []upstream.InterestTarget, get func(string) (upstream.PlexItem, error)) (bool, bool, error) {
	if len(targets) == 0 {
		return false, false, nil
	}
	kind, tmdb, plexKey := m.Kind, m.TMDBID, m.ID
	var season *int
	if m.Kind == "season" || m.Kind == "episode" {
		seasonItem := m
		if m.Kind == "episode" {
			var err error
			seasonItem, err = get(m.ParentID)
			if err != nil {
				return false, false, err
			}
			if seasonItem.Kind != "season" || seasonItem.ParentID != m.GrandparentID || seasonItem.LibraryID != m.LibraryID {
				return false, false, upstream.ErrIncomplete
			}
		}
		parent, err := get(seasonItem.ParentID)
		if err != nil {
			return false, false, err
		}
		if parent.Kind != "show" || parent.LibraryID != m.LibraryID {
			return false, false, upstream.ErrIncomplete
		}
		kind, tmdb, season = "tv", parent.TMDBID, seasonItem.Index
		plexKey = parent.ID
	} else if kind == "show" {
		kind = "tv"
	}
	requested, following := false, false
	for _, t := range targets {
		if t.Type != kind || t.TMDBID != tmdb || tmdb <= 0 || (t.Season != nil && (season == nil || *season != *t.Season)) {
			continue
		}
		requested = requested || (t.Requested && slices.Contains(t.PlexKeys, plexKey))
		following = following || t.Following
	}
	return requested, following, nil
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
