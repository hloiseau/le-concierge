package eris

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestOrder66AsksNaturallyWhenThereIsNoIdentifiableTarget(t *testing.T) {
	server := featureModelServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []Message       `json:"messages"`
			Tools    json.RawMessage `json:"tools"`
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil || len(payload.Tools) != 0 {
			t.Error("verbal reply gained external tools")
		}
		var conversation struct {
			Invoker Speaker       `json:"invoker"`
			Turns   []Order66Turn `json:"conversation"`
		}
		json.Unmarshal([]byte(payload.Messages[len(payload.Messages)-1].Content), &conversation)
		if conversation.Invoker.ID != "11111" || len(conversation.Turns) != 1 {
			t.Error("model lost the actual invocation context")
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"action\":\"chat\",\"text\":\"Qui est ma cible ?\"}"}}]}`))
	}))
	defer server.Close()
	c := New(server.URL, "", "model")
	c.V2 = true
	actor := Speaker{ID: "11111", Name: "Hugo"}
	reply, err := c.Order66(context.Background(), actor, []Order66Turn{{Author: actor, Text: "ordre66"}})
	if err != nil || reply != "Qui est ma cible ?" || len(c.sessions) != 0 {
		t.Fatal("ambiguous verbal attack was not an isolated clarification", reply, err)
	}
}

func TestOrder66CanDemonstrateWithoutARealMember(t *testing.T) {
	for _, scenario := range []struct {
		question  string
		bystander bool
	}{{"ordre 66 de test", false}, {"ordre66, fais une démonstration sans cible réelle", false}, {"ordre66, test sans cible", true}} {
		t.Run(scenario.question, func(t *testing.T) {
			calls := 0
			server := featureModelServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var payload struct {
					Messages []Message
					Tools    json.RawMessage
				}
				json.NewDecoder(r.Body).Decode(&payload)
				if len(payload.Tools) != 0 {
					t.Error("demonstration gained external tools")
				}
				for _, message := range payload.Messages {
					if strings.Contains(message.Content, `"status":"clarification_required"`) {
						t.Error("demo was forced into a missing-member clarification")
					}
				}
				answer := `{"action":"chat","text":"Tu confonds parler fort et avoir quelque chose à dire."}`
				if scenario.bystander {
					answer = `{"targetDiscordId":"","text":"Tu confonds parler fort et avoir quelque chose à dire."}`
				}
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: answer}}}})
			}))
			defer server.Close()
			c := New(server.URL, "", "model")
			c.V2 = true
			actor := Speaker{ID: "11111", Name: "Hugo"}
			turns := []Order66Turn{}
			if scenario.bystander {
				turns = append(turns, Order66Turn{Author: Speaker{ID: "22222", Name: "Autre membre"}, Text: "Salut"})
			}
			turns = append(turns, Order66Turn{Author: actor, Text: scenario.question})
			reply, err := c.Order66(context.Background(), actor, turns)
			if err != nil || reply != "Tu confonds parler fort et avoir quelque chose à dire." || calls != 1 {
				t.Fatal("test without a real target was refused", reply, err, calls)
			}
		})
	}
}
