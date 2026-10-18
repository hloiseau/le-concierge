package config

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type DiscordInterest struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	RoleID    string `json:"roleId"`
	ChannelID string `json:"channelId"`
}

var interestKey = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func (c *Config) loadDiscordInterests() error {
	raw := os.Getenv("DISCORD_INTERESTS_JSON")
	c.DiscordInterestsChannelID = os.Getenv("DISCORD_INTERESTS_CHANNEL_ID")
	if raw == "" && c.DiscordInterestsChannelID == "" {
		return nil
	}
	if raw == "" || !Snowflake(c.DiscordInterestsChannelID) {
		return errors.New("DISCORD_INTERESTS_JSON and DISCORD_INTERESTS_CHANNEL_ID require a paired interest list and menu channel ID")
	}
	if strings.TrimSpace(c.DiscordBotToken) == "" || !Snowflake(c.DiscordGuildID) || !Snowflake(c.DiscordClientID) {
		return errors.New("Discord interests require DISCORD_BOT_TOKEN and valid DISCORD_GUILD_ID and DISCORD_CLIENT_ID")
	}
	if c.DiscordInterestsChannelID == c.DiscordGuildID {
		return errors.New("DISCORD_INTERESTS_CHANNEL_ID must identify a channel, not the guild")
	}
	if len(raw) > 64*1024 || !utf8.ValidString(raw) {
		return errors.New("DISCORD_INTERESTS_JSON requires valid UTF-8 JSON up to 64 KiB")
	}
	var interests []DiscordInterest
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&interests) != nil || d.Decode(new(any)) != io.EOF || len(interests) < 1 || len(interests) > 25 {
		return errors.New("DISCORD_INTERESTS_JSON requires a strict array of 1 to 25 interests")
	}
	keys, ids := map[string]bool{}, map[string]bool{c.DiscordGuildID: true, c.DiscordInterestsChannelID: true}
	for i := range interests {
		item := &interests[i]
		if !interestKey.MatchString(item.Key) || keys[item.Key] {
			return errors.New("DISCORD_INTERESTS_JSON requires unique lowercase ASCII keys, 1 to 32 characters")
		}
		keys[item.Key] = true
		if strings.ContainsFunc(item.Label, unicode.IsControl) || strings.ContainsAny(item.Label, "\u2028\u2029") {
			return errors.New("DISCORD_INTERESTS_JSON labels cannot contain control characters")
		}
		item.Label = strings.TrimSpace(item.Label)
		if !validInterestLabel(item.Label) {
			return errors.New("DISCORD_INTERESTS_JSON labels require 1 to 80 characters without mentions or Markdown")
		}
		for _, id := range []string{item.RoleID, item.ChannelID} {
			if !Snowflake(id) || ids[id] {
				return errors.New("DISCORD_INTERESTS_JSON requires distinct role and channel IDs, excluding the guild and menu channel")
			}
			ids[id] = true
		}
	}
	c.DiscordInterests = interests
	return nil
}

func validInterestLabel(label string) bool {
	if count := utf8.RuneCountInString(label); count < 1 || count > 80 {
		return false
	}
	for _, r := range label {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r) && !strings.ContainsRune(" -–—'’.,:;!?()/&+", r) {
			return false
		}
	}
	return true
}
