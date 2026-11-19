package eris

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hloiseau/le-concierge/internal/websearch"
)

type Plan struct {
	Action          string   `json:"action"`
	Query           string   `json:"query,omitempty"`
	MediaID         int64    `json:"mediaId,omitempty"`
	TargetDiscordID string   `json:"targetDiscordId,omitempty"`
	Relation        string   `json:"relation,omitempty"`
	Source          string   `json:"source,omitempty"`
	Kind            string   `json:"kind,omitempty"`
	Genre           string   `json:"genre,omitempty"`
	MaxMinutes      int      `json:"maxMinutes,omitempty"`
	Unwatched       bool     `json:"unwatched,omitempty"`
	Seasons         []int    `json:"seasons,omitempty"`
	Text            string   `json:"text,omitempty"`
	URL             string   `json:"url,omitempty"`
	Year            int      `json:"year,omitempty"`
	RequestID       int64    `json:"requestId,omitempty"`
	SubscriptionID  int64    `json:"subscriptionId,omitempty"`
	Channel         string   `json:"channel,omitempty"`
	Setting         string   `json:"setting,omitempty"`
	Value           string   `json:"value,omitempty"`
	Options         []string `json:"options,omitempty"`
	DurationHours   int      `json:"durationHours,omitempty"`
	Multiselect     bool     `json:"multiselect,omitempty"`
	StartAt         string   `json:"startAt,omitempty"`
	EndAt           string   `json:"endAt,omitempty"`
}

func ParsePlan(raw string) (Plan, error) {
	var p Plan
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Validate() != nil {
		return Plan{}, errors.New("invalid media plan")
	}
	return p, nil
}

func (p Plan) Validate() error {
	invalid := errors.New("invalid media plan")
	if p.TargetDiscordID != "" && ((p.TargetDiscordID != "self" && !discordID(p.TargetDiscordID)) || (p.Action != "request" && p.Action != "status")) {
		return invalid
	}
	if p.Action == "lookup" || p.Action == "collection" || p.Action == "request" {
		if p.Text != "" || p.Relation != "" || p.Source != "" || p.Genre != "" || p.MaxMinutes != 0 || p.Unwatched || p.URL != "" || p.RequestID != 0 || p.SubscriptionID != 0 || p.Channel != "" || p.Setting != "" || p.Value != "" || len(p.Options) > 0 || p.DurationHours != 0 || p.Multiselect || p.StartAt != "" || p.EndAt != "" {
			return invalid
		}
		if p.Action == "lookup" {
			if p.MediaID != 0 || len(p.Seasons) > 0 || strings.TrimSpace(p.Query) != p.Query || strings.ContainsFunc(p.Query, unicode.IsControl) || utf8.RuneCountInString(p.Query) < 2 || utf8.RuneCountInString(p.Query) > 200 || (p.Kind != "" && p.Kind != "movie" && p.Kind != "show") || (p.Year != 0 && (p.Year < 1801 || p.Year > 2999)) {
				return invalid
			}
			return nil
		}
		if p.MediaID <= 0 || p.MediaID > 2147483647 || p.Query != "" || p.Year != 0 || (p.Kind != "movie" && p.Kind != "show") || len(p.Seasons) > 20 {
			return invalid
		}
		if p.Action == "collection" && (p.Kind != "movie" || len(p.Seasons) > 0) || p.Kind == "movie" && len(p.Seasons) > 0 || p.Action == "request" && p.Kind == "show" && len(p.Seasons) == 0 {
			return invalid
		}
		seen := map[int]bool{}
		for _, season := range p.Seasons {
			if season < 1 || season > 1000 || seen[season] {
				return invalid
			}
			seen[season] = true
		}
		return nil
	}
	if p.MediaID != 0 && p.Action != "follow" {
		return invalid
	}
	if (len(p.Options) > 0 || p.DurationHours != 0 || p.Multiselect) && p.Action != "poll" || (p.StartAt != "" || p.EndAt != "") && p.Action != "event" {
		return invalid
	}
	if (p.SubscriptionID != 0 && p.Action != "unfollow") || (p.Channel != "" && p.Action != "follow") || ((p.Setting != "" || p.Value != "") && p.Action != "preferences") || (p.RequestID != 0 && p.Action != "diagnostic") || (p.Year != 0 && p.Action != "cinema" && p.Action != "follow" && p.Action != "anime") {
		return invalid
	}
	if p.Action == "poll" || p.Action == "event" {
		return p.validateDiscordCreation()
	}
	if p.Action == "gifts" {
		if p.URL != "" || p.Kind != "" || p.Source != "" || p.Relation != "" || p.Genre != "" || p.MaxMinutes != 0 || p.Unwatched || len(p.Seasons) > 0 || p.Year != 0 || utf8.RuneCountInString(p.Query) > 100 || utf8.RuneCountInString(p.Text) > 200 || strings.TrimSpace(p.Query) != p.Query || strings.TrimSpace(p.Text) != p.Text || strings.ContainsFunc(p.Query+p.Text, unicode.IsControl) {
			return invalid
		}
		return nil
	}
	if p.Action == "anime" {
		if p.Year != 0 && p.Query == "" {
			return invalid
		}
		if p.Text != "" || p.URL != "" || p.Source != "" || p.MaxMinutes != 0 || p.Unwatched || len(p.Seasons) > 0 || (p.Kind != "anime" && p.Kind != "manga") || (p.Year != 0 && (p.Year < 1801 || p.Year > 2999)) || (p.Relation != "" && p.Relation != "recommend" && p.Relation != "french") || strings.TrimSpace(p.Query) != p.Query || strings.ContainsFunc(p.Query, unicode.IsControl) || utf8.RuneCountInString(p.Query) > 200 || (p.Query != "" && utf8.RuneCountInString(p.Query) < 2) || (p.Query == "" && p.Relation != "recommend") || (p.Genre != "" && (p.Relation != "recommend" || p.Query != "" || !validAnimeGenre(p.Genre))) || (p.Relation == "french" && (p.Kind != "manga" || p.Year != 0)) {
			return invalid
		}
		return nil
	}
	if p.Action == "follow" || p.Action == "follows" || p.Action == "unfollow" || p.Action == "preferences" {
		return p.validatePersonalTool()
	}
	if p.Action == "diagnostic" {
		if p.Text != "" || p.Kind != "" || p.Relation != "" || p.Source != "" || p.URL != "" || p.Year != 0 || p.Genre != "" || p.MaxMinutes != 0 || p.Unwatched || len(p.Seasons) > 0 || p.RequestID < 0 || p.RequestID > 2147483647 || (p.RequestID != 0 && p.Query != "") || (p.RequestID == 0 && (strings.TrimSpace(p.Query) != p.Query || strings.ContainsFunc(p.Query, unicode.IsControl) || utf8.RuneCountInString(p.Query) < 2 || utf8.RuneCountInString(p.Query) > 200)) {
			return invalid
		}
		return nil
	}
	if p.RequestID != 0 {
		return invalid
	}
	if p.Action == "cinema" {
		if strings.TrimSpace(p.Query) != p.Query || strings.ContainsFunc(p.Query, unicode.IsControl) || utf8.RuneCountInString(p.Query) < 2 || utf8.RuneCountInString(p.Query) > 200 || (p.Kind != "" && p.Kind != "movie" && p.Kind != "show") || (p.Year != 0 && (p.Year < 1801 || p.Year > 2999)) || (p.Relation != "" && p.Relation != "cast" && p.Relation != "director" && p.Relation != "trailer" && p.Relation != "overview") || p.URL != "" || p.Text != "" || p.Source != "" || p.Genre != "" || p.MaxMinutes != 0 || p.Unwatched || len(p.Seasons) > 0 {
			return invalid
		}
		return nil
	}
	if p.Year != 0 {
		return invalid
	}
	if p.Action == "web" {
		if p.Text != "" || p.Genre != "" || p.MaxMinutes != 0 || p.Unwatched || len(p.Seasons) > 0 || p.Relation != "" || p.Source != "" || (p.Kind != "" && p.Kind != "youtube") {
			return invalid
		}
		if p.URL != "" {
			if p.Query != "" || p.Kind != "" || !websearch.PublicURL(p.URL) || len(p.URL) > 300 {
				return invalid
			}
		} else if strings.TrimSpace(p.Query) != p.Query || strings.ContainsFunc(p.Query, unicode.IsControl) || utf8.RuneCountInString(p.Query) < 2 || utf8.RuneCountInString(p.Query) > 200 {
			return invalid
		}
		return nil
	}
	if p.URL != "" {
		return invalid
	}
	for _, s := range []string{p.Query, p.Genre} {
		if strings.TrimSpace(s) != s || strings.ContainsFunc(s, unicode.IsControl) || utf8.RuneCountInString(s) > 200 {
			return invalid
		}
	}
	if p.Kind != "" && p.Kind != "movie" && p.Kind != "show" {
		return invalid
	}
	if p.MaxMinutes < 0 || p.MaxMinutes > 600 || len(p.Genre) > 40 || len(p.Seasons) > 20 || utf8.RuneCountInString(p.Text) > 1000 {
		return invalid
	}
	seen := map[int]bool{}
	for _, n := range p.Seasons {
		if n < 1 || n > 1000 || seen[n] {
			return invalid
		}
		seen[n] = true
	}
	switch p.Action {
	case "silent":
		if p.Text != "" || p.Query != "" || p.Kind != "" {
			return invalid
		}
	case "chat":
		if strings.TrimSpace(p.Text) == "" || p.Query != "" || p.Kind != "" {
			return invalid
		}
	case "search":
		if utf8.RuneCountInString(p.Query) < 2 || p.Text != "" {
			return invalid
		}
	case "recommend":
		if p.Text != "" || (p.Source != "" && p.Source != "plex" && p.Source != "seerr") || (p.Relation != "" && p.Relation != "similar" && p.Relation != "director") {
			return invalid
		}
		if (p.Query == "") != (p.Relation == "") || (p.Relation != "" && (utf8.RuneCountInString(p.Query) < 2 || p.Kind == "show" || p.Genre != "" || p.MaxMinutes != 0)) || (p.Source == "seerr" && (p.Relation == "" || p.Unwatched)) {
			return invalid
		}
	case "recent", "status", "monitoring":
		if p.Text != "" || p.Kind != "" || (p.Action != "status" && p.Query != "") {
			return invalid
		}
	default:
		return invalid
	}
	if p.Action != "recommend" && (p.Genre != "" || p.MaxMinutes != 0 || p.Unwatched) {
		return invalid
	}
	if p.Action != "recommend" && (p.Relation != "" || p.Source != "") {
		return invalid
	}
	if len(p.Seasons) > 0 && (p.Action != "follow" || p.Kind != "show") {
		return invalid
	}
	return nil
}

// Proof is supplied by the Discord transport, never by the model.
type ToolRequest struct {
	Plan             Plan   `json:"plan"`
	Interaction      []byte `json:"interaction,omitempty"`
	Timestamp        string `json:"timestamp,omitempty"`
	Signature        string `json:"signature,omitempty"`
	ChannelID        string `json:"channelId,omitempty"`
	MessageID        string `json:"messageId,omitempty"`
	ContextMessageID string `json:"contextMessageId,omitempty"`
	CallID           string `json:"callId,omitempty"`
}

type ToolReply struct {
	Content    string            `json:"content"`
	Web        *websearch.Result `json:"web,omitempty"`
	Diagnostic *DiagnosticFacts  `json:"diagnostic,omitempty"`
}
