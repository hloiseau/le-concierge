package httpapi

import (
	"context"
	"fmt"
	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
	"math"
)

func (s *Server) erisDiagnostic(ctx context.Context, u store.User, grant upstream.Grant, p eris.Plan) eris.ToolReply {
	member, err := s.Upstream.SeerrUser(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, u.PlexID)
	if err != nil {
		return erisFailure("diagnostic_unavailable")
	}
	requestID, title := p.RequestID, fmt.Sprintf("#%d", p.RequestID)
	if requestID == 0 {
		items, err := s.Upstream.FindPersonalRequests(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, member, p.Query)
		if err != nil {
			return erisFailure("diagnostic_unavailable")
		}
		if len(items) != 1 {
			candidates := []map[string]any{}
			for i, item := range items {
				if i == 5 {
					break
				}
				candidates = append(candidates, map[string]any{"requestId": item.ID, "title": item.Title, "status": item.Status})
			}
			return erisData(map[string]any{"status": "identify_request", "candidates": candidates})
		}
		requestID, title = items[0].ID, items[0].Title
		if items[0].Status == "pending" {
			return erisDiagnosticResult(&eris.DiagnosticFacts{Title: title, Stage: "approval"})
		}
		if items[0].Status == "declined" {
			return erisDiagnosticResult(&eris.DiagnosticFacts{Title: title, Stage: "declined"})
		}
	}
	result, err := s.Upstream.DiagnosePersonalRequest(ctx, s.diagnosticSources(), grant, requestID, member)
	if err != nil {
		return erisFailure("diagnostic_unavailable")
	}
	facts := &eris.DiagnosticFacts{Title: title, Stage: result.Stage, MissingEpisodes: result.MissingEpisodes, Partial: result.QueueTruncated || result.HistoryTruncated}
	if result.ProgressPercent != nil {
		rounded := math.RoundToEven(*result.ProgressPercent)
		facts.ProgressPercent = &rounded
	}
	if result.PlexStatus == "indexed" {
		facts.PlexIndex = "indexed"
	}
	return erisDiagnosticResult(facts)
}

func erisDiagnosticResult(facts *eris.DiagnosticFacts) eris.ToolReply {
	if !facts.Valid() {
		return erisFailure("diagnostic_invalid")
	}
	reply := erisData(facts)
	reply.Diagnostic = facts
	return reply
}
