package httpapi

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"time"

	"github.com/hloiseau/le-concierge/internal/notifications"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

// Channel reminders concern the original Seerr requesters. They run without
// individual Hub sessions and never reuse private watch history or DM consent.
func (s *Server) deliverChannelReminders(ctx context.Context, now time.Time) error {
	channel := s.Config.DiscordNotificationChannelID
	if channel == "" || s.Discord == nil {
		return nil
	}
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return err
	}
	if hour := now.In(loc).Hour(); hour < 8 || hour >= 22 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := s.Upstream.SeerrPlexServer(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, s.Config.PlexServerID); err != nil {
		return err
	}
	snapshot, err := s.Upstream.MaintainerrRemovals(ctx, s.Config.MaintainerrURL, s.Config.PlexServerID)
	if err != nil {
		return err
	}
	rules, err := s.Upstream.MaintainerrRules(ctx, s.Config.MaintainerrURL)
	if err != nil {
		return err
	}
	requests, err := s.Upstream.SeerrRequestedMedia(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey)
	if err != nil {
		return err
	}
	owners := map[int64]upstream.SeerrRequester{}
	ownerFailures := map[int64]error{}
	var deferredErr error
	sort.Slice(snapshot.Items, func(i, j int) bool {
		a, b := snapshot.Items[i].EstimatedAt, snapshot.Items[j].EstimatedAt
		return a != nil && (b == nil || a.Before(*b))
	})
	seen := map[string]bool{}
	for _, removal := range snapshot.Items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if seen[removal.MediaID] {
			continue
		}
		if removal.EstimatedAt == nil || removal.RuleEvaluationFailed || !removal.EstimatedAt.After(now) || removal.EstimatedAt.After(now.Add(7*24*time.Hour)) {
			continue
		}
		rule, exists := rules[removal.CollectionID]
		if !exists || !rule.Active {
			continue
		}
		milestone := 7
		if !removal.EstimatedAt.After(now.Add(2 * 24 * time.Hour)) {
			milestone = 2
		}
		key := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d:%d:%d:%d:%d", s.Config.PlexServerID, removal.MediaID, removal.CollectionID, removal.MembershipID, removal.Action, removal.EstimatedAt.UnixMilli(), milestone))))
		pending, err := s.Store.ChannelReminderPending(ctx, channel, key, now)
		if err != nil {
			return err
		}
		if !pending {
			seen[removal.MediaID] = true
			continue
		}
		// Parent collection keys match Seerr's Plex links exactly; season/episode
		// membership is resolved through Tautulli when that source is configured.
		plexKey := removal.MediaID
		metadata := upstream.PlexItem{ID: removal.MediaID, Kind: removal.Kind}
		var season *int
		kind := "movie"
		if removal.Kind != "movie" {
			kind = "tv"
		}
		if removal.Kind == "season" || removal.Kind == "episode" {
			if s.Config.TautulliURL == "" {
				continue
			}
			metadata, season, err = s.Upstream.TautulliRemovalParent(ctx, s.Config.TautulliURL, s.Config.TautulliAPIKey, s.Config.PlexServerID, removal)
			if err != nil {
				deferredErr = err
				continue
			}
			plexKey = metadata.ParentID
			if removal.Kind == "episode" {
				plexKey = metadata.GrandparentID
			}
		}
		if rule.Excludes(metadata) {
			continue
		}
		matched := []upstream.SeerrRequestedMedia{}
		for _, request := range requests {
			if request.Type == kind && slices.Contains(request.PlexKeys, plexKey) && (season == nil || slices.Contains(request.Seasons, *season)) {
				matched = append(matched, request)
			}
		}
		if len(matched) == 0 {
			continue
		}
		for _, request := range matched {
			if request.TMDBID != matched[0].TMDBID {
				return upstream.ErrIncomplete
			}
		}
		seen[removal.MediaID] = true
		recipients := []string{}
		requesterUnavailable := false
		for _, request := range matched {
			if failure := ownerFailures[request.UserID]; failure != nil {
				requesterUnavailable = requesterUnavailable || !errors.Is(failure, upstream.ErrNotFound)
				continue
			}
			owner, ok := owners[request.UserID]
			if !ok {
				owner, err = s.Upstream.SeerrRequester(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, request.UserID)
				if err != nil {
					deferredErr = err
					ownerFailures[request.UserID] = err
					requesterUnavailable = requesterUnavailable || !errors.Is(err, upstream.ErrNotFound)
					continue
				}
				if len(owner.DiscordIDs) == 0 && owner.PlexID != "" {
					id, e := s.Store.LinkedDiscordForPlex(ctx, owner.PlexID)
					if e != nil {
						return e
					}
					if id != "" {
						owner.DiscordIDs = []string{id}
					}
				}
				owners[request.UserID] = owner
			}
			for _, id := range owner.DiscordIDs {
				if !slices.Contains(recipients, id) {
					recipients = append(recipients, id)
				}
			}
		}
		if len(recipients) == 0 || requesterUnavailable {
			continue
		}
		if len(recipients) > 25 {
			return upstream.ErrIncomplete
		}
		title, err := s.Upstream.SeerrMediaTitle(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, kind, matched[0].TMDBID)
		if err != nil {
			deferredErr = err
			continue
		}
		if season != nil {
			title += fmt.Sprintf(" — saison %d", *season)
			if removal.Kind == "episode" {
				if metadata.Index != nil {
					title += fmt.Sprintf(", épisode %d", *metadata.Index)
				} else {
					title += ", un épisode (numéro indisponible)"
				}
			}
		}
		nonce, claimed, err := s.Store.ClaimChannelReminder(ctx, channel, key, *removal.EstimatedAt, now)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		link := "https://app.plex.tv/desktop/#!/server/" + url.PathEscape(s.Config.PlexServerID) + "/details?key=" + url.QueryEscape("/library/metadata/"+removal.MediaID)
		err = s.Discord.SendChannelValidated(ctx, s.Config.DiscordGuildID, channel, nonce, notifications.Message{Title: title, URL: link, Description: "vous avez demandé ce contenu sur Seerr. Départ estimé le " + removal.EstimatedAt.In(loc).Format("02/01 à 15:04") + " (Maintainerr)."}, recipients, func() error {
			if time.Now().After(*removal.EstimatedAt) || time.Since(snapshot.UpdatedAt) > time.Minute || ctx.Err() != nil {
				return upstream.ErrDenied
			}
			return nil
		})
		if err == nil {
			if err = s.Store.FinishChannelReminder(ctx, channel, key, time.Now(), "", 0, false); err != nil {
				return err
			}
			continue
		}
		var failure *notifications.DeliveryError
		if errors.As(err, &failure) {
			if e := s.Store.FinishChannelReminder(ctx, channel, key, time.Now(), failure.Code, failure.RetryAfter, failure.Permanent); e != nil {
				return e
			}
		} else {
			_ = s.Store.FinishChannelReminder(ctx, channel, key, time.Now(), "transport_unavailable", time.Minute, false)
		}
		return err
	}
	if err := s.Store.PruneChannelReminders(ctx, now); err != nil {
		return err
	}
	return deferredErr
}
