package httpapi

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) submitErisRequest(ctx context.Context, p erisPending) eris.ToolReply {
	base, secret := s.Config.SeerrURL, s.Config.SeerrAPIKey
	seasons := slices.Clone(p.Seasons)
	slices.Sort(seasons)
	key := fmt.Sprintf("%x", sha256.Sum256(fmt.Appendf(nil, "%q:%q:%d:%s:%d:%v:standard", base, p.UserID, p.SeerrID, p.Candidate.Kind, p.Candidate.ID, seasons)))
	reserved, previousID, err := s.Store.ReserveSeerrAttempt(ctx, key)
	if err != nil {
		return eris.ToolReply{Content: "Je n’ai rien envoyé à Seerr. Je ne peux pas vérifier l’historique des demandes pour le moment."}
	}
	label := candidateLabel(p.Candidate, seasons)
	if !reserved {
		if previousID != 0 {
			return eris.ToolReply{Content: fmt.Sprintf("Seerr avait enregistré la demande #%d pour %s. Vérifie son état avec /mes-demandes.", previousID, label)}
		}
		page, readErr := s.Upstream.Requests(ctx, base, secret, p.SeerrID, 0)
		if readErr == nil {
			for _, item := range page.Items {
				found := slices.Clone(item.Seasons)
				slices.Sort(found)
				if item.ID > 0 && item.Type == p.Candidate.Kind && item.MediaID == p.Candidate.ID && item.Is4k != nil && !*item.Is4k && slices.Equal(seasons, found) {
					return eris.ToolReply{Content: fmt.Sprintf("J’ai retrouvé la demande #%d pour %s dans Seerr. Tu peux voir son état avec /mes-demandes.", item.ID, label)}
				}
			}
		}
		return eris.ToolReply{Content: "Une demande identique a déjà été tentée. Seerr n’a pas encore permis de confirmer son résultat. Vérifie /mes-demandes ou Seerr."}
	}

	requestID, err := s.Upstream.CreateMediaRequest(ctx, base, secret, p.SeerrID, p.Candidate, seasons)
	// Finalize even if the original HTTP connection was closed after the write.
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	switch {
	case err == nil:
		if err := s.Store.ConfirmSeerrAttempt(recordCtx, key, requestID); err != nil {
			slog.Warn("Eris Seerr attempt receipt write failed")
		}
		return eris.ToolReply{Content: fmt.Sprintf("Demande #%d créée : %s. Tu peux suivre son état avec /mes-demandes.", requestID, label)}
	case errors.Is(err, upstream.ErrDenied):
		if err := s.Store.ReleaseSeerrAttempt(recordCtx, key); err != nil {
			slog.Warn("Eris Seerr attempt release failed")
		}
		return eris.ToolReply{Content: "Seerr a refusé la demande : vérifie tes droits ou ton quota dans le Hub."}
	case errors.Is(err, upstream.ErrRequestConflict):
		return eris.ToolReply{Content: "Ce contenu est déjà demandé ou aucune saison supplémentaire n’est à demander. Vérifie /mes-demandes ou Seerr."}
	default:
		return eris.ToolReply{Content: "Je ne peux pas confirmer l’enregistrement. Vérifie /mes-demandes ou Seerr ; je ne renvoie pas la demande."}
	}
}
