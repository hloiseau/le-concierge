package discord

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/config"
)

type erisAmbientMessage struct{ Author, Text string }
type erisAmbientTurn struct {
	Messages            []erisAmbientMessage
	LastID              string
	LastSeen, NextCheck time.Time
	Count               int
	Awaiting            bool
}

var filmClue = regexp.MustCompile(`(?i)(?:cherche|retrouv|souviens|rappelle|nom|titre|quel|quoi|comment).{0,80}(?:film|série|serie)|(?:film|série|serie).{0,80}(?:appelle|titre|nom|avec|où|dans lequel)`)

func (b *Bot) invalidateErisParticipation(channel, eventID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if state := b.erisAmbient[channel]; state != nil {
		state.LastID = eventID
	}
}

// The cheap gate runs locally. Even silence/errors consume the five-minute
// inference budget; ignoring Eris never causes her to prompt the channel again.
func (b *Bot) considerErisParticipation(parent context.Context, s *discordgo.Session, event *discordgo.MessageCreate) {
	m := event.Message
	if !b.Eris.V2 || !b.Config.DiscordMessageContent || b.Eris.Memory == nil ||
		m.Type != discordgo.MessageTypeDefault || m.MessageReference != nil || len(m.Mentions) != 0 || len(m.MentionRoles) != 0 || m.MentionEveryone || strings.Contains(m.Content, "<@") ||
		m.Flags&discordgo.MessageFlagsEphemeral != 0 || m.Timestamp.IsZero() || time.Since(m.Timestamp) > erisFollowupWindow || time.Until(m.Timestamp) > 30*time.Second || strings.TrimSpace(m.Content) == "" {
		return
	}
	b.mu.Lock()
	enabled, err := b.Eris.Memory.Participation(m.GuildID, m.ChannelID)
	if err != nil || !enabled {
		b.mu.Unlock()
		return
	}
	now := time.Now()
	if b.erisAmbient == nil {
		b.erisAmbient = map[string]*erisAmbientTurn{}
	}
	state := b.erisAmbient[m.ChannelID]
	if state == nil {
		if len(b.erisAmbient) >= 256 {
			b.mu.Unlock()
			return
		}
		state = &erisAmbientTurn{}
		b.erisAmbient[m.ChannelID] = state
	}
	if now.Sub(state.LastSeen) > erisFollowupWindow {
		state.Messages, state.Count = nil, 0
	}
	state.LastSeen, state.LastID = now, m.ID
	text := []rune(strings.TrimSpace(m.Content))
	if len(text) > 250 {
		text = text[:250]
	}
	state.Messages = append(state.Messages, erisAmbientMessage{m.Author.ID, string(text)})
	if len(state.Messages) > 6 {
		state.Messages = state.Messages[len(state.Messages)-6:]
	}
	state.Count++
	authors := map[string]bool{}
	var lines []string
	for _, message := range state.Messages {
		authors[message.Author] = true
		lines = append(lines, message.Author+": "+message.Text)
	}
	active := b.erisPublic[m.ChannelID]
	if state.Awaiting || now.Before(state.NextCheck) || b.erisWorkers != 0 ||
		(active != nil && active.Message != nil && now.Before(active.Expires)) ||
		(!filmClue.MatchString(m.Content) && (state.Count < 8 || len(authors) < 2)) {
		b.mu.Unlock()
		return
	}
	state.NextCheck, state.Count = now.Add(5*time.Minute), 0
	b.erisWorkers++
	b.mu.Unlock()
	b.workers.Add(1)
	go func() {
		defer b.workers.Done()
		defer func() { b.mu.Lock(); b.erisWorkers--; b.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(parent, 20*time.Second)
		defer cancel()
		reply, err := b.Eris.Participate(ctx, strings.Join(lines, "\n"))
		if err != nil || reply == "" {
			return
		}
		b.mu.Lock()
		current := b.erisAmbient[m.ChannelID]
		enabled, err := b.Eris.Memory.Participation(m.GuildID, m.ChannelID)
		valid := current == state && current.LastID == m.ID && enabled && err == nil
		b.mu.Unlock()
		if !valid {
			return
		}
		sent, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{Content: reply, Reference: event.Reference(), AllowedMentions: &discordgo.MessageAllowedMentions{}}, discordgo.WithContext(ctx))
		// A failed send is ambiguous: do not repeat an unsolicited intervention.
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.erisAmbient[m.ChannelID] != state {
			return
		}
		state.Awaiting = true
		state.NextCheck = time.Now().Add(5 * time.Minute)
		if err != nil || sent == nil || !config.Snowflake(sent.ID) || state.LastID != m.ID {
			return
		}
		if b.erisPublic == nil {
			b.erisPublic = map[string]*erisPublicTurn{}
		}
		if len(b.erisPublic) >= 256 && b.erisPublic[m.ChannelID] == nil {
			return
		}
		stamp := sent.Timestamp
		if stamp.IsZero() {
			stamp = time.Now()
		}
		b.erisPublic[m.ChannelID] = &erisPublicTurn{Message: &discordgo.Message{ID: sent.ID, ChannelID: m.ChannelID, GuildID: m.GuildID, Content: reply, Timestamp: stamp, Author: &discordgo.User{ID: b.Config.DiscordClientID, Bot: true}}, Question: string(text), AuthorID: m.Author.ID, LastEvent: sent.ID, Expires: stamp.Add(erisFollowupWindow)}
	}()
}
