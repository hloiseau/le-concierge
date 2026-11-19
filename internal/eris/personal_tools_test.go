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
