package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/upstream"
)

func seedPlexOnly(t *testing.T, s *Server, plexID string) (string, string) {
	t.Helper()
	u, err := s.Store.LoginPlex(context.Background(), plexID, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	value := token()
	expires := time.Now().Add(time.Hour)
	s.sessions[digest(value)] = session{u.ID, expires}
	s.grants[u.ID] = upstream.Grant{PlexID: plexID, AccountToken: "account-secret", ServerToken: "resource-secret", VerifiedAt: time.Now(), ExpiresAt: expires}
	return value, u.ID
}
func cookiesCall(s *Server, method, path, origin string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("Origin", origin)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func namedCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("missing cookie %s", name)
	return nil
}
func plexProvider(t *testing.T, s *Server, resources string, restricted bool) {
	t.Helper()
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v2/pins":
			return response(200, `{"id":1,"code":"long-pin-code","expiresIn":300}`), nil
		case "/api/v2/pins/1":
			return response(200, `{"id":1,"code":"long-pin-code","authToken":"account-secret"}`), nil
		case "/api/v2/user":
			if restricted {
				return response(200, `{"id":77,"restricted":true}`), nil
			}
			return response(200, `{"id":77,"username":"Alice","restricted":false}`), nil
		case "/api/v2/resources":
			return response(200, resources), nil
		case "/api/v1/user":
			return response(200, `{"pageInfo":{"results":1},"results":[{"id":42,"plexId":77}]}`), nil
		default:
			t.Errorf("unexpected upstream %s", r.URL.Path)
			return response(500, `{}`), nil
		}
	})
}

const authorizedResources = `[{"clientIdentifier":"server-a","provides":"server","accessToken":"resource-secret"}]`

func TestAnonymousPlexLoginIsBoundConsumedAndPrivate(t *testing.T) {
	s := fixture(t)
	plexProvider(t, s, authorizedResources, false)
	if w := call(s, "POST", "/api/v1/auth/plex/start", "", "https://attacker.example", ""); w.Code != 403 {
		t.Fatal("login CSRF accepted")
	}
	start := call(s, "POST", "/api/v1/auth/plex/start", "", s.Config.PublicOrigin, "")
	if start.Code != 200 {
		t.Fatal(start.Body)
	}
	pin := namedCookie(t, start, s.plexCookieName())
	if !pin.HttpOnly || pin.SameSite != http.SameSiteStrictMode || len(pin.Value) != 64 {
		t.Fatal("unsafe pre-auth cookie")
	}
	if len(s.sessions) != 0 {
		t.Fatal("session issued before Plex verification")
	}
	if w := call(s, "POST", "/api/v1/auth/plex/poll", "", s.Config.PublicOrigin, ""); w.Code != 410 {
		t.Fatal("cross-browser PIN accepted")
	}
	if w := cookiesCall(s, "POST", "/api/v1/auth/plex/poll", "https://attacker.example", pin); w.Code != 403 {
		t.Fatal("poll CSRF accepted")
	}
	poll := cookiesCall(s, "POST", "/api/v1/auth/plex/poll", s.Config.PublicOrigin, pin)
	if poll.Code != 200 || !strings.Contains(poll.Body.String(), `"connected":true`) {
		t.Fatal(poll.Body)
	}
	session := namedCookie(t, poll, s.cookieName())
	if !session.HttpOnly || session.SameSite != http.SameSiteStrictMode || session.Value == pin.Value {
		t.Fatal("unsafe authenticated session")
	}
	if w := cookiesCall(s, "POST", "/api/v1/auth/plex/poll", s.Config.PublicOrigin, pin); w.Code != 410 {
		t.Fatal("PIN replay accepted")
	}
	w := call(s, "GET", "/api/v1/session", session.Value, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"Alice"`) || !strings.Contains(w.Body.String(), `"plexConnected":true`) || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "discordId") {
		t.Fatalf("invalid Plex-only session: %s", w.Body)
	}
	if w := call(s, "GET", "/api/v1/services", session.Value, "", ""); w.Code != 200 {
		t.Fatal("Plex alone cannot enter Hub")
	}
	if w := call(s, "PUT", "/api/v1/preferences", session.Value, s.Config.PublicOrigin, `{"notifications":true}`); w.Code != 200 {
		t.Fatal("Plex-only preferences failed")
	}
	if w := call(s, "POST", "/api/v1/auth/logout", session.Value, s.Config.PublicOrigin, ""); w.Code != 200 {
		t.Fatal("logout failed")
	}
	if w := call(s, "GET", "/api/v1/services", session.Value, "", ""); w.Code != 401 {
		t.Fatal("logout left service links accessible")
	}
}
func TestPlexLoginRequiresConfiguredServerAccess(t *testing.T) {
	for _, tc := range []struct {
		name, resources string
		restricted      bool
	}{
		{"wrong server", `[{"clientIdentifier":"other-server","provides":"server","accessToken":"secret"}]`, false},
		{"no sharing", `[]`, false}, {"managed account", authorizedResources, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t)
			plexProvider(t, s, tc.resources, tc.restricted)
			start := call(s, "POST", "/api/v1/auth/plex/start", "", s.Config.PublicOrigin, "")
			poll := cookiesCall(s, "POST", "/api/v1/auth/plex/poll", s.Config.PublicOrigin, namedCookie(t, start, s.plexCookieName()))
			if poll.Code != 403 || len(s.sessions) != 0 || len(s.grants) != 0 {
				t.Fatalf("unauthorized login: %d", poll.Code)
			}
			var count int
			s.Store.DB.QueryRow(`SELECT count(*) FROM users`).Scan(&count)
			if count != 0 {
				t.Fatal("unauthorized account persisted")
			}
		})
	}
}
func TestLogoutCancelsPlexVerificationInFlight(t *testing.T) {
	s := fixture(t)
	plexProvider(t, s, authorizedResources, false)
	start := call(s, "POST", "/api/v1/auth/plex/start", "", s.Config.PublicOrigin, "")
	pin := namedCookie(t, start, s.plexCookieName())
	base := s.Upstream.HTTP.Transport
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v2/resources" {
			cookiesCall(s, "POST", "/api/v1/auth/logout", s.Config.PublicOrigin, pin)
		}
		return base.RoundTrip(r)
	})
	w := cookiesCall(s, "POST", "/api/v1/auth/plex/poll", s.Config.PublicOrigin, pin)
	if w.Code != 410 || len(s.sessions) != 0 {
		t.Fatal("logout resurrected an in-flight session")
	}
}
func TestDiscordLinkCannotSurviveLogout(t *testing.T) {
	s := fixture(t)
	cookie, _ := seedPlexOnly(t, s, "77")
	start := call(s, "GET", "/api/v1/auth/discord/start", cookie, "", "")
	location, _ := url.Parse(start.Header().Get("Location"))
	state := location.Query().Get("state")
	call(s, "POST", "/api/v1/auth/logout", cookie, s.Config.PublicOrigin, "")
	s.Upstream.HTTP.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("provider contacted after logout")
		return response(500, `{}`), nil
	})
	w := cookiesCall(s, "GET", "/api/v1/auth/discord/callback?code=test&state="+state, "", namedCookie(t, start, s.oauthCookieName()))
	if w.Code != 401 {
		t.Fatalf("stale link accepted: %d", w.Code)
	}
}
func TestPrivateEndpointsRejectDiscordOnlyExpiredAndRevokedSessions(t *testing.T) {
	for _, kind := range []string{"discord-only", "expired", "revoked", "provider-outage"} {
		t.Run(kind, func(t *testing.T) {
			s := fixture(t)
			cookie := seed(t, s, "11111", kind != "discord-only")
			if kind == "expired" {
				s.sessions[digest(cookie)] = session{"11111", time.Now().Add(-time.Second)}
			}
			if kind == "revoked" || kind == "provider-outage" {
				g := s.grants["11111"]
				g.VerifiedAt = time.Now().Add(-6 * time.Minute)
				s.grants["11111"] = g
			}
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "plex.tv" {
					t.Error("private data fetched after access failure")
				}
				status := 403
				if kind == "provider-outage" {
					status = 503
				}
				return response(status, `{}`), nil
			})
			for _, path := range []string{"/api/v1/services", "/api/v1/preferences", "/api/v1/requests", "/api/v1/recent", "/api/v1/auth/discord/start"} {
				w := call(s, "GET", path, cookie, "", "")
				if w.Code < 400 {
					t.Errorf("%s bypassed gate: %d", path, w.Code)
				}
			}
			w := call(s, "GET", "/api/v1/session", cookie, "", "")
			if strings.Contains(w.Body.String(), `"discordId"`) || strings.Contains(w.Body.String(), `"plexConnected":true`) {
				t.Fatal("unauthorized session advertised")
			}
		})
	}
}
