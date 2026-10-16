package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/store"
)

type subscriptionSource struct {
	available    bool
	denied       bool
	failure      bool
	self         bool
	beforeDetail func()
}

func subscriptionFixture(t *testing.T) (*Server, string, *subscriptionSource) {
	t.Helper()
	s := fixture(t)
	cookie := seed(t, s, "11111", true)
	f := &subscriptionSource{}
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Error("subscription mutated provider")
		}
		switch r.URL.Path {
		case "/api/v1/user":
			return response(200, `{"pageInfo":{"results":1},"results":[{"id":7,"plexId":11111}]}`), nil
		case "/api/v1/auth/me":
			if r.Header.Get("X-Api-User") != "7" {
				t.Error("missing Seerr user context")
			}
			return response(200, `{"id":7}`), nil
		case "/api/v1/movie/12":
			if f.beforeDetail != nil {
				f.beforeDetail()
			}
			if f.failure {
				return response(500, "private detail"), nil
			}
			requestedBy := 8
			if f.self {
				requestedBy = 7
			}
			key := ""
			status := 2
			if f.available {
				key = "42"
				status = 5
			}
			return response(200, fmt.Sprintf(`{"id":12,"title":"Shared film","mediaInfo":{"tmdbId":12,"mediaType":"movie","ratingKey":%q,"status":%d,"requests":[{"status":2,"requestedBy":{"id":%d,"displayName":"Private requester"}}]}}`, key, status, requestedBy)), nil
		case "/identity":
			return response(200, `{"MediaContainer":{"machineIdentifier":"server-a"}}`), nil
		case "/library/metadata/42":
			if r.Header.Get("X-Plex-Token") != "resource-11111" {
				t.Error("availability used another token")
			}
			if f.denied {
				return response(404, "hidden"), nil
			}
			return response(200, `{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"42","type":"movie","guid":"plex://movie/x","Guid":[{"id":"tmdb://12"}],"Media":[{"Part":[{"key":"/library/parts/1/file.mkv","exists":true,"accessible":true}]}]}]}}`), nil
		default:
			t.Errorf("unexpected subscription upstream: %s", r.URL.Path)
			return response(500, "unexpected"), nil
		}
	})
	return s, cookie, f
}

func TestSubscriptionCreateListDeleteOwnership(t *testing.T) {
	s, cookie, f := subscriptionFixture(t)
	body := `{"type":"movie","tmdbId":12,"channel":"hub"}`
	for range 2 {
		w := call(s, "POST", "/api/v1/subscriptions", cookie, s.Config.PublicOrigin, body)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"following"`) || strings.Contains(w.Body.String(), "Private requester") {
			t.Fatalf("create: %d %s", w.Code, w.Body)
		}
	}
	items, total, err := s.Store.Subscriptions(context.Background(), "11111", 0)
	if err != nil || total != 1 {
		t.Fatalf("duplicate follow: %d %v", total, err)
	}
	bob := seed(t, s, "22222", true)
	path := fmt.Sprintf("/api/v1/subscriptions/%d", items[0].ID)
	if w := call(s, "DELETE", path, bob, s.Config.PublicOrigin, ""); w.Code != 404 {
		t.Fatalf("other owner deleted: %d %s", w.Code, w.Body)
	}
	if w := call(s, "GET", "/api/v1/subscriptions", bob, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("other owner list: %d %s", w.Code, w.Body)
	}
	f.available = true
	for range 2 {
		w := call(s, "GET", "/api/v1/subscriptions", cookie, "", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"available"`) || !strings.Contains(w.Body.String(), "https://app.plex.tv/") {
			t.Fatalf("available list: %d %s", w.Code, w.Body)
		}
	}
	var events int
	if err := s.Store.DB.QueryRow(`SELECT COUNT(*) FROM subscription_availability`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("availability event dedup: %d %v", events, err)
	}
	f.denied = true
	if w := call(s, "GET", "/api/v1/subscriptions", cookie, "", ""); w.Code != 200 || strings.Contains(w.Body.String(), "Shared film") {
		t.Fatalf("revoked content leaked: %d %s", w.Code, w.Body)
	} else if !strings.Contains(w.Body.String(), `"status":"unavailable"`) || !strings.Contains(w.Body.String(), fmt.Sprintf(`"id":%d`, items[0].ID)) {
		t.Fatalf("revoked follow cannot be unsubscribed: %s", w.Body)
	}
	if w := call(s, "DELETE", path, cookie, s.Config.PublicOrigin, ""); w.Code != 200 {
		t.Fatalf("owner unsubscribe: %d %s", w.Code, w.Body)
	}
	if err := s.Store.DB.QueryRow(`SELECT COUNT(*) FROM subscription_availability`).Scan(&events); err != nil || events != 0 {
		t.Fatalf("unsubscribe left event: %d %v", events, err)
	}
}

func TestSubscriptionAlreadyAvailableAndOwnRequestDedup(t *testing.T) {
	s, cookie, f := subscriptionFixture(t)
	f.available = true
	body := `{"type":"movie","tmdbId":12,"channel":"hub"}`
	w := call(s, "POST", "/api/v1/subscriptions", cookie, s.Config.PublicOrigin, body)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"already_available"`) || strings.Contains(w.Body.String(), `"subscription"`) {
		t.Fatalf("already available: %d %s", w.Code, w.Body)
	}
	_, total, err := s.Store.Subscriptions(context.Background(), "11111", 0)
	if err != nil || total != 0 {
		t.Fatalf("already available queued false alert: %d %v", total, err)
	}
	f.available = false
	f.self = true
	w = call(s, "POST", "/api/v1/subscriptions", cookie, s.Config.PublicOrigin, body)
	if w.Code != 200 {
		t.Fatalf("follow own request: %d %s", w.Code, w.Body)
	}
	f.available = true
	w = call(s, "GET", "/api/v1/subscriptions", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"available"`) {
		t.Fatalf("own request availability: %d %s", w.Code, w.Body)
	}
	var events int
	if err := s.Store.DB.QueryRow(`SELECT COUNT(*) FROM subscription_availability`).Scan(&events); err != nil || events != 0 {
		t.Fatalf("own request generated duplicate event: %d %v", events, err)
	}
}

func TestSubscriptionTrustBoundaries(t *testing.T) {
	s, cookie, f := subscriptionFixture(t)
	valid := `{"type":"movie","tmdbId":12,"channel":"hub"}`
	if w := call(s, "POST", "/api/v1/subscriptions", cookie, "https://attacker.invalid", valid); w.Code != 403 {
		t.Fatalf("origin: %d", w.Code)
	}
	if w := call(s, "GET", "/api/v1/subscriptions", "", "", ""); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	for _, body := range []string{`{"type":"movie","tmdbId":12,"channel":"hub","userId":"22222"}`, valid + ` {}`, `{"type":"movie","tmdbId":12,"season":1,"channel":"hub"}`, `{"type":"tv","tmdbId":12,"season":-1,"channel":"hub"}`} {
		if w := call(s, "POST", "/api/v1/subscriptions", cookie, s.Config.PublicOrigin, body); w.Code != 400 {
			t.Fatalf("invalid body accepted: %s %d", body, w.Code)
		}
	}
	if w := call(s, "POST", "/api/v1/subscriptions", cookie, s.Config.PublicOrigin, strings.Replace(valid, "hub", "discord", 1)); w.Code != 409 {
		t.Fatalf("unimplemented Discord channel accepted: %d", w.Code)
	}
	if w := call(s, "GET", "/api/v1/subscriptions?userId=22222", cookie, "", ""); w.Code != 400 {
		t.Fatalf("identity query accepted: %d", w.Code)
	}
	f.beforeDetail = func() { s.mu.Lock(); delete(s.grants, "11111"); s.mu.Unlock() }
	if w := call(s, "POST", "/api/v1/subscriptions", cookie, s.Config.PublicOrigin, valid); w.Code != 404 {
		t.Fatalf("stale grant: %d %s", w.Code, w.Body)
	}
	_, total, _ := s.Store.Subscriptions(context.Background(), "11111", 0)
	if total != 0 {
		t.Fatal("revoked grant persisted subscription")
	}
}

func TestSubscriptionOutageNeverReturnsSavedMedia(t *testing.T) {
	s, cookie, f := subscriptionFixture(t)
	_, err := s.Store.SaveSubscription(context.Background(), "11111", store.SubscriptionTarget{Type: "movie", TMDBID: 12}, "hub")
	if err != nil {
		t.Fatal(err)
	}
	f.failure = true
	w := call(s, "GET", "/api/v1/subscriptions", cookie, "", "")
	if w.Code != 503 || strings.Contains(w.Body.String(), "Shared film") {
		t.Fatalf("outage reused media: %d %s", w.Code, w.Body)
	}
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
}
