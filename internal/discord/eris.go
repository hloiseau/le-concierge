package discord

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/hloiseau/le-concierge/internal/eris"
)

func (b *Bot) handleEris(w http.ResponseWriter, i Interaction) {
	options, _ := CommandOptions(i)
	if options.ChatMessage == "" && !options.Forget {
		_ = json.NewEncoder(w).Encode(map[string]any{"type": 4, "data": Message("Mentionne @Eris avec ton message pour discuter dans le salon. Utilise /eris message:… pour me parler en privé, ou /eris oublier:true pour effacer ma mémoire de nos conversations.")})
		return
	}
	// Separate capacity keeps a slow model from occupying the media workers.
	if !b.reserveEris() {
		_ = json.NewEncoder(w).Encode(map[string]any{"type": 4, "data": Message("Je termine une conversation. Réessaie dans un instant.")})
		return
	}
	w.WriteHeader(http.StatusAccepted)
	b.workers.Add(1)
	go func() {
		defer b.workers.Done()
		defer func() { b.mu.Lock(); b.erisWorkers--; b.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if !b.acknowledge(ctx, i) {
			return
		}
		reply := b.erisReply(ctx, i.GuildID+":"+i.Member.User.ID, options.ChatMessage, options.Forget)
		b.webhook(ctx, i, "PATCH", "/messages/@original", Message(reply))
	}()
}

func (b *Bot) erisReply(ctx context.Context, owner, message string, forget bool) string {
	reply, err := b.Eris.Reply(ctx, owner, message, forget)
	if errors.Is(err, eris.ErrBusy) {
		return "Je termine une conversation. Réessaie dans quelques secondes."
	}
	if err != nil {
		return "Mon moteur de conversation ne répond pas pour le moment. Mes commandes restent disponibles avec /aide."
	}
	return reply
}

func (b *Bot) reserveEris() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.erisWorkers >= 2 {
		return false
	}
	b.erisWorkers++
	return true
}
