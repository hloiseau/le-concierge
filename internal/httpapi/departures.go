package httpapi

import (
	"net/http"
)

func (s *Server) departures(w http.ResponseWriter, r *http.Request) {
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	if !s.departuresEnabled() {
		problem(w, 503, "not_configured", "Les sources des départs ne sont pas encore configurées.")
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "Cette liste utilise uniquement votre compte connecté.")
		return
	}
	if s.rate("departures:"+u.ID, 6) {
		problem(w, 429, "rate_limited", "Patientez avant d’actualiser les départs.")
		return
	}
	g, ok := s.requireGrant(w, r, u)
	if !ok {
		return
	}
	page, err := s.Departures.List(r.Context(), s.Config, g)
	if err != nil {
		problem(w, 503, "departures_unavailable", "Les départs ne peuvent pas être vérifiés actuellement. Aucune ancienne liste n’est utilisée.")
		return
	}
	// Logout, unlink and account deletion invalidate an in-flight personal result.
	currentKey, current, ok := s.session(r)
	if !ok || currentKey != key || current.UserID != u.ID {
		problem(w, 401, "unauthenticated", "Reconnectez-vous avec Plex.")
		return
	}
	user, err := s.Store.User(r.Context(), u.ID)
	if err != nil || user.PlexID != u.PlexID {
		problem(w, 403, "plex_required", "Reconnectez-vous avec Plex.")
		return
	}
	if _, ok = s.requireGrant(w, r, user); !ok {
		return
	}
	jsonOut(w, 200, page)
}
func (s *Server) departuresEnabled() bool {
	return s.Config.MaintainerrURL != "" && s.Config.TautulliURL != "" && s.Config.PlexURL != ""
}
