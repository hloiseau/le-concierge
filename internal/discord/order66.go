package discord

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/eris"
)

var order66Prefix = regexp.MustCompile(`(?i)^ordre\s*66(?:\b|$)`)

func order66Invocation(text, botID string) bool {
	text = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "<@"+botID+">", ""), "<@!"+botID+">", ""))
	text = strings.TrimSpace(erisVocative.ReplaceAllString(text, "${1}"))
	return order66Prefix.MatchString(text)
}

func roleplayTurn(m *discordgo.Message, channel, bot string) (eris.RoleplayTurn, bool) {
	if m == nil || m.ChannelID != channel || !config.Snowflake(m.ID) || m.Author == nil || !config.Snowflake(m.Author.ID) || m.Author.Bot || m.Author.ID == bot || m.WebhookID != "" || m.Flags&64 != 0 || (m.Type != discordgo.MessageTypeDefault && m.Type != discordgo.MessageTypeReply) || time.Since(m.Timestamp) > 10*time.Minute || time.Until(m.Timestamp) > 30*time.Second {
		return eris.RoleplayTurn{}, false
	}
	turn := eris.RoleplayTurn{Author: discordSpeaker(m.Author.ID, "", m.Author.GlobalName, m.Author.Username), Text: m.Content}
	for _, u := range m.Mentions {
		if u != nil && config.Snowflake(u.ID) && !u.Bot && u.ID != bot && len(turn.Mentions) < 8 {
			turn.Mentions = append(turn.Mentions, discordSpeaker(u.ID, "", u.GlobalName, u.Username))
		}
	}
	ref, r := m.MessageReference, m.ReferencedMessage
	if r != nil && ref != nil && config.Snowflake(ref.MessageID) && ref.MessageID == r.ID && ref.ChannelID == channel && (ref.GuildID == "" || ref.GuildID == m.GuildID) && r.ChannelID == channel && r.Author != nil && config.Snowflake(r.Author.ID) && !r.Author.Bot && r.Author.ID != bot && r.WebhookID == "" && r.Flags&64 == 0 {
		speaker := discordSpeaker(r.Author.ID, "", r.Author.GlobalName, r.Author.Username)
		turn.ReplyTo = &speaker
	}
	return turn, true
}

func (b *Bot) erisOrder66(ctx context.Context, session *discordgo.Session, current *discordgo.Message) string {
	turn, ok := roleplayTurn(current, current.ChannelID, b.Config.DiscordClientID)
	if !ok {
		return "⚠️ `ERIS_UNAVAILABLE`"
	}
	turns := []eris.RoleplayTurn{}
	// A direct mention/reply already names the interlocutor; otherwise read a
	// small public window, including the invoker's preceding human exchange.
	if len(turn.Mentions) == 0 && turn.ReplyTo == nil {
		messages, err := session.ChannelMessages(current.ChannelID, ErisContextLimit, current.ID, "", "", discordgo.WithContext(ctx))
		if err == nil {
			for i := len(messages) - 1; i >= 0; i-- {
				if value, valid := roleplayTurn(messages[i], current.ChannelID, b.Config.DiscordClientID); valid {
					turns = append(turns, value)
				}
			}
		}
	}
	turns = append(turns, turn)
	text, err := b.Eris.Order66(ctx, turn.Author, turns)
	if err != nil {
		return "⚠️ `ERIS_UNAVAILABLE`"
	}
	return text
}
