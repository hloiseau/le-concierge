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
	localNow := now.In(loc)
	if hour := localNow.Hour(); hour < 8 || hour >= 22 {
		return nil
	}
	quietStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 22, 0, 0, 0, loc)
	ctx, cancel := context.WithTimeout(ctx, min(45*time.Second, quietStart.Sub(localNow)))
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
		message, recipients, err := s.channelReminderMessage(ctx, removal, rule, requests, owners, ownerFailures, loc)
		if err != nil {
			deferredErr = err
		}
		if len(recipients) == 0 {
			continue
		}
		seen[removal.MediaID] = true
		nonce, claimed, err := s.Store.ClaimChannelReminder(ctx, channel, key, *removal.EstimatedAt, now)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		err = s.Discord.SendChannelValidated(ctx, s.Config.DiscordGuildID, channel, nonce, message, recipients, func() error {
			if ctx.Err() != nil {
				return &notifications.DeliveryError{Code: "source_unavailable"}
			}
			if time.Now().After(*removal.EstimatedAt) || time.Since(snapshot.UpdatedAt) > time.Minute {
				return upstream.ErrDenied
			}
			return s.revalidateChannelReminder(ctx, removal, message, recipients, loc)
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

// The same preparation runs initially and again after Discord's channel lookup.
// Empty recipients mean the current rule/request no longer permits an announcement.
func (s *Server) channelReminderMessage(ctx context.Context, removal upstream.Removal, rule upstream.RemovalRule, requests []upstream.SeerrRequestedMedia, owners map[int64]upstream.SeerrRequester, ownerFailures map[int64]error, loc *time.Location) (notifications.Message, []string, error) {
	if !rule.Active || removal.EstimatedAt == nil || removal.RuleEvaluationFailed {
		return notifications.Message{}, nil, nil
	}
	plexKey := removal.MediaID
	metadata := upstream.PlexItem{ID: removal.MediaID, Kind: removal.Kind}
	var season *int
	kind := "movie"
	if removal.Kind != "movie" {
		kind = "tv"
	}
	if removal.Kind == "season" || removal.Kind == "episode" {
		if s.Config.TautulliURL == "" {
			return notifications.Message{}, nil, nil
		}
		var err error
		metadata, season, err = s.Upstream.TautulliRemovalParent(ctx, s.Config.TautulliURL, s.Config.TautulliAPIKey, s.Config.PlexServerID, removal)
		if err != nil {
			return notifications.Message{}, nil, err
		}
		plexKey = metadata.ParentID
		if removal.Kind == "episode" {
			plexKey = metadata.GrandparentID
		}
	}
	if rule.Excludes(metadata) {
		return notifications.Message{}, nil, nil
	}
	matched := []upstream.SeerrRequestedMedia{}
	for _, request := range requests {
		if request.Type == kind && slices.Contains(request.PlexKeys, plexKey) && (season == nil || slices.Contains(request.Seasons, *season)) {
			matched = append(matched, request)
		}
	}
	if len(matched) == 0 {
		return notifications.Message{}, nil, nil
	}
	for _, request := range matched {
		if request.TMDBID != matched[0].TMDBID {
			return notifications.Message{}, nil, upstream.ErrIncomplete
		}
	}
	recipients := []string{}
	var deferredErr error
	requesterUnavailable := false
	for _, request := range matched {
		if failure := ownerFailures[request.UserID]; failure != nil {
			requesterUnavailable = requesterUnavailable || !errors.Is(failure, upstream.ErrNotFound)
			continue
		}
		owner, ok := owners[request.UserID]
		if !ok {
			var err error
			owner, err = s.Upstream.SeerrRequester(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, request.UserID)
			if err != nil {
				deferredErr = err
				ownerFailures[request.UserID] = err
				requesterUnavailable = requesterUnavailable || !errors.Is(err, upstream.ErrNotFound)
				continue
			}
			if len(owner.DiscordIDs) == 0 && owner.PlexID != "" {
				id, err := s.Store.LinkedDiscordForPlex(ctx, owner.PlexID)
				if err != nil {
					return notifications.Message{}, nil, err
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
		return notifications.Message{}, nil, deferredErr
	}
	if len(recipients) > 25 {
		return notifications.Message{}, nil, upstream.ErrIncomplete
	}
	title, err := s.Upstream.SeerrMediaTitle(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, kind, matched[0].TMDBID)
	if err != nil {
		return notifications.Message{}, nil, err
	}
	description := "vous avez demandé ce contenu sur Seerr. Départ estimé le " + removal.EstimatedAt.In(loc).Format("02/01 à 15:04") + " (Maintainerr)."
	if season != nil {
		scope := fmt.Sprintf("saison %d", *season)
		if removal.Kind == "episode" {
			if metadata.Index != nil {
				scope += fmt.Sprintf(", épisode %d", *metadata.Index)
			} else {
				scope += ", un épisode (numéro indisponible)"
			}
		}
		// Keep the removal scope outside the independently truncated title.
		description = scope + " : " + description
	}
	link := "https://app.plex.tv/desktop/#!/server/" + url.PathEscape(s.Config.PlexServerID) + "/details?key=" + url.QueryEscape("/library/metadata/"+removal.MediaID)
	sort.Strings(recipients)
	return notifications.Message{Title: title, URL: link, Description: description}, recipients, deferredErr
}

func (s *Server) revalidateChannelReminder(ctx context.Context, original upstream.Removal, message notifications.Message, recipients []string, loc *time.Location) error {
	unavailable := &notifications.DeliveryError{Code: "source_unavailable"}
	changed := &notifications.DeliveryError{Code: "source_changed"}
	if err := s.Upstream.SeerrPlexServer(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, s.Config.PlexServerID); err != nil {
		return unavailable
	}
	snapshot, err := s.Upstream.MaintainerrRemovals(ctx, s.Config.MaintainerrURL, s.Config.PlexServerID)
	if err != nil {
		return unavailable
	}
	for _, current := range snapshot.Items {
		if current.MediaID != original.MediaID || current.CollectionID != original.CollectionID || current.MembershipID != original.MembershipID {
			continue
		}
		if current.Kind != original.Kind || current.LibraryID != original.LibraryID || current.Action != original.Action || current.RuleEvaluationFailed || current.EstimatedAt == nil || !current.EstimatedAt.Equal(*original.EstimatedAt) {
			return changed
		}
		rules, err := s.Upstream.MaintainerrRules(ctx, s.Config.MaintainerrURL)
		if err != nil {
			return unavailable
		}
		requests, err := s.Upstream.SeerrRequestedMedia(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey)
		if err != nil {
			return unavailable
		}
		fresh, freshRecipients, err := s.channelReminderMessage(ctx, current, rules[current.CollectionID], requests, map[int64]upstream.SeerrRequester{}, map[int64]error{}, loc)
		if err != nil && !errors.Is(err, upstream.ErrNotFound) {
			return unavailable
		}
		if ctx.Err() != nil || !time.Now().Before(*current.EstimatedAt) || fresh != message || !slices.Equal(freshRecipients, recipients) {
			return changed
		}
		return nil
	}
	return changed
}
