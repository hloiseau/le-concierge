package eris

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

type completionBudget struct{ Tokens, Runes int }

func (c *Chat) dolly() bool { return c.Model == "dolly2-security" }

var dollyKeyFormat = regexp.MustCompile(`^sk-[A-Za-z0-9_-]{20,197}$`)

func ValidDollyKey(key string) bool {
	return dollyKeyFormat.MatchString(key) && !strings.HasPrefix(key, "sk-proj-") && !strings.HasPrefix(key, "sk-svcacct-")
}

func (c *Chat) complete(ctx context.Context, messages []Message, withTools bool, budgets ...completionBudget) (string, error) {
	message, err := c.completeMessage(ctx, messages, withTools, nil, budgets...)
	return message.Content, err
}

func (c *Chat) completeMessage(ctx context.Context, messages []Message, withTools bool, definitions []toolDefinition, budgets ...completionBudget) (Message, error) {
	if c.V2 && textRunes(messages) > contextTextLimit {
		return Message{}, ErrUnavailable
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
			return Message{}, ErrUnavailable
		}
		text, err := c.responses(ctx, messages, rawLimit)
		return Message{Role: "assistant", Content: text}, err
	}
	for _, message := range messages {
		if len(message.Images) > 0 {
			return Message{}, ErrUnavailable
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
	if len(definitions) > 0 {
		payload["tools"], payload["tool_choice"] = definitions, "auto"
		delete(payload, "response_format")
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", c.URL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	key, err := c.apiKey()
	if err != nil || c.dolly() && !ValidDollyKey(key) {
		return Message{}, ErrUnavailable
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return Message{}, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return Message{}, ErrBusy
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Message{}, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit+1))
	if err != nil || int64(len(raw)) > bodyLimit {
		return Message{}, ErrUnavailable
	}
	var completion struct {
		Model   string `json:"model"`
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &completion) != nil || len(completion.Choices) != 1 || completion.Choices[0].Message.Role != "assistant" {
		return Message{}, ErrUnavailable
	}
	message := completion.Choices[0].Message
	toolResponse := len(definitions) > 0 && len(message.ToolCalls) > 0 && len(message.ToolCalls) <= 6 && completion.Choices[0].FinishReason == "tool_calls"
	if c.dolly() && (completion.Model != c.Model || (completion.Choices[0].FinishReason != "stop" && !toolResponse)) {
		return Message{}, ErrUnavailable
	}
	message.Content = strings.TrimSpace(message.Content)
	if (!toolResponse && message.Content == "") || !utf8.ValidString(message.Content) || utf8.RuneCountInString(message.Content) > rawLimit || (len(message.ToolCalls) > 0 && !toolResponse) {
		return Message{}, ErrUnavailable
	}
	return message, nil
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
