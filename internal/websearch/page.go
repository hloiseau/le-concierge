package websearch

import (
	"bytes"
	"context"
	"io"
	"mime"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

func plain(raw string, n int) string {
	title, text, _ := extract(strings.NewReader(raw))
	return clip(title+" "+text, n)
}

// Tokenize bounded HTML; scripts, forms and navigation never become evidence.
func extract(r io.Reader) (title, text, published string) {
	z := html.NewTokenizer(r)
	var body, main strings.Builder
	skip, mainTag, inTitle := "", "", false
	for {
		switch z.Next() {
		case html.ErrorToken:
			if strings.TrimSpace(main.String()) != "" {
				body = main
			}
			return clip(title, 160), clip(body.String(), 1800), clip(published, 80)
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			if skip != "" {
				continue
			}
			switch t.Data {
			case "script", "style", "noscript", "svg", "nav", "header", "aside", "footer", "form", "template":
				skip = t.Data
			case "title":
				inTitle = title == ""
			case "main", "article":
				if mainTag == "" {
					mainTag = t.Data
				}
			case "meta":
				name, content := "", ""
				for _, a := range t.Attr {
					if a.Key == "property" || a.Key == "name" {
						name = a.Val
					}
					if a.Key == "content" {
						content = a.Val
					}
				}
				if name == "article:published_time" || name == "date" {
					published = content
				}
			}
		case html.EndTagToken:
			t := z.Token()
			if t.Data == skip {
				skip = ""
			}
			if t.Data == "title" {
				inTitle = false
			}
			if t.Data == mainTag {
				mainTag = ""
			}
		case html.TextToken:
			if skip == "" {
				value := string(z.Text())
				if inTitle {
					title += value
				} else {
					body.WriteString(value)
					body.WriteByte(' ')
					if mainTag != "" {
						main.WriteString(value)
						main.WriteByte(' ')
					}
				}
			}
		}
	}
}

func (c *Client) read(ctx context.Context, target string) (Source, error) {
	if !c.allowed(target) {
		return Source{}, ErrDenied
	}
	if youtubeURL(target) != "" {
		return c.video(ctx, target)
	}
	b, resp, err := get(ctx, c.public, target, "text/html, text/plain;q=0.9", 512*1024)
	if err != nil {
		return Source{}, err
	}
	s := Source{URL: resp.Request.URL.String(), Kind: "page"}
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch media {
	case "text/plain":
		s.Title, s.Text = s.URL, clip(string(b), 1800)
	case "text/html", "application/xhtml+xml":
		reader, err := charset.NewReader(bytes.NewReader(b), resp.Header.Get("Content-Type"))
		if err != nil {
			return Source{}, ErrUnavailable
		}
		s.Title, s.Text, s.PublishedAt = extract(reader)
	default:
		return Source{}, ErrUnavailable
	}
	if s.Title == "" {
		s.Title = s.URL
	}
	s.Title = clip(s.Title, 160)
	if s.Text == "" {
		return Source{}, ErrUnavailable
	}
	return s, nil
}
