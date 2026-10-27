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

func TestErisV2RequiresResponsesAndKeepsActivationExplicit(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	t.Setenv("ERIS_PROFILE", "v2")
	t.Setenv("ERIS_PROTOCOL", "responses")
	t.Setenv("ERIS_API_URL", "https://api.openai.com/v1")
	t.Setenv("ERIS_API_KEY", strings.Repeat("s", 32))
	t.Setenv("ERIS_API_KEY_FILE", "")
	t.Setenv("ERIS_MODEL", "")
	t.Setenv("ERIS_MEMORY_PATH", "data/character.db")
	if c, err := Load(); err != nil || c.ErisModel != "gpt-6-luna" || c.ErisProfile != "v2" {
		t.Fatal(c.ErisModel, err)
	}
	t.Setenv("ERIS_PROTOCOL", "chat-completions")
	if _, err := Load(); err == nil {
		t.Fatal("oversized v2 prompt allowed on legacy protocol")
	}
	t.Setenv("ERIS_PROTOCOL", "responses")
	t.Setenv("ERIS_API_URL", "http://remote.example/v1")
	if _, err := Load(); err == nil {
		t.Fatal("cloud token allowed over clear HTTP")
	}
}

func TestErisWebEndpointIsExplicitOperatorConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	for _, raw := range []string{"", "http://searxng:8080", "https://search.example/private"} {
		t.Setenv("ERIS_WEB_URL", raw)
		if c, err := Load(); err != nil || c.ErisWebURL != raw {
			t.Fatal("valid optional web configuration rejected", err)
		}
	}
	for _, raw := range []string{"https://user:secret@search.example", "https://search.example?token=secret", "file:///private", "https://search.example/#fragment"} {
		t.Setenv("ERIS_WEB_URL", raw)
		if _, err := Load(); err == nil || strings.Contains(err.Error(), "secret@") {
			t.Fatal("invalid web configuration accepted or credential disclosed")
		}
	}
}
