package upstream

import (
	"context"
	"net/url"
	"strconv"
)

// Resolve descendant membership with current Plex metadata via Tautulli. An
// episode or season key must never be matched as if it were a show key.
func (c *Client) TautulliRemovalParent(ctx context.Context, base, key, serverID string, removal Removal) (PlexItem, *int, error) {
	if _, err := c.TautulliStatus(ctx, base, key, serverID); err != nil {
		return PlexItem{}, nil, err
	}
	var metadata struct {
		ID            tautulliID `json:"rating_key"`
		ParentID      tautulliID `json:"parent_rating_key"`
		GrandparentID tautulliID `json:"grandparent_rating_key"`
		LibraryID     tautulliID `json:"section_id"`
		Kind          string     `json:"media_type"`
		Index         tautulliID `json:"media_index"`
		ParentIndex   tautulliID `json:"parent_media_index"`
	}
	if err := c.tautulli(ctx, base, key, "get_metadata", url.Values{"rating_key": {removal.MediaID}}, &metadata); err != nil {
		return PlexItem{}, nil, err
	}
	if string(metadata.ID) != removal.MediaID || metadata.Kind != removal.Kind || string(metadata.LibraryID) != removal.LibraryID {
		return PlexItem{}, nil, ErrDenied
	}
	parent, index := metadata.ParentID, metadata.Index
	if removal.Kind == "episode" {
		if metadata.ParentID == "" || metadata.ParentID == "0" {
			return PlexItem{}, nil, ErrIncomplete
		}
		parent, index = metadata.GrandparentID, metadata.ParentIndex
	} else if removal.Kind != "season" {
		return PlexItem{}, nil, ErrDenied
	}
	n, err := strconv.Atoi(string(index))
	if err != nil || n < 0 || n > 10000 || parent == "" || parent == "0" {
		return PlexItem{}, nil, ErrIncomplete
	}
	item := PlexItem{ID: string(metadata.ID), Kind: metadata.Kind, ParentID: string(metadata.ParentID), GrandparentID: string(metadata.GrandparentID), LibraryID: string(metadata.LibraryID)}
	if metadata.Index != "" {
		index, err := strconv.Atoi(string(metadata.Index))
		if err != nil || index < 0 || index > 10000 {
			return PlexItem{}, nil, ErrIncomplete
		}
		item.Index = &index
	}
	return item, &n, nil
}
