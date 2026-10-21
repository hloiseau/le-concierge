package upstream

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const downloadLibraryPageSize = 20

type DownloadLibraryItem struct {
	RatingKey   string `json:"ratingKey"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	Year        int    `json:"year,omitempty"`
	Index       *int   `json:"index,omitempty"`
	ParentTitle string `json:"parentTitle,omitempty"`
}

type DownloadLibraryPage struct {
	Items      []DownloadLibraryItem `json:"items"`
	NextOffset *int                  `json:"nextOffset"`
	CheckedAt  time.Time             `json:"checkedAt"`
	ExpiresAt  time.Time             `json:"expiresAt"`
}

func validPlexLibraryID(value string) bool {
	n, err := strconv.ParseInt(value, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == value
}

func ValidDownloadLibraryQuery(query, parent string, offset int) bool {
	if offset < 0 || offset > 10000 || (query == "") == (parent == "") {
		return false
	}
	if parent != "" {
		return validPlexLibraryID(parent)
	}
	return utf8.ValidString(query) && utf8.RuneCountInString(query) >= 2 && utf8.RuneCountInString(query) <= 200 && strings.TrimSpace(query) == query && !strings.ContainsFunc(query, unicode.IsControl)
}

func libraryTitle(value string) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value))
	runes := []rune(value)
	return string(runes[:min(len(runes), 240)])
}

// Plex performs the title filter; each call reads only one bounded page with the member's token.
func (c *Client) DownloadLibrary(ctx context.Context, base, serverID string, g Grant, query, parent string, offset int) (DownloadLibraryPage, error) {
	if !ValidDownloadLibraryQuery(query, parent, offset) || g.ServerToken == "" || serverID == "" || (!g.ExpiresAt.IsZero() && !g.Active(time.Now())) {
		return DownloadLibraryPage{}, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	httpClient := *c.HTTP
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	httpClient.Jar = nil
	reader := &Client{HTTP: &httpClient}
	if err := reader.PlexIdentity(ctx, base, serverID); err != nil {
		return DownloadLibraryPage{}, err
	}
	path := "/library/all"
	expectedType, parentTitle := "", ""
	q := url.Values{"skipRefresh": {"1"}, "X-Plex-Container-Start": {strconv.Itoa(offset)}, "X-Plex-Container-Size": {strconv.Itoa(downloadLibraryPageSize + 1)}}
	if query != "" {
		q.Set("title", query)
		q.Set("type", "1,2")
		q.Set("sort", "titleSort")
	} else {
		item, err := reader.PlexMetadata(ctx, base, parent, g)
		if err != nil {
			return DownloadLibraryPage{}, err
		}
		switch item.Kind {
		case "show":
			expectedType = "season"
		case "season":
			expectedType = "episode"
		default:
			return DownloadLibraryPage{}, ErrNotFound
		}
		parentTitle = libraryTitle(item.Title)
		path = "/library/metadata/" + parent + "/children"
	}
	var result struct {
		Container *struct {
			Offset    *int `json:"offset"`
			Size      *int `json:"size"`
			TotalSize *int `json:"totalSize"`
			Metadata  []struct {
				Key         tautulliID `json:"ratingKey"`
				Parent      tautulliID `json:"parentRatingKey"`
				Title       string     `json:"title"`
				Type        string     `json:"type"`
				Year        int        `json:"year"`
				Index       *int       `json:"index"`
				ParentTitle string     `json:"parentTitle"`
				DeletedAt   int64      `json:"deletedAt"`
				LeafCount   *int       `json:"leafCount"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := reader.JSON(ctx, "GET", base+path+"?"+q.Encode(), plexHeaders(g.ClientID, g.ServerToken), "", &result); err != nil {
		return DownloadLibraryPage{}, err
	}
	p := result.Container
	if p == nil || p.Size == nil || *p.Size != len(p.Metadata) || len(p.Metadata) > downloadLibraryPageSize+1 || (p.Offset != nil && *p.Offset != offset) || (offset > 0 && p.Offset == nil) || (p.TotalSize != nil && (*p.TotalSize < 0 || (len(p.Metadata) > 0 && offset+len(p.Metadata) > *p.TotalSize))) {
		return DownloadLibraryPage{}, ErrIncomplete
	}
	now := time.Now().UTC()
	page := DownloadLibraryPage{Items: []DownloadLibraryItem{}, CheckedAt: now, ExpiresAt: now.Add(30 * time.Second)}
	if !g.Persistent && !g.ExpiresAt.IsZero() && g.ExpiresAt.Before(page.ExpiresAt) {
		page.ExpiresAt = g.ExpiresAt.UTC()
	}
	if !now.Before(page.ExpiresAt) {
		return DownloadLibraryPage{}, ErrDenied
	}
	count := min(len(p.Metadata), downloadLibraryPageSize)
	if len(p.Metadata) > count || (p.TotalSize != nil && offset+count < *p.TotalSize) {
		if count == 0 {
			return DownloadLibraryPage{}, ErrIncomplete
		}
		next := offset + count
		if next <= 10000 {
			page.NextOffset = &next
		}
	}
	seen := map[string]bool{}
	for _, m := range p.Metadata[:count] {
		if !validPlexLibraryID(string(m.Key)) || (expectedType == "" && m.Type != "movie" && m.Type != "show") || (expectedType != "" && (m.Type != expectedType || string(m.Parent) != parent)) || m.Year < 0 || m.Year > 9999 || (m.Index != nil && (*m.Index < 0 || *m.Index > 10000)) {
			return DownloadLibraryPage{}, ErrIncomplete
		}
		title := libraryTitle(m.Title)
		if title == "" {
			return DownloadLibraryPage{}, ErrIncomplete
		}
		if seen[string(m.Key)] || m.DeletedAt > 0 || (m.LeafCount != nil && *m.LeafCount == 0 && m.Type != "movie" && m.Type != "episode") {
			continue
		}
		seen[string(m.Key)] = true
		label := libraryTitle(m.ParentTitle)
		if label == "" {
			label = parentTitle
		}
		page.Items = append(page.Items, DownloadLibraryItem{RatingKey: string(m.Key), Title: title, Type: m.Type, Year: m.Year, Index: m.Index, ParentTitle: label})
	}
	return page, nil
}
