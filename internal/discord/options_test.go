package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCommandOptionsValidateNamesTypesAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name, command, options string
		valid                  bool
		kind                   string
		id, season, page       int
	}{
		{name: "unfiltered surprise", command: "surprise", options: `[]`, valid: true, page: 1, season: -1},
		{name: "personal departures", command: "a-voir", options: `[]`, valid: true, page: 1, season: -1},
		{name: "departures public denied", command: "a-voir", options: `[{"name":"public","type":5,"value":true}]`},
		{name: "departures page denied", command: "a-voir", options: `[{"name":"page","type":4,"value":2}]`},
		{name: "books browse", command: "livres", options: `[]`, valid: true, page: 1, season: -1},
		{name: "diagnostic", command: "diagnostic", options: `[{"name":"demande","type":4,"value":17}]`, valid: true, page: 1, season: -1},
		{name: "diagnostic missing", command: "diagnostic", options: `[]`},
		{name: "diagnostic zero", command: "diagnostic", options: `[{"name":"demande","type":4,"value":0}]`},
		{name: "diagnostic duplicate", command: "diagnostic", options: `[{"name":"demande","type":4,"value":1},{"name":"demande","type":4,"value":2}]`},
		{name: "diagnostic wrong command", command: "livres", options: `[{"name":"demande","type":4,"value":17}]`},
		{name: "books search", command: "livres", options: `[{"name":"recherche","type":3,"value":"Dune"},{"name":"page","type":4,"value":2}]`, valid: true, page: 2, season: -1},
		{name: "book request", command: "demander-livre", options: `[{"name":"recherche","type":3,"value":"Dune"}]`, valid: true, page: 1, season: -1},
		{name: "request missing search", command: "demander-livre", options: `[]`},
		{name: "request no page", command: "demander-livre", options: `[{"name":"recherche","type":3,"value":"Dune"},{"name":"page","type":4,"value":2}]`},
		{name: "empty search", command: "livres", options: `[{"name":"recherche","type":3,"value":"  "}]`},
		{name: "null search", command: "livres", options: `[{"name":"recherche","type":3,"value":null}]`},
		{name: "search control", command: "livres", options: `[{"name":"recherche","type":3,"value":"Dune\n"}]`},
		{name: "search wrong command", command: "recap", options: `[{"name":"recherche","type":3,"value":"Dune"}]`},
		{name: "show surprise", command: "surprise", options: `[{"name":"type","type":3,"value":"show"}]`, valid: true, kind: "show", page: 1, season: -1},
		{name: "wrong surprise kind", command: "surprise", options: `[{"name":"type","type":3,"value":"tv"}]`},
		{name: "movie follow", command: "suivre", options: `[{"name":"type","type":3,"value":"movie"},{"name":"tmdb-id","type":4,"value":2147483647}]`, valid: true, kind: "movie", id: 2147483647, page: 1, season: -1},
		{name: "special season", command: "suivre", options: `[{"name":"season","type":4,"value":0},{"name":"type","type":3,"value":"tv"},{"name":"tmdb-id","type":4,"value":1}]`, valid: true, kind: "tv", id: 1, page: 1, season: 0},
		{name: "last page", command: "mes-suivis", options: `[{"name":"page","type":4,"value":1001}]`, valid: true, page: 1001, season: -1},
		{name: "missing fields", command: "suivre", options: `[]`},
		{name: "missing id", command: "suivre", options: `[{"name":"type","type":3,"value":"tv"}]`},
		{name: "missing kind", command: "suivre", options: `[{"name":"tmdb-id","type":4,"value":1}]`},
		{name: "wrong value type", command: "surprise", options: `[{"name":"type","type":3,"value":1}]`},
		{name: "wrong option type", command: "surprise", options: `[{"name":"type","type":4,"value":"movie"}]`},
		{name: "duplicate", command: "surprise", options: `[{"name":"type","type":3,"value":"movie"},{"name":"type","type":3,"value":"show"}]`},
		{name: "unknown field", command: "surprise", options: `[{"name":"user","type":3,"value":"another-user"}]`},
		{name: "recap option", command: "recap", options: `[{"name":"type","type":3,"value":"movie"}]`},
		{name: "old command option", command: "nouveautes", options: `[{"name":"page","type":4,"value":1}]`},
		{name: "null season", command: "suivre", options: `[{"name":"type","type":3,"value":"tv"},{"name":"tmdb-id","type":4,"value":1},{"name":"season","type":4,"value":null}]`},
		{name: "movie season", command: "suivre", options: `[{"name":"type","type":3,"value":"movie"},{"name":"tmdb-id","type":4,"value":1},{"name":"season","type":4,"value":1}]`},
		{name: "negative season", command: "suivre", options: `[{"name":"type","type":3,"value":"tv"},{"name":"tmdb-id","type":4,"value":1},{"name":"season","type":4,"value":-1}]`},
		{name: "too large season", command: "suivre", options: `[{"name":"type","type":3,"value":"tv"},{"name":"tmdb-id","type":4,"value":1},{"name":"season","type":4,"value":10001}]`},
		{name: "id too large", command: "suivre", options: `[{"name":"type","type":3,"value":"tv"},{"name":"tmdb-id","type":4,"value":2147483648}]`},
		{name: "zero id", command: "suivre", options: `[{"name":"type","type":3,"value":"tv"},{"name":"tmdb-id","type":4,"value":0}]`},
		{name: "fraction id", command: "suivre", options: `[{"name":"type","type":3,"value":"tv"},{"name":"tmdb-id","type":4,"value":1.5}]`},
		{name: "string id", command: "suivre", options: `[{"name":"type","type":3,"value":"tv"},{"name":"tmdb-id","type":4,"value":"1"}]`},
		{name: "zero page", command: "mes-suivis", options: `[{"name":"page","type":4,"value":0}]`},
		{name: "too large page", command: "mes-suivis", options: `[{"name":"page","type":4,"value":1002}]`},
		{name: "number option type", command: "mes-suivis", options: `[{"name":"page","type":10,"value":1}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var i Interaction
			if err := json.Unmarshal([]byte(`{"type":2,"data":{"name":"`+tc.command+`","options":`+tc.options+`}}`), &i); err != nil {
				t.Fatal(err)
			}
			options, err := CommandOptions(i)
			_, _, commandErr := Command(i)
			if (err == nil) != tc.valid || (commandErr == nil) != tc.valid {
				t.Fatalf("validation disagreement: %+v %v %v", options, err, commandErr)
			}
			if tc.valid && (options.Kind != tc.kind || options.TMDBID != tc.id || options.Page != tc.page || (tc.season < 0 && options.Season != nil) || (tc.season >= 0 && (options.Season == nil || *options.Season != tc.season))) {
				t.Fatalf("wrong parsed options %+v", options)
			}
		})
	}
}

func TestInvalidSignedOptionsNeverReachAPIOrDiscord(t *testing.T) {
	b, r := signedInteraction(t, `{"id":"99999","application_id":"12345","guild_id":"23456","type":2,"token":"private-interaction-token","member":{"user":{"id":"11111"}},"data":{"name":"suivre","options":[{"name":"type","type":3,"value":"movie"}]}}`)
	b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("invalid options reached remote service")
		return botResponse(500, `{}`), nil
	})
	w := httptest.NewRecorder()
	b.Handler().ServeHTTP(w, r)
	b.Wait()
	if w.Code != 403 {
		t.Fatalf("invalid options accepted: %d", w.Code)
	}
}

func TestRegisterCommandsMatchesValidatedOptions(t *testing.T) {
	b, _ := signedCommand(t, "aide")
	b.Config.DiscordBotToken = "fake-bot-token"
	calls := 0
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "PUT" || r.URL.String() != "https://discord.com/api/v10/applications/12345/guilds/23456/commands" || r.Header.Get("Authorization") != "Bot fake-bot-token" {
			t.Error("wrong registration target")
		}
		var commands []struct {
			Name    string `json:"name"`
			Type    int    `json:"type"`
			Options []struct {
				Name     string                         `json:"name"`
				Type     int                            `json:"type"`
				Required bool                           `json:"required"`
				MinValue int                            `json:"min_value"`
				MaxValue int                            `json:"max_value"`
				Choices  []struct{ Name, Value string } `json:"choices"`
			} `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&commands); err != nil {
			t.Fatal(err)
		}
		want := map[string]int{"aide": 0, "mes-demandes": 0, "a-voir": 0, "nouveautes": 1, "surprise": 2, "recap": 1, "livres": 2, "demander-livre": 1, "diagnostic": 1}
		if len(commands) != len(want) {
			t.Fatalf("wrong command count: %d", len(commands))
		}
		for _, command := range commands {
			count, exists := want[command.Name]
			if !exists || command.Type != 1 || len(command.Options) != count {
				t.Fatalf("bad command %+v", command)
			}
			delete(want, command.Name)
			for index, option := range command.Options {
				switch option.Name {
				case "public":
					if !PublicCommand(command.Name) || option.Type != 5 || option.Required {
						t.Fatalf("bad public option %+v", option)
					}
				case "demande":
					if option.Type != 4 || !option.Required || option.MinValue != 1 || option.MaxValue != 2147483647 {
						t.Fatalf("bad request option %+v", option)
					}
				case "recherche":
					if option.Type != 3 || option.Required != (command.Name == "demander-livre") {
						t.Fatalf("bad search option %+v", option)
					}
				case "type":
					second := "show"
					if command.Name == "suivre" {
						second = "tv"
					}
					if option.Type != 3 || option.Required != (command.Name == "suivre") || len(option.Choices) != 2 || option.Choices[0].Value != "movie" || option.Choices[1].Value != second {
						t.Fatalf("bad type choices %+v", option)
					}
				case "tmdb-id":
					if index != 1 || option.Type != 4 || !option.Required || option.MinValue != 1 || option.MaxValue != 2147483647 {
						t.Fatalf("bad id option %+v", option)
					}
				case "season":
					if index != 2 || option.Type != 4 || option.Required || option.MinValue != 0 || option.MaxValue != 10000 {
						t.Fatalf("bad season option %+v", option)
					}
				case "page":
					if option.Type != 4 || option.Required || option.MinValue != 1 || option.MaxValue != 1001 {
						t.Fatalf("bad page option %+v", option)
					}
				default:
					t.Fatalf("unvalidated option %s", option.Name)
				}
			}
		}
		return botResponse(200, `[]`), nil
	})
	if err := b.Register(context.Background()); err != nil || calls != 1 {
		t.Fatalf("registration failed %v, %d calls", err, calls)
	}
}
