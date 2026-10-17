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
	if command == "mes-suivis" {
		return s.requestFollowsMessage(ctx, u, options.Page)
	}
	if !s.subscriptionsEnabled() {
		return "Les suivis ne sont pas encore configurés."
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	link := "\nGérer vos suivis et vous désabonner : " + s.Config.PublicOrigin + "/suivis"
	result, err := s.createFollow(ctx, u, subscriptionInput{SubscriptionTarget: store.SubscriptionTarget{Type: options.Kind, TMDBID: int64(options.TMDBID), Season: options.Season}, Channel: "hub"})
	if err != nil {
		return followErrorMessage(err) + link
	}
	if result.Status == "already_available" {
		return "**Déjà disponible dans votre Plex**\n" + safeText(result.Title) + "\n" + result.URL + "\nAucun suivi en attente n’a été créé." + link
	}
	return "**Suivi enregistré dans le Hub**\n" + safeText(result.Title) + "\nLa disponibilité y sera vérifiée à l’ouverture de vos suivis. Pour une série ou une saison, il s’agit du premier épisode accessible." + link
}

func (s *Server) requestFollowsMessage(ctx context.Context, u store.User, page int) string {
	link := "\nRetrouver vos suivis : " + s.Config.PublicOrigin + "/suivis"
	if s.Config.SeerrURL == "" || s.Config.SeerrAPIKey == "" {
		return "Seerr n’est pas encore configuré." + link
	}
	seerrID, err := s.resolveSeerr(ctx, u)
	if err != nil {
		return followErrorMessage(err) + link
	}
	offset := (page - 1) * 10
	requests, err := s.Upstream.Requests(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, seerrID, offset)
	if err != nil {
		return "Vos demandes Seerr sont momentanément indisponibles." + link
	}
	text := fmt.Sprintf("**Vos suivis Seerr**\nPage %d · %d demande(s) suivie(s) automatiquement\n", page, requests.Total)
	if len(requests.Items) == 0 {
		text += "Aucune demande Seerr sur cette page.\n"
	}
	for _, item := range requests.Items {
		line := "• " + safeText(item.Title) + " — " + statusLabel(item.Status)
		if len(item.Seasons) > 0 {
			line += fmt.Sprintf(" · saisons %v", item.Seasons)
		}
		line += "\n"
		if len(text)+len(line) > 1500 {
			text += "Suite dans le Hub.\n"
			break
		}
		text += line
	}
	if offset+requests.Limit < requests.Total {
		text += fmt.Sprintf("Page suivante : /mes-suivis page:%d\n", page+1)
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
