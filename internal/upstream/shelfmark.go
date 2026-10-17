package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ShelfmarkSession is private, in-memory authentication; never serialize it.
type ShelfmarkSession struct {
	UserID   int64  `json:"-"`
	Username string `json:"-"`
	Cookies  string `json:"-"`
}
type BookRequest struct {
	ID                string     `json:"id"`
	Title             string     `json:"title"`
	Authors           []string   `json:"authors"`
	Status            string     `json:"status"`
	DeliveryState     string     `json:"deliveryState"`
	CreatedAt         time.Time  `json:"createdAt"`
	ReviewedAt        *time.Time `json:"reviewedAt,omitempty"`
	DeliveryUpdatedAt *time.Time `json:"deliveryUpdatedAt,omitempty"`
	CatalogStatus     string     `json:"catalogStatus"`
}
type BookRequestsPage struct {
	Items     []BookRequest `json:"items"`
	Page      int           `json:"page"`
	PageSize  int           `json:"pageSize"`
	HasMore   bool          `json:"hasMore"`
	CheckedAt time.Time     `json:"checkedAt"`
}
type shelfmarkAuth struct {
	Authenticated bool   `json:"authenticated"`
	Required      bool   `json:"auth_required"`
	Mode          string `json:"auth_mode"`
	HideLocal     bool   `json:"hide_local_auth"`
	Username      string `json:"username"`
}

func (c *Client) shelfmarkClient() *http.Client {
	client := *c.HTTP
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}
func shelfmarkJSON(ctx context.Context, client *http.Client, method, target, cookies string, body any, out any) error {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return ErrUnavailable
		}
		payload = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookies != "" {
		req.Header.Set("Cookie", cookies)
	}
	resp, err := client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return ErrDenied
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(data) > 2*1024*1024 || json.Unmarshal(data, out) != nil {
		return ErrIncomplete
	}
	return nil
}
func (c *Client) LinkShelfmark(ctx context.Context, base, username, password string) (ShelfmarkSession, error) {
	if !ValidBookCredentials(BookCredentials{Username: username, Password: password}) {
		return ShelfmarkSession{}, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	client := c.shelfmarkClient()
	client.Jar, _ = cookiejar.New(nil)
	var auth shelfmarkAuth
	if err := shelfmarkJSON(ctx, client, "GET", base+"/api/auth/check", "", nil, &auth); err != nil {
		return ShelfmarkSession{}, err
	}
	if !auth.Required || auth.Mode != "builtin" || auth.HideLocal {
		return ShelfmarkSession{}, ErrDenied
	}
	var login struct {
		Success bool `json:"success"`
	}
	if err := shelfmarkJSON(ctx, client, "POST", base+"/api/auth/login", "", map[string]any{"username": username, "password": password, "remember_me": false}, &login); err != nil {
		return ShelfmarkSession{}, err
	}
	if !login.Success {
		return ShelfmarkSession{}, ErrDenied
	}
	target, err := url.Parse(base + "/api/requests")
	if err != nil {
		return ShelfmarkSession{}, ErrIncomplete
	}
	cookieRequest := &http.Request{Header: make(http.Header)}
	for _, cookie := range client.Jar.Cookies(target) {
		cookieRequest.AddCookie(cookie)
	}
	session := ShelfmarkSession{Username: strings.TrimSpace(username), Cookies: cookieRequest.Header.Get("Cookie")}
	if session.Cookies == "" || len(session.Cookies) > 8192 {
		return ShelfmarkSession{}, ErrIncomplete
	}
	id, err := c.shelfmarkIdentity(ctx, base, session)
	if err != nil {
		return ShelfmarkSession{}, err
	}
	session.UserID = id
	return session, nil
}
func (c *Client) shelfmarkIdentity(ctx context.Context, base string, session ShelfmarkSession) (int64, error) {
	if session.Cookies == "" || session.Username == "" {
		return 0, ErrDenied
	}
	client := c.shelfmarkClient()
	var auth shelfmarkAuth
	if err := shelfmarkJSON(ctx, client, "GET", base+"/api/auth/check", session.Cookies, nil, &auth); err != nil {
		return 0, err
	}
	if !auth.Authenticated || !auth.Required || auth.Mode != "builtin" || auth.Username != session.Username {
		return 0, ErrDenied
	}
	var identity struct {
		User struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
			Active   bool   `json:"is_active"`
		} `json:"user"`
	}
	if err := shelfmarkJSON(ctx, client, "GET", base+"/api/users/me/edit-context", session.Cookies, nil, &identity); err != nil {
		return 0, err
	}
	if identity.User.ID <= 0 || identity.User.Username != session.Username || !identity.User.Active || (session.UserID != 0 && session.UserID != identity.User.ID) {
		return 0, ErrDenied
	}
	return identity.User.ID, nil
}
func (c *Client) ShelfmarkRequests(ctx context.Context, base string, session ShelfmarkSession, page int) (BookRequestsPage, error) {
	out := BookRequestsPage{Items: []BookRequest{}, Page: page, PageSize: 20, CheckedAt: time.Now().UTC()}
	if page < 1 || page > 1001 || session.UserID <= 0 {
		return out, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	if _, err := c.shelfmarkIdentity(ctx, base, session); err != nil {
		return out, err
	}
	var rows []struct {
		ID        int64  `json:"id"`
		UserID    int64  `json:"user_id"`
		Status    string `json:"status"`
		Delivery  string `json:"delivery_state"`
		Created   string `json:"created_at"`
		Reviewed  string `json:"reviewed_at"`
		Delivered string `json:"delivery_updated_at"`
		Book      struct {
			Title  string `json:"title"`
			Author string `json:"author"`
		} `json:"book_data"`
	}
	err := shelfmarkJSON(ctx, c.shelfmarkClient(), "GET", fmt.Sprintf("%s/api/requests?limit=21&offset=%d", base, (page-1)*20), session.Cookies, nil, &rows)
	if err != nil {
		return out, err
	}
	if rows == nil || len(rows) > 21 {
		return out, ErrIncomplete
	}
	seen := map[int64]bool{}
	for _, row := range rows {
		if row.UserID != session.UserID {
			return out, ErrDenied
		}
		if row.ID <= 0 || seen[row.ID] || strings.TrimSpace(row.Book.Title) == "" || utf8.RuneCountInString(row.Book.Title) > 500 || utf8.RuneCountInString(row.Book.Author) > 500 {
			return out, ErrIncomplete
		}
		seen[row.ID] = true
		switch row.Status {
		case "pending", "fulfilled", "rejected", "cancelled":
		default:
			return out, ErrIncomplete
		}
		switch row.Delivery {
		case "none", "queued", "resolving", "locating", "downloading", "complete", "error", "cancelled":
		default:
			return out, ErrIncomplete
		}
		created, err := shelfmarkTime(row.Created)
		if err != nil {
			return out, err
		}
		item := BookRequest{ID: strconv.FormatInt(row.ID, 10), Title: strings.TrimSpace(row.Book.Title), Authors: []string{}, Status: row.Status, DeliveryState: row.Delivery, CreatedAt: created, CatalogStatus: "unverified"}
		if author := strings.TrimSpace(row.Book.Author); author != "" {
			item.Authors = append(item.Authors, author)
		}
		if row.Reviewed != "" {
			at, err := shelfmarkTime(row.Reviewed)
			if err != nil {
				return out, err
			}
			item.ReviewedAt = &at
		}
		if row.Delivered != "" {
			at, err := shelfmarkTime(row.Delivered)
			if err != nil {
				return out, err
			}
			item.DeliveryUpdatedAt = &at
		}
		out.Items = append(out.Items, item)
	}
	if len(out.Items) > 20 {
		out.HasMore = true
		out.Items = out.Items[:20]
	}
	return out, nil
}
func shelfmarkTime(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if at, err := time.Parse(layout, raw); err == nil {
			return at.UTC(), nil
		}
	}
	return time.Time{}, ErrIncomplete
}
