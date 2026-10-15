package upstream

import (
	"context"
	"net/url"
)

type DiscordIdentity struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
}

func (c *Client) DiscordLogin(ctx context.Context, clientID, secret, code, redirect, guild string) (DiscordIdentity, error) {
	var identity DiscordIdentity
	var token struct {
		AccessToken string `json:"access_token"`
	}
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "client_secret": {secret}, "code": {code}, "redirect_uri": {redirect}}
	e := c.JSON(ctx, "POST", "https://discord.com/api/oauth2/token", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, form.Encode(), &token)
	if e != nil || token.AccessToken == "" {
		return identity, ErrDenied
	}
	headers := map[string]string{"Authorization": "Bearer " + token.AccessToken}
	if e = c.JSON(ctx, "GET", "https://discord.com/api/v10/users/@me", headers, "", &identity); e != nil {
		return identity, e
	}
	var member struct {
		User    DiscordIdentity `json:"user"`
		Pending bool            `json:"pending"`
	}
	if e = c.JSON(ctx, "GET", "https://discord.com/api/v10/users/@me/guilds/"+guild+"/member", headers, "", &member); e != nil {
		return identity, e
	}
	if member.User.ID != identity.ID || member.Pending {
		return identity, ErrDenied
	}
	return identity, nil
}
