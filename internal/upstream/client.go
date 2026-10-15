package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrDenied = errors.New("upstream access denied")
var ErrUnavailable = errors.New("upstream unavailable")
var ErrNotFound = errors.New("upstream item not found")

type Client struct{ HTTP *http.Client }

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) JSON(ctx context.Context, method, target string, headers map[string]string, body string, out any) error {
	req, e := http.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
	if e != nil {
		return ErrUnavailable
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return ErrDenied
	}
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrUnavailable
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if e != nil || len(b) > 2*1024*1024 {
		return ErrUnavailable
	}
	if out == nil {
		return nil
	}
	if json.Unmarshal(b, out) != nil {
		return ErrUnavailable
	}
	return nil
}
