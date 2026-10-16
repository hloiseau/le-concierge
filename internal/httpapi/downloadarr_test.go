package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const downloadarrJobID = "12345678-1234-4123-8123-123456789abc"
const downloadarrConfirmedInput = `{"ratingKey":"12","quality":"720p-2","audio":"plex","subtitle":"none","confirmed":true}`

func downloadarrFixtureContract(s *Server) map[string]any {
	return map[string]any{"contractVersion": 1, "buildRevision": s.Config.DownloadarrRevision,
		"user": map[string]string{"plexId": "11111"}, "plexServerId": s.Config.PlexServerID,
		"sessionExpiresAt": time.Now().Add(time.Hour).UnixMilli(),
		"capabilities": map[string]any{"qualityOptions": []string{"720p-2"}, "streamOptions": true,
			"preparedDownloads": true, "singleUseTickets": true, "queuePersistence": "memory"}}
}

func downloadarrFixtureJob() map[string]any {
	return map[string]any{"id": downloadarrJobID, "title": "Personal film", "ratingKey": "12",
		"createdAt": time.Now().Add(-time.Minute).UnixMilli(), "quality": "720p-2", "filename": "film.mp4",
		"state": "ready", "stage": "ready", "readyCount": 1, "fileCount": 1, "progress": 100,
		"expiresAt": time.Now().Add(time.Hour).UnixMilli(), "audioLabel": "Plex", "subtitleLabel": "None"}
}

func downloadarrFixtureJSON(status int, value any) *http.Response {
	b, _ := json.Marshal(value)
	return response(status, string(b))
}

func downloadarrAPIFixture(t *testing.T) (*Server, string) {
	t.Helper()
	s := fixture(t)
	s.Config.DownloadarrURL = "https://downloadarr.test"
	s.Config.DownloadarrRevision = strings.Repeat("d", 40)
	cookie := seed(t, s, "11111", true)
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" || strings.Contains(r.URL.RawQuery, "account-") || r.Header.Get("X-Api-Key") != "" {
			t.Error("foreign credential reached Downloadarr")
		}
		if r.URL.Host == "plex.test" {
			if r.URL.Path == "/identity" {
				return response(200, `{"MediaContainer":{"machineIdentifier":"server-a"}}`), nil
			}
			if r.URL.Path == "/library/metadata/12" && r.Header.Get("X-Plex-Token") == "resource-11111" {
				return response(200, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"12","librarySectionID":"1","title":"Personal film","type":"movie","guid":"plex://movie/fixture","Guid":[{"id":"tmdb://42"}],"Media":[{"Part":[{"key":"/library/parts/1/file.mkv","exists":true,"accessible":true}]}]}]}}`), nil
			}
			t.Errorf("unexpected personal Plex read %s", r.URL.Path)
			return response(500, "unexpected"), nil
		}
		if r.URL.Host != "downloadarr.test" || r.Header.Get("X-Plex-Token") != "" {
			t.Error("wrong source/credential")
		}
		if strings.HasPrefix(r.URL.Path, "/api/auth/") || strings.HasSuffix(r.URL.Path, "/file") {
			if r.Header.Get("Authorization") != "" {
				t.Error("bearer reached PIN/file endpoint")
			}
		} else if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("b", 64) {
			t.Error("missing personal Downloadarr bearer")
		}
		switch r.URL.Path {
		case "/api/auth/plex/pin":
			if r.Method != "POST" {
				t.Error("wrong PIN method")
			}
			return downloadarrFixtureJSON(200, map[string]string{"flowId": strings.Repeat("a", 64), "url": "https://app.plex.tv/auth#?clientID=downloadarr-fixture&code=PIN&context%5Bdevice%5D%5Bproduct%5D=LibraryDownloadarr"}), nil
		case "/api/auth/plex/authenticate":
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 1 || body["flowId"] != strings.Repeat("a", 64) {
				t.Error("wrong independent PIN flow")
			}
			return downloadarrFixtureJSON(200, map[string]string{"token": strings.Repeat("b", 64)}), nil
		case "/api/integrations/concierge":
			return downloadarrFixtureJSON(200, downloadarrFixtureContract(s)), nil
		case "/api/downloads/options":
			return response(200, `{"audio":[{"id":"plex","label":"Plex"}],"subtitle":[{"id":"plex","label":"Plex"},{"id":"none","label":"None"}],"season":false,"fileCount":1}`), nil
		case "/api/downloads":
			if r.Method == "POST" {
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil || body["confirmed"] != nil || body["ratingKey"] != "12" || body["quality"] != "720p-2" {
					t.Error("wrong creation payload")
				}
				return downloadarrFixtureJSON(202, downloadarrFixtureJob()), nil
			}
			return downloadarrFixtureJSON(200, map[string]any{"jobs": []any{downloadarrFixtureJob()}}), nil
		case "/api/downloads/" + downloadarrJobID + "/ticket":
			return downloadarrFixtureJSON(200, map[string]string{"ticket": strings.Repeat("c", 64)}), nil
		case "/api/downloads/" + downloadarrJobID + "/file":
			var body map[string]string
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 1 || body["ticket"] != strings.Repeat("c", 64) {
				t.Error("ticket must stay in backend POST body")
			}
			out := response(200, "fixture-file")
			out.ContentLength = 12
			out.Header.Set("Content-Type", "video/mp4")
			out.Header.Set("Content-Disposition", `attachment; filename="film.mp4"`)
			out.Header.Set("Set-Cookie", "upstream=private")
			out.Header.Set("X-Source-Token", "private")
			return out, nil
		case "/api/downloads/" + downloadarrJobID:
			if r.Method != "DELETE" {
				t.Error("wrong cancellation method")
			}
			return response(204, ""), nil
		default:
			t.Errorf("unexpected Downloadarr route %s", r.URL.Path)
			return response(500, "unexpected"), nil
		}
	})
	return s, cookie
}

func linkDownloadarrFixture(t *testing.T, s *Server, cookie string) {
	t.Helper()
	for _, path := range []string{"/api/v1/downloads/link", "/api/v1/downloads/link/poll"} {
		w := call(s, "POST", path, cookie, s.Config.PublicOrigin, "")
		if w.Code != 200 {
			t.Fatalf("link %s: %d %s", path, w.Code, w.Body)
		}
		if path == "/api/v1/downloads/link/poll" && !strings.Contains(w.Body.String(), `"linked":true`) {
			t.Fatal("link incomplete")
		}
		for _, secret := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64), "account-11111", "resource-11111"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("link disclosed credentials")
			}
		}
	}
}

func TestDownloadarrHTTPPersonalLifecycle(t *testing.T) {
	s, cookie := downloadarrAPIFixture(t)
	if w := call(s, "GET", "/api/v1/downloads", cookie, "", ""); w.Code != 403 {
		t.Fatal("unlinked account read jobs")
	}
	linkDownloadarrFixture(t, s, cookie)
	for _, path := range []string{"/api/v1/downloads", "/api/v1/downloads/link", "/api/v1/downloads/options?ratingKey=12&quality=720p-2"} {
		w := call(s, "GET", path, cookie, "", "")
		if w.Code != 200 || strings.Contains(w.Body.String(), strings.Repeat("b", 64)) {
			t.Fatalf("read %s: %d %s", path, w.Code, w.Body)
		}
	}
	if w := call(s, "POST", "/api/v1/downloads", cookie, s.Config.PublicOrigin, downloadarrConfirmedInput); w.Code != 202 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	if w := call(s, "DELETE", "/api/v1/downloads/"+downloadarrJobID, cookie, s.Config.PublicOrigin, ""); w.Code != 204 {
		t.Fatalf("cancel %d %s", w.Code, w.Body)
	}
	if w := call(s, "DELETE", "/api/v1/downloads/link", cookie, s.Config.PublicOrigin, ""); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := call(s, "GET", "/api/v1/downloads", cookie, "", ""); w.Code != 403 {
		t.Fatal("read after unlink")
	}
}

func TestDownloadarrHTTPRejectsSelectorsCSRFAndUnconfirmedMutations(t *testing.T) {
	s, cookie := downloadarrAPIFixture(t)
	linkDownloadarrFixture(t, s, cookie)
	original := s.Upstream.HTTP.Transport
	mutations := 0
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			mutations++
		}
		return original.RoundTrip(r)
	})
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/v1/downloads?userId=22222", "", 400},
		{"GET", "/api/v1/downloads/link?token=foreign", "", 400},
		{"GET", "/api/v1/downloads/options?ratingKey=12&quality=720p-2&quality=720p-4", "", 400},
		{"GET", "/api/v1/downloads/options?ratingKey=12&quality=720p-2&season=maybe", "", 400},
		{"POST", "/api/v1/downloads/link", `{"token":"foreign"}`, 400},
		{"POST", "/api/v1/downloads/link", strings.Repeat(" ", 33) + `{"token":"foreign"}`, 400},
		{"POST", "/api/v1/downloads/link/poll?flowId=foreign", "", 400},
		{"POST", "/api/v1/downloads", `{"ratingKey":"12","quality":"720p-2"}`, 400},
		{"POST", "/api/v1/downloads", strings.Replace(downloadarrConfirmedInput, "720p-2", "1080p-8", 1), 400},
		{"POST", "/api/v1/downloads", strings.Replace(downloadarrConfirmedInput, `"confirmed":true`, `"confirmed":true,"owner":"22222"`, 1), 400},
		{"POST", "/api/v1/downloads", downloadarrConfirmedInput + strings.Repeat(" ", 8193), 400},
		{"POST", "/api/v1/downloads?userId=22222", downloadarrConfirmedInput, 400},
		{"DELETE", "/api/v1/downloads/" + downloadarrJobID + "?owner=22222", "", 400},
		{"POST", "/api/v1/downloads/" + downloadarrJobID + "/file?ticket=foreign", "", 400},
		{"POST", "/api/v1/downloads/" + downloadarrJobID + "/file", `ticket=foreign`, 400},
		{"POST", "/api/v1/downloads/invalid/file", "", 400},
	} {
		w := call(s, tc.method, tc.path, cookie, s.Config.PublicOrigin, tc.body)
		if w.Code != tc.status {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/v1/downloads", downloadarrConfirmedInput}, {"POST", "/api/v1/downloads/link", ""},
		{"POST", "/api/v1/downloads/link/poll", ""}, {"DELETE", "/api/v1/downloads/link", ""},
		{"DELETE", "/api/v1/downloads/" + downloadarrJobID, ""}, {"POST", "/api/v1/downloads/" + downloadarrJobID + "/file", ""},
	} {
		if w := call(s, tc.method, tc.path, cookie, "https://foreign.test", tc.body); w.Code != 403 {
			t.Errorf("CSRF %s %d", tc.path, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/v1/downloads/"+downloadarrJobID+"/file", nil)
	r.Header.Set("Origin", s.Config.PublicOrigin)
	r.AddCookie(&http.Cookie{Name: s.cookieName(), Value: cookie})
	r.Header.Set("Range", "bytes=0-1")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 400 || mutations != 0 {
		t.Fatalf("rejected input reached mutation: status%d count%d", w.Code, mutations)
	}
}

func TestDownloadarrHTTPSessionsOwnersAndDeadlines(t *testing.T) {
	for _, mode := range []string{"anonymous", "other-owner", "other-session", "session-expired", "plex-expired", "link-expired", "plex-changed", "logout", "unlink"} {
		t.Run(mode, func(t *testing.T) {
			s, cookie := downloadarrAPIFixture(t)
			linkDownloadarrFixture(t, s, cookie)
			switch mode {
			case "anonymous":
				cookie = ""
			case "other-owner":
				cookie = seed(t, s, "22222", true)
			case "other-session":
				cookie = seed(t, s, "11111", true)
			case "session-expired":
				v := s.sessions[digest(cookie)]
				v.Expires = time.Now().Add(-time.Second)
				s.sessions[digest(cookie)] = v
			case "plex-expired":
				v := s.grants["11111"]
				v.ExpiresAt = time.Now().Add(-time.Second)
				s.grants["11111"] = v
			case "link-expired":
				v := s.downloadarrGrants["11111"]
				v.ExpiresAt = time.Now().Add(-time.Second)
				s.downloadarrGrants["11111"] = v
			case "plex-changed":
				v := s.grants["11111"]
				v.AccountToken = "changed"
				s.grants["11111"] = v
			case "logout":
				call(s, "POST", "/api/v1/auth/logout", cookie, s.Config.PublicOrigin, "")
			case "unlink":
				call(s, "DELETE", "/api/v1/downloads/link", cookie, s.Config.PublicOrigin, "")
			}
			reads := 0
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) { reads++; return response(500, "private"), nil })
			w := call(s, "GET", "/api/v1/downloads", cookie, "", "")
			if (w.Code != 401 && w.Code != 403) || reads != 0 {
				t.Fatalf("invalid grant read source: %d %s reads%d", w.Code, w.Body, reads)
			}
		})
	}
}

func TestDownloadarrHTTPContractIdentityVersionAndExpiry(t *testing.T) {
	for _, mode := range []string{"identity", "server", "revision", "expiry", "source-revoked"} {
		t.Run(mode, func(t *testing.T) {
			s, cookie := downloadarrAPIFixture(t)
			if w := call(s, "POST", "/api/v1/downloads/link", cookie, s.Config.PublicOrigin, ""); w.Code != 200 {
				t.Fatal(w.Body)
			}
			original := s.Upstream.HTTP.Transport
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/api/integrations/concierge" {
					return original.RoundTrip(r)
				}
				body := downloadarrFixtureContract(s)
				switch mode {
				case "identity":
					body["user"] = map[string]string{"plexId": "22222"}
				case "server":
					body["plexServerId"] = "foreign"
				case "revision":
					body["buildRevision"] = strings.Repeat("e", 40)
				case "expiry":
					body["sessionExpiresAt"] = time.Now().Add(-time.Second).UnixMilli()
				case "source-revoked":
					return response(403, "private upstream details"), nil
				}
				return downloadarrFixtureJSON(200, body), nil
			})
			w := call(s, "POST", "/api/v1/downloads/link/poll", cookie, s.Config.PublicOrigin, "")
			if w.Code == 200 || strings.Contains(w.Body.String(), "private upstream") {
				t.Fatalf("invalid contract accepted %d %s", w.Code, w.Body)
			}
			if _, ok := s.currentDownloadarrGrant(digest(cookie), "11111"); ok {
				t.Fatal("failed connection retained")
			}
		})
	}
}

func TestDownloadarrHTTPLateResponsesCannotRestoreOrExposeRevokedGrant(t *testing.T) {
	for _, phase := range []string{"pin", "poll", "list", "create", "options", "file"} {
		for _, action := range []string{"unlink", "logout"} {
			t.Run(phase+"/"+action, func(t *testing.T) {
				s, cookie := downloadarrAPIFixture(t)
				if phase != "pin" {
					if phase == "poll" {
						call(s, "POST", "/api/v1/downloads/link", cookie, s.Config.PublicOrigin, "")
					} else {
						linkDownloadarrFixture(t, s, cookie)
					}
				}
				original := s.Upstream.HTTP.Transport
				changed := false
				path := map[string]string{"pin": "/api/auth/plex/pin", "poll": "/api/auth/plex/authenticate", "list": "/api/downloads", "create": "/api/downloads", "options": "/library/metadata/12", "file": "/api/downloads/" + downloadarrJobID + "/file"}[phase]
				s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
					out, err := original.RoundTrip(r)
					if !changed && r.URL.Path == path {
						changed = true
						method, route := "DELETE", "/api/v1/downloads/link"
						if action == "logout" {
							method, route = "POST", "/api/v1/auth/logout"
						}
						if w := call(s, method, route, cookie, s.Config.PublicOrigin, ""); w.Code != 200 {
							t.Fatal(w.Body)
						}
					}
					return out, err
				})
				method, route, body := "GET", "/api/v1/downloads", ""
				switch phase {
				case "pin":
					method, route = "POST", "/api/v1/downloads/link"
				case "poll":
					method, route = "POST", "/api/v1/downloads/link/poll"
				case "create":
					method, body = "POST", downloadarrConfirmedInput
				case "options":
					route = "/api/v1/downloads/options?ratingKey=12&quality=720p-2"
				case "file":
					method, route = "POST", "/api/v1/downloads/"+downloadarrJobID+"/file"
				}
				w := call(s, method, route, cookie, s.Config.PublicOrigin, body)
				if !changed || w.Code < 400 || strings.Contains(w.Body.String(), "Personal film") || strings.Contains(w.Body.String(), "fixture-file") {
					t.Fatalf("late content escaped %d %s", w.Code, w.Body)
				}
				if _, ok := s.currentDownloadarrGrant(digest(cookie), "11111"); ok {
					t.Fatal("link restored")
				}
			})
		}
	}
}

func TestDownloadarrHTTPFileHeadersRemainSafe(t *testing.T) {
	for _, mode := range []string{"normal", "path", "extension", "zip", "html"} {
		t.Run(mode, func(t *testing.T) {
			s, cookie := downloadarrAPIFixture(t)
			linkDownloadarrFixture(t, s, cookie)
			original := s.Upstream.HTTP.Transport
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				out, err := original.RoundTrip(r)
				if strings.HasSuffix(r.URL.Path, "/file") {
					switch mode {
					case "path":
						out.Header.Set("Content-Disposition", `attachment; filename="../secret.mp4"`)
					case "extension":
						out.Header.Set("Content-Disposition", `attachment; filename="film.html"`)
					case "zip":
						out.Header.Set("Content-Type", "application/zip")
						out.Header.Set("Content-Disposition", `attachment; filename="season.zip"`)
					case "html":
						out.Header.Set("Content-Type", "text/html")
					}
				}
				return out, err
			})
			w := call(s, "POST", "/api/v1/downloads/"+downloadarrJobID+"/file", cookie, s.Config.PublicOrigin, "")
			if mode == "html" {
				if w.Code != 503 || strings.Contains(w.Body.String(), "fixture-file") {
					t.Fatal("HTML accepted")
				}
				return
			}
			if w.Code != 200 || w.Body.String() != "fixture-file" || w.Header().Get("Set-Cookie") != "" || w.Header().Get("X-Source-Token") != "" || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unsafe file %d %v %s", w.Code, w.Header(), w.Body)
			}
			if (mode == "path" || mode == "extension") && !strings.Contains(w.Header().Get("Content-Disposition"), "telechargement.mp4") {
				t.Fatal("filename not sanitized")
			}
			if mode == "zip" && w.Header().Get("Content-Type") != "application/zip" {
				t.Fatal("ZIP content type lost")
			}
		})
	}
}

type downloadarrObservedBody struct {
	io.ReadCloser
	closed chan struct{}
	once   sync.Once
}

func (b *downloadarrObservedBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() { close(b.closed) })
	return err
}

func TestDownloadarrHTTPActiveStreamStopsOnRevocationAndExpiry(t *testing.T) {
	for _, action := range []string{"unlink", "logout", "expiry", "client-cancel"} {
		t.Run(action, func(t *testing.T) {
			s, cookie := downloadarrAPIFixture(t)
			linkDownloadarrFixture(t, s, cookie)
			reader, writer := io.Pipe()
			observed := &downloadarrObservedBody{ReadCloser: reader, closed: make(chan struct{})}
			defer reader.Close()
			defer writer.Close()
			original := s.Upstream.HTTP.Transport
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				out, err := original.RoundTrip(r)
				if strings.HasSuffix(r.URL.Path, "/file") {
					out.Body = observed
					out.ContentLength = -1
				}
				return out, err
			})
			api := httptest.NewServer(s.Handler())
			defer api.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r, _ := http.NewRequestWithContext(ctx, "POST", api.URL+"/api/v1/downloads/"+downloadarrJobID+"/file", nil)
			r.Header.Set("Origin", s.Config.PublicOrigin)
			r.AddCookie(&http.Cookie{Name: s.cookieName(), Value: cookie})
			written := make(chan error, 1)
			go func() { _, err := io.WriteString(writer, "first"); written <- err }()
			res, err := api.Client().Do(r)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			first := make([]byte, 5)
			if _, err := io.ReadFull(res.Body, first); err != nil || string(first) != "first" {
				t.Fatalf("initial stream %q %v", first, err)
			}
			if err := <-written; err != nil {
				t.Fatal(err)
			}
			switch action {
			case "unlink":
				call(s, "DELETE", "/api/v1/downloads/link", cookie, s.Config.PublicOrigin, "")
			case "logout":
				call(s, "POST", "/api/v1/auth/logout", cookie, s.Config.PublicOrigin, "")
			case "expiry":
				s.mu.Lock()
				g := s.downloadarrGrants["11111"]
				g.ExpiresAt = time.Now().Add(-time.Second)
				s.downloadarrGrants["11111"] = g
				s.mu.Unlock()
			case "client-cancel":
				cancel()
			}
			done := make(chan error, 1)
			go func() { _, err := io.ReadAll(res.Body); done <- err }()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("revoked stream ended as successful file")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("revoked stream remained open")
			}
			select {
			case <-observed.closed:
			case <-time.After(3 * time.Second):
				t.Fatal("cancellation did not close source")
			}
			if _, err := io.WriteString(writer, "private-late-bytes"); err == nil {
				t.Fatal("revocation left source open")
			}
		})
	}
}

func TestDownloadarrHTTPPendingPollIsBoundedAndKeepsItsPrivateFlow(t *testing.T) {
	s, cookie := downloadarrAPIFixture(t)
	if w := call(s, "POST", "/api/v1/downloads/link", cookie, s.Config.PublicOrigin, ""); w.Code != 200 {
		t.Fatal(w.Body)
	}
	original := s.Upstream.HTTP.Transport
	polls := 0
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/auth/plex/authenticate" {
			polls++
			if polls == 1 {
				return response(202, `{"pending":true}`), nil
			}
		}
		return original.RoundTrip(r)
	})
	w := call(s, "POST", "/api/v1/downloads/link/poll", cookie, s.Config.PublicOrigin, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"linked":false`) {
		t.Fatalf("pending %d %s", w.Code, w.Body)
	}
	w = call(s, "POST", "/api/v1/downloads/link/poll", cookie, s.Config.PublicOrigin, "")
	if w.Code != 429 || w.Header().Get("Retry-After") != "2" || polls != 1 {
		t.Fatal("poll bypassed throttle")
	}
	s.mu.Lock()
	g := s.downloadarrGrants["11111"]
	g.LastPoll = time.Now().Add(-3 * time.Second)
	s.downloadarrGrants["11111"] = g
	s.mu.Unlock()
	w = call(s, "POST", "/api/v1/downloads/link/poll", cookie, s.Config.PublicOrigin, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"linked":true`) || polls != 2 {
		t.Fatalf("completion %d %s", w.Code, w.Body)
	}
	if g, ok := s.currentDownloadarrGrant(digest(cookie), "11111"); !ok || g.FlowID != "" || g.Bearer != strings.Repeat("b", 64) {
		t.Fatal("flow not replaced by private session")
	}
}

func TestDownloadarrHTTPUpstreamFailureDoesNotRetryOrLeak(t *testing.T) {
	for _, status := range []int{403, 404, 409, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s, cookie := downloadarrAPIFixture(t)
			linkDownloadarrFixture(t, s, cookie)
			original := s.Upstream.HTTP.Transport
			writes := 0
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/api/downloads" && r.Method == "POST" {
					writes++
					out := response(status, `{"error":"http://private.test/path?token=secret"}`)
					out.Header.Set("Retry-After", "120")
					return out, nil
				}
				return original.RoundTrip(r)
			})
			w := call(s, "POST", "/api/v1/downloads", cookie, s.Config.PublicOrigin, downloadarrConfirmedInput)
			want := status
			if status == 500 {
				want = 503
			}
			if w.Code != want || writes != 1 || strings.Contains(w.Body.String(), "private.test") || strings.Contains(w.Body.String(), "secret") {
				t.Fatalf("unsafe failure %d %s writes%d", w.Code, w.Body, writes)
			}
			if status == 429 && w.Header().Get("Retry-After") != "120" {
				t.Fatal("rate deadline lost")
			}
		})
	}
}

func TestDownloadarrHTTPFileOutlivesOrdinaryWriteDeadline(t *testing.T) {
	s, cookie := downloadarrAPIFixture(t)
	linkDownloadarrFixture(t, s, cookie)
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	original := s.Upstream.HTTP.Transport
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		out, err := original.RoundTrip(r)
		if strings.HasSuffix(r.URL.Path, "/file") {
			deadline, ok := r.Context().Deadline()
			g, valid := s.currentDownloadarrGrant(digest(cookie), "11111")
			if !ok || !valid || time.Until(deadline) <= 30*time.Second || deadline.After(g.ExpiresAt) {
				t.Error("file must inherit the grant deadline rather than the ordinary API timeout")
			}
			out.Body = reader
			out.ContentLength = 11
		}
		return out, err
	})
	api := httptest.NewUnstartedServer(s.Handler())
	api.Config.WriteTimeout = 30 * time.Millisecond
	api.Start()
	defer api.Close()
	request, _ := http.NewRequest("POST", api.URL+"/api/v1/downloads/"+downloadarrJobID+"/file", nil)
	request.Header.Set("Origin", s.Config.PublicOrigin)
	request.AddCookie(&http.Cookie{Name: s.cookieName(), Value: cookie})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer writer.Close()
		io.WriteString(writer, "first")
		<-release
		io.WriteString(writer, "second")
	}()
	res, err := api.Client().Do(request)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	defer res.Body.Close()
	first := make([]byte, 5)
	if _, err := io.ReadFull(res.Body, first); err != nil {
		close(release)
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	close(release)
	last, err := io.ReadAll(res.Body)
	if err != nil || string(first)+string(last) != "firstsecond" {
		t.Fatalf("ordinary timeout truncated file %q %v", last, err)
	}
	<-done
}
