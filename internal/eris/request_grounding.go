package eris

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var seasonNumbers = regexp.MustCompile(`(?i)\bsaisons?\s+([0-9]{1,4}(?:(?:\s*,\s*|\s+et\s+)[0-9]{1,4})*)\b`)
var seasonNumber = regexp.MustCompile(`[0-9]+`)

func groundedSeasons(title string, seasons []int, question string, history []Message) []int {
	// A spelling correction can reuse the latest season named by this member.
	// A fresh instruction, catalogue result or quoted assistant turn cannot.
	bareTitle := func(text string) bool {
		return len(titleWords(text)) == len(titleWords(title)) && prepareTitleMentioned(title, []Message{{Role: "user", Content: text}})
	}
	if bareTitle(question) {
		for i := len(history) - 1; i >= 0; i-- {
			turn := history[i]
			if turn.Role != "user" || bareTitle(turn.Content) {
				continue
			}
			if prepareTitleMentioned(title, []Message{turn}) {
				question = turn.Content
			}
			break
		}
	}
	numbers := map[int]bool{}
	for _, group := range seasonNumbers.FindAllStringSubmatch(question, -1) {
		for _, raw := range seasonNumber.FindAllString(group[1], -1) {
			n, _ := strconv.Atoi(raw)
			numbers[n] = true
		}
	}
	for _, season := range seasons {
		if !numbers[season] {
			return nil
		}
	}
	return seasons
}

func foldTitleAccents(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFD.String(s))
}

func titleWords(s string) []string {
	return strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, foldTitleAccents(s)))
}

func concreteRequestTitle(words []string) bool {
	switch strings.Join(words, " ") {
	case "", "film", "le film", "un film", "ce film", "serie", "la serie", "une serie", "cette serie", "the movie", "a movie", "that movie", "the show", "that show":
		return false
	}
	return true
}

// Close spelling only unlocks catalogue preparation, never a choice or consent.
func prepareTitleMentioned(title string, messages []Message) bool {
	if requestTitleMentioned(title, messages) {
		return true
	}
	words := titleWords(title)
	if !concreteRequestTitle(words) {
		return false
	}
	letters := 0
	for _, word := range words {
		letters += utf8.RuneCountInString(word)
	}
	if len(words) < 2 || len(words) > 12 || letters < 10 {
		return false
	}
	budget := min(3, letters/4)
	for _, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		text := titleWords(message.Content)
		for start := 0; start+len(words) <= len(text); start++ {
			cost := 0
			for i, word := range words {
				actual := text[start+i]
				if actual == word {
					continue
				}
				if min(utf8.RuneCountInString(word), utf8.RuneCountInString(actual)) < 5 {
					cost = budget + 1
					break
				}
				distance := titleEditDistance(word, actual)
				if distance > 2 {
					cost = budget + 1
					break
				}
				cost += distance
				if cost > budget {
					break
				}
			}
			if cost <= budget {
				return true
			}
		}
	}
	return false
}

// Distances beyond two per word cannot be a spelling match here.
func titleEditDistance(a, b string) int {
	x, y := []rune(a), []rune(b)
	if len(x)-len(y) > 2 || len(y)-len(x) > 2 {
		return 3
	}
	previous := make([]int, len(y)+1)
	for i := range previous {
		previous[i] = i
	}
	for i, r := range x {
		current := make([]int, len(y)+1)
		current[0] = i + 1
		for j, s := range y {
			cost := 0
			if r != s {
				cost = 1
			}
			current[j+1] = min(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return min(3, previous[len(y)])
}
