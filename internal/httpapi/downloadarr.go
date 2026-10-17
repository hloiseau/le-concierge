package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

// The independent Downloadarr credential belongs to one current Hub session.
type downloadarrGrant struct {
	Bearer, FlowID, Revision, PlexToken, SessionKey string
	ExpiresAt, LastPoll                             time.Time
	Polling                                         bool
	Context                                         context.Context
	Cancel                                          context.CancelFunc
}

type downloadarrLinkStatus struct {
	Linked         bool       `json:"linked"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
	QualityOptions []string   `json:"qualityOptions,omitempty"`
}

func (s *Server) clearDownloadarrGrantLocked(userID string) {
	if g, ok := s.downloadarrGrants[userID]; ok && g.Cancel != nil {
		g.Cancel()
	}
	delete(s.downloadarrGrants, userID)
}

func (s *Server) downloadarrEnabled() bool {
	return s.Config.DownloadarrURL != "" && s.Config.DownloadarrRevision != ""
}

func (s *Server) currentDownloadarrGrant(key, userID string) (downloadarrGrant, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.downloadarrGrants[userID]
	if !ok || g.SessionKey != key {
		return downloadarrGrant{}, false
	}
	p, plexOK := s.grants[userID]
	sess, sessionOK := s.sessions[key]
	now := time.Now()
	if !plexOK || p.AccountToken != g.PlexToken || !now.Before(p.ExpiresAt) || !now.Before(g.ExpiresAt) || !sessionOK || sess.UserID != userID || !now.Before(sess.Expires) || g.Context.Err() != nil {
		s.clearDownloadarrGrantLocked(userID)
		return downloadarrGrant{}, false
	}
	return g, true
}

func (s *Server) forgetDownloadarrGrant(userID string, g downloadarrGrant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.downloadarrGrants[userID]; ok && current.Revision == g.Revision {
		s.clearDownloadarrGrantLocked(userID)
	}
}

func downloadarrContext(parent context.Context, g downloadarrGrant) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithDeadline(parent, g.ExpiresAt)
	stop := context.AfterFunc(g.Context, cancel)
	return ctx, func() { stop(); cancel() }
}

func (s *Server) downloadarrFailure(w http.ResponseWriter, userID string, g downloadarrGrant, err error) {
	status, code, message := 503, "downloadarr_unavailable", "Downloadarr est indisponible ou son contrat ne correspond pas à la version configurée. Réessayez plus tard."
	var remote *upstream.DownloadarrError
	if errors.Is(err, upstream.ErrDenied) {
		s.forgetDownloadarrGrant(userID, g)
		status, code, message = 403, "downloadarr_link_required", "L’accès Downloadarr n’est plus actif. Actualisez votre accès avec votre compte Plex."
	} else if errors.Is(err, upstream.ErrNotFound) {
		status, code, message = 404, "download_not_found", "Cette préparation a expiré ou a disparu. Actualisez la liste ; aucune conversion n’a été relancée."
	} else if errors.As(err, &remote) {
		switch remote.Status {
		case 400:
			status, code, message = 400, "invalid_download", "Le contenu ou les choix ne sont plus valides. Rouvrez les options dans Downloadarr."
		case 409:
			status, code, message = 409, "download_conflict", "Cette action n’est pas disponible dans l’état actuel. Actualisez la préparation."
		case 429:
			status, code, message = 429, "downloadarr_rate_limited", "Downloadarr demande de patienter avant une nouvelle tentative."
			seconds := max(1, min(3600, int(remote.RetryAfter.Seconds())))
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
		}
	}
	problem(w, status, code, message)
}

func emptyDownloadarrBody(w http.ResponseWriter, r *http.Request) bool {
	b, err := io.ReadAll(io.LimitReader(r.Body, 33))
	if err != nil || len(b) > 32 || (strings.TrimSpace(string(b)) != "" && strings.TrimSpace(string(b)) != "{}") {
		problem(w, 400, "invalid_body", "Cette action n’accepte aucun identifiant de compte ni jeton.")
		return false
	}
	return true
}

func (s *Server) verifyDownloadarr(w http.ResponseWriter, r *http.Request, key string, user store.User, g downloadarrGrant) (upstream.DownloadarrContract, bool) {
	ctx, cancel := downloadarrContext(r.Context(), g)
	defer cancel()
	contract, err := s.Upstream.DownloadarrVerify(ctx, s.Config.DownloadarrURL, g.Bearer, user.PlexID, s.Config.PlexServerID, s.Config.DownloadarrRevision)
	if err != nil {
		s.downloadarrFailure(w, user.ID, g, err)
		return contract, false
	}
	if !s.revalidateUser(w, r, key, user) {
		return contract, false
	}
	current, ok := s.currentDownloadarrGrant(key, user.ID)
	if !ok || current.Revision != g.Revision {
		s.downloadarrFailure(w, user.ID, g, upstream.ErrDenied)
		return contract, false
	}
	// A source can shorten its real session deadline at any observation.
	if contract.ExpiresAt.Before(current.ExpiresAt) {
		s.mu.Lock()
		if v, exists := s.downloadarrGrants[user.ID]; exists && v.Revision == g.Revision {
			v.ExpiresAt = contract.ExpiresAt
			s.downloadarrGrants[user.ID] = v
		}
		s.mu.Unlock()
	}
	return contract, true
}

func (s *Server) downloadarrLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && (!s.origin(w, r) || !emptyDownloadarrBody(w, r)) {
		return
	}
	key, user, ok := s.user(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "Cette connexion utilise votre compte actuel.")
		return
	}
	if r.Method == http.MethodDelete {
		g, ok := s.currentDownloadarrGrant(key, user.ID)
		if ok {
			s.forgetDownloadarrGrant(user.ID, g)
		}
		jsonOut(w, 200, downloadarrLinkStatus{})
		return
	}
	if !s.downloadarrEnabled() {
		problem(w, 503, "not_configured", "L’intégration Downloadarr n’est pas configurée. Vous pouvez ouvrir son application.")
		return
	}
	if r.Method == http.MethodGet {
		g, ok := s.currentDownloadarrGrant(key, user.ID)
		if !ok || g.Bearer == "" {
			jsonOut(w, 200, downloadarrLinkStatus{})
			return
		}
		contract, ok := s.verifyDownloadarr(w, r, key, user, g)
		if ok {
			at := minDownloadarrExpiry(g.ExpiresAt, contract.ExpiresAt)
			jsonOut(w, 200, downloadarrLinkStatus{Linked: true, ExpiresAt: &at, QualityOptions: contract.QualityOptions})
		}
		return
	}
	if s.limited(w, r, "downloadarr-link", 5) {
		return
	}
	plex, err := s.grant(r.Context(), user)
	if err != nil {
		s.downloadarrFailure(w, user.ID, downloadarrGrant{}, upstream.ErrDenied)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	g := downloadarrGrant{PlexToken: plex.AccountToken, SessionKey: key, Revision: token(), ExpiresAt: minDownloadarrExpiry(time.Now().Add(5*time.Minute), plex.ExpiresAt), Context: ctx, Cancel: cancel}
	s.mu.Lock()
	if s.downloadarrGrants == nil {
		s.downloadarrGrants = map[string]downloadarrGrant{}
	}
	s.clearDownloadarrGrantLocked(user.ID)
	s.downloadarrGrants[user.ID] = g
	s.mu.Unlock()
	callCtx, done := downloadarrContext(r.Context(), g)
	defer done()
	pin, err := s.Upstream.DownloadarrStart(callCtx, s.Config.DownloadarrURL)
	if err != nil {
		s.forgetDownloadarrGrant(user.ID, g)
		s.downloadarrFailure(w, user.ID, g, err)
		return
	}
	if !s.revalidateUser(w, r, key, user) {
		s.forgetDownloadarrGrant(user.ID, g)
		return
	}
	s.mu.Lock()
	current, exists := s.downloadarrGrants[user.ID]
	if !exists || current.Revision != g.Revision || g.Context.Err() != nil {
		s.mu.Unlock()
		problem(w, 409, "link_changed", "La connexion a été annulée. Recommencez depuis votre compte.")
		return
	}
	g.FlowID = pin.FlowID
	g.ExpiresAt = minDownloadarrExpiry(g.ExpiresAt, pin.ExpiresAt)
	s.downloadarrGrants[user.ID] = g
	s.mu.Unlock()
	// Plex authorizes the new application credential with the current account;
	// the Hub's Plex token never reaches Downloadarr or the browser.
	if err := s.Upstream.DownloadarrAuthorize(callCtx, pin, plex); err != nil {
		s.forgetDownloadarrGrant(user.ID, g)
		s.downloadarrFailure(w, user.ID, g, err)
		return
	}
	if !s.revalidateUser(w, r, key, user) {
		s.forgetDownloadarrGrant(user.ID, g)
		return
	}
	current, exists = s.currentDownloadarrGrant(key, user.ID)
	if !exists || current.Revision != g.Revision {
		problem(w, 409, "link_changed", "La connexion a été annulée. Actualisez votre accès.")
		return
	}
	jsonOut(w, 200, downloadarrLinkStatus{ExpiresAt: &g.ExpiresAt})
}

func minDownloadarrExpiry(a, b time.Time) time.Time {
	if b.Before(a) {
		return b.UTC()
	}
	return a.UTC()
}

func (s *Server) downloadarrPoll(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) || !emptyDownloadarrBody(w, r) {
		return
	}
	key, user, ok := s.user(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "La connexion utilise le parcours ouvert dans cette session.")
		return
	}
	g, ok := s.currentDownloadarrGrant(key, user.ID)
	if !s.downloadarrEnabled() || !ok || g.FlowID == "" || g.Bearer != "" {
		problem(w, 409, "link_expired", "Recommencez la connexion Downloadarr.")
		return
	}
	s.mu.Lock()
	current := s.downloadarrGrants[user.ID]
	if current.Revision != g.Revision || current.Polling || time.Since(current.LastPoll) < 2*time.Second {
		s.mu.Unlock()
		w.Header().Set("Retry-After", "2")
		problem(w, 429, "rate_limited", "Patientez avant de vérifier la connexion.")
		return
	}
	current.Polling, current.LastPoll = true, time.Now()
	s.downloadarrGrants[user.ID] = current
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if current, exists := s.downloadarrGrants[user.ID]; exists && current.Revision == g.Revision {
			current.Polling = false
			s.downloadarrGrants[user.ID] = current
		}
	}()
	ctx, cancel := downloadarrContext(r.Context(), g)
	defer cancel()
	bearer, pending, err := s.Upstream.DownloadarrAuthenticate(ctx, s.Config.DownloadarrURL, g.FlowID)
	if err != nil {
		s.forgetDownloadarrGrant(user.ID, g)
		s.downloadarrFailure(w, user.ID, g, err)
		return
	}
	if pending {
		if !s.revalidateUser(w, r, key, user) {
			return
		}
		if current, ok := s.currentDownloadarrGrant(key, user.ID); !ok || current.Revision != g.Revision {
			problem(w, 409, "link_changed", "La connexion a été annulée. Actualisez votre accès.")
			return
		}
		jsonOut(w, 200, downloadarrLinkStatus{ExpiresAt: &g.ExpiresAt})
		return
	}
	g.Bearer = bearer
	contract, err := s.Upstream.DownloadarrVerify(ctx, s.Config.DownloadarrURL, bearer, user.PlexID, s.Config.PlexServerID, s.Config.DownloadarrRevision)
	if err != nil {
		s.forgetDownloadarrGrant(user.ID, g)
		s.downloadarrFailure(w, user.ID, g, err)
		return
	}
	if !s.revalidateUser(w, r, key, user) {
		s.forgetDownloadarrGrant(user.ID, g)
		return
	}
	s.mu.Lock()
	current, exists := s.downloadarrGrants[user.ID]
	plex, plexOK := s.grants[user.ID]
	sess, sessionOK := s.sessions[key]
	if !exists || current.Revision != g.Revision || g.Context.Err() != nil || !plexOK || plex.AccountToken != g.PlexToken || !sessionOK || sess.UserID != user.ID || !time.Now().Before(sess.Expires) {
		s.mu.Unlock()
		problem(w, 409, "link_changed", "La connexion a été annulée. Recommencez depuis votre compte.")
		return
	}
	g.ExpiresAt = minDownloadarrExpiry(contract.ExpiresAt, minDownloadarrExpiry(plex.ExpiresAt, sess.Expires))
	g.FlowID, g.Polling = "", false
	s.downloadarrGrants[user.ID] = g
	s.mu.Unlock()
	jsonOut(w, 200, downloadarrLinkStatus{Linked: true, ExpiresAt: &g.ExpiresAt, QualityOptions: contract.QualityOptions})
}

func (s *Server) downloadarrRequest(w http.ResponseWriter, r *http.Request) (string, store.User, downloadarrGrant, bool) {
	key, user, ok := s.user(w, r)
	if !ok {
		return key, user, downloadarrGrant{}, false
	}
	g, linked := s.currentDownloadarrGrant(key, user.ID)
	if !s.downloadarrEnabled() {
		problem(w, 503, "not_configured", "L’intégration Downloadarr n’est pas configurée.")
		return key, user, g, false
	}
	if !linked || g.Bearer == "" {
		s.downloadarrFailure(w, user.ID, g, upstream.ErrDenied)
		return key, user, g, false
	}
	if s.limited(w, r, "downloads", 120) {
		return key, user, g, false
	}
	return key, user, g, true
}

func (s *Server) downloadarrJobs(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "Cette liste concerne seulement votre compte.")
		return
	}
	key, user, g, ok := s.downloadarrRequest(w, r)
	if !ok {
		return
	}
	if _, ok = s.verifyDownloadarr(w, r, key, user, g); !ok {
		return
	}
	ctx, cancel := downloadarrContext(r.Context(), g)
	defer cancel()
	items, err := s.Upstream.DownloadarrJobs(ctx, s.Config.DownloadarrURL, g.Bearer)
	if err != nil {
		s.downloadarrFailure(w, user.ID, g, err)
		return
	}
	contract, ok := s.verifyDownloadarr(w, r, key, user, g)
	if !ok {
		return
	}
	jsonOut(w, 200, struct {
		Items     []upstream.DownloadJob `json:"items"`
		CheckedAt time.Time              `json:"checkedAt"`
		ExpiresAt time.Time              `json:"expiresAt"`
	}{items, time.Now().UTC(), minDownloadarrExpiry(time.Now().Add(30*time.Second), minDownloadarrExpiry(g.ExpiresAt, contract.ExpiresAt))})
}

func (s *Server) downloadarrOptions(w http.ResponseWriter, r *http.Request) {
	key, user, g, ok := s.downloadarrRequest(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	for name, values := range q {
		if !slices.Contains([]string{"ratingKey", "quality", "season", "partKey"}, name) || len(values) != 1 {
			problem(w, 400, "invalid_query", "Choisissez un contenu et une qualité proposés.")
			return
		}
	}
	if q.Get("season") != "" && q.Get("season") != "false" && q.Get("season") != "true" {
		problem(w, 400, "invalid_query", "Le choix de saison est invalide.")
		return
	}
	input := upstream.DownloadInput{RatingKey: q.Get("ratingKey"), Quality: q.Get("quality"), PartKey: q.Get("partKey"), Season: q.Get("season") == "true"}
	contract, ok := s.verifyDownloadarr(w, r, key, user, g)
	if !ok {
		return
	}
	if !slices.Contains(contract.QualityOptions, input.Quality) {
		problem(w, 400, "invalid_download", "Choisissez une qualité autorisée.")
		return
	}
	ctx, cancel := downloadarrContext(r.Context(), g)
	defer cancel()
	options, err := s.Upstream.DownloadarrOptions(ctx, s.Config.DownloadarrURL, g.Bearer, input)
	if err != nil {
		s.downloadarrFailure(w, user.ID, g, err)
		return
	}
	plex, err := s.grant(ctx, user)
	if err != nil {
		s.downloadarrFailure(w, user.ID, g, upstream.ErrDenied)
		return
	}
	item, err := s.Upstream.PlexMetadata(ctx, s.Config.PlexURL, input.RatingKey, plex)
	if err != nil {
		s.downloadarrFailure(w, user.ID, g, err)
		return
	}
	if (input.Season && item.Kind != "season") || (!input.Season && item.Kind != "movie" && item.Kind != "episode") {
		problem(w, 400, "invalid_download", "Choisissez un film, un épisode ou une saison précise.")
		return
	}
	if _, ok = s.verifyDownloadarr(w, r, key, user, g); !ok {
		return
	}
	title := item.Title
	if item.Kind == "season" && item.ParentTitle != "" {
		title = item.ParentTitle + " — " + title
	}
	if item.Kind == "episode" && item.GrandparentTitle != "" {
		title = item.GrandparentTitle + " — " + title
	}
	jsonOut(w, 200, struct {
		upstream.DownloadOptions
		Title string `json:"title"`
		Type  string `json:"type"`
	}{options, title, item.Kind})
}

func (s *Server) downloadarrCreate(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) {
		return
	}
	key, user, g, ok := s.downloadarrRequest(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "Les choix doivent figurer dans la confirmation.")
		return
	}
	var input struct {
		upstream.DownloadInput
		Confirmed bool `json:"confirmed"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8193))
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err != nil || len(body) > 8192 || d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || !input.Confirmed {
		problem(w, 400, "confirmation_required", "Confirmez le contenu, la qualité et les pistes avant de préparer le fichier.")
		return
	}
	contract, ok := s.verifyDownloadarr(w, r, key, user, g)
	if !ok {
		return
	}
	if !slices.Contains(contract.QualityOptions, input.Quality) {
		problem(w, 400, "invalid_download", "Cette qualité n’est pas autorisée.")
		return
	}
	ctx, cancel := downloadarrContext(r.Context(), g)
	defer cancel()
	job, err := s.Upstream.DownloadarrCreate(ctx, s.Config.DownloadarrURL, g.Bearer, input.DownloadInput)
	if err != nil {
		s.downloadarrFailure(w, user.ID, g, err)
		return
	}
	if _, ok = s.verifyDownloadarr(w, r, key, user, g); !ok {
		return
	}
	jsonOut(w, 202, job)
}

func (s *Server) downloadarrCancel(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) || !emptyDownloadarrBody(w, r) {
		return
	}
	key, user, g, ok := s.downloadarrRequest(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "Aucun sélecteur de compte n’est accepté.")
		return
	}
	if _, ok = s.verifyDownloadarr(w, r, key, user, g); !ok {
		return
	}
	ctx, cancel := downloadarrContext(r.Context(), g)
	defer cancel()
	if err := s.Upstream.DownloadarrCancel(ctx, s.Config.DownloadarrURL, g.Bearer, r.PathValue("id")); err != nil {
		s.downloadarrFailure(w, user.ID, g, err)
		return
	}
	if !s.revalidateUser(w, r, key, user) {
		return
	}
	w.WriteHeader(204)
}

func (s *Server) downloadarrFile(w http.ResponseWriter, r *http.Request) {
	if !s.origin(w, r) || !emptyDownloadarrBody(w, r) {
		return
	}
	key, user, g, ok := s.downloadarrRequest(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 || r.Header.Get("Range") != "" {
		problem(w, 400, "invalid_download", "Ce téléchargement n’accepte ni jeton dans l’URL ni reprise partielle.")
		return
	}
	// Only this streaming route can outlive the API's ordinary 30-second write deadline.
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Time{})
	if _, ok = s.verifyDownloadarr(w, r, key, user, g); !ok {
		return
	}
	ctx, cancel := downloadarrContext(r.Context(), g)
	defer cancel()
	ticket, err := s.Upstream.DownloadarrTicket(ctx, s.Config.DownloadarrURL, g.Bearer, r.PathValue("id"))
	if err != nil {
		s.downloadarrFailure(w, user.ID, g, err)
		return
	}
	resp, err := s.Upstream.DownloadarrFile(ctx, s.Config.DownloadarrURL, r.PathValue("id"), ticket)
	if err != nil {
		s.downloadarrFailure(w, user.ID, g, err)
		return
	}
	defer resp.Body.Close()
	if _, ok = s.verifyDownloadarr(w, r, key, user, g); !ok {
		return
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || (mediaType != "video/mp4" && mediaType != "application/zip") {
		problem(w, 503, "invalid_file", "Le service n’a pas renvoyé de fichier compatible.")
		return
	}
	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	filename := params["filename"]
	if err != nil || filename == "" || len(filename) > 240 || strings.ContainsAny(filename, "/\\\r\n\x00") || path.Ext(strings.ToLower(filename)) != map[string]string{"video/mp4": ".mp4", "application/zip": ".zip"}[mediaType] {
		filename = "telechargement" + map[string]string{"video/mp4": ".mp4", "application/zip": ".zip"}[mediaType]
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if resp.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	// Unlink/logout cancels the source context. Session deadlines and changed
	// grants also stop an already-open stream, including a source waiting on bytes.
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopWatch:
				return
			case <-ctx.Done():
				_ = resp.Body.Close()
				return
			case <-ticker.C:
				if current, valid := s.currentDownloadarrGrant(key, user.ID); !valid || current.Revision != g.Revision {
					cancel()
					_ = resp.Body.Close()
					return
				}
			}
		}
	}()
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			current, valid := s.currentDownloadarrGrant(key, user.ID)
			if !valid || current.Revision != g.Revision || ctx.Err() != nil {
				panic(http.ErrAbortHandler)
			}
			_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if _, err := w.Write(buf[:n]); err != nil {
				panic(http.ErrAbortHandler)
			}
			if err := controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
				panic(http.ErrAbortHandler)
			}
		}
		if readErr == io.EOF {
			return
		}
		if readErr != nil {
			panic(http.ErrAbortHandler)
		}
	}
}
