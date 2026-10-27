package eris

import (
	"strings"
	"time"
	"unicode"
)

const TurnTakingInstructions = `
Conversation publique terminée, simple acquiescement ou réaction sans demande : {"action":"silent"}. Tu peux te taire. Une demande explicite, une question ou un choix média attend une vraie réponse ; en privé réponds normalement.`

// SkipPublicFollowup avoids generating a punchline for an acknowledgement.
// Explicit questions and active media choices remain actionable.
func (c *Chat) SkipPublicFollowup(owner, text string, explicit bool) bool {
	if !c.V2 || publicScope(owner) == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	q := strings.Join(words(text), " ")
	room := c.publicRooms[publicScope(owner)]
	switch q {
	case "tais toi", "tu peux te taire", "ferme ta gueule", "tu peux fermer ta gueule", "arrête de parler", "arrete de parler", "silence":
		if room != nil {
			room.quiet = true
		}
		return true
	}
	if explicit {
		if room != nil {
			room.quiet = false
		}
		return false
	}
	if room != nil && room.quiet {
		return true
	}
	if s := c.sessions[owner]; s != nil && s.pending && time.Now().Before(s.choiceUntil) {
		return false
	}
	switch q {
	case "oui", "ouais", "ouai", "ouaip", "yes", "yep", "ok", "okay", "d accord", "merci", "merci beaucoup", "nickel", "parfait", "super", "c est ça", "c est ca", "ouais c est ça", "ouai c est ça", "ouai c est ca", "c est bon", "tkt", "tkt pas", "mdr", "ptdr", "jpp", "jpp l émoji à la fin", "ah pas mal":
		return true
	}
	// Emoji-only responses are reactions, not a request for another turn.
	return strings.TrimSpace(text) != "" && !strings.ContainsAny(text, "?？") && !strings.ContainsFunc(text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) })
}

// ObserveAmbient supplies short-lived context in an already active room. Only
// the Gateway with Message Content enabled calls this; it never generates replies
// or persists the ambient message itself.
func (c *Chat) ObserveAmbient(owner, text string, incoming DiscordInput) {
	incoming = incoming.normalized(owner)
	if !c.V2 || publicScope(owner) == "" || incoming.Speaker.ID == "" || strings.TrimSpace(text) == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	room := c.publicRooms[publicScope(owner)]
	if room == nil || time.Since(room.at) > 30*time.Minute {
		return
	}
	incoming.Ambient = true
	c.observePublic(owner, shortContext(text, 500), incoming, time.Now())
}
