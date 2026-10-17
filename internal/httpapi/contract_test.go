package httpapi

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/hloiseau/le-concierge/internal/community"
	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/store"
)

func loadContract(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile("../../api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(t.Context()); err != nil {
		t.Fatalf("invalid OpenAPI document: %v", err)
	}
	return doc
}

func validateContractResponse(doc *openapi3.T, method, target string, w *httptest.ResponseRecorder) error {
	r := httptest.NewRequest(method, target, nil)
	path := doc.Paths.Value(contractPath(doc, r.URL.Path))
	if path == nil || path.GetOperation(method) == nil {
		return fmt.Errorf("undocumented operation %s %s", method, r.URL.Path)
	}
	operation := path.GetOperation(method)
	response := operation.Responses.Status(w.Code)
	if response == nil && w.Code >= 400 {
		response = operation.Responses.Default()
	}
	if response == nil {
		return fmt.Errorf("undocumented response status %d for %s %s", w.Code, method, r.URL.Path)
	}
	if w.Code == http.StatusNoContent && len(response.Value.Content) == 0 && w.Body.Len() == 0 {
		return nil
	}
	if w.Code < 300 || w.Code >= 400 {
		content := response.Value.Content.Get("application/json")
		if content == nil || content.Schema == nil {
			binary := response.Value.Content.Get(w.Header().Get("Content-Type"))
			if binary != nil && binary.Schema != nil && binary.Schema.Value.Format == "binary" && w.Body.Len() > 0 && w.Body.Len() <= 2*1024*1024 {
				return nil
			}
			return fmt.Errorf("JSON response needs a schema for %s %s status %d", method, r.URL.Path, w.Code)
		}
	}
	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request: r,
			Route:   &routers.Route{Spec: doc, Path: r.URL.Path, PathItem: path, Method: method, Operation: operation},
		},
		Status: w.Code,
		Header: w.Header(),
		Options: &openapi3filter.Options{
			IncludeResponseStatus: true,
			SchemaValidationOptions: []openapi3.SchemaValidationOption{
				openapi3.EnableFormatValidation(),
				// The API's date-time convention is RFC3339 in UTC, including fractional seconds.
				openapi3.WithStringFormatValidator("date-time", openapi3.NewCallbackValidator(func(value string) error {
					if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
						return err
					}
					if !strings.HasSuffix(value, "Z") {
						return fmt.Errorf("date-time must use UTC: %q", value)
					}
					return nil
				})),
				openapi3.WithStringFormatValidator("uri", openapi3.NewCallbackValidator(func(value string) error {
					u, err := url.Parse(value)
					if err != nil || !u.IsAbs() || u.Host == "" || strings.ContainsAny(value, " \t\r\n") {
						return fmt.Errorf("invalid absolute URI: %q", value)
					}
					return nil
				})),
			},
		},
	}
	input.SetBodyBytes(w.Body.Bytes())
	return openapi3filter.ValidateResponse(r.Context(), input)
}

func contractPath(doc *openapi3.T, target string) string {
	path := strings.SplitN(target, "?", 2)[0]
	if doc.Paths.Value(path) != nil {
		return path
	}
	parts := strings.Split(path, "/")
	for template := range doc.Paths.Map() {
		segments := strings.Split(template, "/")
		if len(segments) != len(parts) {
			continue
		}
		match := true
		for i, segment := range segments {
			if segment != parts[i] && !(strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") && parts[i] != "") {
				match = false
				break
			}
		}
		if match {
			return template
		}
	}
	return path
}

// Read literal registrations rather than maintaining a second hand-written route list.
func registeredContractRoutes(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]bool{}
	for _, filename := range files {
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(gotoken.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "HandleFunc" && selector.Sel.Name != "Handle") {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != gotoken.STRING {
				t.Errorf("%s: route registrations must be literal for the contract inventory", filename)
				return true
			}
			pattern, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(pattern, "/api/v1/") {
				routes[pattern] = true
			}
			return true
		})
	}
	return routes
}

func TestOpenAPIResponses(t *testing.T) {
	doc := loadContract(t)
	covered := map[string]bool{}
	check := func(t *testing.T, method, path string, w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, w.Code, status, w.Body)
		}
		if err := validateContractResponse(doc, method, path, w); err != nil {
			t.Fatalf("%s %s: %v\nResponse: %s", method, path, err, w.Body)
		}
		if status < 400 {
			covered[method+" "+contractPath(doc, path)] = true
		}
	}
	for _, tc := range []struct {
		name, method, path, body string
		anonymous                bool
	}{
		{"anonymous session", "GET", "/api/v1/session", "", true},
		{"Plex session", "GET", "/api/v1/session", "", false},
		{"capabilities", "GET", "/api/v1/capabilities", "", true},
		{"services", "GET", "/api/v1/services", "", false},
		{"requests", "GET", "/api/v1/requests?offset=0", "", false},
		{"recent", "GET", "/api/v1/recent?offset=0", "", false},
		{"departures", "GET", "/api/v1/departures", "", false},
		{"reminders", "GET", "/api/v1/reminders", "", false},
		{"get reminder preferences", "GET", "/api/v1/reminders/preferences", "", false},
		{"put reminder preferences", "PUT", "/api/v1/reminders/preferences", `{"enabled":true,"daysBefore":[7,2],"quietStartHour":22,"quietEndHour":8,"dailyCap":10,"allUnwatched":false}`, false},
		{"get preferences", "GET", "/api/v1/preferences", "", false},
		{"put preferences", "PUT", "/api/v1/preferences", `{"notifications":true}`, false},
		{"delete account", "DELETE", "/api/v1/account", "", false},
		{"logout", "POST", "/api/v1/auth/logout", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := contractFixture(t)
			cookie := ""
			if !tc.anonymous {
				var userID string
				cookie, userID = seedPlexOnly(t, s, "77")
				if tc.name == "reminders" {
					if err := s.Store.SaveReminderPreferences(t.Context(), userID, store.ReminderPreferences{Enabled: true, DaysBefore: []int{7, 2}, DailyCap: 10}); err != nil {
						t.Fatal(err)
					}
				}
			}
			w := call(s, tc.method, tc.path, cookie, s.Config.PublicOrigin, tc.body)
			check(t, tc.method, tc.path, w, 200)
			if tc.name == "requests" || tc.name == "recent" || tc.name == "departures" || tc.name == "services" || tc.name == "reminders" {
				var page struct{ Items []json.RawMessage }
				if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) == 0 {
					t.Fatal("contract fixture must exercise nested item schemas")
				}
			}
		})
	}
	t.Run("Plex login", func(t *testing.T) {
		s := fixture(t)
		plexProvider(t, s, authorizedResources, false)
		start := call(s, "POST", "/api/v1/auth/plex/start", "", s.Config.PublicOrigin, "")
		check(t, "POST", "/api/v1/auth/plex/start", start, 200)
		poll := cookiesCall(s, "POST", "/api/v1/auth/plex/poll", s.Config.PublicOrigin, namedCookie(t, start, s.plexCookieName()))
		check(t, "POST", "/api/v1/auth/plex/poll", poll, 200)
	})
	t.Run("Discord linking", func(t *testing.T) {
		s := contractFixture(t)
		cookie, _ := seedPlexOnly(t, s, "77")
		start := call(s, "GET", "/api/v1/auth/discord/start", cookie, "", "")
		check(t, "GET", "/api/v1/auth/discord/start", start, 302)
		location, err := url.Parse(start.Header().Get("Location"))
		if err != nil || location.Host != "discord.com" {
			t.Fatal("missing Discord redirect")
		}
		path := "/api/v1/auth/discord/callback?code=fixture&state=" + location.Query().Get("state")
		callback := cookiesCall(s, "GET", path, "", namedCookie(t, start, s.oauthCookieName()))
		check(t, "GET", path, callback, 303)
		if callback.Header().Get("Location") != "/compte" {
			t.Fatal("missing Hub redirect")
		}
	})
	t.Run("subscriptions", func(t *testing.T) {
		s, cookie, source := subscriptionFixture(t)
		created := call(s, "POST", "/api/v1/subscriptions", cookie, s.Config.PublicOrigin, `{"type":"movie","tmdbId":12,"channel":"hub"}`)
		check(t, "POST", "/api/v1/subscriptions", created, 200)
		var result subscriptionResult
		if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil || result.Subscription == nil {
			t.Fatal("missing subscription", err)
		}
		check(t, "GET", "/api/v1/subscriptions", call(s, "GET", "/api/v1/subscriptions", cookie, "", ""), 200)
		source.available = true
		check(t, "GET", "/api/v1/subscriptions", call(s, "GET", "/api/v1/subscriptions", cookie, "", ""), 200)
		path := fmt.Sprintf("/api/v1/subscriptions/%d", result.Subscription.ID)
		check(t, "DELETE", path, call(s, "DELETE", path, cookie, s.Config.PublicOrigin, ""), 200)
		check(t, "POST", "/api/v1/subscriptions", call(s, "POST", "/api/v1/subscriptions", cookie, s.Config.PublicOrigin, `{"type":"movie","tmdbId":12,"channel":"hub"}`), 200)
	})
	t.Run("extension requests", func(t *testing.T) {
		s, cookie, _ := reminderFixture(t)
		s.Config.AdminIDs = []string{"99999"}
		admin := seed(t, s, "99999", true)
		created := call(s, "POST", "/api/v1/extension-requests", cookie, s.Config.PublicOrigin, `{"mediaId":"42","days":7,"reason":"Finir le film"}`)
		check(t, "POST", "/api/v1/extension-requests", created, 200)
		var item store.ExtensionRequest
		if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil {
			t.Fatal(err)
		}
		check(t, "GET", "/api/v1/extension-requests", call(s, "GET", "/api/v1/extension-requests", cookie, "", ""), 200)
		check(t, "GET", "/api/v1/admin/extension-requests", call(s, "GET", "/api/v1/admin/extension-requests", admin, "", ""), 200)
		check(t, "PUT", "/api/v1/admin/extension-requests", call(s, "PUT", "/api/v1/admin/extension-requests", admin, s.Config.PublicOrigin, fmt.Sprintf(`{"id":%d,"status":"reviewed","note":"Vérifié"}`, item.ID)), 200)
	})
	t.Run("book catalog and external requests", func(t *testing.T) {
		s, cookie, _ := booksFixture(t)
		for _, tc := range []struct{ method, path, body string }{
			{"GET", "/api/v1/books/link", ""},
			{"POST", "/api/v1/books/link", `{"username":"alice-opds","password":"personal-secret"}`},
			{"GET", "/api/v1/books?q=book", ""},
			{"GET", "/api/v1/books/recent", ""},
			{"GET", "/api/v1/books/7/cover", ""},
			{"GET", "/api/v1/book-requests/link?q=book", ""},
			{"GET", "/api/v1/book-collections", ""},
			{"DELETE", "/api/v1/books/link", ""},
		} {
			check(t, tc.method, tc.path, call(s, tc.method, tc.path, cookie, s.Config.PublicOrigin, tc.body), 200)
		}
	})
	t.Run("availability inbox and channel", func(t *testing.T) {
		s, cookie, _, item, source := availabilityFixture(t)
		source.available = true
		check(t, "GET", "/api/v1/subscriptions/notifications", call(s, "GET", "/api/v1/subscriptions/notifications", cookie, "", ""), 200)
		path := fmt.Sprintf("/api/v1/subscriptions/%d", item.ID)
		check(t, "PATCH", path, call(s, "PATCH", path, cookie, s.Config.PublicOrigin, `{"channel":"hub"}`), 200)
	})
	t.Run("personal Shelfmark tracking", func(t *testing.T) {
		s, cookie, _ := shelfmarkAPIFixture(t)
		for _, tc := range []struct{ method, path, body string }{
			{"GET", "/api/v1/book-requests/link-session", ""},
			{"POST", "/api/v1/book-requests/link-session", `{"username":"alice","password":"personal-password"}`},
			{"GET", "/api/v1/book-requests", ""},
			{"DELETE", "/api/v1/book-requests/link-session", ""},
		} {
			check(t, tc.method, tc.path, call(s, tc.method, tc.path, cookie, s.Config.PublicOrigin, tc.body), 200)
		}
	})
	t.Run("personal Downloadarr", func(t *testing.T) {
		s, cookie := downloadarrAPIFixture(t)
		for _, tc := range []struct {
			method, path, body string
			status             int
		}{
			{"GET", "/api/v1/downloads/link", "", 200},
			{"POST", "/api/v1/downloads/link", "", 200},
			{"POST", "/api/v1/downloads/link/poll", "", 200},
			{"GET", "/api/v1/downloads/link", "", 200},
			{"GET", "/api/v1/downloads/options?ratingKey=12&quality=720p-2", "", 200},
			{"POST", "/api/v1/downloads", downloadarrConfirmedInput, 202},
			{"GET", "/api/v1/downloads", "", 200},
			{"POST", "/api/v1/downloads/" + downloadarrJobID + "/file", "", 200},
			{"DELETE", "/api/v1/downloads/" + downloadarrJobID, "", 204},
			{"DELETE", "/api/v1/downloads/link", "", 200},
		} {
			check(t, tc.method, tc.path, call(s, tc.method, tc.path, cookie, s.Config.PublicOrigin, tc.body), tc.status)
		}
	})
	t.Run("delayed personal request reminder", func(t *testing.T) {
		s, cookie, _ := availableRequestFixture(t)
		check(t, "GET", "/api/v1/reminders", call(s, "GET", "/api/v1/reminders", cookie, "", ""), 200)
	})
	t.Run("monitoring and admin activity", func(t *testing.T) {
		s := fixture(t)
		s.Config.MonitoringEnabled = true
		s.Config.TautulliURL = ""
		s.Config.AdminIDs = []string{"11111"}
		cookie := seed(t, s, "11111", true)
		for _, path := range []string{"/api/v1/monitoring", "/api/v1/monitoring/history?range=1h", "/api/v1/admin/plex-activity"} {
			check(t, "GET", path, call(s, "GET", path, cookie, "", ""), 200)
		}
	})
	t.Run("admin diagnostic", func(t *testing.T) {
		s, cookie, _ := diagnosticAPIFixture(t)
		path := "/api/v1/admin/diagnostics?requestId=7"
		check(t, "GET", path, call(s, "GET", path, cookie, "", ""), 200)
	})
	t.Run("community publication", func(t *testing.T) {
		s := fixture(t)
		s.Config.CommunityFeedFile = filepath.Join(t.TempDir(), "publication.json")
		s.Config.CommunityForumIDs = []string{"11111"}
		s.Config.RoadmapIssueIDs = []string{"example/project#1"}
		now := time.Now().UTC()
		published := community.Publication{Version: 1, PublishedAt: now, ExpiresAt: now.Add(time.Hour), Forums: []community.PublishedThread{{ForumID: "11111", ThreadID: "22222", Title: "Proposal", Kind: "proposal", Status: "open", UpdatedAt: now}}, Roadmap: []community.PublishedIssue{{IssueID: "example/project#1", ID: "feature", Title: "Feature", Status: "planned", Dependencies: []string{}, Date: "2027-01-01", DateConfirmed: true, ThreadID: "22222", LinkToIssue: true}}}
		body, err := json.Marshal(published)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(s.Config.CommunityFeedFile, body, 0600); err != nil {
			t.Fatal(err)
		}
		cookie := seed(t, s, "11111", true)
		for _, path := range []string{"/api/v1/community", "/api/v1/roadmap"} {
			check(t, "GET", path, call(s, "GET", path, cookie, "", ""), 200)
		}
	})
	t.Run("errors", func(t *testing.T) {
		for path, item := range doc.Paths.Map() {
			for method, operation := range item.Operations() {
				t.Run(method+" "+path, func(t *testing.T) {
					s := fixture(t)
					s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
						t.Fatalf("unauthenticated request contacted upstream: %s", r.URL.Path)
						return nil, nil
					})
					security := doc.Security
					if operation.Security != nil {
						security = *operation.Security
					}
					if len(security) != 0 {
						check(t, method, path, call(s, method, path, "", s.Config.PublicOrigin, ""), 401)
					}
					// The size limit is a real error response shared by every public operation.
					check(t, method, path, call(s, method, path, "", s.Config.PublicOrigin, strings.Repeat("x", 65537)), 413)
				})
			}
		}
		for _, tc := range []struct {
			method, path, origin, body string
			status                     int
		}{
			{"PUT", "/api/v1/preferences", "https://attacker.example", `{"notifications":true}`, 403},
			{"PUT", "/api/v1/preferences", "http://localhost:3000", `{"notifications":"yes"}`, 400},
			{"POST", "/api/v1/auth/plex/poll", "http://localhost:3000", "", 410},
			{"GET", "/api/v1/auth/discord/callback?state=invalid", "", "", 403},
		} {
			s := contractFixture(t)
			cookie, _ := seedPlexOnly(t, s, "77")
			check(t, tc.method, tc.path, call(s, tc.method, tc.path, cookie, tc.origin, tc.body), tc.status)
		}
	})
	t.Run("route and fixture coverage", func(t *testing.T) {
		registered := registeredContractRoutes(t)
		for path, item := range doc.Paths.Map() {
			for method := range item.Operations() {
				route := method + " " + path
				if !registered[route] {
					t.Errorf("OpenAPI operation has no registered handler: %s", route)
				}
				if !covered[route] {
					t.Errorf("OpenAPI operation needs a successful response fixture: %s", route)
				}
				delete(registered, route)
			}
		}
		for route := range registered {
			t.Errorf("registered handler missing from OpenAPI: %s", route)
		}
	})
}

func contractFixture(t *testing.T) *Server {
	t.Helper()
	s := fixture(t)
	s.Config.Services = []config.Service{{ID: "plex", Name: "Plex", Description: "Films et séries", Category: "watch", URL: "https://app.plex.tv"}}
	s.Config.MaintainerrURL = "http://maintainerr.test"
	s.Config.TautulliURL = "http://tautulli.test"
	s.Config.TautulliAPIKey = "fixture-key"
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.Path {
		case "/api/v1/user":
			body = `{"pageInfo":{"results":1},"results":[{"id":42,"plexId":77}]}`
		case "/api/v1/user/42/requests":
			body = `{"pageInfo":{"results":1},"results":[{"id":7,"type":"tv","status":2,"createdAt":"2026-01-01T14:00:00.123+02:00","requestedBy":{"id":42},"media":{"tmdbId":100,"status":4},"seasons":[{"seasonNumber":1}]}]}`
		case "/api/v1/tv/100":
			body = `{"name":"Fixture series"}`
		case "/identity":
			body = `{"MediaContainer":{"machineIdentifier":"server-a"}}`
		case "/library/recentlyAdded":
			body = `{"MediaContainer":{"Metadata":[{"ratingKey":"42","title":"Fixture film","type":"movie","year":2026,"addedAt":1767268800}]}}`
		case "/library/all":
			body = `{"MediaContainer":{"size":0,"totalSize":0,"Metadata":[]}}`
		case "/api/app/status":
			body = `{"status":1,"version":"3.30.1"}`
		case "/api/media-server/type":
			body = `{"type":"plex"}`
		case "/api/media-server":
			body = `{"machineId":"server-a"}`
		case "/api/collections/overlay-data":
			body = fmt.Sprintf(`[{"id":1,"mediaServerType":"plex","libraryId":"2","type":"movie","isActive":true,"arrAction":0,"deleteAfterDays":7,"mediaCount":1,"media":[{"id":1,"collectionId":1,"mediaServerId":"42","addDate":%q,"isManual":false,"ruleEvaluationFailed":false}]}]`, time.Now().UTC().Format(time.RFC3339))
		case "/api/rules":
			body = `[{"id":5,"collectionId":1,"isActive":true}]`
		case "/api/rules/exclusion":
			body = `[]`
		case "/library/metadata/42":
			body = `{"MediaContainer":{"size":1,"librarySectionID":"2","Metadata":[{"ratingKey":"42","title":"Fixture film","type":"movie","guid":"plex://movie/fixture","viewCount":0}]}}`
		case "/api/v2":
			data := map[string]string{
				"get_tautulli_info": `{"tautulli_version":"v2.18.2"}`,
				"get_server_info":   `{"pms_identifier":"server-a"}`,
				"get_users":         `[{"user_id":77,"is_active":1,"keep_history":1}]`,
				"get_history":       `{"recordsFiltered":1,"data":[{"row_id":1,"user_id":77,"rating_key":42,"media_type":"movie","guid":"plex://movie/fixture","percent_complete":50,"watched_status":0.5,"live":0}]}`,
			}[r.URL.Query().Get("cmd")]
			if data == "" {
				t.Fatalf("unexpected Tautulli command: %s", r.URL.RawQuery)
			}
			body = `{"response":{"result":"success","data":` + data + `}}`
		case "/api/oauth2/token":
			body = `{"access_token":"fixture-discord-token"}`
		case "/api/v10/users/@me":
			body = `{"id":"11111","username":"Alice"}`
		case "/api/v10/users/@me/guilds/23456/member":
			body = `{"user":{"id":"11111"},"pending":false}`
		default:
			t.Fatalf("unexpected upstream request: %s %s", r.Method, r.URL.Path)
		}
		return response(200, body), nil
	})
	return s
}

func TestOpenAPIRejectsResponseDrift(t *testing.T) {
	doc := loadContract(t)
	s := contractFixture(t)
	cookie, _ := seedPlexOnly(t, s, "77")
	baseline := call(s, "GET", "/api/v1/recent", cookie, "", "")
	if err := validateContractResponse(doc, "GET", "/api/v1/recent", baseline); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"missing required", func(v map[string]any) { delete(v, "items") }},
		{"wrong type", func(v map[string]any) { v["offset"] = "0" }},
		{"null array", func(v map[string]any) { v["items"] = nil }},
		{"nested ref", func(v map[string]any) { delete(v["items"].([]any)[0].(map[string]any), "title") }},
		{"unknown property", func(v map[string]any) { v["privateToken"] = "must-not-leak" }},
		{"unknown nested property", func(v map[string]any) { v["items"].([]any)[0].(map[string]any)["privateToken"] = "must-not-leak" }},
		{"invalid date", func(v map[string]any) { v["updatedAt"] = "2026-02-30T12:00:00Z" }},
		{"non UTC", func(v map[string]any) { v["updatedAt"] = "2026-10-16T12:00:00+02:00" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(baseline.Body.Bytes(), &value); err != nil {
				t.Fatal(err)
			}
			tc.edit(value)
			w := httptest.NewRecorder()
			jsonOut(w, 200, value)
			if err := validateContractResponse(doc, "GET", "/api/v1/recent", w); err == nil {
				t.Fatal("contract drift was accepted")
			}
		})
	}
	for name, edit := range map[string]func(map[string]any){
		"nested enum":         func(item map[string]any) { item["status"] = "not-a-request-status" },
		"oneOf invalid date":  func(item map[string]any) { item["createdAt"] = "2026-02-30T12:00:00Z" },
		"oneOf non UTC date":  func(item map[string]any) { item["createdAt"] = "2026-10-16T12:00:00+02:00" },
		"null unknown date":   func(item map[string]any) { item["createdAt"] = nil },
		"missing nested date": func(item map[string]any) { delete(item, "createdAt") },
	} {
		t.Run(name, func(t *testing.T) {
			w := call(s, "GET", "/api/v1/requests", cookie, "", "")
			if err := validateContractResponse(doc, "GET", "/api/v1/requests", w); err != nil {
				t.Fatal(err)
			}
			var value map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
				t.Fatal(err)
			}
			edit(value["items"].([]any)[0].(map[string]any))
			mutated := httptest.NewRecorder()
			jsonOut(mutated, 200, value)
			if err := validateContractResponse(doc, "GET", "/api/v1/requests", mutated); err == nil {
				t.Fatal("invalid request response was accepted")
			}
		})
	}
}

func TestOpenAPIEmptyAndUnknownResponses(t *testing.T) {
	doc := loadContract(t)
	for _, tc := range []struct {
		name, path, upstreamPath, reply string
		items                           int
	}{
		{"empty services", "/api/v1/services", "", "", 0},
		{"empty requests", "/api/v1/requests", "/api/v1/user/42/requests", `{"pageInfo":{"results":0},"results":[]}`, 0},
		{"empty recent", "/api/v1/recent", "/library/recentlyAdded", `{"MediaContainer":{"size":0,"totalSize":0,"Metadata":[]}}`, 0},
		{"empty departures", "/api/v1/departures", "/api/v2", `{"response":{"result":"success","data":{"recordsFiltered":0,"data":[]}}}`, 0},
		{"disabled reminders", "/api/v1/reminders", "", "", 0},
		{"unknown request", "/api/v1/requests", "/api/v1/user/42/requests", `{"pageInfo":{"results":1},"results":[{"id":1,"type":"tv","status":999,"createdAt":"unknown","requestedBy":{"id":42},"media":{"tmdbId":100,"status":999}}]}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := contractFixture(t)
			s.Config.Services = []config.Service{}
			base := s.Upstream.HTTP.Transport
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == tc.upstreamPath && (tc.upstreamPath != "/api/v2" || r.URL.Query().Get("cmd") == "get_history") {
					return response(200, tc.reply), nil
				}
				return base.RoundTrip(r)
			})
			cookie, _ := seedPlexOnly(t, s, "77")
			w := call(s, "GET", tc.path, cookie, "", "")
			if w.Code != 200 {
				t.Fatalf("response %d: %s", w.Code, w.Body)
			}
			if err := validateContractResponse(doc, "GET", tc.path, w); err != nil {
				t.Fatal(err)
			}
			var page struct{ Items []map[string]any }
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != tc.items {
				t.Fatalf("incorrect fixture: %s", w.Body)
			}
			if tc.name == "unknown request" && (page.Items[0]["status"] != "unknown" || page.Items[0]["createdAt"] != "") {
				t.Fatalf("upstream uncertainty lost: %s", w.Body)
			}
		})
	}
}
