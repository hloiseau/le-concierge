package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(r *http.Request, contentType, body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestPublicNetworkBoundary(t *testing.T) {
	for _, raw := range []string{"http://127.1", "http://127.0.0.1", "http://[::1]", "http://[::ffff:10.0.0.1]", "http://10.2.3.4", "http://169.254.169.254/latest", "http://100.64.0.1", "http://192.0.2.1", "http://[64:ff9b::a00:1]", "https://a.local", "https://foo.home.arpa", "http://api", "https://user:secret@example.org", "file:///etc/passwd", "https://example.org:8443", "https://example.org\\@127.0.0.1", "https://example.org\n"} {
		if PublicURL(raw) {
			t.Errorf("accepted %q", raw)
		}
	}
	c := New("http://search:8080", "https://hub.myserver.fr", "https://plex.other.fr")
	for _, raw := range []string{"https://hub.myserver.fr", "https://truenas.myserver.fr", "https://plex.other.fr", "http://search:8080"} {
		if c.allowed(raw) {
			t.Errorf("accepted configured service %s", raw)
		}
	}
	for _, ips := range [][]string{{"127.0.0.1"}, {"8.8.8.8", "192.168.1.1"}, {"1.1.1.1"}, {"8.8.8.8"}} {
		t.Run(strings.Join(ips, ","), func(t *testing.T) {
			lookups, dials := 0, 0
			c.lookup = func(_ context.Context, _, host string) ([]netip.Addr, error) {
				lookups++
				if host == c.origin {
					return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
				}
				var out []netip.Addr
				for _, s := range ips {
					out = append(out, netip.MustParseAddr(s))
				}
				return out, nil
			}
			c.dial = func(_ context.Context, _, address string) (net.Conn, error) {
				dials++
				if address != "8.8.8.8:443" {
					t.Errorf("DNS was not pinned to verified IP: %s", address)
				}
				return nil, errors.New("synthetic dial")
			}
			_, _ = c.public.Get("https://public.example/path")
			want := 0
			if len(ips) == 1 && ips[0] == "8.8.8.8" {
				want = 1
			}
			if dials != want || lookups > 2 {
				t.Fatalf("unsafe resolution: %d dials, %d lookups", dials, lookups)
			}
		})
	}
	req, _ := http.NewRequest("GET", "http://192.168.1.1/", nil)
	if c.public.CheckRedirect(req, []*http.Request{{}}) == nil {
		t.Fatal("private redirect accepted")
	}
	if c.public.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("environment proxy can bypass IP checks")
	}
}

func TestSearchReadsPagesAndLabelsFallbackSnippets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "date vérifiée" || r.URL.Query().Get("format") != "json" {
			t.Error("wrong search contract")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{
			{"title": "private", "url": "http://127.0.0.1/key", "content": "secret"},
			{"title": "Ancien titre", "url": "https://one.example/article", "content": "Ancien extrait"},
			{"title": "Autre annonce", "url": "https://two.example/article", "content": "<b>Date contradictoire : demain</b>", "publishedDate": "2026-10-26"},
		}})
	}))
	defer server.Close()
	c := New(server.URL, "")
	reads := 0
	c.public.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		reads++
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials forwarded")
		}
		if r.URL.Host == "two.example" {
			return nil, errors.New("inaccessible")
		}
		return reply(r, "text/html; charset=utf-8", `<title>Annonce officielle</title><meta property="article:published_time" content="2026-10-27"><script>SECRET</script><nav>MENU</nav><p>Sortie aujourd’hui.</p>`), nil
	})
	result, err := c.Search(context.Background(), "date vérifiée", false)
	if err != nil || !result.Validate() || len(result.Sources) != 2 || reads != 2 {
		t.Fatalf("search: %+v %v reads=%d", result, err, reads)
	}
	if result.Sources[0].Kind != "page" || result.Sources[0].Title != "Annonce officielle" || result.Sources[0].Text != "Sortie aujourd’hui." || result.Sources[0].PublishedAt != "2026-10-27" || result.Sources[1].Kind != "snippet" || strings.Contains(result.Sources[1].Text, "<b>") {
		t.Fatal(result)
	}
}

func TestYouTubeRequiresVerifiedMetadataAndBoundsAttempts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("engines") != "youtube" {
			t.Error("YouTube not requested")
		}
		items := []map[string]string{{"url": "https://www.youtube.com.evil.org/watch?v=jNQXAC9IVRw"}}
		for range 30 {
			items = append(items, map[string]string{"title": "UNVERIFIED", "url": "https://www.youtube.com/watch?v=jNQXAC9IVRw"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": items})
	}))
	defer server.Close()
	c := New(server.URL, "")
	for _, good := range []bool{false, true} {
		reads := 0
		c.public.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			reads++
			if r.URL.Host != "www.youtube.com" || r.URL.Path != "/oembed" {
				t.Fatal("read arbitrary video URL")
			}
			if !good {
				return nil, errors.New("not verified")
			}
			return reply(r, "application/json", `{"title":"Me at the zoo","author_name":"jawed","provider_name":"YouTube","type":"video"}`), nil
		})
		r, err := c.Search(context.Background(), "zoo", true)
		if good && (err != nil || len(r.Sources) != 1 || r.Sources[0].Author != "jawed" || r.Sources[0].Title != "Me at the zoo" || r.Sources[0].Kind != "video") {
			t.Fatal(r, err)
		}
		if !good && (err == nil || len(r.Sources) != 0) {
			t.Fatal("unverified video returned")
		}
		if reads != 1 {
			t.Fatalf("duplicate or unbounded metadata lookup: %d", reads)
		}
	}
}

func TestReadRejectsBinaryOversizeAndPrivateBeforeRequest(t *testing.T) {
	c := New("http://search:8080", "")
	for _, mode := range []string{"binary", "oversize", "private", "redirect"} {
		calls := 0
		c.public.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			switch mode {
			case "binary":
				return reply(r, "application/pdf", "%PDF"), nil
			case "oversize":
				return reply(r, "text/plain", strings.Repeat("x", 512*1024+1)), nil
			case "redirect":
				resp := reply(r, "text/html", "")
				resp.StatusCode = 302
				resp.Header.Set("Location", "http://127.0.0.1/secret")
				return resp, nil
			}
			t.Fatal("private URL reached transport")
			return nil, nil
		})
		target := "https://example.org/page"
		if mode == "private" {
			target = "http://10.0.0.1/secret"
		}
		if _, err := c.Read(context.Background(), target); err == nil || calls > 1 {
			t.Errorf("%s allowed or followed private redirect (%d)", mode, calls)
		}
	}
}

func TestHTMLPrefersArticleAndFirstDocumentTitle(t *testing.T) {
	title, text, _ := extract(strings.NewReader(`<title>Article réel</title><div>Sidebar</div><svg><title>Menu</title></svg><main><h1>Annonce</h1><p>Voici la date.</p></main><footer>Mentions</footer>`))
	if title != "Article réel" || text != "Annonce Voici la date." {
		t.Fatal(title, text)
	}
}

func TestFrenchPublicationNeedsReadPublisherPageNotSnippetOrLookalike(t *testing.T) {
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{
			{"title": "Forum", "url": "https://publisher.fr.evil.example/article", "content": "unverified date"},
			{"title": "Missing page", "url": "https://publisher.fr/blocked", "content": "unverified date"},
			{"title": "Book", "url": "https://www.publisher.fr/book", "content": "old snippet"},
		}})
	}))
	defer engine.Close()
	c := New(engine.URL, "")
	reads := 0
	c.public.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		reads++
		if r.URL.Host == "publisher.fr" {
			return nil, errors.New("inaccessible")
		}
		if r.URL.Host != "www.publisher.fr" {
			t.Fatal("lookalike publisher read")
		}
		return reply(r, "text/html", `<title>Tome 3, édition standard</title><main>Parution française : 30 octobre 2026.</main>`), nil
	})
	r, err := c.SearchPublishers(context.Background(), "Tome 3 édition française", []string{"publisher.fr"})
	if err != nil || reads != 2 || len(r.Sources) != 1 || r.Sources[0].Kind != "page" || r.Sources[0].URL != "https://www.publisher.fr/book" {
		t.Fatal(r, err, reads)
	}
}
