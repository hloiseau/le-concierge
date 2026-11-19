package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
	"github.com/hloiseau/le-concierge/internal/websearch"
)

type ouicheurGrant struct {
	Session             upstream.OuicheurSession
	PlexToken, Revision string
	Expires             time.Time
}

func (s *Server) clearOuicheurGrantLocked(id string) { delete(s.ouicheurGrants, id) }
func (s *Server) currentOuicheurGrant(id string) (ouicheurGrant, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.ouicheurGrants[id]
	p, linked := s.grants[id]
	if !ok || !linked || !p.Active(time.Now()) || p.AccountToken != g.PlexToken || !time.Now().Before(g.Expires) {
		s.clearOuicheurGrantLocked(id)
		return ouicheurGrant{}, false
	}
	if g.Session.Cookie == "" {
		return ouicheurGrant{}, false
	}
	return g, true
}
func (s *Server) ouicheurLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && !s.origin(w, r) {
		return
	}
	key, user, ok := s.user(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		problem(w, 400, "invalid_query", "Cette connexion utilise votre compte personnel.")
		return
	}
	if r.Method == http.MethodDelete {
		s.mu.Lock()
		s.clearOuicheurGrantLocked(user.ID)
		s.mu.Unlock()
		jsonOut(w, 200, bookLinkStatus{})
		return
	}
	if r.Method == http.MethodGet {
		g, linked := s.currentOuicheurGrant(user.ID)
		status := bookLinkStatus{Linked: linked}
		if linked {
			status.ExpiresAt = &g.Expires
		}
		jsonOut(w, 200, status)
		return
	}
	if s.Config.OuicheurURL == "" {
		problem(w, 503, "not_configured", "Ouicheur n’est pas connecté au Concierge.")
		return
	}
	var input struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	d := json.NewDecoder(io.LimitReader(r.Body, 4097))
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || strings.TrimSpace(input.Login) != input.Login || len(input.Login) > 40 || len(input.Password) < 1 || len(input.Password) > 256 || strings.ContainsAny(input.Login, "\r\n\x00") {
		problem(w, 400, "invalid_body", "Indiquez vos identifiants personnels Ouicheur.")
		return
	}
	if s.limited(w, r, "ouicheur-link", 5) {
		return
	}
	p, err := s.grant(r.Context(), user)
	if err != nil {
		problem(w, 403, "plex_required", "Reconnectez-vous avec Plex.")
		return
	}
	// Reserve before login so unlink/logout during the remote call wins.
	pending := ouicheurGrant{PlexToken: p.AccountToken, Revision: token(), Expires: p.ExpiresAt}
	s.mu.Lock()
	if s.ouicheurGrants == nil {
		s.ouicheurGrants = map[string]ouicheurGrant{}
	}
	s.ouicheurGrants[user.ID] = pending
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	remote, err := s.Upstream.OuicheurLogin(ctx, s.Config.OuicheurURL, input.Login, input.Password)
	if err != nil || !s.revalidateUser(w, r, key, user) {
		s.mu.Lock()
		if s.ouicheurGrants[user.ID].Revision == pending.Revision {
			s.clearOuicheurGrantLocked(user.ID)
		}
		s.mu.Unlock()
		if err != nil {
			s.ouicheurFailure(w, err)
		}
		return
	}
	s.mu.Lock()
	current, exists := s.ouicheurGrants[user.ID]
	plex, linked := s.grants[user.ID]
	sess, signed := s.sessions[key]
	if !exists || current.Revision != pending.Revision || !linked || plex.AccountToken != p.AccountToken || !plex.Active(time.Now()) || !signed || sess.UserID != user.ID || !time.Now().Before(sess.Expires) {
		s.mu.Unlock()
		problem(w, 409, "link_changed", "La connexion a changé. Réessayez depuis Mon compte.")
		return
	}
	pending.Session = remote
	if remote.Expires.Before(pending.Expires) {
		pending.Expires = remote.Expires
	}
	s.ouicheurGrants[user.ID] = pending
	s.mu.Unlock()
	jsonOut(w, 200, bookLinkStatus{Linked: true, ExpiresAt: &pending.Expires})
}
func (s *Server) ouicheurFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, upstream.ErrDenied) {
		problem(w, 403, "ouicheur_link_required", "Connexion Ouicheur refusée ou expirée. Reliez votre compte personnel.")
		return
	}
	problem(w, 503, "ouicheur_unavailable", "Ouicheur est indisponible ou son contrat a changé.")
}
func (s *Server) erisGifts(ctx context.Context, u store.User, p eris.Plan, private bool) eris.ToolReply {
	if !private {
		return erisFailure("private_conversation_required")
	}
	if s.Config.OuicheurURL == "" {
		return erisFailure("ouicheur_not_configured")
	}
	g, ok := s.currentOuicheurGrant(u.ID)
	if !ok || g.Session.Cookie == "" {
		return erisData(map[string]any{"error": "ouicheur_link_required", "accountURL": s.Config.PublicOrigin + "/compte"})
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	lists, err := s.Upstream.OuicheurLists(ctx, s.Config.OuicheurURL, g.Session)
	if err != nil {
		return s.erisGiftFailure(u.ID, g, err)
	}
	var selected []upstream.GiftList
	for _, list := range lists {
		if strings.EqualFold(list.Name, p.Query) || list.ID == p.Query {
			selected = append(selected, list)
		}
	}
	if len(selected) == 0 && p.Query != "" {
		for _, list := range lists {
			if strings.Contains(strings.ToLower(list.Name), strings.ToLower(p.Query)) {
				selected = append(selected, list)
			}
		}
	}
	var result eris.ToolReply
	if p.Query == "" || len(selected) != 1 {
		candidates := []map[string]any{}
		for i, list := range lists {
			if i == 8 {
				break
			}
			candidates = append(candidates, map[string]any{"id": list.ID, "name": list.Name})
		}
		result = erisData(map[string]any{"status": "identify_list", "lists": candidates, "partial": len(lists) > 8})
	} else {
		list := selected[0]
		ideas, more, err := s.Upstream.OuicheurIdeas(ctx, s.Config.OuicheurURL, g.Session, list.ID, p.Text)
		if err != nil {
			return s.erisGiftFailure(u.ID, g, err)
		}
		projected := []map[string]any{}
		for i, idea := range ideas {
			if i == 5 {
				more = true
				break
			}
			item := map[string]any{"title": string([]rune(idea.Title)[:min(80, len([]rune(idea.Title)))]), "description": string([]rune(idea.Description)[:min(120, len([]rune(idea.Description)))])}
			if idea.Target > 0 && len(idea.Currency) == 3 {
				item["declaredBudget"] = idea.Target / 100
				item["currency"] = idea.Currency
			}
			projected = append(projected, item)
		}
		raw, _ := json.Marshal(map[string]any{"list": list.Name, "ideas": projected, "partial": more, "reservationStatusExposed": false, "merchantPriceVerified": false})
		evidence := websearch.Result{RetrievedAt: time.Now().UTC().Format(time.RFC3339), Sources: []websearch.Source{{Title: list.Name, URL: s.Config.OuicheurURL + "/lists/" + list.ID, Text: string(raw), Kind: "metadata"}}}
		if !evidence.Validate() {
			return erisFailure("gift_evidence_invalid")
		}
		result = erisData(map[string]any{"status": "evidence_found", "scope": "gift_descriptions_only"})
		result.Web = &evidence
	}
	current, ok := s.currentOuicheurGrant(u.ID)
	if !ok || current.Revision != g.Revision {
		return erisFailure("ouicheur_access_changed")
	}
	return result
}

func (s *Server) erisGiftFailure(id string, g ouicheurGrant, err error) eris.ToolReply {
	if errors.Is(err, upstream.ErrDenied) {
		s.mu.Lock()
		if s.ouicheurGrants[id].Revision == g.Revision {
			s.clearOuicheurGrantLocked(id)
		}
		s.mu.Unlock()
		return erisFailure("ouicheur_link_expired")
	}
	return erisFailure("ouicheur_unavailable_nothing_reserved")
}
