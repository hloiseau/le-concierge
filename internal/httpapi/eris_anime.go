package httpapi

import (
	"context"
	"encoding/json"
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
			return erisFailure("web_not_configured")
		}
		query := p.Query + " sortie édition française manga"
		if utf8.RuneCountInString(query) > 200 {
			return erisFailure("edition_query_too_long")
		}
		r, err := s.Web.SearchPublishers(ctx, query, frenchMangaPublishers)
		if err != nil {
			return erisFailure("publisher_edition_evidence_unavailable")
		}
		reply := erisData(map[string]any{"status": "evidence_found", "scope": "publisher_pages"})
		reply.Web = &r
		return reply
	}
	result, err := s.Upstream.Anime(ctx, p.Query, p.Kind, p.Genre, p.Year, p.Relation == "recommend")
	if err != nil {
		return erisFailure("anilist_unavailable")
	}
	if result.Ambiguous {
		candidates := []map[string]any{}
		for i, m := range result.Items {
			if i == 5 {
				break
			}
			candidates = append(candidates, map[string]any{"title": m.DisplayTitle(), "kind": m.Type, "format": m.Format, "startDate": m.StartDate})
		}
		return erisData(map[string]any{"status": "ambiguous", "candidates": candidates})
	}
	if len(result.Items) == 0 {
		return erisFailure("anilist_no_results")
	}
	evidence := websearch.Result{RetrievedAt: time.Now().UTC().Format(time.RFC3339)}
	for i, m := range result.Items {
		if i == websearch.MaxSources {
			break
		}
		description := []rune(html.UnescapeString(m.Description))
		if len(description) > 400 {
			description = description[:400]
		}
		data := map[string]any{"title": m.DisplayTitle(), "kind": m.Type, "format": m.Format, "originStatus": m.Status, "genres": m.Genres, "episodes": m.Episodes, "volumes": m.Volumes, "originStartDate": m.StartDate, "frenchEditionVerified": false, "nasAvailabilityVerified": false, "description": string(description)}
		if result.Reference != nil {
			data["recommendationReference"] = result.Reference.DisplayTitle()
		}
		raw, _ := json.Marshal(data)
		text := string(raw)

		evidence.Sources = append(evidence.Sources, websearch.Source{Title: m.DisplayTitle(), URL: fmt.Sprintf("https://anilist.co/%s/%d", strings.ToLower(m.Type), m.ID), Text: text, Kind: "metadata"})
	}
	if !evidence.Validate() {
		return erisFailure("anilist_evidence_invalid")
	}
	reply := erisData(map[string]any{"status": "evidence_found", "scope": "origin_metadata"})
	reply.Web = &evidence
	return reply
}
