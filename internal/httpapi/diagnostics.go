package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) diagnosticSources() upstream.DiagnosticSources {
	return upstream.DiagnosticSources{SeerrURL: s.Config.SeerrURL, SeerrKey: s.Config.SeerrAPIKey, RadarrURL: s.Config.RadarrURL, RadarrKey: s.Config.RadarrAPIKey, SonarrURL: s.Config.SonarrURL, SonarrKey: s.Config.SonarrAPIKey, PlexURL: s.Config.PlexURL, PlexServerID: s.Config.PlexServerID}
}
func (s *Server) adminDiagnostic(w http.ResponseWriter, r *http.Request) {
	key, user, ok := s.user(w, r)
	if !ok {
		return
	}
	if !s.Config.Admin(user.DiscordID) {
		problem(w, 403, "admin_required", "Ce diagnostic est réservé aux administrateurs.")
		return
	}
	q := r.URL.Query()
	requestID, err := strconv.ParseInt(q.Get("requestId"), 10, 64)
	if err != nil || requestID < 1 || requestID > 2147483647 || len(q) != 1 || len(q["requestId"]) != 1 {
		problem(w, 400, "invalid_query", "Indiquez un identifiant de demande Seerr entier positif.")
		return
	}
	if s.limited(w, r, "diagnostics", 12) {
		return
	}
	grant, err := s.grant(r.Context(), user)
	if err != nil {
		problem(w, 403, "plex_required", "Reconnectez-vous avec Plex.")
		return
	}
	result, err := s.Upstream.DiagnoseRequest(r.Context(), s.diagnosticSources(), grant, requestID)
	if !s.revalidateUser(w, r, key, user) {
		return
	}
	current, lookupErr := s.Store.User(r.Context(), user.ID)
	if lookupErr != nil || !s.Config.Admin(current.DiscordID) {
		problem(w, 403, "admin_required", "Votre accès administrateur a changé.")
		return
	}
	if err != nil {
		if errors.Is(err, upstream.ErrNotFound) {
			problem(w, 404, "request_not_found", "Demande introuvable.")
			return
		}
		problem(w, 503, "diagnostic_unavailable", "Diagnostic indisponible : vérifiez les services configurés, leurs droits et leur correspondance dans Seerr.")
		return
	}
	jsonOut(w, 200, result)
}
func (s *Server) diagnosticMessage(ctx context.Context, user store.User, requestID int64) string {
	if !s.Config.Admin(user.DiscordID) {
		return "Cette commande est réservée aux administrateurs."
	}
	grant, err := s.grant(ctx, user)
	if err != nil {
		return "Reconnectez-vous au Hub avec Plex."
	}
	result, err := s.Upstream.DiagnoseRequest(ctx, s.diagnosticSources(), grant, requestID)
	current, lookupErr := s.Store.User(ctx, user.ID)
	if lookupErr != nil || !s.Config.Admin(current.DiscordID) || current.DiscordID != user.DiscordID {
		return "Votre accès administrateur a changé."
	}
	if err != nil {
		return "Diagnostic indisponible. Vérifiez les services configurés, leurs droits et leur correspondance dans Seerr."
	}
	message := fmt.Sprintf("**Diagnostic administrateur · %s %s**\n%s\n", strings.ToUpper(result.Source[:1])+result.Source[1:], result.Version, result.Summary)
	for _, evidence := range result.Evidence {
		if evidence.Message == result.Summary {
			continue
		}
		line := "• " + evidence.Message
		if evidence.At != "" {
			line += " (" + evidence.At + ")"
		}
		if len(message)+len(line) > 1550 {
			message += "\nSuite dans l’API de diagnostic administrateur.\n"
			break
		}
		message += line + "\n"
	}
	if result.QueueTruncated || result.HistoryTruncated {
		message += "\nLecture limitée aux 50 éléments de file et 30 événements récents du média.\n"
	}
	if result.GuideURL != "" {
		message += "\n[Guide officiel : vérifier le média avant import manuel](" + result.GuideURL + ")"
	}
	return message
}
