package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssistantFiltersAndVerifiesPlayback(t *testing.T) {
	server := recentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/library/metadata/") {
			if r.URL.Path != "/library/metadata/1" {
				t.Error("watched/long/wrong genre item selected")
			}
			discoveryPlayableResponse(w, r, "movie")
			return
		}
		items := recentMovies(5)
		for _, m := range items {
			m["duration"] = 90 * 60000
			m["Genre"] = []map[string]string{{"tag": "Science-fiction"}}
			m["summary"] = "A test synopsis"
		}
		items[1]["duration"] = 120 * 60000
		items[2]["viewCount"] = 1
		items[3]["Genre"] = []map[string]string{{"tag": "Comédie"}}
		delete(items[4], "duration")
		discoveryPage(w, 0, len(items), items)
	})
	items, err := New().AssistantMedia(context.Background(), server.URL, "server-a", Grant{ServerToken: "user-a"}, MediaFilter{Kind: "movie", Genre: "sci-fi", MaxMinutes: 120, Unwatched: true})
	if err != nil || len(items) != 1 || items[0].Minutes != 90 || items[0].Summary != "A test synopsis" {
		t.Fatalf("bad filters: %+v %v", items, err)
	}
}

func TestRequestsUseVerifiedMemberAndNeverOverridePermissions(t *testing.T) {
	for _, state := range []string{"created", "wrong identity", "denied", "conflict", "ambiguous", "wrong owner", "redirect"} {
		t.Run(state, func(t *testing.T) {
			posts := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Api-Key") != "secret" || r.Header.Get("X-Api-User") != "2" {
					t.Error("wrong impersonation")
				}
				if r.URL.Path == "/api/v1/auth/me" {
					if state == "wrong identity" {
						_, _ = w.Write([]byte(`{"id":1}`))
					} else {
						_, _ = w.Write([]byte(`{"id":2}`))
					}
					return
				}
				if r.Method != "POST" || r.URL.Path != "/api/v1/request" {
					t.Error("unexpected endpoint")
				}
				posts++
				var data map[string]any
				_ = json.NewDecoder(r.Body).Decode(&data)
				if len(data) != 4 || data["mediaType"] != "tv" || data["mediaId"] != float64(123) || data["is4k"] != false {
					t.Errorf("unsafe body: %v", data)
				}
				switch state {
				case "denied":
					w.WriteHeader(403)
				case "conflict":
					w.WriteHeader(409)
				case "ambiguous":
					w.WriteHeader(500)
				case "redirect":
					w.Header().Set("Location", "https://must-not-follow.test")
					w.WriteHeader(307)
				default:
					w.WriteHeader(201)
					owner := "2"
					if state == "wrong owner" {
						owner = "1"
					}
					_, _ = w.Write([]byte(`{"id":9,"type":"tv","requestedBy":{"id":` + owner + `},"media":{"tmdbId":123}}`))
				}
			}))
			defer s.Close()
			id, err := New().CreateMediaRequest(context.Background(), s.URL, "secret", 2, RequestCandidate{ID: 123, Kind: "tv"}, []int{2})
			if state == "created" {
				if err != nil || id != 9 {
					t.Fatal(id, err)
				}
			} else if err == nil {
				t.Fatal("false success")
			}
			if state == "wrong identity" && posts != 0 {
				t.Fatal("wrote with admin identity")
			}
			if posts > 1 {
				t.Fatal("POST retried")
			}
			if state == "ambiguous" && !errors.Is(err, ErrRequestUncertain) {
				t.Fatal("lost uncertainty")
			}
		})
	}
}

func TestRequestSearchDiscardsOtherPeoplesRequests(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-User") != "2" {
			t.Fatal("missing member scope")
		}
		if r.URL.Path == "/api/v1/auth/me" {
			_, _ = w.Write([]byte(`{"id":2}`))
			return
		}
		if r.URL.Query().Get("query") != "Arrival" {
			t.Error("wrong search")
		}
		_, _ = w.Write([]byte(`{"results":[{"id":123,"mediaType":"movie","title":"Arrival","releaseDate":"2016-01-01","mediaInfo":{"requests":[{"requestedBy":{"username":"private-name"}}]}},{"id":222,"mediaType":"person","name":"Person"}]}`))
	}))
	defer s.Close()
	items, err := New().SearchRequestCandidates(context.Background(), s.URL, "secret", 2, "Arrival", "movie")
	if err != nil || len(items) != 1 || items[0].Year != "2016" {
		t.Fatal(items, err)
	}
	raw, _ := json.Marshal(items)
	if strings.Contains(string(raw), "private-name") {
		t.Fatal("requester leaked")
	}
}

func TestRequestSearchPercentEncodesSpacesAndPreservesReservedCharacters(t *testing.T) {
	for _, title := range []string{"Hot Fuzz", "Mission: Impossible", "C++ & Friends", "L'été à Paris"} {
		t.Run(title, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.Header.Get("X-Api-User") != "2" {
					t.Error("wrong member or method")
				}
				if r.URL.Path == "/api/v1/auth/me" {
					_, _ = w.Write([]byte(`{"id":2}`))
					return
				}
				if strings.Contains(r.URL.RawQuery, "+") {
					http.Error(w, "query must be URL encoded", 400)
					return
				}
				if r.URL.Query().Get("query") != title {
					t.Error("title changed during encoding")
				}
				_, _ = w.Write([]byte(`{"results":[{"id":4638,"mediaType":"movie","title":"Hot Fuzz","releaseDate":"2007-02-14"}]}`))
			}))
			defer s.Close()
			items, err := New().SearchRequestCandidates(context.Background(), s.URL, "secret", 2, title, "movie")
			if err != nil || len(items) != 1 {
				t.Fatalf("search rejected title %q: %v", title, err)
			}
		})
	}
}
