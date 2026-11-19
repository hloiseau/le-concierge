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

type erisMediaRequest struct {
	UserID    string
	SeerrID   int64
	Candidate upstream.RequestCandidate
	Seasons   []int
}

func (s *Server) submitErisRequest(ctx context.Context, p erisMediaRequest) eris.ToolReply {
	base, secret := s.Config.SeerrURL, s.Config.SeerrAPIKey
	seasons := slices.Clone(p.Seasons)
	slices.Sort(seasons)
	key := fmt.Sprintf("%x", sha256.Sum256(fmt.Appendf(nil, "%q:%q:%d:%s:%d:%v:standard", base, p.UserID, p.SeerrID, p.Candidate.Kind, p.Candidate.ID, seasons)))
	reserved, previousID, err := s.Store.ReserveSeerrAttempt(ctx, key)
	if err != nil {
		return erisFailure("request_history_unavailable_not_sent")
	}
	receipt := func(status string, id int64) eris.ToolReply {
		return erisData(map[string]any{"status": status, "requestId": id, "media": p.Candidate, "seasons": seasons, "plexAvailability": "not_checked", "arrivalGuaranteed": false, "notificationCreated": false})
	}
	if !reserved {
		if previousID != 0 {
			return receipt("already_created", previousID)
		}
		page, readErr := s.Upstream.Requests(ctx, base, secret, p.SeerrID, 0)
		if readErr == nil {
			for _, item := range page.Items {
				found := slices.Clone(item.Seasons)
				slices.Sort(found)
				if item.ID > 0 && item.Type == p.Candidate.Kind && item.MediaID == p.Candidate.ID && item.Is4k != nil && !*item.Is4k && slices.Equal(seasons, found) {
					return receipt("already_created", item.ID)
				}
			}
		}
		return receipt("outcome_unknown_do_not_retry", 0)
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
		return receipt("created", requestID)
	case errors.Is(err, upstream.ErrDenied):
		if err := s.Store.ReleaseSeerrAttempt(recordCtx, key); err != nil {
			slog.Warn("Eris Seerr attempt release failed")
		}
		return receipt("denied", 0)
	case errors.Is(err, upstream.ErrRequestConflict):
		return receipt("already_requested_or_no_new_seasons", 0)
	default:
		return receipt("outcome_unknown_do_not_retry", 0)
	}
}
