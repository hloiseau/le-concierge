package upstream

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var plexPosterPath = regexp.MustCompile(`^/library/metadata/[1-9][0-9]*/thumb(/[0-9]+)?$`)

func (c *Client) PlexPoster(ctx context.Context, base, serverID, id string, g Grant) ([]byte, string, error) {
	if !validPlexLibraryID(id) || g.ServerToken == "" || (!g.ExpiresAt.IsZero() && !time.Now().Before(g.ExpiresAt)) {
		return nil, "", ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	client := *c.HTTP
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	reader := &Client{HTTP: &client}
	if err := reader.PlexIdentity(ctx, base, serverID); err != nil {
		return nil, "", err
	}
	item, err := reader.PlexMetadata(ctx, base, id, g)
	if err != nil {
		return nil, "", err
	}
	if len(item.Thumb) > 256 || !plexPosterPath.MatchString(item.Thumb) || !strings.HasPrefix(item.Thumb, "/library/metadata/"+id+"/thumb") {
		return nil, "", ErrNotFound
	}
	// Fetch the authorized item's own poster directly so its token stays in a header.
	req, err := http.NewRequestWithContext(ctx, "GET", base+item.Thumb, nil)
	if err != nil {
		return nil, "", ErrUnavailable
	}
	for name, value := range plexHeaders(g.ClientID, g.ServerToken) {
		req.Header.Set(name, value)
	}
	req.Header.Set("Accept", "image/jpeg, image/png, image/webp")
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(body) > 2*1024*1024 {
		return nil, "", ErrUnavailable
	}
	mime := http.DetectContentType(body)
	if mime != "image/jpeg" && mime != "image/png" && mime != "image/webp" {
		return nil, "", ErrUnavailable
	}
	return body, mime, nil
}
