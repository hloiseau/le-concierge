package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/discord"
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
type pendingPin struct {
	Pin      upstream.Pin
	ClientID string
	Expires  time.Time
	LastPoll time.Time
}
type bucket struct {
	Since time.Time
	Count int
}
type Server struct {
	Config   config.Config
	Store    *store.Store
	Upstream *upstream.Client
	mu       sync.Mutex
	sessions map[string]session
	oauth    map[string]time.Time
	pins     map[string]pendingPin
	grants   map[string]upstream.Grant
	limits   map[string]bucket
	seen     map[string]time.Time
}

func New(c config.Config, db *store.Store) *Server {
	return &Server{Config: c, Store: db, Upstream: upstream.New(), sessions: map[string]session{}, oauth: map[string]time.Time{}, pins: map[string]pendingPin{}, grants: map[string]upstream.Grant{}, limits: map[string]bucket{}, seen: map[string]time.Time{}}
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
		problem(w, 401, "unauthenticated", "Connectez-vous avec Discord.")
		return "", store.User{}, false
	}
	u, e := s.Store.User(r.Context(), v.UserID)
	if e != nil {
		problem(w, 401, "unauthenticated", "Reconnectez-vous avec Discord.")
		return "", u, false
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
		if now.After(v) {
			delete(s.oauth, k)
		}
	}
	for k, v := range s.pins {
		if now.After(v.Expires) {
			delete(s.pins, k)
		}
	}
	for k, v := range s.grants {
		if now.After(v.ExpiresAt) {
			delete(s.grants, k)
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
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		start := time.Now()
		s.cleanup()
		m.ServeHTTP(w, r)
		slog.Info("request", "request_id", id, "method", r.Method, "route", r.Pattern, "duration_ms", time.Since(start).Milliseconds())
	})
}
func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	jsonOut(w, 200, map[string]any{"version": "0.1.0", "discordLogin": s.Config.OAuthEnabled(), "plexLink": s.Config.PlexURL != "", "requests": s.Config.SeerrURL != "", "recent": s.Config.PlexURL != "", "subscriptions": false, "notifications": false, "books": false, "monitoring": false})
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
	s.mu.Lock()
	g, has := s.grants[u.DiscordID]
	s.mu.Unlock()
	connected := has && g.PlexID == u.PlexID && time.Now().Before(g.ExpiresAt)
	// Only advertise a Seerr association confirmed by the upstream directory.
	u.SeerrID = 0
	if connected && s.Config.SeerrURL != "" && !s.rate("seerr-session:"+u.DiscordID, 30) {
		if _, err := s.grant(r.Context(), u); err == nil {
			u.SeerrID, _ = s.resolveSeerr(r.Context(), u)
		}
	}
	jsonOut(w, 200, map[string]any{"user": u, "admin": s.Config.Admin(u.DiscordID), "plexConnected": connected, "expiresAt": v.Expires.UTC().Format(time.RFC3339)})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
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
	s.oauth[digest(state)] = time.Now().Add(5 * time.Minute)
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
	expiry, ok := s.oauth[digest(state)]
	delete(s.oauth, digest(state))
	s.mu.Unlock()
	s.cookie(w, s.oauthCookieName(), "", -1, true)
	if !ok || time.Now().After(expiry) || r.URL.Query().Get("code") == "" {
		problem(w, 403, "invalid_state", "Relancez la connexion.")
		return
	}
	identity, e := s.Upstream.DiscordLogin(r.Context(), s.Config.DiscordClientID, s.Config.DiscordClientSecret, r.URL.Query().Get("code"), s.Config.PublicOrigin+"/api/v1/auth/discord/callback", s.Config.DiscordGuildID)
	if e != nil || !config.Snowflake(identity.ID) {
		problem(w, 403, "discord_denied", "Connexion refusée. Vérifiez votre accès au serveur Discord.")
		return
	}
	name := identity.GlobalName
	if name == "" {
		name = identity.Username
	}
	if _, e = s.Store.Upsert(r.Context(), identity.ID, name); e != nil {
		problem(w, 500, "storage_error", "Connexion impossible.")
		return
	}
	value := token()
	s.mu.Lock()
	if len(s.sessions) >= 1000 {
		s.mu.Unlock()
		problem(w, 503, "busy", "Réessayez plus tard.")
		return
	}
	if old, e := r.Cookie(s.cookieName()); e == nil {
		delete(s.sessions, digest(old.Value))
	}
	s.sessions[digest(value)] = session{identity.ID, time.Now().Add(12 * time.Hour)}
	s.mu.Unlock()
	s.cookie(w, s.cookieName(), value, 12*3600, false)
	http.Redirect(w, r, "/compte", http.StatusSeeOther)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	key, v, ok := s.session(r)
	s.mu.Lock()
	delete(s.sessions, key)
	delete(s.pins, key)
	if ok {
		delete(s.grants, v.UserID)
	}
	s.mu.Unlock()
	s.cookie(w, s.cookieName(), "", -1, false)
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
	if e := s.Store.Delete(r.Context(), u.DiscordID); e != nil {
		problem(w, 500, "storage_error", "Suppression impossible.")
		return
	}
	s.mu.Lock()
	for k, v := range s.sessions {
		if v.UserID == u.DiscordID {
			delete(s.sessions, k)
			delete(s.pins, k)
		}
	}
	delete(s.grants, u.DiscordID)
	s.mu.Unlock()
	s.cookie(w, s.cookieName(), "", -1, false)
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (s *Server) plexStart(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	key, _, ok := s.user(w, r)
	if !ok {
		return
	}
	if s.Config.PlexURL == "" {
		problem(w, 503, "not_configured", "Plex n’est pas configuré.")
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
	s.mu.Lock()
	s.pins[key] = pendingPin{Pin: p, ClientID: client, Expires: time.Now().Add(time.Duration(ttl) * time.Second)}
	s.mu.Unlock()
	q := url.Values{"clientID": {client}, "code": {p.Code}, "context[device][product]": {"Le Concierge"}}
	jsonOut(w, 200, map[string]any{"url": "https://app.plex.tv/auth#?" + q.Encode(), "expiresIn": ttl})
}
func (s *Server) plexPoll(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	p, exists := s.pins[key]
	if !exists || time.Now().After(p.Expires) {
		delete(s.pins, key)
		s.mu.Unlock()
		problem(w, 410, "pin_expired", "Relancez la liaison Plex.")
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
	// Consume a successful PIN before verification so concurrent polls cannot bind it twice.
	s.mu.Lock()
	current, exists := s.pins[key]
	if exists && current.Pin.ID == p.Pin.ID {
		delete(s.pins, key)
	}
	s.mu.Unlock()
	if !exists || current.Pin.ID != p.Pin.ID {
		problem(w, 409, "pin_consumed", "Cette liaison a déjà été traitée.")
		return
	}
	g, e := s.Upstream.PlexVerify(r.Context(), account, p.ClientID, s.Config.PlexServerID)
	if e != nil {
		problem(w, 403, "plex_denied", "Ce compte Plex n’a pas accès au serveur, ou Plex ne peut pas le confirmer.")
		return
	}
	if e = s.Store.LinkPlex(r.Context(), u.DiscordID, g.PlexID); e != nil {
		problem(w, 409, "identity_conflict", "Ce compte est déjà lié, ou une autre identité Plex est associée. Contactez l’administrateur.")
		return
	}
	s.mu.Lock()
	sess, valid := s.sessions[key]
	if !valid || !time.Now().Before(sess.Expires) {
		s.mu.Unlock()
		problem(w, 401, "unauthenticated", "Reconnectez-vous.")
		return
	}
	g.ExpiresAt = sess.Expires
	s.grants[u.DiscordID] = g
	s.mu.Unlock()
	jsonOut(w, 200, map[string]bool{"connected": true})
}
func (s *Server) grant(ctx context.Context, u store.User) (upstream.Grant, error) {
	s.mu.Lock()
	g, ok := s.grants[u.DiscordID]
	s.mu.Unlock()
	if !ok || g.PlexID != u.PlexID || time.Now().After(g.ExpiresAt) {
		return upstream.Grant{}, upstream.ErrDenied
	}
	if time.Since(g.VerifiedAt) < 5*time.Minute {
		return g, nil
	}
	fresh, e := s.Upstream.PlexVerify(ctx, g.AccountToken, g.ClientID, s.Config.PlexServerID)
	if e != nil {
		if errors.Is(e, upstream.ErrDenied) {
			s.mu.Lock()
			delete(s.grants, u.DiscordID)
			s.mu.Unlock()
		}
		return upstream.Grant{}, e
	}
	if fresh.PlexID != u.PlexID {
		return upstream.Grant{}, upstream.ErrDenied
	}
	fresh.ExpiresAt = g.ExpiresAt
	s.mu.Lock()
	current, exists := s.grants[u.DiscordID]
	if exists && current.AccountToken == g.AccountToken {
		s.grants[u.DiscordID] = fresh
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
		message := "Liez ou reconnectez votre compte Plex depuis Mon compte."
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
	if err := s.Store.SyncSeerr(ctx, u.DiscordID, u.PlexID, id); err != nil {
		return 0, upstream.ErrDenied
	}
	return id, nil
}

func (s *Server) requests(w http.ResponseWriter, r *http.Request) {
	_, u, ok := s.user(w, r)
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
	seerrID, e := s.resolveSeerr(r.Context(), u)
	if e != nil {
		if errors.Is(e, upstream.ErrSeerrUserNotFound) {
			problem(w, 409, "seerr_unlinked", seerrMissing)
		} else {
			problem(w, 502, "seerr_unavailable", "L’association automatique Seerr est indisponible. Réessayez plus tard.")
		}
		return
	}
	data, e := s.Upstream.Requests(r.Context(), s.Config.SeerrURL, s.Config.SeerrAPIKey, seerrID, off)
	if e != nil {
		problem(w, 502, "seerr_unavailable", "Les demandes ne sont pas disponibles actuellement.")
		return
	}
	jsonOut(w, 200, data)
}
func (s *Server) recent(w http.ResponseWriter, r *http.Request) {
	_, u, ok := s.user(w, r)
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
	items, e := s.Upstream.Recent(r.Context(), s.Config.PlexURL, s.Config.PlexServerID, g, off)
	if e != nil {
		problem(w, 502, "plex_unavailable", "Les nouveautés ne sont pas disponibles actuellement.")
		return
	}
	jsonOut(w, 200, map[string]any{"items": items, "offset": off, "limit": 12, "updatedAt": time.Now().UTC().Format(time.RFC3339)})
}
func (s *Server) getPreferences(w http.ResponseWriter, r *http.Request) {
	_, u, ok := s.user(w, r)
	if ok {
		jsonOut(w, 200, map[string]any{"notifications": u.Notifications, "notificationsAvailable": false, "timezone": "Europe/Paris"})
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
	if e := s.Store.Preferences(r.Context(), u.DiscordID, *p.Notifications); e != nil {
		problem(w, 500, "storage_error", "Enregistrement impossible.")
		return
	}
	jsonOut(w, 200, map[string]any{"notifications": *p.Notifications, "notificationsAvailable": false, "timezone": "Europe/Paris"})
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
	if e != nil || i.Type != 2 || i.ApplicationID != s.Config.DiscordClientID || i.GuildID != s.Config.DiscordGuildID || !config.Snowflake(i.Member.User.ID) {
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
	text := "Commande inconnue. Utilisez /aide."
	if i.Data.Name == "aide" {
		text = discord.Help(s.Config.PublicOrigin)
	} else if i.Data.Name == "mes-demandes" || i.Data.Name == "nouveautes" {
		u, e := s.Store.User(r.Context(), i.Member.User.ID)
		var g upstream.Grant
		if e == nil {
			g, e = s.grant(r.Context(), u)
		}
		if e != nil {
			text = "Connectez-vous au Hub et liez votre compte Plex : " + s.Config.PublicOrigin + "/compte"
		} else if i.Data.Name == "mes-demandes" {
			if s.Config.SeerrURL == "" {
				text = "Seerr n’est pas encore configuré."
			} else {
				seerrID, e := s.resolveSeerr(r.Context(), u)
				var p upstream.RequestPage
				if e == nil {
					p, e = s.Upstream.Requests(r.Context(), s.Config.SeerrURL, s.Config.SeerrAPIKey, seerrID, 0)
				}
				if errors.Is(e, upstream.ErrSeerrUserNotFound) {
					text = seerrMissing
				} else if e != nil {
					text = "Seerr est momentanément indisponible."
				} else {
					text = "**Vos demandes**\n"
					if len(p.Items) == 0 {
						text += "Aucune demande pour le moment."
					}
					for _, v := range p.Items {
						text += "• " + safeText(v.Title) + " — " + statusLabel(v.Status) + "\n"
					}
					text += "\nVoir toutes vos demandes : " + s.Config.PublicOrigin + "/demandes"
				}
			}
		} else {
			p, e := s.Upstream.Recent(r.Context(), s.Config.PlexURL, s.Config.PlexServerID, g, 0)
			if e != nil {
				text = "Plex est momentanément indisponible."
			} else {
				text = "**Derniers ajouts Plex**\n"
				if len(p) == 0 {
					text += "Aucun ajout récent."
				}
				for n, v := range p {
					if n >= 8 {
						break
					}
					text += "• " + safeText(v.Title) + "\n"
				}
				text += "\nOuvrir le Hub : " + s.Config.PublicOrigin
			}
		}
	}
	jsonOut(w, 200, discord.Message(text))
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
	m := map[string]string{"pending": "En attente", "approved": "Approuvée", "declined": "Refusée", "processing": "En cours", "partial": "Partiellement disponible", "available": "Disponible", "unknown": "État inconnu"}
	if x := m[v]; x != "" {
		return x
	}
	return "État inconnu"
}
