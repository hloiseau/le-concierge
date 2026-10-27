package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/eris"
)

func TestOuicheurLinkIsPersonalPrivateAndRevocable(t *testing.T) {
	s := fixture(t)
	cookie := seed(t, s, "11111", true)
	other := seed(t, s, "22222", true)
	s.Config.OuicheurURL = "https://gifts.test"
	secret := strings.Repeat("a", 64)
	calls := 0
	revoked := false
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		switch r.URL.Path {
		case "/api/login":
			resp := response(200, `{"ok":true,"role":"member"}`)
			resp.Header = http.Header{"Set-Cookie": {"wishlister_session=" + secret + "; Path=/; HttpOnly"}}
			return resp, nil
		case "/api/account/sessions":
			if revoked {
				return response(401, `{}`), nil
			}
			return response(200, `[{"id":"`+strings.Repeat("b", 32)+`","current":true,"expires_at":"`+time.Now().Add(time.Hour).UTC().Format(time.RFC3339)+`"}]`), nil
		case "/":
			return response(200, `<label>Liste<select><option value="11111111-1111-1111-1111-111111111111">Alice</option></select></label>`), nil
		case "/api/wishes":
			return response(200, `{"items":[{"list_id":"11111111-1111-1111-1111-111111111111","title":"Un jeu","reserved":1,"donor":"SECRET"}],"next":null}`), nil
		}
		t.Fatal(r.URL.Path)
		return nil, nil
	})
	w := call(s, "POST", "/api/v1/ouicheur/link", cookie, s.Config.PublicOrigin, `{"login":"alice","password":"personal-password"}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), secret) {
		t.Fatal(w.Code, w.Body)
	}
	if w = call(s, "GET", "/api/v1/ouicheur/link", other, "", ""); w.Code != 200 || strings.Contains(w.Body.String(), `"linked":true`) {
		t.Fatal("other account inherited session")
	}
	u, err := s.Store.User(context.Background(), "11111")
	if err != nil {
		t.Fatal(err)
	}
	before := calls
	r := s.erisGifts(context.Background(), u, eris.Plan{Action: "gifts", Query: "Alice"}, false)
	if calls != before || !strings.Contains(r.Content, "/eris") {
		t.Fatal("public read", r)
	}
	r = s.erisGifts(context.Background(), u, eris.Plan{Action: "gifts", Query: "Alice"}, true)
	if r.Web == nil || !strings.Contains(r.Web.Sources[0].Text, "Un jeu") || strings.Contains(r.Web.Sources[0].Text, "SECRET") {
		t.Fatal(r)
	}
	revoked = true
	r = s.erisGifts(context.Background(), u, eris.Plan{Action: "gifts", Query: "Alice"}, true)
	if r.Web != nil || !strings.Contains(r.Content, "expiré") {
		t.Fatal("revocation ignored", r)
	}
	if _, ok := s.currentOuicheurGrant(u.ID); ok {
		t.Fatal("revoked session retained")
	}
}

func TestOuicheurUnlinkDuringLoginWins(t *testing.T) {
	s := fixture(t)
	cookie := seed(t, s, "11111", true)
	s.Config.OuicheurURL = "https://gifts.test"
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/login" {
			resp := response(200, `{"ok":true,"role":"member"}`)
			resp.Header = http.Header{"Set-Cookie": {"wishlister_session=" + strings.Repeat("a", 64)}}
			return resp, nil
		}
		call(s, "DELETE", "/api/v1/ouicheur/link", cookie, s.Config.PublicOrigin, "")
		return response(200, `[{"id":"`+strings.Repeat("b", 32)+`","current":true,"expires_at":"`+time.Now().Add(time.Hour).UTC().Format(time.RFC3339)+`"}]`), nil
	})
	w := call(s, "POST", "/api/v1/ouicheur/link", cookie, s.Config.PublicOrigin, `{"login":"alice","password":"personal-password"}`)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	if _, ok := s.currentOuicheurGrant("11111"); ok {
		t.Fatal("unlink resurrected session")
	}
}
