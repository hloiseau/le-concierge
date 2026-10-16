package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func discoveryPage(w http.ResponseWriter, offset, total int, items []map[string]any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": map[string]any{"offset": offset, "size": len(items), "totalSize": total, "Metadata": items}})
}

func discoveryAccess(w http.ResponseWriter, r *http.Request) {
	items := []map[string]any{}
	for _, key := range strings.Split(strings.TrimPrefix(r.URL.Path, "/library/metadata/"), ",") {
		items = append(items, map[string]any{"ratingKey": key})
	}
	discoveryPage(w, 0, len(items), items)
}

func discoveryPlayableResponse(w http.ResponseWriter, r *http.Request, kind string) {
	id := strings.TrimPrefix(r.URL.Path, "/library/metadata/")
	item := map[string]any{"ratingKey": id, "type": kind, "Media": []map[string]any{{"Part": []map[string]any{{"key": "/library/parts/1/file.mkv", "exists": true, "accessible": true}}}}}
	if strings.HasSuffix(id, "/grandchildren") {
		parent := strings.TrimSuffix(id, "/grandchildren")
		item["ratingKey"], item["type"], item["grandparentRatingKey"] = parent+"99", "episode", parent
		delete(item, "Media")
	} else if strings.HasSuffix(id, "99") {
		item["type"], item["grandparentRatingKey"] = "episode", strings.TrimSuffix(id, "99")
	}
	discoveryPage(w, 0, 1, []map[string]any{item})
}

func TestSurpriseUniformAcrossAllPagesAndDeduplicated(t *testing.T) {
	calls := 0
	server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/library/metadata/") {
			kind := "movie"
			if r.URL.Path == "/library/metadata/3" {
				kind = "show"
			}
			discoveryPlayableResponse(w, r, kind)
			return
		}
		calls++
		if r.URL.Path != "/library/all" || r.URL.Query().Get("type") != "1,2" || r.Header.Get("X-Plex-Container-Size") != "200" {
			t.Errorf("wrong complete catalogue query: %s", r.URL)
		}
		offset, _ := strconv.Atoi(r.Header.Get("X-Plex-Container-Start"))
		items := recentMovies(3)
		items[2]["type"], items[2]["leafCount"] = "show", 1
		// A capped first page and a repeated film must not favour films over shows.
		if offset == 0 {
			discoveryPage(w, 0, 4, items[:2])
		} else {
			discoveryPage(w, offset, 4, []map[string]any{items[1], items[2]})
		}
	})
	for pick := byte(0); pick < 3; pick++ {
		item, err := New().surprise(context.Background(), server.URL, "server-a", Grant{ServerToken: "user-a"}, "", bytes.NewReader([]byte{pick}))
		if err != nil || item.ID != fmt.Sprintf("plex:server-a:%d", pick+1) {
			t.Fatalf("catalogue position %d unreachable: %+v %v", pick, item, err)
		}
		if strings.Contains(item.URL, server.URL) || strings.Contains(item.URL, "user-a") {
			t.Fatal("credential or private host in public link")
		}
	}
	if calls != 6 {
		t.Fatalf("selection skipped full paging: %d calls", calls)
	}
}

func TestSurpriseFiltersEmptyCatalogueAndMissingMedia(t *testing.T) {
	for _, kind := range []string{"movie", "show"} {
		t.Run(kind, func(t *testing.T) {
			for _, state := range []string{"available", "deleted", "empty"} {
				t.Run(state, func(t *testing.T) {
					server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
						if strings.HasPrefix(r.URL.Path, "/library/metadata/") {
							discoveryPlayableResponse(w, r, kind)
							return
						}
						want := "1"
						if kind == "show" {
							want = "2"
						}
						if r.URL.Query().Get("type") != want {
							t.Error("filter not passed to Plex")
						}
						items := recentMovies(1)
						items[0]["type"], items[0]["leafCount"] = kind, 2
						if state == "deleted" {
							items[0]["deletedAt"] = 999
						}
						if state == "empty" {
							items = nil
						}
						discoveryPage(w, 0, len(items), items)
					})
					item, err := New().Surprise(context.Background(), server.URL, "server-a", Grant{ServerToken: "user-a"}, kind)
					if err != nil || ((item.ID != "") != (state == "available")) {
						t.Fatalf("bad selection %+v %v", item, err)
					}
				})
			}
		})
	}
}

func TestSurpriseRejectsPartialCataloguesAndBoundsReads(t *testing.T) {
	for _, state := range []string{"missing total", "changed total", "wrong offset", "oversized catalogue", "page bound", "wrong type"} {
		t.Run(state, func(t *testing.T) {
			calls := 0
			server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/library/all" {
					t.Fatal("partial catalogue used for selection")
				}
				offset, _ := strconv.Atoi(r.Header.Get("X-Plex-Container-Start"))
				items := recentMovies(1)
				items[0]["ratingKey"] = strconv.Itoa(offset + 1)
				total := 2
				switch state {
				case "missing total":
					fmt.Fprint(w, `{"MediaContainer":{"size":0}}`)
					return
				case "changed total":
					if calls == 2 {
						total = 3
					}
				case "wrong offset":
					offset++
				case "oversized catalogue":
					total = discoveryPageSize*discoveryMaxPages + 1
				case "page bound":
					total = 100
				case "wrong type":
					total = 1
					items[0]["type"] = "episode"
				}
				discoveryPage(w, offset, total, items)
			})
			item, err := New().Surprise(context.Background(), server.URL, "server-a", Grant{ServerToken: "user-a"}, "")
			if !errors.Is(err, ErrIncomplete) || item.ID != "" || calls > discoveryMaxPages {
				t.Fatalf("partial result returned %+v %v (%d calls)", item, err, calls)
			}
			if state == "page bound" && calls != discoveryMaxPages {
				t.Fatalf("wrong bound: %d", calls)
			}
		})
	}
}

func TestDiscoveryChecksServerBeforeTokenAndRechecksChosenItem(t *testing.T) {
	for _, state := range []string{"wrong server", "expired", "bad kind", "revoked", "outage", "isolated"} {
		t.Run(state, func(t *testing.T) {
			calls := 0
			server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if strings.HasPrefix(r.URL.Path, "/library/metadata/") {
					switch state {
					case "revoked":
						w.WriteHeader(403)
					case "outage":
						w.WriteHeader(503)
					default:
						discoveryPlayableResponse(w, r, "movie")
					}
					return
				}
				items := recentMovies(1)
				items[0]["title"] = r.Header.Get("X-Plex-Token")
				discoveryPage(w, 0, 1, items)
			})
			g := Grant{ServerToken: "user-a"}
			serverID, kind := "server-a", ""
			if state == "wrong server" {
				serverID = "different"
			}
			if state == "expired" {
				g.ExpiresAt = time.Now().Add(-time.Hour)
			}
			if state == "bad kind" {
				kind = "episode"
			}
			c := New()
			item, err := c.Surprise(context.Background(), server.URL, serverID, g, kind)
			if state == "isolated" {
				if err != nil || item.Title != "user-a" {
					t.Fatalf("wrong user result %+v %v", item, err)
				}
				g.ServerToken = "user-b"
				item, err = c.Surprise(context.Background(), server.URL, serverID, g, kind)
				if err != nil || item.Title != "user-b" {
					t.Fatalf("cross-user result %+v %v", item, err)
				}
				return
			}
			want := ErrDenied
			if state == "outage" {
				want = ErrUnavailable
			}
			if !errors.Is(err, want) || item.ID != "" {
				t.Fatalf("access failure exposed title %+v %v", item, err)
			}
			if (state == "wrong server" || state == "expired" || state == "bad kind") && calls != 0 {
				t.Fatal("credential sent before validation")
			}
		})
	}
}

func TestRecapPeriodUsesCompletedParisDaysAcrossDST(t *testing.T) {
	for _, tc := range []struct {
		now, start, end string
		hours           float64
	}{
		{"2026-03-30T12:00:00Z", "2026-03-23T00:00:00+01:00", "2026-03-30T00:00:00+02:00", 167},
		{"2026-10-26T12:00:00Z", "2026-10-19T00:00:00+02:00", "2026-10-26T00:00:00+01:00", 169},
		{"2026-10-15T23:00:00Z", "2026-10-09T00:00:00+02:00", "2026-10-16T00:00:00+02:00", 168},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		start, end, err := recapPeriod(now)
		if err != nil || start.Format(time.RFC3339) != tc.start || end.Format(time.RFC3339) != tc.end || end.Sub(start).Hours() != tc.hours {
			t.Fatalf("incorrect Paris period %s - %s: %v", start, end, err)
		}
	}
}

func TestRecapFiltersBeforeGroupingAndDeduplicatesAcrossPages(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	start, end, _ := recapPeriod(now)
	episode := func(id string, added int64) map[string]any {
		return map[string]any{"ratingKey": id, "title": "Episode", "type": "episode", "addedAt": added, "grandparentRatingKey": "100", "grandparentTitle": "Series"}
	}
	items := []map[string]any{
		episode("1", end.Unix()), episode("2", end.Unix()-1), episode("2", end.Unix()-1), episode("3", start.Unix()), episode("4", start.Unix()-1),
		{"ratingKey": "5", "title": "Movie", "type": "movie", "addedAt": start.Unix()},
	}
	var checked []string
	server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/library/metadata/") {
			checked = append(checked, strings.Split(strings.TrimPrefix(r.URL.Path, "/library/metadata/"), ",")...)
			discoveryAccess(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("type") != "1,4" || q.Get("addedAt>>") != strconv.FormatInt(start.Unix()-1, 10) || q.Get("addedAt<<") != strconv.FormatInt(end.Unix(), 10) {
			t.Errorf("incorrect range query %s", r.URL)
		}
		offset, _ := strconv.Atoi(r.Header.Get("X-Plex-Container-Start"))
		discoveryPage(w, offset, len(items), items[offset:min(offset+2, len(items))])
	})
	result, err := New().Recap(context.Background(), server.URL, "server-a", Grant{ServerToken: "user-a"}, now)
	if err != nil || len(result.Items) != 2 || result.Items[0].ID != "plex:server-a:100" || result.Items[0].Title != "Series" || result.Items[0].Type != "show" || result.Items[0].AddedAt != end.Add(-time.Second).UTC().Format(time.RFC3339) {
		t.Fatalf("bad recap %+v %v", result, err)
	}
	if strings.Join(checked, ",") != "2,3,5" {
		t.Fatalf("wrong original item access checks %v", checked)
	}
}

func TestRecapDoesNotReturnPartialDataAfterRevocation(t *testing.T) {
	now := time.Now()
	start, _, _ := recapPeriod(now)
	server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/library/metadata/") {
			w.WriteHeader(403)
			return
		}
		items := recentMovies(1)
		items[0]["addedAt"] = start.Unix()
		discoveryPage(w, 0, 1, items)
	})
	result, err := New().Recap(context.Background(), server.URL, "server-a", Grant{ServerToken: "user-a"}, now)
	if !errors.Is(err, ErrDenied) || len(result.Items) != 0 {
		t.Fatalf("revoked recap leaked %+v %v", result, err)
	}
}

func TestSurpriseSkipsUnplayableCandidatesWithoutReplacement(t *testing.T) {
	for _, state := range []string{"missing file", "inaccessible file", "missing flags", "missing exists", "missing accessible", "missing key", "no parts", "no media", "removed", "empty show"} {
		t.Run(state, func(t *testing.T) {
			var checked []string
			server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/library/all" {
					items := recentMovies(2)
					if state == "empty show" {
						items[0]["type"] = "show"
						items[0]["leafCount"] = 1
					}
					discoveryPage(w, 0, 2, items)
					return
				}
				checked = append(checked, r.URL.Path)
				if !strings.HasSuffix(r.URL.Path, "/grandchildren") && (r.URL.Query().Get("checkFiles") != "1" || r.URL.Query().Get("checkFileAvailability") != "1") {
					t.Fatal("playback metadata lacks availability flags")
				}
				if r.URL.Path == "/library/metadata/2" {
					discoveryPlayableResponse(w, r, "movie")
					return
				}
				switch state {
				case "removed":
					w.WriteHeader(404)
				case "empty show":
					if r.URL.Path == "/library/metadata/1" {
						discoveryPlayableResponse(w, r, "show")
					} else {
						discoveryPage(w, 0, 0, nil)
					}
				case "no media":
					fmt.Fprint(w, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"1","type":"movie"}]}}`)
				case "no parts":
					fmt.Fprint(w, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"1","type":"movie","Media":[{}]}]}}`)
				default:
					part := map[string]any{"key": "/library/parts/1/file.mkv", "exists": true, "accessible": true}
					switch state {
					case "missing file":
						part["exists"] = false
					case "inaccessible file":
						part["accessible"] = false
					case "missing flags":
						delete(part, "exists")
						delete(part, "accessible")
					case "missing exists":
						delete(part, "exists")
					case "missing accessible":
						delete(part, "accessible")
					case "missing key":
						delete(part, "key")
					}
					discoveryPage(w, 0, 1, []map[string]any{{"ratingKey": "1", "type": "movie", "Media": []map[string]any{{"Part": []map[string]any{part}}}}})
				}
			})
			item, err := New().surprise(context.Background(), server.URL, "server-a", Grant{ServerToken: "user-a"}, "", bytes.NewReader([]byte{0, 0}))
			if err != nil || item.ID != "plex:server-a:2" || checked[0] != "/library/metadata/1" {
				t.Fatalf("unplayable candidate selected: %+v %v %v", item, err, checked)
			}
			if strings.Count(strings.Join(checked, ","), "/library/metadata/1,") > 1 {
				t.Fatal("candidate retried")
			}
		})
	}
}

func TestSurpriseBoundsPlaybackVerificationAndEmptyResult(t *testing.T) {
	for _, count := range []int{1, discoveryPlaybackReads + 1} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			reads := 0
			server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/library/all" {
					discoveryPage(w, 0, count, recentMovies(count))
					return
				}
				reads++
				id := strings.TrimPrefix(r.URL.Path, "/library/metadata/")
				fmt.Fprintf(w, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":%q,"type":"movie","Media":[]}]}}`, id)
			})
			item, err := New().surprise(context.Background(), server.URL, "server-a", Grant{ServerToken: "user-a"}, "", bytes.NewReader(make([]byte, count)))
			if item.ID != "" || reads != min(count, discoveryPlaybackReads) || (count == 1 && err != nil) || (count > discoveryPlaybackReads && !errors.Is(err, ErrIncomplete)) {
				t.Fatalf("wrong empty/bounded result %+v %v (%d reads)", item, err, reads)
			}
		})
	}
}

func TestSurpriseVerifiesEpisodesAcrossPages(t *testing.T) {
	reads := 0
	metadataReads := 0
	server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/library/all":
			items := recentMovies(1)
			items[0]["type"] = "show"
			discoveryPage(w, 0, 1, items)
		case "/library/metadata/1":
			discoveryPlayableResponse(w, r, "show")
		case "/library/metadata/1/grandchildren":
			reads++
			offset, _ := strconv.Atoi(r.Header.Get("X-Plex-Container-Start"))
			item := map[string]any{"ratingKey": strconv.Itoa(offset + 2), "type": "episode", "grandparentRatingKey": "1"}
			discoveryPage(w, offset, 2, []map[string]any{item})
		case "/library/metadata/2", "/library/metadata/3":
			metadataReads++
			if r.URL.Query().Get("checkFiles") != "1" || r.URL.Query().Get("checkFileAvailability") != "1" {
				t.Fatal("episode metadata lacks file checks")
			}
			id := strings.TrimPrefix(r.URL.Path, "/library/metadata/")
			item := map[string]any{"ratingKey": id, "type": "episode", "grandparentRatingKey": "1"}
			if id == "3" {
				item["Media"] = []map[string]any{{"Part": []map[string]any{{"key": "/library/parts/1/file.mkv", "exists": 1, "accessible": 1}}}}
			}
			discoveryPage(w, 0, 1, []map[string]any{item})
		default:
			t.Errorf("unexpected read %s", r.URL.Path)
		}
	})
	item, err := New().Surprise(context.Background(), server.URL, "server-a", Grant{ServerToken: "user-a"}, "show")
	if err != nil || item.ID != "plex:server-a:1" || reads != 2 || metadataReads != 2 {
		t.Fatalf("episode verification failed %+v %v (%d pages)", item, err, reads)
	}
}

func TestRecapExcludesExplicitlyMissingFiles(t *testing.T) {
	now := time.Now()
	start, _, _ := recapPeriod(now)
	server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/library/all" {
			items := recentMovies(2)
			for _, item := range items {
				item["addedAt"] = start.Unix()
			}
			items[0]["Media"] = []map[string]any{{"Part": []map[string]any{{"key": "/library/parts/1/file.mkv", "exists": false}}}}
			items[1]["Media"] = []map[string]any{{"Part": []map[string]any{{"key": "/library/parts/2/file.mkv"}}}}
			discoveryPage(w, 0, 2, items)
			return
		}
		if r.URL.Path != "/library/metadata/2" {
			t.Fatalf("known missing file included: %s", r.URL.Path)
		}
		discoveryAccess(w, r)
	})
	result, err := New().Recap(context.Background(), server.URL, "server-a", Grant{ServerToken: "user-a"}, now)
	if err != nil || len(result.Items) != 1 || result.Items[0].ID != "plex:server-a:2" {
		t.Fatalf("offline file in recap %+v %v", result, err)
	}
}
