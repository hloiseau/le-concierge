package eris

import (
	"regexp"
	"strings"
)

const RemovalExplanation = "Je ne peux ni annuler une demande déjà envoyée dans Seerr, ni supprimer un film ou une série de Plex. Pour ça, passe par un admin du serveur ; une annulation dans Seerr ne retire pas le fichier de Plex."

var removalCommand = regexp.MustCompile(`^(?:(?:donc|alors) )?(?:(?:est ce que )?(?:tu (?:peux|pourrais|sais)|(?:peux|pourrais|sais) tu) (?:me |nous |le |la |les |l')?|je (?:veux|voudrais|souhaite) |comment (?:je peux |on peut |faire pour )?)?(supprime[rz]?|supprimes|efface[rz]?|effaces|retire[rz]?|retires|enl[eè]ve[rz]?|enl[eè]ves|annule[rz]?|annules)(?: (.*))?$`)
var nonMediaRemoval = regexp.MustCompile(`(?:^|[^\p{L}])(?:m[ée]moire|souvenirs?|messages?|historique|donn[ée]es|compte|lubies?)(?:$|[^\p{L}])`)

// A clear unsupported operation must never fall through to an add/status tool.
// Bare "annule" still cancels a pending choice through MatchChoice.
func IsMediaRemoval(question string) bool {
	for _, clause := range strings.FieldsFunc(question, func(r rune) bool { return strings.ContainsRune(",;.!?\n", r) }) {
		q := choiceText(clause)
		match := removalCommand.FindStringSubmatch(q)
		if match == nil || nonMediaRemoval.MatchString(q) || strings.HasPrefix(match[2], "est ") {
			continue
		}
		if strings.HasPrefix(match[1], "annul") && (match[2] == "" || match[2] == "stp" || match[2] == "s'il te plaît") {
			continue
		}
		return true
	}
	return false
}
