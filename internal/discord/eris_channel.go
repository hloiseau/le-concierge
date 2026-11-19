package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
)

func (b *Bot) handleErisChannel(w http.ResponseWriter, i Interaction) {
	permissions, err := strconv.ParseUint(i.Member.Permissions, 10, 64)
	if i.Type != 2 || err != nil || permissions&8 == 0 || !config.Snowflake(i.ChannelID) {
		http.Error(w, "ERIS_ADMIN_REQUIRED", http.StatusForbidden)
		return
	}
	if !b.reserveEris() {
		_ = json.NewEncoder(w).Encode(map[string]any{"type": 4, "data": Message("⚠️ `ERIS_BUSY`")})
		return
	}
	w.WriteHeader(http.StatusAccepted)
	b.workers.Add(1)
	go func() {
		defer b.workers.Done()
		defer func() { b.mu.Lock(); b.erisWorkers--; b.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		if !b.acknowledge(ctx, i) {
			return
		}
		options, _ := CommandOptions(i)
		facts := b.erisChannelOutcome(i, options.Participation)
		text, err := b.Eris.Explain(ctx, options.Participation, facts)
		if err != nil {
			text = "⚠️ `ERIS_UNAVAILABLE`"
		}
		b.webhookReply(ctx, i, "PATCH", "/messages/@original", Message(text))
	}()
}

// Read back persisted state before asking the model to describe the result.
func (b *Bot) erisChannelOutcome(i Interaction, action string) map[string]any {
	facts := map[string]any{"operation": "channel_participation"}
	if b.Eris.Memory == nil || !b.Eris.V2 || !b.Config.DiscordMessageContent {
		facts["status"] = "not_configured_not_changed"
		return facts
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if action == "activer" || action == "desactiver" {
		if err := b.Eris.Memory.SetParticipation(i.GuildID, i.ChannelID, action == "activer"); err != nil {
			facts["status"] = "write_unverified"
			return facts
		}
		delete(b.erisAmbient, i.ChannelID)
	}
	enabled, err := b.Eris.Memory.Participation(i.GuildID, i.ChannelID)
	if err != nil || action == "activer" && !enabled || action == "desactiver" && enabled {
		facts["status"] = "read_unverified"
		return facts
	}
	facts["status"], facts["enabled"] = "verified", enabled
	facts["minimumIntervalMinutes"], facts["addressedRepliesEnabled"] = 5, true
	return facts
}
