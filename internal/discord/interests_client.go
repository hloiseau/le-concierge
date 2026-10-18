package discord

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
)

type interestRoleClient struct {
	HTTP                  *http.Client
	Token, GuildID, BotID string
	Interests             []config.DiscordInterest
}

var (
	errInterestSelection = errors.New("Cette sélection d’intérêts n’est pas valide. Rouvrez la commande.")
	errInterestAccess    = errors.New("Les rôles d’intérêts ne peuvent pas être gérés actuellement. Un administrateur doit vérifier les droits du bot et les rôles configurés.")
	errInterestSource    = errors.New("Discord ne permet pas de vérifier vos rôles actuellement. Rouvrez la commande pour consulter leur état.")
)

type interestRoleRateLimit struct{ RetryAfter time.Duration }

func (*interestRoleRateLimit) Error() string {
	return "Discord limite temporairement les modifications. Patientez avant de rouvrir la commande."
}

func interestID(id string) bool {
	n, err := strconv.ParseUint(id, 10, 64)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == id && config.Snowflake(id)
}

func (c interestRoleClient) validate(userID string) error {
	if c.Token == "" || !interestID(c.GuildID) || !interestID(c.BotID) || !interestID(userID) || userID == c.BotID || len(c.Interests) == 0 || len(c.Interests) > 25 {
		return errInterestAccess
	}
	keys, roles := map[string]bool{}, map[string]bool{}
	for _, interest := range c.Interests {
		if interest.Key == "" || keys[interest.Key] || !interestID(interest.RoleID) || roles[interest.RoleID] || interest.RoleID == c.GuildID {
			return errInterestAccess
		}
		keys[interest.Key], roles[interest.RoleID] = true, true
	}
	return nil
}

func (c interestRoleClient) request(ctx context.Context, method, path string, out any) error {
	if ctx.Err() != nil {
		return errInterestSource
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://discord.com/api/v10/guilds/"+c.GuildID+path, nil)
	if err != nil {
		return errInterestSource
	}
	req.Header.Set("Authorization", "Bot "+c.Token)
	req.Header.Set("User-Agent", "le-concierge-interests")
	client := http.Client{}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	client.Timeout = 5 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return errInterestSource
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		seconds, _ := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64)
		if !(seconds > 0) {
			seconds = 0
		}
		var limit struct {
			RetryAfter float64 `json:"retry_after"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&limit) == nil && limit.RetryAfter > seconds {
			seconds = limit.RetryAfter
		}
		if seconds <= 0 {
			seconds = 60
		}
		return &interestRoleRateLimit{RetryAfter: time.Duration(min(max(seconds, 1), 86400) * float64(time.Second))}
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return errInterestAccess
	}
	if out == nil {
		if resp.StatusCode != http.StatusNoContent {
			return errInterestSource
		}
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return errInterestSource
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if err != nil || len(body) > 1024*1024 || json.Unmarshal(body, out) != nil {
		return errInterestSource
	}
	return nil
}

func (c interestRoleClient) member(ctx context.Context, id string) ([]string, error) {
	var member struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		Roles        *[]string  `json:"roles"`
		Pending      bool       `json:"pending"`
		TimeoutUntil *time.Time `json:"communication_disabled_until"`
	}
	if err := c.request(ctx, http.MethodGet, "/members/"+id, &member); err != nil {
		return nil, err
	}
	if member.User.ID != id || member.Roles == nil || len(*member.Roles) > 1000 {
		return nil, errInterestSource
	}
	if member.Pending || (member.TimeoutUntil != nil && member.TimeoutUntil.After(time.Now())) {
		return nil, errInterestAccess
	}
	seen := map[string]bool{}
	for _, role := range *member.Roles {
		if !interestID(role) || seen[role] {
			return nil, errInterestSource
		}
		seen[role] = true
	}
	return *member.Roles, nil
}

// Read checks the full allowlist before exposing a selectable role menu.
func (c interestRoleClient) Read(ctx context.Context, userID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := c.validate(userID); err != nil {
		return nil, err
	}
	var roles []struct {
		ID          string `json:"id"`
		Permissions string `json:"permissions"`
		Position    *int   `json:"position"`
		Managed     *bool  `json:"managed"`
	}
	if err := c.request(ctx, http.MethodGet, "/roles", &roles); err != nil {
		return nil, err
	}
	if len(roles) == 0 || len(roles) > 1000 {
		return nil, errInterestSource
	}
	botRoles, err := c.member(ctx, c.BotID)
	if err != nil {
		return nil, err
	}
	botRoles = append(botRoles, c.GuildID)
	positions, permissions, managed := map[string]int{}, map[string]uint64{}, map[string]bool{}
	for _, role := range roles {
		permission, err := strconv.ParseUint(role.Permissions, 10, 64)
		_, duplicate := positions[role.ID]
		if !interestID(role.ID) || duplicate || err != nil || role.Position == nil || *role.Position < 0 || role.Managed == nil {
			return nil, errInterestSource
		}
		positions[role.ID], permissions[role.ID], managed[role.ID] = *role.Position, permission, *role.Managed
	}
	var botPermissions uint64
	highest := 0
	for _, id := range botRoles {
		position, exists := positions[id]
		if !exists {
			return nil, errInterestAccess
		}
		botPermissions |= permissions[id]
		highest = max(highest, position)
	}
	if botPermissions&(1<<28|1<<3) == 0 {
		return nil, errInterestAccess
	}
	for _, interest := range c.Interests {
		position, exists := positions[interest.RoleID]
		if !exists || position >= highest || permissions[interest.RoleID] != 0 || managed[interest.RoleID] {
			return nil, errInterestAccess
		}
	}
	return c.member(ctx, userID)
}

// Apply only changes allowlisted roles; unrelated member roles are never written.
func (c interestRoleClient) Apply(ctx context.Context, userID string, selected []string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := c.validate(userID); err != nil {
		return nil, err
	}
	wanted, keys := map[string]bool{}, map[string]bool{}
	for _, interest := range c.Interests {
		keys[interest.Key] = true
	}
	for _, key := range selected {
		if !keys[key] || wanted[key] {
			return nil, errInterestSelection
		}
		wanted[key] = true
	}
	current, err := c.Read(ctx, userID)
	if err != nil {
		return nil, err
	}
	changed := false
	for _, interest := range c.Interests {
		if wanted[interest.Key] != slices.Contains(current, interest.RoleID) {
			changed = true
			break
		}
	}
	if !changed {
		return current, nil
	}
	// Refresh permissions and the member again immediately before the first write.
	current, err = c.Read(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, interest := range c.Interests {
		if wanted[interest.Key] == slices.Contains(current, interest.RoleID) {
			continue
		}
		method := http.MethodDelete
		if wanted[interest.Key] {
			method = http.MethodPut
		}
		err = c.request(ctx, method, "/members/"+userID+"/roles/"+interest.RoleID, nil)
		if err != nil {
			break
		}
	}
	// Do not immediately query another endpoint during Discord's rate limit.
	var limited *interestRoleRateLimit
	if errors.As(err, &limited) {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, errInterestSource
	}
	actual, readErr := c.member(ctx, userID)
	if readErr != nil {
		return nil, readErr
	}
	if err == nil {
		for _, interest := range c.Interests {
			if wanted[interest.Key] != slices.Contains(actual, interest.RoleID) {
				err = errInterestSource
				break
			}
		}
	}
	return actual, err
}
