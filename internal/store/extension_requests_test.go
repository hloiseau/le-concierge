package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestExtensionRequestPersistenceLimitsAndRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extensions.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	if _, err = s.Upsert(ctx, "11111", "Member"); err != nil {
		t.Fatal(err)
	}
	input := ExtensionRequest{MediaID: "42", Title: "Film", Kind: "movie", URL: "https://app.plex.tv/", EstimatedAt: now.Add(24 * time.Hour), Days: 7, Reason: "Finir"}
	var first ExtensionRequest
	for i := range 10 {
		input.Key = fmt.Sprintf("deadline-%d", i)
		item, err := s.CreateExtensionRequest(ctx, "11111", input, now)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = item
		}
	}
	input.Key = "deadline-0"
	if item, err := s.CreateExtensionRequest(ctx, "11111", input, now); err != nil || item.ID != first.ID {
		t.Fatal("duplicate consumed quota", err)
	}
	input.Key = "deadline-11"
	if _, err := s.CreateExtensionRequest(ctx, "11111", input, now); !errors.Is(err, ErrExtensionLimit) {
		t.Fatal("limit not enforced", err)
	}
	if err := s.ReviewExtensionRequest(ctx, first.ID, "reviewed", "Vérifié", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateExtensionRequest(ctx, "11111", input, now); !errors.Is(err, ErrExtensionLimit) {
		t.Fatal("daily creation cap bypassed by review", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	items, err := s.ExtensionRequests(ctx, "11111")
	if err != nil || len(items) != 10 {
		t.Fatal("restart lost requests", err)
	}
	admin, err := s.ExtensionRequests(ctx, "")
	if err != nil || len(admin) != 9 || admin[0].Requester != "Member" {
		t.Fatal("admin queue incorrect", err)
	}
	if err := s.PruneExtensionRequests(ctx, now.Add(91*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	items, err = s.ExtensionRequests(ctx, "11111")
	if err != nil || len(items) != 0 {
		t.Fatal("retention not applied", err)
	}
	input.Key = "after-retention"
	input.EstimatedAt = now.Add(24 * time.Hour)
	if _, err := s.CreateExtensionRequest(ctx, "11111", input, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "11111"); err != nil {
		t.Fatal(err)
	}
	items, err = s.ExtensionRequests(ctx, "")
	if err != nil || len(items) != 0 {
		t.Fatal("account deletion left requests", err)
	}
}
