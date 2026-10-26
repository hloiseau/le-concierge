package eris

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"
)

type responseOutput struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Status  string `json:"status"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// Responses is text-only: no shell, hosted tools or model-controlled callbacks.
// OAuth provisioning/refresh belongs to the application credential provider.
func (c *Chat) responses(ctx context.Context, messages []Message) (string, error) {
	key := c.Key
	if c.KeyFile != "" {
		raw, err := os.ReadFile(c.KeyFile)
		if err != nil {
			return "", ErrUnavailable
		}
		key = strings.TrimSpace(string(raw))
	}
	if key == "" {
		return "", ErrUnavailable
	}
	input := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		role := message.Role
		if role == "system" {
			role = "developer"
		}
		var content any = message.Content
		if len(message.Images) > 0 {
			parts := []map[string]any{{"type": "input_text", "text": message.Content}}
			for _, data := range message.Images {
				parts = append(parts, map[string]any{"type": "input_image", "image_url": data, "detail": "auto"})
			}
			content = parts
		}
		input = append(input, map[string]any{"role": role, "content": content})
	}
	payload := map[string]any{"model": c.Model, "input": input, "store": false, "stream": true, "reasoning": map[string]string{"effort": "low"}, "text": map[string]any{"format": map[string]string{"type": "json_object"}}}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", c.URL+"/responses", bytes.NewReader(body))
	if err != nil {
		return "", ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return "", ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		return "", ErrBusy
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", ErrUnavailable
	}
	// Consume a bounded SSE stream; only the completed response is authoritative.
	var raw json.RawMessage
	var finished []responseOutput
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 256*1024+1))
	scanner.Buffer(make([]byte, 4096), 64*1024)
	total := 0
	for scanner.Scan() {
		line := scanner.Text()
		total += len(line) + 1
		if total > 256*1024 {
			return "", ErrUnavailable
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event struct {
			Type        string          `json:"type"`
			Response    json.RawMessage `json:"response"`
			OutputIndex *int            `json:"output_index"`
			Item        responseOutput  `json:"item"`
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		if json.Unmarshal([]byte(data), &event) != nil {
			return "", ErrUnavailable
		}
		if event.Type == "error" || event.Type == "response.failed" || event.Type == "response.incomplete" {
			return "", ErrUnavailable
		}
		if event.Type == "response.output_item.added" || event.Type == "response.output_item.done" {
			if event.Item.Type != "reasoning" && (event.Item.Type != "message" || event.Item.Role != "assistant") {
				return "", ErrUnavailable
			}
		}
		if event.Type == "response.output_item.done" {
			if event.OutputIndex == nil || *event.OutputIndex != len(finished) || (event.Item.Status != "" && event.Item.Status != "completed") {
				return "", ErrUnavailable
			}
			for _, part := range event.Item.Content {
				if part.Type != "output_text" {
					return "", ErrUnavailable
				}
			}
			finished = append(finished, event.Item)
		}
		if event.Type == "response.completed" {
			raw = event.Response
			break
		}
	}
	if scanner.Err() != nil || len(raw) == 0 {
		return "", ErrUnavailable
	}
	var out struct {
		Status string           `json:"status"`
		Output []responseOutput `json:"output"`
	}
	if json.Unmarshal(raw, &out) != nil || out.Status != "completed" {
		return "", ErrUnavailable
	}
	// The subscription route can complete with output:[]; finalized stream items
	// carry its text. Never use deltas or return before response.completed.
	if len(out.Output) == 0 {
		out.Output = finished
	}
	text := ""
	for _, item := range out.Output {
		if item.Type == "reasoning" {
			continue
		}
		if item.Type != "message" || item.Role != "assistant" || (item.Status != "" && item.Status != "completed") {
			return "", ErrUnavailable
		}
		for _, part := range item.Content {
			if part.Type != "output_text" {
				return "", ErrUnavailable
			}
			text += part.Text
		}
	}
	text = strings.TrimSpace(text)
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > 2500 {
		return "", ErrUnavailable
	}
	return text, nil
}
