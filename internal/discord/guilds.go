package discord

const erisChatHelp = "Mentionne @Eris dans un salon, puis utilise Répondre sur ses messages pour continuer.\n/eris message:… — Discuter en privé.\n/eris oublier:true — Effacer tes conversations et souvenirs sur ce serveur.\n/eris-salon — Régler la participation spontanée dans ce salon (administrateurs).\nSur ce serveur, Eris est disponible uniquement pour discuter."

func chatCommand(command string) bool {
	return command == "eris" || command == "eris-salon" || command == "aide"
}

func (b *Bot) allowedInteraction(i Interaction, command string) bool {
	if !b.Config.DiscordChatAllowed(i.GuildID) {
		return false
	}
	// Reject all components, including old media confirmation buttons.
	return i.GuildID == b.Config.DiscordGuildID || (i.Type == 2 && chatCommand(command))
}
