package config

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hloiseau/le-concierge/internal/community"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
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
	DiscordBotName, ErisAPIURL, ErisAPIKey, ErisModel                                       string
	PlexGrantKey                                                                            string
	DownloadarrURL, DownloadarrRevision                                                     string
	RadarrURL, RadarrAPIKey, SonarrURL, SonarrAPIKey                                        string
	CommunityFeedFile, CommunitySyncManifest                                                string
	CommunityForumIDs, RoadmapIssueIDs                                                      []string
	Env, Listen, BotListen, PublicOrigin, DatabasePath                                      string
	DiscordClientID, DiscordClientSecret, DiscordGuildID, DiscordPublicKey, DiscordBotToken string
	BotAPIURL, BotAPISecret                                                                 string
	PlexURL, PlexServerID, SeerrURL, SeerrAPIKey                                            string
	MaintainerrURL, TautulliURL, TautulliAPIKey                                             string
	GrimmoryURL, ShelfmarkURL, MetricsFile, ToparrURL                                       string
	MonitoringEnabled                                                                       bool
	AdminIDs                                                                                []string
	DiscordDMsEnabled                                                                       bool
	DiscordNotificationChannelID                                                            string
	DiscordInterests                                                                        []DiscordInterest
	DiscordInterestsChannelID                                                               string
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
	c.GrimmoryURL = strings.TrimRight(os.Getenv("GRIMMORY_URL"), "/")
	c.ShelfmarkURL = strings.TrimRight(os.Getenv("SHELFMARK_URL"), "/")
	c.DownloadarrURL = strings.TrimRight(os.Getenv("DOWNLOADARR_URL"), "/")
	c.DownloadarrRevision = os.Getenv("DOWNLOADARR_REVISION")
	c.MetricsFile = os.Getenv("METRICS_FILE")
	c.ToparrURL = strings.TrimRight(os.Getenv("TOPARR_URL"), "/")
	c.DiscordNotificationChannelID = os.Getenv("DISCORD_NOTIFICATION_CHANNEL_ID")
	c.RadarrURL = strings.TrimRight(os.Getenv("RADARR_URL"), "/")
	c.SonarrURL = strings.TrimRight(os.Getenv("SONARR_URL"), "/")
	c.DiscordBotName = fallback("DISCORD_BOT_NAME", "Eris")
	if c.DiscordBotName == "disabled" {
		c.DiscordBotName = ""
	}
	c.ErisAPIURL = strings.TrimRight(os.Getenv("ERIS_API_URL"), "/")
	c.ErisModel = fallback("ERIS_MODEL", "eris-lfm2.5-v1")
	if c.DiscordBotName != "" && !regexp.MustCompile(`^[\p{L}\p{N} _-]{2,32}$`).MatchString(c.DiscordBotName) {
		return c, errors.New("DISCORD_BOT_NAME requires 2-32 letters, numbers, spaces, underscores or hyphens")
	}
	c.CommunityFeedFile = os.Getenv("COMMUNITY_FEED_FILE")
	c.CommunitySyncManifest = os.Getenv("COMMUNITY_SYNC_MANIFEST")
	for _, entry := range []struct {
		name  string
		dest  *[]string
		valid func(string) bool
	}{
		{"COMMUNITY_FORUM_IDS", &c.CommunityForumIDs, community.ValidForumID},
		{"ROADMAP_ISSUE_IDS", &c.RoadmapIssueIDs, community.ValidIssueID},
	} {
		seen := map[string]bool{}
		for _, value := range strings.Split(os.Getenv(entry.name), ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if !entry.valid(value) || seen[value] || len(*entry.dest) >= 100 {
				return c, fmt.Errorf("invalid %s allowlist", entry.name)
			}
			seen[value] = true
			*entry.dest = append(*entry.dest, value)
		}
	}
	for _, p := range []struct {
		name string
		dest *string
	}{{"ERIS_API_KEY", &c.ErisAPIKey}, {"PLEX_GRANT_KEY", &c.PlexGrantKey}, {"DISCORD_CLIENT_SECRET", &c.DiscordClientSecret}, {"DISCORD_BOT_TOKEN", &c.DiscordBotToken}, {"BOT_API_SECRET", &c.BotAPISecret}, {"SEERR_API_KEY", &c.SeerrAPIKey}, {"TAUTULLI_API_KEY", &c.TautulliAPIKey}, {"RADARR_API_KEY", &c.RadarrAPIKey}, {"SONARR_API_KEY", &c.SonarrAPIKey}} {
		v, e := secret(p.name)
		if e != nil {
			return c, e
		}
		*p.dest = v
	}
	if c.ErisAPIURL != "" && (!ValidURL(c.ErisAPIURL, false) || c.ErisModel == "" || len(c.ErisModel) > 128 || len(c.ErisAPIKey) < 32) {
		return c, errors.New("ERIS_API_URL requires an absolute API URL, ERIS_MODEL and an ERIS_API_KEY of at least 32 characters")
	}
	if c.PlexGrantKey != "" {
		key, err := hex.DecodeString(c.PlexGrantKey)
		if err != nil || len(key) != 32 {
			return c, errors.New("PLEX_GRANT_KEY must be a 32-byte hex key")
		}
	}
	if err := c.loadDiscordInterests(); err != nil {
		return c, err
	}
	if c.Env != "production" && c.Env != "development" {
		return c, errors.New("APP_ENV must be production or development")
	}
	var err error
	if len(c.CommunityForumIDs) > 0 && !community.ValidForumID(c.DiscordGuildID) {
		return c, errors.New("community forums require DISCORD_GUILD_ID")
	}
	if c.CommunityFeedFile != "" && !filepath.IsAbs(c.CommunityFeedFile) {
		return c, errors.New("COMMUNITY_FEED_FILE requires an absolute path")
	}
	if c.CommunitySyncManifest != "" && (!filepath.IsAbs(c.CommunitySyncManifest) || c.CommunityFeedFile == "" || strings.EqualFold(filepath.Clean(c.CommunitySyncManifest), filepath.Clean(c.CommunityFeedFile)) || strings.EqualFold(filepath.Clean(c.CommunitySyncManifest), filepath.Clean(c.CommunityFeedFile)+".retry-after")) {
		return c, errors.New("COMMUNITY_SYNC_MANIFEST requires an absolute path distinct from the configured publication and retry file")
	}
	c.MonitoringEnabled, err = strconv.ParseBool(fallback("MONITORING_ENABLED", strconv.FormatBool(c.MetricsFile != "" || c.ToparrURL != "" || (c.TautulliURL != "" && c.TautulliAPIKey != "" && c.PlexServerID != ""))))
	if err != nil || (c.MetricsFile != "" && !filepath.IsAbs(c.MetricsFile)) {
		return c, errors.New("MONITORING_ENABLED requires a boolean; METRICS_FILE requires an absolute path")
	}
	c.DiscordDMsEnabled, err = strconv.ParseBool(fallback("DISCORD_DMS_ENABLED", "false"))
	if err != nil {
		return c, errors.New("DISCORD_DMS_ENABLED requires a boolean")
	}
	if c.DiscordNotificationChannelID != "" && !Snowflake(c.DiscordNotificationChannelID) {
		return c, errors.New("DISCORD_NOTIFICATION_CHANNEL_ID requires a channel ID")
	}
	if c.DiscordBotToken == "" {
		if c.DiscordDMsEnabled {
			slog.Warn("Discord DMs disabled: DISCORD_BOT_TOKEN is not configured")
			c.DiscordDMsEnabled = false
		}
		if c.DiscordNotificationChannelID != "" {
			slog.Warn("Discord channel reminders disabled: DISCORD_BOT_TOKEN is not configured")
			c.DiscordNotificationChannelID = ""
		}
	}
	if c.DiscordNotificationChannelID != "" && (!Snowflake(c.DiscordGuildID) || c.DiscordBotToken == "" || c.MaintainerrURL == "" || c.SeerrURL == "" || c.PlexServerID == "") {
		return c, errors.New("DISCORD_NOTIFICATION_CHANNEL_ID requires a channel ID, guild, bot token, Maintainerr, Seerr and Plex server")
	}
	if !ValidURL(c.PublicOrigin, true) {
		return c, errors.New("PUBLIC_ORIGIN must be an absolute origin")
	}
	origin, _ := url.Parse(c.PublicOrigin)
	if origin.Scheme != "https" && (c.Env != "development" || (origin.Hostname() != "localhost" && origin.Hostname() != "127.0.0.1")) {
		return c, errors.New("PUBLIC_ORIGIN requires HTTPS outside local development")
	}
	for _, v := range []string{c.PlexURL, c.SeerrURL, c.BotAPIURL, c.MaintainerrURL, c.TautulliURL, c.ToparrURL, c.GrimmoryURL, c.ShelfmarkURL, c.RadarrURL, c.SonarrURL, c.DownloadarrURL} {
		if v != "" && !ValidURL(v, false) {
			return c, errors.New("invalid service URL")
		}
	}
	if c.GrimmoryURL != "" && c.Env != "development" && !strings.HasPrefix(c.GrimmoryURL, "https://") {
		return c, errors.New("GRIMMORY_URL requires HTTPS outside local development")
	}
	if c.ShelfmarkURL != "" && c.Env != "development" && !strings.HasPrefix(c.ShelfmarkURL, "https://") {
		return c, errors.New("SHELFMARK_URL requires HTTPS outside local development")
	}
	if (c.DownloadarrURL == "") != (c.DownloadarrRevision == "") || (c.DownloadarrRevision != "" && !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(c.DownloadarrRevision)) {
		return c, errors.New("DOWNLOADARR_URL and DOWNLOADARR_REVISION require a paired URL and full Git revision")
	}
	if c.DownloadarrURL != "" && (c.PlexURL == "" || (c.Env != "development" && !strings.HasPrefix(c.DownloadarrURL, "https://"))) {
		return c, errors.New("Downloadarr requires Plex and HTTPS outside local development")
	}
	if (c.PlexURL == "") != (c.PlexServerID == "") {
		return c, errors.New("PLEX_URL and PLEX_SERVER_ID must be set together")
	}
	if (c.SeerrURL == "") != (c.SeerrAPIKey == "") {
		return c, errors.New("SEERR_URL and SEERR_API_KEY must be set together")
	}
	if (c.RadarrURL == "") != (c.RadarrAPIKey == "") || (c.SonarrURL == "") != (c.SonarrAPIKey == "") {
		return c, errors.New("Arr URL and API key must be set together")
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
