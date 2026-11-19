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
func TestLiveOrder66VoiceAndStyleFeedback(t *testing.T) {
	key := os.Getenv("ERIS_ROLEPLAY_LIVE_KEY_FILE")
	if key == "" {
		t.Skip("roleplay voice sample not enabled")
	}
	requests, tokens := 0, 0
	c := New("https://api.openai.com/v1", "", "gpt-6-luna")
	c.V2, c.Protocol, c.KeyFile = true, "responses", key
	c.HTTP.Transport = roleplaySampleTransport(func(r *http.Request) (*http.Response, error) {
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
	target := Speaker{ID: "22222", Name: "_p10u"}
	first, err := c.Order66(ctx, actor, []RoleplayTurn{{Author: actor, Text: "ordre 66 sur <@22222>", Mentions: []Speaker{target}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("order66=%q", first)
	if !strings.HasPrefix(first, "<@22222> ") {
		t.Error("target is not explicit", first)
	}
	for _, unwanted := range []string{"dans cette scène", "du théâtre", "sigil", "à un cheveu", "tout se fige"} {
		if strings.Contains(strings.ToLower(first), unwanted) {
			t.Error("reported melodrama remains", unwanted)
		}
	}
	incoming := DiscordInput{MessageID: "99992", Speaker: actor}
	second, err := c.ReplyWithDiscordContext(ctx, "23456:11111:33333", "Ouah c'est méga cringe. Faut que je corrige ça.", false, "Eris (message 88888) : "+first, incoming, func(context.Context, Plan) (ToolReply, error) {
		t.Error("style criticism invoked an external tool")
		return ToolReply{Content: `{"error":"no_external_actions_in_this_sample"}`}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("feedback=%q", second)
	for _, unwanted := range []string{"retirant toute menace", "retire toute menace", "retire la violence", "retirer la violence", "modifie mes directives", "modifie mon code"} {
		if strings.Contains(strings.ToLower(second), unwanted) {
			t.Error("style criticism rewrote the character", unwanted)
		}
	}
	if requests != 2 {
		t.Error("unexpected generation count", requests)
	}
}

type roleplaySampleTransport func(*http.Request) (*http.Response, error)

func (f roleplaySampleTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
