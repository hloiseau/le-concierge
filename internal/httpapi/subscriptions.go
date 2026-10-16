package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

var errSubscriptionChannel = errors.New("subscription channel unavailable")

type subscriptionInput struct {
	store.SubscriptionTarget
	Channel string `json:"channel"`
}

type subscriptionView struct {
	store.Subscription
	Title  string `json:"title"`
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
}

type subscriptionResult struct {
	Status       string            `json:"status"`
	Title        string            `json:"title"`
	URL          string            `json:"url,omitempty"`
	Subscription *subscriptionView `json:"subscription,omitempty"`
}

func (s *Server) subscriptionsEnabled() bool {
	return s.Config.SeerrURL != "" && s.Config.SeerrAPIKey != "" && s.Config.PlexURL != "" && s.Config.PlexServerID != ""
}

func (s *Server) checkSubscription(ctx context.Context, u store.User, g upstream.Grant, target store.SubscriptionTarget) (upstream.SubscriptionMedia, error) {
	seerrID, err := s.resolveSeerr(ctx, u)
	if err != nil {
		return upstream.SubscriptionMedia{}, err
	}
	return s.Upstream.CheckSubscription(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, seerrID, s.Config.PlexURL, s.Config.PlexServerID, g, target.Type, target.TMDBID, target.Season)
}

// createFollow is shared by authenticated HTTP and signed Discord interactions.
func (s *Server) createFollow(ctx context.Context, u store.User, input subscriptionInput) (subscriptionResult, error) {
	if err := store.ValidateSubscription(input.SubscriptionTarget, input.Channel); err != nil {
		return subscriptionResult{}, err
	}
	if input.Channel == "discord" {
		return subscriptionResult{}, errSubscriptionChannel
	}
	g, err := s.grant(ctx, u)
	if err != nil {
		return subscriptionResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	media, err := s.checkSubscription(ctx, u, g, input.SubscriptionTarget)
	if err != nil {
		return subscriptionResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.grants[u.ID]
	owner, err := s.Store.User(ctx, u.ID)
	if err != nil || !exists || owner.PlexID != u.PlexID || current.AccountToken != g.AccountToken || current.ServerToken != g.ServerToken || !time.Now().Before(current.ExpiresAt) {
		return subscriptionResult{}, upstream.ErrDenied
	}
	result := subscriptionResult{Title: media.Title, URL: media.URL}
	if media.Available {
		result.Status = "already_available"
		// A repeated create after import must not leave a pending future delivery.
		return result, s.Store.DeleteSubscriptionTarget(ctx, u.ID, input.SubscriptionTarget)
	}
	item, err := s.Store.SaveSubscription(ctx, u.ID, input.SubscriptionTarget, input.Channel)
	if err != nil {
		return subscriptionResult{}, err
	}
	result.Status = "following"
	result.Subscription = &subscriptionView{Subscription: item, Title: media.Title, Status: "following"}
	return result, nil
}

func (s *Server) listFollows(ctx context.Context, u store.User, offset int) ([]subscriptionView, int, error) {
	items, total, err := s.Store.Subscriptions(ctx, u.ID, offset)
	if err != nil {
		return nil, 0, err
	}
	views := []subscriptionView{}
	if len(items) == 0 {
		return views, total, nil
	}
	g, err := s.grant(ctx, u)
	if err != nil {
		return nil, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	seerrID, err := s.resolveSeerr(ctx, u)
	if err != nil {
		return nil, 0, err
	}
	for _, item := range items {
		media, err := s.Upstream.CheckSubscription(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, seerrID, s.Config.PlexURL, s.Config.PlexServerID, g, item.Type, item.TMDBID, item.Season)
		if errors.Is(err, upstream.ErrDenied) || errors.Is(err, upstream.ErrNotFound) {
			views = append(views, subscriptionView{Subscription: item, Title: "Contenu inaccessible", Status: "unavailable"})
			continue
		}
		if err != nil {
			return nil, 0, err
		}
		status := "following"
		if media.Available {
			status = "available"
			if !media.RequestedBySelf {
				if err := s.recordSubscriptionAvailability(ctx, u, g, item.ID); err != nil {
					return nil, 0, err
				}
			}
		}
		views = append(views, subscriptionView{Subscription: item, Title: media.Title, Status: status, URL: media.URL})
	}
	return views, total, nil
}

func (s *Server) recordSubscriptionAvailability(ctx context.Context, u store.User, g upstream.Grant, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.grants[u.ID]
	owner, err := s.Store.User(ctx, u.ID)
	if err != nil || !exists || owner.PlexID != u.PlexID || current.AccountToken != g.AccountToken || current.ServerToken != g.ServerToken || !time.Now().Before(current.ExpiresAt) {
		return upstream.ErrDenied
	}
	_, err = s.Store.MarkSubscriptionAvailable(ctx, u.ID, id, time.Now())
	return err
}

func subscriptionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errSubscriptionChannel):
		problem(w, 409, "channel_unavailable", "Les suivis sont disponibles dans le Hub. Les alertes Discord de disponibilité ne sont pas encore activées.")
	case errors.Is(err, store.ErrSubscriptionLimit):
		problem(w, 409, "subscription_limit", "La limite de 100 suivis est atteinte. Désabonnez-vous d’un contenu pour en suivre un autre.")
	case errors.Is(err, upstream.ErrSeerrUserNotFound):
		problem(w, 409, "seerr_unlinked", seerrMissing)
	case errors.Is(err, upstream.ErrNotFound), errors.Is(err, upstream.ErrDenied):
		problem(w, 404, "media_not_found", "Ce contenu n’est pas accessible ou ne possède pas de demande active pour ce suivi.")
	default:
		problem(w, 503, "subscriptions_unavailable", "Les suivis ne peuvent pas être vérifiés actuellement. Réessayez plus tard.")
	}
}

func (s *Server) getSubscriptions(w http.ResponseWriter, r *http.Request) {
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	for key, values := range r.URL.Query() {
		if key != "offset" || len(values) != 1 {
			problem(w, 400, "invalid_query", "Pagination des suivis invalide.")
			return
		}
	}
	off, err := offset(r)
	if err != nil {
		problem(w, 400, "invalid_offset", "Pagination invalide.")
		return
	}
	if !s.subscriptionsEnabled() {
		problem(w, 503, "not_configured", "Les sources des suivis ne sont pas configurées.")
		return
	}
	if s.limited(w, r, "subscriptions", 30) {
		return
	}
	items, total, err := s.listFollows(r.Context(), u, off)
	if err != nil {
		subscriptionError(w, err)
		return
	}
	if s.revalidateUser(w, r, key, u) {
		jsonOut(w, 200, map[string]any{"items": items, "total": total, "offset": off, "limit": 10})
	}
}

func (s *Server) postSubscription(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	key, u, ok := s.user(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "Le suivi utilise uniquement votre compte connecté.")
		return
	}
	var input subscriptionInput
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || store.ValidateSubscription(input.SubscriptionTarget, input.Channel) != nil {
		problem(w, 400, "invalid_body", "Le type, l’identifiant TMDB, la saison ou le canal du suivi est invalide.")
		return
	}
	if !s.subscriptionsEnabled() {
		problem(w, 503, "not_configured", "Les sources des suivis ne sont pas configurées.")
		return
	}
	if s.limited(w, r, "subscriptions", 30) {
		return
	}
	result, err := s.createFollow(r.Context(), u, input)
	if err != nil {
		subscriptionError(w, err)
		return
	}
	if s.revalidateUser(w, r, key, u) {
		jsonOut(w, 200, result)
	}
}

func (s *Server) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	_, u, ok := s.user(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != r.PathValue("id") || len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_id", "Identifiant de suivi invalide.")
		return
	}
	if err = s.Store.DeleteSubscription(r.Context(), u.ID, id); errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "subscription_not_found", "Suivi introuvable.")
		return
	}
	if err != nil {
		problem(w, 500, "storage_error", "Désabonnement impossible.")
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
