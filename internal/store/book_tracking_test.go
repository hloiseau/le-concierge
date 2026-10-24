package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func bookMatchFixture() BookMatch {
	return BookMatch{UserID: "owner", Scope: "personal-source", RequestID: "1", BookID: "7", BookJSON: []byte(`{"id":"7","title":"Edition one","formats":["epub"]}`), RequestJSON: []byte(`{"id":"1","status":"fulfilled","deliveryState":"complete"}`), Revision: "confirmed-once", RequestPage: 1, CatalogPage: 1}
}

func TestBookTrackingMigrationAndDurableResume(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "concierge.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Upsert(ctx, "owner", "Owner"); err != nil {
		t.Fatal(err)
	}
	if err = s.LinkPlex(ctx, "owner", "100"); err != nil {
		t.Fatal(err)
	}
	// Recreate the preceding schema with a real owner, then exercise the upgrade.
	if _, err = s.DB.Exec(`DROP TRIGGER book_tracking_identity_changed; DROP TABLE book_availability_events; DROP TABLE book_request_matches; DELETE FROM schema_migrations WHERE version=9;`); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err = s.DB.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 9 {
		t.Fatal(version, err)
	}
	u, err := s.User(ctx, "owner")
	if err != nil || u.PlexID != "100" {
		t.Fatal("migration changed identity", u, err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	m, err := s.ConfirmBookMatch(ctx, bookMatchFixture(), now)
	if err != nil {
		t.Fatal(err)
	}
	m.RequestPage, m.CatalogPage = 3, 8
	if err = s.ObserveBookMatch(ctx, m, false, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	items, err := s.BookMatches(ctx, "owner", "personal-source")
	if err != nil || len(items) != 1 || items[0].RequestPage != 3 || items[0].CatalogPage != 8 || items[0].VerifiedAt != nil || !items[0].ConfirmedAt.Equal(now) {
		t.Fatal("cursor/provenance did not survive reopen", items, err)
	}
	var count int
	var proof string
	if err = s.DB.QueryRow(`SELECT COUNT(*),book_json FROM book_availability_events`).Scan(&count, &proof); err != nil || count != 1 || proof != string(m.BookJSON) {
		t.Fatal("event evidence changed", count, proof, err)
	}
	for _, scope := range []string{"other-personal-source", ""} {
		items, err = s.BookMatches(ctx, "owner", scope)
		if err != nil || len(items) != 0 {
			t.Fatal("source scope crossed", items, err)
		}
	}
	items, err = s.BookMatches(ctx, "another-owner", "personal-source")
	if err != nil || len(items) != 0 {
		t.Fatal("owner scope crossed")
	}
	if err = s.Unlink(ctx, "owner"); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM book_request_matches`).Scan(&count); err != nil || count != 0 {
		t.Fatal("Plex identity unlink retained correlations", count, err)
	}
}

func TestBookTrackingCorrectionAndLateObservationKeepInitialEvidence(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, err = s.Upsert(ctx, "owner", "Owner")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	first, err := s.ConfirmBookMatch(ctx, bookMatchFixture(), now)
	if err != nil {
		t.Fatal(err)
	}
	correction := bookMatchFixture()
	correction.BookID = "8"
	correction.BookJSON = []byte(`{"id":"8","title":"Edition two","formats":["epub"]}`)
	correction.Revision = "corrected"
	second, err := s.ConfirmBookMatch(ctx, correction, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveBookMatch(ctx, first, true, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	items, err := s.BookMatches(ctx, "owner", "personal-source")
	if err != nil || len(items) != 1 || items[0].BookID != "8" {
		t.Fatal("stale response restored previous edition", items, err)
	}
	var state string
	if err = s.DB.QueryRow(`SELECT state FROM book_availability_events WHERE match_id=?`, first.ID).Scan(&state); err != nil || state != "cancelled" {
		t.Fatal("previous event not withdrawn", state, err)
	}
	second.BookJSON = []byte(`{"id":"8","title":"Changed metadata","formats":["epub"]}`)
	if err = s.ObserveBookMatch(ctx, second, true, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var proof string
	if err = s.DB.QueryRow(`SELECT book_json FROM book_availability_events WHERE match_id=?`, second.ID).Scan(&proof); err != nil || proof != string(correction.BookJSON) {
		t.Fatal("initial import observation overwritten", proof, err)
	}
	if err = s.RemoveBookMatch(ctx, "owner", "personal-source", "1"); err != nil {
		t.Fatal(err)
	}
	if err = s.ObserveBookMatch(ctx, second, true, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	items, err = s.BookMatches(ctx, "owner", "personal-source")
	if err != nil || len(items) != 0 {
		t.Fatal("withdrawal was undone", items, err)
	}
	if err = s.PruneBookTracking(ctx, now.Add(91*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM book_availability_events`).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired provenance retained", count, err)
	}
}

func TestBookTrackingBoundsAndPollingRotation(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, err = s.Upsert(ctx, "owner", "Owner")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for i := 1; i <= 100; i++ {
		m := bookMatchFixture()
		m.RequestID = fmt.Sprint(i)
		if _, err = s.ConfirmBookMatch(ctx, m, now.Add(time.Duration(i)*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	m := bookMatchFixture()
	m.RequestID = "101"
	if _, err = s.ConfirmBookMatch(ctx, m, now); !errors.Is(err, ErrBookMatchLimit) {
		t.Fatal("active bound missing", err)
	}
	items, err := s.BookMatches(ctx, "owner", "personal-source")
	if err != nil || items[0].RequestID != "1" {
		t.Fatal(items, err)
	}
	if started, err := s.BeginBookMatchCheck(ctx, items[0], now.Add(time.Minute)); err != nil || !started {
		t.Fatal(started, err)
	}
	items, err = s.BookMatches(ctx, "owner", "personal-source")
	if err != nil || items[0].RequestID != "2" {
		t.Fatal("timed-out source would monopolize rotation", items, err)
	}
	// Historical records count towards a second hard bound even across scopes.
	_, err = s.DB.Exec(`WITH RECURSIVE n(x) AS (SELECT 101 UNION ALL SELECT x+1 FROM n WHERE x<1000)
INSERT INTO book_request_matches(user_id,source_scope,request_id,book_id,book_json,request_json,revision,confirmed_at,checked_at,request_page,catalog_page,active)
SELECT 'owner','old-source',CAST(x AS TEXT),'7','{}','{}','old',1,1,1,1,0 FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	m.Scope = "new-source"
	if _, err = s.ConfirmBookMatch(ctx, m, now); !errors.Is(err, ErrBookMatchLimit) {
		t.Fatal("historical bound missing", err)
	}
	if err = s.PruneBookTracking(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConfirmBookMatch(ctx, m, now); err != nil {
		t.Fatal("expired history did not free capacity", err)
	}
}
