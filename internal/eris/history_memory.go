package eris

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"
)

const summarySchema = `
CREATE TABLE IF NOT EXISTS eris_summaries(owner TEXT PRIMARY KEY, body TEXT NOT NULL, updated INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS eris_summary_members(owner TEXT NOT NULL,member TEXT NOT NULL,PRIMARY KEY(owner,member));
CREATE INDEX IF NOT EXISTS eris_summary_member ON eris_summary_members(member);`

func (m *Memory) loadSummary(owner string) (rollingSummary, error) {
	var body string
	err := m.db.QueryRow(`SELECT body FROM eris_summaries WHERE owner=?`, owner).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return rollingSummary{}, nil
	}
	if err != nil {
		return rollingSummary{}, err
	}
	var summary rollingSummary
	if len(body) > 24000 || json.Unmarshal([]byte(body), &summary) != nil || len(summary.Facts) > 8 || utf8.RuneCountInString(summary.context()) > summaryTextBudget {
		return rollingSummary{}, errors.New("invalid stored summary")
	}
	return summary, nil
}

// The summary inherits the current turn's transitive contributors. Even when its
// text does not mention them, forgetting invalidates this conservative derivative.
func (m *Memory) saveSummary(owner string, summary rollingSummary, turn int64, now time.Time) error {
	if turn == 0 {
		return nil
	}
	_, member := memoryScope(owner)
	body, _ := json.Marshal(summary)
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO eris_summaries VALUES(?,?,?) ON CONFLICT(owner) DO UPDATE SET body=excluded.body,updated=excluded.updated`, owner, string(body), now.Unix()); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT OR IGNORE INTO eris_summary_members VALUES(?,?)`, owner, member); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT OR IGNORE INTO eris_summary_members SELECT ?,member FROM eris_turn_members WHERE turn_id=?`, owner, turn); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *Memory) deriveSummary(turn int64, owner string) error {
	if turn == 0 {
		return nil
	}
	_, err := m.db.Exec(`INSERT OR IGNORE INTO eris_turn_members SELECT ?,member FROM eris_summary_members WHERE owner=?`, turn, owner)
	return err
}
