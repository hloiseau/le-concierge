package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

const bookVerificationTTL = 5 * time.Minute

type bookCatalogMatch struct {
	Book        *upstream.Book `json:"book"`
	Method      string         `json:"method"`
	ConfirmedAt time.Time      `json:"confirmedAt"`
	VerifiedAt  *time.Time     `json:"verifiedAt"`
}
type bookMatchResult struct {
	CatalogStatus string            `json:"catalogStatus"`
	CatalogMatch  *bookCatalogMatch `json:"catalogMatch,omitempty"`
}
type trackedBookRequest struct {
	upstream.BookRequest
	CatalogMatch *bookCatalogMatch `json:"catalogMatch,omitempty"`
}
type trackedBookRequestsPage struct {
	Items     []trackedBookRequest `json:"items"`
	Page      int                  `json:"page"`
	PageSize  int                  `json:"pageSize"`
	HasMore   bool                 `json:"hasMore"`
	CheckedAt time.Time            `json:"checkedAt"`
}

func (s *Server) bookTrackingScope(user store.User, shelf shelfmarkGrant, books bookGrant) string {
	data, _ := json.Marshal([]any{s.Config.ShelfmarkURL, shelf.Session.UserID, s.Config.GrimmoryURL, books.Credentials.Username, user.PlexID})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// The caller holds mu until the resulting read/write is complete, so a late
// upstream response cannot restore data after logout, unlink or account changes.
func (s *Server) bookTrackingOwnerLocked(ctx context.Context, user store.User, shelf shelfmarkGrant, books bookGrant, sessionKey string) bool {
	a, aOK := s.shelfmarkGrants[user.ID]
	b, bOK := s.bookGrants[user.ID]
	p, pOK := s.grants[user.ID]
	now := time.Now()
	if !aOK || !bOK || !pOK || a.Revision != shelf.Revision || b.Revision != books.Revision || a.PlexToken != p.AccountToken || b.PlexToken != p.AccountToken || !now.Before(a.ExpiresAt) || !now.Before(b.ExpiresAt) || !now.Before(p.ExpiresAt) {
		return false
	}
	if sessionKey != "" {
		session, ok := s.sessions[sessionKey]
		if !ok || session.UserID != user.ID || !now.Before(session.Expires) {
			return false
		}
	}
	current, err := s.Store.User(ctx, user.ID)
	return err == nil && current.PlexID != "" && current.PlexID == user.PlexID
}

func bookMatchResponse(m store.BookMatch, now time.Time) bookMatchResult {
	var book upstream.Book
	if json.Unmarshal(m.BookJSON, &book) != nil || book.ID != m.BookID {
		return bookMatchResult{CatalogStatus: "unverified"}
	}
	result := bookMatchResult{CatalogStatus: "check_required", CatalogMatch: &bookCatalogMatch{Method: "user_confirmed", ConfirmedAt: m.ConfirmedAt, VerifiedAt: m.VerifiedAt}}
	if m.VerifiedAt != nil && !m.VerifiedAt.After(now) && now.Before(m.VerifiedAt.Add(bookVerificationTTL)) {
		result.CatalogStatus = "available"
		result.CatalogMatch.Book = &book
	}
	return result
}

// The caller holds mu through the response write, including the source grant
// snapshot, so stored catalogue metadata cannot outlive a concurrent unlink.
func (s *Server) trackedBookRequests(ctx context.Context, user store.User, shelf shelfmarkGrant, books bookGrant, bookLinked bool, page upstream.BookRequestsPage) (trackedBookRequestsPage, error) {
	result := trackedBookRequestsPage{Items: []trackedBookRequest{}, Page: page.Page, PageSize: page.PageSize, HasMore: page.HasMore, CheckedAt: page.CheckedAt}
	for _, item := range page.Items {
		result.Items = append(result.Items, trackedBookRequest{BookRequest: item})
	}
	if !bookLinked {
		return result, nil
	}
	if !s.bookTrackingOwnerLocked(ctx, user, shelf, books, "") {
		return result, nil
	}
	matches, err := s.Store.BookMatches(ctx, user.ID, s.bookTrackingScope(user, shelf, books))
	if err != nil {
		return result, err
	}
	byRequest := map[string]store.BookMatch{}
	for _, match := range matches {
		byRequest[match.RequestID] = match
	}
	for i := range result.Items {
		if match, ok := byRequest[result.Items[i].ID]; ok {
			linked := bookMatchResponse(match, time.Now())
			if result.Items[i].Status != "fulfilled" || result.Items[i].DeliveryState != "complete" {
				linked.CatalogStatus = "check_required"
				if linked.CatalogMatch != nil {
					linked.CatalogMatch.Book = nil
				}
			}
			result.Items[i].CatalogStatus, result.Items[i].CatalogMatch = linked.CatalogStatus, linked.CatalogMatch
		}
	}
	return result, nil
}

func (s *Server) bookRequestCatalogMatch(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	key, user, ok := s.user(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if len(r.URL.Query()) != 0 || !upstream.ValidBookID(id) {
		problem(w, 400, "invalid_query", "Choisissez une demande depuis votre liste.")
		return
	}
	if s.Config.ShelfmarkURL == "" || s.Config.GrimmoryURL == "" {
		problem(w, 503, "not_configured", "Le suivi Shelfmark et le catalogue Grimmory doivent être configurés.")
		return
	}
	shelf, ok := s.currentShelfmarkGrant(user.ID)
	if !ok {
		shelfmarkFailure(w, upstream.ErrDenied)
		return
	}
	books, ok := s.currentBookGrant(user.ID)
	if !ok {
		s.bookFailure(w, upstream.ErrDenied)
		return
	}
	if s.limited(w, r, "book-match", 20) {
		return
	}
	scope := s.bookTrackingScope(user, shelf, books)
	if r.Method == http.MethodDelete {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.bookTrackingOwnerLocked(r.Context(), user, shelf, books, key) {
			problem(w, 409, "link_changed", "L’association a changé. Rechargez la page.")
			return
		}
		if err := s.Store.RemoveBookMatch(r.Context(), user.ID, scope, id); err != nil {
			problem(w, 503, "storage_unavailable", "La correspondance n’a pas pu être retirée.")
			return
		}
		// Invalidate pending confirmations, including a first match not saved yet.
		current := s.shelfmarkGrants[user.ID]
		current.CatalogRevision = token()
		s.shelfmarkGrants[user.ID] = current
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var input struct {
		RequestPage  int    `json:"requestPage"`
		BookID       string `json:"bookId"`
		CatalogPage  int    `json:"catalogPage"`
		CatalogQuery string `json:"catalogQuery"`
	}
	d := json.NewDecoder(io.LimitReader(r.Body, 4097))
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || input.RequestPage < 1 || input.RequestPage > 1001 || input.CatalogPage < 1 || input.CatalogPage > 1001 || !upstream.ValidBookID(input.BookID) || !upstream.ValidBookQuery(input.CatalogQuery) {
		problem(w, 400, "invalid_body", "Choisissez une édition depuis le catalogue personnel.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	requests, err := s.Upstream.ShelfmarkRequests(ctx, s.Config.ShelfmarkURL, shelf.Session, input.RequestPage)
	if err != nil {
		if errors.Is(err, upstream.ErrDenied) {
			s.forgetShelfmarkGrant(user.ID, shelf)
		}
		shelfmarkFailure(w, err)
		return
	}
	var request *upstream.BookRequest
	for i := range requests.Items {
		if requests.Items[i].ID == id {
			request = &requests.Items[i]
			break
		}
	}
	if request == nil {
		problem(w, 409, "selection_changed", "La liste des demandes a changé. Rechargez-la avant de confirmer.")
		return
	}
	if request.Status != "fulfilled" || request.DeliveryState != "complete" {
		problem(w, 409, "request_not_delivered", "La livraison Shelfmark doit être terminée avant de confirmer l’édition.")
		return
	}
	catalog, err := s.Upstream.GrimmoryBooks(ctx, s.Config.GrimmoryURL, s.bookServiceURL("grimmory"), books.Credentials, input.CatalogQuery, input.CatalogPage)
	if err != nil {
		if errors.Is(err, upstream.ErrDenied) {
			s.forgetBookGrant(user.ID, books)
		}
		s.bookFailure(w, err)
		return
	}
	var selected *upstream.Book
	for i := range catalog.Items {
		if catalog.Items[i].ID == input.BookID && len(catalog.Items[i].Formats) > 0 {
			selected = &catalog.Items[i]
			break
		}
	}
	if selected == nil {
		problem(w, 409, "selection_changed", "Cette édition n’est plus disponible sur cette page. Actualisez le catalogue.")
		return
	}
	if !s.revalidateUser(w, r, key, user) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bookTrackingOwnerLocked(ctx, user, shelf, books, key) || s.shelfmarkGrants[user.ID].CatalogRevision != shelf.CatalogRevision {
		problem(w, 409, "link_changed", "L’association a changé. Rechargez la page.")
		return
	}
	data, _ := json.Marshal(selected)
	requestData, _ := json.Marshal(request)
	// Follow the full catalogue after confirmation; a search is navigation, never
	// identity evidence, and metadata changes must not strand the saved cursor.
	catalogPage := input.CatalogPage
	if input.CatalogQuery != "" {
		catalogPage = 1
	}
	match, err := s.Store.ConfirmBookMatch(ctx, store.BookMatch{UserID: user.ID, Scope: scope, RequestID: id, BookID: selected.ID, BookJSON: data, RequestJSON: requestData, Revision: token(), RequestPage: input.RequestPage, CatalogPage: catalogPage}, time.Now().UTC())
	if errors.Is(err, store.ErrBookMatchLimit) {
		problem(w, 409, "book_match_limit", "La limite de correspondances conservées est atteinte (100 actives par compte source, 1 000 avec l’historique). Retirez une correspondance ou attendez l’expiration de l’historique.")
		return
	}
	if err != nil {
		problem(w, 503, "storage_unavailable", "La correspondance n’a pas pu être conservée.")
		return
	}
	jsonOut(w, 200, bookMatchResponse(match, time.Now()))
}

func nextBookScanPage(page int, hasMore bool) int {
	if hasMore && page < 1001 {
		return page + 1
	}
	return 1
}

// Verify one old correspondence using two bounded source pages. Grants
// remain in memory; persisted cursors resume after the owner reconnects.
func (s *Server) refreshBookTracking(ctx context.Context, user store.User) error {
	if _, err := s.grant(ctx, user); err != nil {
		return err
	}
	shelf, ok := s.currentShelfmarkGrant(user.ID)
	if !ok {
		return nil
	}
	books, ok := s.currentBookGrant(user.ID)
	if !ok {
		return nil
	}
	matches, err := s.Store.BookMatches(ctx, user.ID, s.bookTrackingScope(user, shelf, books))
	if err != nil || len(matches) == 0 {
		return err
	}
	m := matches[0]
	// Reserve the turn before network I/O so even a timed-out source cannot
	// monopolize this owner's next polling cycle.
	s.mu.Lock()
	if !s.bookTrackingOwnerLocked(ctx, user, shelf, books, "") {
		s.mu.Unlock()
		return upstream.ErrDenied
	}
	started, err := s.Store.BeginBookMatchCheck(ctx, m, time.Now().UTC())
	s.mu.Unlock()
	if err != nil || !started {
		return err
	}
	failed := func(err error) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.bookTrackingOwnerLocked(ctx, user, shelf, books, "") {
			if saveErr := s.Store.ObserveBookMatch(ctx, m, false, time.Now().UTC()); saveErr != nil {
				return saveErr
			}
		}
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	requests, err := s.Upstream.ShelfmarkRequests(ctx, s.Config.ShelfmarkURL, shelf.Session, m.RequestPage)
	if err != nil {
		if errors.Is(err, upstream.ErrDenied) {
			s.forgetShelfmarkGrant(user.ID, shelf)
		}
		return failed(err)
	}
	requestFound, delivered := false, false
	for _, item := range requests.Items {
		if item.ID == m.RequestID {
			requestFound = true
			delivered = item.Status == "fulfilled" && item.DeliveryState == "complete"
			m.RequestJSON, _ = json.Marshal(item)
			break
		}
	}
	if !requestFound {
		m.RequestPage = nextBookScanPage(m.RequestPage, requests.HasMore)
	}
	catalog, err := s.Upstream.GrimmoryBooks(ctx, s.Config.GrimmoryURL, s.bookServiceURL("grimmory"), books.Credentials, "", m.CatalogPage)
	if err != nil {
		if errors.Is(err, upstream.ErrDenied) {
			s.forgetBookGrant(user.ID, books)
		}
		return failed(err)
	}
	bookFound := false
	for _, item := range catalog.Items {
		if item.ID == m.BookID && len(item.Formats) > 0 {
			bookFound = true
			m.BookJSON, _ = json.Marshal(item)
			break
		}
	}
	if !bookFound {
		m.CatalogPage = nextBookScanPage(m.CatalogPage, catalog.HasMore)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bookTrackingOwnerLocked(ctx, user, shelf, books, "") {
		return upstream.ErrDenied
	}
	return s.Store.ObserveBookMatch(ctx, m, requestFound && delivered && bookFound, time.Now().UTC())
}

// Bound source traffic while allowing a normal collection to stay within the
// five-minute evidence lifetime. Slow sources still expire rather than lie.
func (s *Server) refreshBookTrackingBatch(ctx context.Context, user store.User) error {
	shelf, shelfOK := s.currentShelfmarkGrant(user.ID)
	books, booksOK := s.currentBookGrant(user.ID)
	if !shelfOK || !booksOK {
		return nil
	}
	matches, err := s.Store.BookMatches(ctx, user.ID, s.bookTrackingScope(user, shelf, books))
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-time.Minute)
	for _, match := range matches[:min(20, len(matches))] {
		if match.CheckedAt.After(cutoff) {
			break
		}
		if err := s.refreshBookTracking(ctx, user); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) RunBookTracking(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	lastUser := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := s.Store.PruneBookTracking(ctx, time.Now()); err != nil && ctx.Err() == nil {
			slog.Warn("Book tracking retention cleanup deferred")
		}
		if s.Config.ShelfmarkURL == "" || s.Config.GrimmoryURL == "" {
			continue
		}
		s.mu.Lock()
		ids := []string{}
		for id := range s.shelfmarkGrants {
			if _, ok := s.bookGrants[id]; ok {
				ids = append(ids, id)
			}
		}
		s.mu.Unlock()
		sort.Strings(ids)
		split := sort.SearchStrings(ids, lastUser)
		for split < len(ids) && ids[split] <= lastUser {
			split++
		}
		ids = append(ids[split:], ids[:split]...)
		cycle, cancel := context.WithTimeout(ctx, 45*time.Second)
		for _, id := range ids {
			if cycle.Err() != nil {
				break
			}
			lastUser = id
			user, err := s.Store.User(cycle, id)
			if err == nil {
				err = s.refreshBookTrackingBatch(cycle, user)
			}
			if err != nil && cycle.Err() == nil {
				slog.Warn("Book catalogue verification deferred")
			}
		}
		cancel()
	}
}
