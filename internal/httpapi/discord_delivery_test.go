package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/notifications"
	"github.com/hloiseau/le-concierge/internal/store"
)

type reminderConsentBody struct {
	reader  *strings.Reader
	eof     chan struct{}
	release chan struct{}
	ctx     context.Context
	once    sync.Once
}

func (r *reminderConsentBody) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err == io.EOF {
		r.once.Do(func() { close(r.eof) })
		select {
		case <-r.release:
		case <-r.ctx.Done():
			return n, r.ctx.Err()
		}
	}
	return n, err
}

func TestDiscordConsentCannotOutliveGateAuthorization(t *testing.T) {
	for _, mode := range []string{"logout", "discord_changed"} {
		t.Run(mode, func(t *testing.T) {
			s := fixture(t)
			s.Config.DiscordDMsEnabled = true
			cookie := seed(t, s, "11111", true)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			body := &reminderConsentBody{reader: strings.NewReader(`{"enabled":true,"discordDmEnabled":true,"daysBefore":[7,2],"quietStartHour":22,"quietEndHour":8,"dailyCap":10,"allUnwatched":false}`), eof: make(chan struct{}), release: make(chan struct{}), ctx: ctx}
			defer func() {
				select {
				case <-body.release:
				default:
					close(body.release)
				}
			}()
			r := httptest.NewRequest(http.MethodPut, "/api/v1/reminders/preferences", body).WithContext(ctx)
			r.Header.Set("Origin", s.Config.PublicOrigin)
			r.AddCookie(&http.Cookie{Name: s.cookieName(), Value: cookie})
			w := httptest.NewRecorder()
			s.reminderGate <- struct{}{}
			released := false
			defer func() {
				if !released {
					<-s.reminderGate
				}
			}()
			done := make(chan struct{})
			go func() { s.Handler().ServeHTTP(w, r); close(done) }()
			// EOF is reached only after the initial user/grant authorization. No
			// scheduler delay or wall-clock sleep is needed to reproduce the race.
			select {
			case <-body.eof:
			case <-done:
				t.Fatalf("consent ended before waiting: %d %s", w.Code, w.Body)
			case <-ctx.Done():
				t.Fatal("consent did not reach the gate boundary")
			}
			if mode == "logout" {
				if out := call(s, http.MethodPost, "/api/v1/auth/logout", cookie, s.Config.PublicOrigin, ""); out.Code != 200 {
					t.Fatal(out.Body)
				}
			} else if _, err := s.Store.DB.Exec(`UPDATE users SET discord_id='22222' WHERE id='11111'`); err != nil {
				t.Fatal(err)
			}
			close(body.release)
			<-s.reminderGate
			released = true
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("consent did not finish after releasing gate")
			}
			if w.Code != 403 {
				t.Fatalf("stale consent accepted: %d %s", w.Code, w.Body)
			}
			p, err := s.Store.GetReminderPreferences(context.Background(), "11111")
			if err != nil || p.Enabled || p.DiscordDM {
				t.Fatalf("stale consent persisted: %+v %v", p, err)
			}
		})
	}
}

func queuedDiscordFixture(t *testing.T) (*Server, store.User, *reminderSources) {
	t.Helper()
	s, _, sources := reminderFixture(t)
	s.Config.DiscordDMsEnabled = true
	s.Discord = notifications.NewDiscord("test-discord-token")
	p, err := s.Store.GetReminderPreferences(context.Background(), "11111")
	if err != nil {
		t.Fatal(err)
	}
	p.DiscordDM = true
	if err = s.Store.SaveReminderPreferences(context.Background(), "11111", p); err != nil {
		t.Fatal(err)
	}
	u, err := s.Store.User(context.Background(), "11111")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.refreshReminders(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err = s.Store.DB.QueryRow(`SELECT COUNT(*) FROM notification_deliveries`).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("jobs=%d %v", jobs, err)
	}
	return s, u, sources
}

func discordState(t *testing.T, s *Server) string {
	t.Helper()
	var state string
	if err := s.Store.DB.QueryRow(`SELECT state FROM notification_deliveries`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestDiscordWorkerFreshSourcesAndDurableDedup(t *testing.T) {
	s, u, sources := queuedDiscordFixture(t)
	calls := 0
	s.Discord.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path == "/api/v10/users/@me/channels" {
			return response(200, `{"id":"456","type":1,"recipients":[{"id":"11111"}]}`), nil
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(payload["content"].(string), "Private reminder movie") || payload["enforce_nonce"] != true {
			t.Fatalf("payload=%v", payload)
		}
		return response(200, `{"id":"789","channel_id":"456"}`), nil
	})
	if err := s.deliverDiscordReminders(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || sources.collectionReads != 2 || sources.metadataReads != 2 || discordState(t, s) != "sent" {
		t.Fatalf("calls=%d freshSources=%d state=%s", calls, sources.collectionReads, discordState(t, s))
	}
	if err := s.deliverDiscordReminders(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || sources.collectionReads != 2 {
		t.Fatal("sent notification was retried")
	}
}

func TestDiscordWorkerRejectsObsoleteOrRevokedDecisions(t *testing.T) {
	for _, mode := range []string{"completed", "excluded", "inactive", "moved_deadline", "metadata_denied", "logout_during_read", "unlink_during_read", "opt_out", "server_off", "quiet_hours"} {
		t.Run(mode, func(t *testing.T) {
			s, u, sources := queuedDiscordFixture(t)
			s.Discord.HTTP.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalidated notification contacted Discord")
				return nil, nil
			})
			switch mode {
			case "completed":
				sources.completed = true
			case "excluded":
				sources.excluded = true
			case "inactive":
				sources.inactive = true
			case "moved_deadline":
				sources.addedAt = sources.addedAt.Add(time.Hour)
			case "metadata_denied":
				sources.metadataStatus = 403
			case "logout_during_read":
				sources.beforeMetadata = func() { s.mu.Lock(); delete(s.grants, u.ID); s.mu.Unlock() }
			case "unlink_during_read":
				sources.beforeMetadata = func() {
					if err := s.Store.Unlink(context.Background(), u.ID); err != nil {
						t.Fatal(err)
					}
				}
			case "opt_out":
				p, _ := s.Store.GetReminderPreferences(context.Background(), u.ID)
				p.DiscordDM = false
				if err := s.Store.SaveReminderPreferences(context.Background(), u.ID, p); err != nil {
					t.Fatal(err)
				}
			case "server_off":
				s.Config.DiscordDMsEnabled = false
			case "quiet_hours":
				loc, _ := time.LoadLocation("Europe/Paris")
				hour := time.Now().In(loc).Hour()
				if _, err := s.Store.DB.Exec(`UPDATE reminder_preferences SET quiet_start=?,quiet_end=? WHERE user_id=?`, hour, (hour+1)%24, u.ID); err != nil {
					t.Fatal(err)
				}
			}
			_ = s.deliverDiscordReminders(context.Background(), u)
			state := discordState(t, s)
			if mode == "server_off" {
				if state != "queued" {
					t.Fatal(state)
				}
			} else if state != "failed" {
				t.Fatalf("invalidated job state=%s", state)
			}
		})
	}
}

func TestDiscordWorkerRetriesUnavailableSourcesWithoutOldContent(t *testing.T) {
	s, u, sources := queuedDiscordFixture(t)
	sources.failing = true
	s.Discord.HTTP.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("stale notification sent despite source failure")
		return nil, nil
	})
	if err := s.deliverDiscordReminders(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if discordState(t, s) != "queued" {
		t.Fatal("temporary failure not retryable")
	}
	var attempts int
	var code string
	if err := s.Store.DB.QueryRow(`SELECT attempts,last_error FROM notification_deliveries`).Scan(&attempts, &code); err != nil || attempts != 1 || code != "source_unavailable" {
		t.Fatalf("retry=%d %s %v", attempts, code, err)
	}
}

func TestDiscordWorkerRechecksAfterDMSetup(t *testing.T) {
	for _, mode := range []string{"logout", "opt_out", "identity_changed", "lease_expired"} {
		t.Run(mode, func(t *testing.T) {
			s, u, _ := queuedDiscordFixture(t)
			calls := 0
			s.Discord.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path != "/api/v10/users/@me/channels" {
					t.Fatal("revoked message POST")
				}
				switch mode {
				case "logout":
					s.mu.Lock()
					delete(s.grants, u.ID)
					s.mu.Unlock()
				case "opt_out":
					if err := s.Store.Preferences(context.Background(), u.ID, false); err != nil {
						t.Fatal(err)
					}
				case "identity_changed":
					if _, err := s.Store.DB.Exec(`UPDATE users SET discord_id='22222' WHERE id=?`, u.ID); err != nil {
						t.Fatal(err)
					}
				case "lease_expired":
					if _, err := s.Store.DB.Exec(`UPDATE notification_deliveries SET lease_until=0`); err != nil {
						t.Fatal(err)
					}
				}
				return response(200, `{"id":"456","type":1,"recipients":[{"id":"11111"}]}`), nil
			})
			_ = s.deliverDiscordReminders(context.Background(), u)
			if calls != 1 || discordState(t, s) == "sent" {
				t.Fatal("late revocation leaked private content")
			}
		})
	}
}

func TestDiscordWorkerRetriesWithStableNonceAndStopsClosedDMs(t *testing.T) {
	for _, status := range []int{503, 403} {
		s, u, _ := queuedDiscordFixture(t)
		nonces := []string{}
		s.Discord.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/api/v10/users/@me/channels" {
				return response(200, `{"id":"456","type":1,"recipients":[{"id":"11111"}]}`), nil
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			nonces = append(nonces, payload["nonce"].(string))
			if len(nonces) == 1 {
				return response(status, `{"message":"private error"}`), nil
			}
			return response(200, `{"id":"789","channel_id":"456"}`), nil
		})
		if err := s.deliverDiscordReminders(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Store.DB.Exec(`UPDATE notification_deliveries SET not_before=0`); err != nil {
			t.Fatal(err)
		}
		if err := s.deliverDiscordReminders(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		if status == 503 && (len(nonces) != 2 || nonces[0] != nonces[1] || discordState(t, s) != "sent") {
			t.Fatalf("retry mismatch %v", nonces)
		}
		if status == 403 && (len(nonces) != 1 || discordState(t, s) != "failed") {
			t.Fatalf("closed DM retried %v", nonces)
		}
	}
}
