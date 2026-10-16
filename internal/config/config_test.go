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
