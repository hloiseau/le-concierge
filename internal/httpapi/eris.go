package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

type erisPending struct {
	UserID, PlexID, DiscordID, GuildID, Stage string
	ChannelID                                 string
	Choices                                   []eris.Button
	ChoicePrompt                              string
	SeerrID                                   int64
	Candidate                                 upstream.RequestCandidate
	Seasons                                   []int
	Private                                   bool
	Expires                                   time.Time
	PersonalPlan                              *eris.Plan
}

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
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	caller, err := s.erisCaller(ctx, input)
	userID, private, component := caller.UserID, caller.Private, caller.Component
	if err != nil {
		problem(w, 403, "invalid_caller", "Interaction refusée.")
		return
	}
	if input.Choice != "" && (component != "" || input.Plan.Action != "") {
		problem(w, 400, "invalid_choice", "Choix invalide.")
		return
	}
	if component == "" && input.Choice == "" && (input.Plan.Validate() != nil || input.Plan.Action == "chat") {
		problem(w, 400, "invalid_plan", "Outil refusé.")
		return
	}
	write := func(reply eris.ToolReply) { jsonOut(w, 200, reply) }
	message := func(text string) { write(eris.ToolReply{Content: text}) }
	if component == "" && eris.IsMediaRemoval(caller.Question) && !s.erisUnfollowChoice(caller, input.Choice) {
		message(eris.RemovalExplanation)
		return
	}
	if component == "" && !eris.PersonalWriteRequested(caller.Question, input.Plan) {
		message("Précise le suivi ou le réglage que tu veux modifier. Je n’ai rien changé.")
		return
	}
	if !private && input.Plan.Action == "preferences" {
		message("Pour tes réglages personnels, utilise /eris message:… en privé.")
		return
	}
	// Watch history is private; request titles and statuses can be shared in-channel.
	if !private && input.Plan.Unwatched {
		message("Pour filtrer selon ce que tu as vu ou non, utilise /eris message:… en privé : ton historique de visionnage reste personnel.")
		return
	}
	if component == "" && input.Choice == "" && (input.Plan.Action == "poll" || input.Plan.Action == "event") {
		if s.rate("eris-publish:"+userID, 2) {
			message("Patiente un peu avant une autre création Discord.")
			return
		}
		write(s.erisDiscordCreation(ctx, caller, input.Plan))
		return
	}
	if component == "" && input.Choice == "" && (input.Plan.Action == "web" || input.Plan.Action == "anime") {
		if s.rate("eris-web:"+userID, 10) || (input.Plan.Action == "anime" && s.rate("eris-anime:global", 12)) {
			message("Patiente un peu avant une autre recherche web.")
			return
		}
		if input.Plan.Action == "anime" {
			write(s.erisAnime(ctx, input.Plan))
		} else {
			write(s.erisWeb(ctx, input.Plan))
		}
		return
	}
	u, err := s.Store.UserByDiscord(ctx, userID)
	var grant upstream.Grant
	if err == nil {
		grant, err = s.grant(ctx, u)
	}
	if err != nil {
		message("Connecte-toi au Hub avec Plex et lie Discord dans Mon compte : " + s.Config.PublicOrigin + "/compte")
		return
	}
	if s.rate("eris-tools:"+u.ID, 20) {
		message("Patiente un peu avant une autre recherche.")
		return
	}
	var reply eris.ToolReply
	if input.Choice != "" {
		reply = s.erisTextChoice(ctx, u, caller, input.Choice)
	} else if component != "" {
		reply = s.erisConfirmation(ctx, u, userID, component, private)
	} else {
		reply = s.runErisTool(ctx, u, grant, userID, input.Plan, private)
		if input.Plan.Action == "prepare" && len(reply.Buttons) == 1 {
			parts := strings.Split(reply.Buttons[0].ID, ":")
			s.mu.Lock()
			pending, ok := s.erisPending[parts[2]]
			s.mu.Unlock()
			if ok && eris.ExplicitRequest(caller.Question, pending.Candidate.Title, pending.Candidate.Year, pending.Candidate.Kind, pending.Seasons) {
				reply = s.resolveErisChoice(ctx, u, userID, reply.Buttons[0].ID, private, true)
			}
		}
	}
	current, e := s.Store.UserByDiscord(ctx, userID)
	if e == nil && current.ID == u.ID && current.PlexID == u.PlexID {
		_, e = s.grant(ctx, current)
	} else {
		e = upstream.ErrDenied
	}
	if e != nil {
		message("Ton accès a expiré. Reconnecte-toi au Hub avec Plex.")
		return
	}
	if utf8.RuneCountInString(reply.Content) > 1500 {
		reply.Content = string([]rune(reply.Content)[:1490]) + "…"
	}
	// Bind the entire offered set to the verified channel, never caller metadata.
	s.mu.Lock()
	for _, b := range reply.Buttons {
		parts := strings.Split(b.ID, ":")
		if len(parts) == 3 {
			if p, ok := s.erisPending[parts[2]]; ok {
				p.ChannelID, p.Private, p.Choices = caller.ChannelID, private, append([]eris.Button(nil), reply.Buttons...)
				p.ChoicePrompt = eris.NaturalChoices(reply).Content
				s.erisPending[parts[2]] = p
			}
		}
	}
	s.mu.Unlock()
	write(reply)
}

func (s *Server) erisUnfollowChoice(caller erisCallerContext, choice string) bool {
	parts := strings.Split(choice, ":")
	if len(parts) != 3 || parts[0] != "eris" {
		return false
	}
	s.mu.Lock()
	pending, exists := s.erisPending[parts[2]]
	s.mu.Unlock()
	if !exists || pending.PersonalPlan == nil || pending.PersonalPlan.Action != "unfollow" || pending.DiscordID != caller.UserID || pending.ChannelID != caller.ChannelID || pending.Private != caller.Private || time.Now().After(pending.Expires) {
		return false
	}
	id, matched := eris.MatchChoice(caller.Question, pending.Choices)
	return matched && id == choice
}

type erisCallerContext struct {
	UserID, ChannelID, Component, Question, Reference string
	Private                                           bool
	ReferenceByEris                                   bool
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
		component := ""
		if i.Type == 3 {
			if i.Message.Author.ID != s.Config.DiscordClientID {
				return erisCallerContext{}, upstream.ErrDenied
			}
			component = i.Data.CustomID
		}
		if !s.erisEvent("eris:" + i.ID) {
			return erisCallerContext{}, upstream.ErrDenied
		}
		options, _ := discord.CommandOptions(i)
		return erisCallerContext{UserID: i.Member.User.ID, ChannelID: i.ChannelID, Private: i.Type != 3 || i.Message.Flags&64 != 0, Component: component, Question: options.ChatMessage}, nil
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
	if m.ID != in.MessageID || m.ChannelID != in.ChannelID || m.Author == nil || m.Author.Bot || !config.Snowflake(m.Author.ID) || m.WebhookID != "" || (m.Type != discordgo.MessageTypeDefault && m.Type != discordgo.MessageTypeReply) || time.Since(m.Timestamp) > 5*time.Minute || time.Until(m.Timestamp) > 30*time.Second {
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
	if !addressed || strings.TrimSpace(question) == "" || !s.erisEvent("eris:mention:"+m.ID) {
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
	reply := eris.ToolReply{Content: "Je ne peux pas vérifier ces informations pour le moment. Réessaie plus tard."}
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
			return eris.ToolReply{Content: "Mes capteurs ne sont pas branchés : les mesures du NAS ne sont pas activées 👀"}
		}
		snapshot, err := s.Monitoring.Current(ctx, s.Config)
		if err != nil {
			return reply
		}
		reply.Content = erisMetrics(snapshot.Metrics)
	case "search", "recommend":
		if p.Action == "recommend" && p.Relation != "" {
			return s.erisRelatedMovies(ctx, u, g, discordID, p, private)
		}
		items, err := s.Upstream.AssistantMedia(ctx, s.Config.PlexURL, s.Config.PlexServerID, g, upstream.MediaFilter{Query: p.Query, Kind: p.Kind, Genre: p.Genre, MaxMinutes: p.MaxMinutes, Unwatched: p.Unwatched})
		if err != nil {
			return reply
		}
		if len(items) == 0 {
			reply.Content = "Aucun titre lisible correspondant dans ton catalogue Plex. Tu peux préciser le titre ou élargir les critères."
			return reply
		}
		reply.Content = "**Disponibles sur Plex**"
		if p.Unwatched {
			reply.Content += " · non marqués vus"
		}
		if p.Genre != "" {
			reply.Content += " · " + safeText(p.Genre)
		}
		if p.MaxMinutes > 0 {
			reply.Content += fmt.Sprintf(" · moins de %d min", p.MaxMinutes)
		}
		for _, m := range items {
			kind := "film"
			if m.Type == "show" {
				kind = "série"
			}
			reply.Content += "\n\n**" + safeText(m.Title) + "**"
			if m.Year > 0 {
				reply.Content += fmt.Sprintf(" (%d)", m.Year)
			}
			reply.Content += " · " + kind
			if m.Minutes > 0 {
				reply.Content += fmt.Sprintf(" · %d min", m.Minutes)
				if m.Type == "show" {
					reply.Content += "/épisode"
				}
			}
			if len(m.Genres) > 0 {
				reply.Content += " · " + safeText(strings.Join(m.Genres, ", "))
			}
			if m.Summary != "" {
				reply.Content += "\n" + safeText(m.Summary)
			}
			if private {
				reply.Content += "\n[Ouvrir dans Plex](" + m.URL + ")"
			}
		}
	case "recent":
		page, err := s.Upstream.RecentPage(ctx, s.Config.PlexURL, s.Config.PlexServerID, g, 0, 3)
		if err == nil {
			reply.Content = recentMessage(page.Items, s.Config.PublicOrigin, private)
		}
	case "status":
		id, err := s.Upstream.SeerrUser(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, u.PlexID)
		if err != nil {
			return reply
		}
		items, err := s.Upstream.FindPersonalRequests(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, id, p.Query)
		if err != nil {
			return reply
		}
		reply.Content = "**Tes demandes**"
		if len(items) == 0 {
			reply.Content = "Aucune demande correspondante sur ton compte."
		}
		for index, m := range items {
			if index == 5 {
				reply.Content += "\nLa suite dans /mes-demandes."
				break
			}
			reply.Content += "\n• " + safeText(m.Title) + " — " + statusLabel(m.Status)
			if m.Is4k != nil && *m.Is4k {
				reply.Content += " · 4K"
			}
			if len(m.Seasons) > 0 {
				reply.Content += fmt.Sprintf(" · saisons %v", m.Seasons)
			}
		}
	case "prepare":
		if p.Kind == "show" && len(p.Seasons) == 0 {
			reply.Content = "Quelle saison souhaites-tu demander ? Précise le titre et le numéro de saison."
			return reply
		}
		id, err := s.Upstream.SeerrUser(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, u.PlexID)
		if err != nil {
			return reply
		}
		items, err := s.Upstream.SearchRequestCandidates(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, id, p.Query, p.Kind)
		if err != nil {
			return reply
		}
		reply.Content = "J’ai trouvé ça dans Seerr. Choisis le bon titre :"
		if len(items) == 0 {
			reply.Content = "Aucun titre trouvé dans le catalogue de demandes. Précise le titre ou l’année."
		}
		for index, m := range items {
			if m.Kind == "tv" && len(p.Seasons) == 0 {
				continue
			}
			pending := erisPending{UserID: u.ID, PlexID: u.PlexID, DiscordID: discordID, GuildID: s.Config.DiscordGuildID, SeerrID: id, Candidate: m, Seasons: p.Seasons, Private: private, Stage: "choose", Expires: time.Now().Add(10 * time.Minute)}
			key := s.saveErisPending(pending)
			if key == "" {
				return eris.ToolReply{Content: "Trop de choix en attente. Réessaie dans quelques minutes."}
			}
			reply.Content += fmt.Sprintf("\n%d. %s", index+1, candidateLabel(m, p.Seasons))
			reply.Buttons = append(reply.Buttons, eris.Button{ID: "eris:choose:" + key, Label: fmt.Sprintf("Choisir %d", index+1), Title: m.Title, Year: m.Year, Number: index + 1})
		}
		if len(items) > 0 && len(reply.Buttons) == 0 {
			reply.Content = "Précise le numéro de saison pour demander cette série."
		}
	}
	return reply
}

// Deliberately select aggregate hardware values, never viewers or host identifiers.
func erisMetrics(metrics map[string]store.MetricValue) string {
	value := func(key, format string, scale float64) string {
		m := metrics[key]
		if m.Status != "available" || m.Value == nil {
			return "indisponible"
		}
		return fmt.Sprintf(format, *m.Value/scale)
	}
	return "Le pouls du NAS 🩺\nCPU : " + value("cpu_percent", "%.0f %%", 1) + " · " + value("cpu_temperature", "%.0f °C", 1) +
		"\nRAM : " + value("memory_used_bytes", "%.1f Gio", 1<<30) + " / " + value("memory_total_bytes", "%.1f Gio", 1<<30) +
		"\nGPU vidéo : " + value("gpu_video_percent", "%.0f %%", 1) +
		"\nStockage : " + value("storage_used_bytes", "%.1f Tio", 1<<40) + " / " + value("storage_total_bytes", "%.1f Tio", 1<<40)
}

func (s *Server) erisRelatedMovies(ctx context.Context, u store.User, g upstream.Grant, discordID string, p eris.Plan, private bool) eris.ToolReply {
	failure := eris.ToolReply{Content: "Je n’ai pas pu vérifier les recommandations dans Seerr. Réessaie dans un instant."}
	id, err := s.Upstream.SeerrUser(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, u.PlexID)
	if err != nil {
		return failure
	}
	result, err := s.Upstream.RelatedMovies(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, id, p.Query, p.Relation)
	if errors.Is(err, upstream.ErrMediaReference) {
		return eris.ToolReply{Content: "Je ne trouve pas ce film de référence sans ambiguïté. Précise son titre exact."}
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
	if len(result.Items) == 0 {
		if p.Source == "seerr" {
			return eris.ToolReply{Content: "Je n’ai trouvé aucun autre film correspondant à proposer dans Seerr sans contenu déjà disponible ou en cours de demande."}
		}
		return eris.ToolReply{Content: "Je n’ai trouvé aucun film correspondant lisible dans ton Plex parmi ces recommandations. Je peux aussi chercher dans Seerr."}
	}
	reply := eris.ToolReply{Content: "Si tu veux prolonger **" + safeText(result.Reference.Title) + "** 🍿"}
	if p.Relation == "director" {
		reply.Content += " : voilà ce que **" + safeText(result.Director) + "** a aussi réalisé"
	} else {
		reply.Content += " : quelques cousins à essayer"
	}
	if p.Source != "seerr" {
		reply.Content += " · disponibles sur Plex"
	}
	if p.Unwatched {
		reply.Content += " · non marqués vus"
	}
	for index, m := range result.Items {
		reply.Content += fmt.Sprintf("\n\n%d. **%s**", index+1, safeText(m.Title))
		if m.Year != "" {
			reply.Content += " (" + m.Year + ")"
		}
		if len(m.SharedGenres) > 0 {
			reply.Content += "\n" + safeText(strings.Join(m.SharedGenres, ", "))
		}
		if m.Overview != "" {
			reply.Content += "\n" + safeText(m.Overview)
		}
		if p.Source == "seerr" {
			pending := erisPending{UserID: u.ID, PlexID: u.PlexID, DiscordID: discordID, GuildID: s.Config.DiscordGuildID, SeerrID: id, Candidate: m.RequestCandidate, Private: private, Stage: "choose", Expires: time.Now().Add(10 * time.Minute)}
			key := s.saveErisPending(pending)
			if key == "" {
				return eris.ToolReply{Content: "Trop de choix en attente. Réessaie dans quelques minutes."}
			}
			reply.Buttons = append(reply.Buttons, eris.Button{ID: "eris:choose:" + key, Label: fmt.Sprintf("Demander %d", index+1), Title: m.Title, Year: m.Year, Number: index + 1})
		}
	}
	return reply
}

func candidateLabel(m upstream.RequestCandidate, seasons []int) string {
	title := safeText(m.Title)
	if m.Year != "" {
		title += " (" + m.Year + ")"
	}
	if m.Kind == "tv" {
		title += fmt.Sprintf(" · série, saisons %v", seasons)
	} else {
		title += " · film"
	}
	return title
}

func (s *Server) saveErisPending(p erisPending) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.erisPending == nil {
		s.erisPending = map[string]erisPending{}
	}
	for key, entry := range s.erisPending {
		if time.Now().After(entry.Expires) {
			delete(s.erisPending, key)
		}
	}
	if len(s.erisPending) >= 256 {
		return ""
	}
	key := token()
	s.erisPending[key] = p
	return key
}

func (s *Server) erisConfirmation(ctx context.Context, u store.User, discordID, customID string, private bool) eris.ToolReply {
	return s.resolveErisChoice(ctx, u, discordID, customID, private, false)
}

func (s *Server) resolveErisChoice(ctx context.Context, u store.User, discordID, customID string, private, submit bool) eris.ToolReply {
	parts := strings.Split(customID, ":")
	invalid := eris.ToolReply{Content: "Ce choix a expiré ou a déjà été utilisé. Relance ta demande avec /eris."}
	if len(parts) != 3 {
		return invalid
	}
	s.mu.Lock()
	p, ok := s.erisPending[parts[2]]
	if !ok || time.Now().After(p.Expires) || p.UserID != u.ID || p.PlexID != u.PlexID || p.DiscordID != discordID || p.GuildID != s.Config.DiscordGuildID || p.Private != private || (parts[1] != "cancel" && parts[1] != p.Stage) {
		s.mu.Unlock()
		return invalid
	}
	// Consume before network work: concurrent/replayed clicks cannot issue a second POST.
	delete(s.erisPending, parts[2])
	for _, choice := range p.Choices {
		if fields := strings.Split(choice.ID, ":"); len(fields) == 3 {
			delete(s.erisPending, fields[2])
		}
	}
	s.mu.Unlock()
	if parts[1] == "cancel" {
		if p.PersonalPlan != nil {
			return eris.ToolReply{Content: "Choix abandonné. Aucun suivi modifié."}
		}
		return eris.ToolReply{Content: "Demande annulée. Rien n’a été envoyé."}
	}
	if p.PersonalPlan != nil {
		current, err := s.Store.UserByDiscord(ctx, discordID)
		if err != nil || current.ID != u.ID || current.PlexID != u.PlexID {
			return invalid
		}
		g, err := s.grant(ctx, current)
		if err != nil {
			return invalid
		}
		if p.PersonalPlan.Action == "unfollow" {
			return s.erisFollows(ctx, current, g, *p.PersonalPlan)
		}
		if p.PersonalPlan.Action != "follow" {
			return invalid
		}
		member, err := s.resolveSeerr(ctx, current)
		if err != nil || member != p.SeerrID {
			return invalid
		}
		select {
		case s.reminderGate <- struct{}{}:
			defer func() { <-s.reminderGate }()
		case <-ctx.Done():
			return invalid
		}
		return s.erisFollowCandidate(ctx, current, *p.PersonalPlan, p.Candidate)
	}
	if parts[1] == "choose" && !submit {
		p.Stage = "confirm"
		key := s.saveErisPending(p)
		if key == "" {
			return invalid
		}
		return eris.ToolReply{Content: "Je demande **" + candidateLabel(p.Candidate, p.Seasons) + "** pour toi ?\nQualité standard. Si Seerr l’approuve, le téléchargement pourra démarrer.", Buttons: []eris.Button{{ID: "eris:confirm:" + key, Label: "Confirmer la demande"}, {ID: "eris:cancel:" + key, Label: "Annuler"}}}
	}
	id, err := s.Upstream.SeerrUser(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, u.PlexID)
	current, e := s.Store.UserByDiscord(ctx, discordID)
	if err != nil || e != nil || id != p.SeerrID || current.ID != u.ID || current.PlexID != u.PlexID {
		return invalid
	}
	if _, err = s.grant(ctx, current); err != nil {
		return invalid
	}
	requestID, err := s.Upstream.CreateMediaRequest(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, id, p.Candidate, p.Seasons)
	switch {
	case err == nil:
		return eris.ToolReply{Content: fmt.Sprintf("Demande #%d créée : %s. Tu peux suivre son état avec /mes-demandes.", requestID, candidateLabel(p.Candidate, p.Seasons))}
	case errors.Is(err, upstream.ErrDenied):
		return eris.ToolReply{Content: "Seerr a refusé la demande : vérifie tes droits ou ton quota dans le Hub."}
	case errors.Is(err, upstream.ErrRequestConflict):
		return eris.ToolReply{Content: "Ce contenu est déjà demandé ou aucune saison supplémentaire n’est à demander. Vérifie /mes-demandes ou Seerr."}
	default:
		return eris.ToolReply{Content: "Je ne peux pas confirmer l’enregistrement. Vérifie /mes-demandes ou Seerr avant de réessayer ; je ne renvoie pas automatiquement la demande."}
	}
}

func (s *Server) erisTextChoice(ctx context.Context, u store.User, caller erisCallerContext, choice string) eris.ToolReply {
	invalid := eris.ToolReply{Content: "Je n’ai pas de choix correspondant ici. Redis-moi le titre à demander."}
	parts := strings.Split(choice, ":")
	if len(parts) != 3 || parts[0] != "eris" {
		return invalid
	}
	s.mu.Lock()
	p, ok := s.erisPending[parts[2]]
	s.mu.Unlock()
	if !ok || caller.ChannelID == "" || caller.ChannelID != p.ChannelID {
		return invalid
	}
	id, matched := eris.MatchChoice(caller.Question, p.Choices)
	if !matched || id != choice {
		return invalid
	}
	// A reply elsewhere cannot approve this title even if it says "yes".
	if parts[1] == "confirm" && caller.Reference != "" && !strings.Contains(caller.Reference, candidateLabel(p.Candidate, p.Seasons)) {
		return invalid
	}
	if parts[1] == "choose" && caller.Reference != "" && caller.Reference != p.ChoicePrompt && caller.Reference != eris.ChoiceClarification {
		return invalid
	}
	return s.resolveErisChoice(ctx, u, caller.UserID, choice, caller.Private, true)
}
