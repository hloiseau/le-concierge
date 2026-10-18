package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestExtensionRequestCancellationKeepsHistoryUniquenessAndDailyCap(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	if _, err := s.Upsert(ctx, "alice", "Alice"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	input := ExtensionRequest{MediaID: "42", Title: "Film", Kind: "movie", URL: "https://app.plex.tv/", EstimatedAt: now.Add(3 * 24 * time.Hour), Days: 7, Reason: "Finir"}
	var first ExtensionRequest
	for i := range 10 {
		input.Key = fmt.Sprint(i)
		item, err := s.CreateExtensionRequest(ctx, "alice", input, now)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = item
			if err := s.CancelExtensionRequest(ctx, "bob", item.ID, now); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("another owner cancelled the request", err)
			}
		}
		if err := s.CancelExtensionRequest(ctx, "alice", item.ID, now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CancelExtensionRequest(ctx, "alice", first.ID, now.Add(2*time.Hour)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cancelled request accepted again", err)
	}
	if err := s.ReviewExtensionRequest(ctx, first.ID, "reviewed", "Too late", now.Add(2*time.Hour)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cancelled request was reviewed", err)
	}
	input.Key, input.Days, input.Reason = first.Key, 14, "Changed"
	duplicate, err := s.CreateExtensionRequest(ctx, "alice", input, now.Add(2*time.Hour))
	if err != nil || duplicate.ID != first.ID || duplicate.Status != "cancelled" || duplicate.Days != 7 || duplicate.Reason != "Finir" || !duplicate.CreatedAt.Equal(now) || !duplicate.UpdatedAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("duplicate revived or rewrote cancelled history: %+v %v", duplicate, err)
	}
	input.Key = "new request"
	if _, err := s.CreateExtensionRequest(ctx, "alice", input, now.Add(23*time.Hour)); !errors.Is(err, ErrExtensionLimit) {
		t.Fatal("cancellation bypassed the daily creation limit", err)
	}
	if _, err := s.CreateExtensionRequest(ctx, "alice", input, now.Add(25*time.Hour)); err != nil {
		t.Fatal("cancelled requests consumed pending quota", err)
	}
	history, err := s.ExtensionRequests(ctx, "alice", now.Add(25*time.Hour))
	if err != nil || len(history) != 11 {
		t.Fatal("cancellation removed history", len(history), err)
	}
	queue, err := s.ExtensionRequests(ctx, "", now.Add(25*time.Hour))
	if err != nil || len(queue) != 1 || queue[0].Status != "pending" {
		t.Fatal("cancelled requests remained in the admin queue", queue, err)
	}
}

func TestExtensionRequestExpiryReleasesPendingQuotaButKeepsDailyCap(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	if _, err := s.Upsert(ctx, "alice", "Alice"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	input := ExtensionRequest{MediaID: "42", Title: "Film", Kind: "movie", URL: "https://app.plex.tv/", EstimatedAt: now.Add(time.Hour), Days: 7}
	var first ExtensionRequest
	for i := range 10 {
		input.Key = fmt.Sprint(i)
		item, err := s.CreateExtensionRequest(ctx, "alice", input, now)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = item
		}
	}
	expiredAt := input.EstimatedAt
	history, err := s.ExtensionRequests(ctx, "alice", expiredAt)
	if err != nil || len(history) != 10 {
		t.Fatal("expired history lost", len(history), err)
	}
	for _, item := range history {
		if item.Status != "expired" {
			t.Fatalf("expired pending request still active: %+v", item)
		}
	}
	var savedStatus string
	if err := s.DB.QueryRow(`SELECT status FROM extension_requests WHERE id=?`, first.ID).Scan(&savedStatus); err != nil || savedStatus != "pending" {
		t.Fatal("expiry must be derived without inventing a review or cancellation", savedStatus, err)
	}
	queue, err := s.ExtensionRequests(ctx, "", expiredAt)
	if err != nil || len(queue) != 0 {
		t.Fatal("expired requests filled the admin queue", queue, err)
	}
	if err := s.CancelExtensionRequest(ctx, "alice", first.ID, expiredAt); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("expired request was cancelled", err)
	}
	if err := s.ReviewExtensionRequest(ctx, first.ID, "reviewed", "Late", expiredAt); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("expired request was reviewed", err)
	}
	input.Key, input.EstimatedAt = "new request", now.Add(3*24*time.Hour)
	if _, err := s.CreateExtensionRequest(ctx, "alice", input, now.Add(2*time.Hour)); !errors.Is(err, ErrExtensionLimit) {
		t.Fatal("expiry bypassed the daily creation limit", err)
	}
	if _, err := s.CreateExtensionRequest(ctx, "alice", input, now.Add(25*time.Hour)); err != nil {
		t.Fatal("expired requests consumed pending quota", err)
	}
}

func TestExtensionRequestReviewAndCancellationCannotBothWin(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	if _, err := s.Upsert(ctx, "alice", "Alice"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	for i := range 8 {
		item, err := s.CreateExtensionRequest(ctx, "alice", ExtensionRequest{Key: fmt.Sprint(i), MediaID: "42", Title: "Film", Kind: "movie", EstimatedAt: now.Add(time.Hour), Days: 7}, now)
		if err != nil {
			t.Fatal(err)
		}
		start, results := make(chan struct{}), make(chan error, 2)
		go func() { <-start; results <- s.CancelExtensionRequest(ctx, "alice", item.ID, now) }()
		go func() { <-start; results <- s.ReviewExtensionRequest(ctx, item.ID, "reviewed", "Reviewed", now) }()
		close(start)
		successes := 0
		for range 2 {
			if err := <-results; err == nil {
				successes++
			} else if !errors.Is(err, sql.ErrNoRows) {
				t.Fatal(err)
			}
		}
		if successes != 1 {
			t.Fatal("review and cancellation must be mutually exclusive", successes)
		}
		var status, note string
		if err := s.DB.QueryRow(`SELECT status,admin_note FROM extension_requests WHERE id=?`, item.ID).Scan(&status, &note); err != nil || (status != "cancelled" && status != "reviewed") || (status == "cancelled" && note != "") || (status == "reviewed" && note != "Reviewed") {
			t.Fatal("decision was overwritten", status, note, err)
		}
	}
}

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
	items, err := s.ExtensionRequests(ctx, "11111", now)
	if err != nil || len(items) != 10 {
		t.Fatal("restart lost requests", err)
	}
	admin, err := s.ExtensionRequests(ctx, "", now)
	if err != nil || len(admin) != 9 || admin[0].Requester != "Member" {
		t.Fatal("admin queue incorrect", err)
	}
	if err := s.PruneExtensionRequests(ctx, now.Add(91*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	items, err = s.ExtensionRequests(ctx, "11111", now)
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
	items, err = s.ExtensionRequests(ctx, "", now)
	if err != nil || len(items) != 0 {
		t.Fatal("account deletion left requests", err)
	}
}
