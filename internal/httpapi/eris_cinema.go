package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) erisCinema(ctx context.Context, u store.User, p eris.Plan) eris.ToolReply {
	failure := eris.ToolReply{Content: "Je n’ai pas pu vérifier cette fiche cinéma dans Seerr. Réessaie dans un instant."}
	id, err := s.Upstream.SeerrUser(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, u.PlexID)
	if err != nil {
		return failure
	}
	info, err := s.Upstream.Cinema(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, id, p.Query, p.Kind, p.Year)
	if errors.Is(err, upstream.ErrMediaReference) {
		text := "Je n’identifie pas ce titre sans ambiguïté. Précise le titre, l’année et s’il s’agit d’un film ou d’une série."
		for i, choice := range info.Choices {
			if i == 3 {
				break
			}
			text += "\n• " + safeText(choice.Title) + " (" + choice.Year + ") · " + choice.Kind
		}
		return eris.ToolReply{Content: text}
	}
	if err != nil {
		return failure
	}
	text := "**" + safeText(info.Reference.Title) + "**"
	if info.Reference.Year != "" {
		text += " (" + info.Reference.Year + ")"
	}
	if info.Minutes > 0 && p.Relation == "" {
		text += fmt.Sprintf(" · %d min", info.Minutes)
		if info.Reference.Kind == "tv" {
			text += "/épisode"
		}
	}
	if p.Relation == "" || p.Relation == "overview" {
		if info.Overview == "" {
			text += "\nSynopsis non renseigné dans la fiche."
		} else {
			text += "\n" + safeText(info.Overview)
		}
	}
	if p.Relation == "" || p.Relation == "director" {
		label := "Réalisation"
		if info.Reference.Kind == "tv" {
			label = "Création"
		}
		if len(info.Creators) == 0 {
			text += "\n" + label + " : non renseignée dans la fiche."
		} else {
			text += "\n" + label + " : " + safeText(strings.Join(info.Creators, ", "))
		}
	}
	if p.Relation == "" || p.Relation == "cast" {
		var names []string
		for _, credit := range info.Cast {
			name := safeText(credit.Name)
			if credit.Character != "" && p.Relation == "cast" {
				name += " (" + safeText(credit.Character) + ")"
			}
			names = append(names, name)
		}
		if len(names) == 0 {
			text += "\nDistribution non renseignée dans la fiche."
		} else {
			text += "\nAvec : " + strings.Join(names, ", ")
		}
	}
	if p.Relation == "" || p.Relation == "trailer" {
		if info.TrailerURL == "" {
			text += "\nAucune bande-annonce YouTube référencée dans cette fiche."
		} else {
			text += "\n[Bande-annonce référencée](" + info.TrailerURL + ")"
		}
	}
	text += "\nSource : [TMDB via Seerr](" + info.SourceURL + ")."
	return eris.ToolReply{Content: text}
}
