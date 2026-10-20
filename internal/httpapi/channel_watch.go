package httpapi

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/hloiseau/le-concierge/internal/departures"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

// This cache lives for one delivery pass. Revalidation gets a fresh cache.
type channelWatchState struct {
	grant     upstream.Grant
	unwatched []upstream.UnwatchedItem
	plays     map[string][]upstream.Playback
	metadata  map[string]upstream.PlexItem
	readAt    time.Time
}

func (s *Server) channelRequesterUnwatched(ctx context.Context, owner upstream.SeerrRequester, request upstream.SeerrRequestedMedia, removal upstream.Removal, rule upstream.RemovalRule, states map[string]*channelWatchState) (bool, error) {
	if owner.PlexID == "" || len(owner.DiscordIDs) == 0 {
		return false, nil
	}
	s.mu.Lock()
	userID := ""
	for id, grant := range s.grants {
		if grant.PlexID == owner.PlexID && time.Now().Before(grant.ExpiresAt) {
			userID = id
			break
		}
	}
	s.mu.Unlock()
	if userID == "" {
		return false, nil
	}
	user, err := s.Store.User(ctx, userID)
	if err != nil || user.PlexID != owner.PlexID {
		return false, nil
	}
	grant, err := s.grant(ctx, user)
	if errors.Is(err, upstream.ErrDenied) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	state := states[owner.PlexID]
	if state == nil || state.grant.ServerToken != grant.ServerToken || time.Since(state.readAt) >= time.Minute {
		state = &channelWatchState{grant: grant, plays: map[string][]upstream.Playback{}, metadata: map[string]upstream.PlexItem{}, readAt: time.Now()}
		state.unwatched, err = s.Upstream.PlexUnwatchedItems(ctx, s.Config.PlexURL, s.Config.PlexServerID, grant)
		if err != nil {
			return false, err
		}
		history, err := s.Upstream.TautulliHistory(ctx, s.Config.TautulliURL, s.Config.TautulliAPIKey, s.Config.PlexServerID, owner.PlexID)
		if err != nil {
			return false, err
		}
		for _, play := range history.Items {
			state.plays[play.MediaID] = append(state.plays[play.MediaID], play)
		}
		states[owner.PlexID] = state
	}
	get := func(id string) (upstream.PlexItem, error) {
		if item, ok := state.metadata[id]; ok {
			return item, nil
		}
		item, err := s.Upstream.PlexMetadata(ctx, s.Config.PlexURL, id, grant)
		if err == nil {
			state.metadata[id] = item
		}
		return item, err
	}
	for _, row := range state.unwatched {
		matches := (removal.Kind == row.Kind && removal.MediaID == row.ID) ||
			(removal.Kind == "season" && row.Kind == "episode" && row.ParentID == removal.MediaID) ||
			(removal.Kind == "show" && row.Kind == "episode" && row.GrandparentID == removal.MediaID)
		if !matches {
			continue
		}
		item, err := get(row.ID)
		if errors.Is(err, upstream.ErrNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		if item.GUID != row.GUID || item.Kind != row.Kind || item.LibraryID != removal.LibraryID ||
			(row.Kind == "episode" && (item.ParentID != row.ParentID || item.GrandparentID != row.GrandparentID)) {
			return false, upstream.ErrIncomplete
		}
		if rule.Excludes(item) || departures.Watch(item, state.plays[item.ID]) == "completed" {
			continue
		}
		if item.Kind == "episode" {
			season, err := get(item.ParentID)
			if err != nil {
				return false, err
			}
			if season.Kind != "season" || season.LibraryID != item.LibraryID || season.ParentID != item.GrandparentID || season.Index == nil {
				return false, upstream.ErrIncomplete
			}
			if !slices.Contains(request.Seasons, *season.Index) {
				continue
			}
			show, err := get(item.GrandparentID)
			if err != nil {
				return false, err
			}
			if show.Kind != "show" || show.LibraryID != item.LibraryID || show.TMDBID != request.TMDBID {
				return false, upstream.ErrIncomplete
			}
		} else if item.TMDBID != request.TMDBID {
			return false, upstream.ErrIncomplete
		}
		if ctx.Err() != nil || !time.Now().Before(grant.ExpiresAt) || time.Since(state.readAt) >= time.Minute {
			return false, upstream.ErrUnavailable
		}
		s.mu.Lock()
		current, exists := s.grants[userID]
		s.mu.Unlock()
		if !exists || current.PlexID != grant.PlexID || current.AccountToken != grant.AccountToken {
			return false, nil
		}
		// Positive personal Plex filtering, canonical identity and current
		// metadata establish unwatched state; missing history alone never does.
		return true, nil
	}
	return false, nil
}
