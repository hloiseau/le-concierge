package httpapi

import (
	"context"
	"time"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/websearch"
)

func (s *Server) erisWeb(ctx context.Context, p eris.Plan) eris.ToolReply {
	if s.Config.ErisWebURL == "" || s.Web == nil {
		return erisFailure("web_not_configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var result websearch.Result
	var err error
	if p.URL != "" {
		result, err = s.Web.Read(ctx, p.URL)
	} else {
		result, err = s.Web.Search(ctx, p.Query, p.Kind == "youtube")
	}
	if err != nil || !result.Validate() {
		return erisFailure("web_evidence_unavailable")
	}
	reply := erisData(map[string]any{"status": "evidence_found", "retrievedAt": result.RetrievedAt})
	reply.Web = &result
	return reply
}
