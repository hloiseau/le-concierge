package httpapi

import (
	"context"
	"time"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/websearch"
)

func (s *Server) erisWeb(ctx context.Context, p eris.Plan) eris.ToolReply {
	if s.Config.ErisWebURL == "" || s.Web == nil {
		return eris.ToolReply{Content: "La recherche web n’est pas encore connectée."}
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
		return eris.ToolReply{Content: "Je n’ai pas de source exploitable pour vérifier ça : le lien ou la recherche n’a pas abouti. Je préfère te le dire plutôt qu’inventer."}
	}
	return eris.ToolReply{Content: result.Fallback(), Web: &result}
}
