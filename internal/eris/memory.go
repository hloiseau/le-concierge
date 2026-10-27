package eris

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// Memory stores the character notebook and addressed Discord conversations.
// Media credentials, images and private tool results never enter the notebook.
type Memory struct{ db *sql.DB }

type whim struct {
	Topic  string `json:"topic"`
	Quote  string `json:"quote"`
	Retire bool   `json:"retire,omitempty"`
}

type rememberedWhim struct {
	whim
	Source   string `json:"source"`
	Updated  int64  `json:"updated"`
	LastUsed int64  `json:"last_used"`
}

func OpenMemory(path string) (*Memory, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=1000; PRAGMA secure_delete=ON;
CREATE TABLE IF NOT EXISTS eris_whims(scope TEXT NOT NULL, topic TEXT NOT NULL, quote TEXT NOT NULL, source TEXT NOT NULL, updated INTEGER NOT NULL, used INTEGER NOT NULL, expires INTEGER NOT NULL, retired INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(scope,topic));
CREATE TABLE IF NOT EXISTS eris_whim_members(scope TEXT NOT NULL, topic TEXT NOT NULL, member TEXT NOT NULL, PRIMARY KEY(scope,topic,member));`)
	if err == nil {
		_, err = db.Exec(durableSchema)
	}
	if err == nil {
		_, err = db.Exec(summarySchema)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Memory{db}, nil
}

func (m *Memory) Close() error { return m.db.Close() }

func memoryScope(owner string) (scope, member string) {
	p := strings.Split(owner, ":")
	if len(p) == 3 {
		return p[0] + ":channel:" + p[2], p[0] + ":" + p[1]
	}
	return "private:" + owner, owner
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}

// Recall is deliberately lexical: a small notebook does not need embeddings.
func (m *Memory) recall(owner, question string, now time.Time) (*rememberedWhim, error) {
	scope, _ := memoryScope(owner)
	rows, err := m.db.Query(`SELECT topic,quote,source,updated,used FROM eris_whims WHERE scope=? AND retired=0 AND expires>? ORDER BY updated DESC LIMIT 32`, scope, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var best *rememberedWhim
	bestScore := 0
	for rows.Next() {
		var w rememberedWhim
		if err := rows.Scan(&w.Topic, &w.Quote, &w.Source, &w.Updated, &w.LastUsed); err != nil {
			return nil, err
		}
		score := 0
		for _, word := range words(w.Topic) {
			if len([]rune(word)) < 4 {
				continue
			}
			for _, q := range words(question) {
				if word == q {
					score++
					break
				}
			}
		}
		// A human explicitly naming the topic may recall it even during cooldown.
		if score == 0 || (now.Unix()-w.LastUsed < 6*3600 && !strings.Contains(strings.ToLower(question), strings.ToLower(w.Topic))) {
			continue
		}
		if score > bestScore {
			copy := w
			best = &copy
			bestScore = score
		}
	}
	return best, rows.Err()
}

func (m *Memory) forget(owner string) error {
	_, member := memoryScope(owner)
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM eris_summaries WHERE owner IN(SELECT owner FROM eris_summary_members WHERE member=?)`, member); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM eris_summary_members WHERE owner NOT IN(SELECT owner FROM eris_summaries)`); err != nil {
		return err
	}
	// Remove all recorded derivatives, including another member's later reply.
	if _, err = tx.Exec(`DELETE FROM eris_notes WHERE member=? OR turn_id IN(SELECT turn_id FROM eris_turn_members WHERE member=?)`, member, member); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM eris_turns WHERE member=? OR id IN(SELECT turn_id FROM eris_turn_members WHERE member=?)`, member, member); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM eris_turn_members WHERE turn_id NOT IN(SELECT id FROM eris_turns)`); err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM eris_whims WHERE EXISTS(SELECT 1 FROM eris_whim_members p WHERE p.scope=eris_whims.scope AND p.topic=eris_whims.topic AND p.member=?)`, member)
	if err == nil {
		_, err = tx.Exec(`DELETE FROM eris_whim_members WHERE NOT EXISTS(SELECT 1 FROM eris_whims w WHERE w.scope=eris_whim_members.scope AND w.topic=eris_whim_members.topic)`)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (m *Memory) commit(owner, source string, change *whim, recalled *rememberedWhim, now time.Time, turns ...int64) error {
	scope, member := memoryScope(owner)
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM eris_whims WHERE expires<=?`, now.Unix()); err != nil {
		return err
	}
	if recalled != nil {
		if _, err = tx.Exec(`UPDATE eris_whims SET used=? WHERE scope=? AND topic=?`, now.Unix(), scope, recalled.Topic); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT OR IGNORE INTO eris_whim_members VALUES(?,?,?)`, scope, recalled.Topic, member); err != nil {
			return err
		}
	}
	if change != nil {
		if change.Retire {
			_, err = tx.Exec(`UPDATE eris_whims SET retired=1 WHERE scope=? AND topic=?`, scope, change.Topic)
		} else {
			_, err = tx.Exec(`INSERT INTO eris_whims VALUES(?,?,?,?,?,?,?,0) ON CONFLICT(scope,topic) DO UPDATE SET quote=excluded.quote,source=excluded.source,updated=excluded.updated,used=excluded.used,expires=excluded.expires WHERE eris_whims.retired=0`, scope, change.Topic, change.Quote, source, now.Unix(), now.Unix(), now.Add(30*24*time.Hour).Unix())
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT OR IGNORE INTO eris_whim_members VALUES(?,?,?)`, scope, change.Topic, member); err != nil {
			return err
		}
		// A derived recollection inherits every contributing member for erasure.
		if recalled != nil {
			if _, err = tx.Exec(`INSERT OR IGNORE INTO eris_whim_members SELECT scope,?,member FROM eris_whim_members WHERE scope=? AND topic=?`, change.Topic, scope, recalled.Topic); err != nil {
				return err
			}
		}
		for _, id := range turns {
			if _, err = tx.Exec(`INSERT OR IGNORE INTO eris_whim_members SELECT ?,?,member FROM eris_turn_members WHERE turn_id=?`, scope, change.Topic, id); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(`DELETE FROM eris_whims WHERE scope=? AND topic NOT IN(SELECT topic FROM eris_whims WHERE scope=? ORDER BY updated DESC,topic LIMIT 32)`, scope, scope); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM eris_whim_members WHERE NOT EXISTS(SELECT 1 FROM eris_whims w WHERE w.scope=eris_whim_members.scope AND w.topic=eris_whim_members.topic)`); err != nil {
		return err
	}
	return tx.Commit()
}

// Memory is stripped before the unchanged media-plan validator sees the output.
func parseCharacterPlan(raw string, recalled *rememberedWhim) (Plan, *whim, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &fields) != nil || fields == nil {
		return Plan{}, nil, errors.New("invalid character output")
	}
	metadata := fields["memory"]
	delete(fields, "memory")
	base, _ := json.Marshal(fields)
	p, err := ParsePlan(string(base))
	if err != nil {
		return p, nil, err
	}
	if len(metadata) == 0 || string(metadata) == "null" || p.Action != "chat" {
		return p, nil, nil
	}
	var w whim
	d := json.NewDecoder(strings.NewReader(string(metadata)))
	d.DisallowUnknownFields()
	if d.Decode(&w) != nil || strings.TrimSpace(w.Topic) != w.Topic || utf8.RuneCountInString(w.Topic) < 4 || utf8.RuneCountInString(w.Topic) > 60 || strings.ContainsAny(w.Topic+w.Quote, "\n\r\x00<@") {
		return p, nil, nil
	}
	if w.Retire {
		if recalled == nil || w.Topic != recalled.Topic {
			return p, nil, nil
		}
	} else if utf8.RuneCountInString(w.Quote) < 8 || utf8.RuneCountInString(w.Quote) > 240 || len(words(w.Quote)) < 3 || !strings.Contains(" "+strings.Join(words(p.Text), " ")+" ", " "+strings.Join(words(w.Quote), " ")+" ") {
		return p, nil, nil
	}
	return p, &w, nil
}
