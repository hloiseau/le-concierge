package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"
)

func downloadarrAuthorizeFixture() (DownloadarrPin, Grant) {
	return DownloadarrPin{URL: "https://app.plex.tv/auth#?clientID=downloadarr-fixture&code=fixture-strong-pin&context%5Bdevice%5D%5Bproduct%5D=LibraryDownloadarr",
			FlowID: downloadarrTestToken, ExpiresAt: time.Now().Add(5 * time.Minute)},
		Grant{AccountToken: "personal-account-secret", ClientID: "hub-client", ServerToken: "resource-secret", ExpiresAt: time.Now().Add(time.Hour)}
}

func TestDownloadarrAuthorizeClaimsStrongPINWithoutCookiesOrTokenDisclosure(t *testing.T) {
	pin, plex := downloadarrAuthorizeFixture()
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse("https://plex.tv/")
	jar.SetCookies(u, []*http.Cookie{{Name: "other-account", Value: "private-cookie"}})
	calls := 0
	c := &Client{HTTP: &http.Client{Jar: jar, Timeout: time.Hour, Transport: downloadarrRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://plex.tv/api/v2/pins/link" || r.Method != "PUT" || r.Header.Get("X-Plex-Token") != plex.AccountToken {
			t.Fatal("unexpected claim destination or credential")
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("X-Plex-Client-Identifier") != "downloadarr-fixture" ||
			r.Header.Get("X-Plex-Product") != "LibraryDownloadarr" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatal("wrong claim headers or browser credential forwarded")
		}
		body, _ := io.ReadAll(r.Body)
		values, err := url.ParseQuery(string(body))
		if err != nil || len(values) != 1 || values.Get("code") != "fixture-strong-pin" || strings.Contains(string(body), "secret") {
			t.Fatal("claim body must contain only the form-encoded strong PIN")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 6*time.Second {
			t.Fatal("PIN claim has no bounded deadline")
		}
		return &http.Response{StatusCode: 204, Header: http.Header{"Set-Cookie": {"downloadarr=never-store"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}}
	if err := c.DownloadarrAuthorize(context.Background(), pin, plex); err != nil || calls != 1 {
		t.Fatalf("authorize: %v, calls %d", err, calls)
	}
	if cookies := jar.Cookies(u); len(cookies) != 1 || cookies[0].Name != "other-account" {
		t.Fatal("claim changed the shared cookie jar")
	}
}

func TestDownloadarrAuthorizeRejectsUntrustedPINAndExpiredGrantBeforeNetwork(t *testing.T) {
	for _, mode := range []string{"host", "port", "scheme", "path", "escaped_path", "query", "empty_query", "userinfo", "product", "duplicate", "extra", "code", "short_code", "long_code", "client", "long_client", "flow", "pin_expired", "grant_expired", "token_empty", "token_control", "token_long"} {
		t.Run(mode, func(t *testing.T) {
			pin, plex := downloadarrAuthorizeFixture()
			switch mode {
			case "host":
				pin.URL = strings.Replace(pin.URL, "app.plex.tv", "elsewhere.invalid", 1)
			case "port":
				pin.URL = strings.Replace(pin.URL, "app.plex.tv", "app.plex.tv:443", 1)
			case "scheme":
				pin.URL = strings.Replace(pin.URL, "https:", "http:", 1)
			case "path":
				pin.URL = strings.Replace(pin.URL, "/auth#", "/other#", 1)
			case "escaped_path":
				pin.URL = strings.Replace(pin.URL, "/auth#", "/%61uth#", 1)
			case "query":
				pin.URL = strings.Replace(pin.URL, "#?", "?unexpected=true#?", 1)
			case "empty_query":
				pin.URL = strings.Replace(pin.URL, "#?", "?#?", 1)
			case "userinfo":
				pin.URL = strings.Replace(pin.URL, "//app", "//secret@app", 1)
			case "product":
				pin.URL = strings.Replace(pin.URL, "LibraryDownloadarr", "Other", 1)
			case "duplicate":
				pin.URL += "&code=another-strong-pin"
			case "extra":
				pin.URL += "&forwardUrl=https://other.invalid"
			case "code":
				pin.URL = strings.Replace(pin.URL, "fixture-strong-pin", "secret%0Ainjected", 1)
			case "short_code":
				pin.URL = strings.Replace(pin.URL, "fixture-strong-pin", "1234", 1)
			case "long_code":
				pin.URL = strings.Replace(pin.URL, "fixture-strong-pin", strings.Repeat("a", 129), 1)
			case "client":
				pin.URL = strings.Replace(pin.URL, "downloadarr-fixture", "bad%0D%0Aheader", 1)
			case "long_client":
				pin.URL = strings.Replace(pin.URL, "downloadarr-fixture", strings.Repeat("a", 201), 1)
			case "flow":
				pin.FlowID = "foreign"
			case "pin_expired":
				pin.ExpiresAt = time.Now().Add(-time.Second)
			case "grant_expired":
				plex.ExpiresAt = time.Now().Add(-time.Second)
			case "token_empty":
				plex.AccountToken = ""
			case "token_control":
				plex.AccountToken = "secret\r\nHeader: value"
			case "token_long":
				plex.AccountToken = strings.Repeat("a", 8193)
			}
			c := &Client{HTTP: &http.Client{Transport: downloadarrRoundTrip(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid input reached network")
				return nil, ErrUnavailable
			})}}
			if err := c.DownloadarrAuthorize(context.Background(), pin, plex); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("invalid claim accepted or leaked: %v", err)
			}
		})
	}
}

func TestDownloadarrAuthorizeFailureNeverFollowsRedirectsRetriesOrExposesResponse(t *testing.T) {
	for _, status := range []int{302, 307, 400, 401, 403, 404, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			pin, plex := downloadarrAuthorizeFixture()
			calls := 0
			c := &Client{HTTP: &http.Client{Transport: downloadarrRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://elsewhere.invalid/private"}}, Body: io.NopCloser(strings.NewReader("private-provider-secret"))}, nil
			})}}
			err := c.DownloadarrAuthorize(context.Background(), pin, plex)
			if err == nil || strings.Contains(err.Error(), "private") || calls != 1 {
				t.Fatalf("unsafe failure: %v, calls %d", err, calls)
			}
			if errors.Is(err, ErrDenied) != (status == 401 || status == 403) {
				t.Fatal("wrong denial classification")
			}
		})
	}
	pin, plex := downloadarrAuthorizeFixture()
	c := &Client{HTTP: &http.Client{Transport: downloadarrRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 2*1024*1024+1)))}, nil
	})}}
	if err := c.DownloadarrAuthorize(context.Background(), pin, plex); err == nil {
		t.Fatal("oversized response accepted")
	}
	plex.ExpiresAt = time.Now().Add(20 * time.Millisecond)
	c.HTTP.Transport = downloadarrRoundTrip(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	if err := c.DownloadarrAuthorize(context.Background(), pin, plex); err == nil {
		t.Fatal("expired in-flight grant accepted")
	}
}
