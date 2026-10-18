package httpapi

import (
	"strings"
	"testing"

	"github.com/hloiseau/le-concierge/internal/upstream"
)

func TestRequestPageMessageDistinguishesQualityWithoutGuessing(t *testing.T) {
	fourK, standard := true, false
	page := upstream.RequestPage{Offset: 0, Total: 3, Items: []upstream.Request{
		{Title: "Same title", Status: "available", Is4k: &fourK},
		{Title: "Same title", Status: "processing", Is4k: &standard, Seasons: []int{1, 2}},
		{Title: "Quality absent", Status: "unknown"},
	}}
	message := requestPageMessage(page, "", "https://hub.example")
	for _, expected := range []string{"Same title — Disponible · 4K", "Same title — En cours · Standard · saisons [1 2]", "Quality absent — État inconnu\n", "https://hub.example/demandes"} {
		if !strings.Contains(message, expected) {
			t.Errorf("missing %q: %s", expected, message)
		}
	}
}

func TestRequestPageMessageBoundsTitlesSeasonsAndFooter(t *testing.T) {
	for _, title := range []string{strings.Repeat("a", 5000), strings.Repeat("界🎬", 5000), "@everyone <@12345> **[unsafe]**\nnext"} {
		for _, origin := range []string{"https://hub.example", "https://hub.example/" + strings.Repeat("x", 3000)} {
			page := upstream.RequestPage{Total: 10}
			for n := 0; n < 10; n++ {
				page.Items = append(page.Items, upstream.Request{Title: title, Status: "partial"})
			}
			message := requestPageMessage(page, "partial", origin)
			if len(message) > 2000 || strings.Contains(message, "@everyone") || strings.Contains(message, "<@") || !strings.Contains(message, "Vos demandes et notifications") || !strings.Contains(message, "État : "+statusLabel("partial")) {
				t.Fatalf("unsafe or oversized message (%d): %s", len(message), message)
			}
			page.Items[0].Seasons = make([]int, 10000)
			message = requestPageMessage(page, "partial", origin)
			if len(message) > 2000 || !strings.Contains(message, "Suite dans le Hub") || !strings.Contains(message, "Vos demandes et notifications") {
				t.Fatal("oversized seasons lost the bounded message or footer")
			}
		}
	}
}
