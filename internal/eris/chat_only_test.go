package eris

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChatOnlyKeepsDurableMemoryWithoutExternalActions(t *testing.T) {
	m := openTestMemory(t)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Messages []Message }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		prompt := body.Messages[0].Content
		if !strings.Contains(prompt, "discussion_seule") || !strings.Contains(prompt, DurableMemoryInstructions) || strings.Contains(prompt, "<actions>") {
			t.Error("chat-only prompt lost memory or advertised external actions")
		}
		plan := `{"action":"chat","text":"La stratégie, ça me parle.","memories":[{"kind":"preference","topic":"jeux strategie","quote":"Je préfère les jeux de stratégie.","summary":"Préfère les jeux de stratégie.","aliases":["tactique"]}]}`
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: plan}}}})
	}))
	defer model.Close()
	c := New(model.URL, "", "fixture")
	c.V2, c.Memory = true, m
	owner := "34567:11111:33333"
	input := memoryInput("99999", "11111")
	input.ChatOnly = true
	reply, err := c.ReplyWithDiscordContext(context.Background(), owner, "Je préfère les jeux de stratégie.", false, "", input, func(context.Context, Plan) (ToolReply, error) {
		t.Error("chat-only memory called an external action")
		return ToolReply{}, nil
	})
	if err != nil || reply != "La stratégie, ça me parle." {
		t.Fatal(reply, err)
	}
	c.Delivered(owner, "88888", reply)
	bundle, err := m.recallDurable(owner, "stratégie", input, 0)
	if err != nil || len(bundle.Notes) != 1 {
		t.Fatal("chat-only member preference was not saved", bundle, err)
	}
	other := "23456:11111:33333"
	bundle, err = m.recallDurable(other, "stratégie", input, 0)
	if err != nil || len(bundle.Notes) != 0 || len(bundle.Episodes) != 0 {
		t.Fatal("memory crossed servers", bundle, err)
	}
	id := observeTest(t, m, other, "Mon projet s'appelle Vesper.", "77777")
	if err := m.deliverTurn(id, other, "Vesper.", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reply(context.Background(), "34567:11111", "", true); err != nil {
		t.Fatal(err)
	}
	bundle, err = m.recallDurable(other, "Vesper", input, 0)
	if err != nil || len(bundle.Episodes) != 1 {
		t.Fatal("forget removed another server's memory", bundle, err)
	}
	bundle, err = m.recallDurable(owner, "stratégie", input, 0)
	if err != nil || len(bundle.Notes) != 0 || len(bundle.Episodes) != 0 {
		t.Fatal("forget failed in chat-only guild", bundle, err)
	}
}

func TestForgetDoesNotClearOtherGuildCachedHistory(t *testing.T) {
	c := New("", "", "")
	c.V2 = true
	c.publicRooms = map[string]*publicRoom{
		"23456:33333": {},
		"34567:44444": {},
	}
	c.sessions["23456:11111:33333"] = &conversation{messages: []Message{{Role: "user", Content: "Premier serveur"}}, expires: time.Now().Add(time.Hour)}
	c.sessions["34567:11111:44444"] = &conversation{messages: []Message{{Role: "user", Content: "Second serveur"}}, expires: time.Now().Add(time.Hour)}
	if _, err := c.Reply(context.Background(), "34567:11111", "", true); err != nil {
		t.Fatal(err)
	}
	if c.publicRooms["23456:33333"] == nil || len(c.sessions["23456:11111:33333"].messages) != 1 {
		t.Fatal("forget cleared another guild's cache")
	}
	if c.publicRooms["34567:44444"] != nil || c.sessions["34567:11111:44444"] != nil {
		t.Fatal("forgotten guild retained cached history")
	}
}
