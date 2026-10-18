package community

import (
	"context"
	"net/url"
	"time"
)

// Discovery only keeps identifiers; titles and access are checked by the existing per-thread reads.
type listedThread struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
	GuildID  string `json:"guild_id"`
	Type     int    `json:"type"`
	Metadata *struct {
		Archived  bool   `json:"archived"`
		Timestamp string `json:"archive_timestamp"`
	} `json:"thread_metadata"`
}

type threadListing struct {
	Threads *[]listedThread `json:"threads"`
	HasMore *bool           `json:"has_more"`
}

func (c SyncClient) forumThreads(ctx context.Context, guild, forum string, active threadListing) ([]string, error) {
	ids := []string{}
	seen := map[string]bool{}
	add := func(thread listedThread) error {
		if !ValidForumID(thread.ID) {
			return &SyncError{}
		}
		if !seen[thread.ID] {
			seen[thread.ID] = true
			ids = append(ids, thread.ID)
		}
		if len(ids) > 100 {
			return &SyncError{}
		}
		return nil
	}
	for _, thread := range *active.Threads {
		if thread.ParentID != forum || thread.Type != 11 || (thread.GuildID != "" && thread.GuildID != guild) {
			continue
		}
		if err := add(thread); err != nil {
			return nil, err
		}
	}
	var before time.Time
	for page := 0; page < 5; page++ {
		path := "/channels/" + forum + "/threads/archived/public?limit=100"
		if !before.IsZero() {
			path += "&before=" + url.QueryEscape(before.Format(time.RFC3339Nano))
		}
		var archive threadListing
		ok, err := c.read(ctx, "discord", path, &archive)
		if err != nil {
			return nil, err
		}
		if !ok || archive.Threads == nil || archive.HasMore == nil || len(*archive.Threads) > 100 {
			return nil, &SyncError{}
		}
		var oldest time.Time
		for _, thread := range *archive.Threads {
			if thread.ParentID != forum || thread.Type != 11 || (thread.GuildID != "" && thread.GuildID != guild) || thread.Metadata == nil || !thread.Metadata.Archived {
				return nil, &SyncError{}
			}
			stamp, err := time.Parse(time.RFC3339Nano, thread.Metadata.Timestamp)
			if err != nil || (!before.IsZero() && !stamp.Before(before)) || (!oldest.IsZero() && stamp.After(oldest)) {
				return nil, &SyncError{}
			}
			oldest = stamp
			if err := add(thread); err != nil {
				return nil, err
			}
		}
		if !*archive.HasMore {
			return ids, nil
		}
		if oldest.IsZero() {
			return nil, &SyncError{}
		}
		before = oldest
	}
	return nil, &SyncError{}
}
