package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
)

func panelBot() *Bot {
	return NewBot(config.Config{
		DiscordClientID: "12345", DiscordGuildID: "23456", DiscordBotToken: "private-panel-token", DiscordInterestsChannelID: "34567",
		DiscordInterests: []config.DiscordInterest{
			{Key: "films", Label: "Films", RoleID: "40001", ChannelID: "50001"},
			{Key: "series", Label: "Séries", RoleID: "40002", ChannelID: "50002"},
			{Key: "animation", Label: "Animation", RoleID: "40003", ChannelID: "50003"},
			{Key: "documentaires", Label: "Documentaires", RoleID: "40004", ChannelID: "50004"},
			{Key: "jeux", Label: "Jeux vidéo", RoleID: "40005", ChannelID: "50005"},
			{Key: "musique", Label: "Musique", RoleID: "40006", ChannelID: "50006"},
			{Key: "livres", Label: "Livres et mangas", RoleID: "40007", ChannelID: "50007"},
		},
	})
}

const panelChannel = `{"id":"34567","guild_id":"23456","type":0}`
const panelMessage = `{"id":"67890","channel_id":"34567","author":{"id":"12345"}}`

func TestPublishInterestsCreatesOrUpdatesOnlyAfterVerification(t *testing.T) {
	for _, messageID := range []string{"", "67890"} {
		t.Run("message="+messageID, func(t *testing.T) {
			b := panelBot()
			calls := []string{}
			b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 10*time.Second || r.URL.Host != "discord.com" || r.URL.Scheme != "https" || r.Header.Get("Authorization") != "Bot private-panel-token" {
					t.Fatal("unsafe or unbounded publication request")
				}
				if r.Method == http.MethodGet {
					if r.URL.Path == "/api/v10/channels/34567" {
						return botResponse(200, panelChannel), nil
					}
					return botResponse(200, panelMessage), nil
				}
				raw, _ := io.ReadAll(r.Body)
				var body struct {
					Content  string                                `json:"content"`
					Flags    int                                   `json:"flags"`
					Embeds   []struct{ Title, Description string } `json:"embeds"`
					Mentions struct {
						Parse []string `json:"parse"`
					} `json:"allowed_mentions"`
					Components []struct {
						Type    int `json:"type"`
						Buttons []struct {
							Type, Style int
							ID          string `json:"custom_id"`
							Label       string `json:"label"`
						} `json:"components"`
					} `json:"components"`
				}
				if json.Unmarshal(raw, &body) != nil || body.Flags != 0 || body.Mentions.Parse == nil || len(body.Mentions.Parse) != 0 || len(body.Embeds) != 1 || len(body.Components) != 1 || len(body.Components[0].Buttons) != 1 || r.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("unsafe panel payload: %s", raw)
				}
				button := body.Components[0].Buttons[0]
				if body.Components[0].Type != 1 || button.Type != 2 || button.Style != 1 || button.ID != "interests:open" || button.Label != "Choisir mes salons" {
					t.Fatal("panel button does not open the private interest menu")
				}
				for _, item := range b.Config.DiscordInterests {
					if !strings.Contains(body.Embeds[0].Description, "• "+item.Label) || strings.Contains(string(raw), item.RoleID) || strings.Contains(string(raw), item.ChannelID) {
						t.Fatal("panel lost a configured label or exposed role/channel IDs")
					}
				}
				if strings.Contains(string(raw), "@") || strings.Contains(string(raw), "private-panel-token") {
					t.Fatal("panel contains a mention or secret")
				}
				return botResponse(200, panelMessage), nil
			})
			id, err := b.PublishInterests(t.Context(), messageID)
			want := []string{"GET /api/v10/channels/34567", "POST /api/v10/channels/34567/messages"}
			if messageID != "" {
				want = []string{"GET /api/v10/channels/34567", "GET /api/v10/channels/34567/messages/67890", "PATCH /api/v10/channels/34567/messages/67890"}
			}
			if err != nil || id != "67890" || !slices.Equal(calls, want) || b.Client.Timeout != 8*time.Second {
				t.Fatalf("publication: id=%q err=%v calls=%v", id, err, calls)
			}
		})
	}
}

func TestPublishInterestsRefusesUnverifiedChannelOrMessage(t *testing.T) {
	for _, tc := range []struct{ name, channel, message string }{
		{"channel ID mismatch", `{"id":"99999","guild_id":"23456","type":0}`, panelMessage},
		{"guild mismatch", `{"id":"34567","guild_id":"99999","type":0}`, panelMessage},
		{"DM channel", `{"id":"34567","type":1}`, panelMessage},
		{"forum channel", `{"id":"34567","guild_id":"23456","type":15}`, panelMessage},
		{"missing channel type", `{"id":"34567","guild_id":"23456"}`, panelMessage},
		{"message ID mismatch", panelChannel, `{"id":"99999","channel_id":"34567","author":{"id":"12345"}}`},
		{"message channel mismatch", panelChannel, `{"id":"67890","channel_id":"99999","author":{"id":"12345"}}`},
		{"foreign author", panelChannel, `{"id":"67890","channel_id":"34567","author":{"id":"99999"}}`},
		{"missing author", panelChannel, `{"id":"67890","channel_id":"34567"}`},
		{"webhook impersonation", panelChannel, `{"id":"67890","channel_id":"34567","author":{"id":"12345"},"webhook_id":"99999"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := panelBot()
			b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					t.Fatal("unverified destination or author reached a write")
				}
				if strings.HasSuffix(r.URL.Path, "/67890") {
					return botResponse(200, tc.message), nil
				}
				return botResponse(200, tc.channel), nil
			})
			if id, err := b.PublishInterests(t.Context(), "67890"); err == nil || id != "" {
				t.Fatal("unsafe update accepted")
			}
		})
	}
}

func TestPublishInterestsRejectsInvalidConfigurationBeforeRequests(t *testing.T) {
	for _, mode := range []string{"no interests", "too many interests", "no token", "invalid channel", "invalid guild", "invalid app", "invalid message", "noncanonical message"} {
		t.Run(mode, func(t *testing.T) {
			b, messageID := panelBot(), ""
			switch mode {
			case "no interests":
				b.Config.DiscordInterests = nil
			case "too many interests":
				b.Config.DiscordInterests = make([]config.DiscordInterest, 26)
			case "no token":
				b.Config.DiscordBotToken = ""
			case "invalid channel":
				b.Config.DiscordInterestsChannelID = "34567/messages"
			case "invalid guild":
				b.Config.DiscordGuildID = "guild"
			case "invalid app":
				b.Config.DiscordClientID = "app"
			case "invalid message":
				messageID = "67890/other"
			case "noncanonical message":
				messageID = "067890"
			}
			b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid configuration reached Discord")
				return nil, nil
			})
			if id, err := b.PublishInterests(t.Context(), messageID); err == nil || id != "" {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestPublishInterestsErrorsDoNotRetryRedirectOrLeak(t *testing.T) {
	for _, stage := range []string{"channel", "message", "write"} {
		for _, failure := range []string{"network", "forbidden", "rate limit", "redirect", "oversized", "invalid JSON", "invalid ID", "missing identity"} {
			t.Run(stage+"/"+failure, func(t *testing.T) {
				b := panelBot()
				failedCalls, writes := 0, 0
				b.Client.CheckRedirect = func(*http.Request, []*http.Request) error {
					t.Fatal("panel used the caller redirect policy")
					return nil
				}
				b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
					if r.URL.Host != "discord.com" {
						t.Fatal("followed a redirect with the bot credential")
					}
					current := "channel"
					if strings.Contains(r.URL.Path, "/messages/") {
						current = "message"
					}
					if r.Method != http.MethodGet {
						current = "write"
						writes++
					}
					if stage != current {
						if current == "channel" {
							return botResponse(200, panelChannel), nil
						}
						return botResponse(200, panelMessage), nil
					}
					failedCalls++
					switch failure {
					case "network":
						return nil, errors.New("private-panel-token transport failure")
					case "forbidden":
						return botResponse(403, "private-panel-token"), nil
					case "rate limit":
						return botResponse(429, `{"retry_after":1,"private":"private-panel-token"}`), nil
					case "redirect":
						resp := botResponse(307, "private-panel-token")
						resp.Header.Set("Location", "https://attacker.test/private-panel-token")
						return resp, nil
					case "oversized":
						return botResponse(200, strings.Repeat(" ", 1024*1024+1)), nil
					case "invalid JSON":
						return botResponse(200, "private-panel-token"), nil
					case "invalid ID":
						return botResponse(200, `{"id":"not-an-id","channel_id":"34567","guild_id":"23456","type":0,"author":{"id":"12345"}}`), nil
					default:
						return botResponse(200, `{}`), nil
					}
				})
				id, err := b.PublishInterests(t.Context(), "67890")
				wantWrites := 0
				if stage == "write" {
					wantWrites = 1
				}
				if err == nil || id != "" || failedCalls != 1 || writes != wantWrites || strings.Contains(err.Error(), "private-panel-token") {
					t.Fatalf("unconfirmed/retried/unsafe publication: %q %v failures=%d writes=%d", id, err, failedCalls, writes)
				}
			})
		}
	}
}

func TestPublishInterestsCancellationStopsBeforeWriting(t *testing.T) {
	b := panelBot()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Fatal("cancelled publication wrote a message")
		}
		cancel()
		return nil, r.Context().Err()
	})
	if id, err := b.PublishInterests(ctx, ""); err == nil || id != "" {
		t.Fatal("cancelled publication accepted")
	}
}

func TestPublishInterestsCreateNeverRetriesAnUnconfirmedPost(t *testing.T) {
	b := panelBot()
	writes := 0
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return botResponse(200, panelChannel), nil
		}
		writes++
		if r.Method != http.MethodPost {
			t.Fatal("create changed into another write")
		}
		return nil, errors.New("private-panel-token response lost after posting")
	})
	if id, err := b.PublishInterests(t.Context(), ""); err == nil || id != "" || writes != 1 || strings.Contains(err.Error(), "private-panel-token") {
		t.Fatalf("unconfirmed post retried or leaked: %q %v, writes=%d", id, err, writes)
	}
}

func TestPublishInterestsFitsMaximumConfiguredLabels(t *testing.T) {
	b := panelBot()
	b.Config.DiscordInterests = nil
	for i := 0; i < 25; i++ {
		b.Config.DiscordInterests = append(b.Config.DiscordInterests, config.DiscordInterest{Key: fmt.Sprintf("interest-%d", i), Label: strings.Repeat("é", 80), RoleID: fmt.Sprint(40000 + i), ChannelID: fmt.Sprint(50000 + i)})
	}
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return botResponse(200, panelChannel), nil
		}
		var body struct {
			Content string `json:"content"`
			Embeds  []struct {
				Description string `json:"description"`
			} `json:"embeds"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len([]rune(body.Content)) > 2000 || len(body.Embeds) != 1 || len([]rune(body.Embeds[0].Description)) > 4096 || strings.Count(body.Embeds[0].Description, "• ") != 25 || strings.Count(body.Embeds[0].Description, "é") != 25*80 {
			t.Fatal("maximum configured labels were truncated or exceeded Discord limits")
		}
		return botResponse(200, panelMessage), nil
	})
	if _, err := b.PublishInterests(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
}
