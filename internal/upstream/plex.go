package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
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

func plexHeaders(client, token string) map[string]string {
	h := map[string]string{"X-Plex-Product": "Le Concierge", "X-Plex-Version": "0.1.0", "X-Plex-Client-Identifier": client}
	if token != "" {
		h["X-Plex-Token"] = token
	}
	return h
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
		Restricted bool        `json:"restricted"`
	}
	h := plexHeaders(client, account)
	if e := c.JSON(ctx, "GET", "https://plex.tv/api/v2/user", h, "", &user); e != nil {
		return Grant{}, e
	}
	if user.ID == "" || user.Restricted {
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
			return Grant{PlexID: string(user.ID), AccountToken: account, ClientID: client, ServerToken: r.AccessToken, VerifiedAt: time.Now()}, nil
		}
	}
	return Grant{}, ErrDenied
}
func (c *Client) Recent(ctx context.Context, base, serverID string, g Grant, offset int) ([]Media, error) {
	var identity struct {
		Container struct {
			ID string `json:"machineIdentifier"`
		} `json:"MediaContainer"`
	}
	if e := c.JSON(ctx, "GET", base+"/identity", plexHeaders(g.ClientID, ""), "", &identity); e != nil {
		return nil, e
	}
	if identity.Container.ID != serverID {
		return nil, ErrDenied
	}
	var result struct {
		Container struct {
			Metadata []struct {
				Key     string `json:"ratingKey"`
				Title   string `json:"title"`
				Type    string `json:"type"`
				Year    int    `json:"year"`
				AddedAt int64  `json:"addedAt"`
				Series  string `json:"grandparentTitle"`
				Parent  string `json:"parentTitle"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	h := plexHeaders(g.ClientID, g.ServerToken)
	// Only a configured server URL is contacted, never URLs supplied by Plex resources.
	if e := c.JSON(ctx, "GET", base+"/library/recentlyAdded?X-Plex-Container-Start="+strconv.Itoa(offset)+"&X-Plex-Container-Size=12", h, "", &result); e != nil {
		return nil, e
	}
	out := []Media{}
	for _, m := range result.Container.Metadata {
		if m.Type != "movie" && m.Type != "show" && m.Type != "season" && m.Type != "episode" {
			continue
		}
		title := m.Title
		sub := ""
		if m.Series != "" {
			title = m.Series
			sub = m.Parent + " · " + m.Title
		}
		link := "https://app.plex.tv/desktop/#!/server/" + url.PathEscape(serverID) + "/details?key=" + url.QueryEscape("/library/metadata/"+m.Key)
		out = append(out, Media{ID: m.Key, Title: title, Type: m.Type, Year: m.Year, AddedAt: time.Unix(m.AddedAt, 0).UTC().Format(time.RFC3339), URL: link, Subtitle: sub})
	}
	return out, nil
}
