package upstream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Contract audited at Ouicheur 1.4.5. Never reuse an operator session.
const OuicheurRevision = "bbd143bc03201205a35b0a1009bf8476e110f16a"

var ouicheurToken = regexp.MustCompile(`^[a-f0-9]{64}$`)
var ouicheurID = regexp.MustCompile(`^[a-f0-9-]{36}$`)

type OuicheurSession struct {
	Cookie, ID string
	Expires    time.Time
}
type GiftList struct{ ID, Name string }
type GiftIdea struct {
	Title, Description, Currency string
	Target                       float64
}

func (c *Client) ouicheurRead(ctx context.Context, base, path, cookie, body string) ([]byte, []*http.Cookie, error) {
	method := http.MethodGet
	if body != "" {
		method = http.MethodPost
	}
	r, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, strings.NewReader(body))
	if err != nil {
		return nil, nil, ErrUnavailable
	}
	r.Header.Set("Origin", base)
	r.Header.Set("Accept", "application/json, text/html")
	r.Header.Set("User-Agent", "Le-Concierge-Ouicheur/1")
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		if !ouicheurToken.MatchString(cookie) {
			return nil, nil, ErrDenied
		}
		r.AddCookie(&http.Cookie{Name: "wishlister_session", Value: cookie})
	}
	client := *c.HTTP
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(r)
	if err != nil {
		return nil, nil, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, nil, ErrDenied
	}
	if resp.StatusCode == 404 {
		return nil, nil, ErrNotFound
	}
	if resp.StatusCode != 200 {
		return nil, nil, ErrUnavailable
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(b) > 2*1024*1024 {
		return nil, nil, ErrUnavailable
	}
	return b, resp.Cookies(), nil
}

func (c *Client) OuicheurLogin(ctx context.Context, base, login, password string) (OuicheurSession, error) {
	if strings.TrimSpace(login) != login || len(login) > 40 || len(password) < 1 || len(password) > 256 || strings.ContainsAny(login, "\r\n\x00") {
		return OuicheurSession{}, ErrDenied
	}
	body, _ := json.Marshal(struct {
		Login    string `json:"login,omitempty"`
		Password string `json:"password"`
	}{login, password})
	b, cookies, err := c.ouicheurRead(ctx, base, "/api/login", "", string(body))
	var result struct {
		OK   bool   `json:"ok"`
		Role string `json:"role"`
	}
	if err != nil {
		return OuicheurSession{}, err
	}
	if json.Unmarshal(b, &result) != nil || !result.OK || (result.Role != "member" && result.Role != "owner") {
		return OuicheurSession{}, ErrUnavailable
	}
	var cookie string
	for _, v := range cookies {
		if v.Name == "wishlister_session" && ouicheurToken.MatchString(v.Value) {
			cookie = v.Value
		}
	}
	if cookie == "" {
		return OuicheurSession{}, ErrUnavailable
	}
	return c.OuicheurSession(ctx, base, cookie)
}

func (c *Client) OuicheurSession(ctx context.Context, base, cookie string) (OuicheurSession, error) {
	b, _, err := c.ouicheurRead(ctx, base, "/api/account/sessions", cookie, "")
	if err != nil {
		return OuicheurSession{}, err
	}
	var rows []struct {
		ID      string `json:"id"`
		Current bool   `json:"current"`
		Expires string `json:"expires_at"`
	}
	if json.Unmarshal(b, &rows) != nil || len(rows) > 100 {
		return OuicheurSession{}, ErrUnavailable
	}
	var out OuicheurSession
	for _, row := range rows {
		if row.Current {
			if out.ID != "" || len(row.ID) != 32 {
				return OuicheurSession{}, ErrUnavailable
			}
			expires, err := time.Parse(time.RFC3339, row.Expires)
			if err != nil || !time.Now().Before(expires) {
				return OuicheurSession{}, ErrDenied
			}
			out = OuicheurSession{cookie, row.ID, expires}
		}
	}
	if out.ID == "" {
		return out, ErrDenied
	}
	return out, nil
}

// Preview uses the member's ACL and recipient protections, including for owners.
func (c *Client) OuicheurLists(ctx context.Context, base string, session OuicheurSession) ([]GiftList, error) {
	if err := c.ouicheurCheck(ctx, base, session); err != nil {
		return nil, err
	}
	b, _, err := c.ouicheurRead(ctx, base, "/?preview=1", session.Cookie, "")
	if err != nil {
		return nil, err
	}
	z := html.NewTokenizer(strings.NewReader(string(b)))
	var lists []GiftList
	var id string
	var name strings.Builder
	var label strings.Builder
	inLabel, listSelect := false, false
	seen := map[string]bool{}
	selectDepth := 0
	for {
		t := z.Next()
		if t == html.ErrorToken {
			if z.Err() != io.EOF {
				return nil, ErrUnavailable
			}
			break
		}
		token := z.Token()
		if t == html.StartTagToken && token.Data == "label" {
			inLabel = true
			label.Reset()
		}
		if t == html.TextToken && inLabel && selectDepth == 0 {
			label.WriteString(token.Data)
		}
		if t == html.StartTagToken && token.Data == "select" {
			listSelect = inLabel && (strings.TrimSpace(label.String()) == "Liste" || strings.TrimSpace(label.String()) == "List")
			selectDepth++
		}
		if t == html.StartTagToken && token.Data == "option" && listSelect && selectDepth > 0 {
			id = ""
			name.Reset()
			for _, a := range token.Attr {
				if a.Key == "value" && ouicheurID.MatchString(a.Val) {
					id = a.Val
				}
			}
		}
		if t == html.TextToken && id != "" {
			name.WriteString(token.Data)
		}
		if t == html.EndTagToken && token.Data == "option" && id != "" {
			label := strings.Join(strings.Fields(name.String()), " ")
			if label != "" && len(label) <= 400 && !strings.Contains(label, " · Archiv") && !seen[id] {
				lists = append(lists, GiftList{id, label})
				seen[id] = true
			}
			id = ""
			if len(lists) > 100 {
				return nil, ErrUnavailable
			}
		}
		if t == html.EndTagToken && token.Data == "select" {
			selectDepth--
			listSelect = false
		}
		if t == html.EndTagToken && token.Data == "label" {
			inLabel = false
		}
	}
	if err := c.ouicheurCheck(ctx, base, session); err != nil {
		return nil, err
	}
	return lists, nil
}
func (c *Client) ouicheurCheck(ctx context.Context, base string, expected OuicheurSession) error {
	actual, err := c.OuicheurSession(ctx, base, expected.Cookie)
	if err != nil {
		return err
	}
	if actual.ID != expected.ID {
		return ErrDenied
	}
	return nil
}

// Only descriptive wish fields leave this package. Reservation and payment data
// never enter the model, even when the account can reveal its own surprises.
func (c *Client) OuicheurIdeas(ctx context.Context, base string, session OuicheurSession, listID, query string) ([]GiftIdea, bool, error) {
	if !ouicheurID.MatchString(listID) || len(query) > 300 {
		return nil, false, ErrDenied
	}
	if err := c.ouicheurCheck(ctx, base, session); err != nil {
		return nil, false, err
	}
	q := url.Values{"mode": {"public"}, "list": {listID}, "search": {query}, "locale": {"fr"}, "limit": {"24"}}
	b, _, err := c.ouicheurRead(ctx, base, "/api/wishes?"+q.Encode(), session.Cookie, "")
	if err != nil {
		return nil, false, err
	}
	var page struct {
		Items []struct {
			ListID      string  `json:"list_id"`
			Title       string  `json:"title"`
			Description string  `json:"description"`
			Currency    string  `json:"currency"`
			Target      float64 `json:"target"`
			Hidden      bool    `json:"surprise_hidden"`
		} `json:"items"`
		Next *string `json:"next"`
	}
	if json.Unmarshal(b, &page) != nil || page.Items == nil || len(page.Items) > 24 {
		return nil, false, ErrUnavailable
	}
	var ideas []GiftIdea
	for _, g := range page.Items {
		if g.ListID != listID {
			return nil, false, ErrUnavailable
		}
		// A hidden reservation is no reason to hide the recipient's own wish.
		// We keep the description but never infer whether anyone bought it.
		if g.Title != "" && len(g.Title) <= 1000 && len(g.Description) <= 5000 && g.Target >= 0 {
			ideas = append(ideas, GiftIdea{g.Title, g.Description, g.Currency, g.Target})
		}
	}
	if err := c.ouicheurCheck(ctx, base, session); err != nil {
		return nil, false, err
	}
	return ideas, page.Next != nil, nil
}
