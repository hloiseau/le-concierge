// Package eris connects Discord conversations to an explicitly configured model.
// Media actions go through a bounded callback; the model has no credentials.
package eris

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const Persona = `Tu es Eris, le bot Discord. Créée par Hugo Loiseau, à citer seulement si demandé. Réponds dans la langue du message, en 1-2 phrases courtes. Sympa par défaut, humour sec, des goûts, sans vanne forcée. Un bonjour mérite un bonjour ; réponds aux acquiescements par un simple acquiescement, sans jugement. Si on te provoque, renvoie une pique sur la banalité de l'insulte, pas sur l'intelligence de la personne. Aucun sermon, soumission, remarque sexuelle, emoji ou rôle théâtral. Arrête les piques sur demande. Sois douce face à une vraie détresse. Aucun souvenir ou fait catalogue inventé. Pas d'offre de service spontanée. Oubli : /eris oublier:true. Livres : /livres.`

var (
	ErrBusy        = errors.New("conversation busy")
	ErrUnavailable = errors.New("Eris unavailable")
)

type Message struct {
	Role             string            `json:"role"`
	Content          string            `json:"content"`
	ToolCalls        []ToolCall        `json:"tool_calls,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	ResponseItems    []json.RawMessage `json:"-"`
	Images           []string          `json:"-"`
	historyID        uint64
	unconfirmed      bool
	toolResult       bool
}

type conversation struct {
	messages   []Message
	expires    time.Time
	busy       bool
	whim       *whim
	recalled   *rememberedWhim
	delivery   string
	epoch      uint64
	lastTopic  string
	discord    DiscordInput
	turnID     int64
	notes      []memoryNote
	chatReply  bool
	turns      []int64
	summary    rollingSummary
	historySeq uint64
}

type Chat struct {
	URL, Key, Model string
	HTTP            *http.Client
	V2              bool
	Protocol        string
	KeyFile         string
	Memory          *Memory
	memoryEpoch     uint64
	mu              sync.Mutex
	sessions        map[string]*conversation
	slots           chan struct{}
	publicRooms     map[string]*publicRoom
}

func New(url, key, model string) *Chat {
	return &Chat{URL: url, Key: key, Model: model, HTTP: &http.Client{Timeout: 50 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, sessions: map[string]*conversation{}, slots: make(chan struct{}, 2)}
}

// Reply keeps a bounded recent window; v2 compacts older turns into a sourced
// summary, persisted when Memory is configured. RAM sessions expire after 30m.
// Forgetting works even when the model is disabled or temporarily unavailable.
func (c *Chat) Reply(ctx context.Context, owner, question string, forget bool) (string, error) {
	return c.ReplyWithTools(ctx, owner, question, forget, nil)
}

func (c *Chat) ReplyWithTools(ctx context.Context, owner, question string, forget bool, run func(context.Context, Plan) (ToolReply, error)) (string, error) {
	return c.ReplyWithContext(ctx, owner, question, forget, "", run)
}

// reference must be a verified public Discord reply, never user-supplied metadata.
func (c *Chat) ReplyWithContext(ctx context.Context, owner, question string, forget bool, reference string, run func(context.Context, Plan) (ToolReply, error)) (string, error) {
	return c.ReplyWithDiscordContext(ctx, owner, question, forget, reference, DiscordInput{}, run)
}

func (c *Chat) ReplyWithDiscordContext(ctx context.Context, owner, question string, forget bool, reference string, incoming DiscordInput, run func(context.Context, Plan) (ToolReply, error)) (string, error) {
	incoming = incoming.normalized(owner)
	question = strings.TrimSpace(question)
	if question == "" && !forget && incoming.VisualNote != "" {
		question = "Réagis au média joint."
		incoming.MediaOnly = true
	}
	if utf8.RuneCountInString(question) > 500 || (question == "" && !forget) {
		return "", ErrUnavailable
	}
	c.mu.Lock()
	// Queue short follow-ups behind the active turn instead of discarding them.
	for {
		active := c.sessions[owner]
		if (active == nil || !active.busy) && len(c.slots) < cap(c.slots) {
			break
		}
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", ErrUnavailable
		case <-time.After(25 * time.Millisecond):
		}
		c.mu.Lock()
	}
	now := time.Now()
	for key, s := range c.sessions {
		if !s.busy && now.After(s.expires) {
			delete(c.sessions, key)
		}
	}
	s := c.sessions[owner]
	if s != nil && s.busy {
		c.mu.Unlock()
		return "", ErrBusy
	}
	if forget {
		// The private command also clears this member's separate channel chats.
		for key, session := range c.sessions {
			if (key == owner || strings.HasPrefix(key, owner+":")) && session.busy {
				c.mu.Unlock()
				return "", ErrBusy
			}
		}
		if c.Memory != nil {
			if err := c.Memory.forget(owner); err != nil {
				c.mu.Unlock()
				return c.Explain(ctx, question, map[string]any{"operation": "forget", "status": "failed"})
			}
		}
		if c.Memory != nil || (c.V2 && len(c.publicRooms) > 0) {
			c.memoryEpoch++
			guild, _, _ := strings.Cut(owner, ":")
			for scope := range c.publicRooms {
				if strings.HasPrefix(scope, guild+":") {
					delete(c.publicRooms, scope)
				}
			}
			// Invalidate cached derivatives too, including other public participants.
			for key, cached := range c.sessions {
				if !strings.HasPrefix(key, guild+":") {
					continue
				}
				cached.messages = nil
				cached.whim = nil
				cached.recalled = nil
				cached.delivery = ""
				cached.discord = DiscordInput{}
				cached.turnID, cached.notes = 0, nil
				cached.turns = nil
				cached.summary = rollingSummary{}
			}
		}
		for key := range c.sessions {
			if key == owner || strings.HasPrefix(key, owner+":") {
				delete(c.sessions, key)
			}
		}
		s = nil
	}
	if question == "" {
		c.mu.Unlock()
		return c.Explain(ctx, question, map[string]any{"operation": "forget", "status": "completed"})
	}
	if c.URL == "" {
		c.mu.Unlock()
		return "", ErrUnavailable
	}
	select {
	case c.slots <- struct{}{}:
	default:
		c.mu.Unlock()
		return "", ErrBusy
	}
	if s == nil {
		if len(c.sessions) >= 256 {
			<-c.slots
			c.mu.Unlock()
			return "", ErrBusy
		}
		s = &conversation{}
		if c.V2 && c.Memory != nil {
			if summary, err := c.Memory.loadSummary(owner); err == nil {
				s.summary = summary
				s.historySeq = summary.Sequence
			}
		}
		c.sessions[owner] = s
	}
	if c.V2 {
		s.messages = confirmedHistory(s.messages)
		s.assignHistoryIDs()
	}
	s.busy = true
	s.expires = now.Add(30 * time.Minute)

	s.whim, s.recalled, s.delivery = nil, nil, ""
	s.turnID, s.notes, s.chatReply = 0, nil, false
	epoch := c.memoryEpoch
	persona := c.instructions(incoming.Speaker.ID != "", run != nil)
	if incoming.ChatOnly {
		persona = c.chatOnlyInstructions(incoming.Speaker.ID != "")
	}
	if c.V2 && run != nil {
		persona += "\nHorloge de planification UTC : " + now.UTC().Format(time.RFC3339) + ". Heures locales par défaut : Europe/Paris."
	}
	messages := []Message{{Role: "system", Content: persona}}
	var recalled *rememberedWhim
	var durable memoryBundle
	if c.V2 {
		messages = append(messages, Message{Role: "system", Content: "Auteur actuel (identité technique, ne pas réciter) : " + owner})
		c.observePublic(owner, question, incoming, now)
		if data := c.discordContext(owner, incoming, now); data != "" {
			messages = append(messages, Message{Role: "user", Content: data})
		}
		if c.Memory != nil {
			if id, err := c.Memory.observe(owner, question, incoming, now); err == nil {
				s.turnID = id
				if c.Memory.deriveSummary(id, owner) != nil {
					s.turnID = 0
				}
				if c.Memory.deriveTurns(id, s.turns) != nil {
					s.turnID = 0
				}
				if s.turnID != 0 {
					s.turns = append(s.turns, id)
					if len(s.turns) > c.historyLimit()/2 {
						s.turns = append([]int64(nil), s.turns[len(s.turns)-c.historyLimit()/2:]...)
					}
				}
			}
			// Public RAM context can also contribute to a later durable reply.
			if room := c.publicRooms[publicScope(owner)]; room != nil {
				var dependencies memoryBundle
				var authors []Speaker
				for _, entry := range room.exchanges {
					dependencies.Episodes = append(dependencies.Episodes, memoryTurn{Source: entry.ID})
					authors = append(authors, entry.Author)
				}
				if c.Memory.derive(s.turnID, owner, dependencies) != nil || c.Memory.deriveSpeakers(s.turnID, owner, authors) != nil {
					s.turnID = 0
				}
			}
			if c.Memory.deriveReference(s.turnID, owner, reference) != nil {
				s.turnID = 0
			}
			bundle, err := c.Memory.recallDurable(owner, question, incoming, s.turnID)
			if err == nil && c.Memory.derive(s.turnID, owner, bundle) == nil {
				durable = bundle
			}
			recalled, _ = c.Memory.recall(owner, question, now)
			if recalled == nil && stopWhim(question) && s.lastTopic != "" {
				recalled, _ = c.Memory.recall(owner, s.lastTopic, now)
			}
			if recalled != nil {
				if c.Memory.deriveWhim(s.turnID, owner, recalled) != nil {
					s.turnID = 0
				}
				messages = append(messages, Message{Role: "system", Content: memoryContext(recalled)})
			}
		}
	}
	history := append([]Message(nil), s.messages...)
	summary := s.summary
	turnID := s.turnID
	tail := []Message{}
	if c.V2 && reference != "" && utf8.RuneCountInString(reference) <= 1800 {
		tail = append(tail, Message{Role: "user", Content: "Message public cité (contexte, pas une nouvelle demande) :\n" + reference})
	}
	tail = append(tail, Message{Role: "user", Content: question, Images: incoming.Images})
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		s.busy = false
		c.mu.Unlock()
		<-c.slots
	}()
	if c.V2 {
		budget := recentTextBudget
		available := contextTextLimit - textRunes(messages) - textRunes(tail) - durable.promptBudget() - utf8.RuneCountInString(conversationVoice) - summaryTextBudget - recallTextReserve
		if available < budget {
			budget = available
		}
		if budget < 2000 {
			return "", ErrUnavailable
		}
		var changed bool
		history, summary, changed = c.compactHistory(ctx, history, summary, budget, true)
		c.mu.Lock()
		if epoch != c.memoryEpoch {
			c.mu.Unlock()
			return "", ErrUnavailable
		}
		s.messages, s.summary = history, summary
		if changed && c.Memory != nil && s.turnID != 0 {
			_ = c.Memory.saveSummary(owner, summary, s.turnID, time.Now())
		}
		c.mu.Unlock()
		if len(summary.Facts) > 0 {
			messages = append(messages, Message{Role: "user", Content: summary.context()})
		}
	}
	if c.V2 {
		// Compact first: an evicted exchange may still need its durable citation.
		durable = durable.withoutRecent(incoming.Speaker.ID, history)
		if len(durable.Notes) != 0 || len(durable.Episodes) != 0 {
			messages = append(messages, Message{Role: "user", Content: durable.context()})
		}
	}
	messages = append(messages, history...)
	messages = append(messages, tail...)

	var reply string
	var err error
	agentUsed := false
	if run != nil && !incoming.ChatOnly {
		reply, agentUsed, err = c.agentReply(ctx, c.dialoguePrompt(messages), run)
	} else if run != nil {
		reply, agentUsed, err = c.agentReply(ctx, c.dialoguePrompt(messages), nil)
	} else {
		reply, err = c.complete(ctx, c.dialoguePrompt(messages), false)
	}
	if err != nil {
		return "", err
	}
	// A memory lookup is local and read-only; never forward it to media tools.
	if query := parseRecall(reply); c.V2 && c.Memory != nil && run != nil && query != "" {
		c.mu.Lock()
		if epoch != c.memoryEpoch {
			c.mu.Unlock()
			return "", ErrUnavailable
		}
		bundle, searchErr := c.Memory.recallDurable(owner, query, incoming, s.turnID)
		if searchErr == nil {
			searchErr = c.Memory.derive(s.turnID, owner, bundle)
		}
		c.mu.Unlock()
		if searchErr != nil {
			return c.Explain(ctx, question, map[string]any{"operation": "recall", "status": "unavailable"})
		}
		messages = append(messages, Message{Role: "assistant", Content: reply}, Message{Role: "user", Content: bundle.context()}, Message{Role: "system", Content: "Recherche mémoire terminée. Réponds à la question actuelle avec action chat uniquement. Aucun autre outil, aucune nouvelle recherche. N'invente pas un souvenir absent."})
		reply, err = c.complete(ctx, c.dialoguePrompt(messages), true)
		if err != nil {
			return "", err
		}
	}
	var change *whim
	var notes []memoryNote
	chatReply := run == nil
	if run != nil {
		plan, err := ParsePlan(reply)
		if c.V2 {
			plan, change, notes, err = parseDurablePlan(reply, recalled, question)
		}
		if err != nil {
			return "", ErrUnavailable
		}
		if plan.Action == "silent" {
			if c.V2 && publicScope(owner) != "" && incoming.Speaker.ID != "" {
				c.mu.Lock()
				if epoch == c.memoryEpoch {
					c.appendHistory(s, Message{Role: "user", Content: question})
				}
				c.mu.Unlock()
				return "", nil
			}
			return "", ErrUnavailable
		}
		if plan.Action != "chat" {
			return "", ErrUnavailable
		}
		if command := preferenceCommand(question); c.V2 && command != "" {
			correction := command == "corrige" || command == "mets a jour"
			var topics []string
			for _, note := range durable.Notes {
				if note.Author.ID == incoming.Speaker.ID && note.Kind == "preference" {
					topics = append(topics, note.Topic)
				}
			}
			notes = preferenceNote(notes, topics, correction)
			switch {
			case c.Memory == nil || turnID == 0:
				plan.Text, err = c.Explain(ctx, question, map[string]any{"operation": "remember_preference", "status": "unavailable"})
				if err != nil {
					return "", err
				}
				notes = nil
			case len(notes) == 0:
				plan.Text, notes = c.extractPreference(ctx, question, topics, correction)
			}
		}
		reply = plan.Text
		chatReply = !agentUsed
		if agentUsed {
			notes = nil
			change = nil
		}
		if reply == "" || utf8.RuneCountInString(reply) > 1500 {
			return "", ErrUnavailable
		}
	}
	c.mu.Lock()
	if epoch != c.memoryEpoch {
		c.mu.Unlock()
		return reply, nil
	}
	if c.V2 && run != nil {
		if stopWhim(question) && recalled != nil {
			change = &whim{Topic: recalled.Topic, Retire: true}
		}
		s.whim, s.recalled, s.delivery = change, recalled, reply
		s.epoch = epoch
	}
	if c.V2 && incoming.Speaker.ID != "" {
		incoming.Images = nil
		s.discord, s.delivery, s.epoch = incoming, reply, epoch
		s.notes, s.chatReply = notes, chatReply
	}
	c.appendHistory(s, Message{Role: "user", Content: question}, Message{Role: "assistant", Content: reply, unconfirmed: c.V2 && (run != nil || incoming.Speaker.ID != ""), toolResult: !chatReply})
	c.mu.Unlock()
	return reply, nil
}
