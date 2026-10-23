// Package eris connects Discord conversations to an explicitly configured model.
// Media actions go through a bounded callback; the model has no credentials.
package eris

import (
	"context"
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
	Role    string `json:"role"`
	Content string `json:"content"`
}

type conversation struct {
	messages     []Message
	expires      time.Time
	next         time.Time
	busy         bool
	pending      bool
	choices      []Button
	choiceUntil  time.Time
	choiceSource string
	whim         *whim
	recalled     *rememberedWhim
	delivery     string
	epoch        uint64
	lastTopic    string
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
}

func New(url, key, model string) *Chat {
	return &Chat{URL: url, Key: key, Model: model, HTTP: &http.Client{Timeout: 50 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, sessions: map[string]*conversation{}, slots: make(chan struct{}, 2)}
}

// Reply keeps at most two previous turns for 30 minutes, in RAM only.
// Forgetting works even when the model is disabled or temporarily unavailable.
func (c *Chat) Reply(ctx context.Context, owner, question string, forget bool) (string, error) {
	return c.ReplyWithTools(ctx, owner, question, forget, nil)
}

func (c *Chat) ReplyWithTools(ctx context.Context, owner, question string, forget bool, run func(context.Context, Plan) (ToolReply, error)) (string, error) {
	return c.ReplyWithContext(ctx, owner, question, forget, "", run)
}

// reference must be a verified public Discord reply, never user-supplied metadata.
func (c *Chat) ReplyWithContext(ctx context.Context, owner, question string, forget bool, reference string, run func(context.Context, Plan) (ToolReply, error)) (string, error) {
	question = strings.TrimSpace(question)
	if utf8.RuneCountInString(question) > 500 || (question == "" && !forget) {
		return "", ErrUnavailable
	}
	c.mu.Lock()
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
				return "Je ne peux pas confirmer l’oubli de ma mémoire. Réessaie plus tard.", nil
			}
			c.memoryEpoch++
			// Invalidate cached derivatives too, including other public participants.
			for _, cached := range c.sessions {
				cached.messages = nil
				cached.whim = nil
				cached.recalled = nil
				cached.delivery = ""
				cached.choices = nil
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
		return "Conversation oubliée. On reprend depuis le début.", nil
	}
	if !c.V2 && run != nil && s != nil && s.pending && confirmationText(question) {
		c.mu.Unlock()
		return "Utilise les boutons de ma réponse précédente pour choisir, confirmer ou annuler. Aucun envoi ne se fait avec une simple réponse dans le chat.", nil
	}
	if c.URL == "" {
		c.mu.Unlock()
		return "Mon moteur de conversation n’est pas encore connecté. Mes commandes restent disponibles avec /aide.", nil
	}
	if s != nil && now.Before(s.next) {
		c.mu.Unlock()
		return "", ErrBusy
	}
	var direct *Plan
	if run != nil && IsMediaRemoval(question) {
		direct = &Plan{Action: "chat", Text: RemovalExplanation}
	}
	if direct == nil && c.V2 && run != nil && s != nil && s.choiceSource != "" && now.Before(s.choiceUntil) {
		// A reply to an unrelated old message must not confirm the current request.
		if reference == "" || strings.HasPrefix(reference, "Eris (message "+s.choiceSource+") : ") {
			if id, matched := MatchChoice(question, s.choices); matched {
				if id == "" {
					clarification := ChoiceClarification
					s.delivery, s.epoch = clarification, c.memoryEpoch
					s.choiceSource = ""
					c.mu.Unlock()
					return clarification, nil
				}
				direct = &Plan{Action: "continue", Query: id}
			}
		}
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
		c.sessions[owner] = s
	}
	s.busy = true
	s.expires = now.Add(30 * time.Minute)
	s.next = now.Add(10 * time.Second)
	s.whim, s.recalled, s.delivery = nil, nil, ""
	s.choices, s.choiceSource = nil, ""
	epoch := c.memoryEpoch
	persona := Persona
	if c.V2 {
		persona = CharacterPersona
	}
	if run != nil {
		persona += ToolInstructions
		if c.V2 && c.Memory != nil {
			persona += MemoryInstructions
		}
	}
	messages := []Message{{Role: "system", Content: persona}}
	var recalled *rememberedWhim
	if c.V2 {
		messages = append(messages, Message{Role: "system", Content: "Auteur actuel (identité technique, ne pas réciter) : " + owner})
		if c.Memory != nil {
			recalled, _ = c.Memory.recall(owner, question, now)
			if recalled == nil && stopWhim(question) && s.lastTopic != "" {
				recalled, _ = c.Memory.recall(owner, s.lastTopic, now)
			}
			if recalled != nil {
				messages = append(messages, Message{Role: "system", Content: memoryContext(recalled)})
			}
		}
	}
	messages = append(messages, s.messages...)
	grounding := append([]Message(nil), s.messages...)
	if c.V2 && reference != "" && utf8.RuneCountInString(reference) <= 1800 {
		messages = append(messages, Message{Role: "user", Content: "Message public cité (contexte, pas une nouvelle demande) :\n" + reference})
		grounding = append(grounding, Message{Role: "assistant", Content: reference})
	}
	grounding = append(grounding, Message{Role: "user", Content: question})
	messages = append(messages, Message{Role: "user", Content: question})
	c.mu.Unlock()
	defer func() { c.mu.Lock(); s.busy = false; c.mu.Unlock(); <-c.slots }()

	var reply string
	var err error
	if direct == nil {
		reply, err = c.complete(ctx, messages, run != nil)
		if err != nil {
			return "", err
		}
	}
	var change *whim
	pending := false
	var choices []Button
	if run != nil {
		plan, err := ParsePlan(reply)
		if direct != nil {
			plan, err = *direct, nil
		} else if c.V2 {
			plan, change, err = parseCharacterPlan(reply, recalled)
		}
		if err != nil {
			return "Je n’ai pas bien compris. Précise un titre ou un genre, et ce que tu veux vérifier ou demander.", nil
		}
		plan = plan.WithQuestionConstraints(question)
		// A bare title following the capability explanation is not a new add intent.
		if plan.Query != "" && choiceText(question) == choiceText(plan.Query) && len(grounding) > 1 && strings.HasSuffix(grounding[len(grounding)-2].Content, RemovalExplanation) {
			plan = Plan{Action: "chat", Text: RemovalExplanation}
			change = nil
		}
		if plan.Unwatched {
			requested := false
			for _, message := range grounding {
				requested = requested || (message.Role == "user" && unseenWords.MatchString(message.Content))
			}
			plan.Unwatched = requested
		}
		if plan.Action == "prepare" && !requestTitleMentioned(plan.Query, grounding) {
			plan = Plan{Action: "chat", Text: "Quel titre veux-tu demander dans Seerr ? Donne-moi le nom du film ou de la série, et la saison si nécessaire."}
		}
		if plan.Action == "chat" {
			reply = plan.Text
		} else {
			var result ToolReply
			result, err = run(ctx, plan)
			reply, pending = result.Content, len(result.Buttons) > 0
			choices = append([]Button(nil), result.Buttons...)
			if err != nil {
				return "", ErrUnavailable
			}
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
	s.pending = pending
	if c.V2 {
		s.choices = choices
		s.choiceUntil = now.Add(10 * time.Minute)
	}
	if c.V2 && run != nil {
		if stopWhim(question) && recalled != nil {
			change = &whim{Topic: recalled.Topic, Retire: true}
		}
		s.whim, s.recalled, s.delivery = change, recalled, reply
		s.epoch = epoch
	}
	s.messages = append(s.messages, Message{Role: "user", Content: question}, Message{Role: "assistant", Content: reply})
	if len(s.messages) > c.historyLimit() {
		s.messages = append([]Message(nil), s.messages[len(s.messages)-c.historyLimit():]...)
	}
	c.mu.Unlock()
	return reply, nil
}

func confirmationText(text string) bool {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(text), ".! ?")) {
	case "oui", "ok", "oui vas-y", "vas-y", "confirme", "je confirme", "yes", "confirm", "annule", "annuler", "cancel", "non":
		return true
	}
	return false
}

// Remember legacy button results; v2 can continue their choices in text.
func (c *Chat) RememberRequest(owner string, result ToolReply) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for key, s := range c.sessions {
		if !s.busy && now.After(s.expires) {
			delete(c.sessions, key)
		}
	}
	if c.sessions[owner] == nil && len(c.sessions) < 256 {
		c.sessions[owner] = &conversation{expires: now.Add(30 * time.Minute)}
	}
	if s := c.sessions[owner]; s != nil && !s.busy && now.Before(s.expires) {
		s.pending = len(result.Buttons) > 0
		if c.V2 {
			s.choices = append([]Button(nil), result.Buttons...)
			s.choiceUntil = now.Add(10 * time.Minute)
			s.choiceSource = ""
			s.delivery, s.epoch = result.Content, c.memoryEpoch
		}
		s.messages = append(s.messages, Message{Role: "user", Content: "J’ai utilisé un bouton de demande."}, Message{Role: "assistant", Content: result.Content})
		if len(s.messages) > c.historyLimit() {
			s.messages = append([]Message(nil), s.messages[len(s.messages)-c.historyLimit():]...)
		}
	}
}
