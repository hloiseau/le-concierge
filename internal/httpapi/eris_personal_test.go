package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/store"
)

func erisFollowFixture(t *testing.T) (*Server, *subscriptionSource) {
	t.Helper()
	s, _, source := subscriptionFixture(t)
	original := s.Upstream.HTTP.Transport
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/search" {
			return response(200, `{"results":[{"id":12,"title":"Shared film","mediaType":"movie","releaseDate":"2021-01-01"}]}`), nil
		}
		return original.RoundTrip(r)
	})
	return s, source
}

func TestErisFollowsReuseAvailabilityConsentAndIdempotency(t *testing.T) {
	s, source := erisFollowFixture(t)
	ctx := context.Background()
	u, _ := s.Store.User(ctx, "11111")
	g := s.grants[u.ID]
	plan := eris.Plan{Action: "follow", Query: "Shared film", Kind: "movie", Year: 2021}
	for range 2 {
		r := s.erisFollows(ctx, u, g, plan)
		if !strings.Contains(r.Content, "Suivi #") || !strings.Contains(r.Content, "Hub") {
			t.Fatal(r)
		}
	}
	items, total, err := s.Store.Subscriptions(ctx, u.ID, 0)
	if err != nil || total != 1 {
		t.Fatal("duplicate follow", total, err)
	}
	plan.Channel = "discord"
	r := s.erisFollows(ctx, u, g, plan)
	if !strings.Contains(r.Content, "consentement") {
		t.Fatal("DM activated without consent", r)
	}
	preferences, _ := s.Store.GetReminderPreferences(ctx, u.ID)
	if preferences.DiscordDM {
		t.Fatal("follow changed DM consent")
	}
	r = s.erisFollows(ctx, u, g, eris.Plan{Action: "follows"})
	if !strings.Contains(r.Content, "Shared film") || strings.Contains(r.Content, "Private requester") {
		t.Fatal(r)
	}
	seed(t, s, "22222", true)
	bob, _ := s.Store.User(ctx, "22222")
	r = s.erisFollows(ctx, bob, s.grants[bob.ID], eris.Plan{Action: "unfollow", SubscriptionID: items[0].ID})
	if !strings.Contains(r.Content, "ne trouve pas") {
		t.Fatal("another owner removed a follow", r)
	}
	_, total, _ = s.Store.Subscriptions(ctx, u.ID, 0)
	if total != 1 {
		t.Fatal("cross-member mutation")
	}
	source.available = true
	plan.Channel = "hub"
	r = s.erisFollows(ctx, u, g, plan)
	if !strings.Contains(r.Content, "déjà accessible") {
		t.Fatal(r)
	}
	_, total, _ = s.Store.Subscriptions(ctx, u.ID, 0)
	if total != 0 {
		t.Fatal("available title left a pending alert")
	}
}

func TestErisPersonalWritesRejectRevokedGrantAndVerifyPreferences(t *testing.T) {
	s, _ := erisFollowFixture(t)
	ctx := context.Background()
	u, _ := s.Store.User(ctx, "11111")
	g := s.grants[u.ID]
	seed(t, s, "22222", true)
	beforeBob, _ := s.Store.GetReminderPreferences(ctx, "22222")
	for _, change := range []eris.Plan{
		{Action: "preferences", Setting: "enabled", Value: "true"},
		{Action: "preferences", Setting: "quietHours", Value: "23-9"},
		{Action: "preferences", Setting: "dailyCap", Value: "5"},
		{Action: "preferences", Setting: "daysBefore", Value: "10,3"},
	} {
		r := s.erisPreferences(ctx, u, g, change)
		if !strings.Contains(r.Content, "enregistré et relu") {
			t.Fatal(change, r)
		}
	}
	p, _ := s.Store.GetReminderPreferences(ctx, u.ID)
	if !p.Enabled || p.QuietStartHour != 23 || p.QuietEndHour != 9 || p.DailyCap != 5 || len(p.DaysBefore) != 2 || p.DaysBefore[0] != 10 {
		t.Fatal(p)
	}
	afterBob, _ := s.Store.GetReminderPreferences(ctx, "22222")
	if beforeBob.Enabled != afterBob.Enabled || beforeBob.DailyCap != afterBob.DailyCap {
		t.Fatal("modified another member")
	}
	r := s.erisPreferences(ctx, u, g, eris.Plan{Action: "preferences", Setting: "discordDmEnabled", Value: "true"})
	if !strings.Contains(r.Content, "pas activés") {
		t.Fatal("server-disabled DM accepted", r)
	}
	item, err := s.Store.SaveSubscription(ctx, u.ID, store.SubscriptionTarget{Type: "movie", TMDBID: 12}, "hub")
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	delete(s.grants, u.ID)
	s.mu.Unlock()
	s.erisFollows(ctx, u, g, eris.Plan{Action: "unfollow", SubscriptionID: item.ID})
	s.erisPreferences(ctx, u, g, eris.Plan{Action: "preferences", Setting: "enabled", Value: "false"})
	_, total, _ := s.Store.Subscriptions(ctx, u.ID, 0)
	p, _ = s.Store.GetReminderPreferences(ctx, u.ID)
	if total != 1 || !p.Enabled {
		t.Fatal("revoked grant changed data", total, p)
	}
}

func TestErisFollowOutageAndRevocationNeverPersist(t *testing.T) {
	for _, state := range []string{"failure", "revocation"} {
		s, source := erisFollowFixture(t)
		ctx := context.Background()
		u, _ := s.Store.User(ctx, "11111")
		g := s.grants[u.ID]
		if state == "failure" {
			source.failure = true
		} else {
			source.beforeDetail = func() { s.mu.Lock(); delete(s.grants, u.ID); s.mu.Unlock() }
		}
		r := s.erisFollows(ctx, u, g, eris.Plan{Action: "follow", Query: "Shared film", Kind: "movie"})
		_, total, _ := s.Store.Subscriptions(ctx, u.ID, 0)
		if total != 0 || strings.Contains(r.Content, "enregistré") {
			t.Fatal(state, total, r)
		}
	}
}

func TestErisTransportQuestionCannotBeReplacedByModelWriteIntent(t *testing.T) {
	s := fixture(t)
	s.Config.BotAPISecret = strings.Repeat("s", 32)
	s.Upstream.HTTP.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected upstream before explicit intent")
		return nil, nil
	})
	in := signedErisTool(t, s, eris.Plan{Action: "preferences", Setting: "enabled", Value: "false"}, strings.Replace(erisSlash, "Arrival", "Quels sont mes réglages ?", 1))
	w := callErisTool(s, in)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "rien changé") {
		t.Fatal(w.Code, w.Body.String())
	}
}
