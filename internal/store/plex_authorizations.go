package store

import "context"

const plexAuthorizationsSchema = `
CREATE TABLE plex_authorizations(
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 ciphertext BLOB NOT NULL
);
CREATE TRIGGER plex_identity_changed AFTER UPDATE OF plex_id ON users
WHEN OLD.plex_id IS NOT NEW.plex_id
BEGIN DELETE FROM plex_authorizations WHERE user_id=NEW.id; END;
INSERT INTO schema_migrations(version) VALUES(8);`

// Only authenticated ciphertext belongs here; tokens are encrypted by the API.
func (s *Store) SavePlexAuthorization(ctx context.Context, user string, ciphertext []byte) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO plex_authorizations(user_id,ciphertext) VALUES(?,?)
ON CONFLICT(user_id) DO UPDATE SET ciphertext=excluded.ciphertext`, user, ciphertext)
	return err
}

func (s *Store) DeletePlexAuthorization(ctx context.Context, user string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM plex_authorizations WHERE user_id=?`, user)
	return err
}

func (s *Store) PlexAuthorizations(ctx context.Context) (map[string][]byte, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT user_id,ciphertext FROM plex_authorizations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]byte{}
	for rows.Next() {
		var id string
		var value []byte
		if err := rows.Scan(&id, &value); err != nil {
			return nil, err
		}
		result[id] = value
	}
	return result, rows.Err()
}
