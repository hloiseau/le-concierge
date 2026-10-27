package eris

func validAnimeGenre(genre string) bool {
	switch genre {
	case "Action", "Adventure", "Comedy", "Drama", "Fantasy", "Horror", "Mahou Shoujo", "Mecha", "Music", "Mystery", "Psychological", "Romance", "Sci-Fi", "Slice of Life", "Sports", "Supernatural", "Thriller":
		return true
	}
	return false
}

const AnimeToolInstructions = `
Informations publiques anime/manga : {"action":"anime","query":"titre","kind":"anime"}, kind:"manga" pour un manga. Année facultative avec year si elle est explicitement donnée pour identifier l'œuvre. Titres alternatifs pris en compte ; ambiguïté = précision.
Recommandations anime/manga : relation:"recommend", avec query pour une œuvre de référence, ou sans query et avec genre anglais facultatif (ex. "Fantasy") pour des suggestions générales. Ces recommandations viennent du catalogue public, pas de l'historique du membre. Aucune disponibilité NAS ni demande de livre implicite.
Sortie/édition française d'un manga : {"action":"anime","query":"titre, tome et édition demandés","kind":"manga","relation":"french"}. L'outil vérifie séparément les pages d'éditeurs. Sans tome/édition utile, demande une précision. Une date d'origine AniList n'est jamais une date française. Pour une annonce française d'anime, utilise le web et des sources officielles ; ne remplace pas l'information manquante par une date japonaise.`
