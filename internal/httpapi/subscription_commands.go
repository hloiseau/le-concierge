package httpapi

import (
	"context"
	"errors"
	"time"

	"github.com/hloiseau/le-concierge/internal/discord"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) subscriptionMessage(ctx context.Context, u store.User, options discord.Options) string {
	if !s.subscriptionsEnabled() {
		return "Les suivis ne sont pas encore configurés."
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	link := "\nGérer vos demandes et notifications : " + s.Config.PublicOrigin + "/demandes"
	result, err := s.createFollow(ctx, u, subscriptionInput{SubscriptionTarget: store.SubscriptionTarget{Type: options.Kind, TMDBID: int64(options.TMDBID), Season: options.Season}, Channel: "hub"})
	if err != nil {
		return followErrorMessage(err) + link
	}
	if result.Status == "already_available" {
		return "**Déjà disponible dans votre Plex**\n" + safeText(result.Title) + "\n" + result.URL + "\nAucun suivi en attente n’a été créé." + link
	}
	return "**Suivi enregistré dans le Hub**\n" + safeText(result.Title) + "\nLa disponibilité y sera vérifiée à l’ouverture de vos suivis. Pour une série ou une saison, il s’agit du premier épisode accessible." + link
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
