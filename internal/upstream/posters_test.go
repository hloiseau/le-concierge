package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func fixturePosterPNG(t *testing.T) []byte {
	t.Helper()
	var body bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.RGBA{R: 100, B: 200, A: 255})
	if err := png.Encode(&body, img); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func TestPlexPosterOnlyReadsAuthorizedImages(t *testing.T) {
	for _, mode := range []string{"png", "jpeg", "webp", "invalid id", "expired grant", "missing token", "wrong server", "metadata denied", "foreign item", "external URL", "query token", "missing thumb", "identity redirect", "metadata redirect", "image redirect", "HTML", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			imageReads, allReads := 0, 0
			body := fixturePosterPNG(t)
			if mode == "jpeg" {
				body = []byte("\xff\xd8\xff\xe0fixture")
			}
			if mode == "webp" {
				body = []byte("RIFF\x20\x00\x00\x00WEBPVP8 fixture")
			}
			if mode == "HTML" {
				body = []byte("<!DOCTYPE html><script>bad()</script>")
			}
			if mode == "oversize" {
				body = append(body, make([]byte, 2*1024*1024)...)
			}
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				allReads++
				if r.Method != "GET" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || strings.Contains(r.URL.String(), "personal") {
					t.Error("poster request exposed a credential or cookie")
				}
				switch r.URL.Path {
				case "/identity":
					if r.Header.Get("X-Plex-Token") != "" {
						t.Error("identity probe received private token")
					}
					if mode == "identity redirect" {
						http.Redirect(w, r, "/unexpected", 302)
						return
					}
					id := "server-a"
					if mode == "wrong server" {
						id = "server-b"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": map[string]string{"machineIdentifier": id}})
				case "/library/metadata/12":
					if r.Header.Get("X-Plex-Token") != "personal-server" || r.Header.Get("X-Plex-Client-Identifier") != "client" || r.URL.Query().Get("skipRefresh") != "1" {
						t.Error("metadata did not use personal read access")
					}
					if mode == "metadata denied" {
						w.WriteHeader(403)
						return
					}
					if mode == "metadata redirect" {
						http.Redirect(w, r, "/unexpected", 302)
						return
					}
					thumb := "/library/metadata/12/thumb/123"
					switch mode {
					case "foreign item":
						thumb = "/library/metadata/123/thumb/123"
					case "external URL":
						thumb = "https://foreign.example/poster.png"
					case "query token":
						thumb += "?X-Plex-Token=secret"
					case "missing thumb":
						thumb = ""
					}
					item := map[string]any{"ratingKey": "12", "librarySectionID": "1", "type": "movie", "title": "Fixture film", "guid": "plex://movie/fixture", "thumb": thumb}
					_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": map[string]any{"size": 1, "Metadata": []any{item}}})
				case "/library/metadata/12/thumb/123":
					imageReads++
					if r.Header.Get("X-Plex-Token") != "personal-server" || r.URL.RawQuery != "" {
						t.Error("poster did not use private token header")
					}
					if mode == "image redirect" {
						http.Redirect(w, r, "/unexpected", 302)
						return
					}
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(body)
				default:
					t.Errorf("followed unexpected path %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer source.Close()
			client := New()
			client.HTTP.Jar, _ = cookiejar.New(nil)
			base, _ := url.Parse(source.URL)
			client.HTTP.Jar.SetCookies(base, []*http.Cookie{{Name: "browser", Value: "private"}})
			grant := Grant{AccountToken: "personal-account", ServerToken: "personal-server", ClientID: "client", ExpiresAt: time.Now().Add(time.Hour)}
			id := "12"
			if mode == "invalid id" {
				id = "12/../13"
			}
			if mode == "expired grant" {
				grant.ExpiresAt = time.Now().Add(-time.Second)
			}
			if mode == "missing token" {
				grant.ServerToken = ""
			}
			got, mime, err := client.PlexPoster(context.Background(), source.URL, "server-a", id, grant)
			if mode == "png" || mode == "jpeg" || mode == "webp" {
				if err != nil || mime != "image/"+mode || !bytes.Equal(got, body) || imageReads != 1 {
					t.Fatalf("authorized poster: mime=%s error=%v reads=%d", mime, err, imageReads)
				}
			} else {
				if err == nil || len(got) != 0 || mime != "" {
					t.Fatal("invalid poster accepted")
				}
				if mode == "invalid id" || mode == "expired grant" || mode == "missing token" {
					if allReads != 0 || !errors.Is(err, ErrDenied) {
						t.Fatal("invalid grant reached upstream")
					}
				} else if mode != "image redirect" && mode != "HTML" && mode != "oversize" && imageReads != 0 {
					t.Fatal("unapproved thumbnail path fetched")
				}
			}
		})
	}
}
