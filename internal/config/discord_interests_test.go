package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func interestConfig(t *testing.T) []DiscordInterest {
	t.Helper()
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	t.Setenv("DISCORD_BOT_TOKEN", "fixture-token")
	t.Setenv("DISCORD_BOT_TOKEN_FILE", "")
	t.Setenv("DISCORD_CLIENT_ID", "12345")
	t.Setenv("DISCORD_GUILD_ID", "23456")
	t.Setenv("DISCORD_CLIENT_SECRET", "")
	t.Setenv("DISCORD_PUBLIC_KEY", "")
	t.Setenv("DISCORD_INTERESTS_CHANNEL_ID", "34567")
	t.Setenv("PLEX_URL", "")
	t.Setenv("PLEX_SERVER_ID", "")
	items := []DiscordInterest{{Key: "films", Label: "Films et séries", RoleID: "45678", ChannelID: "56789"}}
	setInterestJSON(t, items)
	return items
}

func setInterestJSON(t *testing.T, items []DiscordInterest) {
	t.Helper()
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISCORD_INTERESTS_JSON", string(raw))
}

func TestDiscordInterestsRequireExplicitPairAndDiscordIdentity(t *testing.T) {
	for _, tc := range []struct {
		name           string
		env            map[string]string
		valid, enabled bool
	}{
		{"disabled", map[string]string{"DISCORD_INTERESTS_JSON": "", "DISCORD_INTERESTS_CHANNEL_ID": "", "DISCORD_BOT_TOKEN": "", "DISCORD_CLIENT_ID": "", "DISCORD_GUILD_ID": ""}, true, false},
		{"configured without Plex", nil, true, true},
		{"missing list", map[string]string{"DISCORD_INTERESTS_JSON": ""}, false, false},
		{"missing menu channel", map[string]string{"DISCORD_INTERESTS_CHANNEL_ID": ""}, false, false},
		{"bad menu channel", map[string]string{"DISCORD_INTERESTS_CHANNEL_ID": "menu"}, false, false},
		{"menu channel mention", map[string]string{"DISCORD_INTERESTS_CHANNEL_ID": "<#34567>"}, false, false},
		{"guild as menu channel", map[string]string{"DISCORD_INTERESTS_CHANNEL_ID": "23456"}, false, false},
		{"missing token", map[string]string{"DISCORD_BOT_TOKEN": ""}, false, false},
		{"blank token", map[string]string{"DISCORD_BOT_TOKEN": " \t"}, false, false},
		{"missing guild", map[string]string{"DISCORD_GUILD_ID": ""}, false, false},
		{"bad guild", map[string]string{"DISCORD_GUILD_ID": "guild"}, false, false},
		{"missing client", map[string]string{"DISCORD_CLIENT_ID": ""}, false, false},
		{"bad client", map[string]string{"DISCORD_CLIENT_ID": "client"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			interestConfig(t)
			for name, value := range tc.env {
				t.Setenv(name, value)
			}
			c, err := Load()
			if (err == nil) != tc.valid || (err == nil && (len(c.DiscordInterests) > 0) != tc.enabled) {
				t.Fatalf("valid=%v enabled=%v: %v", tc.valid, tc.enabled, err)
			}
			if tc.enabled && (c.PlexURL != "" || c.PlexServerID != "" || c.DiscordInterestsChannelID != "34567") {
				t.Fatal("interests unexpectedly require Plex or lost the menu channel")
			}
		})
	}
}

func TestDiscordInterestsUseExistingBotSecretFile(t *testing.T) {
	interestConfig(t)
	t.Setenv("DISCORD_BOT_TOKEN", "")
	path := filepath.Join(t.TempDir(), "bot-token")
	if err := os.WriteFile(path, []byte("fixture-file-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISCORD_BOT_TOKEN_FILE", path)
	if c, err := Load(); err != nil || len(c.DiscordInterests) != 1 || c.DiscordBotToken != "fixture-file-token" {
		t.Fatal("existing bot token file not reused", err)
	}
}

func TestDiscordInterestsJSONIsStrictAndBounded(t *testing.T) {
	interestConfig(t)
	valid := os.Getenv("DISCORD_INTERESTS_JSON")
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"one interest", valid, true},
		{"exact maximum bytes", valid + strings.Repeat(" ", 64*1024-len(valid)), true},
		{"oversized", valid + strings.Repeat(" ", 64*1024-len(valid)+1), false},
		{"empty array", `[]`, false},
		{"null array", `null`, false},
		{"object instead of array", `{}`, false},
		{"null item", `[null]`, false},
		{"trailing object", valid + `{}`, false},
		{"trailing null", valid + ` null`, false},
		{"trailing garbage", valid + ` invalid`, false},
		{"unknown field", strings.Replace(valid, `"key":`, `"secret":"do-not-print-this","key":`, 1), false},
		{"wrong type", strings.Replace(valid, `"roleId":"45678"`, `"roleId":45678`, 1), false},
		{"missing field", `[ {"key":"films","label":"Films","roleId":"45678"} ]`, false},
		{"invalid UTF-8", string([]byte{'[', '"', 0xff, '"', ']'}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DISCORD_INTERESTS_JSON", tc.raw)
			_, err := Load()
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
			if err != nil && strings.Contains(err.Error(), "do-not-print-this") {
				t.Fatal("configuration content exposed in error")
			}
		})
	}
	for _, count := range []int{25, 26} {
		items := []DiscordInterest{}
		for i := 0; i < count; i++ {
			items = append(items, DiscordInterest{Key: fmt.Sprintf("interest-%d", i), Label: "Interest", RoleID: fmt.Sprint(10000 + i), ChannelID: fmt.Sprint(20000 + i)})
		}
		setInterestJSON(t, items)
		c, err := Load()
		if count == 25 && (err != nil || len(c.DiscordInterests) != 25) || count == 26 && err == nil {
			t.Fatalf("interest count %d: %v", count, err)
		}
	}
}

func TestDiscordInterestKeysAndLabels(t *testing.T) {
	for _, tc := range []struct {
		name, key, label string
		valid            bool
	}{
		{"trimmed accented label", "films-4k", "  Films, séries & documentaires (4K)  ", true},
		{"one character", "a", "A", true},
		{"maximum key", "a" + strings.Repeat("1", 31), "Films", true},
		{"maximum label runes", "films", strings.Repeat("é", 80), true},
		{"combining accent", "films", "Se\u0301ries", true},
		{"simple punctuation", "films", "L’actualité : films/séries - animation + SF !", true},
		{"empty key", "", "Films", false},
		{"uppercase key", "Films", "Films", false},
		{"non ASCII key", "séries", "Séries", false},
		{"digit prefix", "4k", "Films", false},
		{"key separator", "film:roles", "Films", false},
		{"key spaces", "films tv", "Films", false},
		{"key too long", "a" + strings.Repeat("1", 32), "Films", false},
		{"empty label", "films", "", false},
		{"blank label", "films", "  ", false},
		{"long label", "films", strings.Repeat("é", 81), false},
		{"newline", "films", "Films\nSéries", false},
		{"leading control", "films", "\tFilms", false},
		{"null control", "films", "Films\x00", false},
		{"invisible format", "films", "Films\u200b", false},
		{"bidi override", "films", "Films\u202e", false},
		{"paragraph separator", "films", "Films\u2029Séries", false},
		{"leading line separator", "films", "\u2028Films", false},
		{"everyone mention", "films", "@everyone", false},
		{"role mention", "films", "<@&45678>", false},
		{"channel mention", "films", "<#56789>", false},
		{"Markdown link", "films", "[Films](https://example.com)", false},
		{"Markdown bold", "films", "**Films**", false},
		{"Markdown italic", "films", "_Films_", false},
		{"Markdown code", "films", "`Films`", false},
		{"Markdown heading", "films", "# Films", false},
		{"Markdown spoiler", "films", "||Films||", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := interestConfig(t)
			items[0].Key, items[0].Label = tc.key, tc.label
			setInterestJSON(t, items)
			c, err := Load()
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
			if err == nil {
				items[0].Label = strings.TrimSpace(tc.label)
				if !reflect.DeepEqual(c.DiscordInterests, items) {
					t.Fatal("interest changed or label not trimmed")
				}
			}
		})
	}
}

func TestDiscordInterestsRejectDuplicateOrUnsafeTargets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func([]DiscordInterest)
	}{
		{"duplicate key", func(items []DiscordInterest) { items[1].Key = items[0].Key }},
		{"duplicate role", func(items []DiscordInterest) { items[1].RoleID = items[0].RoleID }},
		{"duplicate channel", func(items []DiscordInterest) { items[1].ChannelID = items[0].ChannelID }},
		{"same role and channel", func(items []DiscordInterest) { items[0].ChannelID = items[0].RoleID }},
		{"cross-role channel", func(items []DiscordInterest) { items[1].ChannelID = items[0].RoleID }},
		{"everyone role", func(items []DiscordInterest) { items[0].RoleID = "23456" }},
		{"guild as channel", func(items []DiscordInterest) { items[0].ChannelID = "23456" }},
		{"menu channel", func(items []DiscordInterest) { items[0].ChannelID = "34567" }},
		{"menu as role", func(items []DiscordInterest) { items[0].RoleID = "34567" }},
		{"role mention", func(items []DiscordInterest) { items[0].RoleID = "<@&45678>" }},
		{"channel mention", func(items []DiscordInterest) { items[0].ChannelID = "<#56789>" }},
		{"short role", func(items []DiscordInterest) { items[0].RoleID = "1234" }},
		{"long channel", func(items []DiscordInterest) { items[0].ChannelID = strings.Repeat("1", 25) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := append(interestConfig(t), DiscordInterest{Key: "series", Label: "Séries", RoleID: "67890", ChannelID: "78901"})
			tc.change(items)
			setInterestJSON(t, items)
			if _, err := Load(); err == nil {
				t.Fatal("unsafe interest target accepted")
			}
		})
	}
}
