package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscordDMRequiresExplicitServerEnable(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	t.Setenv("DISCORD_DMS_ENABLED", "")
	t.Setenv("DISCORD_BOT_TOKEN", "fixture-token")
	t.Setenv("DISCORD_BOT_TOKEN_FILE", "")
	if c, err := Load(); err != nil || c.DiscordDMsEnabled {
		t.Fatal("bot token silently enabled DMs", err)
	}
	t.Setenv("DISCORD_DMS_ENABLED", "true")
	if c, err := Load(); err != nil || !c.DiscordDMsEnabled {
		t.Fatal("explicit enable failed", err)
	}
	t.Setenv("DISCORD_BOT_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("DMs enabled without token")
	}
	t.Setenv("DISCORD_DMS_ENABLED", "maybe")
	if _, err := Load(); err == nil {
		t.Fatal("invalid boolean accepted")
	}
}

func TestOptionalBooksAndMonitoringConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("PUBLIC_ORIGIN", "https://hub.example")
	t.Setenv("GRIMMORY_URL", "http://books.example")
	if _, err := Load(); err == nil {
		t.Fatal("personal book credentials allowed over plaintext")
	}
	t.Setenv("GRIMMORY_URL", "https://books.example")
	t.Setenv("METRICS_FILE", "relative/snapshot.json")
	if _, err := Load(); err == nil {
		t.Fatal("relative metrics file accepted")
	}
	t.Setenv("METRICS_FILE", filepath.Join(t.TempDir(), "metrics.json"))
	t.Setenv("MONITORING_ENABLED", "invalid")
	if _, err := Load(); err == nil {
		t.Fatal("invalid monitoring switch accepted")
	}
	t.Setenv("MONITORING_ENABLED", "false")
	if c, err := Load(); err != nil || c.MonitoringEnabled || c.GrimmoryURL != "https://books.example" {
		t.Fatalf("optional setup: %v", err)
	}
}

func TestDiagnosticCredentialsMustMatchTheirService(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	for _, service := range []string{"RADARR", "SONARR"} {
		t.Run(service, func(t *testing.T) {
			t.Setenv(service+"_URL", "http://arr.test")
			t.Setenv(service+"_API_KEY", "")
			t.Setenv(service+"_API_KEY_FILE", "")
			if _, err := Load(); err == nil {
				t.Fatal("service accepted without credentials")
			}
			file := filepath.Join(t.TempDir(), "key")
			if err := os.WriteFile(file, []byte("fixture-key\n"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv(service+"_API_KEY_FILE", file)
			c, err := Load()
			if err != nil || (service == "RADARR" && c.RadarrAPIKey != "fixture-key") || (service == "SONARR" && c.SonarrAPIKey != "fixture-key") {
				t.Fatalf("file credential: %v", err)
			}
			t.Setenv(service+"_URL", "")
			if _, err := Load(); err == nil {
				t.Fatal("orphan credential accepted")
			}
		})
	}
}

func TestCommunityConfigurationRequiresExplicitValidSelections(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	t.Setenv("COMMUNITY_FEED_FILE", filepath.Join(t.TempDir(), "community.json"))
	t.Setenv("COMMUNITY_FORUM_IDS", "12345")
	t.Setenv("DISCORD_GUILD_ID", "")
	if _, err := Load(); err == nil {
		t.Fatal("forum accepted without guild")
	}
	t.Setenv("DISCORD_GUILD_ID", "67890")
	t.Setenv("ROADMAP_ISSUE_IDS", "hloiseau/le-concierge#23")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"12345,12345", "https://discord.com/channels/67890/12345", "*"} {
		t.Setenv("COMMUNITY_FORUM_IDS", value)
		if _, err := Load(); err == nil {
			t.Fatalf("invalid forum selection accepted: %s", value)
		}
	}
	t.Setenv("COMMUNITY_FORUM_IDS", "12345")
	t.Setenv("ROADMAP_ISSUE_IDS", "*")
	if _, err := Load(); err == nil {
		t.Fatal("wildcard issue publication accepted")
	}
}

func TestRemovalSourceConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	t.Setenv("MAINTAINERR_URL", "http://maintainerr.test")
	if _, err := Load(); err == nil {
		t.Fatal("sources accepted without Plex server identity")
	}
	t.Setenv("PLEX_URL", "http://plex.test")
	t.Setenv("PLEX_SERVER_ID", "server-a")
	t.Setenv("TAUTULLI_URL", "http://tautulli.test")
	if _, err := Load(); err == nil {
		t.Fatal("Tautulli accepted without key")
	}
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte("test-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TAUTULLI_API_KEY_FILE", keyFile)
	c, err := Load()
	if err != nil || c.TautulliAPIKey != "test-secret" || c.MaintainerrURL != "http://maintainerr.test" {
		t.Fatalf("secret file configuration failed: %v", err)
	}
	t.Setenv("TAUTULLI_API_KEY", "test-secret")
	if _, err := Load(); err == nil {
		t.Fatal("ambiguous key configuration accepted")
	}
	t.Setenv("TAUTULLI_API_KEY", "")
	t.Setenv("MAINTAINERR_URL", "http://user:secret@maintainerr.test")
	if _, err := Load(); err == nil {
		t.Fatal("credentials in service URL accepted")
	}
}

func TestValidation(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	if _, e := Load(); e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{"javascript:alert(1)", "https://user:pass@example.com", "https://example.com/path", "http://evil.example"} {
		t.Setenv("PUBLIC_ORIGIN", v)
		if _, e := Load(); e == nil {
			t.Fatalf("accepted %s", v)
		}
	}
	t.Setenv("PUBLIC_ORIGIN", "https://hub.example.com")
	t.Setenv("APP_ENV", "production")
	t.Setenv("SEERR_API_KEY", "secret")
	t.Setenv("SEERR_API_KEY_FILE", "/not-read")
	if _, e := Load(); e == nil {
		t.Fatal("ambiguous secret accepted")
	}
	t.Setenv("SEERR_API_KEY", "")
	t.Setenv("SEERR_API_KEY_FILE", "")
	t.Setenv("DISCORD_CLIENT_ID", "12345")
	t.Setenv("DISCORD_GUILD_ID", "22222")
	t.Setenv("DISCORD_PUBLIC_KEY", strings.Repeat("a", 64))
	t.Setenv("BOT_API_SECRET", strings.Repeat("x", 32))
	if c, e := Load(); e != nil || c.OAuthEnabled() {
		t.Fatalf("bot-only config should work: %v", e)
	}
}
