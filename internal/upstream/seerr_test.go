package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestRequestArtworkAndRequesterUseValidatedUpstreamFields(t *testing.T) {
	for _, tc := range []struct{ name, path, want string }{
		{"TMDB image", "/abc_def-12.jpg", "https://image.tmdb.org/t/p/w342/abc_def-12.jpg"},
		{"missing", "", ""},
		{"absolute URL", "https://foreign.example/poster.jpg", ""},
		{"query credential", "/poster.jpg?token=private", ""},
		{"parent path", "/../poster.jpg", ""},
		{"SVG", "/poster.svg", ""},
		{"oversized path", "/" + strings.Repeat("a", 201) + ".jpg", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Api-Key") != "fixture-key" || r.URL.Query().Has("apikey") {
					t.Error("unsafe request credentials")
				}
				switch r.URL.Path {
				case "/api/v1/user/42/requests":
					fmt.Fprint(w, `{"pageInfo":{"results":1},"results":[{"id":1,"type":"movie","status":2,"requestedBy":{"id":42,"displayName":" Alice ","username":"secondary","plexUsername":"tertiary"},"media":{"tmdbId":100,"status":5}}]}`)
				case "/api/v1/movie/100":
					_ = json.NewEncoder(w).Encode(map[string]string{"title": "Fixture film", "posterPath": tc.path})
				default:
					t.Error("unexpected request path")
					w.WriteHeader(404)
				}
			}))
			defer source.Close()
			page, err := New().Requests(t.Context(), source.URL, "fixture-key", 42, 0)
			if err != nil || len(page.Items) != 1 || page.Items[0].PosterURL != tc.want || page.Items[0].RequesterName != "Alice" {
				t.Fatalf("request artwork/name: %+v %v", page, err)
			}
		})
	}
}

func TestRequestVariantPreservesMissingStandardAnd4K(t *testing.T) {
	for _, source := range []string{"", `,"is4k":null`, `,"is4k":false`, `,"is4k":true`} {
		t.Run(source, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/user/42/requests" {
					fmt.Fprintf(w, `{"pageInfo":{"results":1},"results":[{"id":1,"type":"movie","status":2,"requestedBy":{"id":42},"media":{"tmdbId":100,"status":3,"status4k":5}%s}]}`, source)
					return
				}
				fmt.Fprint(w, `{"title":"Fixture film"}`)
			}))
			defer server.Close()
			status := "processing"
			if source == `,"is4k":true` {
				status = "available"
			}
			for _, filter := range []string{"all", status} {
				page, err := New().RequestsByStatus(t.Context(), server.URL, "key", 42, 0, filter)
				if err != nil || len(page.Items) != 1 || page.Items[0].Status != status {
					t.Fatal("request variant changed its status", page, err)
				}
				encoded, _ := json.Marshal(page.Items[0])
				var item map[string]any
				_ = json.Unmarshal(encoded, &item)
				value, exists := item["is4k"]
				wantPresent := strings.Contains(source, "true") || strings.Contains(source, "false")
				if exists != wantPresent || (exists && value != (source == `,"is4k":true`)) {
					t.Fatal("request variant omitted or invented", string(encoded))
				}
			}
		})
	}
}

func TestSeerrPlexIdentityLookup(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		id         int64
		err        error
	}{
		{"exact identity", `{"pageInfo":{"results":2},"results":[{"id":7,"plexId":123,"username":"someone else"},{"id":8,"plexId":456,"username":"same name"}]}`, 7, nil},
		{"local account", `{"pageInfo":{"results":1},"results":[{"id":8,"plexId":null,"username":"123"}]}`, 0, ErrSeerrUserNotFound},
		{"missing permissions", `{"pageInfo":{"results":1},"results":[{"id":7,"username":"123"}]}`, 0, ErrUnavailable},
		{"duplicate Plex identity", `{"pageInfo":{"results":2},"results":[{"id":7,"plexId":123},{"id":8,"plexId":123}]}`, 0, ErrDenied},
		{"no matching identity", `{"pageInfo":{"results":1},"results":[{"id":7,"plexId":456}]}`, 0, ErrSeerrUserNotFound},
		{"empty directory", `{"pageInfo":{"results":0},"results":[]}`, 0, ErrSeerrUserNotFound},
		{"truncated directory", `{"pageInfo":{"results":2},"results":[]}`, 0, ErrUnavailable},
		{"missing pagination", `{"results":[]}`, 0, ErrUnavailable},
		{"excessive directory", `{"pageInfo":{"results":5001},"results":[]}`, 0, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/user" || r.Method != "GET" || r.Header.Get("X-Api-Key") != "key" {
					t.Error("unexpected lookup")
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			id, err := New().SeerrUser(context.Background(), server.URL, "key", "123")
			if id != tc.id || !errors.Is(err, tc.err) {
				t.Fatalf("got %d %v; want %d %v", id, err, tc.id, tc.err)
			}
		})
	}
}
func TestSeerrLookupScansAllPages(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprint(duplicate), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Query().Get("skip") == "0" {
					fmt.Fprint(w, `{"pageInfo":{"results":2},"results":[{"id":7,"plexId":123}]}`)
					return
				}
				if r.URL.Query().Get("skip") != "1" {
					t.Error("pagination not advanced")
				}
				plex := 456
				if duplicate {
					plex = 123
				}
				fmt.Fprintf(w, `{"pageInfo":{"results":2},"results":[{"id":8,"plexId":%d}]}`, plex)
			}))
			defer server.Close()
			id, err := New().SeerrUser(context.Background(), server.URL, "key", "123")
			if calls != 2 || (!duplicate && (id != 7 || err != nil)) || (duplicate && !errors.Is(err, ErrDenied)) {
				t.Fatalf("partial directory accepted: %d %v calls=%d", id, err, calls)
			}
		})
	}
}

func TestRequestDatesAreUTCOrUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
	}{
		{"UTC", `"2026-10-16T12:00:00Z"`, "2026-10-16T12:00:00Z"},
		{"offset with fractions", `"2026-10-16T01:30:00.123+02:00"`, "2026-10-15T23:30:00.123Z"},
		{"missing", "", ""},
		{"empty", `""`, ""},
		{"null", `null`, ""},
		{"no timezone", `"2026-10-16T12:00:00"`, ""},
		{"invalid date", `"2026-02-30T12:00:00Z"`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/user/42/requests" {
					field := ""
					if tc.source != "" {
						field = `,"createdAt":` + tc.source
					}
					fmt.Fprintf(w, `{"pageInfo":{"results":1},"results":[{"id":1,"type":"movie","status":999,"requestedBy":{"id":42},"media":{"tmdbId":100,"status":999}%s}]}`, field)
					return
				}
				if r.URL.Path != "/api/v1/movie/100" {
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()
			page, err := New().Requests(t.Context(), server.URL, "fixture-key", 42, 0)
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("request lost on incomplete upstream data: %+v %v", page, err)
			}
			item := page.Items[0]
			if item.CreatedAt != tc.want || item.Status != "unknown" || item.Title != "Film #100" || item.Seasons == nil {
				t.Fatalf("incorrect fallback or UTC date: %+v", item)
			}
		})
	}
}

func TestRequestStatusKeepsFailureAndDeclineDistinctFromMedia(t *testing.T) {
	for _, tc := range []struct {
		request, media int
		want           string
	}{
		{1, 2, "pending"}, {2, 2, "approved"}, {2, 3, "processing"},
		{2, 4, "partial"}, {5, 5, "available"}, {3, 5, "declined"},
		{4, 3, "error"}, {4, 5, "error"}, {5, 1, "unknown"}, {999, 999, "unknown"},
	} {
		t.Run(fmt.Sprintf("%d/%d", tc.request, tc.media), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/user/42/requests" {
					fmt.Fprintf(w, `{"pageInfo":{"results":1},"results":[{"id":1,"type":"movie","status":%d,"requestedBy":{"id":42},"media":{"tmdbId":100,"status":%d}}]}`, tc.request, tc.media)
					return
				}
				fmt.Fprint(w, `{"title":"A film"}`)
			}))
			defer server.Close()
			page, err := New().Requests(t.Context(), server.URL, "fixture-key", 42, 0)
			if err != nil || len(page.Items) != 1 || page.Items[0].Status != tc.want {
				t.Fatalf("status: %+v %v", page, err)
			}
		})
	}
}

func TestRequest4KStatusDoesNotUseHDAvailability(t *testing.T) {
	for _, tc := range []struct {
		status4k int
		want     string
	}{{0, "approved"}, {2, "approved"}, {3, "processing"}, {4, "partial"}, {5, "available"}} {
		t.Run(fmt.Sprint(tc.status4k), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/user/42/requests" {
					fmt.Fprintf(w, `{"pageInfo":{"results":1},"results":[{"id":1,"type":"movie","status":2,"is4k":true,"requestedBy":{"id":42},"media":{"tmdbId":100,"status":5,"status4k":%d}}]}`, tc.status4k)
					return
				}
				fmt.Fprint(w, `{"title":"A film"}`)
			}))
			defer server.Close()
			page, err := New().Requests(t.Context(), server.URL, "fixture-key", 42, 0)
			if err != nil || len(page.Items) != 1 || page.Items[0].Status != tc.want {
				t.Fatalf("HD availability used for 4K: %+v %v", page, err)
			}
		})
	}
}

func TestRequestPageReadBoundRejectsOversizedUpstreamBeforeEnrichment(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"pageInfo":{"results":11},"results":[`)
		for i := 1; i <= 11; i++ {
			if i > 1 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"id":%d,"type":"movie","status":2,"requestedBy":{"id":42},"media":{"tmdbId":100,"status":5}}`, i)
		}
		fmt.Fprint(w, "]}")
	}))
	defer server.Close()
	page, err := New().Requests(t.Context(), server.URL, "fixture-key", 42, 0)
	if !errors.Is(err, ErrUnavailable) || len(page.Items) != 0 || calls != 1 {
		t.Fatalf("unbounded page accepted: %+v %v calls=%d", page, err, calls)
	}
}

func TestRequestsByStatusFiltersBeforePaginatingAndEnrichesOnlySelectedPage(t *testing.T) {
	for _, offset := range []int{0, 10, 20, 10000} {
		t.Run(strconv.Itoa(offset), func(t *testing.T) {
			listReads, detailReads := 0, 0
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/user/42/requests" {
					listReads++
					if r.URL.Query().Get("take") != "100" || r.Header.Get("X-Api-Key") != "fixture-key" {
						t.Error("incorrect personal list request")
					}
					skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
					if skip != 0 && skip != 100 {
						t.Errorf("unexpected source offset %d", skip)
					}
					fmt.Fprint(w, `{"pageInfo":{"results":121},"results":[`)
					for id := skip + 1; id <= min(skip+100, 121); id++ {
						if id > skip+1 {
							fmt.Fprint(w, ",")
						}
						media := 2
						if id > 100 && id <= 120 {
							media = 5
						}
						fmt.Fprintf(w, `{"id":%d,"type":"movie","status":2,"requestedBy":{"id":42},"media":{"tmdbId":%d,"status":%d}}`, id, id, media)
					}
					fmt.Fprint(w, "]}")
					return
				}
				detailReads++
				id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/v1/movie/"))
				if listReads != 2 || id < 101+offset || id > 110+offset {
					t.Errorf("enriched outside validated selection: %s after %d list reads", r.URL.Path, listReads)
				}
				fmt.Fprintf(w, `{"title":"Film %d"}`, id)
			}))
			defer source.Close()
			page, err := New().RequestsByStatus(t.Context(), source.URL, "fixture-key", 42, offset, "available")
			want := 10
			if offset >= 20 {
				want = 0
			}
			if err != nil || page.Total != 20 || page.Offset != offset || page.Limit != 10 || page.Items == nil || len(page.Items) != want || listReads != 2 || detailReads != want {
				t.Fatalf("filtered pagination: %+v %v list=%d detail=%d", page, err, listReads, detailReads)
			}
			for i, item := range page.Items {
				if item.ID != int64(101+offset+i) || item.Status != "available" || item.Title != fmt.Sprintf("Film %d", item.ID) {
					t.Fatalf("wrong filtered item: %+v", item)
				}
			}
		})
	}
}

func TestRequestsByStatusUsesExistingNormalizedStatuses(t *testing.T) {
	rows := []struct {
		request, media, media4k int
		is4k                    bool
		want                    string
	}{
		{1, 2, 0, false, "pending"}, {2, 2, 0, false, "approved"},
		{2, 3, 0, false, "processing"}, {2, 4, 0, false, "partial"},
		{5, 5, 0, false, "available"}, {3, 5, 0, false, "declined"},
		{4, 5, 0, false, "error"}, {5, 1, 0, false, "unknown"},
		{2, 5, 3, true, "processing"},
	}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user/42/requests" {
			fmt.Fprint(w, `{"title":"Film"}`)
			return
		}
		fmt.Fprintf(w, `{"pageInfo":{"results":%d},"results":[`, len(rows))
		for i, row := range rows {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"id":%d,"type":"movie","status":%d,"is4k":%t,"requestedBy":{"id":42},"media":{"tmdbId":%d,"status":%d,"status4k":%d}}`, i+1, row.request, row.is4k, i+1, row.media, row.media4k)
		}
		fmt.Fprint(w, "]}")
	}))
	defer source.Close()
	for _, status := range []string{"pending", "approved", "processing", "partial", "available", "declined", "error", "unknown", "", "all"} {
		t.Run(status, func(t *testing.T) {
			page, err := New().RequestsByStatus(t.Context(), source.URL, "key", 42, 0, status)
			want := []int64{}
			for i, row := range rows {
				if status == "" || status == "all" || row.want == status {
					want = append(want, int64(i+1))
				}
			}
			if err != nil || page.Total != len(want) || len(page.Items) != len(want) {
				t.Fatalf("normalized filter %s: %+v %v", status, page, err)
			}
			for i, item := range page.Items {
				if item.ID != want[i] {
					t.Fatalf("unexpected request: %+v", item)
				}
			}
		})
	}
}

func TestRequestsByStatusRejectsIncompleteOrForeignListsBeforeEnrichment(t *testing.T) {
	row := `{"id":1,"type":"movie","status":2,"requestedBy":{"id":42},"media":{"tmdbId":100,"status":5}}`
	page := func(total int, rows string) string {
		return fmt.Sprintf(`{"pageInfo":{"results":%d},"results":[%s]}`, total, rows)
	}
	for _, tc := range []struct {
		name   string
		bodies []string
		want   error
	}{
		{"missing total", []string{`{"results":[]}`}, ErrIncomplete},
		{"negative total", []string{page(-1, "")}, ErrIncomplete},
		{"over bound", []string{page(1001, "")}, ErrIncomplete},
		{"missing last page", []string{page(2, row), page(2, "")}, ErrIncomplete},
		{"changed total", []string{page(2, row), page(3, strings.Replace(row, `"id":1`, `"id":2`, 1))}, ErrIncomplete},
		{"duplicate page", []string{page(2, row), page(2, row)}, ErrIncomplete},
		{"duplicate row", []string{page(2, row+","+row)}, ErrIncomplete},
		{"missing id", []string{page(1, strings.Replace(row, `"id":1,`, "", 1))}, ErrIncomplete},
		{"exceeds total", []string{page(0, row)}, ErrIncomplete},
		{"oversized page", []string{page(101, strings.TrimSuffix(strings.Repeat(row+",", 101), ","))}, ErrIncomplete},
		{"foreign nonmatch", []string{page(1, strings.ReplaceAll(strings.Replace(row, `"status":5`, `"status":2`, 1), `"id":42`, `"id":99`))}, ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/user/42/requests" || calls >= len(tc.bodies) {
					t.Errorf("unexpected enrichment or extra scan: %s", r.URL.Path)
					w.WriteHeader(500)
					return
				}
				fmt.Fprint(w, tc.bodies[calls])
				calls++
			}))
			defer source.Close()
			result, err := New().RequestsByStatus(t.Context(), source.URL, "key", 42, 0, "available")
			if !errors.Is(err, tc.want) || len(result.Items) != 0 || result.Total != 0 || calls != len(tc.bodies) {
				t.Fatalf("partial/foreign data exposed: %+v %v calls=%d", result, err, calls)
			}
		})
	}
}

func TestRequestsByStatusBoundsSourceCalls(t *testing.T) {
	calls := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls > 10 || r.URL.Path != "/api/v1/user/42/requests" {
			t.Error("unbounded scan or premature enrichment")
		}
		fmt.Fprintf(w, `{"pageInfo":{"results":11},"results":[{"id":%d,"type":"movie","status":2,"requestedBy":{"id":42},"media":{"tmdbId":100,"status":5}}]}`, calls)
	}))
	defer source.Close()
	page, err := New().RequestsByStatus(t.Context(), source.URL, "key", 42, 0, "available")
	if !errors.Is(err, ErrIncomplete) || len(page.Items) != 0 || calls != 10 {
		t.Fatalf("scan bound: %+v %v calls=%d", page, err, calls)
	}
}

func TestRequestsByStatusCancellationDoesNotReturnPartialMatches(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			fmt.Fprint(w, `{"pageInfo":{"results":2},"results":[{"id":1,"type":"movie","status":2,"requestedBy":{"id":42},"media":{"tmdbId":100,"status":5}}]}`)
			return
		}
		if calls != 2 || r.URL.Path != "/api/v1/user/42/requests" {
			t.Error("unexpected enrichment or scan after cancellation")
		}
		cancel()
		<-r.Context().Done()
	}))
	defer source.Close()
	page, err := New().RequestsByStatus(ctx, source.URL, "key", 42, 0, "available")
	if !errors.Is(err, ErrUnavailable) || len(page.Items) != 0 || page.Total != 0 || calls != 2 {
		t.Fatalf("cancelled scan returned partial matches: %+v %v calls=%d", page, err, calls)
	}
}
