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

func TestTautulliHistoryContract(t *testing.T) {
	for _, mode := range []string{"paged", "empty", "foreign user", "duplicate", "missing page", "changed total", "too large", "disabled history", "missing user", "inactive user", "duplicate user", "wrong server", "new version", "error envelope", "no completion", "no live flag"} {
		t.Run(mode, func(t *testing.T) {
			pages := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/v2" || r.Header.Get("X-Api-Key") != "private-key" || strings.Contains(r.URL.String(), "private-key") || r.URL.Query().Has("apikey") {
					t.Errorf("unsafe API request: %s", r.URL.Path)
				}
				var data any
				switch r.URL.Query().Get("cmd") {
				case "get_tautulli_info":
					version := "v2.18.2"
					if mode == "new version" {
						version = "v2.19.0"
					}
					data = map[string]any{"tautulli_version": version}
				case "get_server_info":
					id := "server-a"
					if mode == "wrong server" {
						id = "server-b"
					}
					data = map[string]any{"pms_identifier": id}
				case "get_users":
					keep := 1
					if mode == "disabled history" {
						keep = 0
					}
					active := 1
					if mode == "inactive user" {
						active = 0
					}
					user := map[string]any{"user_id": 11111, "row_id": 99, "is_active": active, "keep_history": keep}
					data = []any{map[string]any{"user_id": 0}, user}
					if mode == "missing user" {
						data = []any{}
					}
					if mode == "duplicate user" {
						data = []any{user, user}
					}
				case "get_history":
					pages++
					q := r.URL.Query()
					for k, v := range map[string]string{"user_id": "11111", "grouping": "0", "include_activity": "0", "length": "100", "order_column": "date", "order_dir": "asc"} {
						if q.Get(k) != v {
							t.Errorf("missing history restriction %s", k)
						}
					}
					start, _ := strconv.Atoi(q.Get("start"))
					total := 101
					if mode == "empty" {
						total = 0
					}
					if mode == "too large" {
						total = 5001
					}
					if mode == "changed total" && start > 0 {
						total = 102
					}
					rows := []map[string]any{}
					for n := start; n < min(start+100, total); n++ {
						user := 11111
						if mode == "foreign user" {
							user = 22222
						}
						rowID := n + 1
						if mode == "duplicate" && start > 0 {
							rowID = 1
						}
						row := map[string]any{"row_id": rowID, "user_id": user, "rating_key": "42", "parent_rating_key": "", "grandparent_rating_key": nil, "media_type": "movie", "percent_complete": 95, "watched_status": 0.75, "live": 0, "machine_id": "player-not-server"}
						if n == 100 {
							row["watched_status"] = 1
							row["percent_complete"] = 80
							row["rating_key"] = 43
							row["media_type"] = "episode"
							row["parent_rating_key"] = "44"
							row["grandparent_rating_key"] = "45"
						}
						if mode == "no completion" {
							delete(row, "watched_status")
						}
						if mode == "no live flag" {
							delete(row, "live")
						}
						if n == 1 {
							row["percent_complete"] = 101 // Tautulli does not cap this ratio.
						}
						rows = append(rows, row)
					}
					if mode == "missing page" && start > 0 {
						rows = []map[string]any{}
					}
					data = map[string]any{"recordsFiltered": total, "recordsTotal": 99999, "data": rows}
				default:
					t.Errorf("unexpected command: %s", r.URL.Query().Get("cmd"))
				}
				result := "success"
				if mode == "error envelope" {
					result = "error"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"result": result, "message": "do not expose this private message", "data": data}})
			}))
			defer server.Close()
			history, err := New().TautulliHistory(context.Background(), server.URL, "private-key", "server-a", "11111")
			if mode == "paged" {
				if err != nil || len(history.Items) != 101 || pages != 2 || history.UpdatedAt.IsZero() {
					t.Fatalf("pagination failed: %v rows=%d pages=%d", err, len(history.Items), pages)
				}
				if history.Items[0].Completed || history.Items[1].Percent != 101 || history.Items[1].Completed || !history.Items[100].Completed || history.Items[100].ParentID != "44" || history.Items[100].GrandparentID != "45" {
					t.Fatal("completion must follow Tautulli markers/threshold, not a hardcoded percentage")
				}
			} else if mode == "empty" {
				if err != nil || history.Items == nil || len(history.Items) != 0 || pages != 1 {
					t.Fatal("empty recorded history must stay empty, not create unwatched rows")
				}
			} else {
				if err == nil || len(history.Items) != 0 || strings.Contains(fmt.Sprint(err), "private") {
					t.Fatalf("unsafe failure: %v %+v", err, history)
				}
				if (mode == "foreign user" || mode == "wrong server") && !errors.Is(err, ErrDenied) {
					t.Fatalf("wrong identity not denied: %v", err)
				}
				if (mode == "wrong server" || mode == "new version" || mode == "disabled history" || mode == "missing user" || mode == "inactive user" || mode == "duplicate user") && pages != 0 {
					t.Fatal("history accessed before source/identity verification")
				}
			}
		})
	}
}
