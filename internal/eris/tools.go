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
Un seul objet JSON, une action, aucun markdown :
Discussion, goût, avis entre des titres cités : {"action":"chat","text":"réponse"}, pas de recherche spontanée.
« Tu as X sur Plex ? » : {"action":"search","query":"X","kind":"movie"}, jamais prepare.
Conseil par genre : {"action":"recommend","genre":"thriller","kind":"movie"}. Genre anglais, maxMinutes si limite, unwatched:true si non vu.
Film similaire : {"action":"recommend","query":"titre de référence","relation":"similar","kind":"movie"}.
« Du même réal ? » reste une recherche catalogue : {"action":"recommend","query":"titre de référence","relation":"director","source":"seerr","kind":"movie"}. Garde le catalogue de la conversation.
source:"seerr" si demandé, sinon Plex. query=TITRE DE RÉFÉRENCE. « Recommande un film à demander » = recommend, jamais prepare. Par référence : films seulement, sans genre/durée.
Nouveautés : {"action":"recent"}. Constantes, CPU/RAM, état du NAS : {"action":"monitoring"}.
État des demandes : {"action":"status"}, query si titre connu.
« Demande/ajoute X dans Seerr » : {"action":"prepare","query":"X","kind":"movie"}. Série : kind:"show", seasons:[2].
query=titre seul. Résous les pronoms avec la conversation. Omet les filtres non demandés. Titre/saison manquant : chat. L'application seule confirme/envoie les demandes. N'invente aucun résultat.`

type Plan struct {
	Action     string `json:"action"`
	Query      string `json:"query,omitempty"`
	Relation   string `json:"relation,omitempty"`
	Source     string `json:"source,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Genre      string `json:"genre,omitempty"`
	MaxMinutes int    `json:"maxMinutes,omitempty"`
	Unwatched  bool   `json:"unwatched,omitempty"`
	Seasons    []int  `json:"seasons,omitempty"`
	Text       string `json:"text,omitempty"`
}

var unseenWords = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(non[- ]vus?|pas(?: encore| déjà)? (?:regard[ée]s?|vus?)|jamais (?:regard[ée]s?|vus?)|unwatched|unseen|haven.t (?:watched|seen))(?:$|[^\p{L}])`)
var requestFollowup = regexp.MustCompile(`(?i)^(?:(?:alors )?demande[ -](?:le|la)|(?:tu peux|peux[- ]tu|pourrais[- ]tu) (?:le|la) demander)(?: pour moi)?(?: dans seerr)?[.!? ]*$`)
var sameDirector = regexp.MustCompile(`(?i)(?:même|meme) (?:réal(?:isateur|isatrice)?|real(?:isateur|isatrice)?|director)(?:$|[^\p{L}])`)
var outsidePlex = regexp.MustCompile(`(?i)(?:pas(?: encore| déjà)? (?:sur|dans)(?: mon|ton|le)? plex|absents? (?:de|du) plex|hors plex)`)

// Preserve explicit constraints when the model drops them or repeats a search.
func (p Plan) WithQuestionConstraints(question string) Plan {
	question = strings.Join(strings.Fields(question), " ")
	if (p.Action == "prepare" || p.Action == "search" || p.Action == "recommend") && p.Query != "" && p.Kind != "show" && sameDirector.MatchString(question) {
		source := p.Source
		if strings.Contains(strings.ToLower(question), "seerr") {
			source = "seerr"
		}
		p = Plan{Action: "recommend", Query: p.Query, Kind: "movie", Relation: "director", Source: source, Unwatched: p.Unwatched}
	}
	if p.Action == "recommend" && unseenWords.MatchString(question) {
		p.Unwatched = true
	}
	if p.Action == "recommend" && p.Relation != "" && outsidePlex.MatchString(question) {
		p.Source, p.Unwatched = "seerr", false
	}
	if (p.Action == "search" || p.Action == "recommend") && p.Query != "" && requestFollowup.MatchString(question) {
		p = Plan{Action: "prepare", Query: p.Query, Kind: p.Kind, Seasons: p.Seasons}
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
		if p.Text != "" || (p.Source != "" && p.Source != "plex" && p.Source != "seerr") || (p.Relation != "" && p.Relation != "similar" && p.Relation != "director") {
			return invalid
		}
		if (p.Query == "") != (p.Relation == "") || (p.Relation != "" && (utf8.RuneCountInString(p.Query) < 2 || p.Kind == "show" || p.Genre != "" || p.MaxMinutes != 0)) || (p.Source == "seerr" && (p.Relation == "" || p.Unwatched)) {
			return invalid
		}
	case "recent", "status", "monitoring":
		if p.Text != "" || p.Kind != "" || (p.Action != "status" && p.Query != "") {
			return invalid
		}
	default:
		return invalid
	}
	if p.Action != "recommend" && (p.Genre != "" || p.MaxMinutes != 0 || p.Unwatched) {
		return invalid
	}
	if p.Action != "recommend" && (p.Relation != "" || p.Source != "") {
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
	Choice      string          `json:"choice,omitempty"`
	Interaction json.RawMessage `json:"interaction,omitempty"`
	Timestamp   string          `json:"timestamp,omitempty"`
	Signature   string          `json:"signature,omitempty"`
	ChannelID   string          `json:"channelId,omitempty"`
	MessageID   string          `json:"messageId,omitempty"`
}

type Button struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Title  string `json:"title,omitempty"`
	Number int    `json:"number,omitempty"`
}
type ToolReply struct {
	Content string   `json:"content"`
	Buttons []Button `json:"buttons,omitempty"`
}
