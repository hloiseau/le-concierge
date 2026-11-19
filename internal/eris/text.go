package eris

import (
	"golang.org/x/text/unicode/norm"
	"strings"
	"unicode"
)

func foldTitleAccents(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFD.String(s))
}
