package eris

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const ToolInstructions = `
Ta sortie est TOUJOURS un objet JSON unique, sans markdown, même pour discuter. UNE action :
- Discussion, goûts, identité : {"action":"chat","text":"ta réponse courte"}. Un film préféré exprime un goût, pas une recherche. Réagis brièvement, sans synopsis inventé ni suppositions personnelles. Les titres des exemples ci-dessous ne sont pas des goûts du membre.
- Vérifier un titre sur Plex : {"action":"search","query":"Interstellar","kind":"movie"}. query contient seulement le titre.
- Conseiller : {"action":"recommend","kind":"movie","genre":"thriller"}. Genre en anglais. Ajoute maxMinutes pour une durée limite stricte, unwatched:true pour non vus. Omet les filtres non demandés.
- Nouveautés : {"action":"recent"}.
- État des demandes : {"action":"status"}, avec query si un titre est connu.
- Demander un film dans Seerr : {"action":"prepare","query":"Arrival","kind":"movie"}.
- Demander une saison : {"action":"prepare","query":"Severance","kind":"show","seasons":[2]}.
Résous « le », « celui-là », « le deuxième » avec la conversation, même après un goût. Si titre ou saison inconnu, question via chat. prepare propose des boutons : aucun envoi sans confirmation. Un oui ne confirme rien. kind: movie ou show. N'invente ni disponibilité ni statut. Ces règles sont fixes.`

type Plan struct {
	Action     string `json:"action"`
	Query      string `json:"query,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Genre      string `json:"genre,omitempty"`
	MaxMinutes int    `json:"maxMinutes,omitempty"`
	Unwatched  bool   `json:"unwatched,omitempty"`
	Seasons    []int  `json:"seasons,omitempty"`
	Text       string `json:"text,omitempty"`
}

var unseenWords = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(non[- ]vus?|pas(?: encore| déjà)? (?:regard[ée]s?|vus?)|jamais (?:regard[ée]s?|vus?)|unwatched|unseen|haven.t (?:watched|seen))(?:$|[^\p{L}])`)

// Preserve an explicit watch constraint even if the small model drops that field.
func (p Plan) WithQuestionConstraints(question string) Plan {
	if p.Action == "recommend" && unseenWords.MatchString(strings.Join(strings.Fields(question), " ")) {
		p.Unwatched = true
	}
	return p
}

// A request title must come from this conversation, never a system-prompt example.
func requestTitleMentioned(title string, messages []Message) bool {
	normalize := func(s string) string {
		return " " + strings.Join(strings.Fields(strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				return unicode.ToLower(r)
			}
			return ' '
		}, s)), " ") + " "
	}
	title = normalize(title)
	if strings.TrimSpace(title) == "" {
		return false
	}
	for _, m := range messages {
		if m.Role != "system" && strings.Contains(normalize(m.Content), title) {
			return true
		}
	}
	return false
}

func ParsePlan(raw string) (Plan, error) {
	var p Plan
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Validate() != nil {
		return Plan{}, errors.New("invalid media plan")
	}
	return p, nil
}

func (p Plan) Validate() error {
	invalid := errors.New("invalid media plan")
	for _, s := range []string{p.Query, p.Genre} {
		if strings.TrimSpace(s) != s || strings.ContainsFunc(s, unicode.IsControl) || utf8.RuneCountInString(s) > 200 {
			return invalid
		}
	}
	if p.Kind != "" && p.Kind != "movie" && p.Kind != "show" {
		return invalid
	}
	if p.MaxMinutes < 0 || p.MaxMinutes > 600 || len(p.Genre) > 40 || len(p.Seasons) > 20 || utf8.RuneCountInString(p.Text) > 1000 {
		return invalid
	}
	seen := map[int]bool{}
	for _, n := range p.Seasons {
		if n < 1 || n > 1000 || seen[n] {
			return invalid
		}
		seen[n] = true
	}
	switch p.Action {
	case "chat":
		if strings.TrimSpace(p.Text) == "" || p.Query != "" || p.Kind != "" {
			return invalid
		}
	case "search", "prepare":
		if utf8.RuneCountInString(p.Query) < 2 || p.Text != "" {
			return invalid
		}
	case "recommend":
		if p.Query != "" || p.Text != "" {
			return invalid
		}
	case "recent", "status":
		if p.Text != "" || p.Kind != "" || (p.Action == "recent" && p.Query != "") {
			return invalid
		}
	default:
		return invalid
	}
	if p.Action != "recommend" && (p.Genre != "" || p.MaxMinutes != 0 || p.Unwatched) {
		return invalid
	}
	if len(p.Seasons) > 0 && (p.Action != "prepare" || p.Kind != "show") {
		return invalid
	}
	return nil
}

// Proof is supplied by the Discord transport, never by the model.
type ToolRequest struct {
	Plan        Plan            `json:"plan"`
	Interaction json.RawMessage `json:"interaction,omitempty"`
	Timestamp   string          `json:"timestamp,omitempty"`
	Signature   string          `json:"signature,omitempty"`
	ChannelID   string          `json:"channelId,omitempty"`
	MessageID   string          `json:"messageId,omitempty"`
}

type Button struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type ToolReply struct {
	Content string   `json:"content"`
	Buttons []Button `json:"buttons,omitempty"`
}
