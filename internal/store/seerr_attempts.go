package store

import "context"

const seerrAttemptSchema = `
CREATE TABLE IF NOT EXISTS eris_seerr_attempts(
 action_key TEXT PRIMARY KEY,
 request_id INTEGER NOT NULL DEFAULT 0 CHECK(request_id>=0)
);`

// ReserveSeerrAttempt commits before Seerr can receive a POST. Zero denotes an
// in-flight or uncertain attempt, which must never be retried automatically.
func (s *Store) ReserveSeerrAttempt(ctx context.Context, key string) (bool, int64, error) {
	result, err := s.DB.ExecContext(ctx, `INSERT INTO eris_seerr_attempts(action_key) VALUES(?) ON CONFLICT(action_key) DO NOTHING`, key)
	if err != nil {
		return false, 0, err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 1 {
		return n == 1, 0, err
	}
	var requestID int64
	err = s.DB.QueryRowContext(ctx, `SELECT request_id FROM eris_seerr_attempts WHERE action_key=?`, key).Scan(&requestID)
	return false, requestID, err
}

func (s *Store) ConfirmSeerrAttempt(ctx context.Context, key string, requestID int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE eris_seerr_attempts SET request_id=? WHERE action_key=? AND request_id=0`, requestID, key)
	return err
}

// ReleaseSeerrAttempt is for a definitive refusal without a Seerr write.
func (s *Store) ReleaseSeerrAttempt(ctx context.Context, key string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM eris_seerr_attempts WHERE action_key=? AND request_id=0`, key)
	return err
}
