package config

import "testing"

func TestShelfmarkRequiresSecurePersonalLoginURL(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("PUBLIC_ORIGIN", "https://hub.example")
	for _, value := range []string{"http://shelfmark.example", "https://user:secret@shelfmark.example", "https://shelfmark.example?token=secret", "https://shelfmark.example#session"} {
		t.Setenv("SHELFMARK_URL", value)
		if _, err := Load(); err == nil {
			t.Fatalf("accepted insecure Shelfmark URL %q", value)
		}
	}
	t.Setenv("SHELFMARK_URL", "https://shelfmark.example/base/")
	if c, err := Load(); err != nil || c.ShelfmarkURL != "https://shelfmark.example/base" {
		t.Fatalf("valid Shelfmark URL: %v", err)
	}
}
