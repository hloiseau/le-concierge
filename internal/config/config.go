package config

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Service struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url,omitempty"`
	Category    string `json:"category"`
}
type Config struct {
	Env, Listen, BotListen, PublicOrigin, DatabasePath                                      string
	DiscordClientID, DiscordClientSecret, DiscordGuildID, DiscordPublicKey, DiscordBotToken string
	BotAPIURL, BotAPISecret                                                                 string
	PlexURL, PlexServerID, SeerrURL, SeerrAPIKey                                            string
	MaintainerrURL, TautulliURL, TautulliAPIKey                                             string
	AdminIDs                                                                                []string
	DiscordDMsEnabled                                                                       bool
	Services                                                                                []Service
}

var snowflake = regexp.MustCompile(`^[0-9]{5,24}$`)

func Snowflake(v string) bool { return snowflake.MatchString(v) }
func secret(name string) (string, error) {
	v, f := os.Getenv(name), os.Getenv(name+"_FILE")
	if v != "" && f != "" {
		return "", fmt.Errorf("set only %s or %s_FILE", name, name)
	}
	if f != "" {
		b, e := os.ReadFile(f)
		if e != nil {
			return "", fmt.Errorf("cannot read %s_FILE", name)
		}
		v = strings.TrimSpace(string(b))
	}
	return v, nil
}
func fallback(name, value string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return value
}
func ValidURL(raw string, origin bool) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	return !origin || u.Path == "" || u.Path == "/"
}
func Load() (Config, error) {
	c := Config{Env: fallback("APP_ENV", "production"), Listen: fallback("LISTEN_ADDR", ":8080"), BotListen: fallback("BOT_LISTEN_ADDR", ":8081"), PublicOrigin: strings.TrimRight(fallback("PUBLIC_ORIGIN", "http://localhost:3000"), "/"), DatabasePath: fallback("DATABASE_PATH", "data/concierge.db"), DiscordClientID: os.Getenv("DISCORD_CLIENT_ID"), DiscordGuildID: os.Getenv("DISCORD_GUILD_ID"), DiscordPublicKey: os.Getenv("DISCORD_PUBLIC_KEY"), BotAPIURL: strings.TrimRight(fallback("BOT_API_URL", "http://api:8080"), "/"), PlexURL: strings.TrimRight(os.Getenv("PLEX_URL"), "/"), PlexServerID: os.Getenv("PLEX_SERVER_ID"), SeerrURL: strings.TrimRight(os.Getenv("SEERR_URL"), "/")}
	c.MaintainerrURL = strings.TrimRight(os.Getenv("MAINTAINERR_URL"), "/")
	c.TautulliURL = strings.TrimRight(os.Getenv("TAUTULLI_URL"), "/")
	for _, p := range []struct {
		name string
		dest *string
	}{{"DISCORD_CLIENT_SECRET", &c.DiscordClientSecret}, {"DISCORD_BOT_TOKEN", &c.DiscordBotToken}, {"BOT_API_SECRET", &c.BotAPISecret}, {"SEERR_API_KEY", &c.SeerrAPIKey}, {"TAUTULLI_API_KEY", &c.TautulliAPIKey}} {
		v, e := secret(p.name)
		if e != nil {
			return c, e
		}
		*p.dest = v
	}
	if c.Env != "production" && c.Env != "development" {
		return c, errors.New("APP_ENV must be production or development")
	}
	var err error
	c.DiscordDMsEnabled, err = strconv.ParseBool(fallback("DISCORD_DMS_ENABLED", "false"))
	if err != nil || (c.DiscordDMsEnabled && c.DiscordBotToken == "") {
		return c, errors.New("DISCORD_DMS_ENABLED requires a boolean and a bot token when enabled")
	}
	if !ValidURL(c.PublicOrigin, true) {
		return c, errors.New("PUBLIC_ORIGIN must be an absolute origin")
	}
	origin, _ := url.Parse(c.PublicOrigin)
	if origin.Scheme != "https" && (c.Env != "development" || (origin.Hostname() != "localhost" && origin.Hostname() != "127.0.0.1")) {
		return c, errors.New("PUBLIC_ORIGIN requires HTTPS outside local development")
	}
	for _, v := range []string{c.PlexURL, c.SeerrURL, c.BotAPIURL, c.MaintainerrURL, c.TautulliURL} {
		if v != "" && !ValidURL(v, false) {
			return c, errors.New("invalid service URL")
		}
	}
	if (c.PlexURL == "") != (c.PlexServerID == "") {
		return c, errors.New("PLEX_URL and PLEX_SERVER_ID must be set together")
	}
	if (c.SeerrURL == "") != (c.SeerrAPIKey == "") {
		return c, errors.New("SEERR_URL and SEERR_API_KEY must be set together")
	}
	if (c.TautulliURL == "") != (c.TautulliAPIKey == "") {
		return c, errors.New("TAUTULLI_URL and TAUTULLI_API_KEY must be set together")
	}
	if (c.MaintainerrURL != "" || c.TautulliURL != "") && c.PlexServerID == "" {
		return c, errors.New("removal sources require PLEX_SERVER_ID")
	}
	if c.DiscordClientSecret != "" {
		if !Snowflake(c.DiscordClientID) || !Snowflake(c.DiscordGuildID) || c.DiscordClientSecret == "" {
			return c, errors.New("Discord OAuth requires client ID, secret and guild ID")
		}
	}
	if c.DiscordPublicKey != "" {
		b, e := hex.DecodeString(c.DiscordPublicKey)
		if e != nil || len(b) != 32 {
			return c, errors.New("DISCORD_PUBLIC_KEY must be a 32-byte hex key")
		}
		if !Snowflake(c.DiscordClientID) || !Snowflake(c.DiscordGuildID) || len(c.BotAPISecret) < 32 {
			return c, errors.New("Discord interactions require app/guild IDs and BOT_API_SECRET (32 characters minimum)")
		}
	}
	for _, id := range strings.Split(os.Getenv("ADMIN_DISCORD_IDS"), ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			if !Snowflake(id) {
				return c, errors.New("invalid ADMIN_DISCORD_IDS")
			}
			c.AdminIDs = append(c.AdminIDs, id)
		}
	}
	c.Services = []Service{{ID: "plex", Name: "Plex", Description: "Films et séries", Category: "watch"}, {ID: "seerr", Name: "Seerr", Description: "Demander un film ou une série", Category: "watch"}, {ID: "downloadarr", Name: "Downloadarr", Description: "Emporter vos contenus", Category: "tools"}, {ID: "torrent-watch", Name: "Torrent Watch", Description: "Suivre les téléchargements", Category: "tools"}, {ID: "shelfmark", Name: "Shelfmark", Description: "Demander une lecture", Category: "read"}, {ID: "grimmory", Name: "Grimmory", Description: "Livres et mangas", Category: "read"}}
	if raw := os.Getenv("SERVICE_LINKS_JSON"); raw != "" {
		var links map[string]string
		if e := json.Unmarshal([]byte(raw), &links); e != nil {
			return c, errors.New("invalid SERVICE_LINKS_JSON")
		}
		for i := range c.Services {
			v := links[c.Services[i].ID]
			if v != "" {
				u, e := url.Parse(v)
				if e != nil || u.Hostname() == "" || u.User != nil || u.Scheme != "https" {
					return c, errors.New("public service links require HTTPS")
				}
			}
			c.Services[i].URL = v
		}
	}
	return c, nil
}
func (c Config) Admin(id string) bool {
	for _, v := range c.AdminIDs {
		if v == id {
			return true
		}
	}
	return false
}
func (c Config) OAuthEnabled() bool {
	return c.DiscordClientID != "" && c.DiscordClientSecret != "" && c.DiscordGuildID != ""
}
