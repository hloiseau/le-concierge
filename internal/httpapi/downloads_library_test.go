package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/upstream"
)

func downloadLibraryAPIFixture(t *testing.T) (*Server, string) {
	t.Helper()
	s := fixture(t)
	s.Config.DownloadarrURL = "https://downloadarr.test"
	s.Config.DownloadarrRevision = strings.Repeat("d", 40)
	cookie := seed(t, s, "11111", true)
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "plex.test" {
			t.Errorf("picker contacted unexpected host %s", r.URL.Host)
			return response(500, ""), nil
		}
		if r.URL.Path == "/identity" {
			return response(200, `{"MediaContainer":{"machineIdentifier":"server-a"}}`), nil
		}
		if r.Header.Get("X-Plex-Token") != "resource-11111" || r.Header.Get("Authorization") != "" {
			t.Error("picker did not use personal Plex token")
		}
		switch r.URL.Path {
		case "/library/all":
			return response(200, `{"MediaContainer":{"size":2,"Metadata":[{"ratingKey":"12","title":"Personal film","type":"movie","year":2026},{"ratingKey":"20","title":"Personal show","type":"show"}]}}`), nil
		case "/library/metadata/20":
			return response(200, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"20","title":"Personal show","type":"show","guid":"plex://show/20","librarySectionID":"1"}]}}`), nil
		case "/library/metadata/21":
			return response(200, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"21","parentRatingKey":"20","title":"Season one","type":"season","guid":"plex://season/21","librarySectionID":"1"}]}}`), nil
		case "/library/metadata/20/children":
			return response(200, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"21","parentRatingKey":"20","title":"Season one","type":"season","index":1}]}}`), nil
		case "/library/metadata/21/children":
			return response(200, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"22","parentRatingKey":"21","title":"First episode","type":"episode","index":1}]}}`), nil
		default:
			t.Errorf("unexpected picker read %s", r.URL.Path)
			return response(500, ""), nil
		}
	})
	return s, cookie
}

func TestDownloadsLibraryPersonalSearchAndNavigationWithoutDownloadarrLink(t *testing.T) {
	s, cookie := downloadLibraryAPIFixture(t)
	for _, query := range []string{"q=film", "parent=20", "parent=21"} {
		w := call(s, "GET", "/api/v1/downloads/library?"+query, cookie, "", "")
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body)
		}
		var page upstream.DownloadLibraryPage
		if json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) == 0 || page.ExpiresAt.Sub(page.CheckedAt) > 30*time.Second {
			t.Fatal("invalid page")
		}
		if strings.Contains(w.Body.String(), "resource-") || strings.Contains(w.Body.String(), "account-") {
			t.Fatal("credentials escaped")
		}
	}
	if len(s.downloadarrGrants) != 0 {
		t.Fatal("browsing created Downloadarr session")
	}
}

func TestDownloadsLibrarySelectorsAndAnonymousAccess(t *testing.T) {
	s, cookie := downloadLibraryAPIFixture(t)
	if w := call(s, "GET", "/api/v1/downloads/library?q=film", "", "", ""); w.Code != 401 {
		t.Fatal("anonymous library access")
	}
	for _, query := range []string{"", "q=a", "q=film&parent=20", "q=film&token=x", "q=film&q=other", "q=film&offset=-1", "q=film&offset=10001", "q=film&offset=01", "q=film&offset=", "parent=../20", "parent=020", "q=%0afilm", "q=film&token=hidden;ignored", "q=film&bad=%ZZ"} {
		w := call(s, "GET", "/api/v1/downloads/library?"+query, cookie, "", "")
		if w.Code != 400 {
			t.Fatalf("invalid query %s: %d", query, w.Code)
		}
	}
	s.Config.DownloadarrURL = ""
	if w := call(s, "GET", "/api/v1/downloads/library?q=film", cookie, "", ""); w.Code != 503 {
		t.Fatal("disabled picker accepted")
	}
}

func TestDownloadsLibraryKeepsAccountsSeparate(t *testing.T) {
	s, firstCookie := downloadLibraryAPIFixture(t)
	secondCookie := seed(t, s, "22222", true)
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/identity" {
			return response(200, `{"MediaContainer":{"machineIdentifier":"server-a"}}`), nil
		}
		if r.URL.Path != "/library/all" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		title := "First personal film"
		if r.Header.Get("X-Plex-Token") == "resource-22222" {
			title = "Second personal film"
		} else if r.Header.Get("X-Plex-Token") != "resource-11111" {
			t.Fatal("unexpected credential")
		}
		return downloadarrFixtureJSON(200, map[string]any{"MediaContainer": map[string]any{"size": 1, "Metadata": []any{map[string]any{"ratingKey": "12", "title": title, "type": "movie"}}}}), nil
	})
	for _, item := range []struct{ cookie, want, reject string }{{firstCookie, "First personal film", "Second personal film"}, {secondCookie, "Second personal film", "First personal film"}, {firstCookie, "First personal film", "Second personal film"}} {
		w := call(s, "GET", "/api/v1/downloads/library?q=film", item.cookie, "", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), item.want) || strings.Contains(w.Body.String(), item.reject) {
			t.Fatalf("account isolation: %d %s", w.Code, w.Body)
		}
	}
}

func TestDownloadsLibraryDiscardsLateLogoutAndGrantReplacement(t *testing.T) {
	for _, change := range []string{"logout", "replacement", "expiry"} {
		t.Run(change, func(t *testing.T) {
			s, cookie := downloadLibraryAPIFixture(t)
			original := s.Upstream.HTTP.Transport
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				result, err := original.RoundTrip(r)
				if r.URL.Path == "/library/all" {
					s.mu.Lock()
					switch change {
					case "logout":
						delete(s.sessions, digest(cookie))
					case "replacement":
						g := s.grants["11111"]
						g.ServerToken = "new-resource"
						s.grants["11111"] = g
					case "expiry":
						sess := s.sessions[digest(cookie)]
						sess.Expires = time.Now().Add(-time.Second)
						s.sessions[digest(cookie)] = sess
					}
					s.mu.Unlock()
				}
				return result, err
			})
			w := call(s, "GET", "/api/v1/downloads/library?q=film", cookie, "", "")
			if w.Code != 401 && w.Code != 403 {
				t.Fatalf("late result accepted: %d", w.Code)
			}
			if strings.Contains(w.Body.String(), "Personal film") {
				t.Fatal("late private metadata escaped")
			}
		})
	}
}
