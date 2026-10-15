package httpapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func fixture(t *testing.T) *Server {
	t.Helper()
	db, e := store.Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return New(config.Config{Env: "development", PublicOrigin: "http://localhost:3000", DiscordClientID: "12345", DiscordClientSecret: "not-real", DiscordGuildID: "23456", PlexURL: "http://plex.test", PlexServerID: "server-a", SeerrURL: "http://seerr.test", SeerrAPIKey: "secret"}, db)
}
func call(s *Server, method, path, cookie, origin, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: s.cookieName(), Value: cookie})
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func seed(t *testing.T, s *Server, id string, plex bool) string {
	t.Helper()
	if _, e := s.Store.Upsert(context.Background(), id, "User "+id); e != nil {
		t.Fatal(e)
	}
	value := token()
	s.sessions[digest(value)] = session{id, time.Now().Add(time.Hour)}
	if plex {
		if e := s.Store.LinkPlex(context.Background(), id, id); e != nil {
			t.Fatal(e)
		}
		s.grants[id] = upstream.Grant{PlexID: id, AccountToken: "account-" + id, ServerToken: "resource-" + id, ClientID: "client", VerifiedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	}
	return value
}
func TestAuthBoundaryAndPreferences(t *testing.T) {
	s := fixture(t)
	for _, path := range []string{"/api/v1/requests", "/api/v1/recent", "/api/v1/preferences"} {
		if w := call(s, "GET", path, "", "", ""); w.Code != 401 {
			t.Fatalf("anonymous access %s %d", path, w.Code)
		}
	}
	a := seed(t, s, "11111", false)
	b := seed(t, s, "22222", false)
	if w := call(s, "GET", "/api/v1/recent", a, "", ""); w.Code != 403 {
		t.Fatal("Discord alone granted Plex access")
	}
	if w := call(s, "PUT", "/api/v1/preferences", a, "https://attacker.example", `{"notifications":true}`); w.Code != 403 {
		t.Fatal("CSRF accepted")
	}
	if w := call(s, "PUT", "/api/v1/preferences", a, s.Config.PublicOrigin, `{"notifications":true,"discordId":"22222"}`); w.Code != 400 {
		t.Fatal("identity injection accepted")
	}
	if w := call(s, "PUT", "/api/v1/preferences", a, s.Config.PublicOrigin, `{"notifications":true}`); w.Code != 200 {
		t.Fatalf("preferences update %s", w.Body.String())
	}
	w := call(s, "GET", "/api/v1/preferences", b, "", "")
	if !strings.Contains(w.Body.String(), `"notifications":false`) {
		t.Fatal("preferences leaked across users")
	}
	if w = call(s, "POST", "/api/v1/auth/logout", a, s.Config.PublicOrigin, ""); w.Code != 200 {
		t.Fatal("logout failed")
	}
	if w = call(s, "GET", "/api/v1/preferences", a, "", ""); w.Code != 401 {
		t.Fatal("logout did not invalidate session")
	}
	if w = call(s, "DELETE", "/api/v1/account", b, s.Config.PublicOrigin, ""); w.Code != 200 {
		t.Fatal("delete failed")
	}
	if _, e := s.Store.User(context.Background(), "22222"); e == nil {
		t.Fatal("personal data not deleted")
	}
}
func TestOAuthStateBoundAndOneTime(t *testing.T) {
	s := fixture(t)
	calls := 0
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		switch r.URL.Path {
		case "/api/oauth2/token":
			if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Error("OAuth is not form encoded")
			}
			return response(200, `{"access_token":"private-discord-token"}`), nil
		case "/api/v10/users/@me":
			return response(200, `{"id":"11111","username":"alice"}`), nil
		case "/api/v10/users/@me/guilds/23456/member":
			return response(200, `{"user":{"id":"11111"},"pending":false}`), nil
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
			return nil, nil
		}
	})
	start := call(s, "GET", "/api/v1/auth/discord/start", "", "", "")
	loc, _ := url.Parse(start.Header().Get("Location"))
	state := loc.Query().Get("state")
	if len(state) != 64 || !strings.Contains(loc.Query().Get("scope"), "guilds.members.read") {
		t.Fatal("unsafe OAuth redirect")
	}
	endpoint := "/api/v1/auth/discord/callback?code=test&state=" + state
	if w := call(s, "GET", endpoint, "", "", ""); w.Code != 403 || calls != 0 {
		t.Fatal("state accepted without browser cookie")
	}
	req := httptest.NewRequest("GET", endpoint, nil)
	req.AddCookie(start.Result().Cookies()[0])
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 303 || calls != 3 || strings.Contains(w.Body.String(), "private-discord-token") {
		t.Fatalf("OAuth failed %d %s", w.Code, w.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == s.cookieName() {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie")
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 403 || calls != 3 {
		t.Fatal("OAuth callback replay accepted")
	}
}
func TestPlexPinBoundConsumedAndNoTokenLeak(t *testing.T) {
	s := fixture(t)
	a := seed(t, s, "11111", false)
	b := seed(t, s, "22222", false)
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v2/pins":
			return response(200, `{"id":1,"code":"long-pin-code","expiresIn":300}`), nil
		case "/api/v2/pins/1":
			return response(200, `{"id":1,"code":"long-pin-code","authToken":"account-secret"}`), nil
		case "/api/v2/user":
			return response(200, `{"id":77,"restricted":false}`), nil
		case "/api/v2/resources":
			return response(200, `[{"clientIdentifier":"server-a","provides":"server","accessToken":"resource-secret"}]`), nil
		case "/api/v1/user":
			return response(200, `{"pageInfo":{"results":1},"results":[{"id":42,"plexId":77}]}`), nil
		default:
			t.Fatalf("unexpected URL %s", r.URL.Path)
			return nil, nil
		}
	})
	if w := call(s, "POST", "/api/v1/auth/plex/start", a, s.Config.PublicOrigin, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call(s, "POST", "/api/v1/auth/plex/poll", b, s.Config.PublicOrigin, ""); w.Code != 410 {
		t.Fatal("PIN crossed browser boundary")
	}
	if w := call(s, "POST", "/api/v1/auth/plex/poll", a, s.Config.PublicOrigin, ""); w.Code != 200 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("PIN failed or token leaked")
	}
	if w := call(s, "POST", "/api/v1/auth/plex/poll", a, s.Config.PublicOrigin, ""); w.Code != 410 {
		t.Fatal("PIN replay accepted")
	}
	w := call(s, "GET", "/api/v1/session", a, "", "")
	if strings.Contains(w.Body.String(), "secret") || !strings.Contains(w.Body.String(), `"plexConnected":true`) {
		t.Fatal("unsafe session DTO")
	}
}
func TestPlexRevocationAndOutageFailClosed(t *testing.T) {
	for _, status := range []int{403, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			s := fixture(t)
			a := seed(t, s, "11111", true)
			g := s.grants["11111"]
			g.VerifiedAt = time.Now().Add(-6 * time.Minute)
			s.grants["11111"] = g
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "plex.tv" {
					t.Fatal("media fetched after failed authorization")
				}
				return response(status, `{}`), nil
			})
			w := call(s, "GET", "/api/v1/recent", a, "", "")
			if w.Code != 403 && w.Code != 503 {
				t.Fatalf("not fail closed: %d", w.Code)
			}
			if status == 403 {
				if _, ok := s.grants["11111"]; ok {
					t.Fatal("revoked token retained")
				}
			}
		})
	}
}
func TestBotCannotInventIdentity(t *testing.T) {
	s := fixture(t)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	s.Config.DiscordPublicKey = hex.EncodeToString(public)
	s.Config.BotAPISecret = strings.Repeat("s", 32)
	body := `{"id":"99999","application_id":"12345","guild_id":"23456","type":2,"member":{"user":{"id":"11111"}},"data":{"name":"aide"}}`
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	request := func(payload string, signed bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/internal/discord", strings.NewReader(payload))
		r.Header.Set("Authorization", "Bearer "+s.Config.BotAPISecret)
		if signed {
			r.Header.Set("X-Signature-Timestamp", stamp)
			r.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(private, []byte(stamp+body))))
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := request(body, false); w.Code != 403 {
		t.Fatal("service secret alone permitted impersonation")
	}
	if w := request(strings.Replace(body, "11111", "22222", 1), true); w.Code != 403 {
		t.Fatal("tampered user accepted")
	}
	w := request(body, true)
	if w.Code != 200 {
		t.Fatalf("valid interaction refused %s", w.Body.String())
	}
	var data map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &data)
	if data["flags"] != float64(64) {
		t.Fatal("private result not ephemeral")
	}
	if w = request(body, true); w.Code != 409 {
		t.Fatal("interaction replay accepted")
	}
}

func TestRequestsAutomaticallyResolvePlexIdentity(t *testing.T) {
	s := fixture(t)
	cookie := seed(t, s, "11111", true)
	// An old, incorrect manual mapping must never grant another user's requests.
	if err := s.Store.LinkSeerr(context.Background(), "11111", 99); err != nil {
		t.Fatal(err)
	}
	requested := false
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v1/user":
			return response(200, `{"pageInfo":{"results":2},"results":[{"id":42,"plexId":11111},{"id":99,"plexId":22222}]}`), nil
		case "/api/v1/user/42/requests":
			requested = true
			return response(200, `{"pageInfo":{"results":0},"results":[]}`), nil
		default:
			t.Errorf("wrong account accessed: %s", r.URL.Path)
			return response(500, `{}`), nil
		}
	})
	w := call(s, "GET", "/api/v1/requests?userId=99", cookie, "", "")
	if w.Code != 200 || !requested {
		t.Fatalf("automatic mapping failed: %d %s", w.Code, w.Body)
	}
	u, err := s.Store.User(context.Background(), "11111")
	if err != nil || u.SeerrID != 42 {
		t.Fatal("verified mapping not saved")
	}
	w = call(s, "GET", "/api/v1/session", cookie, "", "")
	if !strings.Contains(w.Body.String(), `"seerrId":42`) {
		t.Fatal("automatic association not reflected in session")
	}
}
func TestSeerrLookupFailuresNeverUseStoredMapping(t *testing.T) {
	for _, tc := range []struct {
		name, body   string
		status, want int
	}{
		{"not registered", `{"pageInfo":{"results":0},"results":[]}`, 200, 409},
		{"no permission", `{}`, 403, 502},
		{"outage", `{}`, 503, 502},
		{"ambiguous", `{"pageInfo":{"results":2},"results":[{"id":42,"plexId":11111},{"id":43,"plexId":11111}]}`, 200, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t)
			cookie := seed(t, s, "11111", true)
			if err := s.Store.LinkSeerr(context.Background(), "11111", 99); err != nil {
				t.Fatal(err)
			}
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/api/v1/user" {
					t.Error("requests fetched without a verified Seerr identity")
				}
				return response(tc.status, tc.body), nil
			})
			w := call(s, "GET", "/api/v1/requests", cookie, "", "")
			if w.Code != tc.want {
				t.Fatalf("got %d %s", w.Code, w.Body)
			}
			w = call(s, "GET", "/api/v1/session", cookie, "", "")
			if strings.Contains(w.Body.String(), `"seerrId"`) {
				t.Fatal("unverified stored mapping advertised")
			}
		})
	}
}

func TestDiscordRequestsUseAutomaticSeerrAssociation(t *testing.T) {
	s := fixture(t)
	seed(t, s, "11111", true)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	s.Config.DiscordPublicKey = hex.EncodeToString(public)
	s.Config.BotAPISecret = strings.Repeat("s", 32)
	calls := 0
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		switch r.URL.Path {
		case "/api/v1/user":
			return response(200, `{"pageInfo":{"results":1},"results":[{"id":42,"plexId":11111}]}`), nil
		case "/api/v1/user/42/requests":
			return response(200, `{"pageInfo":{"results":0},"results":[]}`), nil
		default:
			t.Errorf("wrong account accessed: %s", r.URL.Path)
			return response(500, `{}`), nil
		}
	})
	body := `{"id":"99999","application_id":"12345","guild_id":"23456","type":2,"member":{"user":{"id":"11111"}},"data":{"name":"mes-demandes"}}`
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	r := httptest.NewRequest("POST", "/internal/discord", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+s.Config.BotAPISecret)
	r.Header.Set("X-Signature-Timestamp", stamp)
	r.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(private, []byte(stamp+body))))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || calls != 2 || !strings.Contains(w.Body.String(), "Aucune demande") || !strings.Contains(w.Body.String(), `"flags":64`) {
		t.Fatalf("bot mapping failed: %d %s calls=%d", w.Code, w.Body, calls)
	}
}
