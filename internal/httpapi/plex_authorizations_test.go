package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func persistentLogin(t *testing.T, s *Server) (*http.Cookie, store.User) {
	t.Helper()
	s.Config.PlexGrantKey = strings.Repeat("ab", 32)
	plexProvider(t, s, authorizedResources, false)
	start := call(s, "POST", "/api/v1/auth/plex/start", "", s.Config.PublicOrigin, "")
	poll := cookiesCall(s, "POST", "/api/v1/auth/plex/poll", s.Config.PublicOrigin, namedCookie(t, start, s.plexCookieName()))
	if poll.Code != 200 {
		t.Fatal(poll.Body)
	}
	cookie := namedCookie(t, poll, s.cookieName())
	u, err := s.Store.User(context.Background(), s.sessions[digest(cookie.Value)].UserID)
	if err != nil {
		t.Fatal(err)
	}
	return cookie, u
}

func TestPersistentPlexAuthorizationSurvivesRestartWithoutWebSession(t *testing.T) {
	s := fixture(t)
	cookie, u := persistentLogin(t, s)
	ctx := context.Background()
	records, err := s.Store.PlexAuthorizations(ctx)
	if err != nil || len(records) != 1 {
		t.Fatal("authorization not saved", err)
	}
	for _, secret := range []string{"account-secret", "resource-secret", "ClientID", "PlexID"} {
		if bytes.Contains(records[u.ID], []byte(secret)) {
			t.Fatal("plaintext in database")
		}
	}
	// Natural browser expiry must not expire the background authorization.
	sess := s.sessions[digest(cookie.Value)]
	sess.Expires = time.Now().Add(-time.Hour)
	s.sessions[digest(cookie.Value)] = sess
	g := s.grants[u.ID]
	g.ExpiresAt = sess.Expires
	s.grants[u.ID] = g
	s.cleanup()
	if _, err := s.grant(ctx, u); err != nil {
		t.Fatal("browser expiration stopped reminders", err)
	}
	if w := cookiesCall(s, "GET", "/api/v1/services", "", cookie); w.Code != 401 {
		t.Fatal("expired web session regained access")
	}
	resumed := New(s.Config, s.Store)
	if err := resumed.RestorePlexAuthorizations(ctx); err != nil {
		t.Fatal(err)
	}
	if len(resumed.sessions) != 0 || !resumed.grants[u.ID].VerifiedAt.IsZero() || resumed.grants[u.ID].ServerToken != "" {
		t.Fatal("restored a browser session or trusted cached verification")
	}
	if w := cookiesCall(resumed, "GET", "/api/v1/services", "", cookie); w.Code != 401 {
		t.Fatal("old cookie survived restart")
	}
	// A Plex outage must stop personal access, without destroying valid credentials.
	resumed.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		return response(503, `{}`), nil
	})
	if _, err := resumed.grant(ctx, u); err == nil {
		t.Fatal("restored token trusted while Plex unavailable")
	}
	plexProvider(t, resumed, authorizedResources, false)
	fresh, err := resumed.grant(ctx, u)
	if err != nil || fresh.ServerToken != "resource-secret" || !fresh.Persistent {
		t.Fatal("fresh server access not acquired", err)
	}
}

func TestSavedPlexAuthorizationIsBoundAndAuthenticated(t *testing.T) {
	for _, change := range []string{"wrong-key", "missing-key", "server", "origin", "owner", "corrupt"} {
		t.Run(change, func(t *testing.T) {
			s := fixture(t)
			_, u := persistentLogin(t, s)
			ctx := context.Background()
			switch change {
			case "wrong-key":
				s.Config.PlexGrantKey = strings.Repeat("cd", 32)
			case "missing-key":
				s.Config.PlexGrantKey = ""
			case "server":
				s.Config.PlexServerID = "other-server"
			case "origin":
				s.Config.PublicOrigin = "https://other.example"
			case "owner":
				second, err := s.Store.LoginPlex(ctx, "88", "Bob")
				if err != nil {
					t.Fatal(err)
				}
				records, _ := s.Store.PlexAuthorizations(ctx)
				if err := s.Store.SavePlexAuthorization(ctx, second.ID, records[u.ID]); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				records, _ := s.Store.PlexAuthorizations(ctx)
				value := records[u.ID]
				value[len(value)-1] ^= 1
				if err := s.Store.SavePlexAuthorization(ctx, u.ID, value); err != nil {
					t.Fatal(err)
				}
			}
			resumed := New(s.Config, s.Store)
			if err := resumed.RestorePlexAuthorizations(ctx); err == nil || len(resumed.grants) != 0 {
				t.Fatal("unauthenticated authorization restored")
			}
		})
	}
}

func TestPersistentPlexRevocation(t *testing.T) {
	for _, reason := range []string{"logout", "delete-account", "plex-revoked", "identity-changed"} {
		t.Run(reason, func(t *testing.T) {
			s := fixture(t)
			cookie, u := persistentLogin(t, s)
			ctx := context.Background()
			switch reason {
			case "logout":
				w := cookiesCall(s, "POST", "/api/v1/auth/logout", s.Config.PublicOrigin, cookie)
				if w.Code != 200 {
					t.Fatal(w.Body)
				}
			case "delete-account":
				w := cookiesCall(s, "DELETE", "/api/v1/account", s.Config.PublicOrigin, cookie)
				if w.Code != 200 {
					t.Fatal(w.Body)
				}
			case "plex-revoked", "identity-changed":
				g := s.grants[u.ID]
				g.VerifiedAt = time.Time{}
				s.grants[u.ID] = g
				s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
					if reason == "identity-changed" {
						if r.URL.Path == "/api/v2/user" {
							return response(200, `{"id":88,"username":"Bob","restricted":false}`), nil
						}
						return response(200, authorizedResources), nil
					}
					return response(401, `{}`), nil
				})
				if _, err := s.grant(ctx, u); err == nil {
					t.Fatal("revoked identity still accepted")
				}
			}
			resumed := New(s.Config, s.Store)
			if err := resumed.RestorePlexAuthorizations(ctx); err != nil || len(resumed.grants) != 0 {
				t.Fatal("revoked authorization restored", err)
			}
		})
	}
}

func TestRejectedOldPlexCheckDoesNotRevokeNewLogin(t *testing.T) {
	s := fixture(t)
	_, u := persistentLogin(t, s)
	ctx := context.Background()
	g := s.grants[u.ID]
	g.VerifiedAt = time.Time{}
	s.grants[u.ID] = g
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		newGrant := upstream.Grant{PlexID: u.PlexID, AccountToken: "new-account-token", ClientID: "new-client", VerifiedAt: time.Now()}
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.savePlexAuthorization(ctx, u, &newGrant); err != nil {
			t.Fatal(err)
		}
		s.grants[u.ID] = newGrant
		return response(401, `{}`), nil
	})
	if _, err := s.grant(ctx, u); err == nil {
		t.Fatal("old rejected request accepted")
	}
	resumed := New(s.Config, s.Store)
	if err := resumed.RestorePlexAuthorizations(ctx); err != nil || resumed.grants[u.ID].AccountToken != "new-account-token" {
		t.Fatal("new login revoked by old request", err)
	}
}

func TestRestoredAuthorizationProcessesPersonalRemindersWithoutBrowser(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unwatched", true: "completed"}[completed], func(t *testing.T) {
			s, u, sources := queuedDiscordFixture(t)
			ctx := context.Background()
			s.Config.PlexGrantKey = strings.Repeat("ab", 32)
			s.Config.DiscordBotToken = "fixture-token"
			g := s.grants[u.ID]
			if err := s.savePlexAuthorization(ctx, u, &g); err != nil {
				t.Fatal(err)
			}
			resumed := New(s.Config, s.Store)
			if err := resumed.RestorePlexAuthorizations(ctx); err != nil {
				t.Fatal(err)
			}
			resumed.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/api/v2/user":
					if r.Header.Get("X-Plex-Token") != "account-11111" {
						t.Fatal("wrong personal account token")
					}
					return response(200, `{"id":11111,"username":"Alice"}`), nil
				case "/api/v2/resources":
					return response(200, `[{"clientIdentifier":"server-a","provides":"server","accessToken":"resource-11111"}]`), nil
				default:
					return s.Upstream.HTTP.Transport.RoundTrip(r)
				}
			})
			sources.completed = completed
			messages := 0
			resumed.Discord.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/api/v10/users/@me/channels" {
					return response(200, `{"id":"456","type":1,"recipients":[{"id":"11111"}]}`), nil
				}
				messages++
				return response(200, `{"id":"789","channel_id":"456"}`), nil
			})
			if _, err := resumed.refreshReminders(ctx, u); err != nil {
				t.Fatal(err)
			}
			if err := resumed.deliverDiscordReminders(ctx, u); err != nil {
				t.Fatal(err)
			}
			want := 1
			if completed {
				want = 0
			}
			if messages != want || len(resumed.sessions) != 0 {
				t.Fatal("incorrect background reminder delivery", messages)
			}
		})
	}
}

func TestPersistentLogoutStorageFailureDoesNotClaimRevocation(t *testing.T) {
	s := fixture(t)
	cookie, _ := persistentLogin(t, s)
	if _, err := s.Store.DB.Exec(`CREATE TRIGGER refuse_revoke BEFORE DELETE ON plex_authorizations BEGIN SELECT RAISE(ABORT,'fixture storage failure'); END;`); err != nil {
		t.Fatal(err)
	}
	w := cookiesCall(s, "POST", "/api/v1/auth/logout", s.Config.PublicOrigin, cookie)
	if w.Code != 503 || strings.Contains(w.Body.String(), "fixture storage failure") {
		t.Fatal("logout falsely reported success or leaked storage details", w.Code)
	}
	if _, err := s.Store.DB.Exec(`DROP TRIGGER refuse_revoke`); err != nil {
		t.Fatal(err)
	}
	w = cookiesCall(s, "POST", "/api/v1/auth/logout", s.Config.PublicOrigin, cookie)
	if w.Code != 200 {
		t.Fatal("logout retry failed", w.Code)
	}
}
