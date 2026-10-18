package upstream

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTautulliRemovalEpisodeIndexMissingOrInvalid(t *testing.T) {
	for _, field := range []string{"", `,"media_index":null`, `,"media_index":""`, `,"media_index":"10001"`} {
		t.Run(field, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := ""
				switch r.URL.Query().Get("cmd") {
				case "get_tautulli_info":
					data = `{"tautulli_version":"v2.18.2"}`
				case "get_server_info":
					data = `{"pms_identifier":"server-a"}`
				case "get_metadata":
					data = `{"rating_key":"42","media_type":"episode","section_id":"2","parent_rating_key":"40","grandparent_rating_key":"38","parent_media_index":"3"` + field + `}`
				default:
					t.Fatal("unexpected Tautulli query")
				}
				fmt.Fprint(w, `{"response":{"result":"success","data":`+data+`}}`)
			}))
			defer server.Close()
			item, season, err := New().TautulliRemovalParent(context.Background(), server.URL, "fixture", "server-a", Removal{MediaID: "42", Kind: "episode", LibraryID: "2"})
			if field == `,"media_index":"10001"` {
				if err == nil {
					t.Fatal("out-of-range episode index accepted")
				}
				return
			}
			if err != nil || item.Index != nil || season == nil || *season != 3 {
				t.Fatal("missing episode index was invented or season was lost", item, season, err)
			}
		})
	}
}
