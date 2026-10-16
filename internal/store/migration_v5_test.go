package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestV5MigrationPreservesExistingConsentWithoutNewAlerts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v4.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY);
CREATE TABLE users(id TEXT PRIMARY KEY,discord_id TEXT UNIQUE,name TEXT NOT NULL,plex_id TEXT UNIQUE,seerr_id INTEGER UNIQUE,notifications INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
INSERT INTO schema_migrations VALUES(1),(2);
` + remindersSchema + subscriptionsSchema + notificationDeliverySchema + extensionRequestsSchema + `
INSERT INTO schema_migrations VALUES(4);
INSERT INTO users(id,discord_id,name,plex_id,seerr_id,notifications) VALUES('11111','11111','Alice','77',42,1);
INSERT INTO reminder_preferences(user_id,days_before,quiet_start,quiet_end,daily_cap,all_unwatched,discord_dm) VALUES('11111','[5,1]',23,7,4,1,1);
INSERT INTO subscriptions(id,user_id,media_type,tmdb_id,season,channel,created_at) VALUES(1,'11111','movie',10,-1,'discord','2026-09-01T12:00:00Z');
INSERT INTO subscription_availability(subscription_id,available_at) VALUES(1,'2026-10-01T12:00:00Z');
INSERT INTO reminder_outbox(user_id,decision_key,media_key,title,plex_url,reason,milestone_days,due_at,not_before,expires_at,created_at,state,sent_at) VALUES('11111','already-delivered','42:action','Movie','https://app.plex.tv','unfinished_before_removal',2,1792500000000,1792300000000,1792500000000,1792300000000,'sent',1792300000000);
`)
	legacy.Close()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		p, err := s.GetReminderPreferences(ctx, "11111")
		if err != nil || !p.Enabled || !p.DiscordDM || !p.AllUnwatched || p.DailyCap != 4 || p.QuietStartHour != 23 || p.QuietEndHour != 7 || len(p.DaysBefore) != 2 || p.DaysBefore[0] != 5 {
			t.Fatalf("existing preferences changed: %+v %v", p, err)
		}
		var newConsent, events, delivered int
		if err = s.DB.QueryRow(`SELECT available_unwatched FROM reminder_preferences WHERE user_id='11111'`).Scan(&newConsent); err != nil || newConsent != 0 {
			t.Fatal("new delayed reminder consent was inferred", err)
		}
		if err = s.DB.QueryRow(`SELECT COUNT(*) FROM availability_events`).Scan(&events); err != nil || events != 0 {
			t.Fatal("migration backfilled old availability notifications", err)
		}
		if err = s.DB.QueryRow(`SELECT COUNT(*) FROM reminder_outbox WHERE state='sent' AND decision_key='already-delivered' AND available_at IS NULL`).Scan(&delivered); err != nil || delivered != 1 {
			t.Fatal("existing delivery history changed", err)
		}
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
