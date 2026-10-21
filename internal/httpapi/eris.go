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
	SeerrID                                   int64
	Candidate                                 upstream.RequestCandidate
	Seasons                                   []int
	Plan                                      eris.Plan
	Expires                                   time.Time
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
	userID, private, component, err := s.erisCaller(ctx, input)
	if err != nil {
		problem(w, 403, "invalid_caller", "Interaction refusée.")
		return
	}
	if component == "" && (input.Plan.Validate() != nil || input.Plan.Action == "chat") {
		problem(w, 400, "invalid_plan", "Outil refusé.")
		return
	}
	write := func(reply eris.ToolReply) { jsonOut(w, 200, reply) }
	message := func(text string) { write(eris.ToolReply{Content: text}) }
	// No private requests or watch filters are read for public replies.
	if !private && (input.Plan.Action == "status" || input.Plan.Unwatched) {
		message("Pour tes demandes ou tes contenus non vus, parle-moi en privé avec /eris message:… ; ces informations restent personnelles.")
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
	if component != "" {
		reply = s.erisConfirmation(ctx, u, userID, component)
	} else {
		reply = s.runErisTool(ctx, u, grant, userID, input.Plan, private)
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
	write(reply)
}

func (s *Server) erisCaller(ctx context.Context, in eris.ToolRequest) (string, bool, string, error) {
	if len(in.Interaction) > 0 {
		if in.ChannelID != "" || in.MessageID != "" {
			return "", false, "", upstream.ErrDenied
		}
		i, err := discord.Verify(s.Config.DiscordPublicKey, in.Timestamp, in.Signature, in.Interaction, time.Now())
		if err != nil || i.ApplicationID != s.Config.DiscordClientID || i.GuildID != s.Config.DiscordGuildID || !config.Snowflake(i.ID) || !config.Snowflake(i.Member.User.ID) {
			return "", false, "", upstream.ErrDenied
		}
		command, _, e := discord.Command(i)
		if e != nil || command != "eris" {
			return "", false, "", upstream.ErrDenied
		}
		component := ""
		if i.Type == 3 {
			if i.Message.Author.ID != s.Config.DiscordClientID {
				return "", false, "", upstream.ErrDenied
			}
			component = i.Data.CustomID
		}
		if !s.erisEvent("eris:" + i.ID) {
			return "", false, "", upstream.ErrDenied
		}
		return i.Member.User.ID, true, component, nil
	}
	if in.Timestamp != "" || in.Signature != "" || !config.Snowflake(in.ChannelID) || !config.Snowflake(in.MessageID) || s.Config.DiscordBotToken == "" {
		return "", false, "", upstream.ErrDenied
	}
	// Gateway events have no signature: re-read the message and channel from Discord.
	// Neither the model nor a caller-controlled privacy flag can choose the identity.
	headers := map[string]string{"Authorization": "Bot " + s.Config.DiscordBotToken}
	var channel discordgo.Channel
	if err := s.Upstream.JSON(ctx, "GET", "https://discord.com/api/v10/channels/"+in.ChannelID, headers, "", &channel); err != nil || channel.ID != in.ChannelID || channel.GuildID != s.Config.DiscordGuildID {
		return "", false, "", upstream.ErrDenied
	}
	var m discordgo.Message
	if err := s.Upstream.JSON(ctx, "GET", "https://discord.com/api/v10/channels/"+in.ChannelID+"/messages/"+in.MessageID, headers, "", &m); err != nil {
		return "", false, "", upstream.ErrDenied
	}
	if m.ID != in.MessageID || m.ChannelID != in.ChannelID || m.Author == nil || m.Author.Bot || !config.Snowflake(m.Author.ID) || m.WebhookID != "" || (m.Type != discordgo.MessageTypeDefault && m.Type != discordgo.MessageTypeReply) || time.Since(m.Timestamp) > 5*time.Minute || time.Until(m.Timestamp) > 30*time.Second {
		return "", false, "", upstream.ErrDenied
	}
	m.GuildID = channel.GuildID
	if question, addressed := discord.ErisQuestion(&m, s.Config.DiscordClientID); !addressed || strings.TrimSpace(question) == "" || !s.erisEvent("eris:mention:"+m.ID) {
		return "", false, "", upstream.ErrDenied
	}
	return m.Author.ID, false, "", nil
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
	case "search", "recommend":
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
		if !private {
			key := s.saveErisPending(erisPending{UserID: u.ID, PlexID: u.PlexID, DiscordID: discordID, GuildID: s.Config.DiscordGuildID, Plan: p, Stage: "open", Expires: time.Now().Add(10 * time.Minute)})
			if key == "" {
				return eris.ToolReply{Content: "Trop de choix en attente. Réessaie dans quelques minutes."}
			}
			return eris.ToolReply{Content: "On continue en privé avec le bouton ci-dessous. Tu pourras choisir le titre et confirmer avant tout envoi à Seerr.", Buttons: []eris.Button{{ID: "eris:open:" + key, Label: "Préparer en privé"}}}
		}
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
		reply.Content = "Quel titre veux-tu demander ? Rien ne sera lancé avant ta confirmation."
		if len(items) == 0 {
			reply.Content = "Aucun titre trouvé dans le catalogue de demandes. Précise le titre ou l’année."
		}
		for index, m := range items {
			if m.Kind == "tv" && len(p.Seasons) == 0 {
				continue
			}
			pending := erisPending{UserID: u.ID, PlexID: u.PlexID, DiscordID: discordID, GuildID: s.Config.DiscordGuildID, SeerrID: id, Candidate: m, Seasons: p.Seasons, Stage: "choose", Expires: time.Now().Add(10 * time.Minute)}
			key := s.saveErisPending(pending)
			if key == "" {
				return eris.ToolReply{Content: "Trop de choix en attente. Réessaie dans quelques minutes."}
			}
			reply.Content += fmt.Sprintf("\n%d. %s", index+1, candidateLabel(m, p.Seasons))
			reply.Buttons = append(reply.Buttons, eris.Button{ID: "eris:choose:" + key, Label: fmt.Sprintf("Choisir %d", index+1)})
		}
		if len(items) > 0 && len(reply.Buttons) == 0 {
			reply.Content = "Précise le numéro de saison pour demander cette série."
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

func (s *Server) erisConfirmation(ctx context.Context, u store.User, discordID, customID string) eris.ToolReply {
	parts := strings.Split(customID, ":")
	invalid := eris.ToolReply{Content: "Ce choix a expiré ou a déjà été utilisé. Relance ta demande avec /eris."}
	if len(parts) != 3 {
		return invalid
	}
	s.mu.Lock()
	p, ok := s.erisPending[parts[2]]
	if !ok || time.Now().After(p.Expires) || p.UserID != u.ID || p.PlexID != u.PlexID || p.DiscordID != discordID || p.GuildID != s.Config.DiscordGuildID || (parts[1] != "cancel" && parts[1] != p.Stage) {
		s.mu.Unlock()
		return invalid
	}
	// Consume before network work: concurrent/replayed clicks cannot issue a second POST.
	delete(s.erisPending, parts[2])
	s.mu.Unlock()
	if parts[1] == "cancel" {
		return eris.ToolReply{Content: "Demande annulée. Rien n’a été envoyé."}
	}
	if parts[1] == "open" {
		g, err := s.grant(ctx, u)
		if err != nil {
			return invalid
		}
		return s.runErisTool(ctx, u, g, discordID, p.Plan, true)
	}
	if parts[1] == "choose" {
		p.Stage = "confirm"
		key := s.saveErisPending(p)
		if key == "" {
			return invalid
		}
		return eris.ToolReply{Content: "Confirmer la demande de **" + candidateLabel(p.Candidate, p.Seasons) + "** ?\nQualité standard, avec les droits et quotas de ton compte Seerr. Le téléchargement peut démarrer si la demande est approuvée.", Buttons: []eris.Button{{ID: "eris:confirm:" + key, Label: "Confirmer la demande"}, {ID: "eris:cancel:" + key, Label: "Annuler"}}}
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
