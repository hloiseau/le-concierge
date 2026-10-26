package discord

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/eris"
)

// RunMentions adds the Gateway alongside HTTP interactions. Discord supplies
// explicitly mentioned messages without the privileged Message Content intent.
func (b *Bot) RunMentions(ctx context.Context) {
	if b.Config.DiscordBotToken == "" || b.Config.ErisAPIURL == "" {
		return
	}
	s, err := discordgo.New("Bot " + b.Config.DiscordBotToken)
	if err != nil {
		slog.Warn("Discord mentions initialization failed")
		return
	}
	s.Identify.Intents = discordgo.IntentsGuildMessages
	if b.Config.DiscordMessageContent {
		s.Identify.Intents |= discordgo.IntentsMessageContent
	}
	s.StateEnabled = false
	s.SyncEvents = true
	// Library diagnostics can include payloads; log only our own fixed messages.
	s.LogLevel = -1
	s.Client = b.Client
	s.AddHandler(func(_ *discordgo.Session, _ *discordgo.Ready) { slog.Info("Discord mentions ready") })
	s.AddHandler(func(_ *discordgo.Session, _ *discordgo.Resumed) { slog.Info("Discord mentions resumed") })
	s.AddHandler(func(_ *discordgo.Session, _ *discordgo.Disconnect) {
		b.clearPublicErisTurns("")
		slog.Warn("Discord mentions disconnected")
	})
	var gate sync.Mutex
	stopping := false
	remove := s.AddHandler(func(s *discordgo.Session, event *discordgo.MessageCreate) {
		gate.Lock()
		defer gate.Unlock()
		if !stopping && ctx.Err() == nil {
			b.handleMention(ctx, s, event)
		}
	})
	defer func() {
		gate.Lock()
		stopping = true
		gate.Unlock()
		remove()
		_ = s.Close()
	}()
	for ctx.Err() == nil {
		// Check the token's identity before receiving or answering any messages.
		user, err := s.User("@me", discordgo.WithContext(ctx))
		if err == nil && user.ID == b.Config.DiscordClientID && user.Bot {
			if s.Open() == nil {
				<-ctx.Done()
				return
			}
		}
		slog.Warn("Discord mentions connection failed; retrying")
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

func (b *Bot) mentionQuestion(event *discordgo.MessageCreate) (string, bool) {
	if event == nil || event.Message == nil {
		return "", false
	}
	m := event.Message
	if m.GuildID != b.Config.DiscordGuildID || m.Author == nil || m.Author.Bot || m.WebhookID != "" ||
		m.Author.ID == b.Config.DiscordClientID || !config.Snowflake(m.Author.ID) || !config.Snowflake(m.ChannelID) || !config.Snowflake(m.ID) ||
		(m.Type != discordgo.MessageTypeDefault && m.Type != discordgo.MessageTypeReply) {
		return "", false
	}
	return ErisQuestion(m, b.Config.DiscordClientID)
}

// ErisQuestion is shared with the API's independent Discord message verification.
func ErisQuestion(m *discordgo.Message, botID string) (string, bool) {
	if m == nil {
		return "", false
	}
	mentioned := false
	for _, user := range m.Mentions {
		if user != nil && user.ID == botID {
			mentioned = true
		}
	}
	normal, nickname := "<@"+botID+">", "<@!"+botID+">"
	explicit := mentioned && (strings.Contains(m.Content, normal) || strings.Contains(m.Content, nickname))
	r, ref := m.ReferencedMessage, m.MessageReference
	reply := m.Type == discordgo.MessageTypeReply && r != nil && ref != nil &&
		config.Snowflake(ref.MessageID) && ref.MessageID == r.ID && ref.ChannelID == m.ChannelID &&
		(ref.GuildID == "" || ref.GuildID == m.GuildID) && r.ChannelID == m.ChannelID &&
		r.Author != nil && r.Author.Bot && r.Author.ID == botID && r.WebhookID == ""
	named := m.Type == discordgo.MessageTypeDefault && m.MessageReference == nil && len(m.Mentions) == 0 && len(m.MentionRoles) == 0 && !m.MentionEveryone && !strings.Contains(m.Content, "<@") && erisAddress.MatchString(strings.TrimSpace(m.Content))
	if !explicit && !reply && !named {
		return "", false
	}
	question := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(m.Content, normal, ""), nickname, ""))
	if named {
		// Strip only the verified vocative, preserving the exact request/choice.
		question = strings.TrimSpace(erisVocative.ReplaceAllString(question, "${1}"))
	}
	return question, true
}

func (b *Bot) handleMention(parent context.Context, s *discordgo.Session, event *discordgo.MessageCreate) {
	if event == nil || event.Message == nil || event.GuildID != b.Config.DiscordGuildID || !config.Snowflake(event.ChannelID) || !config.Snowflake(event.ID) {
		return
	}
	if event.Author != nil && event.Author.ID == b.Config.DiscordClientID {
		return
	}
	b.invalidateErisParticipation(event.ChannelID, event.ID)
	if event.Author == nil || event.Author.Bot || event.WebhookID != "" || !config.Snowflake(event.Author.ID) || (event.Type != discordgo.MessageTypeDefault && event.Type != discordgo.MessageTypeReply) {
		b.beginErisTurn(event.Message, false)
		return
	}
	question, ok := b.mentionQuestion(event)
	if !b.rememberEvent("mention:" + event.ID) {
		return
	}
	previous := b.beginErisTurn(event.Message, ok)
	if !ok && previous == nil {
		b.considerErisParticipation(parent, s, event)
		return
	}
	if !b.reserveEris() {
		return
	}
	b.workers.Add(1)
	go func() {
		defer b.workers.Done()
		defer func() { b.mu.Lock(); b.erisWorkers--; b.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(parent, 60*time.Second)
		defer cancel()
		proof := eris.ToolRequest{ChannelID: event.ChannelID, MessageID: event.ID}
		reference := erisReference(event.Message, b.Config.DiscordClientID)
		if !ok {
			messages, err := s.ChannelMessages(event.ChannelID, ErisContextLimit, event.ID, "", "", discordgo.WithContext(ctx))
			if err != nil {
				return
			}
			anchor, between := ErisContext(event.Message, messages, b.Config.DiscordClientID, previous.Message.ID, time.Now())
			if anchor == nil {
				return
			}
			var eligible bool
			question, eligible = ImplicitErisQuestion(event.Message, anchor, b.Config.DiscordClientID, time.Now())
			if !eligible || !b.currentErisTurn(event.ChannelID, event.ID) {
				return
			}
			addressed, err := b.Eris.AddressedWithContext(ctx, previous.Question, anchor.Content, question, event.Author.ID == previous.AuthorID, between)
			if err != nil || !addressed || !b.currentErisTurn(event.ChannelID, event.ID) {
				return
			}
			proof.ContextMessageID = anchor.ID
			reference = "Eris (message " + anchor.ID + ") : " + anchor.Content
		}
		reply := "Oui ? Mentionne-moi avec ton message pour discuter ici, ou utilise /eris pour une conversation privée."
		var components []discordgo.MessageComponent
		if utf8.RuneCountInString(question) > 500 {
			reply = "Fais un peu plus court : 500 caractères maximum par message."
		} else if question != "" || len(event.Attachments) > 0 || len(event.Embeds) > 0 {
			_ = s.ChannelTyping(event.ChannelID, discordgo.WithContext(ctx))
			owner := event.GuildID + ":" + event.Author.ID + ":" + event.ChannelID
			incoming := b.erisDiscordInput(ctx, s, event.Message)
			result := b.erisDiscordReply(ctx, owner, question, false, proof, reference, incoming)
			reply = result.Content
			for _, button := range result.Buttons {
				components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.Button{Style: discordgo.SecondaryButton, Label: button.Label, CustomID: button.ID}}})
			}
		}
		if !ok && !b.currentErisTurn(event.ChannelID, event.ID) {
			return
		}
		sent, err := s.ChannelMessageSendComplex(event.ChannelID, &discordgo.MessageSend{
			Content: reply, Reference: event.Reference(), Components: components,
			AllowedMentions: &discordgo.MessageAllowedMentions{},
		}, discordgo.WithContext(ctx))
		if err != nil {
			slog.Warn("Discord mention reply failed")
		} else if sent != nil {
			b.Eris.Delivered(event.GuildID+":"+event.Author.ID+":"+event.ChannelID, sent.ID, reply)
			if question != "" && utf8.RuneCountInString(question) <= 500 {
				b.rememberPublicErisReply(event.Message, sent, question, reply)
			}
		}
	}()
}

// Even an explicit mention cannot smuggle an unverified referenced author.
func erisReference(m *discordgo.Message, botID string) string {
	if m == nil || m.Type != discordgo.MessageTypeReply {
		return ""
	}
	r, ref := m.ReferencedMessage, m.MessageReference
	if r == nil || ref == nil || !config.Snowflake(ref.MessageID) || ref.MessageID != r.ID || ref.ChannelID != m.ChannelID || (ref.GuildID != "" && ref.GuildID != m.GuildID) || r.ChannelID != m.ChannelID || r.Author == nil || !r.Author.Bot || r.Author.ID != botID || r.WebhookID != "" || r.Flags&64 != 0 || r.Content == "" || utf8.RuneCountInString(r.Content) > 1500 {
		return ""
	}
	return "Eris (message " + r.ID + ") : " + r.Content
}
