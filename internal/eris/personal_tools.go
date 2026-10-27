package eris

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (p Plan) validatePersonalTool() error {
	invalid := errors.New("invalid personal tool plan")
	if p.Text != "" || p.Relation != "" || p.Source != "" || p.URL != "" || p.RequestID != 0 || p.Genre != "" || p.MaxMinutes != 0 || p.Unwatched {
		return invalid
	}
	if p.Action == "follow" {
		if strings.TrimSpace(p.Query) != p.Query || strings.ContainsFunc(p.Query, unicode.IsControl) || utf8.RuneCountInString(p.Query) < 2 || utf8.RuneCountInString(p.Query) > 200 || (p.Kind != "" && p.Kind != "movie" && p.Kind != "show") || (p.Year != 0 && (p.Year < 1801 || p.Year > 2999)) || (p.Channel != "" && p.Channel != "hub" && p.Channel != "discord") || len(p.Seasons) > 1 || (len(p.Seasons) > 0 && (p.Kind != "show" || p.Seasons[0] < 1 || p.Seasons[0] > 1000)) {
			return invalid
		}
		return nil
	}
	if p.Kind != "" || len(p.Seasons) > 0 || p.Query != "" {
		return invalid
	}
	if p.Action == "unfollow" && (p.SubscriptionID < 1 || p.SubscriptionID > 2147483647) {
		return invalid
	}
	if p.Action == "preferences" && (p.Setting != "" || p.Value != "") && !validPreference(p.Setting, p.Value) {
		return invalid
	}
	return nil
}

func validPreference(setting, value string) bool {
	switch setting {
	case "enabled", "discordDmEnabled", "allUnwatched", "availableUnwatched":
		return value == "true" || value == "false"
	case "dailyCap":
		n, err := strconv.Atoi(value)
		return err == nil && n >= 1 && n <= 100 && strconv.Itoa(n) == value
	case "quietHours", "daysBefore":
		separator, max, limit := ",", 30, 5
		if setting == "quietHours" {
			separator, max, limit = "-", 23, 2
		}
		parts := strings.Split(value, separator)
		if len(parts) < 1 || len(parts) > limit || (setting == "quietHours" && len(parts) != 2) {
			return false
		}
		seen := map[int]bool{}
		for _, part := range parts {
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 || n > max || strconv.Itoa(n) != part || (setting == "daysBefore" && (n == 0 || seen[n])) {
				return false
			}
			seen[n] = true
		}
		return true
	}
	return false
}

var followIntent = regexp.MustCompile(`(?i)(?:préviens|previens|avertis|notifie|abonne|suivre|suis|alerte)[ -]`)
var stopFollowIntent = regexp.MustCompile(`(?i)(?:arrête|arrete|retire|désabonne|desabonne|supprime|ne .*plus|me .*plus)`)
var preferenceIntent = regexp.MustCompile(`(?i)(?:active|désactive|desactive|coupe|mets|met |règle|regle|change|limite|je veux|ne .*plus|pas de)`)
var directMessageIntent = regexp.MustCompile(`(?i)(?:\bdm\b|priv[ée]|discord)`)

func subscriptionMentioned(id int64, messages []Message) bool {
	pattern := regexp.MustCompile(`(?i)(?:#|suivi\s*(?:n[°o]\s*)?)` + strconv.FormatInt(id, 10) + `(?:$|[^0-9])`)
	for _, message := range messages {
		if message.Role != "system" && pattern.MatchString(message.Content) {
			return true
		}
	}
	return false
}

// The transport supplies this question; model output cannot authorize a write.
func PersonalWriteRequested(question string, p Plan) bool {
	switch p.Action {
	case "follow":
		return followIntent.MatchString(question) && !stopFollowIntent.MatchString(question) && (p.Channel != "discord" || directMessageIntent.MatchString(question))
	case "unfollow":
		return stopFollowIntent.MatchString(question)
	case "preferences":
		return p.Setting == "" || preferenceIntent.MatchString(question)
	default:
		return true
	}
}

const PersonalToolInstructions = `
Suivre une disponibilité sur demande : {"action":"follow","query":"titre seul","kind":"movie","year":2021,"channel":"hub"}. Omet l'année inconnue. Série : kind:"show", seasons:[2] pour une saison précise, sinon premier épisode accessible de la série. channel:"discord" seulement si un DM est demandé ; cela n'active pas le consentement DM. Aucun ajout média implicite.
Consulter mes suivis : {"action":"follows"}. Retirer un suivi : {"action":"unfollow","subscriptionId":7}, avec le numéro donné par l'outil ; sans numéro consulte les suivis avant de demander lequel. Retirer un suivi ne supprime ni une demande Seerr ni un fichier.
Réglages personnels, en privé : {"action":"preferences"}. Modifier un réglage explicitement demandé : {"action":"preferences","setting":"enabled","value":"false"}. Réglages disponibles : enabled (notifications générales), discordDmEnabled, allUnwatched (rappels de retraits non vus), availableUnwatched (relances des demandes disponibles non vues), dailyCap (1–100), quietHours (ex. "22-8", heures Europe/Paris), daysBefore (jours avant retrait, ex. "7,2"). Les booléens sont les chaînes "true"/"false". Un réglage par tour ; demande une précision pour une valeur manquante ou plusieurs réglages distincts. Ne désactive pas les notifications générales pour retirer un seul suivi. N'annonce une modification qu'après le résultat vérifié de l'outil.`
