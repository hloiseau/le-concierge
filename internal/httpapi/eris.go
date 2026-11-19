package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/discord"
	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

var erisToolCallID = regexp.MustCompile(`^tool-[1-9][0-9]?$`)

func (s *Server) erisTool(w http.ResponseWriter, r *http.Request) {
	if len(s.Config.BotAPISecret) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.Config.BotAPISecret)) != 1 {
		problem(w, 401, "unauthorized", "Service non autorisé.")
		return
	}
	var input eris.ToolRequest
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF {
		problem(w, 400, "invalid_body", "Requête invalide.")
		return
	}
	if !erisToolCallID.MatchString(input.CallID) || input.Plan.Validate() != nil || input.Plan.Action == "chat" || input.Plan.Action == "silent" {
		problem(w, 400, "invalid_call", "Appel outil invalide.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	caller, err := s.erisCaller(ctx, input)
	if err != nil {
		problem(w, 403, "invalid_caller", "Interaction refusée.")
		return
	}
	write := func(reply eris.ToolReply) {
		if utf8.RuneCountInString(reply.Content) > 12000 {
			reply = erisFailure("tool_result_too_large")
		}
		jsonOut(w, 200, reply)
	}
	fail := func(code string) { write(erisFailure(code)) }
	if !caller.Private && (input.Plan.Action == "preferences" || input.Plan.Unwatched) {
		fail("private_conversation_required")
		return
	}
	if input.Plan.Action == "poll" || input.Plan.Action == "event" {
		if s.rate("eris-publish:"+caller.UserID, 2) {
			fail("rate_limited")
			return
		}
		write(s.erisDiscordCreation(ctx, caller, input.Plan))
		return
	}
	if input.Plan.Action == "web" || input.Plan.Action == "anime" {
		if s.rate("eris-web:"+caller.UserID, 10) || input.Plan.Action == "anime" && s.rate("eris-anime:global", 12) {
			fail("rate_limited")
			return
		}
		if input.Plan.Action == "anime" {
			write(s.erisAnime(ctx, input.Plan))
		} else {
			write(s.erisWeb(ctx, input.Plan))
		}
		return
	}
	u, err := s.Store.UserByDiscord(ctx, caller.UserID)
	var grant upstream.Grant
	if err == nil {
		grant, err = s.grant(ctx, u)
	}
	if err != nil {
		write(erisData(map[string]any{"error": "hub_access_required", "accountURL": s.Config.PublicOrigin + "/compte"}))
		return
	}
	if s.rate("eris-tools:"+u.ID, 20) {
		fail("rate_limited")
		return
	}
	var reply eris.ToolReply
	if input.Plan.Action == "lookup" || input.Plan.Action == "collection" || input.Plan.Action == "request" {
		reply = s.erisAgentMedia(ctx, u, caller, input.Plan)
	} else {
		reply = s.runErisTool(ctx, u, grant, caller.UserID, input.Plan, caller.Private)
	}
	current, err := s.Store.UserByDiscord(ctx, caller.UserID)
	if err == nil && current.ID == u.ID && current.PlexID == u.PlexID {
		_, err = s.grant(ctx, current)
	} else {
		err = upstream.ErrDenied
	}
	if err != nil {
		fail("access_expired")
		return
	}
	write(reply)
}

type erisCallerContext struct {
	UserID, ChannelID, Question, Reference string
	Private                                bool
	ReferenceByEris                        bool
}

func (s *Server) erisCaller(ctx context.Context, in eris.ToolRequest) (erisCallerContext, error) {
	if len(in.Interaction) > 0 {
		if in.ChannelID != "" || in.MessageID != "" || in.ContextMessageID != "" {
			return erisCallerContext{}, upstream.ErrDenied
		}
		i, err := discord.Verify(s.Config.DiscordPublicKey, in.Timestamp, in.Signature, in.Interaction, time.Now())
		if err != nil || i.ApplicationID != s.Config.DiscordClientID || i.GuildID != s.Config.DiscordGuildID || !config.Snowflake(i.ID) || !config.Snowflake(i.Member.User.ID) {
			return erisCallerContext{}, upstream.ErrDenied
		}
		command, _, e := discord.Command(i)
		if e != nil || command != "eris" {
			return erisCallerContext{}, upstream.ErrDenied
		}
		if i.Type != 2 {
			return erisCallerContext{}, upstream.ErrDenied
		}
		if !s.erisEvent("eris:" + i.ID + ":" + in.CallID) {
			return erisCallerContext{}, upstream.ErrDenied
		}
		options, _ := discord.CommandOptions(i)
		return erisCallerContext{UserID: i.Member.User.ID, ChannelID: i.ChannelID, Private: true, Question: options.ChatMessage}, nil
	}
	if in.Timestamp != "" || in.Signature != "" || !config.Snowflake(in.ChannelID) || !config.Snowflake(in.MessageID) || (in.ContextMessageID != "" && !config.Snowflake(in.ContextMessageID)) || s.Config.DiscordBotToken == "" {
		return erisCallerContext{}, upstream.ErrDenied
	}
	// Gateway events have no signature: re-read the message and channel from Discord.
	// Neither the model nor a caller-controlled privacy flag can choose the identity.
	headers := map[string]string{"Authorization": "Bot " + s.Config.DiscordBotToken}
	var channel discordgo.Channel
	if err := s.Upstream.JSON(ctx, "GET", "https://discord.com/api/v10/channels/"+in.ChannelID, headers, "", &channel); err != nil || channel.ID != in.ChannelID || channel.GuildID != s.Config.DiscordGuildID {
		return erisCallerContext{}, upstream.ErrDenied
	}
	var m discordgo.Message
	if err := s.Upstream.JSON(ctx, "GET", "https://discord.com/api/v10/channels/"+in.ChannelID+"/messages/"+in.MessageID, headers, "", &m); err != nil {
		return erisCallerContext{}, upstream.ErrDenied
	}
	if m.ID != in.MessageID || m.ChannelID != in.ChannelID || strings.TrimSpace(m.Content) == "" || m.Author == nil || m.Author.Bot || !config.Snowflake(m.Author.ID) || m.WebhookID != "" || (m.Type != discordgo.MessageTypeDefault && m.Type != discordgo.MessageTypeReply) || time.Since(m.Timestamp) > 5*time.Minute || time.Until(m.Timestamp) > 30*time.Second {
		return erisCallerContext{}, upstream.ErrDenied
	}
	m.GuildID = channel.GuildID
	question, addressed := discord.ErisQuestion(&m, s.Config.DiscordClientID)
	reference := ""
	referenceByEris := false
	if in.ContextMessageID != "" {
		if addressed {
			return erisCallerContext{}, upstream.ErrDenied
		}
		var previous []*discordgo.Message
		if err := s.Upstream.JSON(ctx, "GET", fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages?before=%s&limit=%d", in.ChannelID, in.MessageID, discord.ErisContextLimit), headers, "", &previous); err != nil {
			return erisCallerContext{}, upstream.ErrDenied
		}
		anchor, _ := discord.ErisContext(&m, previous, s.Config.DiscordClientID, in.ContextMessageID, time.Now())
		if anchor == nil {
			return erisCallerContext{}, upstream.ErrDenied
		}
		question, addressed = discord.ImplicitErisQuestion(&m, anchor, s.Config.DiscordClientID, time.Now())
		reference = anchor.Content
		referenceByEris = true
	}
	if !addressed || !s.erisEvent("eris:mention:"+m.ID+":"+in.CallID) {
		return erisCallerContext{}, upstream.ErrDenied
	}
	if in.ContextMessageID == "" && m.ReferencedMessage != nil {
		reference = m.ReferencedMessage.Content
		referenceByEris = m.ReferencedMessage.Author != nil && m.ReferencedMessage.Author.ID == s.Config.DiscordClientID
	}
	return erisCallerContext{UserID: m.Author.ID, ChannelID: channel.ID, Question: question, Reference: reference, ReferenceByEris: referenceByEris}, nil
}

func (s *Server) erisEvent(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, until := range s.seen {
		if now.After(until) {
			delete(s.seen, key)
		}
	}
	if _, ok := s.seen[id]; ok || len(s.seen) >= 10000 {
		return false
	}
	s.seen[id] = now.Add(5 * time.Minute)
	return true
}

func (s *Server) runErisTool(ctx context.Context, u store.User, g upstream.Grant, discordID string, p eris.Plan, private bool) eris.ToolReply {
	switch p.Action {
	case "gifts":
		return s.erisGifts(ctx, u, p, private)
	case "cinema":
		return s.erisCinema(ctx, u, p)
	case "diagnostic":
		return s.erisDiagnostic(ctx, u, g, p)
	case "follow", "follows", "unfollow":
		return s.erisFollows(ctx, u, g, p)
	case "preferences":
		return s.erisPreferences(ctx, u, g, p)
	case "monitoring":
		if !s.Config.MonitoringEnabled || s.Monitoring == nil {
			return erisFailure("monitoring_unavailable")
		}
		snapshot, err := s.Monitoring.Current(ctx, s.Config)
		if err != nil {
			return erisFailure("monitoring_unavailable")
		}
		return eris.ToolReply{Content: erisMetrics(snapshot.Metrics)}
	case "search", "recommend":
		if p.Action == "recommend" && p.Relation != "" {
			return s.erisRelatedMovies(ctx, u, g, discordID, p, private)
		}
		items, err := s.Upstream.AssistantMedia(ctx, s.Config.PlexURL, s.Config.PlexServerID, g, upstream.MediaFilter{Query: p.Query, Kind: p.Kind, Genre: p.Genre, MaxMinutes: p.MaxMinutes, Unwatched: p.Unwatched})
		if err != nil {
			return erisFailure("plex_catalogue_unavailable")
		}
		projected := []map[string]any{}
		for _, m := range items {
			item := map[string]any{"title": m.Title, "year": m.Year, "kind": m.Type, "minutes": m.Minutes, "genres": m.Genres, "summary": m.Summary, "playable": true}
			if private {
				item["url"] = m.URL
			}
			projected = append(projected, item)
		}
		return erisData(map[string]any{"catalogue": "plex", "items": projected, "unwatched": p.Unwatched})
	case "recent":
		page, err := s.Upstream.RecentPage(ctx, s.Config.PlexURL, s.Config.PlexServerID, g, 0, 3)
		if err != nil {
			return erisFailure("plex_recent_unavailable")
		}
		items := []map[string]any{}
		for _, m := range page.Items {
			item := map[string]any{"title": m.Title, "year": m.Year, "kind": m.Type, "subtitle": m.Subtitle, "addedAt": m.AddedAt}
			if private {
				item["url"] = m.URL
			}
			items = append(items, item)
		}
		return erisData(map[string]any{"catalogue": "plex", "items": items, "partial": page.NextOffset != nil})
	case "status":
		id, err := s.Upstream.SeerrUser(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, u.PlexID)
		if err != nil {
			return erisFailure("request_status_unavailable")
		}
		items, err := s.Upstream.FindPersonalRequests(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, id, p.Query)
		if err != nil {
			return erisFailure("request_status_unavailable")
		}
		projected := []map[string]any{}
		for i, m := range items {
			if i == 5 {
				break
			}
			projected = append(projected, map[string]any{"requestId": m.ID, "title": m.Title, "status": m.Status, "is4k": m.Is4k, "seasons": m.Seasons})
		}
		return erisData(map[string]any{"requests": projected, "partial": len(items) > 5})
	}
	return erisFailure("unsupported_tool")
}

// Hardware aggregates only; stale measurements never carry a numeric value.
func erisMetrics(metrics map[string]store.MetricValue) string {
	selected := map[string]store.MetricValue{}
	for _, key := range []string{"cpu_percent", "cpu_temperature", "memory_used_bytes", "memory_total_bytes", "gpu_video_percent", "storage_used_bytes", "storage_total_bytes"} {
		m := metrics[key]
		if m.Status != "available" || m.Value == nil {
			m.Value = nil
			if m.Status == "" || m.Status == "available" {
				m.Status = "unavailable"
			}
		}
		m.Unit = store.MetricUnits[key]
		selected[key] = m
	}
	raw, _ := json.Marshal(map[string]any{"metrics": selected})
	return string(raw)
}

func (s *Server) erisRelatedMovies(ctx context.Context, u store.User, g upstream.Grant, discordID string, p eris.Plan, private bool) eris.ToolReply {
	failure := erisFailure("recommendations_unavailable")
	id, err := s.Upstream.SeerrUser(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, u.PlexID)
	if err != nil {
		return failure
	}
	result, err := s.Upstream.RelatedMovies(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, id, p.Query, p.Relation)
	if errors.Is(err, upstream.ErrMediaReference) {
		return erisData(map[string]any{"status": "ambiguous_reference"})
	}
	if err != nil {
		return failure
	}
	if p.Source != "seerr" {
		result.Items, err = s.Upstream.RelatedPlayable(ctx, s.Config.PlexURL, s.Config.PlexServerID, g, result, p.Unwatched)
		if err != nil {
			return failure
		}
	} else {
		items := []upstream.RelatedMovie{}
		for _, m := range result.Items {
			if m.MediaInfo != nil && ((m.MediaInfo.Status != 1 && m.MediaInfo.Status != 7) || m.MediaInfo.Status4K == 5 || m.MediaInfo.Status4K == 6) {
				continue
			}
			items = append(items, m)
			if len(items) == 3 {
				break
			}
		}
		result.Items = items
	}
	items := []map[string]any{}
	for _, m := range result.Items {
		items = append(items, map[string]any{"id": m.ID, "title": m.Title, "year": m.Year, "overview": m.Overview, "sharedGenres": m.SharedGenres})
	}
	catalogue := "plex"
	if p.Source == "seerr" {
		catalogue = "seerr"
	}
	return erisData(map[string]any{"reference": result.Reference, "director": result.Director, "relation": p.Relation, "catalogue": catalogue, "playable": p.Source != "seerr", "unwatched": p.Unwatched, "items": items})
}
