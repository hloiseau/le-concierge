package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/store"
)

func pendingExtensionFixture(t *testing.T) (*Server, string, store.ExtensionRequest, *reminderSources) {
	t.Helper()
	s, cookie, source := reminderFixture(t)
	s.Config.AdminIDs = []string{"99999"}
	w := call(s, "POST", "/api/v1/extension-requests", cookie, s.Config.PublicOrigin, `{"mediaId":"42","days":7,"reason":"Finir"}`)
	var item store.ExtensionRequest
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &item) != nil || item.Status != "pending" {
		t.Fatalf("pending request fixture: %d %s", w.Code, w.Body)
	}
	return s, cookie, item, source
}

func TestExtensionRequestCancellationIsOwnerOnlyAndPreservesHistory(t *testing.T) {
	for _, mode := range []string{"owner", "other owner", "admin", "anonymous", "CSRF", "missing", "reviewed", "declined", "cancelled", "expired", "body injection", "query injection", "noncanonical id", "invalid id", "sources disabled"} {
		t.Run(mode, func(t *testing.T) {
			s, cookie, item, source := pendingExtensionFixture(t)
			path, origin, body := fmt.Sprintf("/api/v1/extension-requests/%d", item.ID), s.Config.PublicOrigin, ""
			want := 200
			switch mode {
			case "other owner", "admin":
				user := "22222"
				if mode == "admin" {
					user = "99999"
				}
				cookie = seed(t, s, user, true)
				want = 409
			case "anonymous":
				cookie, want = "", 401
			case "CSRF":
				origin, want = "https://another.example", 403
			case "missing":
				path, want = "/api/v1/extension-requests/999999", 409
			case "reviewed", "declined":
				if err := s.Store.ReviewExtensionRequest(t.Context(), item.ID, mode, "Admin reply", time.Now()); err != nil {
					t.Fatal(err)
				}
				want = 409
			case "cancelled":
				if err := s.Store.CancelExtensionRequest(t.Context(), "11111", item.ID, time.Now()); err != nil {
					t.Fatal(err)
				}
				want = 409
			case "expired":
				if _, err := s.Store.DB.Exec(`UPDATE extension_requests SET estimated_at=? WHERE id=?`, time.Now().Add(-time.Second).UnixMilli(), item.ID); err != nil {
					t.Fatal(err)
				}
				want = 409
			case "body injection":
				body, want = `{"userId":"11111"}`, 400
			case "query injection":
				path, want = path+"?userId=11111", 400
			case "noncanonical id":
				path, want = "/api/v1/extension-requests/01", 400
			case "invalid id":
				path, want = "/api/v1/extension-requests/0", 400
			case "sources disabled":
				s.Config.MaintainerrURL = ""
			}
			reads := source.collectionReads
			w := call(s, "DELETE", path, cookie, origin, body)
			if w.Code != want || (want == 409 && !strings.Contains(w.Body.String(), "request_changed")) {
				t.Fatalf("cancel: %d want %d %s", w.Code, want, w.Body)
			}
			if source.collectionReads != reads {
				t.Fatal("cancellation depended on Maintainerr")
			}
			history, err := s.Store.ExtensionRequests(t.Context(), "11111", time.Now())
			if err != nil || len(history) != 1 || history[0].ID != item.ID {
				t.Fatal("cancellation removed request history", history, err)
			}
			status := "pending"
			if want == 200 || mode == "cancelled" {
				status = "cancelled"
			} else if mode == "reviewed" || mode == "declined" || mode == "expired" {
				status = mode
			}
			if history[0].Status != status || history[0].Reason != item.Reason || !history[0].CreatedAt.Equal(item.CreatedAt) {
				t.Fatalf("unexpected request history: %+v", history[0])
			}
			if mode == "owner" {
				w = call(s, "POST", "/api/v1/extension-requests", cookie, s.Config.PublicOrigin, `{"mediaId":"42","days":14,"reason":"Changed"}`)
				var duplicate store.ExtensionRequest
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &duplicate) != nil || duplicate.ID != item.ID || duplicate.Status != "cancelled" || duplicate.Days != item.Days || duplicate.Reason != item.Reason {
					t.Fatalf("POST revived a cancelled request: %d %s", w.Code, w.Body)
				}
			}
		})
	}
}

type extensionCancelBody struct{ beforeRead func() }

func (b *extensionCancelBody) Read([]byte) (int, error) {
	if b.beforeRead != nil {
		run := b.beforeRead
		b.beforeRead = nil
		run()
	}
	return 0, io.EOF
}

func TestExtensionRequestCancellationRevalidatesAtMutation(t *testing.T) {
	for _, mode := range []string{"logout", "session changed", "session expired", "grant expired", "grant replaced", "identity changed", "reviewed", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s, cookie, item, _ := pendingExtensionFixture(t)
			body := &extensionCancelBody{beforeRead: func() {
				switch mode {
				case "logout":
					if w := call(s, "POST", "/api/v1/auth/logout", cookie, s.Config.PublicOrigin, ""); w.Code != 200 {
						t.Fatal(w.Body)
					}
				case "session changed", "session expired":
					s.mu.Lock()
					sess := s.sessions[digest(cookie)]
					if mode == "session changed" {
						sess.UserID = "22222"
					} else {
						sess.Expires = time.Now().Add(-time.Second)
					}
					s.sessions[digest(cookie)] = sess
					s.mu.Unlock()
				case "grant expired", "grant replaced":
					s.mu.Lock()
					grant := s.grants["11111"]
					if mode == "grant expired" {
						grant.ExpiresAt = time.Now().Add(-time.Second)
					} else {
						grant.AccountToken = "new-account-token"
					}
					s.grants["11111"] = grant
					s.mu.Unlock()
				case "identity changed":
					if err := s.Store.Unlink(t.Context(), "11111"); err != nil {
						t.Fatal(err)
					}
					if err := s.Store.LinkPlex(t.Context(), "11111", "22222"); err != nil {
						t.Fatal(err)
					}
				case "reviewed":
					if err := s.Store.ReviewExtensionRequest(t.Context(), item.ID, "reviewed", "Admin reply", time.Now()); err != nil {
						t.Fatal(err)
					}
				case "cancelled":
					if err := s.Store.CancelExtensionRequest(t.Context(), "11111", item.ID, time.Now()); err != nil {
						t.Fatal(err)
					}
				}
			}}
			r := httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/extension-requests/%d", item.ID), body)
			r.Header.Set("Origin", s.Config.PublicOrigin)
			r.AddCookie(&http.Cookie{Name: s.cookieName(), Value: cookie})
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			want, status := 403, "pending"
			if mode == "reviewed" || mode == "cancelled" {
				want, status = 409, mode
			}
			if w.Code != want {
				t.Fatalf("stale cancellation accepted: %d want %d %s", w.Code, want, w.Body)
			}
			history, err := s.Store.ExtensionRequests(t.Context(), "11111", time.Now())
			if err != nil || len(history) != 1 || history[0].Status != status {
				t.Fatal("stale cancellation changed the request", history, err)
			}
		})
	}
}

func TestExtensionRequestPastDeadlineLeavesAdminQueueAndCannotBeReviewed(t *testing.T) {
	s, cookie, item, _ := pendingExtensionFixture(t)
	admin := seed(t, s, "99999", true)
	if _, err := s.Store.DB.Exec(`UPDATE extension_requests SET estimated_at=? WHERE id=?`, time.Now().Add(-time.Second).UnixMilli(), item.ID); err != nil {
		t.Fatal(err)
	}
	if w := call(s, "GET", "/api/v1/extension-requests", cookie, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"expired"`) {
		t.Fatalf("expired history missing: %d %s", w.Code, w.Body)
	}
	if w := call(s, "GET", "/api/v1/admin/extension-requests", admin, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("expired request still actionable: %d %s", w.Code, w.Body)
	}
	for _, status := range []string{"reviewed", "declined"} {
		if w := call(s, "PUT", "/api/v1/admin/extension-requests", admin, s.Config.PublicOrigin, fmt.Sprintf(`{"id":%d,"status":%q,"note":"Too late"}`, item.ID, status)); w.Code != 409 || !strings.Contains(w.Body.String(), "request_changed") {
			t.Fatalf("expired request reviewed: %d %s", w.Code, w.Body)
		}
	}
}

func TestExtensionRequestsOwnerAndAdminWorkflow(t *testing.T) {
	s, cookie, _ := reminderFixture(t)
	s.Config.AdminIDs = []string{"99999"}
	adminCookie := seed(t, s, "99999", true)
	otherCookie := seed(t, s, "22222", true)
	body := `{"mediaId":"42","days":7,"reason":"Finir le film"}`
	w := call(s, "POST", "/api/v1/extension-requests", cookie, s.Config.PublicOrigin, body)
	if w.Code != 200 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var item store.ExtensionRequest
	if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.ID <= 0 || item.Title != "Private reminder movie" || item.Status != "pending" || item.Days != 7 || item.Requester != "" {
		t.Fatalf("unexpected request: %+v", item)
	}
	duplicate := call(s, "POST", "/api/v1/extension-requests", cookie, s.Config.PublicOrigin, body)
	var second store.ExtensionRequest
	if duplicate.Code != 200 || json.Unmarshal(duplicate.Body.Bytes(), &second) != nil || second.ID != item.ID {
		t.Fatalf("duplicate: %s", duplicate.Body)
	}
	for _, tc := range []struct {
		cookie, path string
		code         int
		contains     bool
	}{
		{cookie, "/api/v1/extension-requests", 200, true},
		{otherCookie, "/api/v1/extension-requests", 200, false},
		{cookie, "/api/v1/admin/extension-requests", 403, false},
		{adminCookie, "/api/v1/admin/extension-requests", 200, true},
	} {
		w := call(s, "GET", tc.path, tc.cookie, "", "")
		if w.Code != tc.code || strings.Contains(w.Body.String(), item.Title) != tc.contains {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body)
		}
	}
	review := fmt.Sprintf(`{"id":%d,"status":"reviewed","note":"Échéance vérifiée manuellement"}`, item.ID)
	if w := call(s, "PUT", "/api/v1/admin/extension-requests", cookie, s.Config.PublicOrigin, review); w.Code != 403 {
		t.Fatalf("owner could review: %d", w.Code)
	}
	if w := call(s, "PUT", "/api/v1/admin/extension-requests", adminCookie, "https://attacker.test", review); w.Code != 403 {
		t.Fatal("CSRF accepted")
	}
	if w := call(s, "PUT", "/api/v1/admin/extension-requests", adminCookie, s.Config.PublicOrigin, review); w.Code != 200 {
		t.Fatalf("review: %s", w.Body)
	}
	if w := call(s, "PUT", "/api/v1/admin/extension-requests", adminCookie, s.Config.PublicOrigin, review); w.Code != 409 {
		t.Fatal("double review accepted")
	}
	own := call(s, "GET", "/api/v1/extension-requests", cookie, "", "")
	if !strings.Contains(own.Body.String(), `"status":"reviewed"`) || !strings.Contains(own.Body.String(), "manuellement") {
		t.Fatal(own.Body)
	}
	queue := call(s, "GET", "/api/v1/admin/extension-requests", adminCookie, "", "")
	if strings.Contains(queue.Body.String(), item.Title) {
		t.Fatal("resolved request still pending")
	}
}

func TestExtensionRequestsRejectStaleAndUntrustedInput(t *testing.T) {
	for _, tc := range []struct {
		name, body, origin string
		mutate             func(*Server, *reminderSources)
		code               int
	}{
		{"forged title", `{"mediaId":"42","days":7,"title":"fake"}`, "", nil, 400},
		{"too long", `{"mediaId":"42","days":31}`, "", nil, 400},
		{"other media", `{"mediaId":"99","days":7}`, "", nil, 409},
		{"CSRF", `{"mediaId":"42","days":7}`, "https://attacker.test", nil, 403},
		{"cancelled", `{"mediaId":"42","days":7}`, "", func(_ *Server, f *reminderSources) { f.excluded = true }, 409},
		{"source down", `{"mediaId":"42","days":7}`, "", func(_ *Server, f *reminderSources) { f.failing = true }, 503},
		{"revoked", `{"mediaId":"42","days":7}`, "", func(s *Server, f *reminderSources) {
			f.beforeMetadata = func() { s.mu.Lock(); delete(s.grants, "11111"); s.mu.Unlock() }
		}, 403},
		{"no reviewer", `{"mediaId":"42","days":7}`, "", func(s *Server, _ *reminderSources) { s.Config.AdminIDs = nil }, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, cookie, f := reminderFixture(t)
			s.Config.AdminIDs = []string{"99999"}
			if tc.mutate != nil {
				tc.mutate(s, f)
			}
			origin := tc.origin
			if origin == "" {
				origin = s.Config.PublicOrigin
			}
			w := call(s, http.MethodPost, "/api/v1/extension-requests", cookie, origin, tc.body)
			if w.Code != tc.code {
				t.Fatalf("got %d want %d: %s", w.Code, tc.code, w.Body)
			}
			items, err := s.Store.ExtensionRequests(t.Context(), "11111", time.Now())
			if err != nil || len(items) != 0 {
				t.Fatal("rejected request persisted", err, items)
			}
		})
	}
}
