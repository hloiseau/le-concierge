package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/websearch"
)

func TestWebUsesVerifiedDiscordIdentityWithoutPlexGrant(t *testing.T) {
	reads := 0
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads++
		if r.URL.Path != "/search" || r.Header.Get("Authorization") != "" || strings.Contains(r.URL.RawQuery, "11111") {
			t.Error("wrong request or private identity exported")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]string{"title": "Annonce", "url": "https://unresolvable.invalid/article", "content": "Extrait daté du 27 octobre."}}})
	}))
	defer engine.Close()
	s := fixture(t)
	s.Config.BotAPISecret = strings.Repeat("s", 32)
	s.Config.ErisWebURL = engine.URL
	s.Web = websearch.New(engine.URL, "")
	s.Upstream.HTTP.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("web attempted private Plex or service read")
		return nil, nil
	})
	in := signedErisTool(t, s, eris.Plan{Action: "web", Query: "une annonce"}, erisSlash)
	w := callErisTool(s, in)
	var result eris.ToolReply
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || result.Web == nil || result.Web.Sources[0].Kind != "snippet" || reads != 1 {
		t.Fatal(w.Code, w.Body.String(), reads)
	}
	w = callErisTool(s, in)
	if w.Code != 403 || reads != 1 {
		t.Fatal("replayed tool proof accepted", w.Code, reads)
	}
	in = signedErisTool(t, s, eris.Plan{Action: "web", Query: "une annonce"}, strings.Replace(erisSlash, "99999", "99998", 1))
	in.Signature = strings.Repeat("0", 128)
	if w := callErisTool(s, in); w.Code != 403 || reads != 1 {
		t.Fatal("forged web caller accepted", w.Code, reads)
	}
}
