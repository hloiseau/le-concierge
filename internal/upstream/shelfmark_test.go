package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type shelfmarkFixture struct {
	rows           []map[string]any
	mode, username string
	owner          int64
	calls, logins  int
	wrongOwner     bool
}

func newShelfmarkFixture(t *testing.T) (*Client, string, *shelfmarkFixture) {
	t.Helper()
	f := &shelfmarkFixture{mode: "builtin", username: "alice", owner: 7, rows: []map[string]any{}}
	f.rows = append(f.rows, map[string]any{"id": 1, "user_id": 7, "status": "fulfilled", "delivery_state": "complete", "created_at": "2026-10-15 12:00:00", "reviewed_at": "2026-10-15T14:00:00+02:00", "delivery_updated_at": "2026-10-15T13:00:00+00:00", "book_data": map[string]any{"title": "<script>Novel</script>", "author": "Author", "private_field": "secret"}, "release_data": map[string]any{"source_id": "private-source", "path": "/nas/private"}, "admin_note": "private admin detail"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls++
		if r.Header.Get("X-Api-Key") != "" || r.Header.Get("Authorization") != "" {
			t.Error("shared authentication sent")
		}
		if r.Method != "GET" && !(r.URL.Path == "/api/auth/login" && r.Method == "POST") {
			t.Errorf("unexpected mutation %s %s", r.Method, r.URL.Path)
		}
		var out any
		switch r.URL.Path {
		case "/api/auth/login":
			f.logins++
			var input map[string]any
			if json.NewDecoder(r.Body).Decode(&input) != nil || input["username"] != "alice" || input["password"] != "personal-password" || input["remember_me"] != false {
				t.Error("wrong login body")
			}
			http.SetCookie(w, &http.Cookie{Name: "shelfmark_session", Value: "personal-cookie", Path: "/", HttpOnly: true})
			out = map[string]any{"success": true}
		case "/api/auth/check":
			authenticated := r.Header.Get("Cookie") == "shelfmark_session=personal-cookie"
			out = map[string]any{"authenticated": authenticated, "auth_required": true, "auth_mode": f.mode, "username": f.username}
		case "/api/users/me/edit-context":
			if r.Header.Get("Cookie") != "shelfmark_session=personal-cookie" {
				t.Error("personal cookie missing")
			}
			out = map[string]any{"user": map[string]any{"id": f.owner, "username": f.username, "is_active": true, "settings": map[string]string{"private_token": "secret"}}}
		case "/api/requests":
			if r.Header.Get("Cookie") != "shelfmark_session=personal-cookie" || r.URL.Query().Get("limit") != "21" {
				t.Error("unbounded or anonymous request list")
			}
			out = f.rows
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)
	return New(), srv.URL, f
}
func TestShelfmarkPersonalLoginAndLifecycleNeverInventImport(t *testing.T) {
	c, base, f := newShelfmarkFixture(t)
	session, err := c.LinkShelfmark(context.Background(), base, "alice", "personal-password")
	if err != nil || session.UserID != 7 || session.Cookies == "" || f.logins != 1 || f.calls != 4 {
		t.Fatalf("link %+v %v calls=%d", session, err, f.calls)
	}
	page, err := c.ShelfmarkRequests(context.Background(), base, session, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].DeliveryState != "complete" || page.Items[0].CatalogStatus != "unverified" || f.calls != 7 {
		t.Fatalf("requests %+v %v", page, err)
	}
	if page.Items[0].CreatedAt.Format("2006-01-02T15:04:05Z07:00") != "2026-10-15T12:00:00Z" || !page.Items[0].ReviewedAt.Equal(page.Items[0].CreatedAt) {
		t.Fatal("source timestamps misinterpreted")
	}
	b, _ := json.Marshal(page)
	secret, _ := json.Marshal(session)
	if string(secret) != "{}" {
		t.Fatal("session is serializable")
	}
	for _, raw := range []string{"private-source", "/nas/", "secret", "personal-cookie", "admin_note"} {
		if strings.Contains(string(b), raw) {
			t.Fatalf("exposed %s", raw)
		}
	}
}
func TestShelfmarkSessionIsolationRevocationAndMalformedResponses(t *testing.T) {
	for _, mode := range []string{"other identity", "other owner", "unknown status", "invalid date", "too many rows", "no auth", "proxy"} {
		t.Run(mode, func(t *testing.T) {
			c, base, f := newShelfmarkFixture(t)
			session, err := c.LinkShelfmark(context.Background(), base, "alice", "personal-password")
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "other identity":
				f.owner = 8
			case "other owner":
				f.rows[0]["user_id"] = 8
			case "unknown status":
				f.rows[0]["delivery_state"] = "imported"
			case "invalid date":
				f.rows[0]["created_at"] = "yesterday"
			case "too many rows":
				for range 22 {
					f.rows = append(f.rows, map[string]any{})
				}
			case "no auth":
				session.Cookies = "revoked"
			case "proxy":
				f.mode = "proxy"
			}
			if _, err = c.ShelfmarkRequests(context.Background(), base, session, 1); err == nil {
				t.Fatal("unverified personal data accepted")
			}
		})
	}
}
func TestShelfmarkPaginationAndNoUnsupportedLogin(t *testing.T) {
	c, base, f := newShelfmarkFixture(t)
	for _, mode := range []string{"none", "proxy", "oidc", "cwa"} {
		f.mode = mode
		if _, err := c.LinkShelfmark(context.Background(), base, "alice", "personal-password"); err == nil {
			t.Fatal("unsupported login accepted")
		}
	}
	if f.logins != 0 {
		t.Fatal("password submitted to unsupported authentication")
	}
	f.mode = "builtin"
	session, err := c.LinkShelfmark(context.Background(), base, "alice", "personal-password")
	if err != nil {
		t.Fatal(err)
	}
	for id := 2; id <= 21; id++ {
		f.rows = append(f.rows, map[string]any{"id": id, "user_id": 7, "status": "pending", "delivery_state": "none", "created_at": "2026-10-15 12:00:00", "book_data": map[string]any{"title": fmt.Sprint("Book ", id), "author": "Author"}})
	}
	page, err := c.ShelfmarkRequests(context.Background(), base, session, 2)
	if err != nil || len(page.Items) != 20 || !page.HasMore || page.Page != 2 {
		t.Fatalf("pagination %+v %v", page, err)
	}
}
func TestShelfmarkLoginNeverFollowsRedirect(t *testing.T) {
	reached := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 307) }))
	defer srv.Close()
	c := New()
	c.HTTP.CheckRedirect = nil
	if _, err := c.LinkShelfmark(context.Background(), srv.URL, "alice", "personal-password"); err == nil || reached {
		t.Fatal("redirect followed")
	}
}
