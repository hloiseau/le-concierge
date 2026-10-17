package config

import "testing"

func TestChannelRemindersNeedIndependentExplicitDestination(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	t.Setenv("DISCORD_DMS_ENABLED", "false")
	t.Setenv("DISCORD_NOTIFICATION_CHANNEL_ID", "12345")
	t.Setenv("DISCORD_BOT_TOKEN", "")
	t.Setenv("DISCORD_BOT_TOKEN_FILE", "")
	if c, err := Load(); err != nil || c.DiscordNotificationChannelID != "" {
		t.Fatal("missing optional bot token must disable channel reminders without blocking startup", err)
	}
	t.Setenv("DISCORD_BOT_TOKEN", "fixture")
	t.Setenv("DISCORD_GUILD_ID", "67890")
	t.Setenv("PLEX_URL", "https://plex.example")
	t.Setenv("PLEX_SERVER_ID", "server-a")
	t.Setenv("MAINTAINERR_URL", "https://maintainerr.example")
	t.Setenv("SEERR_URL", "https://seerr.example")
	t.Setenv("SEERR_API_KEY", "fixture")
	c, err := Load()
	if err != nil || c.DiscordNotificationChannelID != "12345" || c.DiscordDMsEnabled {
		t.Fatal("channel and DM configuration coupled", err)
	}
	for _, missing := range []string{"DISCORD_GUILD_ID", "PLEX_URL", "PLEX_SERVER_ID", "MAINTAINERR_URL", "SEERR_URL", "SEERR_API_KEY"} {
		t.Run("token present, missing "+missing, func(t *testing.T) {
			t.Setenv(missing, "")
			if _, err := Load(); err == nil {
				t.Fatal("configured bot accepted incomplete channel prerequisites")
			}
		})
	}
	t.Setenv("DISCORD_NOTIFICATION_CHANNEL_ID", "@everyone")
	if _, err := Load(); err == nil {
		t.Fatal("non-ID channel accepted")
	}
	t.Setenv("DISCORD_BOT_TOKEN", "")
	if _, err := Load(); err == nil || err.Error() != "DISCORD_NOTIFICATION_CHANNEL_ID requires a channel ID" {
		t.Fatal("missing token must not mask an invalid channel ID", err)
	}
}
