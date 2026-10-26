package discord

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/config"
)

const erisFollowupWindow = 2 * time.Minute
const ErisContextLimit = 6

var erisAddress = regexp.MustCompile(`(?i)^(?:(?:salut|bonjour|bonsoir|coucou|hey|hé|eh)[ ,!]+)?[ée]ris(?:\s*[,!?:]|\s+(?:bonjour|salut|tu|t['’]|peux|pourrais|dis|dit)\b|\s*$)|[,!?]\s*[ée]ris\s*[.!?]*$`)
var erisVocative = regexp.MustCompile(`(?i)^((?:(?:salut|bonjour|bonsoir|coucou|hey|hé|eh)[ ,!]+)?)[ée]ris\s*[,!?:]?|[,!?]\s*[ée]ris\s*[.!?]*$`)

type erisPublicTurn struct {
	Message            *discordgo.Message
	Question, AuthorID string
	LastEvent          string
	Expires            time.Time
	Intervening        int
	NextCheck          time.Time
}

// ImplicitErisQuestion validates a recent public bot turn. The API obtains
// previous independently from Discord; the bot additionally classifies the text.
func ImplicitErisQuestion(m, previous *discordgo.Message, botID string, now time.Time) (string, bool) {
	if m == nil || previous == nil || m.Author == nil || m.Author.Bot || m.Author.ID == botID || m.WebhookID != "" ||
		!config.Snowflake(m.ID) || !config.Snowflake(m.ChannelID) || !config.Snowflake(m.Author.ID) ||
		m.Type != discordgo.MessageTypeDefault || m.MessageReference != nil || len(m.Mentions) != 0 || len(m.MentionRoles) != 0 || m.MentionEveryone || strings.Contains(m.Content, "<@") ||
		previous.Author == nil || !previous.Author.Bot || previous.Author.ID != botID || previous.WebhookID != "" ||
		!config.Snowflake(previous.ID) || previous.ID == m.ID || previous.ChannelID != m.ChannelID || previous.Flags&discordgo.MessageFlagsEphemeral != 0 ||
		(previous.Type != discordgo.MessageTypeDefault && previous.Type != discordgo.MessageTypeReply) ||
		(previous.GuildID != "" && previous.GuildID != m.GuildID) || previous.Content == "" || utf8.RuneCountInString(previous.Content) > 1500 ||
		previous.Timestamp.IsZero() || m.Timestamp.Before(previous.Timestamp) || m.Timestamp.Sub(previous.Timestamp) > erisFollowupWindow ||
		now.Sub(previous.Timestamp) > erisFollowupWindow || now.Before(previous.Timestamp) || now.Sub(m.Timestamp) > erisFollowupWindow || m.Timestamp.After(now.Add(30*time.Second)) {
		return "", false
	}
	question := strings.TrimSpace(m.Content)
	return question, question != "" && utf8.RuneCountInString(question) <= 500
}

// Retain a bounded conversation across interleaved messages, invalidating work
// already in flight whenever a newer event arrives.
func (b *Bot) beginErisTurn(m *discordgo.Message, direct bool) *erisPublicTurn {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for channel, turn := range b.erisPublic {
		if now.After(turn.Expires) {
			delete(b.erisPublic, channel)
		}
	}
	turn := b.erisPublic[m.ChannelID]
	if turn == nil {
		if !direct || len(b.erisPublic) >= 256 {
			return nil
		}
		if b.erisPublic == nil {
			b.erisPublic = map[string]*erisPublicTurn{}
		}
		turn = &erisPublicTurn{Expires: now.Add(erisFollowupWindow)}
		b.erisPublic[m.ChannelID] = turn
	}
	previous := *turn
	turn.LastEvent = m.ID
	turn.Intervening++
	if direct {
		turn.Message = nil
		turn.Intervening, turn.NextCheck = 0, time.Time{}
	}
	if !b.Config.DiscordMessageContent || !b.Eris.V2 || direct {
		return nil
	}
	if _, ok := ImplicitErisQuestion(m, previous.Message, b.Config.DiscordClientID, now); !ok {
		return nil
	}
	if turn.Intervening > ErisContextLimit || (m.Author.ID != previous.AuthorID && now.Before(turn.NextCheck)) {
		return nil
	}
	turn.NextCheck = now.Add(10 * time.Second)
	return &previous
}

// Discord returns newest first. Never cross a more recent Eris response or use
// a context outside the bounded window; both bot and API run this validation.
func ErisContext(m *discordgo.Message, recent []*discordgo.Message, botID, contextID string, now time.Time) (*discordgo.Message, string) {
	if len(recent) > ErisContextLimit {
		return nil, ""
	}
	var between []string
	for _, prior := range recent {
		if prior == nil || prior.Author == nil || prior.ChannelID != m.ChannelID || prior.Timestamp.After(m.Timestamp) || prior.Flags&discordgo.MessageFlagsEphemeral != 0 {
			return nil, ""
		}
		if prior.ID == contextID {
			if _, ok := ImplicitErisQuestion(m, prior, botID, now); !ok {
				return nil, ""
			}
			// Present interleaved messages oldest first, with authors kept separate.
			for a, z := 0, len(between)-1; a < z; a, z = a+1, z-1 {
				between[a], between[z] = between[z], between[a]
			}
			return prior, strings.Join(between, "\n")
		}
		if prior.Author.ID == botID {
			return nil, ""
		}
		if !prior.Author.Bot && prior.WebhookID == "" {
			text := []rune(prior.Content)
			if len(text) > 200 {
				text = text[:200]
			}
			between = append(between, prior.Author.ID+": "+string(text))
		}
	}
	return nil, ""
}

func (b *Bot) currentErisTurn(channel, eventID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	turn := b.erisPublic[channel]
	return turn != nil && turn.LastEvent == eventID && time.Now().Before(turn.Expires)
}

func (b *Bot) rememberPublicErisReply(event, sent *discordgo.Message, question, reply string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	turn := b.erisPublic[event.ChannelID]
	if turn == nil || turn.LastEvent != event.ID || !config.Snowflake(sent.ID) {
		return
	}
	now := time.Now()
	stamp := sent.Timestamp
	if stamp.IsZero() {
		stamp = now
	}
	turn.Message = &discordgo.Message{ID: sent.ID, GuildID: event.GuildID, ChannelID: event.ChannelID, Content: reply, Timestamp: stamp,
		Author: &discordgo.User{ID: b.Config.DiscordClientID, Bot: true}}
	turn.Question, turn.AuthorID, turn.LastEvent, turn.Expires = question, event.Author.ID, sent.ID, stamp.Add(erisFollowupWindow)
	turn.Intervening, turn.NextCheck = 0, time.Time{}
	if ambient := b.erisAmbient[event.ChannelID]; ambient != nil {
		ambient.Awaiting = false
	}
}

func (b *Bot) clearPublicErisTurns(author string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for channel, turn := range b.erisPublic {
		if author == "" || turn.AuthorID == author {
			delete(b.erisPublic, channel)
		}
	}
	for channel, turn := range b.erisAmbient {
		if author == "" {
			delete(b.erisAmbient, channel)
			continue
		}
		for _, message := range turn.Messages {
			if message.Author == author {
				delete(b.erisAmbient, channel)
				break
			}
		}
	}
}
