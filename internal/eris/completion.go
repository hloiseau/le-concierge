package eris

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"
)

type completionBudget struct{ Tokens, Runes int }

func (c *Chat) dolly() bool { return c.Model == "dolly2-security" }

func (c *Chat) complete(ctx context.Context, messages []Message, withTools bool, budgets ...completionBudget) (string, error) {
	if c.V2 && textRunes(messages) > contextTextLimit {
		return "", ErrUnavailable
	}
	rawLimit := 1500
	tokens := 160
	if c.V2 && c.Memory != nil && withTools {
		tokens, rawLimit = 800, 6000
	}
	if len(budgets) > 0 {
		tokens, rawLimit = budgets[0].Tokens, budgets[0].Runes
	}
	if c.Protocol == "responses" {
		if c.dolly() {
			return "", ErrUnavailable
		}
		return c.responses(ctx, messages, rawLimit)
	}
	for _, message := range messages {
		if len(message.Images) > 0 {
			return "", ErrUnavailable
		}
	}
	temperature := 0.65
	if withTools {
		temperature = 0.45
	}
	payload := map[string]any{"model": c.Model, "messages": messages, "max_tokens": tokens, "temperature": temperature, "stream": false}
	bodyLimit := int64(16 * 1024)
	if c.dolly() {
		// Zro counts hidden reasoning against the same completion budget.
		payload["max_tokens"] = 1536
		payload["reasoning_effort"] = "low"
		bodyLimit = 128 * 1024
	}
	if withTools {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", c.URL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	key, err := c.apiKey()
	if err != nil || c.dolly() && len(key) < 32 {
		return "", ErrUnavailable
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return "", ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", ErrBusy
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit+1))
	if err != nil || int64(len(raw)) > bodyLimit {
		return "", ErrUnavailable
	}
	var completion struct {
		Model   string `json:"model"`
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &completion) != nil || len(completion.Choices) != 1 || completion.Choices[0].Message.Role != "assistant" {
		return "", ErrUnavailable
	}
	if c.dolly() && (completion.Model != c.Model || completion.Choices[0].FinishReason != "stop") {
		return "", ErrUnavailable
	}
	reply := strings.TrimSpace(completion.Choices[0].Message.Content)
	if reply == "" || !utf8.ValidString(reply) || utf8.RuneCountInString(reply) > rawLimit {
		return "", ErrUnavailable
	}
	return reply, nil
}

func (c *Chat) apiKey() (string, error) {
	if c.KeyFile == "" {
		return c.Key, nil
	}
	raw, err := os.ReadFile(c.KeyFile)
	if err != nil {
		return "", ErrUnavailable
	}
	return strings.TrimSpace(string(raw)), nil
}
