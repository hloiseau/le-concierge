package eris

import "testing"

func TestDiscordPlansCannotChooseDestinationIdentityOrInvalidPayload(t *testing.T) {
	for _, raw := range []string{`{"action":"poll","text":"Quel film ?","options":["Alien","Arrival"]}`, `{"action":"event","text":"Soirée Alien","startAt":"2027-01-10T21:00:00+01:00","endAt":"2027-01-10T23:00:00+01:00"}`} {
		if _, err := ParsePlan(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{
		`{"action":"poll","text":"Quel film ?","options":["Alien"]}`,
		`{"action":"poll","text":"Quel film ?","options":["Alien","ALIEN"]}`,
		`{"action":"poll","text":"@everyone","options":["A","B"]}`,
		`{"action":"poll","text":"Question","options":["A","B"],"channelId":"12345"}`,
		`{"action":"poll","text":"Question","options":["A","B"],"durationHours":9999}`,
		`{"action":"event","text":"Soirée","startAt":"2027-01-10T21:00:00+01:00","endAt":"2027-01-10T20:00:00+01:00"}`,
		`{"action":"event","text":"Soirée","startAt":"2027-01-10T21:00:00+01:00","endAt":"2027-01-12T21:00:00+01:00"}`,
		`{"action":"web","query":"test","multiselect":true}`,
		`{"action":"preferences","options":["A","B"]}`,
	} {
		if _, err := ParsePlan(raw); err == nil {
			t.Fatal("invalid creation plan accepted", raw)
		}
	}
}

func TestDiscordWritesNeedCreationIntentAndPublicGrounding(t *testing.T) {
	if DiscordCreationRequested("J'aime les sondages", Plan{Action: "poll"}) || !DiscordCreationRequested("Fais un sondage", Plan{Action: "poll"}) {
		t.Fatal("creation intent not distinguished")
	}
	if !PublicTextMentioned("Alien (1979)", "Crée un sondage Alien 1979 ou Arrival", "") || PublicTextMentioned("PRIVATE SECRET", "Crée un sondage Alien ou Arrival", "") {
		t.Fatal("payload not grounded")
	}
}
