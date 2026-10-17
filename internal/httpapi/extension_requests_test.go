package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/store"
)

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
			items, err := s.Store.ExtensionRequests(t.Context(), "11111")
			if err != nil || len(items) != 0 {
				t.Fatal("rejected request persisted", err, items)
			}
		})
	}
}
