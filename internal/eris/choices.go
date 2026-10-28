package eris

import (
	"strconv"
	"strings"
	"unicode"
)

const ChoiceClarification = "Lequel ? Donne-moi le titre ou son numéro 🍿"

func choiceText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "’", "'")
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, ",", " ")
	return strings.Join(strings.Fields(strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(".!?", r) })), " ")
}

// MatchChoice accepts a whole utterance, never a fragment inside a longer message.
// Both the bot and the API use it; the API checks the original Discord text.
func MatchChoice(text string, choices []Button) (string, bool) {
	if len(choices) == 0 {
		return "", false
	}
	q := choiceText(text)
	for _, word := range []string{"non", "non merci", "annule", "annuler", "laisse tomber", "cancel"} {
		if q == word {
			parts := strings.Split(choices[0].ID, ":")
			if len(parts) == 3 {
				return "eris:cancel:" + parts[2], true
			}
		}
	}
	yes := false
	for _, word := range []string{"oui", "oui vas y", "vas y", "ok", "oui merci", "je confirme", "confirme", "confirmer", "oui ajoute le", "ajoute le", "demande le"} {
		yes = yes || q == word
	}
	if strings.HasPrefix(choices[0].ID, "eris:confirm:") {
		if yes {
			return choices[0].ID, true
		}
		return "", false
	}
	if yes {
		if len(choices) == 1 {
			return choices[0].ID, true
		}
		return "", true
	}
	prefixes := []string{"je veux ", "je prends ", "prends ", "demande ", "ajoute ", "choisis ", "choisir "}
	if choices[0].Operation == "unfollow" {
		prefixes = append(prefixes, "retire ", "supprime ", "arrête ", "arrete ")
	}
	if choices[0].Operation == "follow" {
		prefixes = append(prefixes, "suis ", "suivre ")
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(q, prefix) {
			q = strings.TrimPrefix(q, prefix)
			break
		}
	}
	q = strings.TrimSuffix(q, " s'il te plaît")
	q = strings.TrimSuffix(q, " stp")
	ordinals := map[int][]string{1: {"premier", "1er"}, 2: {"deuxième", "deuxieme", "second", "2e", "2ème"}, 3: {"troisième", "troisieme", "3e", "3ème"}}
	match := ""
	for _, b := range choices {
		if !strings.HasPrefix(b.ID, "eris:choose:") {
			continue
		}
		selected := b.Title != "" && foldTitleAccents(q) == foldTitleAccents(choiceText(b.Title))
		if b.Year != "" && b.Title != "" {
			selected = selected || foldTitleAccents(q) == foldTitleAccents(choiceText(b.Title+" ("+b.Year+")")) || foldTitleAccents(q) == foldTitleAccents(choiceText(b.Title+" "+b.Year)) || q == "celui de "+b.Year || q == b.Year || q == "le "+b.Year
		}
		number := strconv.Itoa(b.Number)
		for _, word := range append([]string{number, "le " + number, "numéro " + number, "le numéro " + number}, ordinals[b.Number]...) {
			selected = selected || (b.Number > 0 && (q == word || q == "le "+word))
		}
		if selected {
			if match != "" {
				return "", true
			}
			match = b.ID
		}
	}
	return match, match != ""
}

// Keep the API's exact titles, seasons and consent wording; only add chat guidance.
func NaturalChoices(reply ToolReply) ToolReply {
	if len(reply.Buttons) == 0 {
		return reply
	}
	hint := "\nPour choisir, donne-moi le titre ou le numéro de la proposition."
	if strings.HasPrefix(reply.Buttons[0].ID, "eris:confirm:") {
		hint = "\nUn « oui, vas-y » pour confirmer, ou « annule »."
	}
	runes := []rune(reply.Content)
	if len(runes)+len([]rune(hint)) > 1500 {
		reply.Content = string(runes[:1500-len([]rune(hint))-1]) + "…"
	}
	reply.Content += hint
	return reply
}
