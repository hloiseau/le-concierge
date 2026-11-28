package discord

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/eris"
)

func explicitErisMention(m *discordgo.Message, botID string) bool {
	if !strings.Contains(m.Content, "<@"+botID+">") && !strings.Contains(m.Content, "<@!"+botID+">") {
		return false
	}
	return slices.ContainsFunc(m.Mentions, func(u *discordgo.User) bool { return u != nil && u.ID == botID })
}

func localReply(m *discordgo.Message) bool {
	if m == nil || m.Type != discordgo.MessageTypeReply || m.MessageReference == nil {
		return false
	}
	ref := m.MessageReference
	return ref.Type == discordgo.MessageReferenceTypeDefault && config.Snowflake(ref.MessageID) && ref.MessageID != m.ID &&
		ref.ChannelID == m.ChannelID && (ref.GuildID == "" || ref.GuildID == m.GuildID)
}

func visibleReplyMessage(message, current *discordgo.Message, botID string) bool {
	return message != nil && config.Snowflake(message.ID) && message.ChannelID == current.ChannelID &&
		(message.GuildID == "" || message.GuildID == current.GuildID) && message.WebhookID == "" &&
		message.Flags&discordgo.MessageFlagsEphemeral == 0 && message.Author != nil &&
		config.Snowflake(message.Author.ID) && message.Author.Bot == (message.Author.ID == botID) &&
		(message.Type == discordgo.MessageTypeDefault || message.Type == discordgo.MessageTypeReply)
}

// VerifiedErisReplyTarget is shared by the Gateway and the API's independent
// Discord read. A reply to another member only invites Eris with an inline mention.
func VerifiedErisReplyTarget(m *discordgo.Message, botID string) *discordgo.Message {
	if !localReply(m) {
		return nil
	}
	r := m.ReferencedMessage
	if !visibleReplyMessage(r, m, botID) || r.ID != m.MessageReference.MessageID ||
		(!r.Author.Bot && !explicitErisMention(m, botID)) {
		return nil
	}
	return r
}

func mayReadReplyHistory(ctx context.Context, s *discordgo.Session, m *discordgo.Message) bool {
	opt := discordgo.WithContext(ctx)
	ch, err := s.Channel(m.ChannelID, opt)
	if err != nil || ch == nil || ch.ID != m.ChannelID || ch.GuildID != m.GuildID {
		return false
	}
	scope := ch.ID
	if ch.IsThread() {
		scope = ch.ParentID // Threads inherit permission overwrites from their parent.
		if !config.Snowflake(scope) {
			return false
		}
	}
	perms, err := s.UserChannelPermissions(m.Author.ID, scope, opt)
	const need = discordgo.PermissionViewChannel | discordgo.PermissionReadMessageHistory
	return err == nil && perms&need == need
}

func discordExcerpt(m *discordgo.Message, quoted bool) eris.DiscordExcerpt {
	nick := ""
	if m.Member != nil {
		nick = m.Member.Nick
	}
	return eris.DiscordExcerpt{MessageID: m.ID, Author: discordSpeaker(m.Author.ID, nick, m.Author.GlobalName, m.Author.Username),
		Text: m.Content, Quoted: quoted, ByEris: m.Author.Bot}
}

func (b *Bot) replyContext(ctx context.Context, s *discordgo.Session, current *discordgo.Message) (*discordgo.Message, []eris.DiscordExcerpt) {
	if !localReply(current) {
		return nil, nil
	}
	botID := b.Config.DiscordClientID
	quote := VerifiedErisReplyTarget(current, botID)
	if current.ReferencedMessage != nil && quote == nil {
		return nil, nil
	}
	readHistory := b.Config.DiscordMessageContent && s != nil && explicitErisMention(current, botID) && mayReadReplyHistory(ctx, s, current)
	if quote == nil && readHistory {
		fresh, err := s.ChannelMessage(current.ChannelID, current.MessageReference.MessageID, discordgo.WithContext(ctx))
		if err == nil {
			resolved := *current
			resolved.ReferencedMessage = fresh
			quote = VerifiedErisReplyTarget(&resolved, botID)
		}
	}
	if quote == nil {
		return nil, nil
	}
	var out []eris.DiscordExcerpt
	if readHistory {
		// In a thread, ChannelID is the thread itself. Never read its parent.
		recent, err := s.ChannelMessages(current.ChannelID, ErisContextLimit, quote.ID, "", "", discordgo.WithContext(ctx))
		if err == nil && len(recent) <= ErisContextLimit {
			before, _ := strconv.ParseUint(quote.ID, 10, 64)
			for _, msg := range recent {
				if !visibleReplyMessage(msg, current, b.Config.DiscordClientID) || strings.TrimSpace(msg.Content) == "" {
					continue
				}
				id, err := strconv.ParseUint(msg.ID, 10, 64)
				if err != nil || id >= before || msg.Timestamp.IsZero() || quote.Timestamp.IsZero() ||
					msg.Timestamp.After(quote.Timestamp) || quote.Timestamp.Sub(msg.Timestamp) > 30*time.Minute {
					continue
				}
				out = append(out, discordExcerpt(msg, false))
				before = id // Discord returns messages newest first.
			}
			slices.Reverse(out)
		}
	}
	// An ordinary Eris reply already has its own cited text and provenance.
	if !quote.Author.Bot || erisReference(current, b.Config.DiscordClientID) == "" {
		out = append(out, discordExcerpt(quote, true))
	}
	return quote, out
}
