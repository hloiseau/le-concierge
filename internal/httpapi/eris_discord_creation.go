package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

type erisCreation struct {
	Expires time.Time
	Reply   eris.ToolReply
}

func (s *Server) erisDiscordCreation(ctx context.Context, caller erisCallerContext, p eris.Plan) eris.ToolReply {
	clarify := eris.ToolReply{Content: "Précise la création souhaitée, avec les choix du sondage ou le nom et les horaires de la soirée à publier."}
	if p.Validate() != nil || !config.Snowflake(caller.ChannelID) || s.Config.DiscordBotToken == "" {
		return clarify
	}
	requested := eris.DiscordCreationRequested(caller.Question, p)
	if !requested && caller.ReferenceByEris && (strings.Contains(strings.ToLower(caller.Reference), "précise") || strings.Contains(strings.ToLower(caller.Reference), "manque")) && (strings.Contains(strings.ToLower(caller.Reference), "sondage") || strings.Contains(strings.ToLower(caller.Reference), "soirée")) {
		requested = eris.PublicTextMentioned(p.Text, caller.Question, "")
		for _, option := range p.Options {
			requested = requested || eris.PublicTextMentioned(option, caller.Question, "")
		}
	}
	if !requested {
		return clarify
	}
	if p.Action == "poll" {
		for _, option := range p.Options {
			if !eris.PublicTextMentioned(option, caller.Question, caller.Reference) {
				return clarify
			}
		}
		if !eris.PublicTextMentioned(p.Text, caller.Question, caller.Reference) {
			p.Text = "Quel choix préférez-vous ?"
		}
		if p.DurationHours == 0 {
			p.DurationHours = 24
		}
	} else {
		if !eris.PublicTextMentioned(p.Text, caller.Question, caller.Reference) {
			return clarify
		}
		start, _ := time.Parse(time.RFC3339, p.StartAt)
		if !start.After(time.Now().Add(time.Minute)) || start.After(time.Now().AddDate(1, 0, 0)) {
			return eris.ToolReply{Content: "Donne une date de début future, dans l’année à venir, et une heure de fin après le début."}
		}
		if p.Query != "" && !eris.PublicTextMentioned(p.Query, caller.Question, caller.Reference) {
			p.Query = ""
		}
	}
	var channel discordgo.Channel
	if err := s.Upstream.JSON(ctx, "GET", "https://discord.com/api/v10/channels/"+caller.ChannelID, map[string]string{"Authorization": "Bot " + s.Config.DiscordBotToken}, "", &channel); err != nil || channel.ID != caller.ChannelID || channel.GuildID != s.Config.DiscordGuildID {
		return eris.ToolReply{Content: "Je ne peux pas vérifier le salon de publication."}
	}
	if p.Action == "event" {
		if !s.erisPublicEventChannel(ctx, channel) {
			return eris.ToolReply{Content: "Un événement du serveur est visible au-delà d’un salon privé. Demande sa création depuis un salon texte visible par tout le serveur, dont je peux vérifier les droits."}
		}
	}
	raw, _ := json.Marshal(p)
	key := digest(caller.UserID + ":" + caller.ChannelID + ":" + string(raw))
	unknown := eris.ToolReply{Content: "Je ne peux pas confirmer la création. Vérifie le salon ou les événements du serveur avant de réessayer ; je ne renvoie pas automatiquement cette création."}
	s.mu.Lock()
	if s.erisCreations == nil {
		s.erisCreations = map[string]erisCreation{}
	}
	for k, v := range s.erisCreations {
		if time.Now().After(v.Expires) {
			delete(s.erisCreations, k)
		}
	}
	if old, exists := s.erisCreations[key]; exists {
		s.mu.Unlock()
		return old.Reply
	}
	if len(s.erisCreations) >= 256 {
		s.mu.Unlock()
		return eris.ToolReply{Content: "Trop de créations récentes. Réessaie plus tard."}
	}
	// shortcut: local uncertainty lasts 15 min; use a durable ledger for longer recovery.
	s.erisCreations[key] = erisCreation{Expires: time.Now().Add(15 * time.Minute), Reply: unknown}
	s.mu.Unlock()
	finish := func(reply eris.ToolReply, definitelyNotCreated bool) eris.ToolReply {
		s.mu.Lock()
		defer s.mu.Unlock()
		if definitelyNotCreated {
			delete(s.erisCreations, key)
		} else {
			entry := s.erisCreations[key]
			entry.Reply = reply
			s.erisCreations[key] = entry
		}
		return reply
	}
	payload := map[string]any{}
	if p.Action == "poll" {
		answers := []map[string]any{}
		for _, option := range p.Options {
			answers = append(answers, map[string]any{"poll_media": map[string]string{"text": option}})
		}
		payload = map[string]any{"poll": map[string]any{"question": map[string]string{"text": p.Text}, "answers": answers, "duration": p.DurationHours, "allow_multiselect": p.Multiselect, "layout_type": 1}, "allowed_mentions": map[string]any{"parse": []string{}}, "nonce": key[:24], "enforce_nonce": true}
	} else {
		location := "https://discord.com/channels/" + s.Config.DiscordGuildID + "/" + caller.ChannelID
		payload = map[string]any{"name": p.Text, "privacy_level": 2, "entity_type": 3, "scheduled_start_time": p.StartAt, "scheduled_end_time": p.EndAt, "entity_metadata": map[string]string{"location": location}, "description": p.Query}
		var existing []upstream.DiscordCreated
		if err := s.Upstream.JSON(ctx, "GET", "https://discord.com/api/v10/guilds/"+s.Config.DiscordGuildID+"/scheduled-events", map[string]string{"Authorization": "Bot " + s.Config.DiscordBotToken}, "", &existing); err != nil || len(existing) > 1000 {
			return finish(eris.ToolReply{Content: "Je ne peux pas vérifier les événements existants ; je n’en crée pas un à l’aveugle."}, true)
		}
		start, _ := time.Parse(time.RFC3339, p.StartAt)
		end, _ := time.Parse(time.RFC3339, p.EndAt)
		for _, event := range existing {
			if config.Snowflake(event.ID) && event.GuildID == s.Config.DiscordGuildID && event.CreatorID == s.Config.DiscordClientID && event.Name == p.Text && event.EntityType == 3 && event.EntityMetadata.Location == location && event.StartAt.Equal(start) && event.EndAt.Equal(end) {
				return finish(eris.ToolReply{Content: "Cette soirée existe déjà : https://discord.com/events/" + s.Config.DiscordGuildID + "/" + event.ID}, false)
			}
		}
	}
	created, err := s.Upstream.CreateDiscord(ctx, s.Config.DiscordBotToken, s.Config.DiscordGuildID, caller.ChannelID, p.Action, payload)
	if errors.Is(err, upstream.ErrDenied) {
		return finish(eris.ToolReply{Content: "Discord a refusé la création. Vérifie les droits du bot dans ce salon : Envoyer des messages/sondages, ou Créer des événements pour une soirée."}, true)
	}
	if err != nil {
		return finish(unknown, false)
	}
	if p.Action == "poll" {
		if created.Author.ID != s.Config.DiscordClientID || created.Poll.Multiselect != p.Multiselect || created.Poll.Question.Text != p.Text || len(created.Poll.Answers) != len(p.Options) {
			return finish(unknown, false)
		}
		for i, answer := range created.Poll.Answers {
			if answer.Media.Text != p.Options[i] {
				return finish(unknown, false)
			}
		}
		return finish(eris.ToolReply{Content: "Sondage créé dans ce salon : https://discord.com/channels/" + s.Config.DiscordGuildID + "/" + caller.ChannelID + "/" + created.ID}, false)
	}
	start, _ := time.Parse(time.RFC3339, p.StartAt)
	end, _ := time.Parse(time.RFC3339, p.EndAt)
	if created.Name != p.Text || !created.StartAt.Equal(start) || !created.EndAt.Equal(end) || created.EntityMetadata.Location != "https://discord.com/channels/"+s.Config.DiscordGuildID+"/"+caller.ChannelID || created.CreatorID != "" && created.CreatorID != s.Config.DiscordClientID {
		return finish(unknown, false)
	}
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return finish(unknown, false)
	}
	return finish(eris.ToolReply{Content: fmt.Sprintf("Soirée créée : **%s**, du %s au %s (Europe/Paris).\nhttps://discord.com/events/%s/%s", safeText(p.Text), start.In(paris).Format("02/01/2006 15:04"), end.In(paris).Format("02/01/2006 15:04"), s.Config.DiscordGuildID, created.ID)}, false)
}

func (s *Server) erisPublicEventChannel(ctx context.Context, channel discordgo.Channel) bool {
	if channel.Type != discordgo.ChannelTypeGuildText && channel.Type != discordgo.ChannelTypeGuildNews {
		return false
	}
	var roles []struct {
		ID          string `json:"id"`
		Permissions string `json:"permissions"`
	}
	if err := s.Upstream.JSON(ctx, "GET", "https://discord.com/api/v10/guilds/"+s.Config.DiscordGuildID+"/roles", map[string]string{"Authorization": "Bot " + s.Config.DiscordBotToken}, "", &roles); err != nil || len(roles) > 250 {
		return false
	}
	for _, role := range roles {
		if role.ID != s.Config.DiscordGuildID {
			continue
		}
		permissions, err := strconv.ParseUint(role.Permissions, 10, 64)
		if err != nil {
			return false
		}
		if permissions&discordgo.PermissionAdministrator != 0 {
			return true
		}
		for _, overwrite := range channel.PermissionOverwrites {
			if overwrite.ID == role.ID {
				permissions = permissions&^uint64(overwrite.Deny) | uint64(overwrite.Allow)
			}
		}
		return permissions&discordgo.PermissionViewChannel != 0
	}
	return false
}
