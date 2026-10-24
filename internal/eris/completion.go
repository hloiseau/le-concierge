package eris

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

func (c *Chat) complete(ctx context.Context, messages []Message, withTools bool) (string, error) {
	if c.Protocol == "responses" {
		return c.responses(ctx, messages)
	}
	temperature := 0.65
	if withTools {
		temperature = 0.45
	}
	payload := map[string]any{"model": c.Model, "messages": messages, "max_tokens": 160, "temperature": temperature, "stream": false}
	if withTools {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", c.URL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	resp, err := c.HTTP.Do(req)
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024+1))
	if err != nil || len(raw) > 16*1024 {
		return "", ErrUnavailable
	}
	var completion struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &completion) != nil || len(completion.Choices) != 1 || completion.Choices[0].Message.Role != "assistant" {
		return "", ErrUnavailable
	}
	reply := strings.TrimSpace(completion.Choices[0].Message.Content)
	if reply == "" || !utf8.ValidString(reply) || utf8.RuneCountInString(reply) > 1500 {
		return "", ErrUnavailable
	}
	return reply, nil
}
