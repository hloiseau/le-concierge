package eris

import (
	"strconv"
	"strings"
)

// ExplicitRequest matches the whole verified utterance to catalogue metadata.
// Anything conditional, negative or unresolved falls back to a visible choice.
func ExplicitRequest(text, title, year, kind string, seasons []int) bool {
	q := choiceText(text)
	for _, suffix := range []string{" s'il te plaît", " s'il te plait", " stp", " merci", " pour moi", " dans seerr", " sur seerr", " sur plex", " à plex"} {
		q = strings.TrimSuffix(q, suffix)
	}
	found := false
	for _, prefix := range []string{"tu peux me ", "tu peux ", "peux tu me ", "peux tu ", "pourrais tu me ", "pourrais tu "} {
		q = strings.TrimPrefix(q, prefix)
	}
	for _, prefix := range []string{"demande moi ", "ajoute moi ", "rajoute moi ", "mets moi ", "demande ", "ajoute ", "rajoute ", "mets ", "demander ", "ajouter ", "rajouter ", "mettre "} {
		if strings.HasPrefix(q, prefix) {
			q, found = strings.TrimPrefix(q, prefix), true
			break
		}
	}
	if !found || title == "" {
		return false
	}
	titles := []string{choiceText(title)}
	if year != "" {
		titles = append(titles, choiceText(title+" ("+year+")"), choiceText(title+" "+year), choiceText(title+" de "+year))
	}
	for _, name := range titles {
		if kind == "movie" && len(seasons) == 0 && (q == name || q == "le film "+name) {
			return true
		}
		if kind == "tv" && len(seasons) == 1 {
			n := strconv.Itoa(seasons[0])
			if q == name+" saison "+n || q == "la série "+name+" saison "+n || q == "la saison "+n+" de "+name {
				return true
			}
		}
	}
	return false
}
