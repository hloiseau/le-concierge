package config

import (
	"path/filepath"
	"testing"
)

func TestMonitoringUsesConfiguredSourcesByDefault(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	t.Setenv("PLEX_URL", "http://plex.test")
	t.Setenv("PLEX_SERVER_ID", "server-a")
	t.Setenv("TAUTULLI_API_KEY_FILE", "")
	for _, tc := range []struct {
		name, url, key, file, flag string
		want                       bool
	}{
		{name: "no sources"},
		{name: "configured Tautulli", url: "http://tautulli.test", key: "fixture-key", want: true},
		{name: "configured collector", file: filepath.Join(t.TempDir(), "host.json"), want: true},
		{name: "explicit opt out", url: "http://tautulli.test", key: "fixture-key", flag: "false"},
		{name: "explicit enable", flag: "true", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TAUTULLI_URL", tc.url)
			t.Setenv("TAUTULLI_API_KEY", tc.key)
			t.Setenv("METRICS_FILE", tc.file)
			t.Setenv("MONITORING_ENABLED", tc.flag)
			c, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if c.MonitoringEnabled != tc.want {
				t.Fatalf("monitoring enabled = %v, want %v", c.MonitoringEnabled, tc.want)
			}
		})
	}
}
