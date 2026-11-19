package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hloiseau/le-concierge/internal/eris"
)

func (b *Bot) handleEris(w http.ResponseWriter, i Interaction, proof eris.ToolRequest) {
	options, _ := CommandOptions(i)
	if options.ChatMessage == "" && !options.Forget {
		options.ChatMessage = "Eris ?"
	}
	if !b.reserveEris() {
		_ = json.NewEncoder(w).Encode(map[string]any{"type": 4, "data": Message("⚠️ `ERIS_BUSY`")})
		return
	}
	w.WriteHeader(http.StatusAccepted)
	b.workers.Add(1)
	go func() {
		defer b.workers.Done()
		defer func() { b.mu.Lock(); b.erisWorkers--; b.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if !b.acknowledge(ctx, i) {
			return
		}
		owner := i.GuildID + ":" + i.Member.User.ID
		if options.Forget {
			b.clearPublicErisGuildTurns(i.GuildID, i.Member.User.ID)
		}
		result := b.erisDiscordReply(ctx, owner, options.ChatMessage, options.Forget, proof, "", eris.DiscordInput{MessageID: i.ID, Speaker: discordSpeaker(i.Member.User.ID, i.Member.Nick, i.Member.User.GlobalName, i.Member.User.Username)})
		if id, ok := b.webhookReply(ctx, i, "PATCH", "/messages/@original", Message(result.Content)); ok {
			if id == "" {
				id = i.ID
			}
			b.Eris.Delivered(owner, id, result.Content)
		}
	}()
}

func (b *Bot) erisMediaReply(ctx context.Context, owner, message string, forget bool, proof eris.ToolRequest) eris.ToolReply {
	return b.erisContextReply(ctx, owner, message, forget, proof, "")
}
func (b *Bot) erisContextReply(ctx context.Context, owner, message string, forget bool, proof eris.ToolRequest, reference string) eris.ToolReply {
	return b.erisDiscordReply(ctx, owner, message, forget, proof, reference, eris.DiscordInput{})
}
func (b *Bot) erisDiscordReply(ctx context.Context, owner, message string, forget bool, proof eris.ToolRequest, reference string, incoming eris.DiscordInput) eris.ToolReply {
	guild, _, _ := strings.Cut(owner, ":")
	incoming.ChatOnly = guild != b.Config.DiscordGuildID
	calls := 0
	text, err := b.Eris.ReplyWithDiscordContext(ctx, owner, message, forget, reference, incoming, func(ctx context.Context, plan eris.Plan) (eris.ToolReply, error) {
		if incoming.ChatOnly {
			return eris.ToolReply{Content: `{"error":"tools_unavailable_on_this_server"}`}, nil
		}
		if proof.ContextMessageID != "" && !b.currentErisTurn(proof.ChannelID, proof.MessageID) {
			return eris.ToolReply{}, eris.ErrUnavailable
		}
		calls++
		proof.CallID = fmt.Sprintf("tool-%d", calls)
		proof.Plan = plan
		return b.erisTool(ctx, proof), nil
	})
	if errors.Is(err, eris.ErrBusy) {
		return eris.ToolReply{Content: "⚠️ `ERIS_BUSY`"}
	}
	if err != nil {
		return eris.ToolReply{Content: "⚠️ `ERIS_UNAVAILABLE`"}
	}
	return eris.ToolReply{Content: text}
}

func (b *Bot) erisTool(ctx context.Context, input eris.ToolRequest) eris.ToolReply {
	failure := eris.ToolReply{Content: `{"error":"hub_unavailable"}`}
	if input.Plan.Action == "request" {
		failure.Content = `{"error":"request_outcome_unknown","automaticRetry":false,"next":"check_status"}`
	}
	body, _ := json.Marshal(input)
	req, err := http.NewRequestWithContext(ctx, "POST", b.Config.BotAPIURL+"/internal/eris", bytes.NewReader(body))
	if err != nil {
		return failure
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.Config.BotAPISecret)
	client := *b.Client
	client.Timeout = 22 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return failure
	}
	defer resp.Body.Close()
	var result eris.ToolReply
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&result) != nil || result.Content == "" || !utf8.ValidString(result.Content) || utf8.RuneCountInString(result.Content) > 12000 {
		return failure
	}
	if result.Web != nil && ((input.Plan.Action != "web" && input.Plan.Action != "anime" && input.Plan.Action != "gifts") || !result.Web.Validate()) {
		return failure
	}
	if result.Diagnostic != nil && (input.Plan.Action != "diagnostic" || result.Web != nil || !result.Diagnostic.Valid()) {
		return failure
	}
	return result
}

func (b *Bot) reserveEris() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.erisWorkers >= 8 {
		return false
	}
	b.erisWorkers++
	return true
}
