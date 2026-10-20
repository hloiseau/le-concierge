package upstream

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

type PlexItem struct {
	ID, ParentID, GrandparentID, LibraryID                    string
	Title, ParentTitle, GrandparentTitle, Kind, GUID, Edition string
	ViewCount, ViewOffset                                     int64
	TMDBID                                                    int64
	AddedAt                                                   int64
	Playable                                                  bool
	Index                                                     *int
	Thumb                                                     string
}

func (c *Client) PlexIdentity(ctx context.Context, base, serverID string) error {
	var result struct {
		Container struct {
			ID string `json:"machineIdentifier"`
		} `json:"MediaContainer"`
	}
	if err := c.JSON(ctx, "GET", base+"/identity", nil, "", &result); err != nil {
		return err
	}
	if serverID == "" || result.Container.ID != serverID {
		return ErrDenied
	}
	return nil
}

type plexMetadata struct {
	ID               tautulliID `json:"ratingKey"`
	ParentID         tautulliID `json:"parentRatingKey"`
	GrandparentID    tautulliID `json:"grandparentRatingKey"`
	LibraryID        tautulliID `json:"librarySectionID"`
	Title            string     `json:"title"`
	ParentTitle      string     `json:"parentTitle"`
	GrandparentTitle string     `json:"grandparentTitle"`
	Kind             string     `json:"type"`
	GUID             string     `json:"guid"`
	Thumb            string     `json:"thumb"`
	// Plex also sends Guid (external IDs); capture it to avoid Go's case-insensitive matching.
	ExternalGUIDs []struct {
		ID string `json:"id"`
	} `json:"Guid"`
	Index      *int            `json:"index"`
	Edition    string          `json:"editionTitle"`
	ViewCount  int64           `json:"viewCount"`
	ViewOffset int64           `json:"viewOffset"`
	AddedAt    int64           `json:"addedAt"`
	Media      json.RawMessage `json:"Media"`
}

// Use the user's server token. skipRefresh avoids an implicit agent refresh.
func (c *Client) PlexMetadata(ctx context.Context, base, id string, g Grant) (PlexItem, error) {
	items, err := c.PlexMetadataBatch(ctx, base, []string{id}, g)
	if err != nil {
		return PlexItem{}, err
	}
	item, ok := items[id]
	if !ok {
		return PlexItem{}, ErrIncomplete
	}
	return item, nil
}

// Plex accepts comma-separated keys. Small batches avoid one round trip per
// episode while retaining exactly the same per-user metadata validation.
func (c *Client) PlexMetadataBatch(ctx context.Context, base string, ids []string, g Grant) (map[string]PlexItem, error) {
	if len(ids) == 0 || len(ids) > 50 || g.ServerToken == "" {
		return nil, ErrDenied
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != id || wanted[id] {
			return nil, ErrDenied
		}
		wanted[id] = true
	}

	var result struct {
		Container struct {
			plexMetadata
			Size     *int           `json:"size"`
			Metadata []plexMetadata `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := c.JSON(ctx, "GET", base+"/library/metadata/"+strings.Join(ids, ",")+"?skipRefresh=1&includeGuids=1&checkFiles=1&checkFileAvailability=1", plexHeaders(g.ClientID, g.ServerToken), "", &result); err != nil {
		return nil, err
	}
	if result.Container.Size == nil || *result.Container.Size != len(result.Container.Metadata) || len(result.Container.Metadata) > len(ids) {
		return nil, ErrIncomplete
	}
	out := map[string]PlexItem{}
	for _, m := range result.Container.Metadata {
		if !wanted[string(m.ID)] {
			return nil, ErrIncomplete
		}
		if _, exists := out[string(m.ID)]; exists {
			return nil, ErrIncomplete
		}
		item, err := validatedPlexMetadata(m, result.Container.plexMetadata)
		if err != nil {
			return nil, err
		}
		out[item.ID] = item
	}
	return out, nil
}

func validatedPlexMetadata(m, container plexMetadata) (PlexItem, error) {
	if m.Title == "" || m.GUID == "" || m.ViewCount < 0 || m.ViewOffset < 0 {
		return PlexItem{}, ErrIncomplete
	}

	if m.LibraryID == "" {
		m.LibraryID = container.LibraryID
	}
	if m.ParentID == "" {
		m.ParentID = container.ParentID
	}
	if m.GrandparentID == "" {
		m.GrandparentID = container.GrandparentID
	}
	if m.ParentTitle == "" {
		m.ParentTitle = container.ParentTitle
	}
	if m.GrandparentTitle == "" {
		m.GrandparentTitle = container.GrandparentTitle
	}
	if m.LibraryID == "" || (m.Kind == "season" && m.ParentID == "") || (m.Kind == "episode" && (m.ParentID == "" || m.GrandparentID == "")) {
		return PlexItem{}, ErrIncomplete
	}
	var tmdb int64
	for _, guid := range m.ExternalGUIDs {
		if strings.HasPrefix(guid.ID, "tmdb://") {
			id, err := strconv.ParseInt(strings.TrimPrefix(guid.ID, "tmdb://"), 10, 64)
			if err != nil || id <= 0 || (tmdb != 0 && tmdb != id) {
				return PlexItem{}, ErrIncomplete
			}
			tmdb = id
		}
	}
	var playback subscriptionPlexMetadata
	if len(m.Media) > 0 && json.Unmarshal(m.Media, &playback.Media) != nil {
		return PlexItem{}, ErrIncomplete
	}
	return PlexItem{ID: string(m.ID), ParentID: string(m.ParentID), GrandparentID: string(m.GrandparentID), LibraryID: string(m.LibraryID), Title: m.Title, ParentTitle: m.ParentTitle, GrandparentTitle: m.GrandparentTitle, Kind: m.Kind, GUID: m.GUID, Edition: m.Edition, ViewCount: m.ViewCount, ViewOffset: m.ViewOffset, TMDBID: tmdb, Index: m.Index, AddedAt: m.AddedAt, Playable: playableSubscriptionItem(playback), Thumb: m.Thumb}, nil
}
