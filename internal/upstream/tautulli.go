package upstream

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const TautulliVersion = "2.18.2"

// Tautulli returns IDs as either integers or strings; movie parent keys are empty.
type tautulliID string

func (id *tautulliID) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" || string(raw) == `""` {
		*id = ""
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return err
	}
	n, err := number.Int64()
	if err != nil || n < 0 {
		return ErrIncomplete
	}
	*id = tautulliID(strconv.FormatInt(n, 10))
	return nil
}

type Playback struct {
	ID                                           int64
	MediaID, ParentID, GrandparentID, Kind, GUID string
	Percent                                      float64
	Completed                                    bool
}

// History contains recorded observations only. An absent row never proves
// "unwatched", and current playback is intentionally excluded from this history.
type History struct {
	ServerID  string
	PlexID    string
	Version   string
	UpdatedAt time.Time
	Items     []Playback
}

func (c *Client) tautulli(ctx context.Context, base, key, command string, query url.Values, out any) error {
	if key == "" {
		return ErrDenied
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("cmd", command)
	var envelope struct {
		Response struct {
			Result string          `json:"result"`
			Data   json.RawMessage `json:"data"`
		} `json:"response"`
	}
	// v2.18.2 supports a header: never put the key into URLs or diagnostic output.
	if err := c.JSON(ctx, "GET", base+"/api/v2?"+query.Encode(), map[string]string{"X-Api-Key": key}, "", &envelope); err != nil {
		return err
	}
	if envelope.Response.Result != "success" || len(envelope.Response.Data) == 0 || string(envelope.Response.Data) == "null" {
		return ErrUnavailable
	}
	if json.Unmarshal(envelope.Response.Data, out) != nil {
		return ErrUnavailable
	}
	return nil
}

func (c *Client) TautulliStatus(ctx context.Context, base, key, serverID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if serverID == "" {
		return "", ErrDenied
	}
	var app struct {
		Version string `json:"tautulli_version"`
	}
	if err := c.tautulli(ctx, base, key, "get_tautulli_info", nil, &app); err != nil {
		return "", err
	}
	if strings.TrimPrefix(app.Version, "v") != TautulliVersion {
		return "", ErrUnsupportedVersion
	}
	var server struct {
		ID string `json:"pms_identifier"`
	}
	if err := c.tautulli(ctx, base, key, "get_server_info", nil, &server); err != nil {
		return "", err
	}
	if server.ID != serverID {
		return "", ErrDenied
	}
	return app.Version, nil
}

func (c *Client) TautulliHistory(ctx context.Context, base, key, serverID, plexID string) (History, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	id, err := strconv.ParseInt(plexID, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != plexID {
		return History{}, ErrDenied
	}
	version, err := c.TautulliStatus(ctx, base, key, serverID)
	if err != nil {
		return History{}, err
	}
	var users []struct {
		ID          tautulliID `json:"user_id"`
		Active      *int       `json:"is_active"`
		KeepHistory *int       `json:"keep_history"`
	}
	// get_users excludes deleted accounts without refreshing Plex or writing to
	// Tautulli. get_user can refresh users as a side effect when an ID is absent.
	if err = c.tautulli(ctx, base, key, "get_users", nil, &users); err != nil {
		return History{}, err
	}
	found := false
	for _, user := range users {
		if string(user.ID) != plexID {
			continue
		}
		if found {
			return History{}, ErrIncomplete
		}
		if user.Active == nil || *user.Active != 1 {
			return History{}, ErrDenied
		}
		if user.KeepHistory == nil || *user.KeepHistory != 1 {
			return History{}, ErrIncomplete
		}
		found = true
	}
	if !found {
		return History{}, ErrDenied
	}
	out := History{Version: version, ServerID: serverID, PlexID: plexID, Items: []Playback{}}
	seen := map[int64]bool{}
	total := -1
	for start := 0; start <= 5000; {
		var page struct {
			Total *int `json:"recordsFiltered"`
			Data  []struct {
				ID            tautulliID `json:"row_id"`
				UserID        tautulliID `json:"user_id"`
				MediaID       tautulliID `json:"rating_key"`
				ParentID      tautulliID `json:"parent_rating_key"`
				GrandparentID tautulliID `json:"grandparent_rating_key"`
				Kind          string     `json:"media_type"`
				GUID          string     `json:"guid"`
				Percent       *float64   `json:"percent_complete"`
				Watched       *float64   `json:"watched_status"`
				Live          *int       `json:"live"`
			} `json:"data"`
		}
		q := url.Values{"user_id": {plexID}, "grouping": {"0"}, "include_activity": {"0"}, "start": {strconv.Itoa(start)}, "length": {"100"}, "order_column": {"date"}, "order_dir": {"asc"}}
		if err = c.tautulli(ctx, base, key, "get_history", q, &page); err != nil {
			return History{}, err
		}
		if page.Total == nil || *page.Total < 0 || *page.Total > 5000 || (total >= 0 && total != *page.Total) || page.Data == nil || len(page.Data) > 100 || start+len(page.Data) > *page.Total {
			return History{}, ErrIncomplete
		}
		total = *page.Total
		for _, row := range page.Data {
			rowID, err := strconv.ParseInt(string(row.ID), 10, 64)
			if err != nil || rowID <= 0 || seen[rowID] {
				return History{}, ErrIncomplete
			}
			if string(row.UserID) != plexID {
				return History{}, ErrDenied
			}
			seen[rowID] = true
			if row.Live == nil || (*row.Live != 0 && *row.Live != 1) {
				return History{}, ErrIncomplete
			}
			if *row.Live != 0 || (row.Kind != "movie" && row.Kind != "episode") {
				continue
			}
			mediaID, err := strconv.ParseInt(string(row.MediaID), 10, 64)
			if err != nil || mediaID <= 0 || row.Percent == nil || *row.Percent < 0 || row.Watched == nil || (*row.Watched != 0 && *row.Watched != .25 && *row.Watched != .5 && *row.Watched != .75 && *row.Watched != 1) {
				return History{}, ErrIncomplete
			}
			out.Items = append(out.Items, Playback{ID: rowID, MediaID: string(row.MediaID), ParentID: string(row.ParentID), GrandparentID: string(row.GrandparentID), Kind: row.Kind, GUID: row.GUID, Percent: *row.Percent, Completed: *row.Watched == 1})
		}
		start += len(page.Data)
		if start == total {
			out.UpdatedAt = time.Now().UTC()
			return out, nil
		}
		if len(page.Data) == 0 {
			return History{}, ErrIncomplete
		}
	}
	return History{}, ErrIncomplete
}
