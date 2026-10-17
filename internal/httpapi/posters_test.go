package httpapi

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"
)

var posterFixturePNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAACAAAAAwCAIAAAD/zu84AAAAL0lEQVR4nO3NQQkAAAgEsItnDpMZ1RQ+hMH+S9ecikAgEAgEAoFAIBAIBAKB4FOwXVi8amAP0wUAAAAASUVORK5CYII=")

func posterAPIFixture(t *testing.T) (*Server, string) {
	t.Helper()
	s := fixture(t)
	cookie := seed(t, s, "11111", true)
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "plex.test" || r.Header.Get("Cookie") != "" || strings.Contains(r.URL.RawQuery, "resource-") || r.Header.Get("Authorization") != "" {
			t.Error("unsafe poster request")
		}
		if r.URL.Path == "/identity" {
			return response(200, `{"MediaContainer":{"machineIdentifier":"server-a"}}`), nil
		}
		if r.Header.Get("X-Plex-Token") != "resource-11111" {
			return response(403, "denied"), nil
		}
		switch r.URL.Path {
		case "/library/metadata/12":
			return response(200, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"12","librarySectionID":"1","title":"Fixture film","type":"movie","guid":"plex://movie/fixture","thumb":"/library/metadata/12/thumb/123"}]}}`), nil
		case "/library/metadata/12/thumb/123":
			out := response(200, string(posterFixturePNG))
			out.ContentLength = int64(len(posterFixturePNG))
			out.Header.Set("Content-Type", "image/png")
			out.Header.Set("Set-Cookie", "private=upstream")
			out.Header.Set("X-Plex-Token", "private-token")
			return out, nil
		default:
			t.Errorf("unexpected poster path %s", r.URL.Path)
			return response(404, "missing"), nil
		}
	})
	return s, cookie
}

func TestPosterPersonalAuthorizationAndHeaders(t *testing.T) {
	s, cookie := posterAPIFixture(t)
	path := "/api/v1/posters/plex/12"
	for _, tc := range []struct {
		name, cookie, path string
		want               int
	}{
		{"anonymous", "", path, 401},
		{"unlinked", seed(t, s, "22222", false), path, 403},
		{"different personal library", seed(t, s, "33333", true), path, 403},
		{"injected source", cookie, path + "?url=https://foreign.example", 400},
		{"invalid item", cookie, "/api/v1/posters/plex/invalid", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if w := call(s, "GET", tc.path, tc.cookie, "", ""); w.Code != tc.want || bytes.Equal(w.Body.Bytes(), posterFixturePNG) {
				t.Fatalf("poster boundary: %d", w.Code)
			}
		})
	}
	w := call(s, "GET", path, cookie, "", "")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), posterFixturePNG) {
		t.Fatalf("poster response %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Set-Cookie") != "" || w.Header().Get("X-Plex-Token") != "" {
		t.Fatal("unsafe image response headers")
	}
}

func TestPosterRevalidatesWhileImageLoads(t *testing.T) {
	for _, mode := range []string{"logout", "account replaced", "resource replaced", "expired"} {
		t.Run(mode, func(t *testing.T) {
			s, cookie := posterAPIFixture(t)
			original := s.Upstream.HTTP.Transport
			started, release := make(chan struct{}), make(chan struct{})
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				out, err := original.RoundTrip(r)
				if strings.Contains(r.URL.Path, "/thumb/") {
					close(started)
					<-release
				}
				return out, err
			})
			done := make(chan int, 1)
			go func() {
				w := call(s, "GET", "/api/v1/posters/plex/12", cookie, "", "")
				if bytes.Equal(w.Body.Bytes(), posterFixturePNG) {
					t.Error("revoked session received image")
				}
				done <- w.Code
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				close(release)
				t.Fatal("image not requested")
			}
			if mode == "logout" {
				if w := call(s, "POST", "/api/v1/auth/logout", cookie, s.Config.PublicOrigin, ""); w.Code != 200 {
					t.Fatal("logout failed")
				}
			} else {
				s.mu.Lock()
				grant := s.grants["11111"]
				switch mode {
				case "account replaced":
					grant.AccountToken = "replacement"
				case "resource replaced":
					grant.ServerToken = "replacement"
				case "expired":
					grant.ExpiresAt = time.Now().Add(-time.Second)
				}
				s.grants["11111"] = grant
				s.mu.Unlock()
			}
			close(release)
			select {
			case code := <-done:
				if code != 401 && code != 403 {
					t.Fatalf("revoked poster returned %d", code)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("poster request stuck")
			}
		})
	}
}
