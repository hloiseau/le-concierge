package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/eris"
)

func humanReplyFixture() *discordgo.MessageCreate {
	m := mentionFixture()
	m.Type, m.Content = discordgo.MessageTypeReply, "<@12345> Tu peux vérifier cette annonce ?"
	m.MessageReference = &discordgo.MessageReference{MessageID: "88888", ChannelID: m.ChannelID, GuildID: m.GuildID}
	m.ReferencedMessage = &discordgo.Message{ID: "88888", ChannelID: m.ChannelID, Timestamp: time.Now(),
		Author: &discordgo.User{ID: "22222", Username: "Bob"}, Content: "Annonce : https://publisher.example/article"}
	return m
}

func TestReplyContextReadsHistoryInsideTheThread(t *testing.T) {
	for _, mode := range []string{"inline", "missing inline", "denied to member", "denied to bot", "deleted"} {
		t.Run(mode, func(t *testing.T) {
			b, _ := signedCommand(t, "eris")
			b.Eris.V2, b.Config.DiscordMessageContent = true, true
			event := humanReplyFixture()
			target := event.ReferencedMessage
			thread := &discordgo.Channel{ID: event.ChannelID, GuildID: event.GuildID, ParentID: "55555", Type: discordgo.ChannelTypeGuildPublicThread}
			parent := &discordgo.Channel{ID: thread.ParentID, GuildID: thread.GuildID}
			if mode == "denied to member" {
				parent.PermissionOverwrites = []*discordgo.PermissionOverwrite{{ID: event.Author.ID, Type: discordgo.PermissionOverwriteTypeMember, Deny: discordgo.PermissionReadMessageHistory}}
			}
			s, _ := discordgo.New("Bot fixture")
			if err := s.State.GuildAdd(&discordgo.Guild{ID: thread.GuildID, Channels: []*discordgo.Channel{parent}, Members: []*discordgo.Member{{User: event.Author}},
				Roles: []*discordgo.Role{{ID: thread.GuildID, Permissions: discordgo.PermissionViewChannel | discordgo.PermissionReadMessageHistory}}}); err != nil {
				t.Fatal(err)
			}
			if mode == "missing inline" || mode == "deleted" {
				event.ReferencedMessage = nil
			}
			history, lookups := 0, 0
			s.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				var body any
				switch {
				case strings.HasSuffix(r.URL.Path, "/channels/33333"):
					body = thread
				case strings.HasSuffix(r.URL.Path, "/channels/33333/messages/88888"):
					lookups++
					if mode == "deleted" {
						return botResponse(404, "{}"), nil
					}
					body = target
				case strings.HasSuffix(r.URL.Path, "/channels/33333/messages"):
					history++
					if r.Method != "GET" || r.URL.Query().Get("before") != target.ID || r.URL.Query().Get("limit") != "6" {
						t.Errorf("wrong history request: %s", r.URL)
					}
					if mode == "denied to bot" {
						return botResponse(403, "{}"), nil
					}
					msg := func(id, channel string) *discordgo.Message {
						return &discordgo.Message{ID: id, ChannelID: channel, Author: event.Author, Content: "extrait " + id, Timestamp: target.Timestamp.Add(-time.Second)}
					}
					hidden := msg("80002", thread.ID)
					hidden.Flags = discordgo.MessageFlagsEphemeral
					old := msg("80001", thread.ID)
					old.Timestamp = target.Timestamp.Add(-time.Hour)
					body = []*discordgo.Message{msg("99998", thread.ID), msg("80005", thread.ID), msg("80004", parent.ID), msg("80003", thread.ID), hidden, old}
				default:
					t.Errorf("unexpected route: %s", r.URL)
					return botResponse(400, "{}"), nil
				}
				raw, _ := json.Marshal(body)
				return botResponse(200, string(raw)), nil
			})}
			in := b.erisDiscordInput(context.Background(), s, event.Message)
			count := 3
			if mode == "denied to member" || mode == "denied to bot" {
				count = 1
			} else if mode == "deleted" {
				count = 0
			}
			if len(in.ReplyContext) != count || in.QuoteUnavailable != (mode == "deleted") ||
				(history == 1) != (mode != "denied to member" && mode != "deleted") ||
				(lookups == 1) != (mode == "missing inline" || mode == "deleted") {
				t.Fatalf("history=%d lookups=%d context=%+v", history, lookups, in)
			}
			if count > 0 {
				quote := in.ReplyContext[count-1]
				if !quote.Quoted || quote.Author.ID != target.Author.ID || quote.Text != target.Content || in.Speaker.ID != event.Author.ID {
					t.Fatalf("wrong attribution: %+v", in)
				}
			}
			if count == 3 && (in.ReplyContext[0].MessageID != "80003" || in.ReplyContext[1].MessageID != "80005") {
				t.Fatalf("wrong history order: %+v", in.ReplyContext)
			}
		})
	}
}

func TestQuotedHumanMediaReachesVisionOnlyForAnAddressedLocalReply(t *testing.T) {
	for _, mode := range []string{"addressed", "members talking", "forward", "other guild", "ephemeral", "webhook"} {
		t.Run(mode, func(t *testing.T) {
			b, _ := signedCommand(t, "eris")
			b.Eris.V2, b.Eris.Protocol = true, "responses"
			event := humanReplyFixture()
			r := event.ReferencedMessage
			r.Attachments = []*discordgo.MessageAttachment{{ContentType: "image/png", URL: "https://cdn.discordapp.com/attachments/example.png"}}
			switch mode {
			case "members talking":
				event.Content = "Bob, tu confirmes ?"
			case "forward":
				event.MessageReference.Type = discordgo.MessageReferenceTypeForward
			case "other guild":
				r.GuildID = "55555"
			case "ephemeral":
				r.Flags = discordgo.MessageFlagsEphemeral
			case "webhook":
				r.WebhookID = "66666"
			}
			requests := 0
			b.Client.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.String() != r.Attachments[0].URL || req.Header.Get("Authorization") != "" {
					t.Errorf("unexpected image request: %s", req.URL)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(erisPNGFixture(t))), Header: make(http.Header)}, nil
			})
			in := b.erisDiscordInput(context.Background(), nil, event.Message)
			ok := mode == "addressed"
			if (requests == 1) != ok || (len(in.Images) == 1) != ok || strings.Contains(in.VisualNote, "Message cité") != ok {
				t.Fatalf("reads=%d images=%d note=%s", requests, len(in.Images), in.VisualNote)
			}
			if mode == "members talking" {
				if _, answered := b.mentionQuestion(event); answered {
					t.Fatal("member-to-member reply invited Eris")
				}
			}
		})
	}
}

func TestMentionReadsQuotedLinkWithCurrentMessageProof(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	b.Eris.V2, b.Eris.URL, b.Config.BotAPIURL = true, "https://model.test", "https://api.test"
	event := humanReplyFixture()
	reads, rounds, sends := 0, 0, 0
	b.Eris.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		rounds++
		var req struct{ Messages []eris.Message }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if rounds == 1 {
			found := false
			for _, m := range req.Messages {
				found = found || m.Role == "user" && strings.Contains(m.Content, event.ReferencedMessage.Content) && strings.Contains(m.Content, `"quoted":true`) && strings.Contains(m.Content, "Bob")
			}
			if !found || req.Messages[len(req.Messages)-1].Content != "Tu peux vérifier cette annonce ?" {
				t.Error("quoted source and active request were not kept separate")
			}
			return botResponse(200, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"read1","type":"function","function":{"name":"web","arguments":"{\"url\":\"https://publisher.example/article\"}"}}]}}]}`), nil
		}
		result := req.Messages[len(req.Messages)-2]
		if result.Role != "tool" || !strings.Contains(result.Content, "La porte ouvre vendredi.") {
			t.Error("page evidence did not reach model")
		}
		return botResponse(200, `{"choices":[{"message":{"role":"assistant","content":"La porte ouvre vendredi [1]."}}]}`), nil
	})
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		reads++
		var proof eris.ToolRequest
		_ = json.NewDecoder(r.Body).Decode(&proof)
		if proof.Plan.Action != "web" || proof.Plan.URL != "https://publisher.example/article" || proof.MessageID != event.ID || proof.ChannelID != event.ChannelID {
			t.Error("web request did not use current member's proof", proof)
		}
		return botResponse(200, `{"content":"ok","web":{"retrievedAt":"2026-11-28T12:00:00Z","sources":[{"kind":"page","url":"https://publisher.example/article","title":"Annonce","text":"La porte ouvre vendredi."}]}}`), nil
	})
	s, _ := discordgo.New("Bot fixture")
	s.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/typing") {
			return botResponse(204, ""), nil
		}
		sends++
		var msg discordgo.MessageSend
		_ = json.NewDecoder(r.Body).Decode(&msg)
		if r.Method != "POST" || !strings.Contains(msg.Content, "vendredi") || !strings.Contains(msg.Content, "https://publisher.example/article") {
			t.Error("missing grounded Discord reply", msg.Content)
		}
		return botResponse(200, `{"id":"99998"}`), nil
	})}
	b.handleMention(context.Background(), s, event)
	b.Wait()
	if reads != 1 || rounds != 2 || sends != 1 {
		t.Fatalf("web=%d model=%d replies=%d", reads, rounds, sends)
	}
}
