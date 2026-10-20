package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestV7MigrationPreservesExtensionHistoryAndSequence(t *testing.T) {
	for _, empty := range []bool{false, true} {
		name := "existing requests"
		if empty {
			name = "previously purged requests"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "v6.db")
			old, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = old.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY);
CREATE TABLE users(id TEXT PRIMARY KEY,discord_id TEXT UNIQUE,name TEXT NOT NULL,plex_id TEXT UNIQUE,seerr_id INTEGER UNIQUE,notifications INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
INSERT INTO schema_migrations VALUES(1),(2);
` + remindersSchema + subscriptionsSchema + notificationDeliverySchema + extensionRequestsSchema + availabilityDeliverySchema + metricsSchema + availableRequestRemindersSchema + channelReminderSchema + `
INSERT INTO schema_migrations VALUES(4),(5),(6);
INSERT INTO users(id,discord_id,name,plex_id,notifications) VALUES('alice','11111','Alice','77',1);
INSERT INTO extension_requests(id,user_id,decision_key,media_id,title,kind,plex_url,estimated_at,days,reason,status,admin_note,created_at,updated_at) VALUES
(1,'alice','pending','42','Film','movie','https://app.plex.tv/',1792500000000,7,'Finish','pending','',1792300000000,1792300000000),
(2,'alice','reviewed','43','Series','season','https://app.plex.tv/',1792500000000,14,'Watch','reviewed','Examined',1792300000000,1792400000000),
(3,'alice','declined','44','Episode','episode','https://app.plex.tv/',1792500000000,2,'Later','declined','Too late',1792300000000,1792400000000),
(99,'alice','purged','45','Old film','movie','https://app.plex.tv/',1792500000000,7,'Old','pending','',1792300000000,1792300000000);
DELETE FROM extension_requests WHERE id=99;`)
			if err == nil && empty {
				_, err = old.Exec(`DELETE FROM extension_requests`)
			}
			old.Close()
			if err != nil {
				t.Fatal(err)
			}
			now := time.UnixMilli(1792400000000).UTC()
			for pass := range 2 {
				s, err := Open(path)
				if err != nil {
					t.Fatal(err)
				}
				var version, sequence int
				if err := s.DB.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 8 {
					t.Fatal("migration version", version, err)
				}
				if pass == 0 {
					if err := s.DB.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name='extension_requests'`).Scan(&sequence); err != nil || sequence != 99 {
						t.Fatal("migration reused a purged request ID", sequence, err)
					}
					items, err := s.ExtensionRequests(t.Context(), "alice", now)
					want := 3
					if empty {
						want = 0
					}
					if err != nil || len(items) != want {
						t.Fatal("migration lost history", len(items), err)
					}
					if !empty {
						for _, item := range items {
							if item.Key != item.Status || item.EstimatedAt.UnixMilli() != 1792500000000 || item.CreatedAt.UnixMilli() != 1792300000000 || item.URL != "https://app.plex.tv/" {
								t.Fatalf("request altered by migration: %+v", item)
							}
						}
						if items[0].AdminNote != "Too late" || items[1].AdminNote != "Examined" || items[1].Days != 14 || items[1].Reason != "Watch" || items[1].UpdatedAt.UnixMilli() != 1792400000000 {
							t.Fatal("migration changed review details", items)
						}
					}
					item, err := s.CreateExtensionRequest(t.Context(), "alice", ExtensionRequest{Key: "new", MediaID: "46", Title: "New", Kind: "movie", EstimatedAt: now.Add(time.Hour), Days: 7}, now)
					if err != nil || item.ID != 100 {
						t.Fatal("new request reused an ID", item.ID, err)
					}
					if err := s.CancelExtensionRequest(t.Context(), "alice", item.ID, now); err != nil {
						t.Fatal("migration did not allow cancellation", err)
					}
				} else {
					var status string
					if err := s.DB.QueryRow(`SELECT status FROM extension_requests WHERE id=100`).Scan(&status); err != nil || status != "cancelled" {
						t.Fatal("restart lost cancellation", status, err)
					}
					if _, err := s.DB.Exec(`UPDATE extension_requests SET status='expired' WHERE id=100`); err == nil {
						t.Fatal("derived expiry was accepted as a stored review state")
					}
					if err := s.Delete(t.Context(), "alice"); err != nil {
						t.Fatal(err)
					}
					var rows int
					if err := s.DB.QueryRow(`SELECT COUNT(*) FROM extension_requests`).Scan(&rows); err != nil || rows != 0 {
						t.Fatal("migration lost owner cascade", rows, err)
					}
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
