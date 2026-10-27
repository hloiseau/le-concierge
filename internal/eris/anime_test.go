package eris

import "testing"

func TestAnimePlanSeparatesPublicCatalogueFrenchEditionAndPlex(t *testing.T) {
	for _, raw := range []string{`{"action":"anime","query":"Cowboy Bebop","kind":"anime"}`, `{"action":"anime","kind":"manga","relation":"recommend","genre":"Fantasy"}`, `{"action":"anime","query":"One Piece tome 114 édition originale","kind":"manga","relation":"french"}`} {
		if _, err := ParsePlan(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{`{"action":"anime","query":"test","kind":"movie"}`, `{"action":"anime","kind":"anime","relation":"recommend","year":2026}`, `{"action":"anime","kind":"manga","relation":"french"}`, `{"action":"anime","kind":"manga","query":"test","unwatched":true}`, `{"action":"anime","kind":"manga","query":"test","subscriptionId":1}`} {
		if _, err := ParsePlan(raw); err == nil {
			t.Fatal("invalid anime plan accepted", raw)
		}
	}
}
