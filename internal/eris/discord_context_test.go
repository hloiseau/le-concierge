package eris

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func discordContextChat(t *testing.T) (*Chat, chan []Message, *string) {
	t.Helper()
	prompts := make(chan []Message, 32)
	raw := `{"action":"chat","text":"Réponse visible."}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Messages []Message }
		_ = json.NewDecoder(r.Body).Decode(&body)
		prompts <- body.Messages
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: raw}}}})
	}))
	t.Cleanup(server.Close)
	c := New(server.URL, "", "model")
	c.V2 = true
	return c, prompts, &raw
}

func discordContextCall(t *testing.T, c *Chat, owner, question string, in DiscordInput, deliver bool) string {
	t.Helper()
	reply, err := c.ReplyWithDiscordContext(context.Background(), owner, question, false, "", in, func(context.Context, Plan) (ToolReply, error) {
		t.Error("unexpected media tool")
		return ToolReply{Content: "Unexpected tool."}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if deliver {
		c.Delivered(owner, "99999", reply)
	}
	return reply
}

func discordData(t *testing.T, messages []Message) map[string]json.RawMessage {
	t.Helper()
	for _, m := range messages {
		if strings.HasPrefix(m.Content, "Contexte Discord vérifié") {
			var data map[string]json.RawMessage
			if err := json.Unmarshal([]byte(strings.SplitN(m.Content, "\n", 2)[1]), &data); err != nil {
				t.Fatal(err)
			}
			return data
		}
	}
	t.Fatal("missing Discord identity context")
	return nil
}

func TestDiscordSpeakersAndPublicConversationStayInTheirChannel(t *testing.T) {
	c, prompts, _ := discordContextChat(t)
	alice := DiscordInput{MessageID: "50001", Speaker: Speaker{ID: "11111", Name: "Alice [BLEU]", Username: "alice"}, Mentions: []Speaker{{ID: "22222", Name: "Bob", Username: "bob"}}}
	bob := DiscordInput{MessageID: "50002", Speaker: Speaker{ID: "22222", Name: "Bob [ROUGE]", Username: "bob"}}
	discordContextCall(t, c, "12345:11111", "PRIVATE-MARKER", alice, true)
	<-prompts
	discordContextCall(t, c, "12345:11111:44444", "OTHER-CHANNEL", alice, true)
	<-prompts
	discordContextCall(t, c, "12345:11111:33333", "Bob a mangé les biscuits.", alice, true)
	first := discordData(t, <-prompts)
	var participants []Speaker
	_ = json.Unmarshal(first["participants"], &participants)
	if len(participants) != 1 || participants[0].ID != "11111" {
		t.Fatal("mentioned user mistaken for a speaker", participants)
	}
	discordContextCall(t, c, "12345:22222:33333", "C'est moi, et je ne regrette rien.", bob, true)
	messages := <-prompts
	data := discordData(t, messages)
	var current Speaker
	_ = json.Unmarshal(data["current"], &current)
	_ = json.Unmarshal(data["participants"], &participants)
	if current.ID != "22222" || current.Name != "Bob [ROUGE]" || len(participants) != 2 {
		t.Fatal(current, participants)
	}
	all, _ := json.Marshal(messages)
	if !strings.Contains(string(all), "Bob a mangé les biscuits") || !strings.Contains(string(all), "Alice [BLEU]") || strings.Contains(string(all), "PRIVATE-MARKER") || strings.Contains(string(all), "OTHER-CHANNEL") {
		t.Fatal("wrong public context", string(all))
	}
	if len(c.publicRooms) != 2 {
		t.Fatal("private command entered public history")
	}
}

func TestPublicHistoryDoesNotInventDeliveryAndForgettingClearsDerivatives(t *testing.T) {
	c, prompts, raw := discordContextChat(t)
	*raw = `{"action":"chat","text":"UNDELIVERED-REPLY"}`
	alice := DiscordInput{MessageID: "50001", Speaker: Speaker{ID: "11111", Name: "Alice"}}
	bob := DiscordInput{MessageID: "50002", Speaker: Speaker{ID: "22222", Name: "Bob"}}
	discordContextCall(t, c, "12345:11111:33333", "VISIBLE-HUMAN-MESSAGE", alice, false)
	<-prompts
	*raw = `{"action":"chat","text":"D'accord."}`
	discordContextCall(t, c, "12345:22222:33333", "Qui parle ?", bob, true)
	data := discordData(t, <-prompts)
	if !strings.Contains(string(data["other_public_exchanges"]), "VISIBLE-HUMAN-MESSAGE") || strings.Contains(string(data["other_public_exchanges"]), "UNDELIVERED-REPLY") {
		t.Fatal("unseen assistant response became public history")
	}
	if _, err := c.Reply(context.Background(), "12345:11111", "", true); err != nil {
		t.Fatal(err)
	}
	if len(c.publicRooms) != 0 {
		t.Fatal("public derivatives survived forgetting")
	}
	bob.MessageID = "50003"
	discordContextCall(t, c, "12345:22222:33333", "Qui reste ?", bob, true)
	data = discordData(t, <-prompts)
	if strings.Contains(string(data["participants"]), "Alice") || strings.Contains(string(data["other_public_exchanges"]), "VISIBLE-HUMAN") {
		t.Fatal("forgotten data reappeared")
	}
}

func TestOtherMembersPublicTitlesCannotAuthorizeAnAddition(t *testing.T) {
	c, prompts, raw := discordContextChat(t)
	discordContextCall(t, c, "12345:11111:33333", "Mon film préféré est Coherence.", DiscordInput{MessageID: "50001", Speaker: Speaker{ID: "11111", Name: "Alice"}}, true)
	<-prompts
	*raw = `{"action":"prepare","query":"Coherence","kind":"movie"}`
	reply := discordContextCall(t, c, "12345:22222:33333", "Demande ce film.", DiscordInput{MessageID: "50002", Speaker: Speaker{ID: "22222", Name: "Bob"}}, true)
	data := discordData(t, <-prompts)
	if !strings.Contains(string(data["other_public_exchanges"]), "Coherence") || !strings.Contains(reply, "Quel titre") {
		t.Fatal("social context became request authorization", reply)
	}
}

func TestMediaWithoutAnExplicitRequestCannotInvokeATool(t *testing.T) {
	c, prompts, raw := discordContextChat(t)
	*raw = `{"action":"prepare","query":"Moon","kind":"movie"}`
	in := DiscordInput{MessageID: "50001", Speaker: Speaker{ID: "11111", Name: "Alice"}, VisualNote: "Aucun aperçu disponible.", MediaOnly: true}
	reply := discordContextCall(t, c, "12345:11111:33333", "https://klipy.com/gifs/moon", in, true)
	<-prompts
	if !strings.Contains(reply, "média") {
		t.Fatal("media-only input was treated as a request", reply)
	}
}

func TestDiscordContextRejectsImpersonationAndBoundsItsWindow(t *testing.T) {
	in := DiscordInput{MessageID: "50001", Speaker: Speaker{ID: "22222", Name: "Bob"}}
	if in.normalized("12345:11111:33333").Speaker.ID != "" {
		t.Fatal("speaker differs from verified conversation owner")
	}
	c := New("", "", "")
	now := time.Now()
	c.observePublic("12345:11111:33333", "OLD", DiscordInput{MessageID: "50001", Speaker: Speaker{ID: "11111", Name: "Old"}}, now.Add(-31*time.Minute))
	c.observePublic("12345:22222:33333", "NEW", in, now)
	if strings.Contains(c.discordContext("12345:22222:33333", in, now), "OLD") {
		t.Fatal("expired context retained")
	}
	for i := 0; i < 150; i++ {
		owner := fmt.Sprintf("12345:22222:%05d", 40000+i)
		c.observePublic(owner, strings.Repeat("x", 1000), in, now)
	}
	if len(c.publicRooms) > 128 {
		t.Fatal("unbounded room count")
	}
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("%05d", 20000+i)
		c.observePublic("12345:"+id+":33333", "message", DiscordInput{MessageID: fmt.Sprintf("%05d", 50000+i), Speaker: Speaker{ID: id, Name: "Member"}}, now)
	}
	room := c.publicRooms["12345:33333"]
	if len(room.members) > 32 || len(room.exchanges) > 12 {
		t.Fatal("unbounded public window")
	}
}

func TestResponsesCarriesImagesWithoutRetainingTheirBytes(t *testing.T) {
	payloads := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		payloads <- string(body)
		fmt.Fprintln(w, `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"action\":\"chat\",\"text\":\"Une image.\"}"}]}]}}`)
	}))
	defer server.Close()
	c := New(server.URL, "key", "model")
	c.V2, c.Protocol = true, "responses"
	in := DiscordInput{MessageID: "50001", Speaker: Speaker{ID: "11111", Name: "Alice"}, Images: []string{"data:image/jpeg;base64,/9j/2Q=="}, VisualNote: "Une image jointe."}
	discordContextCall(t, c, "12345:11111:33333", "", in, true)
	first := <-payloads
	if !strings.Contains(first, `"type":"input_image"`) || !strings.Contains(first, `"type":"input_text"`) || !strings.Contains(first, "data:image/jpeg") {
		t.Fatal("image silently dropped", first)
	}
	for _, m := range c.sessions["12345:11111:33333"].messages {
		if len(m.Images) > 0 {
			t.Fatal("image bytes retained in history")
		}
	}
	in.MessageID, in.Images, in.VisualNote = "50002", nil, ""
	discordContextCall(t, c, "12345:11111:33333", "Merci", in, true)
	if strings.Contains(<-payloads, "data:image") {
		t.Fatal("image resent from old history")
	}
	if _, err := New("http://unused", "", "").complete(context.Background(), []Message{{Role: "user", Content: "image", Images: []string{"data:image/jpeg;base64,/9j/2Q=="}}}, false); err == nil {
		t.Fatal("legacy transport silently drops vision")
	}
}
