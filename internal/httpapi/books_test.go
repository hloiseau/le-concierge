package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
)

const booksFeed = `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/"><opensearch:totalResults>1</opensearch:totalResults><opensearch:startIndex>1</opensearch:startIndex><opensearch:itemsPerPage>20</opensearch:itemsPerPage><entry><id>urn:booklore:book:7</id><title>Private book @everyone</title><author><name>Author</name></author><link rel="http://opds-spec.org/acquisition" href="/api/v1/opds/7/download?fileId=3" type="application/epub+zip"/><link rel="http://opds-spec.org/image" href="/api/v1/opds/7/cover"/></entry></feed>`

func booksFixture(t *testing.T) (*Server, string, *int) {
	t.Helper()
	s := fixture(t)
	s.Config.GrimmoryURL = "https://grimmory.test"
	s.Config.Services = []config.Service{{ID: "grimmory", URL: "https://books.example"}, {ID: "shelfmark", URL: "https://requests.example"}}
	cookie := seed(t, s, "11111", true)
	calls := 0
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		username, password, ok := r.BasicAuth()
		if r.Method != "GET" || r.URL.Host != "grimmory.test" || !ok || username != "alice-opds" || password != "personal-secret" {
			t.Fatalf("unexpected book request %s %s", r.Method, r.URL.Host)
		}
		if strings.HasSuffix(r.URL.Path, "/cover") {
			return response(200, "GIF89a"+strings.Repeat("\x00", 20)), nil
		}
		return response(200, booksFeed), nil
	})
	return s, cookie, &calls
}
func linkBooks(t *testing.T, s *Server, cookie string) {
	t.Helper()
	w := call(s, "POST", "/api/v1/books/link", cookie, s.Config.PublicOrigin, `{"username":"alice-opds","password":"personal-secret"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"linked":true`) {
		t.Fatalf("link: %d %s", w.Code, w.Body)
	}
}
func TestBooksLinkAndCatalogAreUserScoped(t *testing.T) {
	s, cookie, calls := booksFixture(t)
	for _, path := range []string{"/api/v1/books", "/api/v1/books/link", "/api/v1/books/7/cover", "/api/v1/book-requests/link?q=Dune"} {
		if w := call(s, "GET", path, "", "", ""); w.Code != 401 {
			t.Fatalf("anonymous %s: %d", path, w.Code)
		}
	}
	linkBooks(t, s, cookie)
	other := seed(t, s, "22222", true)
	for _, path := range []string{"/api/v1/books", "/api/v1/books/7/cover"} {
		if w := call(s, "GET", path, other, "", ""); w.Code != 403 {
			t.Fatalf("cross-user access: %d", w.Code)
		}
	}
	w := call(s, "GET", "/api/v1/books", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Private book") || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "grimmory.test") {
		t.Fatalf("catalog: %d %s", w.Code, w.Body)
	}
	cover := call(s, "GET", "/api/v1/books/7/cover", cookie, "", "")
	if cover.Code != 200 || cover.Header().Get("Content-Type") != "image/gif" || !strings.Contains(cover.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("cover: %d %v", cover.Code, cover.Header())
	}
	status := call(s, "GET", "/api/v1/books/link", cookie, "", "")
	if strings.Contains(status.Body.String(), "alice-opds") || strings.Contains(status.Body.String(), "secret") {
		t.Fatal("credentials returned")
	}
	if *calls != 3 {
		t.Fatalf("unexpected remote reads %d", *calls)
	}
	if w := call(s, "DELETE", "/api/v1/books/link", cookie, s.Config.PublicOrigin, ""); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := call(s, "GET", "/api/v1/books", cookie, "", ""); w.Code != 403 {
		t.Fatal("unlink retained access")
	}
}
func TestBooksValidateInputAndOriginsBeforeRemoteRead(t *testing.T) {
	s, cookie, calls := booksFixture(t)
	for _, body := range []string{`{"username":"alice:opds","password":"x"}`, `{"username":"alice","password":""}`, `{"username":"alice","password":"x","url":"https://evil.test"}`, `{"username":"alice","password":"x"} {}`} {
		if w := call(s, "POST", "/api/v1/books/link", cookie, s.Config.PublicOrigin, body); w.Code != 400 {
			t.Fatalf("invalid credential accepted %s", w.Body)
		}
	}
	if w := call(s, "POST", "/api/v1/books/link", cookie, "https://evil.test", `{"username":"alice","password":"secret"}`); w.Code != 403 {
		t.Fatal("CSRF")
	}
	for _, path := range []string{"/api/v1/books?userId=22222", "/api/v1/books?q=a&q=b", "/api/v1/books?page=1002", "/api/v1/books?page=0", "/api/v1/books?q=%0A", "/api/v1/book-requests/link?q=", "/api/v1/book-requests/link?q=Dune&page=1"} {
		if w := call(s, "GET", path, cookie, "", ""); w.Code != 400 {
			t.Fatalf("invalid query %s: %d", path, w.Code)
		}
	}
	if *calls != 0 {
		t.Fatal("invalid input sent remotely")
	}
}
func TestBooksRevocationErrorsAndLateReadsNeverReturnStaleCatalog(t *testing.T) {
	for _, state := range []string{"revoked", "outage", "unlink during read", "logout during read", "delete during read", "expiry"} {
		t.Run(state, func(t *testing.T) {
			s, cookie, _ := booksFixture(t)
			linkBooks(t, s, cookie)
			if state == "expiry" {
				s.mu.Lock()
				g := s.bookGrants["11111"]
				g.ExpiresAt = time.Now().Add(-time.Second)
				s.bookGrants["11111"] = g
				s.mu.Unlock()
			}
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				switch state {
				case "revoked":
					return response(403, "private failure"), nil
				case "outage":
					return response(503, "private failure"), nil
				case "unlink during read":
					call(s, "DELETE", "/api/v1/books/link", cookie, s.Config.PublicOrigin, "")
				case "logout during read":
					call(s, "POST", "/api/v1/auth/logout", cookie, s.Config.PublicOrigin, "")
				case "delete during read":
					if err := s.Store.Delete(context.Background(), "11111"); err != nil {
						t.Fatal(err)
					}
				case "expiry":
					t.Fatal("expired credentials used")
				}
				return response(200, booksFeed), nil
			})
			w := call(s, "GET", "/api/v1/books", cookie, "", "")
			if w.Code == 200 || strings.Contains(w.Body.String(), "Private book") || strings.Contains(w.Body.String(), "private failure") {
				t.Fatalf("stale/error source exposed: %d %s", w.Code, w.Body)
			}
			if state == "revoked" {
				if _, ok := s.currentBookGrant("11111"); ok {
					t.Fatal("revoked credential retained")
				}
			}
		})
	}
}
func TestBooksPendingLinkCannotResurrectAfterUnlink(t *testing.T) {
	s, cookie, _ := booksFixture(t)
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		call(s, "DELETE", "/api/v1/books/link", cookie, s.Config.PublicOrigin, "")
		return response(200, booksFeed), nil
	})
	w := call(s, "POST", "/api/v1/books/link", cookie, s.Config.PublicOrigin, `{"username":"alice-opds","password":"personal-secret"}`)
	if w.Code != 409 {
		t.Fatalf("late link: %d %s", w.Code, w.Body)
	}
	if _, ok := s.currentBookGrant("11111"); ok {
		t.Fatal("link recreated")
	}
}
func TestBookCommandsAndRequestFallbackNeverCreateARequest(t *testing.T) {
	s, cookie, calls := booksFixture(t)
	linkBooks(t, s, cookie)
	request := subscriptionInteraction(t, s)
	w := request("90001", "11111", `{"name":"livres"}`)
	var reply struct {
		Content string `json:"content"`
		Flags   int    `json:"flags"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Flags != 64 || strings.Contains(reply.Content, "@everyone") || !strings.Contains(reply.Content, "Private book") {
		t.Fatalf("book reply %s", w.Body)
	}
	if w = request("90001", "11111", `{"name":"livres"}`); w.Code != 409 {
		t.Fatal("replay")
	}
	w = request("90002", "11111", `{"name":"demander-livre","options":[{"name":"recherche","type":3,"value":"Dune & Herbert"}]}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "confirmez") {
		t.Fatalf("request fallback %s", w.Body)
	}
	w = call(s, "GET", "/api/v1/book-requests/link?q=Dune", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mode":"external"`) {
		t.Fatalf("request URL %s", w.Body)
	}
	if *calls != 2 {
		t.Fatalf("fallback contacted service: %d", *calls)
	}
	if text := s.bookRequestMessage(strings.Repeat("📚", 200)); len(text) > 2000 {
		t.Fatal("oversized reply")
	}
}
