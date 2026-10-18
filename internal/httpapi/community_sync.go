package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/hloiseau/le-concierge/internal/community"
)

// Opt-in in-process exporter: one bounded refresh at a time, using the existing bot credential.
func (s *Server) RunCommunity(ctx context.Context) {
	if s.Config.CommunitySyncManifest == "" {
		return
	}
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := s.syncCommunity(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
			slog.Warn("Community refresh deferred; current selection and public source access required")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) syncCommunity(ctx context.Context, now time.Time) error {
	m, err := community.ReadManifest(s.Config.CommunitySyncManifest)
	if err == nil {
		if len(m.Forums) > 0 && m.GuildID != s.Config.DiscordGuildID {
			err = errors.New("publication guild is not configured")
		}
		for _, forum := range m.Forums {
			if !slices.Contains(s.Config.CommunityForumIDs, forum.ID) {
				err = errors.New("publication forum is not configured")
			}
		}
		for _, issue := range m.Issues {
			if !slices.ContainsFunc(s.Config.RoadmapIssueIDs, func(id string) bool { return strings.EqualFold(id, issue.IssueID) }) {
				err = errors.New("publication issue is not configured")
			}
		}
	}
	if err != nil {
		if withdraw := community.Withdraw(s.Config.CommunityFeedFile); withdraw != nil {
			return withdraw
		}
		return err
	}
	client := community.SyncClient{HTTP: s.Upstream.HTTP, DiscordToken: s.Config.DiscordBotToken}
	return client.SyncToFile(ctx, m, s.Config.CommunityFeedFile, now)
}
