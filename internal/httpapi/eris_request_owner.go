package httpapi

import (
	"context"
	"encoding/json"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) erisRequestOwner(ctx context.Context, actor store.User, target string) (store.User, string) {
	if target == "" || target == "self" || target == actor.DiscordID {
		return actor, ""
	}
	if !s.Config.Admin(actor.DiscordID) {
		return store.User{}, "admin_required_for_other_member"
	}
	owner, err := s.Store.UserByDiscord(ctx, target)
	if err != nil || owner.PlexID == "" {
		return store.User{}, "target_member_not_linked_not_sent"
	}
	return owner, ""
}

// The administrator authorizes the operation; the recipient's stable association
// identifies its owner. No personal Plex token of the recipient is used.
func (s *Server) revalidateErisRequestOwner(ctx context.Context, actor, owner store.User) error {
	current, err := s.Store.UserByDiscord(ctx, actor.DiscordID)
	if err != nil || current.ID != actor.ID || current.PlexID != actor.PlexID {
		return upstream.ErrDenied
	}
	if _, err = s.grant(ctx, current); err != nil {
		return err
	}
	if owner.ID != actor.ID && !s.Config.Admin(current.DiscordID) {
		return upstream.ErrDenied
	}
	target, err := s.Store.UserByDiscord(ctx, owner.DiscordID)
	if err != nil || target.ID != owner.ID || target.PlexID != owner.PlexID {
		return upstream.ErrDenied
	}
	return nil
}

func erisOwnedRequestResult(reply eris.ToolReply, actor, owner store.User) eris.ToolReply {
	var facts map[string]any
	if json.Unmarshal([]byte(reply.Content), &facts) != nil || facts == nil {
		return erisFailure("request_result_unverified")
	}
	facts["requestOwner"] = map[string]string{"discordId": owner.DiscordID, "name": owner.Name}
	facts["actedBy"] = map[string]string{"discordId": actor.DiscordID, "name": actor.Name}
	facts["delegated"] = owner.ID != actor.ID
	facts["requestOwnerVerified"] = facts["error"] == nil && (facts["status"] == "created" || facts["status"] == "already_created" || facts["requests"] != nil)
	return erisData(facts)
}
