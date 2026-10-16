package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func diagnosticAPIFixture(t *testing.T) (*Server, string, *int) {
	t.Helper()
	s := fixture(t)
	s.Config.AdminIDs = []string{"11111"}
	s.Config.RadarrURL = "https://radarr.test"
	s.Config.RadarrAPIKey = "arr-secret"
	cookie := seed(t, s, "11111", true)
	calls := 0
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" {
			t.Fatal("diagnostic issued mutation")
		}
		switch r.URL.Path {
		case "/api/v1/request/7":
			return response(200, `{"id":7,"type":"movie","status":2,"createdAt":"2026-10-15T10:00:00Z","serverId":0,"media":{"tmdbId":12,"mediaType":"movie"}}`), nil
		case "/api/v1/settings/radarr":
			return response(200, `[{"id":0,"hostname":"radarr.test","port":443,"useSsl":true,"isDefault":true,"is4k":false,"apiKey":"ignored-secret"}]`), nil
		case "/api/v3/system/status":
			return response(200, `{"appName":"Radarr","version":"6.4.4.10685"}`), nil
		case "/api/v3/movie":
			return response(200, `[{"id":9,"tmdbId":12,"hasFile":false}]`), nil
		case "/api/v3/queue":
			return response(200, `{"page":1,"pageSize":50,"totalRecords":1,"records":[{"movieId":9,"trackedDownloadState":"importBlocked","statusMessages":[{"messages":["Found matching movie via grab history, but release was matched to movie by ID. Manual Import required. /private/nas token=secret"]}]}]}`), nil
		case "/api/v3/history":
			return response(200, `{"page":1,"pageSize":30,"totalRecords":0,"records":[]}`), nil
		default:
			t.Fatalf("unexpected read %s", r.URL.Path)
			return nil, nil
		}
	})
	return s, cookie, &calls
}
func TestDiagnosticHTTPRequiresCurrentAdminAndTypedRequest(t *testing.T) {
	s, cookie, calls := diagnosticAPIFixture(t)
	other := seed(t, s, "22222", true)
	for _, tc := range []struct {
		cookie, path string
		code         int
	}{{"", "/api/v1/admin/diagnostics?requestId=7", 401}, {other, "/api/v1/admin/diagnostics?requestId=7", 403}, {cookie, "/api/v1/admin/diagnostics?requestId=7&requestId=8", 400}, {cookie, "/api/v1/admin/diagnostics?requestId=0", 400}, {cookie, "/api/v1/admin/diagnostics?requestId=7&userId=1", 400}} {
		w := call(s, "GET", tc.path, tc.cookie, "", "")
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body)
		}
	}
	if *calls != 0 {
		t.Fatal("non-admin input reached services")
	}
	w := call(s, "GET", "/api/v1/admin/diagnostics?requestId=7", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"stage":"manual_import"`) || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "/private") {
		t.Fatalf("diagnostic: %d %s", w.Code, w.Body)
	}
}
func TestDiagnosticRevalidatesAdminAfterRemoteRead(t *testing.T) {
	for _, state := range []string{"admin removed", "logged out"} {
		t.Run(state, func(t *testing.T) {
			s, cookie, _ := diagnosticAPIFixture(t)
			original := s.Upstream.HTTP.Transport
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				result, err := original.RoundTrip(r)
				if r.URL.Path == "/api/v3/history" {
					if state == "admin removed" {
						s.Config.AdminIDs = nil
					} else {
						call(s, "POST", "/api/v1/auth/logout", cookie, s.Config.PublicOrigin, "")
					}
				}
				return result, err
			})
			w := call(s, "GET", "/api/v1/admin/diagnostics?requestId=7", cookie, "", "")
			if w.Code == 200 || strings.Contains(w.Body.String(), "Manual Import") {
				t.Fatalf("late admin data escaped %d %s", w.Code, w.Body)
			}
		})
	}
}
func TestDiagnosticDiscordRemainsPrivateAndServerAuthorized(t *testing.T) {
	s, _, calls := diagnosticAPIFixture(t)
	seed(t, s, "22222", true)
	request := subscriptionInteraction(t, s)
	command := `{"name":"diagnostic","options":[{"name":"demande","type":4,"value":7}]}`
	w := request("90001", "22222", command)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "réservée aux administrateurs") || *calls != 0 {
		t.Fatalf("admin boundary %s", w.Body)
	}
	w = request("90002", "11111", command)
	var reply struct {
		Content string `json:"content"`
		Flags   int    `json:"flags"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Flags != 64 || len(reply.Content) > 2000 || strings.Contains(reply.Content, "secret") || !strings.Contains(reply.Content, "Manual Import required") {
		t.Fatalf("unsafe reply %s", w.Body)
	}
	if w = request("90002", "11111", command); w.Code != 409 {
		t.Fatal("replay accepted")
	}
}
