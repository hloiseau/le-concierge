package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) downloadsLibrary(w http.ResponseWriter, r *http.Request) {
	key, user, ok := s.user(w, r)
	if !ok {
		return
	}
	if !s.downloadarrEnabled() {
		problem(w, 503, "not_configured", "Les téléchargements ne sont pas configurés.")
		return
	}
	q, parseError := url.ParseQuery(r.URL.RawQuery)
	if parseError != nil {
		problem(w, 400, "invalid_query", "La recherche est invalide.")
		return
	}
	for name, values := range q {
		if (name != "q" && name != "parent" && name != "offset") || len(values) != 1 || values[0] == "" {
			problem(w, 400, "invalid_query", "Recherchez un titre ou ouvrez une série ou une saison.")
			return
		}
	}
	offset := 0
	if value := q.Get("offset"); value != "" {
		var err error
		offset, err = strconv.Atoi(value)
		if err != nil || strconv.Itoa(offset) != value {
			offset = -1
		}
	}
	if !upstream.ValidDownloadLibraryQuery(q.Get("q"), q.Get("parent"), offset) {
		problem(w, 400, "invalid_query", "Indiquez un titre de 2 à 200 caractères ou ouvrez une série ou une saison.")
		return
	}
	if s.rate("download-library:"+user.ID, 60) {
		problem(w, 429, "rate_limited", "Patientez avant une nouvelle recherche.")
		return
	}
	grant, ok := s.requireGrant(w, r, user)
	if !ok {
		return
	}
	page, err := s.Upstream.DownloadLibrary(r.Context(), s.Config.PlexURL, s.Config.PlexServerID, grant, q.Get("q"), q.Get("parent"), offset)
	if err != nil {
		status, code, message := 503, "plex_unavailable", "La bibliothèque Plex est momentanément indisponible. Réessayez."
		if errors.Is(err, upstream.ErrDenied) {
			status, code, message = 403, "plex_required", "Votre accès Plex doit être vérifié. Reconnectez-vous."
		}
		if errors.Is(err, upstream.ErrNotFound) {
			status, code, message = 404, "content_not_found", "Ce contenu n’est plus accessible dans votre bibliothèque."
		}
		problem(w, status, code, message)
		return
	}
	if !s.revalidateUser(w, r, key, user) {
		return
	}
	s.mu.Lock()
	current, exists := s.grants[user.ID]
	sess, sessionExists := s.sessions[key]
	valid := exists && sessionExists && sess.UserID == user.ID && current.PlexID == grant.PlexID && current.AccountToken == grant.AccountToken && current.ServerToken == grant.ServerToken && current.ClientID == grant.ClientID
	if current.ExpiresAt.Before(page.ExpiresAt) {
		page.ExpiresAt = current.ExpiresAt.UTC()
	}
	if sess.Expires.Before(page.ExpiresAt) {
		page.ExpiresAt = sess.Expires.UTC()
	}
	s.mu.Unlock()
	if !valid || !time.Now().Before(page.ExpiresAt) {
		problem(w, 403, "plex_required", "Votre connexion Plex a changé. Relancez la recherche.")
		return
	}
	jsonOut(w, 200, page)
}
