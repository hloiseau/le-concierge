package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) bookCollections(w http.ResponseWriter, r *http.Request) {
	key, user, ok := s.user(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	for key, values := range query {
		if len(values) != 1 || (key != "series" && key != "page") {
			problem(w, 400, "invalid_query", "Choisissez une série et une page de 1 à 1001.")
			return
		}
	}
	series, page := query.Get("series"), 1
	var err error
	if raw, exists := query["page"]; exists {
		page, err = strconv.Atoi(raw[0])
	}
	if err != nil || page < 1 || page > 1001 || !upstream.ValidBookQuery(series) || strings.TrimSpace(series) != series {
		problem(w, 400, "invalid_query", "Nom de série de 200 caractères maximum et page de 1 à 1001.")
		return
	}
	if s.Config.GrimmoryURL == "" {
		problem(w, 503, "not_configured", "Le catalogue Grimmory n’est pas configuré.")
		return
	}
	g, ok := s.currentBookGrant(user.ID)
	if !ok {
		s.bookFailure(w, upstream.ErrDenied)
		return
	}
	if s.limited(w, r, "books-catalog", 60) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	data, err := s.Upstream.GrimmoryCollections(ctx, s.Config.GrimmoryURL, s.bookServiceURL("grimmory"), g.Credentials, series, page)
	if err != nil {
		if errors.Is(err, upstream.ErrDenied) {
			s.forgetBookGrant(user.ID, g)
		}
		s.bookFailure(w, err)
		return
	}
	if !s.revalidateUser(w, r, key, user) {
		return
	}
	current, ok := s.currentBookGrant(user.ID)
	if !ok || current.Revision != g.Revision {
		s.bookFailure(w, upstream.ErrDenied)
		return
	}
	jsonOut(w, 200, data)
}
