package eris

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Only these member-safe observations reach the narrator, never Arr evidence.
type DiagnosticFacts struct {
	Title           string   `json:"title"`
	Stage           string   `json:"stage"`
	ProgressPercent *float64 `json:"progressPercent,omitempty"`
	MissingEpisodes *int     `json:"missingEpisodes,omitempty"`
	PlexIndex       string   `json:"plexIndex,omitempty"`
	Partial         bool     `json:"partial"`
}

func (d DiagnosticFacts) Valid() bool {
	if strings.TrimSpace(d.Title) == "" || utf8.RuneCountInString(d.Title) > 300 || strings.ContainsFunc(d.Title, unicode.IsControl) || (d.PlexIndex != "indexed" && d.PlexIndex != "") {
		return false
	}
	if d.ProgressPercent != nil && (math.IsNaN(*d.ProgressPercent) || math.IsInf(*d.ProgressPercent, 0) || *d.ProgressPercent < 0 || *d.ProgressPercent > 100) {
		return false
	}
	if d.MissingEpisodes != nil && *d.MissingEpisodes < 0 {
		return false
	}
	switch d.Stage {
	case "approval", "declined", "downloading", "queued", "paused", "sources", "manual_import", "permissions", "space", "rename_import", "importing", "download_failed", "client_unavailable", "plex_indexing", "imported", "indexers", "not_found", "unknown":
		return true
	}
	return false
}
