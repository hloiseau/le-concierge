package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) erisFollows(ctx context.Context, u store.User, g upstream.Grant, p eris.Plan) eris.ToolReply {
	failure := func(err error) eris.ToolReply { return eris.ToolReply{Content: followErrorMessage(err)} }
	if p.Validate() != nil {
		return failure(upstream.ErrDenied)
	}
	if !s.subscriptionsEnabled() {
		return eris.ToolReply{Content: "Les suivis ne sont pas encore configurés."}
	}
	if p.Action == "follows" {
		items, total, err := s.listFollows(ctx, u, 0)
		if err != nil {
			return failure(err)
		}
		text := "Tu n’as aucun suivi de disponibilité."
		if len(items) > 0 {
			text = "**Tes suivis de disponibilité**"
		}
		for i, item := range items {
			if i == 5 {
				break
			}
			text += fmt.Sprintf("\n• #%d · %s", item.ID, safeText(item.Title))
			if item.Season != nil {
				text += fmt.Sprintf(" · saison %d", *item.Season)
			}
		}
		if total > 5 {
			text += "\nLa suite : " + s.Config.PublicOrigin + "/demandes"
		}
		return eris.ToolReply{Content: text}
	}
	select {
	case s.reminderGate <- struct{}{}:
		defer func() { <-s.reminderGate }()
	case <-ctx.Done():
		return failure(ctx.Err())
	}
	if p.Action == "unfollow" {
		err := s.erisPersonalMutation(ctx, u, g, func() error { return s.Store.DeleteSubscription(ctx, u.ID, p.SubscriptionID) })
		if errors.Is(err, sql.ErrNoRows) {
			return eris.ToolReply{Content: "Je ne trouve pas ce suivi sur ton compte. Je n’ai rien retiré."}
		}
		if err != nil {
			return failure(err)
		}
		return eris.ToolReply{Content: fmt.Sprintf("Suivi #%d retiré. La demande Seerr et le média sont conservés.", p.SubscriptionID)}
	}
	member, err := s.resolveSeerr(ctx, u)
	if err != nil {
		return failure(err)
	}
	m, choices, err := s.Upstream.ResolveMediaReference(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, member, p.Query, p.Kind, p.Year)
	if errors.Is(err, upstream.ErrMediaReference) {
		text := "Précise le titre, l’année et le type du contenu à suivre :"
		for i, c := range choices {
			if i == 3 {
				break
			}
			text += "\n• " + safeText(c.Title) + " (" + c.Year + ") · " + c.Kind
		}
		return eris.ToolReply{Content: text}
	}
	if err != nil {
		return failure(err)
	}
	target := store.SubscriptionTarget{Type: m.Kind, TMDBID: m.ID}
	if len(p.Seasons) == 1 {
		target.Season = &p.Seasons[0]
	}
	channel := p.Channel
	if channel == "" {
		channel = "hub"
	}
	result, err := s.createFollow(ctx, u, subscriptionInput{SubscriptionTarget: target, Channel: channel})
	if errors.Is(err, errSubscriptionChannel) {
		return eris.ToolReply{Content: "Le suivi en DM nécessite ton consentement aux notifications et aux messages privés, ainsi que leur activation sur le serveur. Règle-les en privé avec /eris ou dans Mon compte ; je n’ai pas activé de DM."}
	}
	if err != nil {
		return failure(err)
	}
	if result.Status == "already_available" {
		return eris.ToolReply{Content: "**" + safeText(result.Title) + "** est déjà accessible dans ton Plex. Aucun suivi en attente n’a été créé."}
	}
	text := fmt.Sprintf("Suivi #%d enregistré pour **%s** dans le Hub. La disponibilité sera vérifiée à l’ouverture de tes suivis.", result.Subscription.ID, safeText(result.Title))
	if channel == "discord" {
		text = fmt.Sprintf("Suivi #%d enregistré pour **%s** avec alerte Discord en DM lorsque la disponibilité sera confirmée. Le DM n’a pas encore été envoyé.", result.Subscription.ID, safeText(result.Title))
	}
	if target.Type == "tv" {
		text += " Pour une série ou une saison, la disponibilité correspond au premier épisode accessible."
	}
	return eris.ToolReply{Content: text}
}

// Called under the delivery gate; authorization and identity cannot change mid-write.
func (s *Server) erisPersonalMutation(ctx context.Context, u store.User, g upstream.Grant, write func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.grants[u.ID]
	owner, err := s.Store.User(ctx, u.ID)
	if err != nil || !exists || current.PlexID != u.PlexID || owner.PlexID != u.PlexID || owner.DiscordID != u.DiscordID || !current.Active(time.Now()) || current.AccountToken != g.AccountToken || current.ServerToken != g.ServerToken {
		return upstream.ErrDenied
	}
	return write()
}

func (s *Server) erisPreferences(ctx context.Context, u store.User, g upstream.Grant, p eris.Plan) eris.ToolReply {
	failure := eris.ToolReply{Content: "Je n’ai pas pu vérifier tes réglages. Aucune modification confirmée."}
	if p.Validate() != nil {
		return failure
	}
	select {
	case s.reminderGate <- struct{}{}:
		defer func() { <-s.reminderGate }()
	case <-ctx.Done():
		return failure
	}
	var preferences store.ReminderPreferences
	err := s.erisPersonalMutation(ctx, u, g, func() error {
		var err error
		preferences, err = s.Store.GetReminderPreferences(ctx, u.ID)
		if err != nil || p.Setting == "" {
			return err
		}
		boolean := p.Value == "true"
		switch p.Setting {
		case "enabled":
			preferences.Enabled = boolean
		case "discordDmEnabled":
			preferences.DiscordDM = boolean
		case "allUnwatched":
			preferences.AllUnwatched = boolean
		case "availableUnwatched":
			preferences.AvailableUnwatched = boolean
		case "dailyCap":
			preferences.DailyCap, _ = strconv.Atoi(p.Value)
		case "quietHours":
			parts := strings.Split(p.Value, "-")
			preferences.QuietStartHour, _ = strconv.Atoi(parts[0])
			preferences.QuietEndHour, _ = strconv.Atoi(parts[1])
		case "daysBefore":
			preferences.DaysBefore = nil
			for _, part := range strings.Split(p.Value, ",") {
				day, _ := strconv.Atoi(part)
				preferences.DaysBefore = append(preferences.DaysBefore, day)
			}
		default:
			return errors.New("unknown preference")
		}
		if p.Setting == "discordDmEnabled" && boolean && (!s.Config.DiscordDMsEnabled || u.DiscordID == "") {
			return errSubscriptionChannel
		}
		if p.Setting == "availableUnwatched" && boolean && (!s.subscriptionsEnabled() || !s.departuresEnabled()) {
			return upstream.ErrUnavailable
		}
		if err := s.Store.SaveReminderPreferences(ctx, u.ID, preferences); err != nil {
			return err
		}
		verified, err := s.Store.GetReminderPreferences(ctx, u.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(verified, preferences) {
			return errors.New("preference verification failed")
		}
		preferences = verified
		return nil
	})
	if errors.Is(err, errSubscriptionChannel) {
		return eris.ToolReply{Content: "Les DM ne sont pas activés sur le serveur ou Discord n’est plus lié. Aucune modification confirmée."}
	}
	if err != nil {
		return failure
	}
	onoff := func(b bool) string {
		if b {
			return "activé"
		}
		return "désactivé"
	}
	text := "**Tes réglages de notifications**"
	if p.Setting != "" {
		text = "Réglage enregistré et relu dans le Hub."
	}
	text += fmt.Sprintf("\nNotifications : %s · DM Discord : %s\nHeures calmes : %d h–%d h (Europe/Paris) · plafond du récapitulatif : %d\nRappels avant retrait : %v jours · tous les non-vus : %s\nRelances de demandes disponibles non vues : %s", onoff(preferences.Enabled), onoff(preferences.DiscordDM), preferences.QuietStartHour, preferences.QuietEndHour, preferences.DailyCap, preferences.DaysBefore, onoff(preferences.AllUnwatched), onoff(preferences.AvailableUnwatched))
	return eris.ToolReply{Content: text}
}
