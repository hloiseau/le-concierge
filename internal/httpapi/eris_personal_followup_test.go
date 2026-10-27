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

func TestPersonalChoicesContinueVerifiedIntentAndRejectOtherOwnerAndReplay(t *testing.T) {
	s, _ := erisFollowFixture(t)
	s.Config.BotAPISecret = strings.Repeat("s", 32)
	seed(t, s, "22222", true)
	original := s.Upstream.HTTP.Transport
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("personal follow created a media request")
		}
		if r.URL.Path == "/api/v1/search" {
			return response(200, `{"results":[{"id":12,"title":"Shared film","mediaType":"movie","releaseDate":"1984-01-01"},{"id":13,"title":"Shared film","mediaType":"movie","releaseDate":"2021-01-01"}]}`), nil
		}
		if r.URL.Path == "/api/v1/movie/13" {
			return response(200, `{"id":13,"title":"Shared film","mediaInfo":{"tmdbId":13,"mediaType":"movie","status":2,"requests":[{"status":2,"requestedBy":{"id":8}}]}}`), nil
		}
		return original.RoundTrip(r)
	})
	call := func(id, user, question, choice string, plan eris.Plan) eris.ToolReply {
		t.Helper()
		body := fmt.Sprintf(`{"id":%q,"application_id":"12345","guild_id":"23456","channel_id":"33333","type":2,"member":{"user":{"id":%q}},"data":{"name":"eris","options":[{"name":"message","type":3,"value":%q}]}}`, id, user, question)
		in := signedErisTool(t, s, plan, body)
		in.Choice = choice
		w := callErisTool(s, in)
		var r eris.ToolReply
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &r) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		return r
	}
	offer := call("99001", "11111", "Préviens-moi quand Shared film arrive", "", eris.Plan{Action: "follow", Query: "Shared film", Kind: "movie"})
	if len(offer.Buttons) != 2 || offer.Buttons[0].Operation != "follow" {
		t.Fatal(offer)
	}
	choice := offer.Buttons[1].ID
	r := call("99002", "22222", "celui de 2021", choice, eris.Plan{})
	_, total, _ := s.Store.Subscriptions(context.Background(), "11111", 0)
	if total != 0 || strings.Contains(r.Content, "enregistré") {
		t.Fatal("other owner selected follow", r, total)
	}
	r = call("99003", "11111", "celui de 2021", choice, eris.Plan{})
	items, total, _ := s.Store.Subscriptions(context.Background(), "11111", 0)
	if !strings.Contains(r.Content, "enregistré") || total != 1 || items[0].TMDBID != 13 {
		t.Fatal("year followup did not create selected follow", r, total)
	}
	r = call("99004", "11111", "2021", choice, eris.Plan{})
	if strings.Contains(r.Content, "enregistré") {
		t.Fatal("consumed choice reused", r)
	}
	offer = call("99005", "11111", "Retire un suivi", "", eris.Plan{Action: "unfollow"})
	if len(offer.Buttons) != 1 || offer.Buttons[0].Operation != "unfollow" {
		t.Fatal(offer)
	}
	r = call("99006", "11111", "retire le premier", offer.Buttons[0].ID, eris.Plan{})
	_, total, _ = s.Store.Subscriptions(context.Background(), "11111", 0)
	if total != 0 || !strings.Contains(r.Content, "retiré") {
		t.Fatal("removal followup intercepted or failed", r, total)
	}
}
