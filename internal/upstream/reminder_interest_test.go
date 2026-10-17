package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestInterestsNeverUseTruncatedOrChangingOwnerLists(t *testing.T) {
	for _, mode := range []string{"complete", "changed total", "duplicate", "other owner", "empty next page"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				offset := r.URL.Query().Get("skip")
				total := 2
				rows := []any{}
				id, owner := 1, 9
				if offset == "1" {
					id = 2
					if mode == "changed total" {
						total = 3
					}
					if mode == "duplicate" {
						id = 1
					}
					if mode == "other owner" {
						owner = 10
					}
				}
				if offset != "1" || mode != "empty next page" {
					rows = append(rows, map[string]any{"id": id, "type": "tv", "status": 2, "requestedBy": map[string]int{"id": owner}, "media": map[string]int{"tmdbId": 77}, "seasons": []any{map[string]int{"seasonNumber": 1}}})
				}
				json.NewEncoder(w).Encode(map[string]any{"pageInfo": map[string]int{"results": total}, "results": rows})
			}))
			defer srv.Close()
			items, err := New().RequestInterests(context.Background(), srv.URL, "fixture-key", 9)
			if mode == "complete" {
				if err != nil || len(items) != 2 || items[0].Season == nil || *items[0].Season != 1 {
					t.Fatalf("complete: %+v %v", items, err)
				}
			} else if err == nil {
				t.Fatalf("accepted %s", mode)
			}
			if calls != 2 {
				t.Fatalf("unbounded or incomplete reads: %d", calls)
			}
		})
	}
}

func TestRequestInterestsKeepsRequestDateSeparateFromPlexCandidateKeys(t *testing.T) {
	created := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "fixture-key" || r.URL.Path != "/api/v1/user/9/requests" {
			t.Error("request owner route changed")
		}
		json.NewEncoder(w).Encode(map[string]any{"pageInfo": map[string]int{"results": 1}, "results": []any{map[string]any{"id": 7, "type": "movie", "status": 5, "createdAt": created, "requestedBy": map[string]int{"id": 9}, "media": map[string]any{"tmdbId": 22, "ratingKey": "84", "ratingKey4k": "85"}}}})
	}))
	defer srv.Close()
	rows, err := New().RequestInterests(context.Background(), srv.URL, "fixture-key", 9)
	if err != nil || len(rows) != 1 || rows[0].RequestID != 7 || !rows[0].RequestedAt.Equal(created) || len(rows[0].PlexKeys) != 1 || rows[0].PlexKeys[0] != "84" {
		t.Fatalf("source facts lost: %+v %v", rows, err)
	}
}

func TestRequestInterestsOnlyTargetsRequestedQuality(t *testing.T) {
	for _, tc := range []struct {
		name          string
		is4k          bool
		hd, uhd, want string
	}{
		{"HD request", false, "84", "85", "84"},
		{"4K request", true, "84", "85", "85"},
		{"4K not in Plex yet", true, "84", "", ""},
		{"HD not in Plex yet", false, "", "85", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"pageInfo": map[string]int{"results": 1}, "results": []any{map[string]any{"id": 7, "type": "movie", "status": 2, "is4k": tc.is4k, "requestedBy": map[string]int{"id": 9}, "media": map[string]any{"tmdbId": 22, "ratingKey": tc.hd, "ratingKey4k": tc.uhd}}}})
			}))
			defer srv.Close()
			rows, err := New().RequestInterests(t.Context(), srv.URL, "fixture-key", 9)
			if err != nil || len(rows) != 1 {
				t.Fatal("request lost", err)
			}
			keys := rows[0].PlexKeys
			if tc.want == "" {
				if len(keys) != 0 {
					t.Fatal("unrequested quality substituted", keys)
				}
			} else if len(keys) != 1 || keys[0] != tc.want {
				t.Fatal("wrong requested quality", keys)
			}
		})
	}
}
