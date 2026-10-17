package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSubscriptionsOwnershipGranularityAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "subscriptions.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"alice", "bob"} {
		if _, err = s.Upsert(ctx, id, id); err != nil {
			t.Fatal(err)
		}
	}
	season := 0
	targets := []SubscriptionTarget{{Type: "movie", TMDBID: 12}, {Type: "tv", TMDBID: 12}, {Type: "tv", TMDBID: 12, Season: &season}}
	var film Subscription
	for _, target := range targets {
		item, err := s.SaveSubscription(ctx, "alice", target, "hub")
		if err != nil {
			t.Fatal(err)
		}
		if target.Type == "movie" {
			film = item
		}
	}
	changed, err := s.SaveSubscription(ctx, "alice", targets[0], "discord")
	if err != nil || changed.ID != film.ID || !changed.CreatedAt.Equal(film.CreatedAt) || changed.Channel != "discord" {
		t.Fatalf("idempotent channel change: %+v %v", changed, err)
	}
	if _, err = s.SaveSubscription(ctx, "bob", targets[0], "hub"); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteSubscription(ctx, "bob", film.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("other owner deleted follow: %v", err)
	}
	if fresh, err := s.MarkSubscriptionAvailable(ctx, "bob", film.ID, time.Now()); err != nil || fresh {
		t.Fatalf("other owner marked availability: %v %v", fresh, err)
	}
	if fresh, err := s.MarkSubscriptionAvailable(ctx, "alice", film.ID, time.Now()); err != nil || !fresh {
		t.Fatalf("first availability missing: %v %v", fresh, err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	items, total, err := s.Subscriptions(ctx, "alice", 0)
	if err != nil || total != 3 || len(items) != 3 || items[0].Season == nil || *items[0].Season != 0 {
		t.Fatalf("restart/granularity: %+v %d %v", items, total, err)
	}
	if fresh, err := s.MarkSubscriptionAvailable(ctx, "alice", film.ID, time.Now()); err != nil || fresh {
		t.Fatalf("duplicate availability after restart: %v %v", fresh, err)
	}
	if err = s.Delete(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	var events int
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM subscription_availability`).Scan(&events); err != nil || events != 0 {
		t.Fatalf("account event cascade: %d %v", events, err)
	}
	_, total, err = s.Subscriptions(ctx, "bob", 0)
	if err != nil || total != 1 {
		t.Fatalf("other owner lost follow: %d %v", total, err)
	}
}

func TestSubscriptionsValidationAndLimit(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Upsert(ctx, "alice", "Alice"); err != nil {
		t.Fatal(err)
	}
	negative, huge, season := -1, 10001, 1
	for _, target := range []SubscriptionTarget{{Type: "movie", TMDBID: 0}, {Type: "person", TMDBID: 1}, {Type: "movie", TMDBID: 1, Season: &season}, {Type: "tv", TMDBID: 1, Season: &negative}, {Type: "tv", TMDBID: 1, Season: &huge}} {
		if _, err = s.SaveSubscription(ctx, "alice", target, "hub"); err == nil {
			t.Fatalf("invalid target accepted: %+v", target)
		}
	}
	for id := int64(1); id <= 100; id++ {
		if _, err = s.SaveSubscription(ctx, "alice", SubscriptionTarget{Type: "movie", TMDBID: id}, "hub"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.SaveSubscription(ctx, "alice", SubscriptionTarget{Type: "movie", TMDBID: 101}, "hub"); !errors.Is(err, ErrSubscriptionLimit) {
		t.Fatalf("unbounded subscriptions: %v", err)
	}
	if _, err = s.SaveSubscription(ctx, "alice", SubscriptionTarget{Type: "movie", TMDBID: 1}, "discord"); err != nil {
		t.Fatalf("limit blocked existing channel update: %v", err)
	}
	items, total, err := s.Subscriptions(ctx, "alice", 10)
	if err != nil || total != 100 || len(items) != 10 || items[0].TMDBID != 90 {
		t.Fatalf("pagination: %+v %d %v", items, total, err)
	}
}
