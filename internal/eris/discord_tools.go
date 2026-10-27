package eris

import (
	"errors"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"
	"unicode/utf8"
)

func (p Plan) validateDiscordCreation() error {
	bad := errors.New("invalid Discord creation")
	if p.Kind != "" || p.Genre != "" || p.MaxMinutes != 0 || p.Unwatched || len(p.Seasons) > 0 || p.Relation != "" || p.Source != "" || p.URL != "" || p.Year != 0 || p.RequestID != 0 || p.SubscriptionID != 0 || p.Channel != "" || p.Setting != "" || p.Value != "" {
		return bad
	}
	valid := func(s string, max int) bool {
		return strings.TrimSpace(s) == s && s != "" && utf8.RuneCountInString(s) <= max && !strings.ContainsFunc(s, unicode.IsControl) && !strings.Contains(s, "<@") && !strings.Contains(s, "@everyone") && !strings.Contains(s, "@here")
	}
	if p.Action == "poll" {
		if !valid(p.Text, 300) || p.Query != "" || len(p.Options) < 2 || len(p.Options) > 10 || p.DurationHours < 0 || p.DurationHours > 768 {
			return bad
		}
		seen := map[string]bool{}
		for _, option := range p.Options {
			key := strings.ToLower(option)
			if !valid(option, 55) || seen[key] {
				return bad
			}
			seen[key] = true
		}
		return nil
	}
	start, e1 := time.Parse(time.RFC3339, p.StartAt)
	end, e2 := time.Parse(time.RFC3339, p.EndAt)
	if !valid(p.Text, 100) || utf8.RuneCountInString(p.Query) > 200 || strings.ContainsFunc(p.Query, unicode.IsControl) || e1 != nil || e2 != nil || !end.After(start) || end.Sub(start) > 24*time.Hour {
		return bad
	}
	return nil
}

var publishIntent = regexp.MustCompile(`(?i)(?:crée|cree|créer|creer|fais|faire|lance|lancer|organise|organiser|programme|programmer)`)
var pollWord = regexp.MustCompile(`(?i)(?:sondage|vote)`)
var eventWord = regexp.MustCompile(`(?i)(?:soir[ée]e|[ée]v[ée]nement)`)

func DiscordCreationRequested(question string, p Plan) bool {
	if !publishIntent.MatchString(question) {
		return false
	}
	return p.Action == "poll" && pollWord.MatchString(question) || p.Action == "event" && eventWord.MatchString(question)
}

func PublicTextMentioned(text, question, reference string) bool {
	normalize := func(s string) string { return " " + strings.Join(words(s), " ") + " " }
	return strings.Contains(normalize(question), normalize(text)) || reference != "" && strings.Contains(normalize(reference), normalize(text))
}

const DiscordToolInstructions = `
Sondage Discord explicitement demandé : {"action":"poll","text":"question","options":["choix A","choix B"],"durationHours":24}. Deux à dix choix réellement donnés dans le message ou une référence publique, jamais issus d'une mémoire privée. Question explicite si donnée ; sinon question neutre. Durée par défaut 24 h, multiselect:true seulement si demandé. Il sera publié dans le salon actuel ; aucun envoi avant résultat de l'application.
Soirée/événement Discord explicitement demandé : {"action":"event","text":"nom donné","startAt":"date RFC3339 avec décalage","endAt":"date RFC3339 avec décalage"}. Nom, date, début et fin doivent être connus ; demande les précisions manquantes. Heures par défaut Europe/Paris, utilise l'horloge fournie pour les dates relatives. L'événement renvoie au salon actuel, sans rejoindre de salon vocal. Une proposition ou une question sur les possibilités reste chat. Aucun autre salon, serveur ou participant à notifier choisi par le modèle.`
