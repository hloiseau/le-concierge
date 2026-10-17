package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMonitoringUsesConfiguredSourcesByDefault(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	t.Setenv("PLEX_URL", "http://plex.test")
	t.Setenv("PLEX_SERVER_ID", "server-a")
	t.Setenv("TAUTULLI_API_KEY_FILE", "")
	for _, tc := range []struct {
		name, url, key, file, flag, toparr string
		want                               bool
	}{
		{name: "no sources"},
		{name: "configured Tautulli", url: "http://tautulli.test", key: "fixture-key", want: true},
		{name: "configured collector", file: filepath.Join(t.TempDir(), "host.json"), want: true},
		{name: "configured Toparr", toparr: "http://toparr.test/", want: true},
		{name: "Toparr opt out", toparr: "http://toparr.test", flag: "false"},
		{name: "explicit opt out", url: "http://tautulli.test", key: "fixture-key", flag: "false"},
		{name: "explicit enable", flag: "true", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TAUTULLI_URL", tc.url)
			t.Setenv("TAUTULLI_API_KEY", tc.key)
			t.Setenv("METRICS_FILE", tc.file)
			t.Setenv("TOPARR_URL", tc.toparr)
			t.Setenv("MONITORING_ENABLED", tc.flag)
			c, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if c.MonitoringEnabled != tc.want {
				t.Fatalf("monitoring enabled = %v, want %v", c.MonitoringEnabled, tc.want)
			}
			if c.ToparrURL != strings.TrimRight(tc.toparr, "/") {
				t.Fatal("Toparr URL not normalized")
			}
		})
	}
}

func TestToparrRejectsUnsafeURL(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	for _, raw := range []string{"file:///etc/passwd", "http://user:secret@toparr.test", "http://toparr.test?token=secret"} {
		t.Setenv("TOPARR_URL", raw)
		if _, err := Load(); err == nil {
			t.Fatal("invalid Toparr URL accepted")
		}
	}
}
