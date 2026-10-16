package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hloiseau/le-concierge/internal/discord"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) subscriptionMessage(ctx context.Context, u store.User, command string, options discord.Options) string {
	if !s.subscriptionsEnabled() {
		return "Les suivis ne sont pas encore configurés."
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	link := "\nGérer vos suivis et vous désabonner : " + s.Config.PublicOrigin + "/suivis"
	if command == "suivre" {
		result, err := s.createFollow(ctx, u, subscriptionInput{SubscriptionTarget: store.SubscriptionTarget{Type: options.Kind, TMDBID: int64(options.TMDBID), Season: options.Season}, Channel: "hub"})
		if err != nil {
			return followErrorMessage(err) + link
		}
		if result.Status == "already_available" {
			return "**Déjà disponible dans votre Plex**\n" + safeText(result.Title) + "\n" + result.URL + "\nAucun suivi en attente n’a été créé." + link
		}
		return "**Suivi enregistré dans le Hub**\n" + safeText(result.Title) + "\nLa disponibilité y sera vérifiée à l’ouverture de vos suivis. Pour une série ou une saison, il s’agit du premier épisode accessible." + link
	}
	offset := (options.Page - 1) * 10
	items, total, err := s.listFollows(ctx, u, offset)
	if err != nil {
		return followErrorMessage(err) + link
	}
	text := fmt.Sprintf("**Vos suivis**\nPage %d · %d suivi(s)\n", options.Page, total)
	if len(items) == 0 {
		text += "Aucun suivi accessible sur cette page.\n"
	}
	for _, item := range items {
		status := "En attente"
		if item.Status == "available" {
			status = "Disponible dans votre Plex"
		} else if item.Status == "unavailable" {
			status = "Accès indisponible"
		}
		line := "• " + safeText(item.Title) + " — " + status
		if item.Season != nil {
			line += fmt.Sprintf(" · saison %d", *item.Season)
		}
		line += "\n"
		if len(text)+len(line) > 1500 {
			text += "Suite dans le Hub.\n"
			break
		}
		text += line
	}
	if offset+10 < total {
		text += fmt.Sprintf("Page suivante : /mes-suivis page:%d\n", options.Page+1)
	}
	return text + link
}

func followErrorMessage(err error) string {
	switch {
	case errors.Is(err, upstream.ErrSeerrUserNotFound):
		return seerrMissing
	case errors.Is(err, store.ErrSubscriptionLimit):
		return "Vous avez atteint la limite de 100 suivis."
	case errors.Is(err, upstream.ErrDenied), errors.Is(err, upstream.ErrNotFound):
		return "Ce contenu n’est pas accessible ou ne possède pas de demande active pour ce suivi."
	default:
		return "Les suivis ne peuvent pas être vérifiés actuellement. Réessayez plus tard."
	}
}
