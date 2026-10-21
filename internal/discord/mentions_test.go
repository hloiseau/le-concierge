package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/websocket"
	"github.com/hloiseau/le-concierge/internal/eris"
)

func mentionFixture() *discordgo.MessageCreate {
	return &discordgo.MessageCreate{Message: &discordgo.Message{
		ID: "99999", GuildID: "23456", ChannelID: "33333", Content: "<@12345> Bonjour",
		Author: &discordgo.User{ID: "11111"}, Mentions: []*discordgo.User{{ID: "12345"}},
	}}
}

func TestMentionAcceptsHumanMentionsAndRepliesInConfiguredGuild(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	for _, tc := range []struct {
		name     string
		change   func(*discordgo.MessageCreate)
		accepted bool
	}{
		{"mention", func(*discordgo.MessageCreate) {}, true},
		{"nickname", func(m *discordgo.MessageCreate) { m.Content = "Bonjour <@!12345>" }, true},
		{"bare mention", func(m *discordgo.MessageCreate) { m.Content = "<@12345>" }, true},
		{"reply with explicit mention", func(m *discordgo.MessageCreate) { m.Type = discordgo.MessageTypeReply }, true},
		{"reply to Eris without inline mention", func(m *discordgo.MessageCreate) {
			m.Type = discordgo.MessageTypeReply
			m.Content = "Tu peux le demander pour moi dans seerr ?"
			m.MessageReference = &discordgo.MessageReference{MessageID: "88888", ChannelID: m.ChannelID, GuildID: m.GuildID}
			m.ReferencedMessage = &discordgo.Message{ID: "88888", ChannelID: m.ChannelID, Author: &discordgo.User{ID: "12345", Bot: true}}
		}, true},
		{"plain name", func(m *discordgo.MessageCreate) { m.Content = "Eris bonjour" }, false},
		{"other guild", func(m *discordgo.MessageCreate) { m.GuildID = "34567" }, false},
		{"DM", func(m *discordgo.MessageCreate) { m.GuildID = "" }, false},
		{"bot", func(m *discordgo.MessageCreate) { m.Author.Bot = true }, false},
		{"self", func(m *discordgo.MessageCreate) { m.Author.ID = "12345" }, false},
		{"webhook", func(m *discordgo.MessageCreate) { m.WebhookID = "44444" }, false},
		{"system", func(m *discordgo.MessageCreate) { m.Type = discordgo.MessageTypeChannelPinnedMessage }, false},
		{"implicit reply ping", func(m *discordgo.MessageCreate) { m.Type = discordgo.MessageTypeReply; m.Content = "Bonjour" }, false},
		{"role", func(m *discordgo.MessageCreate) { m.Content = "<@&12345> Bonjour" }, false},
		{"no mention metadata", func(m *discordgo.MessageCreate) { m.Mentions = nil }, false},
		{"missing author", func(m *discordgo.MessageCreate) { m.Author = nil }, false},
		{"invalid channel", func(m *discordgo.MessageCreate) { m.ChannelID = "../12345" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mentionFixture()
			tc.change(m)
			question, ok := b.mentionQuestion(m)
			if ok != tc.accepted || (ok && strings.Contains(question, "<@")) {
				t.Fatalf("question=%q accepted=%v", question, ok)
			}
		})
	}
}

func TestReplyTargetMustBeErisInTheSameChannel(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	for _, state := range []string{"valid", "no ping", "another author", "webhook", "other channel", "other guild", "wrong reference", "deleted", "forward"} {
		m := mentionFixture()
		m.Type = discordgo.MessageTypeReply
		m.Content = "Tu peux le demander pour moi dans seerr ?"
		m.MessageReference = &discordgo.MessageReference{MessageID: "88888", ChannelID: m.ChannelID, GuildID: m.GuildID}
		m.ReferencedMessage = &discordgo.Message{ID: "88888", ChannelID: m.ChannelID, Author: &discordgo.User{ID: "12345", Bot: true}}
		switch state {
		case "no ping":
			m.Mentions = nil
		case "another author":
			m.ReferencedMessage.Author.ID = "22222"
		case "webhook":
			m.ReferencedMessage.WebhookID = "77777"
		case "other channel":
			m.ReferencedMessage.ChannelID = "44444"
		case "other guild":
			m.MessageReference.GuildID = "34567"
		case "wrong reference":
			m.MessageReference.MessageID = "77777"
		case "deleted":
			m.ReferencedMessage = nil
		case "forward":
			m.Type = discordgo.MessageTypeDefault
		}
		if question, ok := b.mentionQuestion(m); ok != (state == "valid" || state == "no ping") || (ok && question != m.Content) {
			t.Fatalf("%s: question=%q accepted=%v", state, question, ok)
		}
	}
}

func TestReplyToErisOffersOnlyPrivateHandoffButton(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	b.Eris.URL = "http://model.test/v1"
	b.Config.BotAPIURL = "http://api.test"
	questions := []string{}
	b.Eris.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		var body struct{ Messages []eris.Message }
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, message := range body.Messages {
			if message.Role == "user" {
				questions = append(questions, message.Content)
			}
		}
		return botResponse(200, `{"choices":[{"message":{"role":"assistant","content":"{\"action\":\"prepare\",\"query\":\"Hot Fuzz\",\"kind\":\"movie\"}"}}]}`), nil
	})
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		var in eris.ToolRequest
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.MessageID != "99999" || in.ChannelID != "33333" || in.Plan.Query != "Hot Fuzz" {
			t.Error("reply proof or title lost")
		}
		return botResponse(200, `{"content":"On continue en privé.","buttons":[{"id":"eris:open:`+strings.Repeat("a", 64)+`","label":"Préparer en privé"},{"id":"eris:confirm:`+strings.Repeat("b", 64)+`","label":"Must stay private"}]}`), nil
	})
	s, _ := discordgo.New("Bot fixture")
	replies := 0
	s.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/typing") {
			return botResponse(204, ""), nil
		}
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		raw, _ := json.Marshal(payload)
		if !strings.Contains(string(raw), "eris:open:") || strings.Contains(string(raw), "eris:confirm:") {
			t.Error("missing handoff or public confirmation")
		}
		replies++
		return botResponse(200, `{"id":"88888"}`), nil
	})}
	m := mentionFixture()
	m.Content = "Tu peux le demander pour moi dans seerr ?"
	m.Mentions = nil
	m.Type = discordgo.MessageTypeReply
	m.MessageReference = &discordgo.MessageReference{MessageID: "88888", ChannelID: m.ChannelID, GuildID: m.GuildID}
	m.ReferencedMessage = &discordgo.Message{ID: "88888", ChannelID: m.ChannelID, Author: &discordgo.User{ID: "12345", Bot: true}}
	b.handleMention(context.Background(), s, m)
	b.Wait()
	if replies != 1 || len(questions) != 1 || questions[0] != m.Content {
		t.Fatal("reply was ignored", replies, questions)
	}
}

func TestMentionReplyUsesPublicHistoryWithoutPrivateContextOrPings(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	b.Eris.URL = "http://model.test/v1"
	var prompts [][]eris.Message
	b.Eris.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		var body struct{ Messages []eris.Message }
		_ = json.NewDecoder(r.Body).Decode(&body)
		prompts = append(prompts, body.Messages)
		return botResponse(200, `{"choices":[{"message":{"role":"assistant","content":"{\"action\":\"chat\",\"text\":\"Bonjour @everyone <@22222>.\"}"}}]}`), nil
	})
	if _, err := b.Eris.Reply(context.Background(), "23456:11111", "secret privé", false); err != nil {
		t.Fatal(err)
	}
	s, _ := discordgo.New("Bot fixture-token")
	replies := 0
	s.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/channels/33333/typing") {
			return botResponse(204, ""), nil
		}
		if !strings.HasSuffix(r.URL.Path, "/channels/33333/messages") {
			t.Errorf("unexpected route: %s", r.URL.Path)
		}
		var body struct {
			Content   string
			Allowed   *discordgo.MessageAllowedMentions `json:"allowed_mentions"`
			Reference discordgo.MessageReference        `json:"message_reference"`
			Flags     int
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Content == "" || body.Reference.MessageID != "99999" || body.Flags != 0 || body.Allowed == nil || len(body.Allowed.Parse) != 0 || len(body.Allowed.Users) != 0 || len(body.Allowed.Roles) != 0 || body.Allowed.RepliedUser {
			t.Error("wrong public reply or unsafe mentions")
		}
		replies++
		return botResponse(200, `{"id":"88888"}`), nil
	})}
	m := mentionFixture()
	b.handleMention(context.Background(), s, m)
	b.Wait()
	b.handleMention(context.Background(), s, m)
	b.Wait()
	if replies != 1 || len(prompts) != 2 || len(prompts[1]) != 2 || prompts[1][1].Content != "Bonjour" {
		t.Fatalf("duplicate reply or private history leaked: replies=%d prompts=%+v", replies, prompts)
	}
	// A second member and a second channel each start with no shared history.
	for _, owner := range []string{"23456:22222:33333", "23456:11111:44444"} {
		if _, err := b.Eris.Reply(context.Background(), owner, "Salut", false); err != nil {
			t.Fatal(err)
		}
		if len(prompts[len(prompts)-1]) != 2 {
			t.Fatal("public conversations shared history")
		}
	}
}

func TestMentionsGatewayReceivesResumesAndStops(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	b.Config.DiscordBotToken = "fixture-token"
	b.Config.ErisAPIURL = "http://model.test"
	b.Config.DiscordMessageContent = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	disconnect := make(chan struct{})
	var connections atomic.Int32
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		number := connections.Add(1)
		_ = conn.WriteJSON(map[string]any{"op": 10, "d": map[string]any{"heartbeat_interval": 60000}})
		var packet struct {
			Op   int
			Data struct {
				Intents int
				Token   string
				Session string `json:"session_id"`
			} `json:"d"`
		}
		if err := conn.ReadJSON(&packet); err != nil {
			t.Error(err)
			return
		}
		if number == 1 {
			if packet.Op != 2 || packet.Data.Intents != 512|32768 || packet.Data.Token != "Bot fixture-token" {
				t.Errorf("wrong identify: op=%d intents=%d", packet.Op, packet.Data.Intents)
			}
			_ = conn.WriteJSON(map[string]any{"op": 0, "s": 1, "t": "READY", "d": map[string]any{"session_id": "test-session", "user": map[string]any{"id": "12345", "bot": true}}})
		} else {
			if packet.Op != 6 || packet.Data.Session != "test-session" {
				t.Error("connection did not resume")
			}
			_ = conn.WriteJSON(map[string]any{"op": 0, "s": 3, "t": "RESUMED", "d": map[string]any{}})
		}
		m := mentionFixture()
		_ = conn.WriteJSON(map[string]any{"op": 0, "s": 4, "t": "MESSAGE_CREATE", "d": m.Message})
		if number == 1 {
			select {
			case <-disconnect:
			case <-ctx.Done():
			}
			return
		}
		m.ID = "10000"
		_ = conn.WriteJSON(map[string]any{"op": 0, "s": 5, "t": "MESSAGE_CREATE", "d": m.Message})
		<-ctx.Done()
	}))
	defer server.Close()
	replies := make(chan string, 4)
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/users/@me"):
			return botResponse(200, `{"id":"12345","bot":true}`), nil
		case strings.HasSuffix(r.URL.Path, "/gateway"):
			return botResponse(200, `{"url":"`+strings.Replace(server.URL, "http://", "ws://", 1)+`"}`), nil
		case strings.HasSuffix(r.URL.Path, "/typing"):
			return botResponse(204, ""), nil
		case strings.HasSuffix(r.URL.Path, "/messages"):
			var body struct {
				Reference discordgo.MessageReference `json:"message_reference"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			replies <- body.Reference.MessageID
			return botResponse(200, `{"id":"88888"}`), nil
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			return botResponse(500, ""), nil
		}
	})
	done := make(chan struct{})
	go func() { defer close(done); b.RunMentions(ctx) }()
	for _, expected := range []string{"99999", "10000"} {
		select {
		case id := <-replies:
			if id != expected {
				t.Errorf("unexpected/duplicate reply %s, want %s", id, expected)
			}
		case <-ctx.Done():
			t.Error("gateway reply timed out")
		}
		if expected == "99999" {
			close(disconnect)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway did not stop")
	}
	b.Wait()
	if len(replies) != 0 {
		t.Fatal("duplicate replay was answered")
	}
}

func TestBareAndOversizedMentionsDoNotCallTheModel(t *testing.T) {
	for _, question := range []string{"", strings.Repeat("é", 501)} {
		b, _ := signedCommand(t, "eris")
		b.Eris.HTTP.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected inference")
			return botResponse(500, ""), nil
		})
		s, _ := discordgo.New("Bot fixture-token")
		count := 0
		s.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			count++
			if !strings.HasSuffix(r.URL.Path, "/messages") {
				t.Error("unexpected route")
			}
			return botResponse(200, `{"id":"88888"}`), nil
		})}
		m := mentionFixture()
		m.Content = "<@12345> " + question
		b.handleMention(context.Background(), s, m)
		b.Wait()
		if count != 1 {
			t.Fatal("missing guidance")
		}
	}
}
