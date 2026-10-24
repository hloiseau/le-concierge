package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/eris"
)

func TestErisDirectRequestUsesVerifiedTextAndCatalogue(t *testing.T) {
	for _, state := range []string{"exact", "year", "negative", "conditional", "question", "wrong title", "wrong year", "homonyms", "series", "wrong season", "missing season", "revoked", "uncertain"} {
		t.Run(state, func(t *testing.T) {
			s := fixture(t)
			s.Config.BotAPISecret = strings.Repeat("s", 32)
			seed(t, s, "11111", true)
			question := "Tu peux demander le film Arrival dans Seerr stp ?"
			plan := eris.Plan{Action: "prepare", Query: "Arrival", Kind: "movie"}
			catalogue := `{"results":[{"id":123,"mediaType":"movie","title":"Arrival","releaseDate":"2016-01-01"}]}`
			switch state {
			case "year":
				question = "Ajoute Arrival (2016)"
			case "negative":
				question = "Ne demande pas Arrival"
			case "conditional":
				question = "Demande Arrival mais pas maintenant"
			case "question":
				question = "Tu as Arrival ?"
			case "wrong title":
				question = "Demande Bronson"
			case "wrong year":
				question = "Demande Arrival (1996)"
			case "homonyms":
				catalogue = `{"results":[{"id":123,"mediaType":"movie","title":"Arrival","releaseDate":"2016-01-01"},{"id":456,"mediaType":"movie","title":"Arrival","releaseDate":"1996-01-01"}]}`
			case "series", "wrong season", "missing season":
				question = "Demande la saison 2 de Severance"
				plan = eris.Plan{Action: "prepare", Query: "Severance", Kind: "show", Seasons: []int{2}}
				catalogue = `{"results":[{"id":123,"mediaType":"tv","name":"Severance","firstAirDate":"2022-01-01"}]}`
				if state == "wrong season" {
					plan.Seasons = []int{1}
				}
				if state == "missing season" {
					question = "Demande Severance"
					plan.Seasons = nil
				}
			}
			posts := 0
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/api/v1/user":
					return response(200, `{"pageInfo":{"results":1},"results":[{"id":2,"plexId":11111}]}`), nil
				case "/api/v1/auth/me":
					return response(200, `{"id":2}`), nil
				case "/api/v1/search":
					if state == "revoked" {
						_ = s.Store.Delete(context.Background(), "11111")
					}
					return response(200, catalogue), nil
				case "/api/v1/request":
					posts++
					if r.Method != "POST" || r.Header.Get("X-Api-User") != "2" {
						t.Error("wrong write identity")
					}
					var body struct {
						MediaID int    `json:"mediaId"`
						Kind    string `json:"mediaType"`
						Seasons []int  `json:"seasons"`
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil || body.MediaID != 123 {
						t.Error("wrong title")
					}
					if state == "series" && (len(body.Seasons) != 1 || body.Seasons[0] != 2) {
						t.Error("wrong seasons")
					}
					if state == "uncertain" {
						return response(500, `{}`), nil
					}
					return response(201, fmt.Sprintf(`{"id":9,"type":%q,"requestedBy":{"id":2},"media":{"tmdbId":123}}`, body.Kind)), nil
				default:
					t.Error("unexpected route", r.URL.Path)
					return response(500, `{}`), nil
				}
			})
			body := strings.Replace(erisSlash, `"value":"Arrival"`, `"value":`+fmt.Sprintf("%q", question), 1)
			in := signedErisTool(t, s, plan, body)
			w := callErisTool(s, in)
			want := state == "exact" || state == "year" || state == "series" || state == "uncertain"
			if w.Code != 200 || posts != map[bool]int{true: 1, false: 0}[want] {
				t.Fatalf("posts=%d status=%d %s", posts, w.Code, w.Body)
			}
			if want && state != "uncertain" && (!strings.Contains(w.Body.String(), "créée") || strings.Contains(w.Body.String(), "eris:choose:")) {
				t.Fatal("redundant confirmation", w.Body)
			}
			if state == "uncertain" && (!strings.Contains(w.Body.String(), "Vérifie /mes-demandes") || strings.Contains(w.Body.String(), "créée")) {
				t.Fatal("ambiguous write retried or called successful", w.Body)
			}
			before := posts
			if again := callErisTool(s, in); again.Code != 403 || posts != before {
				t.Fatal("proof replayed")
			}
		})
	}
}
