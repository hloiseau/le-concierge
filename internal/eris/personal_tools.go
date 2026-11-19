package eris

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (p Plan) validatePersonalTool() error {
	invalid := errors.New("invalid personal tool plan")
	if p.Text != "" || p.Relation != "" || p.Source != "" || p.URL != "" || p.RequestID != 0 || p.Genre != "" || p.MaxMinutes != 0 || p.Unwatched {
		return invalid
	}
	if p.Action == "follow" {
		if strings.TrimSpace(p.Query) != p.Query || strings.ContainsFunc(p.Query, unicode.IsControl) || (p.MediaID == 0 && utf8.RuneCountInString(p.Query) < 2) || p.MediaID < 0 || p.MediaID > 2147483647 || (p.MediaID > 0 && (p.Query != "" || p.Kind == "")) || utf8.RuneCountInString(p.Query) > 200 || (p.Kind != "" && p.Kind != "movie" && p.Kind != "show") || (p.Year != 0 && (p.Year < 1801 || p.Year > 2999)) || (p.Channel != "" && p.Channel != "hub" && p.Channel != "discord") || len(p.Seasons) > 1 || (len(p.Seasons) > 0 && (p.Kind != "show" || p.Seasons[0] < 1 || p.Seasons[0] > 1000)) {
			return invalid
		}
		return nil
	}
	if p.Kind != "" || len(p.Seasons) > 0 || p.Query != "" {
		return invalid
	}
	if p.Action == "unfollow" && (p.SubscriptionID < 0 || p.SubscriptionID > 2147483647) {
		return invalid
	}
	if p.Action == "preferences" && (p.Setting != "" || p.Value != "") && !validPreference(p.Setting, p.Value) {
		return invalid
	}
	return nil
}

func validPreference(setting, value string) bool {
	switch setting {
	case "enabled", "discordDmEnabled", "allUnwatched", "availableUnwatched":
		return value == "true" || value == "false"
	case "dailyCap":
		n, err := strconv.Atoi(value)
		return err == nil && n >= 1 && n <= 100 && strconv.Itoa(n) == value
	case "quietHours", "daysBefore":
		separator, max, limit := ",", 30, 5
		if setting == "quietHours" {
			separator, max, limit = "-", 23, 2
		}
		parts := strings.Split(value, separator)
		if len(parts) < 1 || len(parts) > limit || (setting == "quietHours" && len(parts) != 2) {
			return false
		}
		seen := map[int]bool{}
		for _, part := range parts {
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 || n > max || strconv.Itoa(n) != part || (setting == "daysBefore" && (n == 0 || seen[n])) {
				return false
			}
			seen[n] = true
		}
		return true
	}
	return false
}
