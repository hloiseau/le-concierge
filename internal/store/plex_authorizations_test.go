package store

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestPlexAuthorizationMigrationPersistenceAndIdentityCascade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authorization.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	u, err := s.LoginPlex(ctx, "77", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`DROP TRIGGER book_tracking_identity_changed; DROP TABLE book_availability_events; DROP TABLE book_request_matches; DROP TRIGGER plex_identity_changed; DROP TABLE plex_authorizations; DELETE FROM schema_migrations WHERE version>=8;`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal("v7 migration failed", err)
	}
	ciphertext := []byte{1, 2, 3, 4}
	if err := s.SavePlexAuthorization(ctx, u.ID, ciphertext); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	records, err := s.PlexAuthorizations(ctx)
	if err != nil || !bytes.Equal(records[u.ID], ciphertext) {
		t.Fatal("authorization lost across database reopen", err)
	}
	if _, err := s.DB.Exec(`UPDATE users SET plex_id='88' WHERE id=?`, u.ID); err != nil {
		t.Fatal(err)
	}
	records, err = s.PlexAuthorizations(ctx)
	if err != nil || len(records) != 0 {
		t.Fatal("old identity authorization survived", err)
	}
	if err := s.SavePlexAuthorization(ctx, u.ID, ciphertext); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	records, err = s.PlexAuthorizations(ctx)
	if err != nil || len(records) != 0 {
		t.Fatal("deleted account kept its authorization", err)
	}
}
