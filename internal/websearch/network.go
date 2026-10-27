// Package websearch reads bounded public evidence without service credentials.
package websearch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode"
)

var ErrUnavailable = errors.New("public web unavailable")
var ErrDenied = errors.New("not a public web URL")

var reserved = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || (ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip)) {
		return false
	}
	for _, p := range reserved {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// PublicURL validates syntax; DNS is checked again at the actual connection.
func PublicURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\\\r\n\t") || u.Opaque != "" || u.User != nil || u.Hostname() == "" {
		return false
	}
	if (u.Scheme != "https" || (u.Port() != "" && u.Port() != "443")) && (u.Scheme != "http" || (u.Port() != "" && u.Port() != "80")) {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Zone() == "" && publicIP(ip)
	}
	last := host[strings.LastIndex(host, ".")+1:]
	return strings.Contains(host, ".") && strings.ContainsFunc(last, unicode.IsLetter) && !strings.HasSuffix(host, ".localhost") && !strings.HasSuffix(host, ".local") && !strings.HasSuffix(host, ".internal") && !strings.HasSuffix(host, ".home.arpa")
}

func (c *Client) allowed(raw string) bool {
	if !PublicURL(raw) || len(raw) > 300 {
		return false
	}
	u, _ := url.Parse(raw)
	return !c.blockedHost(u.Hostname())
}

func (c *Client) blockedHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return c.blocked[host] || (c.domain != "" && (host == c.domain || strings.HasSuffix(host, "."+c.domain)))
}

func (c *Client) publicClient() *http.Client {
	transport := &http.Transport{
		// No environment proxy: it could resolve a different, private address.
		Proxy: nil, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 4 * time.Second,
		MaxResponseHeaderBytes: 32 * 1024, MaxIdleConns: 8, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second,
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || c.blockedHost(host) {
			return nil, ErrDenied
		}
		addresses, err := c.lookup(ctx, "ip", host)
		if err != nil || len(addresses) == 0 {
			return nil, ErrUnavailable
		}
		for _, ip := range addresses {
			if !publicIP(ip) {
				return nil, ErrDenied
			}
		}
		// A public reverse proxy still leads back to the NAS. Deny its IP too.
		if c.origin != "" {
			protected, err := c.lookup(ctx, "ip", c.origin)
			if err != nil {
				return nil, ErrUnavailable
			}
			for _, ip := range addresses {
				for _, own := range protected {
					if ip.Unmap() == own.Unmap() {
						return nil, ErrDenied
					}
				}
			}
		}
		// Pin the validated IP, rather than doing a second DNS lookup.
		for _, ip := range addresses {
			conn, err := c.dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, ErrUnavailable
	}
	return &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || !c.allowed(req.URL.String()) {
			return ErrDenied
		}
		return nil
	}}
}
