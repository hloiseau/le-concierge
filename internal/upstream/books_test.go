package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func opdsFixture(entries string, page, total int) string {
	return fmt.Sprintf(`<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/" xmlns:dc="http://purl.org/dc/terms/"><opensearch:totalResults>%d</opensearch:totalResults><opensearch:startIndex>%d</opensearch:startIndex><opensearch:itemsPerPage>20</opensearch:itemsPerPage>%s</feed>`, total, (page-1)*20+1, entries)
}
func TestGrimmoryBooksScopedPaginationAndEditions(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		user, password, ok := r.BasicAuth()
		if !ok || password != "personal-secret" || r.Method != "GET" || r.URL.Path != "/api/v1/opds/catalog" || r.URL.Query().Get("q") != "Dune & Herbert" || r.URL.Query().Get("size") != "20" {
			t.Error("unexpected request")
		}
		if user == "reader-b" {
			fmt.Fprint(w, opdsFixture("", 1, 0))
			return
		}
		if r.URL.Query().Get("page") != "2" {
			t.Error("wrong page")
		}
		entry := `<entry><id>urn:booklore:book:7</id><title>Dune &amp; Herbert</title><author><name>Frank Herbert</name></author><dc:identifier>urn:isbn:0441172717</dc:identifier><meta property="belongs-to-collection">Dune</meta><meta property="group-position">1</meta><link rel="http://opds-spec.org/acquisition" href="/api/v1/opds/7/download?fileId=3" type="application/epub+zip"/><link rel="http://opds-spec.org/acquisition" href="/api/v1/opds/7/download?fileId=4" type="application/pdf"/><link rel="http://opds-spec.org/image" href="/api/v1/opds/7/cover?timestamp"/><summary>/private/nas/books/file.epub</summary></entry>`
		otherEdition := strings.ReplaceAll(entry, "book:7", "book:8")
		otherEdition = strings.ReplaceAll(otherEdition, "opds/7/", "opds/8/")
		fmt.Fprint(w, opdsFixture(entry+entry+otherEdition, 2, 70))
	}))
	defer server.Close()
	client := New()
	data, err := client.GrimmoryBooks(context.Background(), server.URL, "https://books.example/library", BookCredentials{"reader-a", "personal-secret"}, "Dune & Herbert", 2)
	if err != nil || len(data.Items) != 2 || !data.HasMore || data.Page != 2 || calls != 1 {
		t.Fatalf("catalog: %+v %v calls=%d", data, err, calls)
	}
	book := data.Items[0]
	if book.Title != "Dune & Herbert" || len(book.Formats) != 2 || book.ISBN != "0441172717" || book.Series != "Dune" || book.Volume != "1" || book.URL != "https://books.example/library/book/7" || book.CoverURL != "/api/v1/books/7/cover" {
		t.Fatalf("normalization: %+v", book)
	}
	encoded, _ := json.Marshal(data)
	if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "personal-secret") || strings.Contains(string(encoded), server.URL) {
		t.Fatal("private source data escaped")
	}
	other, err := client.GrimmoryBooks(context.Background(), server.URL, "", BookCredentials{"reader-b", "personal-secret"}, "Dune & Herbert", 1)
	if err != nil || len(other.Items) != 0 || calls != 2 {
		t.Fatal("cross-user cache or empty catalog failure")
	}
}
func TestGrimmoryRejectsBadSourcesAndRedirects(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       error
	}{
		{"denied", "secret", 403, ErrDenied}, {"outage", "private path", 503, ErrUnavailable}, {"missing", "", 404, ErrNotFound},
		{"html", "<html></html>", 200, ErrUnavailable}, {"missing metadata", `<feed xmlns="http://www.w3.org/2005/Atom"/>`, 200, ErrUnavailable},
		{"wrong page", opdsFixture("", 2, 0), 200, ErrUnavailable}, {"oversized", strings.Repeat("a", 2*1024*1024+1), 200, ErrUnavailable},
		{"bad id", opdsFixture(`<entry><id>urn:booklore:book:../secret</id><title>Bad</title></entry>`, 1, 1), 200, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			_, err := New().GrimmoryBooks(context.Background(), server.URL, "", BookCredentials{"reader", "secret"}, "", 1)
			if err != tc.want {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
	destinationCalls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls++ }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer redirect.Close()
	client := New()
	client.HTTP = &http.Client{}
	if _, err := client.GrimmoryBooks(context.Background(), redirect.URL, "", BookCredentials{"reader", "secret"}, "", 1); err != ErrUnavailable || destinationCalls != 0 {
		t.Fatal("credentials followed redirect")
	}
}
func TestBooksNeverFollowAdvertisedPrivateOrForeignLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, opdsFixture(`<entry><id>urn:booklore:book:1</id><title>Safe</title><link rel="http://opds-spec.org/acquisition" href="https://evil.example/api/v1/opds/1/download?fileId=1" type="application/epub+zip"/><link rel="http://opds-spec.org/image" href="file:///private/cover"/><link rel="http://opds-spec.org/acquisition" href="/api/v1/opds/2/download?fileId=1" type="application/pdf"/></entry>`, 1, 1))
	}))
	defer server.Close()
	data, err := New().GrimmoryBooks(context.Background(), server.URL, "", BookCredentials{"reader", "secret"}, "", 1)
	if err != nil || len(data.Items) != 1 || len(data.Items[0].Formats) != 0 || data.Items[0].CoverURL != "" {
		t.Fatalf("unsafe links %+v %v", data, err)
	}
}
func TestGrimmoryCoverBoundsAndContent(t *testing.T) {
	for _, body := range []string{"GIF89a" + strings.Repeat("\x00", 20), "<svg onload='alert(1)'/>", strings.Repeat("x", 2*1024*1024+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/opds/7/cover" {
				t.Error("wrong cover path")
			}
			fmt.Fprint(w, body)
		}))
		data, mime, err := New().GrimmoryCover(context.Background(), server.URL, BookCredentials{"reader", "secret"}, "7")
		if strings.HasPrefix(body, "GIF") {
			if err != nil || mime != "image/gif" || len(data) == 0 {
				t.Fatal("valid image refused")
			}
		} else if err == nil {
			t.Fatal("unsafe image accepted")
		}
		server.Close()
	}
}
func TestShelfmarkLinkIsOnlyAnEncodedSearch(t *testing.T) {
	query := "Dune & <script> # ISBN/é"
	link, err := ShelfmarkSearchLink("https://requests.example/books/", query)
	u, _ := url.Parse(link)
	hash, _ := url.ParseQuery(u.EscapedFragment())
	if err != nil || u.Host != "requests.example" || u.RawQuery != "" || hash.Get("q") != query || hash.Get("search_by") != "general" || u.Path != "/books/" {
		t.Fatalf("bad search %s %v", link, err)
	}
	for _, base := range []string{"javascript:alert(1)", "https://secret@requests.example", "https://requests.example?token=secret"} {
		if _, err := ShelfmarkSearchLink(base, "Dune"); err == nil {
			t.Fatal("bad base accepted")
		}
	}
	for _, query := range []string{"", strings.Repeat("é", 201), "Dune\n"} {
		if _, err := ShelfmarkSearchLink("https://requests.example", query); err == nil {
			t.Fatal("bad query accepted")
		}
	}
}
