package upstream

import (
	"context"
	"errors"
	"strconv"
	"time"
)

var ErrUnsupportedVersion = errors.New("upstream version is not supported by the verified contract")
var ErrIncomplete = errors.New("upstream snapshot is incomplete or inconsistent")

const MaintainerrVersion = "3.30.1"

// Removal is a source observation, not permission to notify or a guaranteed
// deletion: exclusions, parent membership and user access still need checking.
type Removal struct {
	MembershipID             int64
	AddedAt                  time.Time
	DelayDays                *int
	CollectionID             int64
	MediaID, LibraryID, Kind string
	Action                   int
	EstimatedAt              *time.Time
	RuleEvaluationFailed     bool
	Manual                   bool
}

type RemovalSnapshot struct {
	ServerID       string
	Version, Build string
	UpdatedAt      time.Time
	Collections    int
	Items          []Removal
}

func (c *Client) MaintainerrRemovals(ctx context.Context, base, serverID string) (RemovalSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if serverID == "" {
		return RemovalSnapshot{}, ErrDenied
	}
	var app struct {
		Status  int    `json:"status"`
		Version string `json:"version"`
		Build   string `json:"commitTag"`
	}
	if err := c.JSON(ctx, "GET", base+"/api/app/status", nil, "", &app); err != nil {
		return RemovalSnapshot{}, err
	}
	if app.Status != 1 {
		return RemovalSnapshot{}, ErrUnavailable
	}
	if app.Version != MaintainerrVersion {
		return RemovalSnapshot{}, ErrUnsupportedVersion
	}
	var kind struct {
		Type string `json:"type"`
	}
	if err := c.JSON(ctx, "GET", base+"/api/media-server/type", nil, "", &kind); err != nil {
		return RemovalSnapshot{}, err
	}
	if kind.Type != "plex" {
		return RemovalSnapshot{}, ErrDenied
	}
	var server struct {
		ID string `json:"machineId"`
	}
	if err := c.JSON(ctx, "GET", base+"/api/media-server", nil, "", &server); err != nil {
		return RemovalSnapshot{}, err
	}
	if server.ID != serverID {
		return RemovalSnapshot{}, ErrDenied
	}
	var collections []struct {
		ID         int64  `json:"id"`
		ServerType string `json:"mediaServerType"`
		LibraryID  string `json:"libraryId"`
		Kind       string `json:"type"`
		Active     *bool  `json:"isActive"`
		Action     *int   `json:"arrAction"`
		Days       *int   `json:"deleteAfterDays"`
		Count      *int   `json:"mediaCount"`
		Media      []struct {
			ID                   int64     `json:"id"`
			CollectionID         int64     `json:"collectionId"`
			MediaID              string    `json:"mediaServerId"`
			AddedAt              time.Time `json:"addDate"`
			RuleEvaluationFailed *bool     `json:"ruleEvaluationFailed"`
			Manual               *bool     `json:"isManual"`
			ManualSource         *string   `json:"manualMembershipSource"`
		} `json:"media"`
	}
	// Unlike the paged content route, overlay-data does not enrich metadata or
	// clear stale Plex links. The shared client bounds the full response to 2 MiB.
	if err := c.JSON(ctx, "GET", base+"/api/collections/overlay-data", nil, "", &collections); err != nil {
		return RemovalSnapshot{}, err
	}
	if collections == nil || len(collections) > 500 {
		return RemovalSnapshot{}, ErrIncomplete
	}
	out := RemovalSnapshot{Version: app.Version, Build: app.Build, ServerID: serverID, Collections: len(collections), Items: []Removal{}}
	seenCollections := map[int64]bool{}
	seenRows := map[int64]bool{}
	for _, collection := range collections {
		if collection.ID <= 0 || seenCollections[collection.ID] || collection.Active == nil || collection.Action == nil || collection.Count == nil || collection.Media == nil || *collection.Count != len(collection.Media) {
			return RemovalSnapshot{}, ErrIncomplete
		}
		seenCollections[collection.ID] = true
		if !*collection.Active {
			continue
		}
		action := *collection.Action
		if action != 0 && action != 1 && action != 2 && action != 5 {
			continue
		}
		if collection.ServerType != "plex" || collection.LibraryID == "" {
			return RemovalSnapshot{}, ErrDenied
		}
		if collection.Kind != "movie" && collection.Kind != "show" && collection.Kind != "season" && collection.Kind != "episode" {
			return RemovalSnapshot{}, ErrIncomplete
		}
		if action == 5 && collection.Kind != "season" {
			continue
		}
		seenMedia := map[string]bool{}
		for _, member := range collection.Media {
			id, err := strconv.ParseInt(member.MediaID, 10, 64)
			if err != nil || id <= 0 || member.ID <= 0 || member.CollectionID != collection.ID || member.RuleEvaluationFailed == nil || member.Manual == nil || seenRows[member.ID] || seenMedia[member.MediaID] || len(seenRows) >= 10000 {
				return RemovalSnapshot{}, ErrIncomplete
			}
			seenRows[member.ID], seenMedia[member.MediaID] = true, true
			item := Removal{CollectionID: collection.ID, MembershipID: member.ID, MediaID: member.MediaID, LibraryID: collection.LibraryID, Kind: collection.Kind, Action: action, AddedAt: member.AddedAt, DelayDays: collection.Days, RuleEvaluationFailed: *member.RuleEvaluationFailed, Manual: *member.Manual || member.ManualSource != nil}
			// Zero/null/negative delays never become a confident deadline. The
			// worker uses elapsed 24-hour days, including across DST changes.
			if collection.Days != nil && *collection.Days > 0 && *collection.Days <= 36500 && !member.AddedAt.IsZero() {
				due := member.AddedAt.Add(time.Duration(*collection.Days) * 24 * time.Hour).UTC()
				item.EstimatedAt = &due
			}
			out.Items = append(out.Items, item)
		}
	}
	out.UpdatedAt = time.Now().UTC()
	return out, nil
}
