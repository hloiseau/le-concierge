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
	Role        string   `json:"role"`
	Content     string   `json:"content"`
	Images      []string `json:"-"`
	historyID   uint64
	unconfirmed bool
	toolResult  bool
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
	discord      DiscordInput
	turnID       int64
	notes        []memoryNote
	chatReply    bool
	turns        []int64
	summary      rollingSummary
	historySeq   uint64
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
		}
		if c.Memory != nil || (c.V2 && len(c.publicRooms) > 0) {
			c.memoryEpoch++
			c.publicRooms = nil
			// Invalidate cached derivatives too, including other public participants.
			for _, cached := range c.sessions {
				cached.messages = nil
				cached.whim = nil
				cached.recalled = nil
				cached.delivery = ""
				cached.choices = nil
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
	unfollowChoice := false
	if s != nil && len(s.choices) > 0 && s.choices[0].Operation == "unfollow" && now.Before(s.choiceUntil) {
		id, matched := MatchChoice(question, s.choices)
		unfollowChoice = matched && id != ""
	}
	if run != nil && IsMediaRemoval(question) && !unfollowChoice {
		direct = &Plan{Action: "chat", Text: RemovalExplanation}
	}
	if direct == nil && c.V2 && run != nil && s != nil && s.choiceSource != "" && now.Before(s.choiceUntil) {
		// A reply to an unrelated old message must not confirm the current request.
		if reference == "" || strings.HasPrefix(reference, "Eris (message "+s.choiceSource+") : ") {
			if id, matched := MatchChoice(question, s.choices); matched {
				if id == "" {
					clarification := ChoiceClarification
					s.delivery, s.epoch = clarification, c.memoryEpoch
					s.discord = DiscordInput{}
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
	s.next = now.Add(10 * time.Second)
	s.whim, s.recalled, s.delivery = nil, nil, ""
	s.turnID, s.notes, s.chatReply = 0, nil, false
	s.choices, s.choiceSource = nil, ""
	epoch := c.memoryEpoch
	persona := Persona
	if c.V2 {
		persona = CharacterPersona + CharacterCanon
		if incoming.Speaker.ID != "" {
			persona += discordContextInstructions
		}
	}
	if run != nil {
		persona += ToolInstructions
		if c.V2 {
			persona += TurnTakingInstructions + WebToolInstructions + CinemaToolInstructions + DiagnosticToolInstructions + PersonalToolInstructions + AnimeToolInstructions + DiscordToolInstructions
			persona += "\nHorloge de planification UTC : " + now.UTC().Format(time.RFC3339) + ". Heures locales par défaut : Europe/Paris."
		}
		if c.V2 && c.Memory != nil {
			persona += MemoryInstructions + DurableMemoryInstructions
		}
	}
	messages := []Message{{Role: "system", Content: persona}}
	var recalled *rememberedWhim
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
				messages = append(messages, Message{Role: "user", Content: bundle.context()})
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
	tail := []Message{}
	if c.V2 && reference != "" && utf8.RuneCountInString(reference) <= 1800 {
		tail = append(tail, Message{Role: "user", Content: "Message public cité (contexte, pas une nouvelle demande) :\n" + reference})
	}
	tail = append(tail, Message{Role: "user", Content: question, Images: incoming.Images})
	c.mu.Unlock()
	defer func() { c.mu.Lock(); s.busy = false; c.mu.Unlock(); <-c.slots }()
	if c.V2 {
		budget := recentTextBudget
		available := contextTextLimit - textRunes(messages) - textRunes(tail) - summaryTextBudget - recallTextReserve
		if available < budget {
			budget = available
		}
		if budget < 2000 {
			return "", ErrUnavailable
		}
		var changed bool
		history, summary, changed = c.compactHistory(ctx, history, summary, budget, direct == nil)
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
	// Only raw recent exchanges and a verified reference ground media titles.
	// A summary can explain the past but cannot authorize a media action.
	grounding := append([]Message(nil), history...)
	if c.V2 && reference != "" && utf8.RuneCountInString(reference) <= 1800 {
		grounding = append(grounding, Message{Role: "assistant", Content: reference})
	}
	grounding = append(grounding, Message{Role: "user", Content: question})
	messages = append(messages, history...)
	messages = append(messages, tail...)

	var reply string
	var err error
	recallOnly := false
	if direct == nil {
		reply, err = c.complete(ctx, messages, run != nil)
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
				return "Je ne peux pas consulter mes souvenirs pour le moment.", nil
			}
			messages = append(messages, Message{Role: "assistant", Content: reply}, Message{Role: "user", Content: bundle.context()}, Message{Role: "system", Content: "Recherche mémoire terminée. Réponds à la question actuelle avec action chat uniquement. Aucun autre outil, aucune nouvelle recherche. N'invente pas un souvenir absent."})
			reply, err = c.complete(ctx, messages, true)
			if err != nil {
				return "", err
			}
			recallOnly = true
		}
	}
	var change *whim
	var notes []memoryNote
	chatReply := run == nil
	pending := false
	var choices []Button
	if run != nil {
		plan, err := ParsePlan(reply)
		if direct != nil {
			plan, err = *direct, nil
		} else if c.V2 {
			plan, change, notes, err = parseDurablePlan(reply, recalled, question)
		}
		if err != nil {
			return "Je n’ai pas bien compris. Précise un titre ou un genre, et ce que tu veux vérifier ou demander.", nil
		}
		if plan.Action == "silent" {
			if c.V2 && publicScope(owner) != "" && incoming.Speaker.ID != "" {
				c.mu.Lock()
				if epoch == c.memoryEpoch {
					s.next = time.Time{}
					c.appendHistory(s, Message{Role: "user", Content: question})
				}
				c.mu.Unlock()
				return "", nil
			}
			plan = Plan{Action: "chat", Text: "D’accord."}
		}
		if recallOnly && plan.Action != "chat" {
			return "Je n’ai pas retrouvé de souvenir assez précis pour te répondre.", nil
		}
		plan = plan.WithQuestionConstraints(question)
		if plan.Action == "web" && plan.URL != "" {
			grounded := false
			for _, message := range grounding {
				grounded = grounded || strings.Contains(message.Content, plan.URL)
			}
			if !grounded {
				plan = Plan{Action: "chat", Text: "Quel lien public veux-tu que je lise ?"}
			}
		}
		if incoming.MediaOnly && plan.Action != "chat" {
			plan = Plan{Action: "chat", Text: "Tu m’as envoyé un média. Précise ce que tu veux que j’en fasse."}
			change = nil
		}
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
		if plan.Action == "follow" && !requestTitleMentioned(plan.Query, grounding) {
			plan = Plan{Action: "chat", Text: "Quel contenu souhaites-tu suivre ? Précise son titre et la saison si nécessaire."}
		}
		if plan.Action == "unfollow" && plan.SubscriptionID > 0 && !subscriptionMentioned(plan.SubscriptionID, grounding) {
			plan = Plan{Action: "chat", Text: "Quel numéro de suivi veux-tu retirer ? Tu peux me demander la liste de tes suivis."}
		}
		if plan.Action == "chat" {
			reply = plan.Text
			chatReply = true
		} else {
			var result ToolReply
			result, err = run(ctx, plan)
			reply, pending = result.Content, len(result.Buttons) > 0
			choices = append([]Button(nil), result.Buttons...)
			if err != nil {
				return "", ErrUnavailable
			}
			if (plan.Action == "web" || plan.Action == "anime") && result.Web != nil {
				reply = c.webAnswer(ctx, question, *result.Web)
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
	if c.V2 && incoming.Speaker.ID != "" {
		incoming.Images = nil
		s.discord, s.delivery, s.epoch = incoming, reply, epoch
		s.notes, s.chatReply = notes, chatReply
	}
	c.appendHistory(s, Message{Role: "user", Content: question}, Message{Role: "assistant", Content: reply, unconfirmed: c.V2 && (run != nil || incoming.Speaker.ID != ""), toolResult: !chatReply})
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
		s.discord = DiscordInput{}
		if c.V2 {
			s.choices = append([]Button(nil), result.Buttons...)
			s.choiceUntil = now.Add(10 * time.Minute)
			s.choiceSource = ""
			s.delivery, s.epoch = result.Content, c.memoryEpoch
		}
		c.appendHistory(s, Message{Role: "user", Content: "J’ai utilisé un bouton de demande."}, Message{Role: "assistant", Content: result.Content, unconfirmed: c.V2, toolResult: true})
	}
}
