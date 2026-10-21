package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) plexPoster(w http.ResponseWriter, r *http.Request) {
	key, user, ok := s.user(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "Cette affiche utilise vos droits Plex.")
		return
	}
	g, ok := s.requireGrant(w, r, user)
	if !ok {
		return
	}
	if s.limited(w, r, "posters", 120) {
		return
	}
	body, mime, err := s.Upstream.PlexPoster(r.Context(), s.Config.PlexURL, s.Config.PlexServerID, r.PathValue("id"), g)
	if err != nil {
		status := 503
		if errors.Is(err, upstream.ErrDenied) {
			status = 403
		}
		if errors.Is(err, upstream.ErrNotFound) {
			status = 404
		}
		problem(w, status, "poster_unavailable", "Cette affiche n’est pas disponible.")
		return
	}
	if !s.revalidateUser(w, r, key, user) {
		return
	}
	s.mu.Lock()
	current, exists := s.grants[user.ID]
	valid := exists && current.AccountToken == g.AccountToken && current.ServerToken == g.ServerToken && current.Active(time.Now())
	s.mu.Unlock()
	if !valid {
		problem(w, 403, "plex_required", "Votre connexion Plex a changé.")
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}
