package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func shelfmarkAPIFixture(t *testing.T) (*Server, string, *int) {
	t.Helper()
	s := fixture(t)
	s.Config.ShelfmarkURL = "https://shelfmark.test"
	cookie := seed(t, s, "11111", true)
	calls := 0
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Api-Key") != "" {
			t.Fatal("shared credential sent")
		}
		if r.Method != "GET" && r.URL.Path != "/api/auth/login" {
			t.Fatal("unexpected mutation")
		}
		switch r.URL.Path {
		case "/api/auth/check":
			return response(200, fmt.Sprintf(`{"auth_required":true,"auth_mode":"builtin","authenticated":%v,"username":"alice"}`, r.Header.Get("Cookie") == "shelfmark_session=personal-cookie")), nil
		case "/api/auth/login":
			out := response(200, `{"success":true}`)
			out.Header.Set("Set-Cookie", "shelfmark_session=personal-cookie; Path=/; HttpOnly; Secure")
			return out, nil
		case "/api/users/me/edit-context":
			return response(200, `{"user":{"id":7,"username":"alice","is_active":true,"private_token":"secret"}}`), nil
		case "/api/requests":
			return response(200, `[{"id":1,"user_id":7,"status":"fulfilled","delivery_state":"complete","created_at":"2026-10-15 12:00:00","book_data":{"title":"A book","author":"An author"},"release_data":{"path":"/private/nas"}}]`), nil
		default:
			t.Fatalf("unexpected route %s", r.URL.Path)
			return nil, nil
		}
	})
	return s, cookie, &calls
}
func linkShelfmarkFixture(t *testing.T, s *Server, cookie string) {
	t.Helper()
	w := call(s, "POST", "/api/v1/book-requests/link-session", cookie, s.Config.PublicOrigin, `{"username":"alice","password":"personal-password"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"linked":true`) {
		t.Fatalf("link %d %s", w.Code, w.Body)
	}
}
func TestShelfmarkHTTPPersonalLifecycleAndUnlink(t *testing.T) {
	s, cookie, calls := shelfmarkAPIFixture(t)
	other := seed(t, s, "22222", true)
	if w := call(s, "GET", "/api/v1/book-requests", cookie, "", ""); w.Code != 403 || *calls != 0 {
		t.Fatal("unlinked account read source")
	}
	if w := call(s, "POST", "/api/v1/book-requests/link-session", cookie, "https://other.test", `{"username":"alice","password":"personal-password"}`); w.Code != 403 || *calls != 0 {
		t.Fatal("CSRF login accepted")
	}
	linkShelfmarkFixture(t, s, cookie)
	for _, path := range []string{"/api/v1/book-requests?userId=7", "/api/v1/book-requests?page=0", "/api/v1/book-requests?page=1&page=2"} {
		if w := call(s, "GET", path, cookie, "", ""); w.Code != 400 {
			t.Fatalf("query accepted %s", path)
		}
	}
	if w := call(s, "GET", "/api/v1/book-requests", other, "", ""); w.Code != 403 {
		t.Fatal("grant crossed owners")
	}
	w := call(s, "GET", "/api/v1/book-requests", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"catalogStatus":"unverified"`) || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("requests %d %s", w.Code, w.Body)
	}
	if w = call(s, "DELETE", "/api/v1/book-requests/link-session", cookie, s.Config.PublicOrigin, ""); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if _, ok := s.currentShelfmarkGrant("11111"); ok {
		t.Fatal("unlink retained session")
	}
	if w = call(s, "GET", "/api/v1/book-requests", cookie, "", ""); w.Code != 403 {
		t.Fatal("read after unlink")
	}
}
func TestShelfmarkLateResponsesCannotRestoreRevokedLink(t *testing.T) {
	for _, phase := range []string{"link", "requests"} {
		for _, action := range []string{"unlink", "logout"} {
			t.Run(phase+action, func(t *testing.T) {
				s, cookie, _ := shelfmarkAPIFixture(t)
				if phase == "requests" {
					linkShelfmarkFixture(t, s, cookie)
				}
				original := s.Upstream.HTTP.Transport
				s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
					out, err := original.RoundTrip(r)
					if (phase == "link" && r.URL.Path == "/api/users/me/edit-context") || (phase == "requests" && r.URL.Path == "/api/requests") {
						path, method := "/api/v1/book-requests/link-session", "DELETE"
						if action == "logout" {
							path, method = "/api/v1/auth/logout", "POST"
						}
						if w := call(s, method, path, cookie, s.Config.PublicOrigin, ""); w.Code != 200 {
							t.Fatal(w.Body)
						}
					}
					return out, err
				})
				path, method, body := "/api/v1/book-requests", "GET", ""
				if phase == "link" {
					path, method, body = "/api/v1/book-requests/link-session", "POST", `{"username":"alice","password":"personal-password"}`
				}
				w := call(s, method, path, cookie, s.Config.PublicOrigin, body)
				if w.Code == 200 || strings.Contains(w.Body.String(), "A book") {
					t.Fatalf("late response escaped %d %s", w.Code, w.Body)
				}
				if _, ok := s.currentShelfmarkGrant("11111"); ok {
					t.Fatal("revoked link restored")
				}
			})
		}
	}
}
func TestShelfmarkRevocationForgetsSession(t *testing.T) {
	s, cookie, _ := shelfmarkAPIFixture(t)
	linkShelfmarkFixture(t, s, cookie)
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		return response(401, `{"error":"private upstream details"}`), nil
	})
	if w := call(s, "GET", "/api/v1/book-requests", cookie, "", ""); w.Code != 403 || strings.Contains(w.Body.String(), "private upstream") {
		t.Fatal(w.Body)
	}
	if _, ok := s.currentShelfmarkGrant("11111"); ok {
		t.Fatal("revoked cookie retained")
	}
	if _, err := s.Store.User(context.Background(), "11111"); err != nil {
		t.Fatal(err)
	}
}
