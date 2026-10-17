package upstream

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSeerrChannelRequestersUseStableIDsAndCompletePages(t *testing.T) {
	for _, mode := range []string{"normal", "4k", "legacy", "invalid Discord ID", "wrong user", "incomplete list", "declined"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.Header.Get("X-Api-Key") != "fixture-key" {
					t.Fatal("unsafe Seerr request")
				}
				switch r.URL.Path {
				case "/api/v1/request":
					if mode == "incomplete list" {
						fmt.Fprint(w, `{"pageInfo":{"results":2},"results":[]}`)
						return
					}
					status := 2
					if mode == "declined" {
						status = 3
					}
					fmt.Fprintf(w, `{"pageInfo":{"results":1},"results":[{"id":1,"type":"tv","status":%d,"is4k":%v,"requestedBy":{"id":7},"media":{"tmdbId":8,"ratingKey":"42","ratingKey4k":"43"},"seasons":[{"seasonNumber":2}]}]}`, status, mode == "4k")
				case "/api/v1/user/7":
					id := 7
					if mode == "wrong user" {
						id = 8
					}
					fmt.Fprintf(w, `{"id":%d,"plexId":777,"displayName":"duplicate pseudo"}`, id)
				case "/api/v1/user/7/settings/notifications":
					if mode == "legacy" {
						fmt.Fprint(w, `{"discordId":"123"}`)
					} else if mode == "invalid Discord ID" {
						fmt.Fprint(w, `{"discordIds":["@everyone"]}`)
					} else {
						fmt.Fprint(w, `{"discordIds":["123","123","456"]}`)
					}
				default:
					t.Errorf("unexpected %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			client := New()
			requests, err := client.SeerrRequestedMedia(context.Background(), server.URL, "fixture-key")
			if mode == "incomplete list" {
				if err == nil {
					t.Fatal("incomplete list accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "declined" {
				if len(requests) != 0 {
					t.Fatal("declined request included")
				}
				return
			}
			wantKey := "42"
			if mode == "4k" {
				wantKey = "43"
			}
			if len(requests) != 1 || len(requests[0].PlexKeys) != 1 || requests[0].PlexKeys[0] != wantKey || requests[0].Seasons[0] != 2 {
				t.Fatalf("lost request scope: %+v", requests)
			}
			owner, err := client.SeerrRequester(context.Background(), server.URL, "fixture-key", requests[0].UserID)
			if mode == "invalid Discord ID" || mode == "wrong user" {
				if err == nil {
					t.Fatal("invalid recipient accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if mode == "legacy" {
				want = 1
			}
			if owner.PlexID != "777" || len(owner.DiscordIDs) != want || owner.DiscordIDs[0] != "123" {
				t.Fatalf("wrong exact identity: %+v", owner)
			}
		})
	}
}

func TestTautulliRemovalParentChecksSeasonAndLibrary(t *testing.T) {
	for _, mode := range []string{"season", "episode", "missing season parent", "wrong library", "wrong item"} {
		t.Run(mode, func(t *testing.T) {
			kind := "season"
			if mode == "episode" || mode == "missing season parent" {
				kind = "episode"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Api-Key") != "fixture" || strings.Contains(r.URL.RawQuery, "apikey") {
					t.Fatal("unsafe credentials")
				}
				data := ""
				switch r.URL.Query().Get("cmd") {
				case "get_tautulli_info":
					data = `{"tautulli_version":"v2.18.2"}`
				case "get_server_info":
					data = `{"pms_identifier":"server-a"}`
				case "get_metadata":
					id := "42"
					lib := "2"
					if mode == "wrong item" {
						id = "99"
					}
					if mode == "wrong library" {
						lib = "3"
					}
					data = fmt.Sprintf(`{"rating_key":%q,"media_type":%q,"section_id":%q,"parent_rating_key":"40","grandparent_rating_key":"38","media_index":"2","parent_media_index":"3"}`, id, kind, lib)
					if mode == "missing season parent" {
						data = strings.Replace(data, `"parent_rating_key":"40"`, `"parent_rating_key":""`, 1)
					}
				default:
					t.Fatal("unexpected query")
				}
				fmt.Fprint(w, `{"response":{"result":"success","data":`+data+`}}`)
			}))
			defer server.Close()
			metadata, season, err := New().TautulliRemovalParent(context.Background(), server.URL, "fixture", "server-a", Removal{MediaID: "42", Kind: kind, LibraryID: "2"})
			if mode == "wrong library" || mode == "wrong item" || mode == "missing season parent" {
				if err == nil {
					t.Fatal("unverified metadata accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if mode == "episode" {
				want = 3
			}
			if metadata.ID != "42" || metadata.ParentID != "40" || season == nil || *season != want {
				t.Fatal("wrong parent", metadata, season)
			}
		})
	}
}
