package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hloiseau/le-concierge/internal/store"
)

func (s *Server) extensionsEnabled() bool { return s.departuresEnabled() && len(s.Config.AdminIDs) > 0 }

func (s *Server) getExtensionRequests(w http.ResponseWriter, r *http.Request) {
	_, u, ok := s.user(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "La liste utilise uniquement votre compte connecté.")
		return
	}
	items, err := s.Store.ExtensionRequests(r.Context(), u.ID, time.Now())
	if err != nil {
		problem(w, 500, "storage_error", "Lecture des demandes impossible.")
		return
	}
	jsonOut(w, 200, map[string]any{"items": items})
}

func (s *Server) postExtensionRequest(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	var input struct {
		MediaID string `json:"mediaId"`
		Days    int    `json:"days"`
		Reason  string `json:"reason"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if len(r.URL.Query()) != 0 || d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || input.MediaID == "" || len(input.MediaID) > 200 || input.Days < 1 || input.Days > 30 || utf8.RuneCountInString(input.Reason) > 500 {
		problem(w, 400, "invalid_body", "Indiquez un contenu, un délai de 1 à 30 jours et un motif de 500 caractères maximum.")
		return
	}
	if !s.extensionsEnabled() {
		problem(w, 503, "not_configured", "Les demandes de délai ne sont pas activées.")
		return
	}
	if s.limited(w, r, "extension-requests", 12) {
		return
	}
	g, err := s.grant(r.Context(), u)
	if err != nil {
		problem(w, 403, "plex_required", "Reconnectez-vous avec Plex.")
		return
	}
	p, err := s.Store.GetReminderPreferences(r.Context(), u.ID)
	if err != nil {
		problem(w, 500, "storage_error", "Lecture des préférences impossible.")
		return
	}
	page, err := s.personalDepartures(r.Context(), u, g, p, true)
	if err != nil {
		problem(w, 503, "departures_unavailable", "Le départ ne peut pas être vérifié actuellement.")
		return
	}
	if !s.revalidateUser(w, r, key, u) {
		return
	}
	var request *store.ExtensionRequest
	now := time.Now().UTC()
	for _, item := range page.Items {
		if item.ID == input.MediaID && item.EstimatedAt.After(now) {
			request = &store.ExtensionRequest{Key: digest(s.Config.PlexServerID + ":" + reminderMediaKey(item) + ":" + strconv.FormatInt(item.EstimatedAt.UnixMilli(), 10)), MediaID: item.ID, Title: item.Title, Kind: item.Kind, URL: item.URL, EstimatedAt: item.EstimatedAt, Days: input.Days, Reason: strings.TrimSpace(input.Reason)}
			break
		}
	}
	if request == nil || !now.Before(page.ExpiresAt) {
		problem(w, 409, "departure_changed", "Ce départ a changé. Actualisez la liste avant de réessayer.")
		return
	}
	// Keep the saved request bound to the same live session and Plex grant.
	s.mu.Lock()
	now = time.Now().UTC()
	current, exists := s.grants[u.ID]
	sess, sessionExists := s.sessions[key]
	owner, err := s.Store.User(r.Context(), u.ID)
	if err != nil || !exists || !sessionExists || sess.UserID != u.ID || !now.Before(sess.Expires) || owner.PlexID != u.PlexID || current.AccountToken != g.AccountToken || current.ServerToken != g.ServerToken || !current.Active(now) || !now.Before(page.ExpiresAt) || !now.Before(request.EstimatedAt) {
		s.mu.Unlock()
		problem(w, 403, "plex_required", "Reconnectez-vous avec Plex.")
		return
	}
	created, err := s.Store.CreateExtensionRequest(r.Context(), u.ID, *request, now)
	s.mu.Unlock()
	if errors.Is(err, store.ErrExtensionLimit) {
		problem(w, 429, "request_limit", "Vous avez déjà dix demandes en attente ou créées depuis 24 heures.")
		return
	}
	if err != nil {
		problem(w, 500, "storage_error", "Enregistrement de la demande impossible.")
		return
	}
	jsonOut(w, 200, created)
}

func (s *Server) cancelExtensionRequest(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	g, ok := s.requireGrant(w, r, u)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != r.PathValue("id") || len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_id", "Identifiant de demande invalide.")
		return
	}
	if json.NewDecoder(r.Body).Decode(new(any)) != io.EOF {
		problem(w, 400, "invalid_body", "L’annulation ne prend aucun paramètre supplémentaire.")
		return
	}
	s.mu.Lock()
	now := time.Now().UTC()
	current, exists := s.grants[u.ID]
	sess, sessionExists := s.sessions[key]
	owner, err := s.Store.User(r.Context(), u.ID)
	if err != nil || !exists || !sessionExists || sess.UserID != u.ID || !now.Before(sess.Expires) || owner.PlexID != u.PlexID || current.PlexID != u.PlexID || current.AccountToken != g.AccountToken || current.ServerToken != g.ServerToken || !current.Active(now) {
		s.mu.Unlock()
		problem(w, 403, "plex_required", "Votre accès a changé. Reconnectez-vous avec Plex.")
		return
	}
	err = s.Store.CancelExtensionRequest(r.Context(), u.ID, id, now)
	s.mu.Unlock()
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 409, "request_changed", "Cette demande n’est plus en attente ou son échéance est passée.")
		return
	}
	if err != nil {
		problem(w, 500, "storage_error", "Annulation de la demande impossible.")
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (s *Server) extensionAdmin(w http.ResponseWriter, r *http.Request) (string, store.User, bool) {
	key, u, ok := s.user(w, r)
	if !ok {
		return key, u, false
	}
	if !s.Config.Admin(u.DiscordID) {
		problem(w, 403, "admin_required", "Cette file est réservée aux administrateurs.")
		return key, u, false
	}
	return key, u, true
}

func (s *Server) adminExtensionRequests(w http.ResponseWriter, r *http.Request) {
	key, u, ok := s.extensionAdmin(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "La file affiche les 100 plus anciennes demandes en attente.")
		return
	}
	items, err := s.Store.ExtensionRequests(r.Context(), "", time.Now())
	if err != nil {
		problem(w, 500, "storage_error", "Lecture de la file impossible.")
		return
	}
	if !s.revalidateUser(w, r, key, u) {
		return
	}
	owner, err := s.Store.User(r.Context(), u.ID)
	if err != nil || !s.Config.Admin(owner.DiscordID) {
		problem(w, 403, "admin_required", "Accès administrateur requis.")
		return
	}
	jsonOut(w, 200, map[string]any{"items": items})
}

func (s *Server) reviewExtensionRequest(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	key, u, ok := s.extensionAdmin(w, r)
	if !ok {
		return
	}
	var input struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if len(r.URL.Query()) != 0 || d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || input.ID <= 0 || (input.Status != "reviewed" && input.Status != "declined") || utf8.RuneCountInString(input.Note) > 500 {
		problem(w, 400, "invalid_body", "Réponse administrateur invalide.")
		return
	}
	s.mu.Lock()
	current, exists := s.grants[u.ID]
	sess, sessionExists := s.sessions[key]
	owner, err := s.Store.User(r.Context(), u.ID)
	now := time.Now().UTC()
	if err != nil || !exists || !sessionExists || sess.UserID != u.ID || !now.Before(sess.Expires) || !current.Active(now) || owner.PlexID != u.PlexID || !s.Config.Admin(owner.DiscordID) {
		s.mu.Unlock()
		problem(w, 403, "admin_required", "Accès administrateur requis.")
		return
	}
	err = s.Store.ReviewExtensionRequest(r.Context(), input.ID, input.Status, input.Note, now)
	s.mu.Unlock()
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 409, "request_changed", "Cette demande n’est plus en attente ou son échéance est passée.")
		return
	}
	if err != nil {
		problem(w, 500, "storage_error", "Enregistrement de la réponse impossible.")
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
