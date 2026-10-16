package upstream

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const BookPageSize = 20

type BookCredentials struct{ Username, Password string }
type Book struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Authors   []string `json:"authors"`
	Series    string   `json:"series,omitempty"`
	Volume    string   `json:"volume,omitempty"`
	ISBN      string   `json:"isbn,omitempty"`
	Language  string   `json:"language,omitempty"`
	Publisher string   `json:"publisher,omitempty"`
	Formats   []string `json:"formats"`
	URL       string   `json:"url,omitempty"`
	CoverURL  string   `json:"coverUrl,omitempty"`
}
type BookPage struct {
	Items    []Book `json:"items"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
	HasMore  bool   `json:"hasMore"`
}
type opdsLink struct {
	Rel   string `xml:"rel,attr"`
	Href  string `xml:"href,attr"`
	Type  string `xml:"type,attr"`
	Title string `xml:"title,attr"`
}
type opdsEntry struct {
	ID      string `xml:"id"`
	Title   string `xml:"title"`
	Authors []struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Identifiers []string `xml:"identifier"`
	Language    string   `xml:"http://purl.org/dc/terms/ language"`
	Publisher   string   `xml:"http://purl.org/dc/terms/ publisher"`
	Metadata    []struct {
		Property string `xml:"property,attr"`
		Value    string `xml:",chardata"`
	} `xml:"meta"`
	Links []opdsLink `xml:"link"`
}

func ValidBookQuery(query string) bool {
	return utf8.ValidString(query) && utf8.RuneCountInString(query) <= 200 && !strings.ContainsFunc(query, unicode.IsControl)
}
func ValidBookID(id string) bool {
	n, err := strconv.ParseInt(id, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == id
}
func ValidBookCredentials(credentials BookCredentials) bool {
	return credentials.Username != "" && len(credentials.Username) <= 200 && !strings.ContainsAny(credentials.Username, ":\r\n") && credentials.Password != "" && len(credentials.Password) <= 1024 && !strings.ContainsAny(credentials.Password, "\r\n")
}

// GrimmoryBooks uses the user's dedicated OPDS credentials; no administrator fallback.
func (c *Client) GrimmoryBooks(ctx context.Context, base, publicBase string, credentials BookCredentials, query string, page int) (BookPage, error) {
	return c.grimmoryBooks(ctx, base, publicBase, credentials, query, "", page)
}

func (c *Client) grimmoryBooks(ctx context.Context, base, publicBase string, credentials BookCredentials, query, series string, page int) (BookPage, error) {
	return c.grimmoryBookPage(ctx, base, publicBase, credentials, "/api/v1/opds/catalog", query, series, page)
}

func (c *Client) GrimmoryRecentBooks(ctx context.Context, base, publicBase string, credentials BookCredentials, page int) (BookPage, error) {
	return c.grimmoryBookPage(ctx, base, publicBase, credentials, "/api/v1/opds/recent", "", "", page)
}

func (c *Client) grimmoryBookPage(ctx context.Context, base, publicBase string, credentials BookCredentials, path, query, series string, page int) (BookPage, error) {
	result := BookPage{Items: []Book{}, Page: page, PageSize: BookPageSize}
	if !ValidBookCredentials(credentials) || !ValidBookQuery(query) || !ValidBookQuery(series) || page < 1 || page > 1001 {
		return result, ErrDenied
	}
	q := url.Values{"q": {strings.TrimSpace(query)}, "page": {strconv.Itoa(page)}, "size": {strconv.Itoa(BookPageSize)}}
	if series != "" {
		q.Del("q")
		q.Set("series", series)
	}
	body, _, err := c.bookRead(ctx, base, path+"?"+q.Encode(), credentials, 2*1024*1024)
	if err != nil {
		return result, err
	}
	var feed struct {
		XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
		Total   *int        `xml:"totalResults"`
		Start   *int        `xml:"startIndex"`
		Size    *int        `xml:"itemsPerPage"`
		Entries []opdsEntry `xml:"entry"`
	}
	if xml.Unmarshal(body, &feed) != nil || feed.Total == nil || *feed.Total < 0 || feed.Start == nil || *feed.Start != (page-1)*BookPageSize+1 || feed.Size == nil || *feed.Size != BookPageSize || len(feed.Entries) > BookPageSize {
		return result, ErrUnavailable
	}
	result.HasMore = *feed.Total > page*BookPageSize
	seen := map[string]bool{}
	for _, entry := range feed.Entries {
		id := strings.TrimPrefix(entry.ID, "urn:booklore:book:")
		if !strings.HasPrefix(entry.ID, "urn:booklore:book:") || !ValidBookID(id) || strings.TrimSpace(entry.Title) == "" {
			return result, ErrUnavailable
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		book := Book{ID: id, Title: bookText(entry.Title, 300), Authors: []string{}, Formats: []string{}}
		book.Language = bookText(entry.Language, 80)
		book.Publisher = bookText(entry.Publisher, 200)
		for _, author := range entry.Authors {
			if name := bookText(author.Name, 150); name != "" && len(book.Authors) < 20 {
				book.Authors = append(book.Authors, name)
			}
		}
		for _, identifier := range entry.Identifiers {
			isbn := strings.TrimPrefix(identifier, "urn:isbn:")
			if strings.HasPrefix(identifier, "urn:isbn:") && len(isbn) >= 10 && len(isbn) <= 17 && strings.IndexFunc(isbn, func(r rune) bool { return !(r >= '0' && r <= '9' || r == '-' || r == 'X' || r == 'x') }) < 0 {
				book.ISBN = isbn
				break
			}
		}
		for _, meta := range entry.Metadata {
			switch meta.Property {
			case "belongs-to-collection":
				book.Series = bookText(meta.Value, 200)
			case "group-position":
				// Keep fractional and non-numeric labels verbatim; never infer gaps.
				book.Volume = bookText(meta.Value, 40)
			}
		}
		if series != "" && book.Series != series {
			return BookPage{}, ErrUnavailable
		}
		for _, link := range entry.Links {
			u, e := url.Parse(link.Href)
			if e != nil || u.IsAbs() || u.Host != "" || u.User != nil || u.Fragment != "" {
				continue
			}
			if (link.Rel == "http://opds-spec.org/image" || link.Rel == "http://opds-spec.org/image/thumbnail") && u.Path == "/api/v1/opds/"+id+"/cover" {
				book.CoverURL = "/api/v1/books/" + id + "/cover"
			}
			if link.Rel != "http://opds-spec.org/acquisition" || u.Path != "/api/v1/opds/"+id+"/download" || !ValidBookID(u.Query().Get("fileId")) {
				continue
			}
			format := bookFormat(link.Type)
			if format != "" {
				duplicate := false
				for _, existing := range book.Formats {
					duplicate = duplicate || existing == format
				}
				if !duplicate {
					book.Formats = append(book.Formats, format)
				}
			}
		}
		if u, err := url.Parse(publicBase); err == nil && u.Hostname() != "" && (u.Scheme == "https" || u.Scheme == "http") && u.User == nil && u.RawQuery == "" && u.Fragment == "" {
			u.Path = strings.TrimRight(u.Path, "/") + "/book/" + id
			book.URL = u.String()
		}
		result.Items = append(result.Items, book)
	}
	return result, nil
}

func bookText(value string, max int) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value))
	runes := []rune(value)
	if len(runes) > max {
		return string(runes[:max])
	}
	return value
}
func bookFormat(mime string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0])) {
	case "application/epub+zip":
		return "EPUB"
	case "application/pdf":
		return "PDF"
	case "application/x-mobipocket-ebook":
		return "MOBI"
	case "application/vnd.amazon.ebook":
		return "AZW3"
	case "application/x-fictionbook+xml":
		return "FB2"
	case "application/zip":
		return "ZIP"
	case "application/x-cbz", "application/vnd.comicbook+zip":
		return "CBZ"
	case "application/x-cbr", "application/vnd.comicbook-rar":
		return "CBR"
	case "application/x-cb7", "application/x-7z-compressed":
		return "CB7"
	case "audio/mpeg":
		return "MP3"
	case "audio/mp4", "audio/x-m4b":
		return "M4B"
	case "audio/ogg":
		return "OGG"
	case "audio/opus":
		return "OPUS"
	case "audio/flac", "audio/x-flac":
		return "FLAC"
	}
	return ""
}
func (c *Client) GrimmoryCover(ctx context.Context, base string, credentials BookCredentials, id string) ([]byte, string, error) {
	if !ValidBookID(id) || !ValidBookCredentials(credentials) {
		return nil, "", ErrDenied
	}
	body, _, err := c.bookRead(ctx, base, "/api/v1/opds/"+id+"/cover", credentials, 2*1024*1024)
	if err != nil {
		return nil, "", err
	}
	mime := http.DetectContentType(body)
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return body, mime, nil
	}
	return nil, "", ErrUnavailable
}
func (c *Client) bookRead(ctx context.Context, base, path string, credentials BookCredentials, limit int64) ([]byte, string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, "", ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return nil, "", ErrUnavailable
	}
	req.SetBasicAuth(credentials.Username, credentials.Password)
	req.Header.Set("Accept", "application/atom+xml, image/*")
	// Even an injected client must not forward these credentials through a redirect.
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, "", ErrDenied
	}
	if resp.StatusCode == 404 {
		return nil, "", ErrNotFound
	}
	if resp.StatusCode != 200 {
		return nil, "", ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, "", ErrUnavailable
	}
	return body, resp.Header.Get("Content-Type"), nil
}

func ShelfmarkSearchLink(base, query string) (string, error) {
	if !ValidBookQuery(query) || strings.TrimSpace(query) == "" {
		return "", ErrDenied
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", ErrUnavailable
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String() + "#" + url.Values{"q": {strings.TrimSpace(query)}, "search_by": {"general"}}.Encode(), nil
}
