package discord

import (
	"github.com/bwmarrin/discordgo"
	"strings"
	"testing"
)

func TestQuotedContextIsVerifiedEvenWithExplicitMention(t *testing.T) {
	for _, mode := range []string{"valid", "private", "wrong author", "wrong channel", "wrong reference", "webhook", "too long", "deleted"} {
		m := mentionFixture().Message
		m.Type = discordgo.MessageTypeReply
		m.MessageReference = &discordgo.MessageReference{MessageID: "88888", ChannelID: m.ChannelID, GuildID: m.GuildID}
		m.ReferencedMessage = &discordgo.Message{ID: "88888", ChannelID: m.ChannelID, Author: &discordgo.User{ID: "12345", Bot: true}, Content: "Coherence est disponible."}
		switch mode {
		case "private":
			m.ReferencedMessage.Flags = 64
		case "wrong author":
			m.ReferencedMessage.Author.ID = "66666"
		case "wrong channel":
			m.ReferencedMessage.ChannelID = "44444"
		case "wrong reference":
			m.MessageReference.MessageID = "77777"
		case "webhook":
			m.ReferencedMessage.WebhookID = "55555"
		case "too long":
			m.ReferencedMessage.Content = strings.Repeat("a", 1501)
		case "deleted":
			m.ReferencedMessage = nil
		}
		text := erisReference(m, "12345")
		if (text != "") != (mode == "valid") {
			t.Fatal(mode, text)
		}
	}
}
