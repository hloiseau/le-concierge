package upstream

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBookCollectionsKeepEditionsLanguagesFractionalAndSpecialLabels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, _ := r.BasicAuth()
		if r.URL.Path != "/api/v1/opds/catalog" || r.URL.Query().Get("series") != "Saga & friends" || r.URL.Query().Get("q") != "" {
			t.Fatalf("unfiltered/private series navigation: %s", r.URL)
		}
		if user == "other" {
			fmt.Fprint(w, opdsFixture("", 1, 0))
			return
		}
		entries := ""
		for i, volume := range []string{"1.5", "HS", ""} {
			language := "fr"
			if i == 1 {
				language = "en"
			}
			entries += fmt.Sprintf(`<entry><id>urn:booklore:book:%d</id><title>Same work title</title><dc:identifier>urn:isbn:0441172717</dc:identifier><dc:language>%s</dc:language><dc:publisher>Publisher</dc:publisher><updated>2000-01-01T00:00:00Z</updated><meta property="belongs-to-collection">Saga &amp; friends</meta><meta property="group-position">%s</meta></entry>`, i+1, language, volume)
		}
		fmt.Fprint(w, opdsFixture(entries, 1, 25))
	}))
	defer server.Close()
	client := New()
	data, err := client.GrimmoryCollections(t.Context(), server.URL, "https://books.example", BookCredentials{"reader", "secret"}, "Saga & friends", 1)
	if err != nil || len(data.Items) != 1 || len(data.Items[0].Books) != 3 || !data.HasMore || data.ReferenceStatus != "unavailable" || data.Source != "grimmory_opds" || !data.ExpiresAt.After(data.FetchedAt) {
		t.Fatalf("collections %+v %v", data, err)
	}
	books := data.Items[0].Books
	if books[0].Volume != "1.5" || books[1].Volume != "HS" || books[2].Volume != "" || books[0].Language != "fr" || books[1].Language != "en" || books[0].Publisher != "Publisher" || books[0].ID == books[1].ID {
		t.Fatalf("edition metadata lost: %+v", books)
	}
	other, err := client.GrimmoryCollections(t.Context(), server.URL, "", BookCredentials{"other", "secret"}, "Saga & friends", 1)
	if err != nil || len(other.Items) != 0 {
		t.Fatalf("cross-account collection: %+v %v", other, err)
	}
}

func TestBookCollectionsRejectIgnoredSeriesFilterAndRetainEmptyPagePagination(t *testing.T) {
	for _, empty := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			entry := `<entry><id>urn:booklore:book:1</id><title>Private unmatched series</title><meta property="belongs-to-collection">Other</meta></entry>`
			if empty {
				entry = ""
			}
			fmt.Fprint(w, opdsFixture(entry, 1, 40))
		}))
		result, err := New().GrimmoryCollections(t.Context(), server.URL, "", BookCredentials{"reader", "secret"}, "Selected", 1)
		server.Close()
		if empty {
			if err != nil || len(result.Items) != 0 || !result.HasMore {
				t.Fatalf("filtered empty page %+v %v", result, err)
			}
		} else if err != ErrUnavailable || len(result.Items) != 0 {
			t.Fatalf("ignored series filter returned %+v %v", result, err)
		}
	}
	if _, err := New().GrimmoryCollections(t.Context(), "http://unreachable.invalid", "", BookCredentials{"reader", "secret"}, strings.Repeat("x", 201), 1); err != ErrDenied {
		t.Fatal("oversized filter attempted remote access")
	}
}
