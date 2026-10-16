package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/notifications"
	"github.com/hloiseau/le-concierge/internal/store"
)

func availabilityFixture(t *testing.T) (*Server, string, store.User, store.Subscription, *subscriptionSource) {
	t.Helper()
	s, cookie, f := subscriptionFixture(t)
	s.Config.DiscordDMsEnabled = true
	s.Discord = notifications.NewDiscord("mock-token")
	p, err := s.Store.GetReminderPreferences(t.Context(), "11111")
	if err != nil {
		t.Fatal(err)
	}
	p.Enabled = true
	p.DiscordDM = true
	p.QuietStartHour = 0
	p.QuietEndHour = 0
	if err = s.Store.SaveReminderPreferences(t.Context(), "11111", p); err != nil {
		t.Fatal(err)
	}
	u, err := s.Store.User(t.Context(), "11111")
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.Store.SaveSubscription(t.Context(), "11111", store.SubscriptionTarget{Type: "movie", TMDBID: 12}, "discord")
	if err != nil {
		t.Fatal(err)
	}
	return s, cookie, u, item, f
}

func mockAvailabilityDiscord(t *testing.T, s *Server, beforeChannel func(), status int) (*int, *string) {
	t.Helper()
	messages := 0
	payload := ""
	s.Discord.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bot mock-token" {
			t.Error("wrong private transport")
		}
		switch r.URL.Path {
		case "/api/v10/users/@me/channels":
			if beforeChannel != nil {
				beforeChannel()
			}
			return response(200, `{"id":"44444","type":1,"recipients":[{"id":"11111"}]}`), nil
		case "/api/v10/channels/44444/messages":
			messages++
			body, _ := io.ReadAll(r.Body)
			payload = string(body)
			if status == 403 {
				return response(403, `{"code":50007}`), nil
			}
			if status == 429 {
				return response(429, `{"retry_after":120}`), nil
			}
			return response(status, `{"id":"55555","channel_id":"44444"}`), nil
		default:
			t.Errorf("unexpected Discord path %s", r.URL.Path)
			return response(500, ""), nil
		}
	})
	return &messages, &payload
}

func TestAvailabilityWorkerWaitsForImportAndDeliversOnePrivateEvent(t *testing.T) {
	s, _, u, _, f := availabilityFixture(t)
	messages, payload := mockAvailabilityDiscord(t, s, nil, 200)
	if err := s.refreshAvailability(t.Context(), u, time.Now()); err != nil {
		t.Fatal(err)
	}
	inbox, err := s.Store.AvailabilityInbox(t.Context(), u.ID)
	if err != nil || len(inbox) != 0 {
		t.Fatalf("alert before import: %+v %v", inbox, err)
	}
	f.available = true
	for range 2 {
		if err = s.refreshAvailability(t.Context(), u, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err = s.deliverAvailabilityDiscord(t.Context(), u); err != nil {
			t.Fatal(err)
		}
	}
	if *messages != 1 || !strings.Contains(*payload, "Shared film") || strings.Contains(*payload, "Private requester") || !strings.Contains(*payload, `"parse":[]`) || !strings.Contains(*payload, `"enforce_nonce":true`) {
		t.Fatalf("unsafe/duplicate private delivery: %d %s", *messages, *payload)
	}
	inbox, err = s.Store.AvailabilityInbox(t.Context(), u.ID)
	if err != nil || len(inbox) != 1 {
		t.Fatalf("missing durable inbox: %+v %v", inbox, err)
	}
}

func TestAvailabilityDeliveryResumesAfterProcessReplacementAndPlexLogin(t *testing.T) {
	s, _, u, _, f := availabilityFixture(t)
	f.available = true
	if err := s.refreshAvailability(t.Context(), u, time.Now()); err != nil {
		t.Fatal(err)
	}
	resumed := New(s.Config, s.Store)
	resumed.Upstream = s.Upstream
	resumed.Discord = notifications.NewDiscord("mock-token")
	messages, _ := mockAvailabilityDiscord(t, resumed, nil, 200)
	if len(resumed.grants) != 0 {
		t.Fatal("tokens survived process replacement")
	}
	seed(t, resumed, "11111", true)
	if err := resumed.deliverAvailabilityDiscord(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	if *messages != 1 {
		t.Fatalf("durable job lost after renewed login: %d", *messages)
	}
}

func TestAvailabilityRevocationBeforeMessagePostSendsNothing(t *testing.T) {
	for _, mode := range []string{"unsubscribe", "grant", "media", "own_request", "preferences", "channel"} {
		t.Run(mode, func(t *testing.T) {
			s, _, u, item, f := availabilityFixture(t)
			f.available = true
			if err := s.refreshAvailability(t.Context(), u, time.Now()); err != nil {
				t.Fatal(err)
			}
			messages, _ := mockAvailabilityDiscord(t, s, func() {
				var err error
				switch mode {
				case "unsubscribe":
					err = s.Store.DeleteSubscription(t.Context(), u.ID, item.ID)
				case "grant":
					s.mu.Lock()
					delete(s.grants, u.ID)
					s.mu.Unlock()
				case "media":
					f.denied = true
				case "own_request":
					f.self = true
				case "preferences":
					err = s.Store.Preferences(t.Context(), u.ID, false)
				case "channel":
					err = s.Store.UpdateSubscriptionChannel(t.Context(), u.ID, item.ID, "hub")
				}
				if err != nil {
					t.Fatal(err)
				}
			}, 200)
			_ = s.deliverAvailabilityDiscord(t.Context(), u)
			if *messages != 0 {
				t.Fatalf("revocation %s still sent private content", mode)
			}
		})
	}
}

func TestAvailabilityClosedDMStopsAndRateLimitPersists(t *testing.T) {
	for _, status := range []int{403, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s, _, u, _, f := availabilityFixture(t)
			f.available = true
			if err := s.refreshAvailability(t.Context(), u, time.Now()); err != nil {
				t.Fatal(err)
			}
			messages, _ := mockAvailabilityDiscord(t, s, nil, status)
			if err := s.deliverAvailabilityDiscord(t.Context(), u); err != nil {
				t.Fatal(err)
			}
			if err := s.deliverAvailabilityDiscord(t.Context(), u); err != nil {
				t.Fatal(err)
			}
			if *messages != 1 {
				t.Fatalf("failure caused immediate repeated send: %d", *messages)
			}
			var state, code string
			var attempts int
			if err := s.Store.DB.QueryRow(`SELECT state,last_error,attempts FROM availability_discord_deliveries`).Scan(&state, &code, &attempts); err != nil {
				t.Fatal(err)
			}
			if attempts != 1 || (status == 403 && (state != "failed" || code != "dm_unavailable")) || (status == 429 && (state != "queued" || code != "rate_limited")) {
				t.Fatalf("failure state %s %s attempts=%d", state, code, attempts)
			}
			inbox, err := s.Store.AvailabilityInbox(t.Context(), u.ID)
			if err != nil || len(inbox) != 1 {
				t.Fatalf("transport failure erased Hub inbox: %+v %v", inbox, err)
			}
		})
	}
}

func TestAvailabilityTransientRevalidationFailureRetriesWithoutSending(t *testing.T) {
	s, _, u, _, f := availabilityFixture(t)
	f.available = true
	if err := s.refreshAvailability(t.Context(), u, time.Now()); err != nil {
		t.Fatal(err)
	}
	messages, _ := mockAvailabilityDiscord(t, s, func() { f.failure = true }, 200)
	if err := s.deliverAvailabilityDiscord(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	var state, code string
	if err := s.Store.DB.QueryRow(`SELECT state,last_error FROM availability_discord_deliveries`).Scan(&state, &code); err != nil {
		t.Fatal(err)
	}
	if *messages != 0 || state != "queued" || code != "source_unavailable" {
		t.Fatalf("source failure: sent=%d state=%s code=%s", *messages, state, code)
	}
}

func TestAvailabilityInboxNeverExposesStaleOrOtherOwnerMedia(t *testing.T) {
	s, cookie, _, _, f := availabilityFixture(t)
	f.available = true
	w := call(s, "GET", "/api/v1/subscriptions/notifications", cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Shared film") || strings.Contains(w.Body.String(), "Private requester") {
		t.Fatalf("inbox %d %s", w.Code, w.Body)
	}
	bob := seed(t, s, "22222", true)
	if w = call(s, "GET", "/api/v1/subscriptions/notifications", bob, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("cross-owner inbox %d %s", w.Code, w.Body)
	}
	f.denied = true
	if w = call(s, "GET", "/api/v1/subscriptions/notifications", cookie, "", ""); w.Code != 200 || strings.Contains(w.Body.String(), "Shared film") {
		t.Fatalf("revoked inbox %d %s", w.Code, w.Body)
	}
	f.denied = false
	f.failure = true
	if w = call(s, "GET", "/api/v1/subscriptions/notifications", cookie, "", ""); w.Code != 503 || strings.Contains(w.Body.String(), "Shared film") {
		t.Fatalf("stale inbox after outage %d %s", w.Code, w.Body)
	}
}

func TestAvailabilityPerFollowChannelRequiresGlobalConsentAndOwner(t *testing.T) {
	s, cookie, _, item, _ := availabilityFixture(t)
	path := fmt.Sprintf("/api/v1/subscriptions/%d", item.ID)
	if w := call(s, "PATCH", path, cookie, s.Config.PublicOrigin, `{"channel":"hub"}`); w.Code != 200 {
		t.Fatalf("hub channel %d %s", w.Code, w.Body)
	}
	p, _ := s.Store.GetReminderPreferences(t.Context(), "11111")
	p.DiscordDM = false
	if err := s.Store.SaveReminderPreferences(t.Context(), "11111", p); err != nil {
		t.Fatal(err)
	}
	if w := call(s, "PATCH", path, cookie, s.Config.PublicOrigin, `{"channel":"discord"}`); w.Code != 409 {
		t.Fatalf("discord without consent %d %s", w.Code, w.Body)
	}
	p.DiscordDM = true
	if err := s.Store.SaveReminderPreferences(t.Context(), "11111", p); err != nil {
		t.Fatal(err)
	}
	if w := call(s, "PATCH", path, cookie, s.Config.PublicOrigin, `{"channel":"discord"}`); w.Code != 200 {
		t.Fatalf("consented discord %d %s", w.Code, w.Body)
	}
	bob := seed(t, s, "22222", true)
	if w := call(s, "PATCH", path, bob, s.Config.PublicOrigin, `{"channel":"hub"}`); w.Code != 404 {
		t.Fatalf("cross-owner channel %d %s", w.Code, w.Body)
	}
	if w := call(s, "PATCH", path, cookie, "https://attacker.invalid", `{"channel":"hub"}`); w.Code != 403 {
		t.Fatalf("csrf channel %d", w.Code)
	}
	if w := call(s, "PATCH", path, cookie, s.Config.PublicOrigin, `{"channel":"hub","userId":"22222"}`); w.Code != 400 {
		t.Fatalf("injected owner %d", w.Code)
	}
	w := call(s, "POST", "/api/v1/subscriptions", cookie, s.Config.PublicOrigin, `{"type":"movie","tmdbId":12,"channel":"discord"}`)
	var body subscriptionResult
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Subscription == nil || body.Subscription.Channel != "discord" {
		t.Fatalf("create with consent %d %s", w.Code, w.Body)
	}
}

func TestAvailabilityOwnSeerrRequestDoesNotGenerateEvent(t *testing.T) {
	s, _, u, _, f := availabilityFixture(t)
	f.available = true
	f.self = true
	if err := s.refreshAvailability(context.Background(), u, time.Now()); err != nil {
		t.Fatal(err)
	}
	var events int
	if err := s.Store.DB.QueryRow(`SELECT COUNT(*) FROM availability_events`).Scan(&events); err != nil || events != 0 {
		t.Fatalf("duplicated Seerr event %d %v", events, err)
	}
}

func TestAvailabilityInboxWithdrawsPreferencesAndSubscriptionsChangedDuringLookup(t *testing.T) {
	for _, mode := range []string{"preferences", "unsubscribe"} {
		t.Run(mode, func(t *testing.T) {
			s, cookie, u, item, f := availabilityFixture(t)
			f.available = true
			if err := s.refreshAvailability(t.Context(), u, time.Now()); err != nil {
				t.Fatal(err)
			}
			f.beforeDetail = func() {
				var err error
				if mode == "preferences" {
					err = s.Store.Preferences(t.Context(), u.ID, false)
				} else {
					err = s.Store.DeleteSubscription(t.Context(), u.ID, item.ID)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			w := call(s, "GET", "/api/v1/subscriptions/notifications", cookie, "", "")
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
				t.Fatalf("%s changed during lookup: %d %s", mode, w.Code, w.Body)
			}
		})
	}
}
