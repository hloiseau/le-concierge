package eris

import (
	"context"
	"strings"
	"time"
)

const stalledTurnInstructions = `Le dernier tour a bloqué. Reprends l'objectif à partir de la demande précédente, des précisions déjà données et du nouveau message de l'auteur. Si l'auteur a répondu à ta question, utilise sa réponse. Si tu ne peux traiter qu'une partie de la demande, traite-la et indique le blocage précis. Ne redemande pas la même précision ou la même démarche et ne relance pas une écriture qui a pu aboutir.`

const dialogueFallback = "Je bloque sur ma réponse à cette demande."

func repeatedQuestion(previous, current string) bool {
	return strings.ContainsAny(current, "?？") && choiceText(previous) == choiceText(current)
}

// A single conversation-only pass can repair invalid output or a repeated question.
func (c *Chat) recoverChat(ctx context.Context, messages []Message, discord bool, previous Message) (Plan, bool) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	retry := append([]Message(nil), messages...)
	retry[0].Content = c.instructions(discord, false) + "\n" + stalledTurnInstructions +
		` Pour cette réponse, les outils sont indisponibles. Réponds uniquement avec {"action":"chat","text":"ta réponse"}, texte de 1 000 caractères maximum. Si une vérification est nécessaire, indique ce qui manque sans inventer de résultat.`
	raw, err := c.complete(ctx, c.dialoguePrompt(retry), true)
	if err == nil {
		plan, err := ParsePlan(raw)
		if err == nil && plan.Action == "chat" && !repeatedQuestion(previous.Content, plan.Text) {
			return plan, false
		}
	}
	return Plan{Action: "chat", Text: dialogueFallback}, true
}

func acknowledgesAccess(text string) bool {
	text = strings.TrimSuffix(choiceText(text), " cheffe")
	switch text {
	case "c'est fait", "j'ai lié discord", "discord est lié", "c'est déjà lié", "j'ai reconnecté plex", "je me suis reconnecté", "done":
		return true
	default:
		return false
	}
}

func (r ToolReply) needsAccountStep() bool {
	return r.Issue == ToolDiscordLinkRequired || r.Issue == ToolPlexAccessRequired
}

func (r ToolReply) accountRetryReply() string {
	switch r.Issue {
	case ToolDiscordLinkRequired:
		return "J’ai revérifié. Le Hub ne trouve toujours pas de liaison pour ce compte Discord. Vérifie quel compte Discord est affiché dans Mon compte."
	case ToolPlexAccessRequired:
		return "J’ai revérifié. Discord est bien lié et le Hub attend toujours une autorisation Plex valide. Vérifie que le compte Plex utilisé dans le Hub a accès au serveur."
	default:
		return "J’ai retenté la vérification. " + r.Content
	}
}

func (p Plan) isAccountRead() bool {
	switch p.Action {
	case "search", "recommend", "recent", "status", "diagnostic", "cinema", "monitoring", "follows", "gifts":
		return true
	default:
		return false
	}
}
