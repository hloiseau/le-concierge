package departures

import (
	"testing"

	"github.com/hloiseau/le-concierge/internal/upstream"
)

func TestRequestedRemovalKeepsExactPlexQualityWhileFollowingRemainsTitleBased(t *testing.T) {
	season := 2
	for _, kind := range []string{"movie", "episode"} {
		for _, mode := range []string{"matching request", "other quality", "not indexed", "explicit follow"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				m := upstream.PlexItem{ID: "84", Kind: kind, TMDBID: 22, LibraryID: "1", ParentID: "80", GrandparentID: "79"}
				target := upstream.InterestTarget{Type: "movie", TMDBID: 22, Requested: true, PlexKeys: []string{"84"}}
				if kind == "episode" {
					target.Type = "tv"
					target.Season = &season
					target.PlexKeys = []string{"79"}
				}
				switch mode {
				case "other quality":
					target.PlexKeys = []string{"999"}
				case "not indexed":
					target.PlexKeys = nil
				case "explicit follow":
					target.Requested = false
					target.Following = true
					target.PlexKeys = nil
				}
				get := func(id string) (upstream.PlexItem, error) {
					if id == "80" {
						return upstream.PlexItem{ID: "80", Kind: "season", LibraryID: "1", ParentID: "79", Index: &season}, nil
					}
					if id == "79" {
						return upstream.PlexItem{ID: "79", Kind: "show", LibraryID: "1", TMDBID: 22}, nil
					}
					t.Fatalf("unexpected metadata id %s", id)
					return upstream.PlexItem{}, nil
				}
				requested, following, err := matchInterest(m, []upstream.InterestTarget{target}, get)
				if err != nil || requested != (mode == "matching request") || following != (mode == "explicit follow") {
					t.Fatalf("requested=%v following=%v error=%v", requested, following, err)
				}
			})
		}
	}
}
