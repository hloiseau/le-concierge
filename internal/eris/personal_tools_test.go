package eris

import "testing"

func TestPersonalPlansBoundChangesAndKeepOtherToolsStrict(t *testing.T) {
	for _, raw := range []string{
		`{"action":"follow","query":"Dune","year":2021,"kind":"movie"}`,
		`{"action":"follow","query":"Dark","kind":"show","seasons":[2],"channel":"discord"}`,
		`{"action":"follows"}`, `{"action":"unfollow","subscriptionId":7}`, `{"action":"preferences"}`,
		`{"action":"preferences","setting":"quietHours","value":"22-8"}`,
		`{"action":"preferences","setting":"daysBefore","value":"7,2"}`,
		`{"action":"preferences","setting":"discordDmEnabled","value":"true"}`,
	} {
		if _, err := ParsePlan(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{
		`{"action":"follow","query":"Dark","kind":"show","seasons":[1,2]}`,
		`{"action":"follow","query":"Dark","channel":"everyone"}`,
		`{"action":"unfollow","subscriptionId":-1}`, `{"action":"unfollow","query":"Dune"}`,
		`{"action":"preferences","setting":"admin","value":"true"}`,
		`{"action":"preferences","setting":"quietHours","value":"25-8"}`,
		`{"action":"preferences","setting":"daysBefore","value":"7,7"}`,
		`{"action":"preferences","setting":"dailyCap","value":"0"}`,
		`{"action":"preferences","setting":"enabled","value":"1"}`,
		`{"action":"web","query":"test","setting":"enabled","value":"false"}`,
		`{"action":"diagnostic","requestId":7,"subscriptionId":2}`,
		`{"action":"cinema","query":"Dune","channel":"discord"}`,
		`{"action":"chat","text":"salut","subscriptionId":2}`,
	} {
		if _, err := ParsePlan(raw); err == nil {
			t.Fatal("invalid personal plan accepted", raw)
		}
	}
}

func TestPersonalChangesNeedIntentAndUnfollowIsNotMediaRemoval(t *testing.T) {
	for _, test := range []struct {
		q    string
		p    Plan
		want bool
	}{
		{"Préviens-moi quand Dune arrive", Plan{Action: "follow"}, true},
		{"Préviens-moi quand Dune arrive", Plan{Action: "follow", Channel: "discord"}, false},
		{"Préviens-moi en DM quand Dune arrive", Plan{Action: "follow", Channel: "discord"}, true},
		{"Tu as Dune ?", Plan{Action: "follow"}, false},
		{"Ne me préviens plus pour Dune", Plan{Action: "follow"}, false},
		{"Retire mon suivi #7", Plan{Action: "unfollow"}, true},
		{"Quels sont mes suivis ?", Plan{Action: "unfollow"}, false},
		{"Désactive mes notifications", Plan{Action: "preferences", Setting: "enabled"}, true},
		{"Mes réglages ?", Plan{Action: "preferences", Setting: "enabled"}, false},
	} {
		if got := PersonalWriteRequested(test.q, test.p); got != test.want {
			t.Fatal(test, got)
		}
	}
	for _, q := range []string{"retire mon suivi Arrival", "supprime le suivi #7", "annule l’alerte de disponibilité"} {
		if IsMediaRemoval(q) {
			t.Fatal("unfollow intercepted as media removal", q)
		}
	}
	if !IsMediaRemoval("supprime Arrival de Plex") {
		t.Fatal("media removal guard lost")
	}
}

func TestUnfollowIDMustComeFromRecentConversation(t *testing.T) {
	if subscriptionMentioned(7, []Message{{Role: "system", Content: "suivi #7"}, {Role: "user", Content: "retire #77"}}) {
		t.Fatal("prompt example or ID prefix grounded deletion")
	}
	if !subscriptionMentioned(7, []Message{{Role: "assistant", Content: "Suivi #7 enregistré pour Arrival."}}) {
		t.Fatal("real offered follow not found")
	}
}
