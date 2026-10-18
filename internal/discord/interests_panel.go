package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// PublishInterests is an explicit one-off operation; startup never posts a panel.
func (b *Bot) PublishInterests(ctx context.Context, messageID string) (string, error) {
	if b.Config.DiscordBotToken == "" || !interestID(b.Config.DiscordClientID) || !interestID(b.Config.DiscordGuildID) || !interestID(b.Config.DiscordInterestsChannelID) || len(b.Config.DiscordInterests) == 0 || len(b.Config.DiscordInterests) > 25 || (messageID != "" && !interestID(messageID)) {
		return "", errors.New("configure Discord interests and a valid panel message ID before publication")
	}
	labels := make([]string, 0, len(b.Config.DiscordInterests))
	for _, item := range b.Config.DiscordInterests {
		labels = append(labels, "• "+item.Label)
	}
	payload := map[string]any{
		"content":          "Choisissez les salons qui vous intéressent. Le bouton ouvre vos choix en privé ; vous pouvez les modifier à tout moment.",
		"embeds":           []map[string]any{{"title": "Vos centres d’intérêt", "description": strings.Join(labels, "\n")}},
		"allowed_mentions": map[string]any{"parse": []string{}},
		"components":       []map[string]any{{"type": 1, "components": []map[string]any{{"type": 2, "style": 1, "custom_id": "interests:open", "label": "Choisir mes salons"}}}},
	}
	raw, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client := http.Client{}
	if b.Client != nil {
		client = *b.Client
	}
	client.Timeout = 10 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request := func(method, path string, body []byte, out any) error {
		if ctx.Err() != nil {
			return errors.New("Discord interest panel operation was cancelled")
		}
		req, err := http.NewRequestWithContext(ctx, method, "https://discord.com/api/v10"+path, bytes.NewReader(body))
		if err != nil {
			return errors.New("cannot prepare the Discord interest panel request")
		}
		req.Header.Set("Authorization", "Bot "+b.Config.DiscordBotToken)
		req.Header.Set("User-Agent", "le-concierge-interests")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			return errors.New("Discord interest panel operation was not confirmed; inspect the channel before retrying")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return errors.New("Discord rejected the interest panel operation; inspect the channel and bot permissions before retrying")
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
		if err != nil || len(data) > 1024*1024 || json.Unmarshal(data, out) != nil {
			return errors.New("Discord interest panel response was not confirmed; inspect the channel before retrying")
		}
		return nil
	}
	var actor struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	}
	if err := request(http.MethodGet, "/users/@me", nil, &actor); err != nil {
		return "", err
	}
	if actor.ID != b.Config.DiscordClientID || !actor.Bot {
		return "", errors.New("interest panel publication requires the configured application's bot token")
	}
	channelPath := "/channels/" + b.Config.DiscordInterestsChannelID
	var channel struct {
		ID      string `json:"id"`
		GuildID string `json:"guild_id"`
		Type    *int   `json:"type"`
	}
	if err := request(http.MethodGet, channelPath, nil, &channel); err != nil {
		return "", err
	}
	if channel.ID != b.Config.DiscordInterestsChannelID || channel.GuildID != b.Config.DiscordGuildID || channel.Type == nil || *channel.Type != 0 {
		return "", errors.New("interest panel destination must be the configured guild text channel")
	}
	var message struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
		WebhookID string `json:"webhook_id"`
		Author    struct {
			ID string `json:"id"`
		} `json:"author"`
	}
	method, path := http.MethodPost, channelPath+"/messages"
	if messageID != "" {
		path += "/" + messageID
		if err := request(http.MethodGet, path, nil, &message); err != nil {
			return "", err
		}
		if message.ID != messageID || message.ChannelID != channel.ID || message.Author.ID != b.Config.DiscordClientID || message.WebhookID != "" {
			return "", errors.New("interest panel update requires a message from this bot in the configured channel")
		}
		method = http.MethodPatch
	}
	message.ID, message.ChannelID, message.WebhookID, message.Author.ID = "", "", "", ""
	// Never retry a write automatically: a lost response can hide a successful post.
	if err := request(method, path, raw, &message); err != nil {
		return "", err
	}
	if !interestID(message.ID) || (messageID != "" && message.ID != messageID) || message.ChannelID != channel.ID || message.Author.ID != b.Config.DiscordClientID || message.WebhookID != "" {
		return "", errors.New("Discord interest panel identity was not confirmed; inspect the channel before retrying")
	}
	return message.ID, nil
}
