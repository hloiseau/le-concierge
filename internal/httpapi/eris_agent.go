package httpapi

import (
	"context"
	"encoding/json"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func erisData(value any) eris.ToolReply {
	raw, _ := json.Marshal(value)
	return eris.ToolReply{Content: string(raw)}
}

func (s *Server) erisAgentMedia(ctx context.Context, actor, owner store.User, p eris.Plan) eris.ToolReply {
	member, err := s.resolveSeerr(ctx, owner)
	if err != nil {
		return erisData(map[string]string{"error": "seerr_access_unavailable"})
	}
	if p.Action == "lookup" || p.Action == "collection" {
		var candidates []upstream.RequestCandidate
		if p.Action == "lookup" {
			candidates, err = s.Upstream.LookupMedia(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, member, p.Query, p.Kind, p.Year)
		} else {
			candidates, err = s.Upstream.MovieCollection(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, member, p.MediaID)
		}
		if err != nil {
			return erisData(map[string]string{"error": "catalogue_unavailable"})
		}
		return erisData(struct {
			Candidates []upstream.RequestCandidate `json:"candidates"`
		}{candidates})
	}
	candidate, err := s.Upstream.MediaByID(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, member, p.MediaID, p.Kind)
	if err != nil {
		return erisData(map[string]string{"error": "media_identity_unverified", "outcome": "not_sent"})
	}
	if err = s.revalidateErisRequestOwner(ctx, actor, owner); err != nil {
		return erisData(map[string]string{"error": "access_expired", "outcome": "not_sent"})
	}
	request := erisMediaRequest{UserID: owner.ID, SeerrID: member, Candidate: candidate, Seasons: p.Seasons}
	// Identity and grant were verified by erisTool; submit retains the durable
	// idempotency receipt and member permissions, with no human-choice state.
	return s.submitErisRequest(ctx, request)
}

func erisFailure(code string) eris.ToolReply { return erisData(map[string]any{"error": code}) }
