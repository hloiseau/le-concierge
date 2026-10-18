package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/hloiseau/le-concierge/internal/departures"
)

func TestDepartureCommandUsesCurrentPersonalReadOnlySources(t *testing.T) {
	for _, state := range []string{"success", "other account", "unlinked", "expired", "not configured", "limited", "source failure", "completed", "logout during read", "unlink during read", "delete during read", "identity changed during read"} {
		t.Run(state, func(t *testing.T) {
			s, cookie, source := reminderFixture(t)
			user := "11111"
			switch state {
			case "other account":
				seed(t, s, "22222", true)
				user = "22222"
			case "unlinked":
				user = "33333"
			case "expired":
				delete(s.grants, user)
			case "not configured":
				s.Config.MaintainerrURL = ""
			case "limited":
				for range 6 {
					s.rate("departures:"+user, 6)
				}
			case "source failure":
				source.failing = true
			case "completed":
				source.completed = true
			default:
				source.beforeMetadata = func() {
					switch state {
					case "logout during read":
						if w := call(s, "POST", "/api/v1/auth/logout", cookie, s.Config.PublicOrigin, ""); w.Code != 200 {
							t.Fatalf("logout: %d %s", w.Code, w.Body)
						}
					case "unlink during read":
						if err := s.Store.Unlink(context.Background(), user); err != nil {
							t.Fatal(err)
						}
					case "delete during read":
						if err := s.Store.Delete(context.Background(), user); err != nil {
							t.Fatal(err)
						}
					case "identity changed during read":
						if err := s.Store.Unlink(context.Background(), user); err != nil {
							t.Fatal(err)
						}
						if err := s.Store.LinkPlex(context.Background(), user, "22222"); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			upstream := s.Upstream.HTTP.Transport
			s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 20*time.Second {
					t.Error("departure read has no bounded total duration")
				}
				return upstream.RoundTrip(r)
			})
			w := subscriptionInteraction(t, s)("90001", user, `{"name":"a-voir"}`)
			var reply struct {
				Content       string `json:"content"`
				PublicContent string `json:"publicContent"`
				Flags         int    `json:"flags"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Flags != 64 || reply.PublicContent != "" || !strings.Contains(w.Body.String(), `"parse":[]`) {
				t.Fatalf("non-private departure reply: %d %s", w.Code, w.Body)
			}
			if strings.Contains(reply.Content, "Private reminder movie") != (state == "success") {
				t.Fatalf("wrong personal result: %s", reply.Content)
			}
			if state == "success" && (!strings.Contains(reply.Content, "Film · Lecture commencée") || !strings.Contains(reply.Content, "Départ estimé") || !strings.Contains(reply.Content, "https://app.plex.tv/") || !strings.Contains(reply.Content, "/avant-le-depart")) {
				t.Fatalf("missing useful departure details: %s", reply.Content)
			}
			if (state == "other account" || state == "completed") && !strings.Contains(reply.Content, "Aucun départ confirmé") {
				t.Fatalf("empty personal result lost: %s", reply.Content)
			}
			if (state == "unlinked" || state == "expired" || state == "not configured" || state == "limited") && source.collectionReads != 0 {
				t.Fatal("denied command read personal sources")
			}
			for _, table := range []string{"reminder_outbox", "reminder_deliveries", "channel_reminders"} {
				var count int
				if err := s.Store.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("read command queued a notification in %s: %d %v", table, count, err)
				}
			}
		})
	}
}

func TestDepartureCommandNeverFallsBackToOldHistory(t *testing.T) {
	s, _, _ := reminderFixture(t)
	request := subscriptionInteraction(t, s)
	if w := request("90001", "11111", `{"name":"a-voir"}`); !strings.Contains(w.Body.String(), "Private reminder movie") {
		t.Fatalf("fixture did not produce a departure: %s", w.Body)
	}
	upstream := s.Upstream.HTTP.Transport
	s.Upstream.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("cmd") == "get_history" {
			return response(503, "private-history-error"), nil
		}
		return upstream.RoundTrip(r)
	})
	w := request("90002", "11111", `{"name":"a-voir"}`)
	if strings.Contains(w.Body.String(), "Private reminder movie") || !strings.Contains(w.Body.String(), "ne peuvent pas être vérifiés") {
		t.Fatalf("old private result reused during source failure: %s", w.Body)
	}
}

func TestDepartureMessageStatesScopeDatesAndBounds(t *testing.T) {
	now := time.Date(2026, 10, 18, 21, 0, 0, 0, time.UTC)
	page := departures.Page{UpdatedAt: now, ExpiresAt: now.Add(time.Minute)}
	for _, item := range []departures.Item{
		{Kind: "movie", Title: "@everyone Film", Watch: "in_progress"},
		{Kind: "episode", Title: "Série · Saison 1 · Épisode 2", Watch: "unstarted"},
		{Kind: "season", Title: "Série · Saison 2", Watch: "unknown"},
		{Kind: "show", Title: "Toute la série", Watch: "unknown"},
		{Kind: "movie", Title: "Already completed", Watch: "completed"},
	} {
		item.EstimatedAt = time.Date(2026, 10, 19, 23, 30, 0, 0, time.UTC)
		page.Items = append(page.Items, item)
	}
	text := departureMessage(page, "https://hub.example", now)
	for _, want := range []string{"Film · Lecture commencée", "Épisode · Marqué non vu dans Plex", "Saison · Visionnage incertain", "Série · Visionnage incertain", "Départ estimé le 20/10/2026", "18/10/2026 à 23:00", "Échéances estimées, susceptibles de changer", "https://hub.example/avant-le-depart"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "@everyone") || strings.Contains(text, "Already completed") {
		t.Fatalf("mention or completed item shown: %s", text)
	}
	for _, title := range []string{"Film", strings.Repeat("😀", 200)} {
		page.Items = nil
		for range 100 {
			page.Items = append(page.Items, departures.Item{Kind: "movie", Title: title, Watch: "unknown", EstimatedAt: now.Add(time.Hour)})
		}
		text = departureMessage(page, "https://hub.example", now)
		shown := strings.Count(text, "• Film")
		if shown == 0 || shown > 8 || len(text) > 2000 || len(utf16.Encode([]rune(text))) > 2000 || !strings.Contains(text, fmt.Sprintf("%d autre(s) contenu(s)", 100-shown)) || !strings.Contains(text, "/avant-le-depart") {
			t.Fatalf("items silently truncated or oversized: %d shown, %d bytes: %s", shown, len(text), text)
		}
	}
	if text = departureMessage(page, "https://"+strings.Repeat("x", 3000)+".example", now); len(text) > 2000 {
		t.Fatal("long configured origin exceeded message limit")
	}
	if text = departureMessage(page, "https://hub.example", page.ExpiresAt); strings.Contains(text, "• Film") || !strings.Contains(text, "plus à jour") {
		t.Fatalf("expired source remained visible: %s", text)
	}
}
