package discord

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/hloiseau/le-concierge/internal/config"
)

func (b *Bot) handleErisChannel(w http.ResponseWriter, i Interaction) {
	reply := func(text string) { _ = json.NewEncoder(w).Encode(map[string]any{"type": 4, "data": Message(text)}) }
	permissions, err := strconv.ParseUint(i.Member.Permissions, 10, 64)
	if i.Type != 2 || err != nil || permissions&8 == 0 || !config.Snowflake(i.ChannelID) {
		reply("Ce réglage est réservé aux administrateurs, depuis le salon concerné.")
		return
	}
	if b.Eris.Memory == nil || !b.Eris.V2 || !b.Config.DiscordMessageContent {
		reply("La participation nécessite Eris v2, la mémoire persistante et l'accès au contenu des messages. Le réglage n'a pas été modifié.")
		return
	}
	options, _ := CommandOptions(i)
	b.mu.Lock()
	defer b.mu.Unlock()
	if options.Participation == "activer" || options.Participation == "desactiver" {
		if err := b.Eris.Memory.SetParticipation(i.GuildID, i.ChannelID, options.Participation == "activer"); err != nil {
			reply("Impossible d'enregistrer le réglage. Réessaie plus tard.")
			return
		}
		delete(b.erisAmbient, i.ChannelID)
	}
	enabled, err := b.Eris.Memory.Participation(i.GuildID, i.ChannelID)
	if err != nil {
		reply("Impossible de lire le réglage. Réessaie plus tard.")
	} else if enabled {
		reply("Participation spontanée activée dans ce salon, en mode économe : au plus une intervention toutes les cinq minutes. Je continue aussi à répondre quand on me parle.")
	} else {
		reply("Participation spontanée désactivée dans ce salon. Je réponds toujours quand on me parle clairement.")
	}
}
