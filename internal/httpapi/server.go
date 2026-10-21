package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/departures"
	"github.com/hloiseau/le-concierge/internal/discord"
	"github.com/hloiseau/le-concierge/internal/monitoring"
	"github.com/hloiseau/le-concierge/internal/notifications"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type session struct {
	UserID  string
	Expires time.Time
}
type oauthAttempt struct {
	Expires    time.Time
	SessionKey string
	UserID     string
}
type pendingPin struct {
	Pin        upstream.Pin
	ClientID   string
	Expires    time.Time
	LastPoll   time.Time
	Processing bool
}
type bucket struct {
	Since time.Time
	Count int
}
type Server struct {
	Config            config.Config
	Store             *store.Store
	Upstream          *upstream.Client
	Departures        *departures.Service
	Discord           *notifications.Discord
	Monitoring        *monitoring.Service
	mu                sync.Mutex
	sessions          map[string]session
	oauth             map[string]oauthAttempt
	pins              map[string]pendingPin
	grants            map[string]upstream.Grant
	bookGrants        map[string]bookGrant
	shelfmarkGrants   map[string]shelfmarkGrant
	downloadarrGrants map[string]downloadarrGrant
	limits            map[string]bucket
	seen              map[string]time.Time
	reminderGate      chan struct{}
}

func New(c config.Config, db *store.Store) *Server {
	client := upstream.New()
	s := &Server{Config: c, Store: db, Upstream: client, Departures: departures.New(client), sessions: map[string]session{}, oauth: map[string]oauthAttempt{}, pins: map[string]pendingPin{}, grants: map[string]upstream.Grant{}, limits: map[string]bucket{}, seen: map[string]time.Time{}, reminderGate: make(chan struct{}, 1)}
	if c.DiscordDMsEnabled || c.DiscordNotificationChannelID != "" {
		s.Discord = notifications.NewDiscord(c.DiscordBotToken)
	}
	s.Monitoring = monitoring.New(client, db)
	return s
}
func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic("random source unavailable")
	}
	return hex.EncodeToString(b)
}
func digest(v string) string { b := sha256.Sum256([]byte(v)); return hex.EncodeToString(b[:]) }
func jsonOut(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func problem(w http.ResponseWriter, status int, code, message string) {
	jsonOut(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func (s *Server) cookieName() string {
	if s.Config.Env == "development" {
		return "concierge_session"
	}
	return "__Host-concierge_session"
}
func (s *Server) oauthCookieName() string {
	if s.Config.Env == "development" {
		return "concierge_oauth"
	}
	return "__Host-concierge_oauth"
}
func (s *Server) cookie(w http.ResponseWriter, name, value string, maxAge int, lax bool) {
	same := http.SameSiteStrictMode
	if lax {
		same = http.SameSiteLaxMode
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: s.Config.Env != "development", HttpOnly: true, SameSite: same, MaxAge: maxAge})
}
func (s *Server) session(r *http.Request) (string, session, bool) {
	c, e := r.Cookie(s.cookieName())
	if e != nil {
		return "", session{}, false
	}
	key := digest(c.Value)
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[key]
	if !ok || !time.Now().Before(v.Expires) {
		delete(s.sessions, key)
		return key, v, false
	}
	return key, v, true
}
func (s *Server) user(w http.ResponseWriter, r *http.Request) (string, store.User, bool) {
	key, v, ok := s.session(r)
	if !ok {
		problem(w, 401, "unauthenticated", "Connectez-vous avec Plex.")
		return "", store.User{}, false
	}
	u, e := s.Store.User(r.Context(), v.UserID)
	if e != nil {
		problem(w, 401, "unauthenticated", "Reconnectez-vous avec Plex.")
		return "", u, false
	}
	if _, ok := s.requireGrant(w, r, u); !ok {
		return "", store.User{}, false
	}
	return key, u, true
}
func (s *Server) origin(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != s.Config.PublicOrigin {
		problem(w, 403, "invalid_origin", "Origine de la requête refusée.")
		return false
	}
	return true
}

// Remote reads can outlive a logout, an account deletion or an identity change.
func (s *Server) revalidateUser(w http.ResponseWriter, r *http.Request, key string, user store.User) bool {
	currentKey, current, ok := s.session(r)
	if !ok || currentKey != key || current.UserID != user.ID {
		problem(w, 401, "unauthenticated", "Reconnectez-vous avec Plex.")
		return false
	}
	u, err := s.Store.User(r.Context(), user.ID)
	if err != nil || u.PlexID != user.PlexID {
		problem(w, 403, "plex_required", "Reconnectez-vous avec Plex.")
		return false
	}
	_, ok = s.requireGrant(w, r, u)
	return ok
}
func (s *Server) limited(w http.ResponseWriter, r *http.Request, scope string, max int) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if s.rate(scope+":"+host, max) {
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "rate_limited", "Patientez avant de réessayer.")
		return true
	}
	return false
}
func (s *Server) rate(key string, max int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, b := range s.limits {
		if now.Sub(b.Since) > time.Minute {
			delete(s.limits, k)
		}
	}
	b := s.limits[key]
	if now.Sub(b.Since) > time.Minute {
		b = bucket{Since: now}
	}
	b.Count++
	s.limits[key] = b
	return b.Count > max || len(s.limits) > 2000
}
func (s *Server) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.sessions {
		if !now.Before(v.Expires) {
			delete(s.sessions, k)
			delete(s.pins, k)
		}
	}
	for k, v := range s.oauth {
		if now.After(v.Expires) {
			delete(s.oauth, k)
		}
	}
	for k, v := range s.pins {
		if now.After(v.Expires) {
			delete(s.pins, k)
		}
	}
	for k, v := range s.grants {
		if !v.Active(now) {
			delete(s.grants, k)
			s.clearBookGrantLocked(k)
			s.clearShelfmarkGrantLocked(k)
			s.clearDownloadarrGrantLocked(k)
		}
	}
	for userID, g := range s.downloadarrGrants {
		sess, exists := s.sessions[g.SessionKey]
		if !now.Before(g.ExpiresAt) || g.Context.Err() != nil || !exists || sess.UserID != userID || !now.Before(sess.Expires) {
			s.clearDownloadarrGrantLocked(userID)
		}
	}
	for k, v := range s.seen {
		if now.After(v) {
			delete(s.seen, k)
		}
	}
}
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { jsonOut(w, 200, map[string]string{"status": "ok"}) })
	m.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if s.Store.DB.PingContext(r.Context()) != nil {
			problem(w, 503, "not_ready", "Database unavailable")
			return
		}
		jsonOut(w, 200, map[string]string{"status": "ready"})
	})
	m.HandleFunc("GET /api/v1/capabilities", s.capabilities)
	m.HandleFunc("GET /api/v1/services", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := s.user(w, r); !ok {
			return
		}
		jsonOut(w, 200, map[string]any{"items": s.Config.Services})
	})
	m.HandleFunc("GET /api/v1/session", s.getSession)
	m.HandleFunc("GET /api/v1/auth/discord/start", s.login)
	m.HandleFunc("GET /api/v1/auth/discord/callback", s.callback)
	m.HandleFunc("POST /api/v1/auth/logout", s.logout)
	m.HandleFunc("POST /api/v1/auth/plex/start", s.plexStart)
	m.HandleFunc("POST /api/v1/auth/plex/poll", s.plexPoll)
	m.HandleFunc("DELETE /api/v1/account", s.deleteAccount)
	m.HandleFunc("GET /api/v1/requests", s.requests)
	m.HandleFunc("GET /api/v1/recent", s.recent)
	m.HandleFunc("GET /api/v1/posters/plex/{id}", s.plexPoster)
	m.HandleFunc("GET /api/v1/departures", s.departures)
	m.HandleFunc("GET /api/v1/reminders", s.reminderInbox)
	m.HandleFunc("GET /api/v1/reminders/preferences", s.getReminderPreferences)
	m.HandleFunc("PUT /api/v1/reminders/preferences", s.putReminderPreferences)
	m.HandleFunc("GET /api/v1/subscriptions", s.getSubscriptions)
	m.HandleFunc("POST /api/v1/subscriptions", s.postSubscription)
	m.HandleFunc("DELETE /api/v1/subscriptions/{id}", s.deleteSubscription)
	m.HandleFunc("PATCH /api/v1/subscriptions/{id}", s.patchSubscriptionChannel)
	m.HandleFunc("GET /api/v1/subscriptions/notifications", s.availabilityInbox)
	m.HandleFunc("GET /api/v1/books/link", s.booksLink)
	m.HandleFunc("POST /api/v1/books/link", s.booksLink)
	m.HandleFunc("DELETE /api/v1/books/link", s.booksLink)
	m.HandleFunc("GET /api/v1/books", s.booksCatalog)
	m.HandleFunc("GET /api/v1/books/recent", s.recentBooks)
	m.HandleFunc("GET /api/v1/book-collections", s.bookCollections)
	m.HandleFunc("GET /api/v1/books/{id}/cover", s.booksCover)
	m.HandleFunc("GET /api/v1/book-requests/link", s.bookRequestLink)
	m.HandleFunc("GET /api/v1/book-requests/link-session", s.shelfmarkLink)
	m.HandleFunc("POST /api/v1/book-requests/link-session", s.shelfmarkLink)
	m.HandleFunc("DELETE /api/v1/book-requests/link-session", s.shelfmarkLink)
	m.HandleFunc("GET /api/v1/book-requests", s.shelfmarkRequests)
	m.HandleFunc("GET /api/v1/downloads/link", s.downloadarrLink)
	m.HandleFunc("POST /api/v1/downloads/link", s.downloadarrLink)
	m.HandleFunc("DELETE /api/v1/downloads/link", s.downloadarrLink)
	m.HandleFunc("POST /api/v1/downloads/link/poll", s.downloadarrPoll)
	m.HandleFunc("GET /api/v1/downloads", s.downloadarrJobs)
	m.HandleFunc("POST /api/v1/downloads", s.downloadarrCreate)
	m.HandleFunc("GET /api/v1/downloads/options", s.downloadarrOptions)
	m.HandleFunc("GET /api/v1/downloads/library", s.downloadsLibrary)
	m.HandleFunc("DELETE /api/v1/downloads/{id}", s.downloadarrCancel)
	m.HandleFunc("POST /api/v1/downloads/{id}/file", s.downloadarrFile)
	m.HandleFunc("GET /api/v1/monitoring", s.getMonitoring)
	m.HandleFunc("GET /api/v1/monitoring/history", s.getMonitoringHistory)
	m.HandleFunc("GET /api/v1/admin/plex-activity", s.adminPlexActivity)
	m.HandleFunc("GET /api/v1/admin/diagnostics", s.adminDiagnostic)
	m.HandleFunc("GET /api/v1/community", s.getCommunity)
	m.HandleFunc("GET /api/v1/roadmap", s.getRoadmap)
	m.HandleFunc("GET /api/v1/extension-requests", s.getExtensionRequests)
	m.HandleFunc("POST /api/v1/extension-requests", s.postExtensionRequest)
	m.HandleFunc("DELETE /api/v1/extension-requests/{id}", s.cancelExtensionRequest)
	m.HandleFunc("GET /api/v1/admin/extension-requests", s.adminExtensionRequests)
	m.HandleFunc("PUT /api/v1/admin/extension-requests", s.reviewExtensionRequest)
	m.HandleFunc("GET /api/v1/preferences", s.getPreferences)
	m.HandleFunc("PUT /api/v1/preferences", s.putPreferences)
	m.HandleFunc("POST /internal/discord", s.interaction)
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { problem(w, 404, "not_found", "Route inconnue.") })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := token()[:16]
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.ContentLength > 64*1024 {
			problem(w, 413, "too_large", "Requête trop volumineuse.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		_, pattern := m.Handler(r)
		if pattern != "POST /api/v1/downloads/{id}/file" {
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			r = r.WithContext(ctx)
		}
		start := time.Now()
		s.cleanup()
		m.ServeHTTP(w, r)
		slog.Info("request", "request_id", id, "method", r.Method, "route", r.Pattern, "duration_ms", time.Since(start).Milliseconds())
	})
}
func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	jsonOut(w, 200, map[string]any{
		"version": "0.10.0", "persistentPlexAuthorization": s.Config.PlexGrantKey != "", "downloads": s.downloadarrEnabled(), "discordLogin": s.Config.OAuthEnabled(), "plexLink": s.Config.PlexURL != "",
		"requests": s.Config.SeerrURL != "", "recent": s.Config.PlexURL != "", "departures": s.departuresEnabled(),
		"subscriptions": s.subscriptionsEnabled(), "subscriptionNotifications": s.subscriptionsEnabled(),
		"notifications":           s.departuresEnabled() || s.subscriptionsEnabled(),
		"discordDMs":              s.Config.DiscordDMsEnabled && (s.departuresEnabled() || s.subscriptionsEnabled()),
		"discordChannelReminders": s.Config.DiscordNotificationChannelID != "" && s.Discord != nil,
		"extensionRequests":       s.extensionsEnabled(), "books": s.Config.GrimmoryURL != "", "booksCatalog": s.Config.GrimmoryURL != "", "recentBooks": s.Config.GrimmoryURL != "",
		"bookRequests": s.bookServiceURL("shelfmark") != "", "bookRequestTracking": s.Config.ShelfmarkURL != "", "bookCollections": s.Config.GrimmoryURL != "", "monitoring": s.Config.MonitoringEnabled,
		"community": s.Config.CommunityFeedFile != "" && len(s.Config.CommunityForumIDs) > 0,
		"roadmap":   s.Config.CommunityFeedFile != "" && len(s.Config.RoadmapIssueIDs) > 0,
	})
}
func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	_, v, ok := s.session(r)
	if !ok {
		jsonOut(w, 200, map[string]any{"user": nil})
		return
	}
	u, e := s.Store.User(r.Context(), v.UserID)
	if e != nil {
		jsonOut(w, 200, map[string]any{"user": nil})
		return
	}
	if _, err := s.grant(r.Context(), u); err != nil {
		if errors.Is(err, upstream.ErrUnavailable) {
			problem(w, 503, "plex_unavailable", "La vérification Plex est momentanément indisponible. Réessayez.")
		} else {
			jsonOut(w, 200, map[string]any{"user": nil})
		}
		return
	}
	u.SeerrID = 0
	if s.Config.SeerrURL != "" && !s.rate("seerr-session:"+u.ID, 30) {
		u.SeerrID, _ = s.resolveSeerr(r.Context(), u)
	}
	jsonOut(w, 200, map[string]any{"user": u, "admin": s.Config.Admin(u.DiscordID), "plexConnected": true, "expiresAt": v.Expires.UTC().Format(time.RFC3339)})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	if !s.Config.OAuthEnabled() {
		problem(w, 503, "not_configured", "La connexion Discord n’est pas encore configurée.")
		return
	}
	if s.limited(w, r, "login", 10) {
		return
	}
	state := token()
	s.mu.Lock()
	if len(s.oauth) >= 1000 {
		s.mu.Unlock()
		problem(w, 503, "busy", "Réessayez plus tard.")
		return
	}
	s.oauth[digest(state)] = oauthAttempt{Expires: time.Now().Add(5 * time.Minute), SessionKey: key, UserID: u.ID}
	s.mu.Unlock()
	s.cookie(w, s.oauthCookieName(), state, 300, true)
	q := url.Values{"client_id": {s.Config.DiscordClientID}, "redirect_uri": {s.Config.PublicOrigin + "/api/v1/auth/discord/callback"}, "response_type": {"code"}, "scope": {"identify guilds.members.read"}, "state": {state}, "prompt": {"consent"}}
	http.Redirect(w, r, "https://discord.com/oauth2/authorize?"+q.Encode(), http.StatusFound)
}
func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	c, e := r.Cookie(s.oauthCookieName())
	state := r.URL.Query().Get("state")
	if e != nil || len(state) != 64 || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		problem(w, 403, "invalid_state", "Connexion expirée ou invalide.")
		return
	}
	s.mu.Lock()
	attempt, ok := s.oauth[digest(state)]
	delete(s.oauth, digest(state))
	s.mu.Unlock()
	s.cookie(w, s.oauthCookieName(), "", -1, true)
	if !ok || time.Now().After(attempt.Expires) || r.URL.Query().Get("code") == "" {
		problem(w, 403, "invalid_state", "Relancez la connexion.")
		return
	}
	s.mu.Lock()
	sess, valid := s.sessions[attempt.SessionKey]
	s.mu.Unlock()
	if !valid || sess.UserID != attempt.UserID || !time.Now().Before(sess.Expires) {
		problem(w, 401, "unauthenticated", "Reconnectez-vous avec Plex avant de lier Discord.")
		return
	}
	u, err := s.Store.User(r.Context(), sess.UserID)
	if err != nil {
		problem(w, 401, "unauthenticated", "Reconnectez-vous avec Plex.")
		return
	}
	if _, ok := s.requireGrant(w, r, u); !ok {
		return
	}
	identity, e := s.Upstream.DiscordLogin(r.Context(), s.Config.DiscordClientID, s.Config.DiscordClientSecret, r.URL.Query().Get("code"), s.Config.PublicOrigin+"/api/v1/auth/discord/callback", s.Config.DiscordGuildID)
	if e != nil || !config.Snowflake(identity.ID) {
		problem(w, 403, "discord_denied", "Connexion refusée. Vérifiez votre accès au serveur Discord.")
		return
	}
	// Check the initiating session again after the provider round trip. Holding
	// this lock through the transaction prevents a concurrent logout from linking.
	s.mu.Lock()
	current, valid := s.sessions[attempt.SessionKey]
	if !valid || current != sess || !time.Now().Before(current.Expires) {
		s.mu.Unlock()
		problem(w, 401, "unauthenticated", "Reconnectez-vous avec Plex.")
		return
	}
	merged, err := s.Store.LinkDiscord(r.Context(), u.ID, u.PlexID, identity.ID)
	if err != nil {
		s.mu.Unlock()
		problem(w, 409, "identity_conflict", "Ce compte Discord est déjà lié à un autre compte Plex, ou votre compte possède une autre liaison Discord.")
		return
	}
	if merged != "" {
		for key, v := range s.sessions {
			if v.UserID == merged {
				delete(s.sessions, key)
			}
		}
		delete(s.grants, merged)
		s.clearBookGrantLocked(merged)
		s.clearShelfmarkGrantLocked(merged)
		s.clearDownloadarrGrantLocked(merged)
	}
	value := token()
	delete(s.sessions, attempt.SessionKey)
	s.sessions[digest(value)] = sess
	s.mu.Unlock()
	s.cookie(w, s.cookieName(), value, int(time.Until(sess.Expires).Seconds()), false)
	http.Redirect(w, r, "/compte", http.StatusSeeOther)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	key, v, ok := s.session(r)
	s.mu.Lock()
	if ok {
		if err := s.Store.DeletePlexAuthorization(r.Context(), v.UserID); err != nil {
			s.mu.Unlock()
			problem(w, 503, "storage_error", "Déconnexion impossible. Réessayez pour arrêter les rappels.")
			return
		}
	}
	delete(s.sessions, key)
	delete(s.pins, key)
	if pending, err := r.Cookie(s.plexCookieName()); err == nil {
		delete(s.pins, digest(pending.Value))
	}
	if ok {
		delete(s.grants, v.UserID)
		s.clearBookGrantLocked(v.UserID)
		s.clearShelfmarkGrantLocked(v.UserID)
		s.clearDownloadarrGrantLocked(v.UserID)
	}
	s.mu.Unlock()
	s.cookie(w, s.cookieName(), "", -1, false)
	s.cookie(w, s.plexCookieName(), "", -1, false)
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	_, u, ok := s.user(w, r)
	if !ok {
		return
	}
	if e := s.Store.Delete(r.Context(), u.ID); e != nil {
		problem(w, 500, "storage_error", "Suppression impossible.")
		return
	}
	s.mu.Lock()
	for k, v := range s.sessions {
		if v.UserID == u.ID {
			delete(s.sessions, k)
			delete(s.pins, k)
		}
	}
	delete(s.grants, u.ID)
	s.clearBookGrantLocked(u.ID)
	s.clearShelfmarkGrantLocked(u.ID)
	s.clearDownloadarrGrantLocked(u.ID)
	s.mu.Unlock()
	s.cookie(w, s.cookieName(), "", -1, false)
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (s *Server) plexCookieName() string {
	if s.Config.Env == "development" {
		return "concierge_plex"
	}
	return "__Host-concierge_plex"
}
func (s *Server) plexStart(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	if s.Config.PlexURL == "" || s.Config.PlexServerID == "" {
		problem(w, 503, "not_configured", "La connexion Plex n’est pas encore configurée.")
		return
	}
	if s.limited(w, r, "plex", 10) {
		return
	}
	client := "le-concierge-" + token()[:24]
	p, e := s.Upstream.PlexPin(r.Context(), client)
	if e != nil {
		problem(w, 502, "plex_unavailable", "Plex est momentanément indisponible.")
		return
	}
	ttl := min(p.ExpiresIn, 300)
	value := token()
	s.mu.Lock()
	if old, err := r.Cookie(s.plexCookieName()); err == nil {
		delete(s.pins, digest(old.Value))
	}
	if len(s.pins) >= 1000 {
		s.mu.Unlock()
		problem(w, 503, "busy", "Réessayez plus tard.")
		return
	}
	s.pins[digest(value)] = pendingPin{Pin: p, ClientID: client, Expires: time.Now().Add(time.Duration(ttl) * time.Second)}
	s.mu.Unlock()
	s.cookie(w, s.plexCookieName(), value, ttl, false)
	jsonOut(w, 200, map[string]any{"url": upstream.PlexAuthURL(client, p.Code), "expiresIn": ttl})
}
func (s *Server) plexPoll(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	c, e := r.Cookie(s.plexCookieName())
	if e != nil || len(c.Value) != 64 {
		problem(w, 410, "pin_expired", "Relancez la connexion Plex.")
		return
	}
	key := digest(c.Value)
	s.mu.Lock()
	p, exists := s.pins[key]
	if !exists || time.Now().After(p.Expires) {
		delete(s.pins, key)
		s.mu.Unlock()
		problem(w, 410, "pin_expired", "Relancez la connexion Plex.")
		return
	}
	if p.Processing {
		s.mu.Unlock()
		problem(w, 409, "pin_consumed", "Connexion en cours de vérification.")
		return
	}
	if time.Since(p.LastPoll) < 2*time.Second {
		s.mu.Unlock()
		problem(w, 429, "rate_limited", "Patientez deux secondes.")
		return
	}
	p.LastPoll = time.Now()
	s.pins[key] = p
	s.mu.Unlock()
	account, e := s.Upstream.PlexPoll(r.Context(), p.Pin, p.ClientID)
	if e != nil {
		problem(w, 502, "plex_unavailable", "Vérification Plex indisponible.")
		return
	}
	if account == "" {
		jsonOut(w, 200, map[string]bool{"connected": false})
		return
	}
	// Atomically claim the PIN; retain it until the final commit so cancellation
	// or replacement of this browser attempt also invalidates in-flight work.
	s.mu.Lock()
	current, exists := s.pins[key]
	if exists && !current.Processing && current.Pin.ID == p.Pin.ID {
		current.Processing = true
		s.pins[key] = current
	} else {
		exists = false
	}
	s.mu.Unlock()
	if !exists || current.Pin.ID != p.Pin.ID {
		problem(w, 409, "pin_consumed", "Cette connexion a déjà été traitée.")
		return
	}
	s.cookie(w, s.plexCookieName(), "", -1, false)
	g, e := s.Upstream.PlexVerify(r.Context(), account, p.ClientID, s.Config.PlexServerID)
	if e != nil {
		s.mu.Lock()
		delete(s.pins, key)
		s.mu.Unlock()
		if errors.Is(e, upstream.ErrUnavailable) {
			problem(w, 503, "plex_unavailable", "Plex est momentanément indisponible. Relancez la connexion.")
		} else {
			problem(w, 403, "plex_denied", "Ce compte Plex n’a pas accès à ce serveur.")
		}
		return
	}
	value := token()
	expires := time.Now().Add(12 * time.Hour)
	s.mu.Lock()
	current, exists = s.pins[key]
	delete(s.pins, key)
	if !exists || !current.Processing || current.Pin.ID != p.Pin.ID || !time.Now().Before(current.Expires) {
		s.mu.Unlock()
		problem(w, 410, "pin_expired", "Relancez la connexion Plex.")
		return
	}
	if len(s.sessions) >= 1000 {
		s.mu.Unlock()
		problem(w, 503, "busy", "Réessayez plus tard.")
		return
	}
	u, e := s.Store.LoginPlex(r.Context(), g.PlexID, g.Name)
	if e != nil {
		s.mu.Unlock()
		problem(w, 500, "storage_error", "Connexion impossible.")
		return
	}
	if old, err := r.Cookie(s.cookieName()); err == nil {
		delete(s.sessions, digest(old.Value))
	}
	g.ExpiresAt = expires
	if err := s.savePlexAuthorization(r.Context(), u, &g); err != nil {
		s.mu.Unlock()
		problem(w, 503, "storage_error", "Connexion impossible : autorisation Plex non enregistrée.")
		return
	}
	s.sessions[digest(value)] = session{u.ID, expires}
	s.clearBookGrantLocked(u.ID)
	s.clearShelfmarkGrantLocked(u.ID)
	s.clearDownloadarrGrantLocked(u.ID)
	s.grants[u.ID] = g
	s.mu.Unlock()
	s.cookie(w, s.cookieName(), value, 12*3600, false)
	jsonOut(w, 200, map[string]bool{"connected": true})
}

func (s *Server) grant(ctx context.Context, u store.User) (upstream.Grant, error) {
	s.mu.Lock()
	g, ok := s.grants[u.ID]
	s.mu.Unlock()
	if !ok || g.PlexID != u.PlexID || !g.Active(time.Now()) {
		return upstream.Grant{}, upstream.ErrDenied
	}
	if time.Since(g.VerifiedAt) < 5*time.Minute {
		return g, nil
	}
	fresh, e := s.Upstream.PlexVerify(ctx, g.AccountToken, g.ClientID, s.Config.PlexServerID)
	if e == nil && fresh.PlexID != u.PlexID {
		e = upstream.ErrDenied
	}
	if e != nil {
		if errors.Is(e, upstream.ErrDenied) {
			s.mu.Lock()
			// A slow rejected check must not revoke a newer login.
			if current, exists := s.grants[u.ID]; exists && current.AccountToken == g.AccountToken {
				if err := s.Store.DeletePlexAuthorization(ctx, u.ID); err != nil {
					slog.Warn("Revoked Plex authorization cleanup deferred")
				}
				delete(s.grants, u.ID)
				s.clearBookGrantLocked(u.ID)
				s.clearShelfmarkGrantLocked(u.ID)
				s.clearDownloadarrGrantLocked(u.ID)
			}
			s.mu.Unlock()
		}
		return upstream.Grant{}, e
	}
	fresh.ExpiresAt = g.ExpiresAt
	fresh.Persistent = g.Persistent
	s.mu.Lock()
	current, exists := s.grants[u.ID]
	if exists && current.AccountToken == g.AccountToken {
		s.grants[u.ID] = fresh
	}
	s.mu.Unlock()
	if !exists || current.AccountToken != g.AccountToken {
		return upstream.Grant{}, upstream.ErrDenied
	}
	return fresh, nil
}
func (s *Server) requireGrant(w http.ResponseWriter, r *http.Request, u store.User) (upstream.Grant, bool) {
	g, e := s.grant(r.Context(), u)
	if e != nil {
		status := 403
		code := "plex_required"
		message := "Connectez-vous avec Plex pour accéder au Hub."
		if errors.Is(e, upstream.ErrUnavailable) {
			status = 503
			code = "plex_unavailable"
			message = "La vérification Plex est momentanément indisponible."
		}
		problem(w, status, code, message)
		return g, false
	}
	return g, true
}
func offset(r *http.Request) (int, error) {
	v := r.URL.Query().Get("offset")
	if v == "" {
		return 0, nil
	}
	n, e := strconv.Atoi(v)
	if e != nil || n < 0 || n > 10000 {
		return 0, errors.New("invalid offset")
	}
	return n, nil
}

const seerrMissing = "Connectez-vous une première fois à Seerr avec le même compte Plex, puis réessayez. L’association est automatique."

func (s *Server) resolveSeerr(ctx context.Context, u store.User) (int64, error) {
	id, err := s.Upstream.SeerrUser(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, u.PlexID)
	if err != nil {
		return 0, err
	}
	if err := s.Store.SyncSeerr(ctx, u.ID, u.PlexID, id); err != nil {
		return 0, upstream.ErrDenied
	}
	return id, nil
}

func (s *Server) requests(w http.ResponseWriter, r *http.Request) {
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	if _, ok = s.requireGrant(w, r, u); !ok {
		return
	}
	if s.limited(w, r, "media", 60) {
		return
	}
	if s.Config.SeerrURL == "" {
		problem(w, 503, "not_configured", "Seerr n’est pas configuré.")
		return
	}
	off, e := offset(r)
	if e != nil {
		problem(w, 400, "invalid_offset", "Pagination invalide.")
		return
	}
	statuses := r.URL.Query()["status"]
	status := r.URL.Query().Get("status")
	if len(statuses) > 1 || (status != "" && status != "all" && !upstream.ValidRequestStatus(status)) {
		problem(w, 400, "invalid_status", "Filtre de statut invalide.")
		return
	}
	seerrID, e := s.resolveSeerr(r.Context(), u)
	if e != nil {
		if errors.Is(e, upstream.ErrSeerrUserNotFound) {
			problem(w, 409, "seerr_unlinked", seerrMissing)
		} else {
			problem(w, 502, "seerr_unavailable", "L’association automatique Seerr est indisponible. Réessayez plus tard.")
		}
		return
	}
	data, e := s.Upstream.RequestsByStatus(r.Context(), s.Config.SeerrURL, s.Config.SeerrAPIKey, seerrID, off, status)
	if e != nil {
		if errors.Is(e, upstream.ErrIncomplete) {
			problem(w, 502, "seerr_incomplete", "La liste complète des demandes est indisponible. Le filtre ne peut pas être appliqué ; réessayez ou affichez tous les statuts.")
		} else {
			problem(w, 502, "seerr_unavailable", "Les demandes ne sont pas disponibles actuellement.")
		}
		return
	}
	if s.revalidateUser(w, r, key, u) {
		jsonOut(w, 200, data)
	}
}
func (s *Server) recent(w http.ResponseWriter, r *http.Request) {
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	g, ok := s.requireGrant(w, r, u)
	if !ok {
		return
	}
	if s.limited(w, r, "media", 60) {
		return
	}
	off, e := offset(r)
	if e != nil {
		problem(w, 400, "invalid_offset", "Pagination invalide.")
		return
	}
	page, e := s.Upstream.RecentPage(r.Context(), s.Config.PlexURL, s.Config.PlexServerID, g, off, 12)
	if e != nil {
		problem(w, 502, "plex_unavailable", "Les nouveautés ne sont pas disponibles actuellement.")
		return
	}
	if !s.revalidateUser(w, r, key, u) {
		return
	}
	jsonOut(w, 200, map[string]any{"items": page.Items, "offset": off, "limit": 12, "nextOffset": page.NextOffset, "updatedAt": page.UpdatedAt})
}
func (s *Server) getPreferences(w http.ResponseWriter, r *http.Request) {
	_, u, ok := s.user(w, r)
	if ok {
		jsonOut(w, 200, map[string]any{"notifications": u.Notifications, "notificationsAvailable": s.departuresEnabled() || s.subscriptionsEnabled(), "timezone": "Europe/Paris"})
	}
}
func (s *Server) putPreferences(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	_, u, ok := s.user(w, r)
	if !ok {
		return
	}
	var p struct {
		Notifications *bool `json:"notifications"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || p.Notifications == nil {
		problem(w, 400, "invalid_body", "Préférences invalides.")
		return
	}
	if e := s.Store.Preferences(r.Context(), u.ID, *p.Notifications); e != nil {
		problem(w, 500, "storage_error", "Enregistrement impossible.")
		return
	}
	jsonOut(w, 200, map[string]any{"notifications": *p.Notifications, "notificationsAvailable": s.departuresEnabled() || s.subscriptionsEnabled(), "timezone": "Europe/Paris"})
}
func (s *Server) interaction(w http.ResponseWriter, r *http.Request) {
	expected := "Bearer " + s.Config.BotAPISecret
	if len(s.Config.BotAPISecret) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expected)) != 1 {
		problem(w, 401, "unauthorized", "Service non autorisé.")
		return
	}
	body, e := io.ReadAll(r.Body)
	if e != nil {
		problem(w, 400, "invalid_body", "Requête invalide.")
		return
	}
	i, e := discord.Verify(s.Config.DiscordPublicKey, r.Header.Get("X-Signature-Timestamp"), r.Header.Get("X-Signature-Ed25519"), body, time.Now())
	command, off, commandErr := discord.Command(i)
	if e != nil || commandErr != nil || i.ApplicationID != s.Config.DiscordClientID || i.GuildID != s.Config.DiscordGuildID || !config.Snowflake(i.Member.User.ID) || !config.Snowflake(i.ID) {
		problem(w, 403, "invalid_interaction", "Interaction refusée.")
		return
	}
	s.mu.Lock()
	_, duplicate := s.seen[i.ID]
	if !duplicate {
		s.seen[i.ID] = time.Now().Add(5 * time.Minute)
	}
	s.mu.Unlock()
	if duplicate {
		problem(w, 409, "duplicate", "Interaction déjà traitée.")
		return
	}
	options, _ := discord.CommandOptions(i)
	if command == "mes-suivis" {
		command, off = "mes-demandes", (options.Page-1)*10
	}
	text := "Commande inconnue. Utilisez /aide."
	publicText := ""
	var pagination *discord.RequestPagination
	var recentPagination *discord.RecentPagination
	if command == "aide" {
		text = discord.Help(s.Config.PublicOrigin, s.Config.Services)
	} else if command == "mes-demandes" || command == "a-voir" || command == "nouveautes" || command == "surprise" || command == "recap" || command == "suivre" || command == "livres" || command == "demander-livre" || command == "diagnostic" {
		u, e := s.Store.UserByDiscord(r.Context(), i.Member.User.ID)
		var g upstream.Grant
		if e == nil {
			g, e = s.grant(r.Context(), u)
		}
		if e != nil {
			text = "Connectez-vous au Hub avec Plex, puis liez Discord dans Mon compte : " + s.Config.PublicOrigin + "/compte"
		} else if s.rate("discord-personal:"+u.ID, 30) {
			text = "Patientez avant de relancer une commande personnelle."
		} else if command == "diagnostic" {
			text = s.diagnosticMessage(r.Context(), u, int64(options.RequestID))
		} else if command == "a-voir" {
			text = s.departuresMessage(r.Context(), u, g)
		} else if command == "livres" {
			text = s.booksMessage(r.Context(), u, options.Query, options.Page)
		} else if command == "demander-livre" {
			text = s.bookRequestMessage(options.Query)
		} else if command == "surprise" || command == "recap" {
			text, publicText = s.discoveryMessage(r.Context(), g, command, options.Kind, time.Now())
		} else if command == "suivre" {
			text = s.subscriptionMessage(r.Context(), u, options)
		} else if command == "mes-demandes" {
			text, pagination = s.requestMessage(r.Context(), u, off, options.Status)
		} else {
			p, e := s.Upstream.RecentPage(r.Context(), s.Config.PlexURL, s.Config.PlexServerID, g, off, 6)
			if e == nil {
				_, e = s.grant(r.Context(), u)
			}
			if e != nil {
				text = "Plex est momentanément indisponible."
			} else {
				text = recentMessage(p.Items, s.Config.PublicOrigin, true)
				publicText = recentMessage(p.Items, s.Config.PublicOrigin, false)
				recentPagination = &discord.RecentPagination{Offset: off, NextOffset: p.NextOffset}
			}
		}
		current, err := s.Store.UserByDiscord(r.Context(), i.Member.User.ID)
		if err == nil && current.ID == u.ID && current.PlexID == u.PlexID {
			_, err = s.grant(r.Context(), current)
		} else {
			err = upstream.ErrDenied
		}
		if err != nil {
			text = "Votre accès a expiré. Reconnectez-vous au Hub avec Plex : " + s.Config.PublicOrigin
			pagination, recentPagination = nil, nil
			publicText = ""
		}
	}
	reply := discord.Message(text)
	if options.Public && discord.PublicCommand(command) && publicText != "" {
		reply["publicContent"] = publicText
	}
	if pagination != nil {
		reply["pagination"] = pagination
	}
	if recentPagination != nil {
		reply["recentPagination"] = recentPagination
	}
	jsonOut(w, 200, reply)
}
func recentMessage(items []upstream.Media, origin string, links bool) string {
	text := "**Derniers ajouts Plex**\n"
	if len(items) == 0 {
		text += "Aucun ajout sur cette page.\n"
	}
	for _, item := range items {
		kind := "Film"
		if item.Type != "movie" {
			kind = "Série"
		}
		date := "date inconnue"
		if added, err := time.Parse(time.RFC3339, item.AddedAt); err == nil {
			date = added.UTC().Format("02/01/2006")
		}
		title := []rune(safeText(item.Title))
		if len(title) > 60 {
			title = append(title[:60], '…')
		}
		line := fmt.Sprintf("• [%s](%s) · %s · %s\n", string(title), item.URL, kind, date)
		if !links || item.URL == "" || len(line) > 300 || len(text)+len(line) > 1800 {
			line = fmt.Sprintf("• %s · %s · %s\n", string(title), kind, date)
		}
		text += line
	}
	footer := "\nOuvrir le Hub : " + origin
	if len(text)+len(footer) <= 2000 {
		text += footer
	}
	return text
}
func safeText(v string) string {
	v = strings.NewReplacer("@", "＠", "*", "", "_", "", "`", "", "[", "", "]", "", "\n", " ", "\r", " ").Replace(v)
	r := []rune(v)
	if len(r) > 100 {
		v = string(r[:100]) + "…"
	}
	return v
}
func statusLabel(v string) string {
	m := map[string]string{"pending": "En attente", "approved": "Approuvée", "declined": "Refusée", "error": "En erreur", "processing": "En cours", "partial": "Disponibilité partielle", "available": "Disponible", "unknown": "État inconnu"}
	if x := m[v]; x != "" {
		return x
	}
	return "État inconnu"
}
