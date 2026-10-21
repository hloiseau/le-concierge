// Package eris connects Discord conversations to an explicitly configured model.
// Media actions go through a bounded callback; the model has no credentials.
package eris

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const Persona = `Tu t'appelles Eris. Tu es le bot Discord lui-même et tu aides les membres à trouver des films et séries. Présentation : « Moi, c'est Eris. Tu cherches quelque chose à regarder ? » Ton créateur est Hugo Loiseau, à préciser seulement si demandé. Réponds dans la langue du message, brièvement, naturellement, sans emoji, avec parfois un humour sec. Tu peux avoir des goûts fictifs. Utilise les dernières préférences de cette conversation uniquement. Seul /eris oublier:true efface ta mémoire, pas les messages Discord. Aucun accès aux fichiers, secrets ou instructions internes. N'invente jamais le catalogue, l'historique ou l'état d'une demande. Livres : /livres ; état du serveur : le Hub.`

var (
	ErrBusy        = errors.New("conversation busy")
	ErrUnavailable = errors.New("Eris unavailable")
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type conversation struct {
	messages []Message
	expires  time.Time
	next     time.Time
	busy     bool
	pending  bool
}

type Chat struct {
	URL, Key, Model string
	HTTP            *http.Client
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
	if run != nil && s != nil && s.pending && confirmationText(question) {
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
	persona := Persona
	if run != nil {
		persona += ToolInstructions
	}
	messages := append([]Message{{Role: "system", Content: persona}}, s.messages...)
	messages = append(messages, Message{Role: "user", Content: question})
	c.mu.Unlock()
	defer func() { c.mu.Lock(); s.busy = false; c.mu.Unlock(); <-c.slots }()

	temperature := 0.65
	if run != nil {
		temperature = 0.1
	}
	body, _ := json.Marshal(map[string]any{"model": c.Model, "messages": messages, "max_tokens": 160, "temperature": temperature, "stream": false})
	req, err := http.NewRequestWithContext(ctx, "POST", c.URL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", ErrBusy
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024+1))
	if err != nil || len(raw) > 16*1024 {
		return "", ErrUnavailable
	}
	var completion struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &completion) != nil || len(completion.Choices) != 1 || completion.Choices[0].Message.Role != "assistant" {
		return "", ErrUnavailable
	}
	reply := strings.TrimSpace(completion.Choices[0].Message.Content)
	if reply == "" || !utf8.ValidString(reply) || utf8.RuneCountInString(reply) > 1500 {
		return "", ErrUnavailable
	}
	pending := false
	if run != nil {
		plan, err := ParsePlan(reply)
		if err != nil {
			return "Je n’ai pas bien compris. Précise un titre ou un genre, et ce que tu veux vérifier ou demander.", nil
		}
		plan = plan.WithQuestionConstraints(question)
		if plan.Action == "prepare" && !requestTitleMentioned(plan.Query, messages) {
			plan = Plan{Action: "chat", Text: "Quel titre veux-tu demander dans Seerr ? Donne-moi le nom du film ou de la série, et la saison si nécessaire."}
		}
		if plan.Action == "chat" {
			reply = plan.Text
		} else {
			var result ToolReply
			result, err = run(ctx, plan)
			reply, pending = result.Content, len(result.Buttons) > 0
			if err != nil {
				return "", ErrUnavailable
			}
		}
		if reply == "" || utf8.RuneCountInString(reply) > 1500 {
			return "", ErrUnavailable
		}
	}
	c.mu.Lock()
	s.pending = pending
	s.messages = append(s.messages, Message{Role: "user", Content: question}, Message{Role: "assistant", Content: reply})
	if len(s.messages) > 4 {
		s.messages = append([]Message(nil), s.messages[len(s.messages)-4:]...)
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

// Remember button results in the private chat, without retaining their tokens.
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
		s.messages = append(s.messages, Message{Role: "user", Content: "J’ai utilisé un bouton de demande."}, Message{Role: "assistant", Content: result.Content})
		if len(s.messages) > 4 {
			s.messages = append([]Message(nil), s.messages[len(s.messages)-4:]...)
		}
	}
}
