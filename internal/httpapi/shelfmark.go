package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/hloiseau/le-concierge/internal/upstream"
)

type shelfmarkGrant struct {
	Session             upstream.ShelfmarkSession
	ExpiresAt           time.Time
	Revision, PlexToken string
	CatalogRevision     string
}

func (s *Server) clearShelfmarkGrantLocked(userID string) { delete(s.shelfmarkGrants, userID) }
func (s *Server) currentShelfmarkGrant(userID string) (shelfmarkGrant, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.shelfmarkGrants[userID]
	plex, plexOK := s.grants[userID]
	if !ok || !plexOK || plex.AccountToken != g.PlexToken || !time.Now().Before(g.ExpiresAt) || !plex.Active(time.Now()) {
		s.clearShelfmarkGrantLocked(userID)
		return shelfmarkGrant{}, false
	}
	return g, g.Session.UserID > 0
}
func (s *Server) forgetShelfmarkGrant(userID string, g shelfmarkGrant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.shelfmarkGrants[userID]; ok && current.Revision == g.Revision {
		s.clearShelfmarkGrantLocked(userID)
	}
}
func shelfmarkFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, upstream.ErrDenied) {
		problem(w, 403, "shelfmark_link_required", "Reliez votre compte personnel Shelfmark. Seule sa connexion locale est prise en charge ; un accès expiré doit être renouvelé.")
		return
	}
	problem(w, 503, "shelfmark_unavailable", "Shelfmark est indisponible ou son contrat a changé. Réessayez plus tard.")
}
func (s *Server) shelfmarkLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && !s.origin(w, r) {
		return
	}
	key, user, ok := s.user(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "Cette association utilise votre compte connecté.")
		return
	}
	if r.Method == http.MethodDelete {
		s.mu.Lock()
		s.clearShelfmarkGrantLocked(user.ID)
		s.mu.Unlock()
		jsonOut(w, 200, bookLinkStatus{})
		return
	}
	if r.Method == http.MethodGet {
		g, linked := s.currentShelfmarkGrant(user.ID)
		status := bookLinkStatus{Linked: linked}
		if linked {
			at := g.ExpiresAt.UTC()
			status.ExpiresAt = &at
		}
		jsonOut(w, 200, status)
		return
	}
	if s.Config.ShelfmarkURL == "" {
		problem(w, 503, "not_configured", "Le suivi Shelfmark n’est pas configuré.")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	d := json.NewDecoder(io.LimitReader(r.Body, 4097))
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || !upstream.ValidBookCredentials(upstream.BookCredentials{Username: input.Username, Password: input.Password}) {
		problem(w, 400, "invalid_body", "Indiquez vos identifiants personnels Shelfmark.")
		return
	}
	if s.limited(w, r, "shelfmark-link", 5) {
		return
	}
	g, err := s.grant(r.Context(), user)
	if err != nil {
		shelfmarkFailure(w, upstream.ErrDenied)
		return
	}
	pending := shelfmarkGrant{ExpiresAt: g.ExpiresAt.UTC(), Revision: token(), PlexToken: g.AccountToken}
	s.mu.Lock()
	if s.shelfmarkGrants == nil {
		s.shelfmarkGrants = map[string]shelfmarkGrant{}
	}
	s.shelfmarkGrants[user.ID] = pending
	s.mu.Unlock()
	session, err := s.Upstream.LinkShelfmark(r.Context(), s.Config.ShelfmarkURL, input.Username, input.Password)
	input.Password = ""
	if err != nil {
		s.forgetShelfmarkGrant(user.ID, pending)
		shelfmarkFailure(w, err)
		return
	}
	if !s.revalidateUser(w, r, key, user) {
		s.forgetShelfmarkGrant(user.ID, pending)
		return
	}
	s.mu.Lock()
	current, currentOK := s.shelfmarkGrants[user.ID]
	plex, plexOK := s.grants[user.ID]
	sess, sessionOK := s.sessions[key]
	now := time.Now()
	if !currentOK || current.Revision != pending.Revision || !plexOK || plex.AccountToken != pending.PlexToken || !plex.Active(now) || !sessionOK || sess.UserID != user.ID || !now.Before(sess.Expires) {
		s.mu.Unlock()
		problem(w, 409, "link_changed", "L’association a changé. Réessayez depuis votre compte.")
		return
	}
	pending.Session = session
	s.shelfmarkGrants[user.ID] = pending
	s.mu.Unlock()
	jsonOut(w, 200, bookLinkStatus{Linked: true, ExpiresAt: &pending.ExpiresAt})
}
func (s *Server) shelfmarkRequests(w http.ResponseWriter, r *http.Request) {
	key, user, ok := s.user(w, r)
	if !ok {
		return
	}
	page := 1
	q := r.URL.Query()
	for name, values := range q {
		if name != "page" || len(values) != 1 {
			problem(w, 400, "invalid_query", "Indiquez une page de 1 à 1001.")
			return
		}
	}
	if value, exists := q["page"]; exists {
		var err error
		page, err = strconv.Atoi(value[0])
		if err != nil || page < 1 || page > 1001 {
			problem(w, 400, "invalid_query", "Indiquez une page de 1 à 1001.")
			return
		}
	}
	if s.Config.ShelfmarkURL == "" {
		problem(w, 503, "not_configured", "Le suivi Shelfmark n’est pas configuré.")
		return
	}
	g, ok := s.currentShelfmarkGrant(user.ID)
	if !ok {
		shelfmarkFailure(w, upstream.ErrDenied)
		return
	}
	if s.limited(w, r, "shelfmark-requests", 30) {
		return
	}
	result, err := s.Upstream.ShelfmarkRequests(r.Context(), s.Config.ShelfmarkURL, g.Session, page)
	if err != nil {
		if errors.Is(err, upstream.ErrDenied) {
			s.forgetShelfmarkGrant(user.ID, g)
		}
		shelfmarkFailure(w, err)
		return
	}
	if !s.revalidateUser(w, r, key, user) {
		return
	}
	books, bookLinked := s.currentBookGrant(user.ID)
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.shelfmarkGrants[user.ID]
	plex, plexOK := s.grants[user.ID]
	session, sessionOK := s.sessions[key]
	now := time.Now()
	owner, ownerErr := s.Store.User(r.Context(), user.ID)
	if !ok || current.Revision != g.Revision || !plexOK || plex.AccountToken != g.PlexToken || !now.Before(current.ExpiresAt) || !now.Before(plex.ExpiresAt) || !sessionOK || session.UserID != user.ID || !now.Before(session.Expires) || ownerErr != nil || owner.PlexID != user.PlexID {
		shelfmarkFailure(w, upstream.ErrDenied)
		return
	}
	tracked, err := s.trackedBookRequests(r.Context(), user, g, books, bookLinked, result)
	if err != nil {
		problem(w, 503, "storage_unavailable", "Les correspondances de livres sont momentanément indisponibles.")
		return
	}
	jsonOut(w, 200, tracked)
}
