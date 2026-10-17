package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const downloadarrTestID = "12345678-1234-4234-8234-123456789abc"

var downloadarrTestToken = strings.Repeat("a", 64)
var downloadarrTestRevision = strings.Repeat("b", 40)

func downloadarrContractFixture() map[string]any {
	return map[string]any{"contractVersion": 1, "buildRevision": downloadarrTestRevision,
		"user": map[string]any{"plexId": "42"}, "plexServerId": "fixture-server", "sessionExpiresAt": time.Now().Add(time.Hour).UnixMilli(),
		"capabilities": map[string]any{"qualityOptions": []string{"720p-2", "720p-4", "1080p-8"}, "streamOptions": true,
			"preparedDownloads": true, "singleUseTickets": true, "queuePersistence": "memory"}}
}
func downloadarrJobFixture() map[string]any {
	return map[string]any{"id": downloadarrTestID, "title": "Fixture film", "ratingKey": "12", "createdAt": time.Now().Add(-time.Minute).UnixMilli(),
		"season": false, "quality": "720p-2", "filename": "Fixture film - 720p-2.mp4", "state": "preparing", "readyCount": 0,
		"fileCount": 1, "stage": "waiting", "progress": nil, "expiresAt": time.Now().Add(time.Hour).UnixMilli(),
		"audioLabel": "Plex selection", "subtitleLabel": "None"}
}
func downloadarrOptionsFixture() map[string]any {
	return map[string]any{"audio": []DownloadStreamOption{{"plex", "Plex selection"}, {"stream:3", "French"}},
		"subtitle": []DownloadStreamOption{{"plex", "Plex selection"}, {"none", "None"}, {"stream:4", "English"}}, "season": false, "fileCount": 1}
}

func TestDownloadarrPersonalWorkflow(t *testing.T) {
	ctx := context.Background()
	polls := 0
	var called []string
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = append(called, r.Method+" "+r.URL.Path)
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-Plex-Token") != "" || strings.Contains(r.URL.RawQuery, downloadarrTestToken) {
			t.Error("credential in cookie/query")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/prefix/api/auth/plex/pin":
			if r.Method != "POST" || r.Header.Get("Authorization") != "" {
				t.Error("pin authentication")
			}
			json.NewEncoder(w).Encode(map[string]any{"flowId": downloadarrTestToken, "id": 1, "code": "fixture-strong-pin", "url": "https://app.plex.tv/auth#?clientID=fixture&code=fixture-strong-pin&context%5Bdevice%5D%5Bproduct%5D=LibraryDownloadarr"})
		case "/prefix/api/auth/plex/authenticate":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if r.Method != "POST" || body["flowId"] != downloadarrTestToken {
				t.Error("wrong flow request")
			}
			polls++
			if polls == 1 {
				w.WriteHeader(202)
				io.WriteString(w, `{"pending":true}`)
			} else {
				json.NewEncoder(w).Encode(map[string]string{"token": downloadarrTestToken})
			}
		case "/prefix/api/integrations/concierge":
			if r.Header.Get("Authorization") != "Bearer "+downloadarrTestToken {
				t.Error("missing personal bearer")
			}
			json.NewEncoder(w).Encode(downloadarrContractFixture())
		case "/prefix/api/downloads/options":
			if r.URL.Query().Get("ratingKey") != "12" || r.URL.Query().Get("quality") != "720p-2" || r.URL.Query().Get("season") != "false" || r.URL.Query().Get("partKey") != "/library/parts/3/4/file.mkv" {
				t.Error("wrong options query")
			}
			json.NewEncoder(w).Encode(downloadarrOptionsFixture())
		case "/prefix/api/downloads":
			if r.Method == "POST" {
				var body DownloadInput
				json.NewDecoder(r.Body).Decode(&body)
				if body.Audio != "stream:3" || body.Subtitle != "none" || body.PartKey != "/library/parts/3/4/file.mkv" {
					t.Error("lost explicit choices")
				}
				w.WriteHeader(202)
				json.NewEncoder(w).Encode(downloadarrJobFixture())
			} else {
				json.NewEncoder(w).Encode(map[string]any{"jobs": []any{downloadarrJobFixture()}})
			}
		case "/prefix/api/downloads/" + downloadarrTestID + "/ticket":
			if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+downloadarrTestToken {
				t.Error("ticket request")
			}
			json.NewEncoder(w).Encode(map[string]string{"ticket": strings.Repeat("c", 64)})
		case "/prefix/api/downloads/" + downloadarrTestID + "/file":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if r.Method != "POST" || r.Header.Get("Authorization") != "" || body["ticket"] != strings.Repeat("c", 64) {
				t.Error("file must use body ticket only")
			}
			w.Header().Set("Content-Type", "video/mp4")
			io.WriteString(w, "fixture bytes")
		case "/prefix/api/downloads/" + downloadarrTestID:
			if r.Method != "DELETE" {
				t.Error("wrong cancellation method")
			}
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer source.Close()
	c := New()
	c.HTTP.Jar, _ = cookiejar.New(nil)
	u, _ := url.Parse(source.URL)
	c.HTTP.Jar.SetCookies(u, []*http.Cookie{{Name: "admin", Value: "not-forwarded"}})
	plexCalls := 0
	c.HTTP.Transport = downloadarrRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "plex.tv" {
			return http.DefaultTransport.RoundTrip(r)
		}
		plexCalls++
		if r.URL.String() != "https://plex.tv/api/v2/pins/link" || r.Header.Get("X-Plex-Token") != "personal-account" ||
			r.Method != "PUT" || r.Header.Get("X-Plex-Client-Identifier") != "fixture" || r.Header.Get("X-Plex-Product") != "LibraryDownloadarr" {
			t.Fatal("PIN claim used the wrong account, application or target")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	base := source.URL + "/prefix/"
	before := time.Now()
	pin, err := c.DownloadarrStart(ctx, base)
	if err != nil || pin.FlowID != downloadarrTestToken || pin.ExpiresAt.Before(before.Add(5*time.Minute)) || pin.ExpiresAt.After(time.Now().Add(5*time.Minute)) {
		t.Fatalf("pin: %+v %v", pin, err)
	}
	serialized, _ := json.Marshal(pin)
	if strings.Contains(string(serialized), downloadarrTestToken) {
		t.Fatal("flow serialized")
	}
	if err := c.DownloadarrAuthorize(ctx, pin, Grant{AccountToken: "personal-account", ExpiresAt: time.Now().Add(time.Hour)}); err != nil || plexCalls != 1 {
		t.Fatalf("authorize: %v, requests %d", err, plexCalls)
	}
	if token, pending, err := c.DownloadarrAuthenticate(ctx, base, pin.FlowID); err != nil || !pending || token != "" {
		t.Fatalf("pending: %v %v", pending, err)
	}
	token, pending, err := c.DownloadarrAuthenticate(ctx, base, pin.FlowID)
	if err != nil || pending || token != downloadarrTestToken {
		t.Fatalf("authenticated: %v", err)
	}
	contract, err := c.DownloadarrVerify(ctx, base, token, "42", "fixture-server", downloadarrTestRevision)
	if err != nil || len(contract.QualityOptions) != 3 || contract.ExpiresAt.Location() != time.UTC {
		t.Fatalf("contract: %+v %v", contract, err)
	}
	input := DownloadInput{RatingKey: "12", Quality: "720p-2", PartKey: "/library/parts/3/4/file.mkv", Audio: "stream:3", Subtitle: "none"}
	options, err := c.DownloadarrOptions(ctx, base, token, input)
	if err != nil || len(options.Audio) != 2 {
		t.Fatalf("options: %+v %v", options, err)
	}
	job, err := c.DownloadarrCreate(ctx, base, token, input)
	if err != nil || job.Progress != nil || job.CreatedAt.Location() != time.UTC {
		t.Fatalf("create: %+v %v", job, err)
	}
	jobs, err := c.DownloadarrJobs(ctx, base, token)
	if err != nil || len(jobs) != 1 || jobs[0].Progress != nil {
		t.Fatalf("jobs: %+v %v", jobs, err)
	}
	ticket, err := c.DownloadarrTicket(ctx, base, token, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.DownloadarrFile(ctx, base, job.ID, ticket)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(data) != "fixture bytes" {
		t.Fatalf("file: %q %v", data, err)
	}
	if err := c.DownloadarrCancel(ctx, base, token, job.ID); err != nil {
		t.Fatal(err)
	}
	if len(called) != 10 {
		t.Fatalf("unexpected request/retry count %d", len(called))
	}
}

func TestDownloadarrContractRejectsUnprovedIdentityVersionCapabilitiesAndDeadline(t *testing.T) {
	for _, mode := range []string{"identity", "server", "revision", "unversioned", "version", "expired", "milliseconds", "future", "streams", "downloads", "tickets", "persistence", "quality", "duplicate", "empty"} {
		t.Run(mode, func(t *testing.T) {
			body := downloadarrContractFixture()
			caps := body["capabilities"].(map[string]any)
			switch mode {
			case "identity":
				body["user"] = map[string]string{"plexId": "43"}
			case "server":
				body["plexServerId"] = "other"
			case "revision":
				body["buildRevision"] = strings.Repeat("c", 40)
			case "unversioned":
				body["buildRevision"] = nil
			case "version":
				body["contractVersion"] = 2
			case "expired":
				body["sessionExpiresAt"] = time.Now().Add(-time.Minute).UnixMilli()
			case "milliseconds":
				body["sessionExpiresAt"] = time.Now().Add(time.Hour).Unix()
			case "future":
				body["sessionExpiresAt"] = time.Now().Add(48 * time.Hour).UnixMilli()
			case "streams":
				caps["streamOptions"] = false
			case "downloads":
				caps["preparedDownloads"] = false
			case "tickets":
				caps["singleUseTickets"] = false
			case "persistence":
				caps["queuePersistence"] = "sqlite"
			case "quality":
				caps["qualityOptions"] = []string{"original"}
			case "duplicate":
				caps["qualityOptions"] = []string{"720p-2", "720p-2"}
			case "empty":
				caps["qualityOptions"] = []string{}
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(body) }))
			defer s.Close()
			_, err := New().DownloadarrVerify(context.Background(), s.URL, downloadarrTestToken, "42", "fixture-server", downloadarrTestRevision)
			if err == nil {
				t.Fatal("invalid contract accepted")
			}
			if (mode == "identity" || mode == "server" || mode == "expired") && !errors.Is(err, ErrDenied) {
				t.Fatalf("wrong classification %v", err)
			}
		})
	}
}

func TestDownloadarrJobsNormalizeAndRejectInvalidSnapshots(t *testing.T) {
	for _, mode := range []string{"null", "zero", "ready", "error", "progress", "missing_progress", "state", "stage", "false_ready", "count", "id", "filename", "expiry", "too_many", "duplicates", "missing_jobs"} {
		t.Run(mode, func(t *testing.T) {
			job := downloadarrJobFixture()
			jobs := []any{job}
			switch mode {
			case "zero":
				job["progress"] = 0
			case "ready":
				job["state"] = "ready"
				job["stage"] = "ready"
				job["progress"] = 100
				job["readyCount"] = 1
			case "error":
				job["state"] = "error"
				job["error"] = "secret https://private.invalid?token=secret"
				job["title"] = "Fixture\r\nfilm"
			case "progress":
				job["progress"] = 101
			case "missing_progress":
				delete(job, "progress")
			case "state":
				job["state"] = "completed"
			case "stage":
				job["stage"] = "unknown"
			case "false_ready":
				job["state"] = "ready"
			case "count":
				job["fileCount"] = 101
			case "id":
				job["id"] = "../../private"
			case "filename":
				job["filename"] = "../private.mp4"
			case "expiry":
				job["expiresAt"] = time.Now().Add(48 * time.Hour).UnixMilli()
			case "too_many":
				jobs = []any{job, job, job}
			case "duplicates":
				jobs = []any{job, job}
			case "missing_jobs":
				jobs = nil
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(map[string]any{"jobs": jobs}) }))
			defer s.Close()
			got, err := New().DownloadarrJobs(context.Background(), s.URL, downloadarrTestToken)
			if mode != "null" && mode != "zero" && mode != "ready" && mode != "error" {
				if err == nil {
					t.Fatal("invalid snapshot accepted")
				}
				return
			}
			if err != nil || len(got) != 1 {
				t.Fatalf("%+v %v", got, err)
			}
			if mode == "null" && got[0].Progress != nil || mode == "zero" && (got[0].Progress == nil || *got[0].Progress != 0) {
				t.Fatal("missing and zero progress conflated")
			}
			data, _ := json.Marshal(got)
			if strings.Contains(string(data), "secret") || strings.Contains(string(data), "private") || strings.Contains(got[0].Title, "\n") {
				t.Fatal("provider diagnostics leaked")
			}
		})
	}
}

func TestDownloadarrStrictOptionsAndInputs(t *testing.T) {
	input := DownloadInput{RatingKey: "12", Quality: "720p-2"}
	for _, mode := range []string{"valid", "season", "duplicates", "audio_none", "unknown_choice", "missing_plex", "wrong_scope", "too_many", "blank_label"} {
		t.Run(mode, func(t *testing.T) {
			body := downloadarrOptionsFixture()
			request := input
			switch mode {
			case "season":
				request.Season = true
				body["season"] = true
				body["fileCount"] = 12
				body["audio"] = []DownloadStreamOption{{"plex", "Plex"}, {"match:YWJj", "French"}}
				body["subtitle"] = []DownloadStreamOption{{"plex", "Plex"}, {"none", "None"}}
			case "duplicates":
				body["audio"] = []DownloadStreamOption{{"plex", "Plex"}, {"plex", "Plex"}}
			case "audio_none":
				body["audio"] = []DownloadStreamOption{{"plex", "Plex"}, {"none", "None"}}
			case "unknown_choice":
				body["audio"] = []DownloadStreamOption{{"plex", "Plex"}, {"https://private", "Bad"}}
			case "missing_plex":
				body["audio"] = []DownloadStreamOption{{"stream:3", "French"}}
			case "wrong_scope":
				body["audio"] = []DownloadStreamOption{{"plex", "Plex"}, {"match:YWJj", "French"}}
			case "too_many":
				body["fileCount"] = 101
			case "blank_label":
				body["audio"] = []DownloadStreamOption{{"plex", "\n"}}
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(body) }))
			defer s.Close()
			_, err := New().DownloadarrOptions(context.Background(), s.URL, downloadarrTestToken, request)
			if (mode == "valid" || mode == "season") != (err == nil) {
				t.Fatalf("unexpected options result %v", err)
			}
		})
	}
	for _, bad := range []DownloadInput{{RatingKey: "../12", Quality: "720p-2"}, {RatingKey: "12", Quality: "original"}, {RatingKey: "12", Quality: "720p-2", Audio: "none"}, {RatingKey: "12", Quality: "720p-2", PartKey: "https://private"}, {RatingKey: "12", Quality: "720p-2", Audio: "match:abc"}, {RatingKey: "12", Quality: "720p-2", Season: true, Audio: "stream:3"}} {
		if _, err := New().DownloadarrCreate(context.Background(), "http://unused.invalid", downloadarrTestToken, bad); err == nil {
			t.Fatalf("accepted invalid input %+v", bad)
		}
	}
}

func TestDownloadarrErrorsAreTypedBoundedAndNeverRetryMutation(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 409, 410, 429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", "90")
				w.WriteHeader(status)
				io.WriteString(w, "secret http://private.invalid?token=secret")
			}))
			defer s.Close()
			_, err := New().DownloadarrCreate(context.Background(), s.URL, downloadarrTestToken, DownloadInput{RatingKey: "12", Quality: "720p-2"})
			var typed *DownloadarrError
			if !errors.As(err, &typed) || typed.Status != status || strings.Contains(err.Error(), "secret") || calls != 1 {
				t.Fatalf("failure classification %v, calls %d", err, calls)
			}
			if (status == 429 || status == 503) && typed.RetryAfter != 90*time.Second {
				t.Fatal("retry delay")
			}
		})
	}
	for _, retry := range []string{"0", "-1", "301", "secret", "9999999999999999999999"} {
		resp := &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{retry}}}
		if downloadarrFailure(resp).(*DownloadarrError).RetryAfter != 0 {
			t.Fatal("unbounded retry delay")
		}
	}
}

func TestDownloadarrRejectsRedirectOversizedResponsesAndUnsafeAuthURL(t *testing.T) {
	redirectCalls := 0
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirectCalls++; w.WriteHeader(500) }))
	defer redirect.Close()
	for _, mode := range []string{"redirect", "oversize", "multiple_json", "unsafe_url", "unsafe_fragment", "expired_flow", "not_pending"} {
		t.Run(mode, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "redirect":
					http.Redirect(w, r, redirect.URL, 307)
				case "oversize":
					io.WriteString(w, strings.Repeat(" ", 2*1024*1024+1))
				case "multiple_json":
					io.WriteString(w, `{} {}`)
				case "unsafe_url":
					json.NewEncoder(w).Encode(map[string]string{"flowId": downloadarrTestToken, "url": "https://evil.invalid/auth#?code=secret"})
				case "unsafe_fragment":
					json.NewEncoder(w).Encode(map[string]string{"flowId": downloadarrTestToken, "url": "https://app.plex.tv/auth#?clientID=fixture&code=fixture&context%5Bdevice%5D%5Bproduct%5D=LibraryDownloadarr&forwardUrl=https://evil.invalid"})
				case "expired_flow":
					w.WriteHeader(400)
					io.WriteString(w, `{"error":"secret"}`)
				case "not_pending":
					w.WriteHeader(202)
					io.WriteString(w, `{"pending":false}`)
				}
			}))
			defer s.Close()
			var err error
			if mode == "expired_flow" || mode == "not_pending" {
				_, _, err = New().DownloadarrAuthenticate(context.Background(), s.URL, downloadarrTestToken)
			} else {
				_, err = New().DownloadarrStart(context.Background(), s.URL)
			}
			if err == nil {
				t.Fatal("unsafe response accepted")
			}
			if mode == "expired_flow" {
				var typed *DownloadarrError
				if !errors.As(err, &typed) || typed.Code != "flow_expired" {
					t.Fatal(err)
				}
			}
		})
	}
	if redirectCalls != 0 {
		t.Fatal("followed redirect")
	}
	for _, base := range []string{"file:///private", "https://user:pass@private.invalid", "https://private.invalid?token=secret", "https://private.invalid#secret"} {
		if _, err := New().DownloadarrStart(context.Background(), base); err == nil {
			t.Fatal("unsafe base accepted")
		}
	}
}

type downloadarrRoundTrip func(*http.Request) (*http.Response, error)

func (f downloadarrRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestDownloadarrFileHasNoGlobalTimeoutAndClosesContext(t *testing.T) {
	var requestContext context.Context
	c := &Client{HTTP: &http.Client{Timeout: time.Nanosecond, Transport: downloadarrRoundTrip(func(r *http.Request) (*http.Response, error) {
		requestContext = r.Context()
		if _, deadline := requestContext.Deadline(); deadline {
			t.Error("short client timeout applied to entire file")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/zip"}}, Body: io.NopCloser(strings.NewReader("fixture bytes"))}, nil
	})}}
	resp, err := c.DownloadarrFile(context.Background(), "http://fixture.invalid", downloadarrTestID, downloadarrTestToken)
	if err != nil || requestContext.Err() != nil {
		t.Fatalf("open stream %v", err)
	}
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if requestContext.Err() != context.Canceled {
		t.Fatal("closing response leaked request context")
	}
	for _, mediaType := range []string{"text/html", "application/json", "video/mp2t"} {
		c.HTTP.Transport = downloadarrRoundTrip(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{mediaType}}, Body: io.NopCloser(strings.NewReader("private"))}, nil
		})
		if _, err := c.DownloadarrFile(context.Background(), "http://fixture.invalid", downloadarrTestID, downloadarrTestToken); err == nil {
			t.Fatal("unexpected file type accepted")
		}
	}
}

func TestDownloadarrBoundariesBeforeNetworkAndCancellation(t *testing.T) {
	calls := 0
	c := &Client{HTTP: &http.Client{Transport: downloadarrRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}}
	ctx := context.Background()
	for _, bearer := range []string{"", "admin-cookie", "bad\r\nHeader: leaked"} {
		if _, err := c.DownloadarrJobs(ctx, "http://fixture.invalid", bearer); !errors.Is(err, ErrDenied) {
			t.Fatal("invalid bearer accepted", err)
		}
	}
	if _, err := c.DownloadarrTicket(ctx, "http://fixture.invalid", downloadarrTestToken, "../escape"); err == nil {
		t.Fatal("invalid job accepted")
	}
	if _, err := c.DownloadarrFile(ctx, "http://fixture.invalid", downloadarrTestID, "secret"); err == nil {
		t.Fatal("invalid ticket accepted")
	}
	if calls != 0 {
		t.Fatal("invalid arguments reached source")
	}
	for _, size := range []int{1, 1500, 1501} {
		choice := "match:" + strings.Repeat("a", size)
		if validDownloadSelection(choice) != (size <= 1500) {
			t.Fatal("choice length boundary")
		}
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.DownloadarrFile(cancelCtx, "http://fixture.invalid", downloadarrTestID, downloadarrTestToken); err == nil {
		t.Fatal("cancelled stream accepted")
	}
	if _, err := c.DownloadarrJobs(cancelCtx, "http://fixture.invalid", downloadarrTestToken); err == nil {
		t.Fatal("cancelled JSON accepted")
	}
	if calls != 2 {
		t.Fatal("unexpected retry", calls)
	}
}
