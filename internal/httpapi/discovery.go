package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) discoveryMessage(ctx context.Context, g upstream.Grant, command, kind string, now time.Time) (string, string) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	var err error
	if command == "surprise" {
		var item upstream.Media
		item, err = s.Upstream.Surprise(ctx, s.Config.PlexURL, s.Config.PlexServerID, g, kind)
		if err == nil {
			if item.ID == "" {
				return "Aucun titre disponible dans votre catalogue Plex pour ce filtre.", "Aucun titre disponible pour ce filtre."
			}
			return "**Votre surprise Plex**\n" + discoveryLine(item, true), "**Surprise Plex**\n" + discoveryLine(item, false)
		}
	} else if command == "recap" {
		var recap upstream.RecapResult
		recap, err = s.Upstream.Recap(ctx, s.Config.PlexURL, s.Config.PlexServerID, g, now)
		if err == nil {
			return recapMessage(recap, true), recapMessage(recap, false)
		}
	} else {
		return "Commande inconnue. Utilisez /aide.", ""
	}
	if errors.Is(err, upstream.ErrDenied) {
		return "Votre accès Plex a changé. Reconnectez-vous au Hub avec Plex.", ""
	}
	if errors.Is(err, upstream.ErrIncomplete) {
		return "Plex n’a pas fourni un catalogue complet dans la limite de lecture. Réessayez plus tard ou utilisez /nouveautes.", ""
	}
	return "Plex est momentanément indisponible. Réessayez plus tard.", ""
}

func discoveryLine(item upstream.Media, links bool) string {
	title := []rune(safeText(item.Title))
	if len(title) > 60 {
		title = append(title[:60], '…')
	}
	kind := "Film"
	if item.Type == "show" {
		kind = "Série"
	}
	line := fmt.Sprintf("• [%s](%s) · %s", string(title), item.URL, kind)
	if !links || !strings.HasPrefix(item.URL, "https://app.plex.tv/desktop/#!/server/") || len(line) > 400 {
		line = fmt.Sprintf("• %s · %s", string(title), kind)
	}
	if item.Subtitle != "" {
		line += " · " + safeText(item.Subtitle)
	}
	return line + "\n"
}

func recapMessage(recap upstream.RecapResult, links bool) string {
	text := fmt.Sprintf("**Récapitulatif Plex**\nDu %s au %s inclus · Europe/Paris\n", recap.Start.Format("02/01/2006"), recap.End.AddDate(0, 0, -1).Format("02/01/2006"))
	if len(recap.Items) == 0 {
		return text + "Aucun ajout disponible dans ce catalogue pendant cette période."
	}
	for index, item := range recap.Items {
		line := discoveryLine(item, links)
		if len(text)+len(line) > 1750 {
			return text + fmt.Sprintf("\n%d autre(s) titre(s) sur cette période. Consultez Plex pour la suite.", len(recap.Items)-index)
		}
		text += line
	}
	return text
}
