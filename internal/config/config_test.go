package config

import (
	"strings"
	"testing"
)

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
