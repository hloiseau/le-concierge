package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/hloiseau/le-concierge/internal/config"
)

// SyncIdentity updates only this bot. Reads avoid consuming username changes on
// every restart. The application ID, avatar, roles and OAuth settings stay intact.
func (b *Bot) SyncIdentity(ctx context.Context) error {
	if b.Config.DiscordBotName == "" {
		return nil
	}
	if b.Config.DiscordBotToken == "" || !config.Snowflake(b.Config.DiscordClientID) || !config.Snowflake(b.Config.DiscordGuildID) {
		return errors.New("Discord identity requires bot token, application and guild IDs")
	}
	var user struct {
		ID, Username string
		Bot          bool
	}
	if err := b.identityRequest(ctx, "GET", "/users/@me", nil, &user); err != nil {
		return err
	}
	if !user.Bot || user.ID != b.Config.DiscordClientID {
		return errors.New("Discord token does not belong to the configured bot")
	}
	var globalErr error
	if user.Username != b.Config.DiscordBotName {
		globalErr = b.identityRequest(ctx, "PATCH", "/users/@me", map[string]string{"username": b.Config.DiscordBotName}, nil)
	}
	path := "/guilds/" + b.Config.DiscordGuildID + "/members/" + user.ID
	var member struct {
		Nick *string `json:"nick"`
	}
	if err := b.identityRequest(ctx, "GET", path, nil, &member); err != nil {
		return errors.Join(globalErr, err)
	}
	var guildErr error
	if member.Nick == nil || *member.Nick != b.Config.DiscordBotName {
		guildErr = b.identityRequest(ctx, "PATCH", "/guilds/"+b.Config.DiscordGuildID+"/members/@me", map[string]string{"nick": b.Config.DiscordBotName}, nil)
	}
	return errors.Join(globalErr, guildErr)
}

func (b *Bot) identityRequest(ctx context.Context, method, path string, payload any, out any) error {
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://discord.com/api/v10"+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("Discord identity request failed")
	}
	req.Header.Set("Authorization", "Bot "+b.Config.DiscordBotToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.Client.Do(req)
	if err != nil {
		return errors.New("Discord identity unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("Discord rejected identity update; verify bot permissions and retry later")
	}
	if out != nil && json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(out) != nil {
		return errors.New("invalid Discord identity response")
	}
	return nil
}
