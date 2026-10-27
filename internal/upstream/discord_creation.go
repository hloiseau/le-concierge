package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"
)

var ErrDiscordUncertain = errors.New("Discord creation not confirmed")
var discordSnowflake = regexp.MustCompile(`^[0-9]{5,24}$`)

type DiscordCreated struct {
	ID             string    `json:"id"`
	ChannelID      string    `json:"channel_id"`
	GuildID        string    `json:"guild_id"`
	CreatorID      string    `json:"creator_id"`
	Name           string    `json:"name"`
	StartAt        time.Time `json:"scheduled_start_time"`
	EndAt          time.Time `json:"scheduled_end_time"`
	EntityType     int       `json:"entity_type"`
	EntityMetadata struct {
		Location string `json:"location"`
	} `json:"entity_metadata"`
	Author struct {
		ID string `json:"id"`
	} `json:"author"`
	Poll *struct {
		Multiselect bool `json:"allow_multiselect"`
		Question    struct {
			Text string `json:"text"`
		} `json:"question"`
		Answers []struct {
			Media struct {
				Text string `json:"text"`
			} `json:"poll_media"`
		} `json:"answers"`
	} `json:"poll"`
}

// A single POST, never retried. Transport/5xx/malformed replies stay uncertain.
func (c *Client) CreateDiscord(ctx context.Context, token, guild, channel, action string, payload any) (DiscordCreated, error) {
	var out DiscordCreated
	if token == "" || !discordSnowflake.MatchString(guild) || !discordSnowflake.MatchString(channel) || action != "poll" && action != "event" {
		return out, ErrDenied
	}
	target := "https://discord.com/api/v10/channels/" + channel + "/messages"
	if action == "event" {
		target = "https://discord.com/api/v10/guilds/" + guild + "/scheduled-events"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return out, ErrDenied
	}
	req, err := http.NewRequestWithContext(ctx, "POST", target, bytes.NewReader(body))
	if err != nil {
		return out, ErrDenied
	}
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("Content-Type", "application/json")
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return out, ErrDiscordUncertain
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return out, ErrDenied
	}
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return out, ErrDiscordUncertain
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024+1))
	if err != nil || len(data) > 32*1024 || json.Unmarshal(data, &out) != nil || !discordSnowflake.MatchString(out.ID) {
		return DiscordCreated{}, ErrDiscordUncertain
	}
	if action == "poll" && (out.ChannelID != channel || out.Poll == nil) || action == "event" && (out.GuildID != guild || out.EntityType != 3) {
		return DiscordCreated{}, ErrDiscordUncertain
	}
	return out, nil
}
