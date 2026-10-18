package discord

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
)

func (b *Bot) interestRevision() string {
	raw, _ := json.Marshal(b.Config.DiscordInterests)
	hash := sha256.Sum256(append([]byte(b.Config.DiscordGuildID+":"+b.Config.DiscordInterestsChannelID+":"), raw...))
	return hex.EncodeToString(hash[:6])
}

// The role IDs come only from configuration; the signed caller can only choose for themselves.
func (b *Bot) interestAction(i Interaction) (string, error) {
	invalid := errors.New("invalid interests interaction")
	if i.Type == 2 && i.Data.Name == "salons" && len(i.Data.Options) == 0 && len(i.Data.Values) == 0 && i.Data.CustomID == "" {
		return "open", nil
	}
	if len(b.Config.DiscordInterests) == 0 || i.Type != 3 || len(i.Data.Options) != 0 || i.Message.Author.ID != b.Config.DiscordClientID || !config.Snowflake(i.Message.ID) {
		return "", invalid
	}
	if i.Data.CustomID == "interests:open" && i.Data.ComponentType == 2 && i.Message.Flags&64 == 0 && i.ChannelID == b.Config.DiscordInterestsChannelID && len(i.Data.Values) == 0 {
		return "open", nil
	}
	parts := strings.Split(i.Data.CustomID, ":")
	if len(parts) != 4 || parts[0] != "interests" || parts[2] != i.Member.User.ID || parts[3] != b.interestRevision() || i.Message.Flags&64 == 0 {
		return "", invalid
	}
	if parts[1] == "clear" && i.Data.ComponentType == 2 && len(i.Data.Values) == 0 {
		return "clear", nil
	}
	if parts[1] != "set" || i.Data.ComponentType != 3 || len(i.Data.Values) == 0 || len(i.Data.Values) > len(b.Config.DiscordInterests) {
		return "", invalid
	}
	seen := map[string]bool{}
	for _, key := range i.Data.Values {
		if seen[key] || !slices.ContainsFunc(b.Config.DiscordInterests, func(item config.DiscordInterest) bool { return item.Key == key }) {
			return "", invalid
		}
		seen[key] = true
	}
	return "set", nil
}

func (b *Bot) handleInterests(w http.ResponseWriter, i Interaction) {
	inline := func(content string) {
		_ = json.NewEncoder(w).Encode(map[string]any{"type": 4, "data": Message(content)})
	}
	if len(b.Config.DiscordInterests) == 0 {
		inline("Les salons thématiques ne sont pas encore configurés sur ce serveur.")
		return
	}
	b.mu.Lock()
	busy := b.interestBusy[i.Member.User.ID]
	cooling := time.Now().Before(b.interestRetryAt)
	if !busy && !cooling {
		b.interestBusy[i.Member.User.ID] = true
	}
	b.mu.Unlock()
	if busy || cooling {
		inline("Une mise à jour est déjà en cours ou Discord demande une pause. Réessayez dans un instant.")
		return
	}
	release := func() { b.mu.Lock(); delete(b.interestBusy, i.Member.User.ID); b.mu.Unlock() }
	select {
	case b.slots <- struct{}{}:
	default:
		release()
		inline("Le Concierge est occupé. Réessayez dans un instant.")
		return
	}
	w.WriteHeader(http.StatusAccepted)
	b.workers.Add(1)
	go func() {
		defer b.workers.Done()
		defer func() { <-b.slots; release() }()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if !b.acknowledge(ctx, i) {
			return
		}
		client := interestRoleClient{HTTP: b.Client, Token: b.Config.DiscordBotToken, GuildID: b.Config.DiscordGuildID, BotID: b.Config.DiscordClientID, Interests: b.Config.DiscordInterests}
		action, _ := b.interestAction(i)
		var roles []string
		var err error
		content := "Choisissez tous les salons que vous voulez rejoindre. Les choix cochés sont vos inscriptions actuelles ; la sélection s’applique immédiatement."
		if action == "open" {
			roles, err = client.Read(ctx, i.Member.User.ID)
		} else {
			selected := i.Data.Values
			if action == "clear" {
				selected = []string{}
			}
			roles, err = client.Apply(ctx, i.Member.User.ID, selected)
			content = "Vos choix sont enregistrés. Vous pouvez les modifier à tout moment."
		}
		if err != nil {
			var rate *interestRoleRateLimit
			if errors.As(err, &rate) {
				b.mu.Lock()
				until := time.Now().Add(rate.RetryAfter)
				if until.After(b.interestRetryAt) {
					b.interestRetryAt = until
				}
				b.mu.Unlock()
			}
			content = "La mise à jour n’a pas pu être entièrement confirmée. Les choix affichés ont été relus sur Discord ; vérifiez-les avant de réessayer."
			if roles == nil {
				content = "Vos salons ne peuvent pas être vérifiés pour le moment. Réessayez avec /salons ; si cela persiste, prévenez l’administrateur."
			}
		}
		b.webhook(ctx, i, "PATCH", "/messages/@original", b.interestMessage(i.Member.User.ID, roles, content))
	}()
}

func (b *Bot) interestMessage(user string, roles []string, content string) map[string]any {
	reply := Message(content)
	reply["components"] = []map[string]any{}
	if roles == nil {
		return reply
	}
	options := []map[string]any{}
	for _, item := range b.Config.DiscordInterests {
		options = append(options, map[string]any{"label": item.Label, "value": item.Key, "default": slices.Contains(roles, item.RoleID)})
	}
	suffix := user + ":" + b.interestRevision()
	reply["components"] = []map[string]any{
		{"type": 1, "components": []map[string]any{{"type": 3, "custom_id": "interests:set:" + suffix, "placeholder": "Choisissez vos centres d’intérêt", "min_values": 1, "max_values": len(options), "options": options}}},
		{"type": 1, "components": []map[string]any{{"type": 2, "style": 2, "custom_id": "interests:clear:" + suffix, "label": "Quitter tous ces salons"}}},
	}
	return reply
}
