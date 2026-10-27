package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestOuicheurPersonalSessionAndSurpriseProjection(t *testing.T) {
	c := New()
	secret := strings.Repeat("a", 64)
	id := strings.Repeat("b", 32)
	list := "11111111-1111-1111-1111-111111111111"
	checks := 0
	c.HTTP.Transport = animeRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "gifts.test" || r.Header.Get("Authorization") != "" || r.Header.Get("Origin") != "https://gifts.test" {
			t.Fatal("foreign credential target")
		}
		if r.URL.Path == "/api/login" {
			var input map[string]string
			json.NewDecoder(r.Body).Decode(&input)
			if input["login"] != "alice" || input["password"] != "personal-password" || r.Method != "POST" {
				t.Fatal("login contract")
			}
			resp := animeResponse(`{"ok":true,"role":"member"}`)
			resp.Header.Add("Set-Cookie", "wishlister_session="+secret+"; HttpOnly; Path=/")
			return resp, nil
		}
		cookie, e := r.Cookie("wishlister_session")
		if e != nil || cookie.Value != secret || r.Method != "GET" {
			t.Fatal("personal cookie missing")
		}
		switch r.URL.Path {
		case "/api/account/sessions":
			checks++
			return animeResponse(`[{"id":"` + id + `","current":true,"expires_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}]`), nil
		case "/":
			if r.URL.Query().Get("preview") != "1" {
				t.Fatal("owner elevation")
			}
			return animeResponse(`<label>Liste<select><option value="">Toutes</option><option value="` + list + `">Alice &amp; Bob</option></select></label><label>Catégorie<select><option value="22222222-2222-2222-2222-222222222222">Category not list</option></select></label><script>private reservation</script>`), nil
		case "/api/wishes":
			q := r.URL.Query()
			if q.Get("mode") != "public" || q.Get("list") != list || q.Get("available") != "" {
				t.Fatal("scope or reservation inference")
			}
			return animeResponse(`{"items":[{"list_id":"` + list + `","title":"Un jeu","description":"Coopératif","target":40,"currency":"EUR","surprise_hidden":true,"reserved":null,"funded":99,"donor":"PRIVATE"}],"next":null}`), nil
		}
		t.Fatal("unexpected endpoint")
		return nil, nil
	})
	session, err := c.OuicheurLogin(context.Background(), "https://gifts.test", "alice", "personal-password")
	if err != nil {
		t.Fatal(err)
	}
	lists, err := c.OuicheurLists(context.Background(), "https://gifts.test", session)
	if err != nil || len(lists) != 1 || lists[0].Name != "Alice & Bob" {
		t.Fatal(lists, err)
	}
	ideas, more, err := c.OuicheurIdeas(context.Background(), "https://gifts.test", session, list, "")
	b, _ := json.Marshal(ideas)
	if err != nil || more || len(ideas) != 1 || strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "reserved") || checks != 5 {
		t.Fatal(ideas, checks, err)
	}
}

func TestOuicheurRevocationDuringPublicReadDiscardsResult(t *testing.T) {
	c := New()
	calls := 0
	secret := strings.Repeat("a", 64)
	id := strings.Repeat("b", 32)
	c.HTTP.Transport = animeRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/account/sessions" {
			calls++
			if calls == 2 {
				resp := animeResponse(`{}`)
				resp.StatusCode = 401
				return resp, nil
			}
			return animeResponse(`[{"id":"` + id + `","current":true,"expires_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}]`), nil
		}
		return animeResponse(`<label>Liste<select><option value="11111111-1111-1111-1111-111111111111">Private</option></select></label>`), nil
	})
	lists, err := c.OuicheurLists(context.Background(), "https://gifts.test", OuicheurSession{Cookie: secret, ID: id})
	if err != ErrDenied || lists != nil {
		t.Fatal("expired cookie fell back to guest", lists, err)
	}
}

func TestOuicheurCredentialsNeverFollowRedirect(t *testing.T) {
	c := New()
	calls := 0
	c.HTTP.Transport = animeRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		resp := animeResponse(`{}`)
		resp.StatusCode = 302
		resp.Header.Set("Location", "https://other.test/login")
		return resp, nil
	})
	if _, err := c.OuicheurLogin(context.Background(), "https://gifts.test", "alice", "password"); err == nil || calls != 1 {
		t.Fatal("redirect accepted", calls, err)
	}
}
