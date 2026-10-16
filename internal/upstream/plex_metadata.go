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

// Use the user's server token. skipRefresh avoids an implicit agent refresh.
func (c *Client) PlexMetadata(ctx context.Context, base, id string, g Grant) (PlexItem, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != id || g.ServerToken == "" {
		return PlexItem{}, ErrDenied
	}
	type metadata struct {
		ID               tautulliID `json:"ratingKey"`
		ParentID         tautulliID `json:"parentRatingKey"`
		GrandparentID    tautulliID `json:"grandparentRatingKey"`
		LibraryID        tautulliID `json:"librarySectionID"`
		Title            string     `json:"title"`
		ParentTitle      string     `json:"parentTitle"`
		GrandparentTitle string     `json:"grandparentTitle"`
		Kind             string     `json:"type"`
		GUID             string     `json:"guid"`
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
	var result struct {
		Container struct {
			metadata
			Size     *int       `json:"size"`
			Metadata []metadata `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := c.JSON(ctx, "GET", base+"/library/metadata/"+id+"?skipRefresh=1&includeGuids=1&checkFiles=1&checkFileAvailability=1", plexHeaders(g.ClientID, g.ServerToken), "", &result); err != nil {
		return PlexItem{}, err
	}
	if result.Container.Size == nil || *result.Container.Size != 1 || len(result.Container.Metadata) != 1 {
		return PlexItem{}, ErrIncomplete
	}
	m := result.Container.Metadata[0]
	if string(m.ID) != id || m.Title == "" || m.GUID == "" || m.ViewCount < 0 || m.ViewOffset < 0 {
		return PlexItem{}, ErrIncomplete
	}
	if m.LibraryID == "" {
		m.LibraryID = result.Container.LibraryID
	}
	if m.ParentID == "" {
		m.ParentID = result.Container.ParentID
	}
	if m.GrandparentID == "" {
		m.GrandparentID = result.Container.GrandparentID
	}
	if m.ParentTitle == "" {
		m.ParentTitle = result.Container.ParentTitle
	}
	if m.GrandparentTitle == "" {
		m.GrandparentTitle = result.Container.GrandparentTitle
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
	return PlexItem{ID: string(m.ID), ParentID: string(m.ParentID), GrandparentID: string(m.GrandparentID), LibraryID: string(m.LibraryID), Title: m.Title, ParentTitle: m.ParentTitle, GrandparentTitle: m.GrandparentTitle, Kind: m.Kind, GUID: m.GUID, Edition: m.Edition, ViewCount: m.ViewCount, ViewOffset: m.ViewOffset, TMDBID: tmdb, Index: m.Index, AddedAt: m.AddedAt, Playable: playableSubscriptionItem(playback)}, nil
}
