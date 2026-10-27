package httpapi

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/websearch"
)

var frenchMangaPublishers = []string{"glenat.com", "pika.fr", "kana.fr", "editions-delcourt.fr", "kurokawa.fr", "ki-oon.com", "panini.fr"}

func (s *Server) erisAnime(ctx context.Context, p eris.Plan) eris.ToolReply {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if p.Relation == "french" {
		if s.Config.ErisWebURL == "" || s.Web == nil {
			return eris.ToolReply{Content: "La vérification des éditions françaises nécessite la recherche web, qui n’est pas encore connectée."}
		}
		query := p.Query + " sortie édition française manga"
		if utf8.RuneCountInString(query) > 200 {
			return eris.ToolReply{Content: "Précise un titre, un tome et une édition plus courts pour la vérification."}
		}
		r, err := s.Web.SearchPublishers(ctx, query, frenchMangaPublishers)
		if err != nil {
			return eris.ToolReply{Content: "Je n’ai pas trouvé de page d’éditeur exploitable pour confirmer cette édition française. Précise le tome et l’édition ; une date d’origine ne suffit pas."}
		}
		return eris.ToolReply{Content: r.Fallback(), Web: &r}
	}
	result, err := s.Upstream.Anime(ctx, p.Query, p.Kind, p.Genre, p.Year, p.Relation == "recommend")
	if err != nil {
		return eris.ToolReply{Content: "Je n’ai pas pu vérifier les données AniList pour le moment."}
	}
	if result.Ambiguous {
		text := "Je n’identifie pas l’œuvre sans ambiguïté. Précise son titre alternatif, le type anime/manga ou l’année d’origine."
		for i, m := range result.Items {
			if i == 3 {
				break
			}
			text += "\n• " + safeText(m.DisplayTitle()) + " · " + safeText(m.Format)
			if m.StartDate.Year != nil {
				text += fmt.Sprintf(" (%d)", *m.StartDate.Year)
			}
		}
		return eris.ToolReply{Content: text}
	}
	if len(result.Items) == 0 {
		return eris.ToolReply{Content: "Je n’ai pas de suggestion ou de fiche suffisamment vérifiée dans AniList pour cette demande."}
	}
	evidence := websearch.Result{RetrievedAt: time.Now().UTC().Format(time.RFC3339)}
	for i, m := range result.Items {
		if i == websearch.MaxSources {
			break
		}
		text := fmt.Sprintf("Métadonnées publiques AniList : %s. Type : %s ; format : %s ; statut d'origine : %s. Genres : %s.", m.DisplayTitle(), m.Type, m.Format, m.Status, strings.Join(m.Genres, ", "))
		if m.Episodes != nil {
			text += fmt.Sprintf(" Nombre total d'épisodes renseigné : %d (pas un nombre d'épisodes disponibles en France ou dans Plex).", *m.Episodes)
		}
		if m.Volumes != nil {
			text += fmt.Sprintf(" Nombre de volumes renseigné pour l'œuvre d'origine : %d ; ce ne sont pas des éditions françaises.", *m.Volumes)
		}
		if m.StartDate.Year != nil {
			text += fmt.Sprintf(" Début de diffusion/publication d'origine : année %d", *m.StartDate.Year)
			if m.StartDate.Month != nil {
				text += fmt.Sprintf(", mois %d", *m.StartDate.Month)
			}
			if m.StartDate.Day != nil {
				text += fmt.Sprintf(", jour %d", *m.StartDate.Day)
			}
			text += ". Aucune sortie française confirmée par cette donnée."
		}
		if result.Reference != nil {
			text += " Recommandation communautaire AniList à partir de " + result.Reference.DisplayTitle() + ", pas de l'historique personnel du membre."
		}
		description := []rune(html.UnescapeString(m.Description))
		if len(description) > 400 {
			description = description[:400]
		}
		text += " Résumé de l'œuvre : " + string(description)
		if utf8.RuneCountInString(text) > 1800 {
			text = string([]rune(text)[:1800])
		}
		evidence.Sources = append(evidence.Sources, websearch.Source{Title: m.DisplayTitle(), URL: fmt.Sprintf("https://anilist.co/%s/%d", strings.ToLower(m.Type), m.ID), Text: text, Kind: "metadata"})
	}
	if !evidence.Validate() {
		return eris.ToolReply{Content: "Les données AniList reçues ne sont pas assez complètes pour répondre."}
	}
	return eris.ToolReply{Content: evidence.Fallback(), Web: &evidence}
}
