package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
)

const interestSlash = `{"id":"99999","application_id":"12345","guild_id":"23456","channel_id":"98765","type":2,"token":"private-interaction-token","member":{"user":{"id":"11111"}},"data":{"name":"salons"}}`
const interestOpen = `{"id":"99999","application_id":"12345","guild_id":"23456","channel_id":"34567","type":3,"token":"private-interaction-token","member":{"user":{"id":"11111"}},"message":{"id":"99998","flags":0,"author":{"id":"12345"}},"data":{"custom_id":"interests:open","component_type":2}}`
const interestSet = `{"id":"99999","application_id":"12345","guild_id":"23456","channel_id":"34567","type":3,"token":"private-interaction-token","member":{"user":{"id":"11111"}},"message":{"id":"99997","flags":64,"author":{"id":"12345"}},"data":{"custom_id":"interests:set:11111:REVISION","component_type":3,"values":["jeux"]}}`

type interestReply struct {
	Content         string `json:"content"`
	Flags           int    `json:"flags"`
	AllowedMentions struct {
		Parse []string `json:"parse"`
	} `json:"allowed_mentions"`
	Components []struct {
		Type       int `json:"type"`
		Components []struct {
			Type      int    `json:"type"`
			CustomID  string `json:"custom_id"`
			Label     string `json:"label"`
			MinValues int    `json:"min_values"`
			MaxValues int    `json:"max_values"`
			Options   []struct {
				Label   string `json:"label"`
				Value   string `json:"value"`
				Default bool   `json:"default"`
			} `json:"options"`
		} `json:"components"`
	} `json:"components"`
}

type interestHTTPFixture struct {
	t                      *testing.T
	stages, writes         []string
	callbacks              []int
	replies                []interestReply
	roles                  map[string]bool
	ackStatus, rolesStatus int
	failWrite, writeStatus int
	writeAttempts          int
}

func signedInterest(t *testing.T, body string) (*Bot, *http.Request, *interestHTTPFixture) {
	t.Helper()
	c := config.Config{
		DiscordClientID: "12345", DiscordGuildID: "23456", DiscordBotToken: "bot-fixture-token", DiscordInterestsChannelID: "34567",
		DiscordInterests: []config.DiscordInterest{
			{Key: "films", Label: "Films et séries", RoleID: "45678", ChannelID: "67890"},
			{Key: "jeux", Label: "Jeux vidéo", RoleID: "56780", ChannelID: "78901"},
		},
	}
	// Keep the normal signature helper while deliberately omitting every API/Plex setting.
	body = strings.ReplaceAll(body, "REVISION", NewBot(c).interestRevision())
	b, r := signedInteraction(t, body)
	c.DiscordPublicKey = b.Config.DiscordPublicKey
	b.Config = c
	f := &interestHTTPFixture{t: t, roles: map[string]bool{"45678": true, "77777": true}, ackStatus: 204, rolesStatus: 200, writeStatus: 403}
	b.Client.Transport = transportFunc(f.roundTrip)
	return b, r, f
}

func (f *interestHTTPFixture) roundTrip(r *http.Request) (*http.Response, error) {
	f.stages = append(f.stages, r.Method+" "+r.URL.Path)
	if r.URL.Host != "discord.com" {
		f.t.Error("interests must not call the Concierge API or Plex")
		return botResponse(500, `{}`), nil
	}
	if strings.HasSuffix(r.URL.Path, "/callback") {
		var callback struct {
			Type int `json:"type"`
			Data struct {
				Flags int `json:"flags"`
			} `json:"data"`
		}
		if json.NewDecoder(r.Body).Decode(&callback) != nil || r.Method != "POST" || (callback.Type == 5 && callback.Data.Flags != 64) {
			f.t.Error("invalid private callback")
		}
		f.callbacks = append(f.callbacks, callback.Type)
		return botResponse(f.ackStatus, `{}`), nil
	}
	if strings.HasPrefix(r.URL.Path, "/api/v10/webhooks/") {
		var reply interestReply
		if json.NewDecoder(r.Body).Decode(&reply) != nil || r.Method != "PATCH" || !strings.HasSuffix(r.URL.Path, "/messages/@original") || reply.Flags != 64 || reply.Components == nil || reply.AllowedMentions.Parse == nil || len(reply.AllowedMentions.Parse) != 0 {
			f.t.Error("interests reply must edit only the private interaction response, with no mentions")
		}
		f.replies = append(f.replies, reply)
		return botResponse(200, `{}`), nil
	}
	if r.Header.Get("Authorization") != "Bot bot-fixture-token" {
		f.t.Error("role access did not use the bot credential")
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v10/guilds/23456/roles":
		if f.rolesStatus != 200 {
			return botResponse(f.rolesStatus, `{"retry_after":30}`), nil
		}
		return botResponse(200, `[{"id":"23456","permissions":"0","position":0,"managed":false},{"id":"34568","permissions":"268435456","position":20,"managed":false},{"id":"45678","permissions":"0","position":3,"managed":false},{"id":"56780","permissions":"0","position":2,"managed":false},{"id":"77777","permissions":"0","position":1,"managed":false}]`), nil
	case "GET /api/v10/guilds/23456/members/12345":
		return botResponse(200, `{"user":{"id":"12345"},"roles":["34568"]}`), nil
	case "GET /api/v10/guilds/23456/members/11111":
		roles := []string{}
		for id, present := range f.roles {
			if present {
				roles = append(roles, id)
			}
		}
		slices.Sort(roles)
		body, _ := json.Marshal(map[string]any{"user": map[string]string{"id": "11111"}, "roles": roles})
		return botResponse(200, string(body)), nil
	}
	for _, id := range []string{"45678", "56780"} {
		if r.URL.Path == "/api/v10/guilds/23456/members/11111/roles/"+id && (r.Method == "PUT" || r.Method == "DELETE") {
			f.writeAttempts++
			if f.writeAttempts == f.failWrite {
				return botResponse(f.writeStatus, `{"retry_after":30}`), nil
			}
			f.writes = append(f.writes, r.Method+" "+id)
			f.roles[id] = r.Method == "PUT"
			return botResponse(204, ""), nil
		}
	}
	f.t.Errorf("unexpected Discord request: %s %s", r.Method, r.URL.Path)
	return botResponse(500, `{}`), nil
}

func assertInterestMenu(t *testing.T, b *Bot, reply interestReply, selected map[string]bool) {
	t.Helper()
	if len(reply.Components) != 2 || reply.Components[0].Type != 1 || len(reply.Components[0].Components) != 1 || len(reply.Components[1].Components) != 1 {
		t.Fatalf("missing private select and clear button: %+v", reply.Components)
	}
	menu, clear := reply.Components[0].Components[0], reply.Components[1].Components[0]
	if menu.Type != 3 || menu.CustomID != "interests:set:11111:"+b.interestRevision() || menu.MinValues != 1 || menu.MaxValues != 2 || len(menu.Options) != 2 || clear.Type != 2 || clear.CustomID != "interests:clear:11111:"+b.interestRevision() {
		t.Fatalf("menu escaped the configured choices/owner: %+v", reply.Components)
	}
	for index, option := range menu.Options {
		if option.Value != b.Config.DiscordInterests[index].Key || option.Label != b.Config.DiscordInterests[index].Label || option.Default != selected[option.Value] {
			t.Errorf("default is not based on current Discord roles: %+v", option)
		}
	}
}

func TestInterestsOpenPrivatelyWithoutConciergeOrPlex(t *testing.T) {
	for name, body := range map[string]string{"slash in another guild channel": interestSlash, "public panel": interestOpen} {
		t.Run(name, func(t *testing.T) {
			b, r, f := signedInterest(t, body)
			w := httptest.NewRecorder()
			b.Handler().ServeHTTP(w, r)
			b.Wait()
			if w.Code != 202 || w.Body.Len() != 0 || !slices.Equal(f.callbacks, []int{5}) || len(f.replies) != 1 || len(f.writes) != 0 || !strings.HasSuffix(f.stages[0], "/callback") {
				t.Fatalf("unsafe open sequence: %d callbacks=%v stages=%v", w.Code, f.callbacks, f.stages)
			}
			assertInterestMenu(t, b, f.replies[0], map[string]bool{"films": true})
		})
	}
}

func TestInterestsEmptyMembershipKeepsThePrivateChoicesUsable(t *testing.T) {
	b, r, f := signedInterest(t, interestOpen)
	f.roles = map[string]bool{}
	b.interestBusy["22222"] = true
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, r)
	b.Wait()
	if w.Code != 202 || len(f.replies) != 1 || len(f.writes) != 0 {
		t.Fatalf("empty member or another member's operation blocked the menu: %d %v", w.Code, f.stages)
	}
	assertInterestMenu(t, b, f.replies[0], map[string]bool{})
}

func TestInterestsCommandIsRegisteredOnlyWhenConfiguredAndNeverHasPublicOption(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		b, _, _ := signedInterest(t, interestSlash)
		if !enabled {
			b.Config.DiscordInterests = nil
		}
		calls := 0
		b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.Method != "PUT" || r.URL.Path != "/api/v10/applications/12345/guilds/23456/commands" {
				t.Error("unexpected command registration target")
			}
			var commands []struct {
				Name    string `json:"name"`
				Type    int    `json:"type"`
				Options []any  `json:"options"`
			}
			if json.NewDecoder(r.Body).Decode(&commands) != nil {
				t.Error("invalid command catalogue")
			}
			found := 0
			for _, command := range commands {
				if command.Name == "salons" {
					found++
					if command.Type != 1 || len(command.Options) != 0 {
						t.Error("salons must not accept public/identity/role options")
					}
				}
			}
			if (found == 1) != enabled || found > 1 {
				t.Errorf("incorrect optional command registration: enabled=%v count=%d", enabled, found)
			}
			return botResponse(200, `[]`), nil
		})
		if err := b.Register(context.Background()); err != nil || calls != 1 {
			t.Fatalf("registration failed: %v calls=%d", err, calls)
		}
	}
}

func TestInterestsSelectionAndClearUpdateOnlyConfiguredRolesAfterPrivateACK(t *testing.T) {
	for _, clear := range []bool{false, true} {
		body := interestSet
		if clear {
			body = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(body, "interests:set:", "interests:clear:"), `"component_type":3`, `"component_type":2`), `,"values":["jeux"]`, "")
		}
		b, r, f := signedInterest(t, body)
		w := httptest.NewRecorder()
		b.Handler().ServeHTTP(w, r)
		b.Wait()
		wanted := []string{"DELETE 45678", "PUT 56780"}
		selected := map[string]bool{"jeux": true}
		if clear {
			wanted = []string{"DELETE 45678"}
			selected = map[string]bool{}
		}
		if w.Code != 202 || !slices.Equal(f.callbacks, []int{6}) || len(f.replies) != 1 || !slices.Equal(f.writes, wanted) || !f.roles["77777"] || !strings.HasSuffix(f.stages[0], "/callback") {
			t.Fatalf("unsafe role change (clear=%v): %d writes=%v stages=%v", clear, w.Code, f.writes, f.stages)
		}
		assertInterestMenu(t, b, f.replies[0], selected)
		if !strings.Contains(f.replies[0].Content, "enregistrés") {
			t.Fatal("confirmed selection has no success response")
		}
	}
}

func TestInterestsFailedACKPreventsEveryRoleReadAndWrite(t *testing.T) {
	for _, body := range []string{interestOpen, interestSet} {
		b, r, f := signedInterest(t, body)
		f.ackStatus = 404
		w := httptest.NewRecorder()
		b.Handler().ServeHTTP(w, r)
		b.Wait()
		if w.Code != 202 || len(f.stages) != 1 || !strings.HasSuffix(f.stages[0], "/callback") || len(f.replies) != 0 || b.interestBusy["11111"] {
			t.Fatalf("failed ACK reached roles or retained the user lock: %d %v", w.Code, f.stages)
		}
	}
}

func TestInterestsRejectForgedOrStaleSignedComponentsWithoutRemoteCalls(t *testing.T) {
	for _, tc := range []struct {
		name, base, old, replacement string
		status                       int
	}{
		{"foreign owner", interestSet, "interests:set:11111:", "interests:set:22222:", 403},
		{"foreign channel", interestOpen, `"channel_id":"34567"`, `"channel_id":"45679"`, 403},
		{"foreign author", interestOpen, `"author":{"id":"12345"}`, `"author":{"id":"33333"}`, 403},
		{"foreign private author", interestSet, `"author":{"id":"12345"}`, `"author":{"id":"33333"}`, 403},
		{"public forged set", interestSet, `"flags":64`, `"flags":0`, 403},
		{"private forged open", interestOpen, `"flags":0`, `"flags":64`, 403},
		{"old revision", interestSet, "REVISION", "000000000000", 403},
		{"unknown key", interestSet, `["jeux"]`, `["admin"]`, 403},
		{"role id instead of key", interestSet, `["jeux"]`, `["56780"]`, 403},
		{"duplicate keys", interestSet, `["jeux"]`, `["jeux","jeux"]`, 403},
		{"empty select", interestSet, `["jeux"]`, `[]`, 403},
		{"null select", interestSet, `["jeux"]`, `null`, 403},
		{"wrong component", interestSet, `"component_type":3`, `"component_type":2`, 403},
		{"extra custom id", interestSet, "REVISION", "REVISION:extra", 403},
		{"unexpected options", interestSlash, `"name":"salons"`, `"name":"salons","options":[{"name":"public","type":5,"value":true}]`, 403},
		{"foreign guild", interestSlash, `"guild_id":"23456"`, `"guild_id":"34567"`, 403},
		{"DM", interestSlash, `"guild_id":"23456"`, `"guild_id":""`, 403},
		{"foreign app", interestOpen, `"application_id":"12345"`, `"application_id":"34567"`, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, r, f := signedInterest(t, strings.Replace(tc.base, tc.old, tc.replacement, 1))
			w := httptest.NewRecorder()
			b.Handler().ServeHTTP(w, r)
			b.Wait()
			if w.Code != tc.status || len(f.stages) != 0 {
				t.Fatalf("invalid input reached Discord: %d %v", w.Code, f.stages)
			}
		})
	}
}

func TestInterestsConfigChangesInvalidateAnAlreadyOpenedSelector(t *testing.T) {
	for _, field := range []string{"key", "label", "role", "channel", "panel"} {
		t.Run(field, func(t *testing.T) {
			b, r, f := signedInterest(t, interestSet)
			switch field {
			case "key":
				b.Config.DiscordInterests[0].Key = "cinema"
			case "label":
				b.Config.DiscordInterests[0].Label = "Cinéma"
			case "role":
				b.Config.DiscordInterests[0].RoleID = "45679"
			case "channel":
				b.Config.DiscordInterests[0].ChannelID = "67891"
			case "panel":
				b.Config.DiscordInterestsChannelID = "34569"
			}
			w := httptest.NewRecorder()
			b.Handler().ServeHTTP(w, r)
			b.Wait()
			if w.Code != 403 || len(f.stages) != 0 {
				t.Fatalf("stale menu reached roles: %d %v", w.Code, f.stages)
			}
		})
	}
}

func TestInterestsReplayAndTamperedSignatureNeverRepeatTheAction(t *testing.T) {
	b, r, f := signedInterest(t, interestSet)
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, r)
	b.Wait()
	calls := len(f.stages)
	for _, tamper := range []bool{false, true} {
		repeated := r.Clone(r.Context())
		raw := string(body)
		want := 409
		if tamper {
			raw += " "
			want = 401
		}
		repeated.Body = io.NopCloser(strings.NewReader(raw))
		w = httptest.NewRecorder()
		b.Handler().ServeHTTP(w, repeated)
		b.Wait()
		if w.Code != want || len(f.stages) != calls {
			t.Fatalf("replay/tampering repeated roles: %d %v", w.Code, f.stages)
		}
	}
}

func TestInterestsUnavailableBusyAndCooldownRespondPrivatelyWithoutRemoteCalls(t *testing.T) {
	for _, mode := range []string{"unconfigured", "busy", "cooldown", "capacity"} {
		t.Run(mode, func(t *testing.T) {
			b, r, f := signedInterest(t, interestSlash)
			switch mode {
			case "unconfigured":
				b.Config.DiscordInterests = nil
			case "busy":
				b.interestBusy["11111"] = true
			case "cooldown":
				b.interestRetryAt = time.Now().Add(time.Minute)
			case "capacity":
				for n := 0; n < cap(b.slots); n++ {
					b.slots <- struct{}{}
				}
			}
			w := httptest.NewRecorder()
			b.Handler().ServeHTTP(w, r)
			b.Wait()
			var reply struct {
				Type int           `json:"type"`
				Data interestReply `json:"data"`
			}
			if json.Unmarshal(w.Body.Bytes(), &reply) != nil || w.Code != 200 || reply.Type != 4 || reply.Data.Flags != 64 || len(reply.Data.Components) != 0 || len(f.stages) != 0 {
				t.Fatalf("unsafe unavailable response: %d %s %v", w.Code, w.Body, f.stages)
			}
			if mode == "capacity" && b.interestBusy["11111"] {
				t.Fatal("full capacity retained the member lock")
			}
		})
	}
}

func TestInterestsFailuresDoNotPretendAllChoicesWereSaved(t *testing.T) {
	for _, partial := range []bool{false, true} {
		b, r, f := signedInterest(t, interestSet)
		if partial {
			f.failWrite = 2
		} else {
			f.rolesStatus = 403
		}
		w := httptest.NewRecorder()
		b.Handler().ServeHTTP(w, r)
		b.Wait()
		if w.Code != 202 || len(f.replies) != 1 || strings.Contains(f.replies[0].Content, "enregistrés") {
			t.Fatalf("failed update was reported as saved: %v", f.replies)
		}
		if partial {
			if !slices.Equal(f.writes, []string{"DELETE 45678"}) {
				t.Fatalf("unexpected partial changes: %v", f.writes)
			}
			assertInterestMenu(t, b, f.replies[0], map[string]bool{})
		} else if len(f.replies[0].Components) != 0 || len(f.writes) != 0 {
			t.Fatal("unverified roles left an active menu")
		}
		if !f.roles["77777"] {
			t.Fatal("an unrelated role was changed")
		}
	}
}

func TestInterestsDiscordRateLimitSetsBotCooldownWithoutRetrying(t *testing.T) {
	b, r, f := signedInterest(t, interestSet)
	f.rolesStatus = 429
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, r)
	b.Wait()
	if w.Code != 202 || len(f.stages) != 3 || len(f.writes) != 0 || len(f.replies) != 1 || len(f.replies[0].Components) != 0 || !time.Now().Before(b.interestRetryAt) {
		t.Fatalf("rate limit was retried or ignored: %d %v cooldown=%v", w.Code, f.stages, b.interestRetryAt)
	}
}
