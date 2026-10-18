package discord

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
)

type interestFixture struct {
	roles       []map[string]any
	botRoles    []string
	memberRoles []string
	calls       []string
	writes      []string
	before      func(*http.Request) *http.Response
}

func interestClientFixture(t *testing.T) (interestRoleClient, *interestFixture) {
	t.Helper()
	f := &interestFixture{botRoles: []string{"70000"}, memberRoles: []string{"40000", "60000"}}
	for _, role := range []struct {
		id       string
		position int
		perm     string
	}{{"10000", 0, "0"}, {"40000", 1, "0"}, {"50000", 2, "0"}, {"60000", 9, "8"}, {"70000", 5, "268435456"}} {
		f.roles = append(f.roles, map[string]any{"id": role.id, "position": role.position, "permissions": role.perm, "managed": false})
	}
	c := interestRoleClient{Token: "private-bot-token", GuildID: "10000", BotID: "20000", Interests: []config.DiscordInterest{{Key: "movies", Label: "Films", RoleID: "40000", ChannelID: "80000"}, {Key: "series", Label: "Séries", RoleID: "50000", ChannelID: "90000"}}}
	c.HTTP = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "discord.com" || r.Header.Get("Authorization") != "Bot private-bot-token" {
			t.Fatal("unexpected destination or credential")
		}
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		if f.before != nil {
			if response := f.before(r); response != nil {
				return response, nil
			}
		}
		var body any
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v10/guilds/10000/roles":
			body = f.roles
		case "GET /api/v10/guilds/10000/members/20000":
			body = map[string]any{"user": map[string]string{"id": "20000"}, "roles": f.botRoles}
		case "GET /api/v10/guilds/10000/members/30000":
			body = map[string]any{"user": map[string]string{"id": "30000"}, "roles": f.memberRoles}
		default:
			prefix := "/api/v10/guilds/10000/members/30000/roles/"
			if !strings.HasPrefix(r.URL.Path, prefix) || (r.Method != http.MethodPut && r.Method != http.MethodDelete) || r.Body != nil {
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			id := strings.TrimPrefix(r.URL.Path, prefix)
			if id != "40000" && id != "50000" {
				t.Fatal("unrelated role written")
			}
			f.writes = append(f.writes, r.Method+" "+id)
			if r.Method == http.MethodPut && !slices.Contains(f.memberRoles, id) {
				f.memberRoles = append(f.memberRoles, id)
			} else if r.Method == http.MethodDelete {
				f.memberRoles = slices.DeleteFunc(f.memberRoles, func(role string) bool { return role == id })
			}
			return botResponse(204, ""), nil
		}
		encoded, _ := json.Marshal(body)
		return botResponse(200, string(encoded)), nil
	})}
	return c, f
}

func TestInterestRolesReadAndApplyPreserveUnrelatedRoles(t *testing.T) {
	c, f := interestClientFixture(t)
	roles, err := c.Read(t.Context(), "30000")
	if err != nil || !slices.Equal(roles, []string{"40000", "60000"}) || len(f.writes) != 0 {
		t.Fatalf("read: %v %v", roles, err)
	}
	roles, err = c.Apply(t.Context(), "30000", []string{"series"})
	if err != nil || !slices.Equal(roles, []string{"60000", "50000"}) || !slices.Equal(f.writes, []string{"DELETE 40000", "PUT 50000"}) {
		t.Fatalf("apply: %v %v, writes=%v", roles, err, f.writes)
	}
	roles, err = c.Apply(t.Context(), "30000", []string{"series"})
	if err != nil || len(f.writes) != 2 || !slices.Contains(roles, "60000") {
		t.Fatal("same selection changed roles", err)
	}
	roles, err = c.Apply(t.Context(), "30000", nil)
	if err != nil || !slices.Equal(roles, []string{"60000"}) || len(f.writes) != 3 {
		t.Fatal("empty selection changed unrelated roles", roles, err)
	}
}

func TestInterestRolesRejectInvalidSelectionsBeforeRemoteCalls(t *testing.T) {
	for _, mode := range []string{"unknown key", "duplicate key", "role ID instead of key", "bot actor", "invalid actor", "invalid guild", "duplicate configured role", "everyone role", "empty config"} {
		t.Run(mode, func(t *testing.T) {
			c, f := interestClientFixture(t)
			user, selected := "30000", []string{"movies"}
			switch mode {
			case "unknown key":
				selected = []string{"admin"}
			case "duplicate key":
				selected = []string{"movies", "movies"}
			case "role ID instead of key":
				selected = []string{"40000"}
			case "bot actor":
				user = c.BotID
			case "invalid actor":
				user = "30000/roles/60000"
			case "invalid guild":
				c.GuildID = "10000/../other"
			case "duplicate configured role":
				c.Interests[1].RoleID = c.Interests[0].RoleID
			case "everyone role":
				c.Interests[0].RoleID = c.GuildID
			case "empty config":
				c.Interests = nil
			}
			if _, err := c.Apply(t.Context(), user, selected); err == nil || len(f.calls) != 0 {
				t.Fatalf("invalid input reached Discord: %v %v", f.calls, err)
			}
		})
	}
}

func TestInterestRolesRejectUnsafeConfigurationAndMemberIdentity(t *testing.T) {
	for _, mode := range []string{"permissions", "managed", "equal hierarchy", "higher hierarchy", "missing target", "missing permission", "missing position", "missing managed", "duplicate role", "bot permission revoked", "bot role missing", "wrong bot", "wrong user", "missing member roles", "duplicate member roles"} {
		t.Run(mode, func(t *testing.T) {
			c, f := interestClientFixture(t)
			switch mode {
			case "permissions":
				f.roles[1]["permissions"] = "8"
			case "managed":
				f.roles[1]["managed"] = true
			case "equal hierarchy":
				f.roles[1]["position"] = 5
			case "higher hierarchy":
				f.roles[1]["position"] = 6
			case "missing target":
				f.roles = slices.Delete(f.roles, 1, 2)
			case "missing permission":
				delete(f.roles[1], "permissions")
			case "missing position":
				delete(f.roles[1], "position")
			case "missing managed":
				delete(f.roles[1], "managed")
			case "duplicate role":
				f.roles = append(f.roles, f.roles[1])
			case "bot permission revoked":
				f.roles[4]["permissions"] = "0"
			case "bot role missing":
				f.botRoles = []string{"99999"}
			case "duplicate member roles":
				f.memberRoles = []string{"40000", "40000"}
			default:
				f.before = func(r *http.Request) *http.Response {
					if (mode == "wrong bot" && strings.HasSuffix(r.URL.Path, "/members/20000")) || (mode == "wrong user" && strings.HasSuffix(r.URL.Path, "/members/30000")) {
						return botResponse(200, `{"user":{"id":"99999"},"roles":[]}`)
					}
					if mode == "missing member roles" && strings.HasSuffix(r.URL.Path, "/members/30000") {
						return botResponse(200, `{"user":{"id":"30000"}}`)
					}
					return nil
				}
			}
			if _, err := c.Apply(t.Context(), "30000", []string{"series"}); err == nil || len(f.writes) != 0 {
				t.Fatal("unsafe roles changed", f.writes, err)
			}
		})
	}
}

func TestInterestRolesRevalidateImmediatelyBeforeMutation(t *testing.T) {
	for _, mode := range []string{"bot permission", "role permission", "role hierarchy", "member changed"} {
		t.Run(mode, func(t *testing.T) {
			c, f := interestClientFixture(t)
			reads := 0
			f.before = func(r *http.Request) *http.Response {
				if r.URL.Path == "/api/v10/guilds/10000/roles" {
					reads++
					if reads == 2 {
						switch mode {
						case "bot permission":
							f.roles[4]["permissions"] = "0"
						case "role permission":
							f.roles[1]["permissions"] = "8"
						case "role hierarchy":
							f.roles[1]["position"] = 6
						case "member changed":
							f.memberRoles = []string{"50000", "60000"}
						}
					}
				}
				return nil
			}
			actual, err := c.Apply(t.Context(), "30000", []string{"series"})
			if mode == "member changed" {
				if err != nil || !slices.Equal(actual, []string{"50000", "60000"}) {
					t.Fatal("member state not refreshed", actual, err)
				}
			} else if err == nil {
				t.Fatal("revocation ignored")
			}
			if reads != 2 || len(f.writes) != 0 {
				t.Fatal("mutation used stale checks", reads, f.writes)
			}
		})
	}
}

func TestInterestRolesAdminPermissionStillRequiresLowerHarmlessRoles(t *testing.T) {
	c, f := interestClientFixture(t)
	f.roles[4]["permissions"] = "8"
	if _, err := c.Apply(t.Context(), "30000", []string{"series"}); err != nil {
		t.Fatal(err)
	}
	f.roles[1]["permissions"] = "1024"
	if _, err := c.Read(t.Context(), "30000"); err == nil {
		t.Fatal("administrator bypassed interest role restrictions")
	}
}

func TestInterestRolesPartialFailureReturnsRecheckedStateAndNeverRetries(t *testing.T) {
	for _, status := range []int{403, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c, f := interestClientFixture(t)
			attempts, readsAfter := 0, 0
			f.before = func(r *http.Request) *http.Response {
				if r.Method == http.MethodPut {
					attempts++
					response := botResponse(status, `{"secret":"private-bot-token","retry_after":600}`)
					response.Header.Set("Retry-After", "600")
					return response
				}
				if attempts > 0 && r.Method == http.MethodGet {
					readsAfter++
				}
				return nil
			}
			actual, err := c.Apply(t.Context(), "30000", []string{"series"})
			if err == nil || strings.Contains(err.Error(), "private-bot-token") || attempts != 1 || !slices.Equal(f.writes, []string{"DELETE 40000"}) {
				t.Fatal("unsafe partial result", actual, err, f.writes, attempts)
			}
			if status == 429 {
				var limit *interestRoleRateLimit
				if actual != nil || readsAfter != 0 || !errors.As(err, &limit) || limit.RetryAfter != 10*time.Minute {
					t.Fatal("rate limit prompted further reads", actual, readsAfter, err)
				}
			} else if !slices.Equal(actual, []string{"60000"}) || readsAfter != 1 {
				t.Fatal("partial failure was not rechecked", actual, readsAfter)
			}
		})
	}
}

func TestInterestRolesRateLimitPreservesDiscordDelay(t *testing.T) {
	for _, tc := range []struct {
		header, body string
		want         time.Duration
	}{
		{"", `{"retry_after":9.25}`, 9250 * time.Millisecond},
		{"", `{"retry_after":0.25}`, time.Second},
		{"120", `{"retry_after":3}`, 2 * time.Minute},
		{"2", `{"retry_after":600}`, 10 * time.Minute},
		{"30", `invalid secret response`, 30 * time.Second},
		{"", `{"retry_after":0}`, time.Minute},
		{"NaN", `{}`, time.Minute},
		{"90000", `{}`, 24 * time.Hour},
	} {
		c, f := interestClientFixture(t)
		f.before = func(*http.Request) *http.Response {
			r := botResponse(429, tc.body)
			r.Header.Set("Retry-After", tc.header)
			return r
		}
		_, err := c.Read(t.Context(), "30000")
		var limit *interestRoleRateLimit
		if !errors.As(err, &limit) || limit.RetryAfter != tc.want || len(f.calls) != 1 {
			t.Fatalf("delay lost: header=%q body=%q error=%v", tc.header, tc.body, err)
		}
	}
}

func TestInterestRolesRejectScreeningAndTimeoutBeforeWrites(t *testing.T) {
	for _, user := range []string{"20000", "30000"} {
		for _, restricted := range []string{`"pending":true`, `"communication_disabled_until":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"`} {
			c, f := interestClientFixture(t)
			f.before = func(r *http.Request) *http.Response {
				if strings.HasSuffix(r.URL.Path, "/members/"+user) {
					return botResponse(200, `{"user":{"id":"`+user+`"},"roles":["70000"],`+restricted+`}`)
				}
				return nil
			}
			if _, err := c.Apply(t.Context(), "30000", []string{"series"}); err == nil || len(f.writes) != 0 {
				t.Fatal("restricted member changed roles", user, restricted, err)
			}
		}
	}
}

func TestInterestRolesFailedFinalReadNeverReturnsStaleSelection(t *testing.T) {
	for _, mode := range []string{"unavailable", "changed after write", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			c, f := interestClientFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f.before = func(r *http.Request) *http.Response {
				if r.Method == http.MethodPut && mode == "deadline" {
					cancel()
				}
				if len(f.writes) == 2 && r.Method == http.MethodGet {
					if mode == "changed after write" {
						f.memberRoles = []string{"60000"}
					} else {
						return botResponse(500, `secret response`)
					}
				}
				return nil
			}
			actual, err := c.Apply(ctx, "30000", []string{"series"})
			if err == nil || (mode != "changed after write" && actual != nil) || (mode == "changed after write" && !slices.Equal(actual, []string{"60000"})) {
				t.Fatal("stale selection confirmed", actual, err)
			}
		})
	}
}

func TestInterestRolesBoundedResponsesAndNoRedirectWithInjectedClient(t *testing.T) {
	for _, mode := range []string{"redirect", "oversized", "invalid JSON", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			c, f := interestClientFixture(t)
			c.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { t.Fatal("injected redirect policy used"); return nil }
			f.before = func(r *http.Request) *http.Response {
				switch mode {
				case "redirect":
					resp := botResponse(302, "")
					resp.Header.Set("Location", "https://evil.invalid/token")
					return resp
				case "oversized":
					return botResponse(200, strings.Repeat(" ", 1024*1024+1))
				case "invalid JSON":
					return botResponse(200, `[] {}`)
				case "timeout":
					<-r.Context().Done()
					return botResponse(500, "")
				}
				return nil
			}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Millisecond)
			defer cancel()
			if _, err := c.Read(ctx, "30000"); err == nil || len(f.calls) != 1 || len(f.writes) != 0 {
				t.Fatal("unbounded or redirected request", f.calls, err)
			}
		})
	}
}

func TestInterestRolesCancelledContextMakesNoRequest(t *testing.T) {
	c, f := interestClientFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Apply(ctx, "30000", []string{"series"}); err == nil || len(f.calls) != 0 {
		t.Fatal("cancelled operation contacted Discord", f.calls, err)
	}
}
