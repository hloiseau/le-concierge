package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hloiseau/le-concierge/internal/discord"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) requestMessage(ctx context.Context, u store.User, offset int, status string) (string, *discord.RequestPagination) {
	if s.Config.SeerrURL == "" {
		return "Seerr n’est pas encore configuré.", nil
	}
	seerrID, err := s.resolveSeerr(ctx, u)
	var page upstream.RequestPage
	if err == nil {
		page, err = s.Upstream.RequestsByStatus(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, seerrID, offset, status)
	}
	if errors.Is(err, upstream.ErrSeerrUserNotFound) {
		return seerrMissing, nil
	}
	if errors.Is(err, upstream.ErrIncomplete) {
		return "La liste complète de vos demandes ne peut pas être vérifiée. Réessayez plus tard ou choisissez Tous les états.", nil
	}
	if err != nil {
		return "Seerr est momentanément indisponible.", nil
	}
	return requestPageMessage(page, status, s.Config.PublicOrigin), &discord.RequestPagination{Offset: offset, Total: page.Total, Status: status}
}

func requestPageMessage(page upstream.RequestPage, status, origin string) string {
	text := "**Vos demandes**\n"
	if status != "" {
		text += "État : " + statusLabel(status) + "\n"
	}
	if page.Total > 0 {
		text += fmt.Sprintf("Page %d · %d demande(s)\n", page.Offset/10+1, page.Total)
	}
	if len(page.Items) == 0 {
		switch {
		case page.Offset > 0:
			text += "Aucune demande sur cette page. Revenez à la page précédente."
		case status != "":
			text += "Aucune demande dans cet état."
		default:
			text += "Aucune demande pour le moment."
		}
	}
	footer := "\nVos demandes et notifications : " + strings.TrimRight(origin, "/") + "/demandes"
	if len(footer) > 350 {
		footer = "\nVos demandes et notifications dans Le Hub, page Mes demandes."
	}
	for _, item := range page.Items {
		line := "• " + safeText(item.Title) + " — " + statusLabel(item.Status)
		if item.Is4k != nil {
			quality := "Standard"
			if *item.Is4k {
				quality = "4K"
			}
			line += " · " + quality
		}
		if len(item.Seasons) > 0 {
			line += fmt.Sprintf(" · saisons %v", item.Seasons)
		}
		if len(text)+len(line)+len(footer)+40 > 2000 {
			text += "Suite dans le Hub.\n"
			break
		}
		text += line + "\n"
	}
	return text + footer
}
