package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

type bookGrant struct {
	Credentials         upstream.BookCredentials
	ExpiresAt           time.Time
	Revision, PlexToken string
}
type bookLinkStatus struct {
	Linked    bool       `json:"linked"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func (s *Server) clearBookGrantLocked(userID string) { delete(s.bookGrants, userID) }
func (s *Server) bookServiceURL(id string) string {
	for _, service := range s.Config.Services {
		if service.ID == id {
			return service.URL
		}
	}
	return ""
}
func (s *Server) currentBookGrant(userID string) (bookGrant, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.bookGrants[userID]
	plex, plexOK := s.grants[userID]
	if !ok || !plexOK || plex.AccountToken != g.PlexToken || !time.Now().Before(g.ExpiresAt) || !plex.Active(time.Now()) || g.Credentials.Username == "" {
		if !ok || !plexOK || plex.AccountToken != g.PlexToken || !time.Now().Before(g.ExpiresAt) {
			s.clearBookGrantLocked(userID)
		}
		return bookGrant{}, false
	}
	return g, true
}
func (s *Server) booksLink(w http.ResponseWriter, r *http.Request) {
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
		s.clearBookGrantLocked(user.ID)
		s.mu.Unlock()
		jsonOut(w, 200, bookLinkStatus{})
		return
	}
	if r.Method == http.MethodGet {
		g, linked := s.currentBookGrant(user.ID)
		status := bookLinkStatus{Linked: linked}
		if linked {
			expires := g.ExpiresAt.UTC()
			status.ExpiresAt = &expires
		}
		jsonOut(w, 200, status)
		return
	}
	if s.Config.GrimmoryURL == "" {
		problem(w, 503, "not_configured", "Le catalogue Grimmory n’est pas configuré.")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	d := json.NewDecoder(io.LimitReader(r.Body, 4097))
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || !upstream.ValidBookCredentials(upstream.BookCredentials{Username: input.Username, Password: input.Password}) {
		problem(w, 400, "invalid_body", "Indiquez vos identifiants OPDS personnels Grimmory.")
		return
	}
	if s.limited(w, r, "books-link", 10) {
		return
	}
	g, err := s.grant(r.Context(), user)
	if err != nil {
		problem(w, 403, "plex_required", "Reconnectez-vous avec Plex.")
		return
	}
	pending := bookGrant{ExpiresAt: g.ExpiresAt.UTC(), Revision: token(), PlexToken: g.AccountToken}
	s.mu.Lock()
	if s.bookGrants == nil {
		s.bookGrants = map[string]bookGrant{}
	}
	s.bookGrants[user.ID] = pending
	s.mu.Unlock()
	credentials := upstream.BookCredentials{Username: input.Username, Password: input.Password}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	_, err = s.Upstream.GrimmoryBooks(ctx, s.Config.GrimmoryURL, "", credentials, "", 1)
	if err != nil {
		s.forgetBookGrant(user.ID, pending)
		s.bookFailure(w, err)
		return
	}
	if !s.revalidateUser(w, r, key, user) {
		s.forgetBookGrant(user.ID, pending)
		return
	}
	s.mu.Lock()
	current, currentOK := s.bookGrants[user.ID]
	plex, plexOK := s.grants[user.ID]
	sess, sessionOK := s.sessions[key]
	if !currentOK || current.Revision != pending.Revision || !plexOK || plex.AccountToken != pending.PlexToken || !plex.Active(time.Now()) || !sessionOK || sess.UserID != user.ID || !time.Now().Before(sess.Expires) {
		s.mu.Unlock()
		problem(w, 409, "link_changed", "L’association a changé. Réessayez depuis votre compte.")
		return
	}
	pending.Credentials = credentials
	s.bookGrants[user.ID] = pending
	s.mu.Unlock()
	jsonOut(w, 200, bookLinkStatus{Linked: true, ExpiresAt: &pending.ExpiresAt})
}
func (s *Server) forgetBookGrant(userID string, grant bookGrant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.bookGrants[userID]; ok && current.Revision == grant.Revision {
		s.clearBookGrantLocked(userID)
	}
}
func (s *Server) bookFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, upstream.ErrDenied) {
		problem(w, 403, "books_link_required", "Reliez vos identifiants OPDS personnels Grimmory. L’accès a peut-être été révoqué.")
		return
	}
	if errors.Is(err, upstream.ErrNotFound) {
		problem(w, 404, "book_not_found", "Ce livre ou cet accès OPDS n’est plus disponible.")
		return
	}
	problem(w, 503, "books_unavailable", "Grimmory est momentanément indisponible ou son contrat OPDS a changé.")
}
func bookQuery(r *http.Request, requestLink bool) (string, int, bool) {
	query := r.URL.Query()
	page := 1
	for key, values := range query {
		if len(values) != 1 || (key != "q" && (requestLink || key != "page")) {
			return "", 0, false
		}
	}
	if raw, ok := query["page"]; ok {
		var err error
		page, err = strconv.Atoi(raw[0])
		if err != nil || page < 1 || page > 1001 {
			return "", 0, false
		}
	}
	q := strings.TrimSpace(query.Get("q"))
	return q, page, upstream.ValidBookQuery(query.Get("q")) && (!requestLink || q != "")
}
func (s *Server) booksCatalog(w http.ResponseWriter, r *http.Request) {
	s.serveBooks(w, r, false)
}
func (s *Server) recentBooks(w http.ResponseWriter, r *http.Request) {
	s.serveBooks(w, r, true)
}
func (s *Server) serveBooks(w http.ResponseWriter, r *http.Request, recent bool) {
	key, user, ok := s.user(w, r)
	if !ok {
		return
	}
	query, page, valid := bookQuery(r, false)
	if !valid || (recent && r.URL.Query().Has("q")) {
		problem(w, 400, "invalid_query", "Recherche de 200 caractères maximum et page de 1 à 1001.")
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
	var pageData upstream.BookPage
	var err error
	if recent {
		pageData, err = s.Upstream.GrimmoryRecentBooks(ctx, s.Config.GrimmoryURL, s.bookServiceURL("grimmory"), g.Credentials, page)
	} else {
		pageData, err = s.Upstream.GrimmoryBooks(ctx, s.Config.GrimmoryURL, s.bookServiceURL("grimmory"), g.Credentials, query, page)
	}
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
	if recent {
		now := time.Now().UTC()
		expires := now.Add(time.Minute)
		if g.ExpiresAt.Before(expires) {
			expires = g.ExpiresAt
		}
		jsonOut(w, 200, struct {
			upstream.BookPage
			UpdatedAt time.Time `json:"updatedAt"`
			ExpiresAt time.Time `json:"expiresAt"`
		}{pageData, now, expires})
		return
	}
	jsonOut(w, 200, pageData)
}
func (s *Server) booksCover(w http.ResponseWriter, r *http.Request) {
	key, user, ok := s.user(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 || !upstream.ValidBookID(r.PathValue("id")) {
		problem(w, 400, "invalid_query", "Identifiant de livre invalide.")
		return
	}
	g, ok := s.currentBookGrant(user.ID)
	if !ok {
		s.bookFailure(w, upstream.ErrDenied)
		return
	}
	if s.limited(w, r, "books-cover", 120) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	body, mime, err := s.Upstream.GrimmoryCover(ctx, s.Config.GrimmoryURL, g.Credentials, r.PathValue("id"))
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
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}

const externalBookRequestMessage = "Choisissez l’édition puis confirmez votre demande dans Shelfmark, avec votre compte et ses règles d’approbation. Le Hub ne crée aucune demande. Si le suivi Shelfmark est activé, retrouvez vos demandes dans Mes demandes de livres après avoir lié votre compte personnel."

func (s *Server) bookRequestLink(w http.ResponseWriter, r *http.Request) {
	_, _, ok := s.user(w, r)
	if !ok {
		return
	}
	query, _, valid := bookQuery(r, true)
	if !valid {
		problem(w, 400, "invalid_query", "Indiquez un titre, un auteur ou un ISBN, de 200 caractères maximum.")
		return
	}
	target, err := upstream.ShelfmarkSearchLink(s.bookServiceURL("shelfmark"), query)
	if err != nil {
		problem(w, 503, "not_configured", "Le lien Shelfmark n’est pas configuré.")
		return
	}
	jsonOut(w, 200, map[string]string{"url": target, "mode": "external", "message": externalBookRequestMessage})
}
func (s *Server) booksMessage(ctx context.Context, user store.User, query string, page int) string {
	if s.Config.GrimmoryURL == "" {
		return "Le catalogue Grimmory n’est pas configuré dans Le Hub."
	}
	g, ok := s.currentBookGrant(user.ID)
	if !ok {
		return "Reliez vos identifiants OPDS personnels dans la page Livres du Hub pour consulter votre catalogue."
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	data, err := s.Upstream.GrimmoryBooks(ctx, s.Config.GrimmoryURL, s.bookServiceURL("grimmory"), g.Credentials, query, page)
	if err != nil {
		if errors.Is(err, upstream.ErrDenied) {
			s.forgetBookGrant(user.ID, g)
			return "Votre accès Grimmory a changé. Reliez vos identifiants OPDS dans Le Hub."
		}
		return "Grimmory est momentanément indisponible. Réessayez plus tard."
	}
	current, ok := s.currentBookGrant(user.ID)
	if !ok || current.Revision != g.Revision {
		return "Votre association Grimmory a changé. Reconnectez-la dans Le Hub."
	}
	message := fmt.Sprintf("**Vos livres et mangas · page %d**\n", page)
	if len(data.Items) == 0 {
		return message + "Aucun livre dans cette page de votre catalogue."
	}
	for index, book := range data.Items {
		line := "• " + safeText(book.Title)
		if len(book.Authors) > 0 {
			line += " · " + safeText(book.Authors[0])
		}
		if len(book.Formats) > 0 {
			line += " · " + strings.Join(book.Formats, ", ")
		} else {
			line += " · Aucun format proposé"
		}
		if len(message)+len(line) > 1700 {
			message += fmt.Sprintf("\n%d autre(s) livre(s) sur cette page. Consultez Le Hub pour les liens de lecture.", len(data.Items)-index)
			break
		}
		message += line + "\n"
	}
	if data.HasMore && page < 1001 {
		message += fmt.Sprintf("\nSuite : /livres page:%d (avec la même recherche).", page+1)
	}
	return message
}
func (s *Server) bookRequestMessage(query string) string {
	target, err := upstream.ShelfmarkSearchLink(s.bookServiceURL("shelfmark"), query)
	if err != nil {
		return "Le lien Shelfmark n’est pas configuré, ou la recherche est invalide."
	}
	if len(target) > 1500 {
		return "Cette recherche est trop longue pour un lien Discord. Ouvrez la page Livres du Hub pour la transmettre à Shelfmark."
	}
	return "[Choisir une édition dans Shelfmark](" + strings.ReplaceAll(strings.ReplaceAll(target, "(", "%28"), ")", "%29") + ")\n" + externalBookRequestMessage
}
