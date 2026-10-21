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
Réponds TOUJOURS par un seul objet JSON, sans markdown. UNE action :
Discussion, goûts, provocation : {"action":"chat","text":"ta réponse"}. Un film préféré est un goût, pas une recherche.
Titre sur Plex : {"action":"search","query":"titre","kind":"movie"}.
Conseil : {"action":"recommend","kind":"movie","genre":"thriller"}. Genre anglais ; maxMinutes si limite, unwatched:true si non vu. Omet les filtres non demandés.
Nouveautés : {"action":"recent"}.
État des demandes : {"action":"status"}, query si titre connu.
Demande Seerr : {"action":"prepare","query":"titre","kind":"movie"}. Série : kind:"show", seasons:[2] pour saison 2.
query contient seulement le titre. Résous « le » avec la conversation, même après un goût. Titre ou saison inconnu : demande via chat. Ne reprends aucun titre d'exemple. prepare affiche des boutons ; seul le bouton confirme. N'invente ni catalogue ni statut. Demande imprécise : pose juste la question manquante, sans vanne.`

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
	switch strings.TrimSpace(title) {
	case "", "film", "le film", "un film", "ce film", "série", "serie", "la série", "une série", "cette série", "la serie", "une serie", "cette serie", "the movie", "a movie", "that movie", "the show", "that show":
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
