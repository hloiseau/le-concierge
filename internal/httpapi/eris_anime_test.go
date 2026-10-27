package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/eris"
)

func TestAnimePublicMetadataNeverClaimsFrenchEditionOrNASAvailability(t *testing.T) {
	s := fixture(t)
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "graphql.anilist.co" || r.Header.Get("Authorization") != "" || r.Header.Get("X-Api-Key") != "" {
			t.Fatal("anime read private services or credentials")
		}
		return response(200, `{"data":{"Page":{"media":[{"id":30013,"type":"MANGA","title":{"english":"One Piece"},"format":"MANGA","status":"RELEASING","description":"A pirate story.","startDate":{"year":1997,"month":7,"day":22}}]}}}`), nil
	})
	r := s.erisAnime(context.Background(), eris.Plan{Action: "anime", Kind: "manga", Query: "One Piece"})
	if r.Web == nil || !r.Web.Validate() || r.Web.Sources[0].Kind != "metadata" || !strings.Contains(r.Web.Sources[0].Text, "Aucune sortie française confirmée") {
		t.Fatal(r)
	}
	r = s.erisAnime(context.Background(), eris.Plan{Action: "anime", Kind: "manga", Query: "One Piece tome 114", Relation: "french"})
	if r.Web != nil || !strings.Contains(r.Content, "web") {
		t.Fatal("French date invented without publisher reader", r)
	}
}
