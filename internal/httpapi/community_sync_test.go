package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/community"
	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func TestCommunityExporterRequiresConfiguredSelectionAndWithdrawsOnFailure(t *testing.T) {
	dir := t.TempDir()
	s := &Server{Config: config.Config{CommunityFeedFile: filepath.Join(dir, "publication.json"), CommunitySyncManifest: filepath.Join(dir, "manifest.json"), CommunityForumIDs: []string{"11111"}, DiscordGuildID: "33333", DiscordBotToken: "fixture-secret"}, Upstream: upstream.New()}
	m := community.Manifest{Version: 1, GuildID: "33333", Forums: []community.ForumSelection{{ID: "11111", Kind: "problem", AllThreads: true, DefaultStatus: "open"}}}
	write := func(m community.Manifest) {
		t.Helper()
		b, _ := json.Marshal(m)
		if err := os.WriteFile(s.Config.CommunitySyncManifest, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bot fixture-secret" {
			t.Fatal("unexpected exporter request")
		}
		switch r.URL.Path {
		case "/api/v10/guilds/33333/roles":
			return response(200, `[{"id":"33333","permissions":"66560"}]`), nil
		case "/api/v10/channels/11111":
			return response(200, `{"id":"11111","guild_id":"33333","type":15,"nsfw":false,"permission_overwrites":[]}`), nil
		case "/api/v10/guilds/33333/threads/active":
			return response(200, `{"threads":[]}`), nil
		case "/api/v10/channels/11111/threads/archived/public":
			return response(200, `{"threads":[],"has_more":false}`), nil
		default:
			t.Fatal("unselected source read")
			return nil, nil
		}
	})
	write(m)
	now := time.Now().UTC()
	if err := s.syncCommunity(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := community.Load(s.Config.CommunityFeedFile, community.Selection{GuildID: "33333", ForumIDs: []string{"11111"}}, now); err != nil {
		t.Fatal("publication not readable", err)
	}
	initialCalls := calls
	for _, change := range []string{"guild", "forum", "issue", "malformed", "missing"} {
		t.Run(change, func(t *testing.T) {
			selected := m
			switch change {
			case "guild":
				selected.GuildID = "44444"
			case "forum":
				selected.Forums = []community.ForumSelection{{ID: "44444", Kind: "problem", AllThreads: true, DefaultStatus: "open"}}
			case "issue":
				selected.Issues = []community.IssueSelection{{IssueID: "private/project#1", ID: "not-selected", Title: "Never published"}}
			}
			write(selected)
			if change == "malformed" {
				_ = os.WriteFile(s.Config.CommunitySyncManifest, []byte(`{`), 0600)
			}
			if change == "missing" {
				_ = os.Remove(s.Config.CommunitySyncManifest)
			}
			_ = os.WriteFile(s.Config.CommunityFeedFile, []byte(`old publication`), 0600)
			if err := s.syncCommunity(t.Context(), now); err == nil {
				t.Fatal("invalid selection accepted")
			}
			if _, err := os.Stat(s.Config.CommunityFeedFile); !os.IsNotExist(err) || calls != initialCalls {
				t.Fatal("old publication retained or unselected source queried")
			}
		})
	}
}

func TestCommunityWorkerIsOptInAndStopsWithoutSourceCalls(t *testing.T) {
	s := &Server{Upstream: upstream.New()}
	s.Upstream.HTTP.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("disabled or cancelled exporter contacted a source")
		return nil, context.Canceled
	})
	s.RunCommunity(t.Context())
	s.Config.CommunitySyncManifest = filepath.Join(t.TempDir(), "missing.json")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s.RunCommunity(ctx)
}
