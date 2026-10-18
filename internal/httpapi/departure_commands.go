package httpapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hloiseau/le-concierge/internal/departures"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) departuresMessage(ctx context.Context, u store.User, g upstream.Grant) string {
	if !s.departuresEnabled() {
		return "Les sources des départs ne sont pas encore configurées."
	}
	if s.rate("departures:"+u.ID, 6) {
		return "Patientez avant d’actualiser les départs."
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p, err := s.Store.GetReminderPreferences(ctx, u.ID)
	if err != nil {
		return "Lecture de vos préférences impossible. Réessayez plus tard."
	}
	page, err := s.personalDepartures(ctx, u, g, p, false)
	if err != nil {
		return "Les départs ne peuvent pas être vérifiés actuellement. Réessayez plus tard."
	}
	return departureMessage(page, s.Config.PublicOrigin, time.Now())
}

func departureMessage(page departures.Page, origin string, now time.Time) string {
	if !now.Before(page.ExpiresAt) {
		return "Les départs ne sont plus à jour. Relancez /a-voir pour les vérifier."
	}
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return "Les dates des départs ne peuvent pas être vérifiées actuellement."
	}
	items := make([]departures.Item, 0, len(page.Items))
	for _, item := range page.Items {
		if item.Watch != "completed" {
			items = append(items, item)
		}
	}
	text := "**À voir avant le départ**\nVos contenus · les 30 prochains jours · Europe/Paris\n"
	footer := "\nListe complète et préférences : " + strings.TrimRight(origin, "/") + "/avant-le-depart"
	if len(footer) > 350 {
		footer = "\nListe complète et préférences dans Le Hub, page Avant le départ."
	}
	footer += "\nSources vérifiées le " + page.UpdatedAt.In(paris).Format("02/01/2006 à 15:04") + ". Échéances estimées, susceptibles de changer."
	if len(items) == 0 {
		return text + "Aucun départ confirmé parmi vos contenus non terminés." + footer
	}
	shown := 0
	for _, item := range items {
		kind := map[string]string{"movie": "Film", "episode": "Épisode", "season": "Saison", "show": "Série"}[item.Kind]
		if kind == "" {
			kind = "Contenu"
		}
		watch := map[string]string{"unstarted": "Marqué non vu dans Plex", "in_progress": "Lecture commencée"}[item.Watch]
		if watch == "" || item.Kind == "season" || item.Kind == "show" {
			watch = "Visionnage incertain"
		}
		title := []rune(safeText(item.Title))
		if len(title) > 60 {
			title = append(title[:60], '…')
		}
		line := fmt.Sprintf("• %s · %s — %s\nDépart estimé le %s", kind, watch, string(title), item.EstimatedAt.In(paris).Format("02/01/2006"))
		if strings.HasPrefix(item.URL, "https://app.plex.tv/desktop/#!/server/") && len(item.URL) < 300 {
			line += " · [Plex](" + item.URL + ")"
		}
		line += "\n"
		// Reserve room for the complete footer and the explicit remainder count.
		if shown == 8 || len(text)+len(line)+len(footer)+90 > 2000 {
			break
		}
		text += line
		shown++
	}
	if shown < len(items) {
		text += fmt.Sprintf("\n%d autre(s) contenu(s) à consulter dans le Hub.\n", len(items)-shown)
	}
	return text + footer
}
