package httpapi

import (
	"context"
	"errors"
	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) erisCinema(ctx context.Context, u store.User, p eris.Plan) eris.ToolReply {
	id, err := s.Upstream.SeerrUser(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, u.PlexID)
	if err != nil {
		return erisFailure("cinema_unavailable")
	}
	info, err := s.Upstream.Cinema(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, id, p.Query, p.Kind, p.Year)
	if errors.Is(err, upstream.ErrMediaReference) {
		return erisData(map[string]any{"status": "ambiguous", "candidates": info.Choices})
	}
	if err != nil {
		return erisFailure("cinema_unavailable")
	}
	return erisData(map[string]any{"reference": info.Reference, "overview": info.Overview, "creators": info.Creators, "cast": info.Cast, "minutes": info.Minutes, "trailerURL": info.TrailerURL, "sourceURL": info.SourceURL})
}
