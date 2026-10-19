package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestErisConfigurationRequiresExplicitEndpointAndKey(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	t.Setenv("ERIS_API_URL", "")
	t.Setenv("ERIS_API_KEY", "")
	t.Setenv("ERIS_API_KEY_FILE", "")
	if c, err := Load(); err != nil || c.ErisAPIURL != "" || c.DiscordBotName != "Eris" {
		t.Fatal("optional chat changed default startup", err)
	}
	t.Setenv("ERIS_API_URL", "http://eris-runtime:8090/v1")
	if _, err := Load(); err == nil {
		t.Fatal("chat accepted without shared key")
	}
	file := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(file, []byte(strings.Repeat("s", 32)), 0600)
	t.Setenv("ERIS_API_KEY_FILE", file)
	if c, err := Load(); err != nil || len(c.ErisAPIKey) != 32 {
		t.Fatal("file secret failed", err)
	}
	t.Setenv("ERIS_API_URL", "https://secret@model.example/v1")
	if _, err := Load(); err == nil {
		t.Fatal("URL credentials accepted")
	}
	t.Setenv("ERIS_API_URL", "")
	t.Setenv("DISCORD_BOT_NAME", "disabled")
	if c, err := Load(); err != nil || c.DiscordBotName != "" {
		t.Fatal("identity opt-out failed", err)
	}
}
