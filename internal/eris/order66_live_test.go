package eris

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in editorial sample: two generations, synthetic identities, no Discord or
// media access. Keep the request budget in the transport, including retries.
func TestLiveOrder66VerbalAttackAndDemo(t *testing.T) {
	key := os.Getenv("ERIS_ORDER66_LIVE_KEY_FILE")
	if key == "" {
		t.Skip("verbal attack sample not enabled")
	}
	requests, tokens := 0, 0
	c := New("https://api.openai.com/v1", "", "gpt-6-luna")
	c.V2, c.Protocol, c.KeyFile = true, "responses", key
	c.HTTP.Transport = order66SampleTransport(func(r *http.Request) (*http.Response, error) {
		if requests >= 2 || r.URL.Scheme != "https" || r.URL.Host != "api.openai.com" || r.URL.Path != "/v1/responses" {
			return nil, ErrUnavailable
		}
		requests++
		response, err := http.DefaultTransport.RoundTrip(r)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 256*1024+1))
		response.Body.Close()
		if err != nil {
			return nil, err
		}
		response.Body = io.NopCloser(bytes.NewReader(body))
		for _, line := range bytes.Split(body, []byte("\n")) {
			if !bytes.HasPrefix(line, []byte("data: ")) {
				continue
			}
			var event struct {
				Type     string `json:"type"`
				Response struct {
					Usage struct {
						Total int `json:"total_tokens"`
					} `json:"usage"`
				} `json:"response"`
			}
			if json.Unmarshal(line[6:], &event) == nil && event.Type == "response.completed" {
				tokens += event.Response.Usage.Total
			}
		}
		return response, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	defer func() { t.Logf("model_requests=%d total_tokens=%d", requests, tokens) }()
	actor := Speaker{ID: "11111", Name: "Hugo"}
	target := Speaker{ID: "22222", Name: "Autre membre"}
	first := "<@22222> T’es le seul à avoir un avis valable ? Même ton ego doit en avoir marre de t’écouter."
	expectedRequests := 0
	t.Run("targeted", func(t *testing.T) {
		expectedRequests++
		var err error
		first, err = c.Order66(ctx, actor, []Order66Turn{{Author: target, Text: "Je suis le seul à avoir un avis valable ici."}, {Author: actor, Text: "ordre 66 sur <@22222>", Mentions: []Speaker{target}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("order66=%q", first)
		if !strings.HasPrefix(first, "<@22222> ") {
			t.Error("verbal attack lost the requested member", first)
		}
		for _, unwanted := range []string{"dans cette scène", "du théâtre", "sigil", "à un cheveu", "tout se fige", "cible humaine éligible", "il manque une cible", "l’empire attendra", "je te plaque", "je lui arrache", "mâchoire", "*je souris"} {
			if strings.Contains(strings.ToLower(first), unwanted) {
				t.Error("physical scene or administrative wording remains", unwanted)
			}
		}
	})
	t.Run("untargeted_example", func(t *testing.T) {
		expectedRequests++
		incoming := DiscordInput{MessageID: "99992", Speaker: actor}
		second, err := c.ReplyWithDiscordContext(ctx, "23456:11111:33333", "Je veux faire un test sans cible", false, "Eris (message 88888) : "+first, incoming, func(context.Context, Plan) (ToolReply, error) {
			t.Error("verbal example invoked an external tool")
			return ToolReply{Content: `{"error":"no_external_actions_in_this_sample"}`}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("followup=%q", second)
		for _, unwanted := range []string{"sans cible, y a pas", "sans cible il n'y a pas", "cible humaine éligible", "il manque une cible", "donne-moi une cible", "je te plaque", "je lui arrache", "mâchoire", "*je souris", "sigil", "du théâtre", "balance le test"} {
			if strings.Contains(strings.ToLower(second), unwanted) {
				t.Error("demonstration was refused without a real target", unwanted)
			}
		}
		if strings.Contains(second, "<@") {
			t.Error("followup selected a Discord member", second)
		}
	})
	if requests != expectedRequests {
		t.Error("unexpected generation count", requests)
	}
}

type order66SampleTransport func(*http.Request) (*http.Response, error)

func (f order66SampleTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
