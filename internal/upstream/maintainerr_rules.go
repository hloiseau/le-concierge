package upstream

import (
	"context"
	"strconv"
)

type RemovalRule struct {
	ID         int64
	Active     bool
	Exclusions []RemovalExclusion
}
type RemovalExclusion struct {
	ID      int64   `json:"id"`
	MediaID *string `json:"mediaServerId"`
	GroupID *int64  `json:"ruleGroupId"`
	Parent  *string `json:"parent"`
	Kind    *string `json:"type"`
}

// Decode only the needed fields; rule responses also contain notification secrets.
func (c *Client) MaintainerrRules(ctx context.Context, base string) (map[int64]RemovalRule, error) {
	var groups []struct {
		ID           int64 `json:"id"`
		CollectionID int64 `json:"collectionId"`
		Active       *bool `json:"isActive"`
	}
	if err := c.JSON(ctx, "GET", base+"/api/rules", nil, "", &groups); err != nil {
		return nil, err
	}
	if groups == nil || len(groups) > 500 {
		return nil, ErrIncomplete
	}
	out := map[int64]RemovalRule{}
	seen := map[int64]bool{}
	count := 0
	for _, g := range groups {
		if g.ID <= 0 || seen[g.ID] || g.Active == nil {
			return nil, ErrIncomplete
		}
		seen[g.ID] = true
		if g.CollectionID <= 0 {
			continue
		}
		if _, duplicate := out[g.CollectionID]; duplicate {
			return nil, ErrIncomplete
		}
		rule := RemovalRule{ID: g.ID, Active: *g.Active}
		if rule.Active {
			if err := c.JSON(ctx, "GET", base+"/api/rules/exclusion?rulegroupId="+strconv.FormatInt(g.ID, 10), nil, "", &rule.Exclusions); err != nil {
				return nil, err
			}
			if rule.Exclusions == nil {
				return nil, ErrIncomplete
			}
			ids := map[int64]bool{}
			for _, e := range rule.Exclusions {
				count++
				if count > 10000 || e.ID <= 0 || ids[e.ID] || (e.GroupID != nil && *e.GroupID != g.ID) {
					return nil, ErrIncomplete
				}
				ids[e.ID] = true
				if e.Kind != nil && *e.Kind != "movie" && *e.Kind != "show" && *e.Kind != "season" && *e.Kind != "episode" {
					return nil, ErrIncomplete
				}
				if e.MediaID == nil || *e.MediaID == "" {
					return nil, ErrIncomplete
				}
			}
		}
		out[g.CollectionID] = rule
	}
	return out, nil
}

func (r RemovalRule) Excludes(m PlexItem) bool {
	for _, e := range r.Exclusions {
		if e.MediaID == nil {
			return true
		}
		id := *e.MediaID
		if id == m.ID {
			return true
		}
		if e.Kind == nil {
			if id == m.ParentID || id == m.GrandparentID || (e.Parent != nil && *e.Parent != "" && (*e.Parent == m.ID || *e.Parent == m.ParentID || *e.Parent == m.GrandparentID)) {
				return true
			}
		} else if *e.Kind == "show" && (id == m.ParentID || id == m.GrandparentID) {
			return true
		} else if *e.Kind == "season" && id == m.ParentID {
			return true
		}
	}
	return false
}
