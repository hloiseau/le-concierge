package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type Pin struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	ExpiresIn int    `json:"expiresIn"`
	AuthToken string `json:"authToken"`
}
type Grant struct {
	Name         string
	PlexID       string
	AccountToken string
	ServerToken  string
	ClientID     string
	VerifiedAt   time.Time
	ExpiresAt    time.Time
}
type Media struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Type     string `json:"type"`
	Year     int    `json:"year,omitempty"`
	AddedAt  string `json:"addedAt"`
	URL      string `json:"url,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
}

// Keep the PIN creator, Plex's browser auth app and subsequent API calls on
// the same device identity. Plex forwards context[device] as X-Plex headers.
var plexDevice = [...]struct{ field, header, value string }{
	{"product", "X-Plex-Product", "Le Hub"},
	{"version", "X-Plex-Version", "0.1.0"},
	{"platform", "X-Plex-Platform", "Web"},
	{"device", "X-Plex-Device", "Browser"},
	{"deviceName", "X-Plex-Device-Name", "Le Hub"},
}

func plexHeaders(client, token string) map[string]string {
	h := map[string]string{"X-Plex-Client-Identifier": client}
	for _, item := range plexDevice {
		h[item.header] = item.value
	}
	if token != "" {
		h["X-Plex-Token"] = token
	}
	return h
}

func PlexAuthURL(client, code string) string {
	q := url.Values{"clientID": {client}, "code": {code}}
	for _, item := range plexDevice {
		q.Set("context[device]["+item.field+"]", item.value)
	}
	// These parameters live in a URI fragment, not a form body. Match the
	// percent-encoded spaces used by Plex's documented auth URL format.
	return "https://app.plex.tv/auth#?" + strings.ReplaceAll(q.Encode(), "+", "%20")
}

func (c *Client) PlexPin(ctx context.Context, client string) (Pin, error) {
	var p Pin
	e := c.JSON(ctx, "POST", "https://plex.tv/api/v2/pins?strong=true", plexHeaders(client, ""), "", &p)
	if e == nil && (p.ID <= 0 || len(p.Code) < 8 || p.ExpiresIn <= 0) {
		e = ErrUnavailable
	}
	return p, e
}
func (c *Client) PlexPoll(ctx context.Context, p Pin, client string) (string, error) {
	var r Pin
	e := c.JSON(ctx, "GET", fmt.Sprintf("https://plex.tv/api/v2/pins/%d?code=%s", p.ID, url.QueryEscape(p.Code)), plexHeaders(client, ""), "", &r)
	if e != nil {
		return "", e
	}
	if r.ID != p.ID || r.Code != p.Code || len(r.AuthToken) > 8192 {
		return "", ErrDenied
	}
	return r.AuthToken, nil
}
func (c *Client) PlexVerify(ctx context.Context, account, client, serverID string) (Grant, error) {
	var user struct {
		ID         json.Number `json:"id"`
		Username   string      `json:"username"`
		Title      string      `json:"title"`
		Restricted bool        `json:"restricted"`
	}
	h := plexHeaders(client, account)
	if e := c.JSON(ctx, "GET", "https://plex.tv/api/v2/user", h, "", &user); e != nil {
		return Grant{}, e
	}
	id, err := user.ID.Int64()
	if err != nil || id <= 0 || user.Restricted {
		return Grant{}, ErrDenied
	}
	var resources []struct {
		ClientID    string `json:"clientIdentifier"`
		Provides    string `json:"provides"`
		AccessToken string `json:"accessToken"`
		Owned       bool   `json:"owned"`
	}
	if e := c.JSON(ctx, "GET", "https://plex.tv/api/v2/resources?includeHttps=1", h, "", &resources); e != nil {
		return Grant{}, e
	}
	for _, r := range resources {
		if r.ClientID == serverID && strings.Contains(","+r.Provides+",", ",server,") && r.AccessToken != "" {
			name := user.Username
			if name == "" {
				name = user.Title
			}
			return Grant{Name: name, PlexID: string(user.ID), AccountToken: account, ClientID: client, ServerToken: r.AccessToken, VerifiedAt: time.Now()}, nil
		}
	}
	return Grant{}, ErrDenied
}
func (c *Client) Recent(ctx context.Context, base, serverID string, g Grant, offset int) ([]Media, error) {
	p, err := c.RecentPage(ctx, base, serverID, g, offset, recentPageSize)
	return p.Items, err
}
