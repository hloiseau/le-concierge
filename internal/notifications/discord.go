// Package notifications delivers private notifications; callers must freshly
// authorize both the recipient and every item before invoking the transport.
package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
)

type Message struct {
	Title       string
	URL         string
	Description string
}

// DeliveryError deliberately excludes upstream bodies, credentials and URLs.
type DeliveryError struct {
	Code       string
	Permanent  bool
	RetryAfter time.Duration
}

func (e *DeliveryError) Error() string { return "discord delivery: " + e.Code }

type Discord struct {
	token            string
	HTTP             *http.Client
	mu               sync.Mutex
	until            time.Time
	denied           bool
	invalidRateLimit bool
	channels         map[string]string
}

func NewDiscord(token string) *Discord {
	return &Discord{token: token, channels: map[string]string{}, HTTP: &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (d *Discord) Send(ctx context.Context, discordID, nonce string, items []Message) error {
	return d.SendValidated(ctx, discordID, nonce, items, nil)
}

// SendValidated repeats the caller's authorization check immediately before the
// message POST, after opening the private channel. A failure sends no message.
func (d *Discord) SendValidated(ctx context.Context, discordID, nonce string, items []Message, beforeSend func() error) error {
	if d.token == "" || !snowflake(discordID) || nonce == "" || len(nonce) > 25 || len(items) == 0 || len(items) > 100 {
		return &DeliveryError{Code: "invalid_delivery", Permanent: true}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var channel struct {
		ID         string `json:"id"`
		Type       int    `json:"type"`
		Recipients []struct {
			ID string `json:"id"`
		} `json:"recipients"`
	}
	d.mu.Lock()
	channelID := d.channels[discordID]
	d.mu.Unlock()
	if channelID == "" {
		if err := d.post(ctx, "/users/@me/channels", map[string]string{"recipient_id": discordID}, &channel); err != nil {
			return err
		}
		if !snowflake(channel.ID) || channel.Type != 1 || len(channel.Recipients) != 1 || channel.Recipients[0].ID != discordID {
			return &DeliveryError{Code: "invalid_private_channel", Permanent: true}
		}
		channelID = channel.ID
		d.mu.Lock()
		if len(d.channels) >= 1024 {
			clear(d.channels)
		}
		d.channels[discordID] = channelID
		d.mu.Unlock()
	}
	if beforeSend != nil {
		if err := beforeSend(); err != nil {
			return &DeliveryError{Code: "authorization_changed", Permanent: true}
		}
	}
	payload := map[string]any{
		"content": digest(items), "nonce": nonce, "enforce_nonce": true,
		"allowed_mentions": map[string]any{"parse": []string{}},
		"flags":            4, // Suppress link previews, including requests to private URLs.
	}
	var message struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
	}
	if err := d.post(ctx, "/channels/"+channelID+"/messages", payload, &message); err != nil {
		return err
	}
	if !snowflake(message.ID) || message.ChannelID != channelID {
		return &DeliveryError{Code: "invalid_response"}
	}
	return nil
}

func (d *Discord) post(ctx context.Context, path string, payload, result any) error {
	return d.request(ctx, http.MethodPost, path, payload, result)
}

// SendChannelValidated only permits explicit user mentions in the configured
// guild's text channel. It never opens DMs or accepts names as mention targets.
func (d *Discord) SendChannelValidated(ctx context.Context, guildID, channelID, nonce string, item Message, recipients []string, beforeSend func() error) error {
	if d.token == "" || !snowflake(guildID) || !snowflake(channelID) || nonce == "" || len(nonce) > 25 || len(recipients) == 0 || len(recipients) > 25 {
		return &DeliveryError{Code: "invalid_delivery", Permanent: true}
	}
	seen := map[string]bool{}
	mentions := []string{}
	for _, id := range recipients {
		if !snowflake(id) {
			return &DeliveryError{Code: "invalid_delivery", Permanent: true}
		}
		if !seen[id] {
			mentions = append(mentions, id)
			seen[id] = true
		}
	}
	var channel struct {
		ID      string `json:"id"`
		GuildID string `json:"guild_id"`
		Type    int    `json:"type"`
	}
	lookupCtx, lookupCancel := context.WithTimeout(ctx, 5*time.Second)
	err := d.request(lookupCtx, http.MethodGet, "/channels/"+channelID, nil, &channel)
	lookupCancel()
	if err != nil {
		return err
	}
	if channel.ID != channelID || channel.GuildID != guildID || (channel.Type != 0 && channel.Type != 5) {
		return &DeliveryError{Code: "invalid_guild_channel", Permanent: true}
	}
	if beforeSend != nil {
		if err := beforeSend(); err != nil {
			var failure *DeliveryError
			if errors.As(err, &failure) {
				return failure
			}
			return &DeliveryError{Code: "authorization_changed", Permanent: true}
		}
	}
	if ctx.Err() != nil {
		return &DeliveryError{Code: "transport_unavailable"}
	}
	body := "Le Concierge — rappel avant retrait\n"
	for _, id := range mentions {
		body += "<@" + id + "> "
	}
	body += "\n" + plain(item.Title, 200) + " — " + plain(item.Description, 300)
	if len(utf16.Encode([]rune(body))) > 1900 {
		return &DeliveryError{Code: "invalid_delivery", Permanent: true}
	}
	if len(item.URL) <= 512 {
		if u, err := url.Parse(item.URL); err == nil && u.Hostname() != "" && u.User == nil && (u.Scheme == "https" || u.Scheme == "http") && !strings.ContainsAny(item.URL, "<>\r\n") {
			if link := "\n<" + u.String() + ">"; len(utf16.Encode([]rune(body+link))) <= 2000 {
				body += link
			}
		}
	}
	var message struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
	}
	// Source revalidation has its own caller deadline; it must not consume the
	// five seconds reserved for the Discord POST.
	sendCtx, sendCancel := context.WithTimeout(ctx, 5*time.Second)
	defer sendCancel()
	if err := d.post(sendCtx, "/channels/"+channelID+"/messages", map[string]any{"content": body, "nonce": nonce, "enforce_nonce": true, "allowed_mentions": map[string]any{"parse": []string{}, "users": mentions}, "flags": 4}, &message); err != nil {
		return err
	}
	if !snowflake(message.ID) || message.ChannelID != channelID {
		return &DeliveryError{Code: "invalid_response"}
	}
	return nil
}

func (d *Discord) request(ctx context.Context, method, path string, payload, result any) error {
	d.mu.Lock()
	denied, invalidRateLimit, wait := d.denied, d.invalidRateLimit, time.Until(d.until)
	d.mu.Unlock()
	if denied {
		return &DeliveryError{Code: "bot_unauthorized", Permanent: true}
	}
	if invalidRateLimit {
		return &DeliveryError{Code: "invalid_rate_limit", Permanent: true}
	}
	if wait > 0 {
		return &DeliveryError{Code: "rate_limited", RetryAfter: wait}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return &DeliveryError{Code: "invalid_delivery", Permanent: true}
	}
	var reader io.Reader
	if method != http.MethodGet {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://discord.com/api/v10"+path, reader)
	if err != nil {
		return &DeliveryError{Code: "invalid_delivery", Permanent: true}
	}
	req.Header.Set("Authorization", "Bot "+d.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return &DeliveryError{Code: "transport_unavailable"}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		return &DeliveryError{Code: "invalid_response"}
	}
	var failure struct {
		Code       int     `json:"code"`
		RetryAfter float64 `json:"retry_after"`
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = json.Unmarshal(data, &failure)
		if resp.StatusCode == http.StatusUnauthorized {
			d.mu.Lock()
			d.denied = true
			d.mu.Unlock()
			return &DeliveryError{Code: "bot_unauthorized", Permanent: true}
		}
		if failure.Code == 50007 || resp.StatusCode == http.StatusForbidden {
			return &DeliveryError{Code: "dm_unavailable", Permanent: true}
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			header, _ := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64)
			seconds := math.Max(header, failure.RetryAfter)
			if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > (24*time.Hour).Seconds() {
				d.mu.Lock()
				d.invalidRateLimit = true
				d.mu.Unlock()
				return &DeliveryError{Code: "invalid_rate_limit", Permanent: true}
			}
			wait := max(time.Second, time.Duration(math.Ceil(seconds*1000))*time.Millisecond)
			d.pause(wait)
			return &DeliveryError{Code: "rate_limited", RetryAfter: wait}
		}
		return &DeliveryError{Code: "upstream_failure", Permanent: resp.StatusCode < 500}
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		seconds, err := strconv.ParseFloat(resp.Header.Get("X-RateLimit-Reset-After"), 64)
		if err == nil && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) && seconds > 0 && seconds <= (24*time.Hour).Seconds() {
			d.pause(time.Duration(math.Ceil(seconds*1000)) * time.Millisecond)
		}
	}
	if json.Unmarshal(data, result) != nil {
		return &DeliveryError{Code: "invalid_response"}
	}
	return nil
}

func (d *Discord) pause(wait time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if next := time.Now().Add(wait); next.After(d.until) {
		d.until = next
	}
}

func snowflake(s string) bool {
	if s == "" || len(s) > 20 || s[0] == '0' {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

func digest(items []Message) string {
	body := "Le Concierge — vos notifications privées\n"
	for i, item := range items {
		line := "\n• " + plain(item.Title, 200)
		if item.Description != "" {
			line += " — " + plain(item.Description, 300)
		}
		if u, err := url.Parse(item.URL); err == nil && u.Hostname() != "" && u.User == nil && (u.Scheme == "https" || u.Scheme == "http") && len(item.URL) <= 1024 && !strings.ContainsAny(item.URL, "<>\r\n") {
			line += "\n<" + u.String() + ">"
		}
		if len(utf16.Encode([]rune(body+line))) > 1850 {
			body += fmt.Sprintf("\n… %d autre(s) notification(s) dans le Hub.", len(items)-i)
			break
		}
		body += line + "\n"
	}
	return body
}

func plain(s string, limit int) string {
	s = strings.Map(func(c rune) rune {
		if unicode.IsControl(c) || c == '\u2028' || c == '\u2029' {
			return ' '
		}
		return c
	}, s)
	runes := []rune(s)
	if len(runes) > limit {
		s = string(runes[:limit]) + "…"
	}
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "~", "\\~", "`", "\\`", "|", "\\|", "<", "‹", ">", "›", "[", "\\[", "]", "\\]", "@", "@\u200b").Replace(strings.TrimSpace(s))
}
