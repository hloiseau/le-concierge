package config

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestMissingDiscordBotTokenDisablesOnlyOptionalNotifications(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	t.Setenv("DISCORD_BOT_TOKEN", "")
	t.Setenv("DISCORD_BOT_TOKEN_FILE", "")
	t.Setenv("BOT_API_SECRET", "fixture-private-api-secret")
	t.Setenv("BOT_API_SECRET_FILE", "")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	for _, tc := range []struct {
		name, dm, channel string
		warnings          int
	}{
		{"disabled", "false", "", 0},
		{"DM only", "true", "", 1},
		{"channel only", "false", "12345", 1},
		{"DM and channel", "true", "12345", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DISCORD_DMS_ENABLED", tc.dm)
			t.Setenv("DISCORD_NOTIFICATION_CHANNEL_ID", tc.channel)
			logs.Reset()
			c, err := Load()
			if err != nil || c.DiscordDMsEnabled || c.DiscordNotificationChannelID != "" {
				t.Fatalf("optional notifications blocked startup or remained enabled: %v", err)
			}
			if c.PublicOrigin != "http://localhost:3000" || c.BotAPISecret != "fixture-private-api-secret" {
				t.Fatal("unrelated application configuration changed")
			}
			lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
			if logs.Len() == 0 {
				lines = nil
			}
			if len(lines) != tc.warnings {
				t.Fatalf("warnings=%d, want %d", len(lines), tc.warnings)
			}
			for _, line := range lines {
				var event map[string]string
				if err := json.Unmarshal([]byte(line), &event); err != nil {
					t.Fatal(err)
				}
				if len(event) != 3 || event["level"] != "WARN" {
					t.Fatal("warning must contain only timestamp, level and a fixed safe message")
				}
				switch event["msg"] {
				case "Discord DMs disabled: DISCORD_BOT_TOKEN is not configured", "Discord channel reminders disabled: DISCORD_BOT_TOKEN is not configured":
				default:
					t.Fatal("warning is not a fixed safe configuration message")
				}
			}
		})
	}
}
