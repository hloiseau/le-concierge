package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
)

const bookMatchInput = `{"requestPage":1,"bookId":"7","catalogPage":1,"catalogQuery":""}`
const bookMatchPath = "/api/v1/book-requests/1/catalog-match"

func bookTrackingFixture(t *testing.T) (*Server, string) {
	t.Helper()
	s, cookie, _ := shelfmarkAPIFixture(t)
	s.Config.GrimmoryURL = "https://grimmory.test"
	s.Config.Services = append(s.Config.Services, config.Service{ID: "grimmory", URL: "https://books.example"})
	original := s.Upstream.HTTP.Transport
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "grimmory.test" {
			return original.RoundTrip(r)
		}
		name, pass, ok := r.BasicAuth()
		if !ok || name != "alice-opds" || pass != "personal-secret" || r.Method != "GET" || r.URL.Path != "/api/v1/opds/catalog" {
			t.Fatalf("unexpected OPDS request: %s", r.URL.Path)
		}
		return response(200, booksFeed), nil
	})
	linkShelfmarkFixture(t, s, cookie)
	linkBooks(t, s, cookie)
	return s, cookie
}

func confirmBookFixture(t *testing.T, s *Server, cookie string) {
	t.Helper()
	w := call(s, "POST", bookMatchPath, cookie, s.Config.PublicOrigin, bookMatchInput)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"catalogStatus":"available"`) {
		t.Fatalf("confirm %d %s", w.Code, w.Body)
	}
}

func TestBookTrackingExplicitMatchLifecycleAndProvenance(t *testing.T) {
	s, cookie := bookTrackingFixture(t)
	confirmBookFixture(t, s, cookie)
	confirmBookFixture(t, s, cookie)
	var count int
	var bookEvidence, requestEvidence string
	if err := s.Store.DB.QueryRow(`SELECT COUNT(*),book_json,request_json FROM book_availability_events`).Scan(&count, &bookEvidence, &requestEvidence); err != nil || count != 1 {
		t.Fatalf("events %d %v", count, err)
	}
	if !strings.Contains(bookEvidence, "Private book") || !strings.Contains(requestEvidence, `"deliveryState":"complete"`) || strings.Contains(requestEvidence, "/private/") {
		t.Fatal("missing normalized proof or retained raw data")
	}
	w := call(s, "GET", "/api/v1/book-requests", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"method":"user_confirmed"`) || !strings.Contains(w.Body.String(), "Private book") {
		t.Fatal(w.Body)
	}
	for _, secret := range []string{"personal-secret", "personal-cookie", "alice-opds", "grimmory.test"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("private source credential/identity escaped")
		}
	}
	if err := validateContractResponse(loadContract(t), "GET", "/api/v1/book-requests", w); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.DB.Exec(`UPDATE book_request_matches SET verified_at=?`, time.Now().Add(-6*time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	w = call(s, "GET", "/api/v1/book-requests", cookie, "", "")
	if !strings.Contains(w.Body.String(), `"catalogStatus":"check_required"`) || !strings.Contains(w.Body.String(), `"book":null`) || strings.Contains(w.Body.String(), "Private book") {
		t.Fatal("stale book metadata escaped", w.Body)
	}
	if err := validateContractResponse(loadContract(t), "GET", "/api/v1/book-requests", w); err != nil {
		t.Fatal(err)
	}
	if w = call(s, "DELETE", bookMatchPath, cookie, s.Config.PublicOrigin, ""); w.Code != 204 {
		t.Fatal(w.Body)
	}
	if w = call(s, "GET", "/api/v1/book-requests", cookie, "", ""); strings.Contains(w.Body.String(), "catalogMatch") {
		t.Fatal("withdrawn match remained visible")
	}
	confirmBookFixture(t, s, cookie)
	var state string
	if err := s.Store.DB.QueryRow(`SELECT COUNT(*),state FROM book_availability_events`).Scan(&count, &state); err != nil || count != 1 || state != "verified" {
		t.Fatalf("reconfirmation duplicated or failed to reactivate event: %d %s %v", count, state, err)
	}
}

func TestBookTrackingSelectionIsExactAndAuthorized(t *testing.T) {
	for _, scenario := range []string{"same title wrong edition", "no acquisition", "delivery incomplete", "other owner", "csrf", "unknown fields", "catalog page moved", "source account changed"} {
		t.Run(scenario, func(t *testing.T) {
			s, cookie := bookTrackingFixture(t)
			original := s.Upstream.HTTP.Transport
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "grimmory.test" {
					feed := booksFeed
					switch scenario {
					case "same title wrong edition":
						feed = strings.ReplaceAll(feed, "book:7", "book:8")
					case "no acquisition":
						feed = strings.ReplaceAll(feed, "http://opds-spec.org/acquisition", "alternate")
					case "catalog page moved":
						feed = strings.ReplaceAll(feed, "<entry>", "<ignored>")
						feed = strings.ReplaceAll(feed, "</entry>", "</ignored>")
					}
					return response(200, feed), nil
				}
				if r.URL.Path == "/api/requests" && scenario == "delivery incomplete" {
					return response(200, `[{"id":1,"user_id":7,"status":"fulfilled","delivery_state":"downloading","created_at":"2026-10-15 12:00:00","book_data":{"title":"A book","author":"An author"}}]`), nil
				}
				return original.RoundTrip(r)
			})
			body, origin, want := bookMatchInput, s.Config.PublicOrigin, 409
			switch scenario {
			case "other owner":
				cookie = seed(t, s, "22222", true)
				want = 403
			case "csrf":
				origin = "https://evil.test"
				want = 403
			case "unknown fields":
				body = strings.TrimSuffix(body, "}") + `,"userId":"22222"}`
				want = 400
			case "source account changed":
				confirmBookFixture(t, s, cookie)
				g := s.bookGrants["11111"]
				g.Credentials.Username = "bob-opds"
				g.Revision = token()
				s.bookGrants["11111"] = g
				w := call(s, "GET", "/api/v1/book-requests", cookie, "", "")
				if w.Code != 200 || strings.Contains(w.Body.String(), "catalogMatch") {
					t.Fatal("association crossed personal source accounts", w.Body)
				}
				return
			}
			w := call(s, "POST", bookMatchPath, cookie, origin, body)
			if w.Code != want {
				t.Fatalf("got %d want %d %s", w.Code, want, w.Body)
			}
			var count int
			if err := s.Store.DB.QueryRow(`SELECT COUNT(*) FROM book_availability_events`).Scan(&count); err != nil || count != 0 {
				t.Fatal("unverified event emitted", count, err)
			}
		})
	}
}

func TestBookTrackingLateReadsCannotRestoreRevokedAccess(t *testing.T) {
	for _, action := range []string{"unlink books", "unlink shelfmark", "logout", "withdraw"} {
		t.Run(action, func(t *testing.T) {
			s, cookie := bookTrackingFixture(t)
			if action == "withdraw" {
				confirmBookFixture(t, s, cookie)
			}
			original := s.Upstream.HTTP.Transport
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				out, err := original.RoundTrip(r)
				if r.URL.Host == "grimmory.test" {
					path, method := "/api/v1/books/link", "DELETE"
					switch action {
					case "unlink shelfmark":
						path = "/api/v1/book-requests/link-session"
					case "logout":
						path = "/api/v1/auth/logout"
						method = "POST"
					case "withdraw":
						path = bookMatchPath
					}
					w := call(s, method, path, cookie, s.Config.PublicOrigin, "")
					if w.Code != 200 && w.Code != 204 {
						t.Fatal(w.Body)
					}
				}
				return out, err
			})
			if action == "withdraw" {
				u, _ := s.Store.User(context.Background(), "11111")
				if err := s.refreshBookTracking(context.Background(), u); err != nil {
					t.Fatal(err)
				}
			} else {
				w := call(s, "POST", bookMatchPath, cookie, s.Config.PublicOrigin, bookMatchInput)
				if w.Code == 200 {
					t.Fatal("late confirmation accepted")
				}
			}
			var active int
			if err := s.Store.DB.QueryRow(`SELECT COUNT(*) FROM book_request_matches WHERE active=1`).Scan(&active); err != nil || active != 0 {
				t.Fatal("late read restored match", active, err)
			}
		})
	}
}

func TestBookTrackingBoundedCursorResumeAndPlexRevocation(t *testing.T) {
	s, cookie := bookTrackingFixture(t)
	confirmBookFixture(t, s, cookie)
	original := s.Upstream.HTTP.Transport
	pages := []string{}
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "grimmory.test" {
			page := r.URL.Query().Get("page")
			pages = append(pages, page)
			if page == "1" {
				return response(200, `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/"><opensearch:totalResults>21</opensearch:totalResults><opensearch:startIndex>1</opensearch:startIndex><opensearch:itemsPerPage>20</opensearch:itemsPerPage></feed>`), nil
			}
			return response(200, strings.ReplaceAll(strings.ReplaceAll(booksFeed, "<opensearch:totalResults>1<", "<opensearch:totalResults>21<"), "<opensearch:startIndex>1<", "<opensearch:startIndex>21<")), nil
		}
		return original.RoundTrip(r)
	})
	u, _ := s.Store.User(context.Background(), "11111")
	if err := s.refreshBookTracking(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	var page int
	var verified *int64
	if err := s.Store.DB.QueryRow(`SELECT catalog_page,verified_at FROM book_request_matches WHERE active=1`).Scan(&page, &verified); err != nil || page != 2 || verified != nil {
		t.Fatalf("cursor not saved: %d %v %v", page, verified, err)
	}
	// Simulate renewed personal grants after restart: durable cursors, no saved secrets.
	a := s.shelfmarkGrants[u.ID]
	a.Revision = token()
	s.shelfmarkGrants[u.ID] = a
	b := s.bookGrants[u.ID]
	b.Revision = token()
	s.bookGrants[u.ID] = b
	if err := s.refreshBookTracking(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(pages) != "[1 2]" {
		t.Fatal("unbounded or restarted scan", pages)
	}
	w := call(s, "GET", "/api/v1/book-requests", cookie, "", "")
	var result trackedBookRequestsPage
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Items[0].CatalogStatus != "available" {
		t.Fatal(w.Body)
	}
	g := s.grants[u.ID]
	g.VerifiedAt = time.Now().Add(-6 * time.Minute)
	s.grants[u.ID] = g
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "grimmory.test" || r.URL.Host == "shelfmark.test" {
			t.Fatal("books read after Plex revocation")
		}
		return response(401, `{}`), nil
	})
	if err := s.refreshBookTracking(context.Background(), u); err == nil {
		t.Fatal("revoked Plex accepted")
	}
	if _, ok := s.currentBookGrant(u.ID); ok {
		t.Fatal("revoked grant retained")
	}
}

func TestBookTrackingWithdrawRejectsPendingConfirmation(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			s, cookie := bookTrackingFixture(t)
			if existing {
				confirmBookFixture(t, s, cookie)
			}
			original := s.Upstream.HTTP.Transport
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				out, err := original.RoundTrip(r)
				if r.URL.Host == "grimmory.test" {
					w := call(s, "DELETE", bookMatchPath, cookie, s.Config.PublicOrigin, "")
					if w.Code != http.StatusNoContent {
						t.Fatalf("withdraw: %d %s", w.Code, w.Body)
					}
				}
				return out, err
			})
			w := call(s, "POST", bookMatchPath, cookie, s.Config.PublicOrigin, bookMatchInput)
			if w.Code != http.StatusConflict {
				t.Fatalf("withdrawn confirmation returned %d: %s", w.Code, w.Body)
			}
			var active int
			if err := s.Store.DB.QueryRow(`SELECT COUNT(*) FROM book_request_matches WHERE active=1`).Scan(&active); err != nil || active != 0 {
				t.Fatalf("withdrawn match restored: %d %v", active, err)
			}
		})
	}
}

func TestBookTrackingBatchBoundsTrafficAndSkipsFreshMatches(t *testing.T) {
	s, cookie := bookTrackingFixture(t)
	confirmBookFixture(t, s, cookie)
	ctx := context.Background()
	u, _ := s.Store.User(ctx, "11111")
	shelf, _ := s.currentShelfmarkGrant(u.ID)
	books, _ := s.currentBookGrant(u.ID)
	matches, err := s.Store.BookMatches(ctx, u.ID, s.bookTrackingScope(u, shelf, books))
	if err != nil || len(matches) != 1 {
		t.Fatal(matches, err)
	}
	for i := 2; i <= 25; i++ {
		m := matches[0]
		m.RequestID = fmt.Sprint(i)
		m.Revision = token()
		if _, err := s.Store.ConfirmBookMatch(ctx, m, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Store.DB.Exec(`UPDATE book_request_matches SET checked_at=?`, time.Now().Add(-2*time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	requests, catalogs := 0, 0
	original := s.Upstream.HTTP.Transport
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/requests" {
			requests++
		}
		if r.URL.Host == "grimmory.test" {
			catalogs++
		}
		return original.RoundTrip(r)
	})
	for _, want := range []int{20, 25, 25} {
		if err := s.refreshBookTrackingBatch(ctx, u); err != nil {
			t.Fatal(err)
		}
		if requests != want || catalogs != want {
			t.Fatalf("unexpected source traffic: requests=%d catalogs=%d want=%d", requests, catalogs, want)
		}
	}
}
