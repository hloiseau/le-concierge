package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestAdditionalChatGuildConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:3000")
	t.Setenv("DISCORD_GUILD_ID", "23456")
	t.Setenv("DISCORD_CHAT_GUILD_IDS", " 34567, 45678 ")
	c, err := Load()
	if err != nil || !reflect.DeepEqual(c.DiscordChatGuildIDs, []string{"34567", "45678"}) {
		t.Fatal(c.DiscordChatGuildIDs, err)
	}
	for _, id := range []string{"23456", "34567", "45678"} {
		if !c.DiscordChatAllowed(id) {
			t.Fatal("configured guild rejected", id)
		}
	}
	for _, id := range []string{"", "56789", "*", "../34567"} {
		if c.DiscordChatAllowed(id) {
			t.Fatal("unconfigured guild accepted", id)
		}
	}
	for _, value := range []string{"*", "34567,", "34567,34567", "23456", "../34567", strings.Join([]string{"10000", "10001", "10002", "10003", "10004", "10005", "10006", "10007", "10008", "10009", "10010"}, ",")} {
		t.Setenv("DISCORD_CHAT_GUILD_IDS", value)
		if _, err := Load(); err == nil {
			t.Fatal("invalid allowlist accepted", value)
		}
	}
	t.Setenv("DISCORD_CHAT_GUILD_IDS", "")
	c, err = Load()
	if err != nil || !c.DiscordChatAllowed("23456") || c.DiscordChatAllowed("34567") {
		t.Fatal("default changed the primary guild", err)
	}
	t.Setenv("DISCORD_CHAT_GUILD_IDS", "34567")
	t.Setenv("DISCORD_GUILD_ID", "")
	if _, err := Load(); err == nil {
		t.Fatal("additional guild accepted without primary")
	}
}
