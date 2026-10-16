package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestRecentBooksUsePersonalFeedAndWithdrawAfterUnlink(t *testing.T) {
	s, cookie, _ := booksFixture(t)
	if w := call(s, "GET", "/api/v1/books/recent", "", "", ""); w.Code != 401 {
		t.Fatal("anonymous recent books")
	}
	linkBooks(t, s, cookie)
	other := seed(t, s, "22222", true)
	if w := call(s, "GET", "/api/v1/books/recent", other, "", ""); w.Code != 403 {
		t.Fatal("cross-owner recent books")
	}
	for _, query := range []string{"?q=book", "?page=0", "?page=1&page=2", "?userId=22222"} {
		if w := call(s, "GET", "/api/v1/books/recent"+query, cookie, "", ""); w.Code != 400 {
			t.Fatal("invalid recent selector")
		}
	}
	unlinked := false
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		name, password, ok := r.BasicAuth()
		if r.URL.Path != "/api/v1/opds/recent" || r.URL.Query().Get("page") != "2" || !ok || name != "alice-opds" || password != "personal-secret" {
			t.Fatal("recent feed request changed")
		}
		if unlinked {
			s.mu.Lock()
			s.clearBookGrantLocked("11111")
			s.mu.Unlock()
		}
		feed := strings.Replace(booksFeed, "<opensearch:startIndex>1<", "<opensearch:startIndex>21<", 1)
		return response(200, feed), nil
	})
	w := call(s, "GET", "/api/v1/books/recent?page=2", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"expiresAt":`) || !strings.Contains(w.Body.String(), "Private book") {
		t.Fatalf("recent books %d %s", w.Code, w.Body)
	}
	unlinked = true
	w = call(s, "GET", "/api/v1/books/recent?page=2", cookie, "", "")
	if w.Code != 403 || strings.Contains(w.Body.String(), "Private book") {
		t.Fatal("late unlinked response retained personal books")
	}
}
