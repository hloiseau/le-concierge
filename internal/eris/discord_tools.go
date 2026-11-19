package eris

import (
	"errors"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"
	"unicode/utf8"
)

func (p Plan) validateDiscordCreation() error {
	bad := errors.New("invalid Discord creation")
	if p.Kind != "" || p.Genre != "" || p.MaxMinutes != 0 || p.Unwatched || len(p.Seasons) > 0 || p.Relation != "" || p.Source != "" || p.URL != "" || p.Year != 0 || p.RequestID != 0 || p.SubscriptionID != 0 || p.Channel != "" || p.Setting != "" || p.Value != "" {
		return bad
	}
	valid := func(s string, max int) bool {
		return strings.TrimSpace(s) == s && s != "" && utf8.RuneCountInString(s) <= max && !strings.ContainsFunc(s, unicode.IsControl) && !strings.Contains(s, "<@") && !strings.Contains(s, "@everyone") && !strings.Contains(s, "@here")
	}
	if p.Action == "poll" {
		if !valid(p.Text, 300) || p.Query != "" || len(p.Options) < 2 || len(p.Options) > 10 || p.DurationHours < 0 || p.DurationHours > 768 {
			return bad
		}
		seen := map[string]bool{}
		for _, option := range p.Options {
			key := strings.ToLower(option)
			if !valid(option, 55) || seen[key] {
				return bad
			}
			seen[key] = true
		}
		return nil
	}
	start, e1 := time.Parse(time.RFC3339, p.StartAt)
	end, e2 := time.Parse(time.RFC3339, p.EndAt)
	if !valid(p.Text, 100) || utf8.RuneCountInString(p.Query) > 200 || strings.ContainsFunc(p.Query, unicode.IsControl) || e1 != nil || e2 != nil || !end.After(start) || end.Sub(start) > 24*time.Hour {
		return bad
	}
	return nil
}

func PublicTextMentioned(text, question, reference string) bool {
	normalize := func(s string) string { return " " + strings.Join(words(s), " ") + " " }
	return strings.Contains(normalize(question), normalize(text)) || reference != "" && strings.Contains(normalize(reference), normalize(text))
}
