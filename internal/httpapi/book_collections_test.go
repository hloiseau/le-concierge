package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestBookCollectionsPersonalAccessValidationAndLateUnlink(t *testing.T) {
	s, cookie, calls := booksFixture(t)
	if w := call(s, "GET", "/api/v1/book-collections", "", "", ""); w.Code != 401 {
		t.Fatalf("anonymous collections %d", w.Code)
	}
	if w := call(s, "GET", "/api/v1/book-collections", cookie, "", ""); w.Code != 403 {
		t.Fatalf("unlinked collections %d", w.Code)
	}
	for _, query := range []string{"?userId=22222", "?series=a&series=b", "?page=0", "?page=1002", "?series=%0A", "?url=https://evil.invalid"} {
		if w := call(s, "GET", "/api/v1/book-collections"+query, cookie, "", ""); w.Code != 400 {
			t.Fatalf("invalid query %s: %d", query, w.Code)
		}
	}
	if *calls != 0 {
		t.Fatal("invalid input fetched catalog")
	}
	linkBooks(t, s, cookie)
	other := seed(t, s, "22222", true)
	if w := call(s, "GET", "/api/v1/book-collections", other, "", ""); w.Code != 403 {
		t.Fatalf("foreign association %d", w.Code)
	}
	unlinked := false
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/opds/catalog" || r.URL.Query().Get("series") != "Saga" {
			t.Fatal("wrong collection provider read")
		}
		if unlinked {
			s.mu.Lock()
			s.clearBookGrantLocked("11111")
			s.mu.Unlock()
		}
		return response(200, strings.Replace(booksFeed, "</entry>", `<meta property="belongs-to-collection">Saga</meta><meta property="group-position">1.5</meta></entry>`, 1)), nil
	})
	w := call(s, "GET", "/api/v1/book-collections?series=Saga", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"volume":"1.5"`) || !strings.Contains(w.Body.String(), `"referenceStatus":"unavailable"`) {
		t.Fatalf("collections %d %s", w.Code, w.Body)
	}
	unlinked = true
	w = call(s, "GET", "/api/v1/book-collections?series=Saga", cookie, "", "")
	if w.Code != 403 || strings.Contains(w.Body.String(), "Private book") {
		t.Fatalf("late revoked collection %d %s", w.Code, w.Body)
	}
}
