package eris

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

type responseOutput struct {
	ID               string          `json:"id,omitempty"`
	Type             string          `json:"type"`
	Role             string          `json:"role,omitempty"`
	Status           string          `json:"status,omitempty"`
	CallID           string          `json:"call_id,omitempty"`
	Name             string          `json:"name,omitempty"`
	Arguments        string          `json:"arguments,omitempty"`
	EncryptedContent string          `json:"encrypted_content,omitempty"`
	Summary          json.RawMessage `json:"summary,omitempty"`
	Content          []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content,omitempty"`
}

func (c *Chat) responses(ctx context.Context, messages []Message, limits ...int) (string, error) {
	message, err := c.responseMessage(ctx, messages, true, nil, limits...)
	return message.Content, err
}

// Only registered local functions may be called. Hosted tools and arbitrary
// provider output types remain unavailable; credentials stay in the transport.
func (c *Chat) responseMessage(ctx context.Context, messages []Message, withJSON bool, definitions []toolDefinition, limits ...int) (Message, error) {
	key, err := c.apiKey()
	if err != nil || key == "" {
		return Message{}, ErrUnavailable
	}
	input := make([]any, 0, len(messages))
	for _, message := range messages {
		if len(message.ResponseItems) > 0 {
			for _, item := range message.ResponseItems {
				input = append(input, item)
			}
			continue
		}
		if message.Role == "tool" {
			input = append(input, map[string]any{"type": "function_call_output", "call_id": message.ToolCallID, "output": message.Content})
			continue
		}
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
	payload := map[string]any{"model": c.Model, "input": input, "store": false, "stream": true, "reasoning": map[string]string{"effort": "low"}}
	if len(definitions) > 0 {
		var tools []map[string]any
		for _, d := range definitions {
			tools = append(tools, map[string]any{"type": "function", "name": d.Function.Name, "description": d.Function.Description, "parameters": d.Function.Parameters, "strict": false})
		}
		payload["tools"], payload["tool_choice"], payload["include"] = tools, "auto", []string{"reasoning.encrypted_content"}
	} else if withJSON {
		payload["text"] = map[string]any{"format": map[string]string{"type": "json_object"}}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", c.URL+"/responses", bytes.NewReader(body))
	if err != nil {
		return Message{}, ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return Message{}, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		return Message{}, ErrBusy
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Message{}, ErrUnavailable
	}
	var raw json.RawMessage
	var finished []responseOutput
	allowed := func(item responseOutput) bool {
		return item.Type == "reasoning" || item.Type == "message" && item.Role == "assistant" || len(definitions) > 0 && item.Type == "function_call"
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 256*1024+1))
	scanner.Buffer(make([]byte, 4096), 64*1024)
	total := 0
	for scanner.Scan() {
		line := scanner.Text()
		total += len(line) + 1
		if total > 256*1024 {
			return Message{}, ErrUnavailable
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var event struct {
			Type        string          `json:"type"`
			Response    json.RawMessage `json:"response"`
			OutputIndex *int            `json:"output_index"`
			Item        responseOutput  `json:"item"`
		}
		if json.Unmarshal([]byte(data), &event) != nil || event.Type == "error" || event.Type == "response.failed" || event.Type == "response.incomplete" {
			return Message{}, ErrUnavailable
		}
		if (event.Type == "response.output_item.added" || event.Type == "response.output_item.done") && !allowed(event.Item) {
			return Message{}, ErrUnavailable
		}
		if event.Type == "response.output_item.done" {
			if event.OutputIndex == nil || *event.OutputIndex != len(finished) || (event.Item.Status != "" && event.Item.Status != "completed") {
				return Message{}, ErrUnavailable
			}
			for _, part := range event.Item.Content {
				if part.Type != "output_text" {
					return Message{}, ErrUnavailable
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
		return Message{}, ErrUnavailable
	}
	var result struct {
		Status string           `json:"status"`
		Output []responseOutput `json:"output"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Status != "completed" {
		return Message{}, ErrUnavailable
	}
	// The subscription route can complete with output:[]; only finalized items
	// followed by response.completed may supply the response in that case.
	if len(result.Output) == 0 {
		result.Output = finished
	}
	message := Message{Role: "assistant"}
	for _, item := range result.Output {
		if !allowed(item) || item.Status != "" && item.Status != "completed" {
			return Message{}, ErrUnavailable
		}
		if item.Type == "function_call" {
			if item.CallID == "" || item.Name == "" || item.Arguments == "" {
				return Message{}, ErrUnavailable
			}
			call := ToolCall{ID: item.CallID, Type: "function"}
			call.Function.Name = item.Name
			call.Function.Arguments = item.Arguments
			message.ToolCalls = append(message.ToolCalls, call)
		} else if item.Type == "message" {
			for _, part := range item.Content {
				if part.Type != "output_text" {
					return Message{}, ErrUnavailable
				}
				message.Content += part.Text
			}
		}
		encoded, _ := json.Marshal(item)
		message.ResponseItems = append(message.ResponseItems, encoded)
	}
	message.Content = strings.TrimSpace(message.Content)
	limit := 2500
	if c.V2 && c.Memory != nil {
		limit = 6000
	}
	if len(limits) > 0 && limits[0] > limit {
		limit = limits[0]
	}
	if message.Content == "" && len(message.ToolCalls) == 0 || len(message.ToolCalls) > 6 || !utf8.ValidString(message.Content) || utf8.RuneCountInString(message.Content) > limit {
		return Message{}, ErrUnavailable
	}
	return message, nil
}
