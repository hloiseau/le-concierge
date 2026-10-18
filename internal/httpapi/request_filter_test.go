package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/upstream"
)

func TestRequestsRejectInvalidStatusBeforeSeerrRead(t *testing.T) {
	for _, query := range []string{"status=bogus", "status=Available", "status=available&status=available", "status=&status=error"} {
		t.Run(query, func(t *testing.T) {
			s := fixture(t)
			cookie := seed(t, s, "11111", true)
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				t.Errorf("invalid filter reached source: %s", r.URL.Path)
				return response(500, `{}`), nil
			})
			w := call(s, "GET", "/api/v1/requests?"+query, cookie, "", "")
			if w.Code != 400 || !strings.Contains(w.Body.String(), `"code":"invalid_status"`) {
				t.Fatalf("invalid filter accepted: %d %s", w.Code, w.Body)
			}
		})
	}
}

func TestRequestsStatusFilterUsesVerifiedOwnerAndFilteredOffset(t *testing.T) {
	for _, status := range []string{"available", "", "all"} {
		t.Run(status, func(t *testing.T) {
			s := fixture(t)
			cookie := seed(t, s, "11111", true)
			listReads := 0
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/api/v1/user":
					return response(200, `{"pageInfo":{"results":2},"results":[{"id":42,"plexId":11111},{"id":99,"plexId":22222}]}`), nil
				case "/api/v1/user/42/requests":
					listReads++
					if status == "available" {
						if r.URL.Query().Get("take") != "100" || r.URL.Query().Get("skip") != "0" {
							t.Errorf("filtered offset sent to source: %s", r.URL.RawQuery)
						}
					} else if r.URL.Query().Get("take") != "10" || r.URL.Query().Get("skip") != "10" {
						t.Errorf("unfiltered pagination changed: %s", r.URL.RawQuery)
					}
					var rows []string
					for id := 1; id <= 11; id++ {
						if status != "available" && id <= 10 {
							continue
						}
						rows = append(rows, `{"id":`+strconv.Itoa(id)+`,"type":"movie","status":2,"requestedBy":{"id":42},"media":{"tmdbId":`+strconv.Itoa(id)+`,"status":5}}`)
					}
					return response(200, `{"pageInfo":{"results":11},"results":[`+strings.Join(rows, ",")+`]}`), nil
				case "/api/v1/movie/11":
					return response(200, `{"title":"Selected film"}`), nil
				default:
					t.Errorf("wrong owner or detail accessed: %s", r.URL.Path)
					return response(500, `{}`), nil
				}
			})
			w := call(s, "GET", "/api/v1/requests?offset=10&status="+status+"&userId=99", cookie, "", "")
			var page upstream.RequestPage
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != 200 || page.Total != 11 || page.Offset != 10 || page.Limit != 10 || len(page.Items) != 1 || page.Items[0].ID != 11 || listReads != 1 {
				t.Fatalf("wrong filtered response: %d %s (err=%v, reads=%d)", w.Code, w.Body, err, listReads)
			}
		})
	}
}

func TestRequestsStatusFilterReportsIncompleteSourceAndRevalidatesSession(t *testing.T) {
	for _, scenario := range []string{"incomplete", "logout"} {
		t.Run(scenario, func(t *testing.T) {
			s := fixture(t)
			cookie := seed(t, s, "11111", true)
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/api/v1/user":
					return response(200, `{"pageInfo":{"results":1},"results":[{"id":42,"plexId":11111}]}`), nil
				case "/api/v1/user/42/requests":
					if scenario == "incomplete" {
						return response(200, `{"pageInfo":{"results":1001},"results":[]}`), nil
					}
					return response(200, `{"pageInfo":{"results":1},"results":[{"id":1,"type":"movie","status":2,"requestedBy":{"id":42},"media":{"tmdbId":100,"status":5}}]}`), nil
				case "/api/v1/movie/100":
					s.mu.Lock()
					delete(s.sessions, digest(cookie))
					s.mu.Unlock()
					return response(200, `{"title":"Private film"}`), nil
				default:
					t.Errorf("unexpected source read: %s", r.URL.Path)
					return response(500, `{}`), nil
				}
			})
			w := call(s, "GET", "/api/v1/requests?status=available", cookie, "", "")
			want := 502
			if scenario == "logout" {
				want = 401
			}
			if w.Code != want || strings.Contains(w.Body.String(), "Private film") || strings.Contains(w.Body.String(), `"items"`) || (scenario == "incomplete" && !strings.Contains(w.Body.String(), `"code":"seerr_incomplete"`)) {
				t.Fatalf("partial or revoked response exposed: %d %s", w.Code, w.Body)
			}
		})
	}
}
