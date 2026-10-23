package eris

import "testing"

func TestExplicitRequestRequiresExactCatalogueScope(t *testing.T) {
	for _, tc := range []struct {
		text, title, year, kind string
		seasons                 []int
		want                    bool
	}{
		{"Demande Arrival", "Arrival", "2016", "movie", nil, true},
		{"Tu peux me rajouter le film Arrival dans Seerr stp ?", "Arrival", "2016", "movie", nil, true},
		{"Ajoute Arrival (2016)", "Arrival", "2016", "movie", nil, true},
		{"Peux-tu demander Arrival pour moi ?", "Arrival", "2016", "movie", nil, true},
		{"Demande la saison 2 de Severance", "Severance", "2022", "tv", []int{2}, true},
		{"Ajoute Severance saison 2", "Severance", "2022", "tv", []int{2}, true},
		{"Ne demande pas Arrival", "Arrival", "2016", "movie", nil, false},
		{"Si tu peux, demande Arrival", "Arrival", "2016", "movie", nil, false},
		{"Demande Arrival mais pas maintenant", "Arrival", "2016", "movie", nil, false},
		{"Demande Arrival si j'ai de la place", "Arrival", "2016", "movie", nil, false},
		{"Tu as Arrival ?", "Arrival", "2016", "movie", nil, false},
		{"Recommande un film à demander comme Arrival", "Arrival", "2016", "movie", nil, false},
		{"Je cite : demande Arrival", "Arrival", "2016", "movie", nil, false},
		{"Demande Arrival", "Arrival 2", "2016", "movie", nil, false},
		{"Demande Arrival (1996)", "Arrival", "2016", "movie", nil, false},
		{"Demande le film Severance", "Severance", "2022", "tv", []int{2}, false},
		{"Demande Severance", "Severance", "2022", "tv", []int{2}, false},
		{"Demande Severance saison 1", "Severance", "2022", "tv", []int{2}, false},
		{"Demande Severance saison 2", "Severance", "2022", "tv", []int{1, 2}, false},
	} {
		if got := ExplicitRequest(tc.text, tc.title, tc.year, tc.kind, tc.seasons); got != tc.want {
			t.Errorf("%q: %v want %v", tc.text, got, tc.want)
		}
	}
}

func TestChoiceDisambiguatesRemakesByYear(t *testing.T) {
	choices := []Button{{ID: "eris:choose:a", Title: "Dune", Year: "1984", Number: 1}, {ID: "eris:choose:b", Title: "Dune", Year: "2021", Number: 2}}
	for _, q := range []string{"celui de 2021", "Dune (2021)", "demande Dune 2021"} {
		if id, ok := MatchChoice(q, choices); !ok || id != "eris:choose:b" {
			t.Fatal(q, id, ok)
		}
	}
	for _, q := range []string{"Dune", "celui de 2021 mais pas maintenant"} {
		if id, _ := MatchChoice(q, choices); id != "" {
			t.Fatal(q, id)
		}
	}
}
