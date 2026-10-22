package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hloiseau/le-concierge/internal/eris"
)

func (b *Bot) handleEris(w http.ResponseWriter, i Interaction, proof eris.ToolRequest) {
	options, _ := CommandOptions(i)
	if i.Type != 3 && options.ChatMessage == "" && !options.Forget {
		_ = json.NewEncoder(w).Encode(map[string]any{"type": 4, "data": Message("Mentionne @Eris, puis utilise Répondre sur mes messages pour continuer dans le salon. Utilise /eris message:… pour me parler en privé, ou /eris oublier:true pour effacer ma mémoire de nos conversations.")})
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
		var result eris.ToolReply
		owner := i.GuildID + ":" + i.Member.User.ID
		public := i.Type == 3 && i.Message.Flags&64 == 0
		if public {
			owner += ":" + i.ChannelID
		}
		if i.Type == 3 {
			result = b.erisTool(ctx, proof)
			b.Eris.RememberRequest(owner, result)
		} else {
			result = b.erisMediaReply(ctx, owner, options.ChatMessage, options.Forget, proof)
		}
		reply := Message(result.Content)
		if public {
			delete(reply, "flags")
		}
		if len(result.Buttons) > 0 {
			buttons := []map[string]any{}
			for _, button := range result.Buttons {
				buttons = append(buttons, map[string]any{"type": 2, "style": 2, "label": button.Label, "custom_id": button.ID})
			}
			reply["components"] = []map[string]any{{"type": 1, "components": buttons}}
		}
		if b.webhook(ctx, i, "PATCH", "/messages/@original", reply) {
			b.Eris.Delivered(owner, i.ID, result.Content)
		}
	}()
}

func (b *Bot) erisMediaReply(ctx context.Context, owner, message string, forget bool, proof eris.ToolRequest) eris.ToolReply {
	return b.erisContextReply(ctx, owner, message, forget, proof, "")
}
func (b *Bot) erisContextReply(ctx context.Context, owner, message string, forget bool, proof eris.ToolRequest, reference string) eris.ToolReply {
	var result eris.ToolReply
	text, err := b.Eris.ReplyWithContext(ctx, owner, message, forget, reference, func(ctx context.Context, plan eris.Plan) (eris.ToolReply, error) {
		proof.Plan = plan
		result = b.erisTool(ctx, proof)
		return result, nil
	})
	if errors.Is(err, eris.ErrBusy) {
		return eris.ToolReply{Content: "Je termine une conversation. Réessaie dans quelques secondes."}
	}
	if err != nil {
		return eris.ToolReply{Content: "Mon moteur de conversation ne répond pas pour le moment. Mes commandes restent disponibles avec /aide."}
	}
	result.Content = text
	return result
}

func (b *Bot) erisTool(ctx context.Context, input eris.ToolRequest) eris.ToolReply {
	failure := eris.ToolReply{Content: "Je ne peux pas vérifier les données du Hub pour le moment. Réessaie plus tard."}
	var interaction Interaction
	if json.Unmarshal(input.Interaction, &interaction) == nil && strings.HasPrefix(interaction.Data.CustomID, "eris:confirm:") {
		failure.Content = "Je ne peux pas confirmer l’enregistrement. Vérifie /mes-demandes ou Seerr avant de réessayer ; je ne renvoie pas automatiquement la demande."
	}
	body, _ := json.Marshal(input)
	req, err := http.NewRequestWithContext(ctx, "POST", b.Config.BotAPIURL+"/internal/eris", bytes.NewReader(body))
	if err != nil {
		return failure
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.Config.BotAPISecret)
	client := *b.Client
	client.Timeout = 22 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return failure
	}
	defer resp.Body.Close()
	var result eris.ToolReply
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(&result) != nil || result.Content == "" || !utf8.ValidString(result.Content) || utf8.RuneCountInString(result.Content) > 1500 || len(result.Buttons) > 3 {
		return failure
	}
	for _, button := range result.Buttons {
		if !ValidErisButton(button.ID) || button.Label == "" || utf8.RuneCountInString(button.Label) > 80 {
			return failure
		}
	}
	return result
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
