package upstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSeerrOwnershipAndNormalization(t *testing.T) {
	wrong := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "key" {
			t.Error("missing server secret")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/user/42/requests":
			if r.URL.Query().Get("skip") != "10" {
				t.Error("pagination lost")
			}
			owner := 42
			if wrong {
				owner = 99
			}
			fmt.Fprintf(w, `{"pageInfo":{"results":12},"results":[{"id":1,"type":"movie","status":2,"requestedBy":{"id":%d},"media":{"tmdbId":100,"status":5},"seasons":[]}]}`, owner)
		case "/api/v1/movie/100":
			fmt.Fprint(w, `{"title":"A movie"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := New()
	p, e := c.Requests(context.Background(), server.URL, "key", 42, 10)
	if e != nil || len(p.Items) != 1 || p.Items[0].Status != "available" || p.Items[0].Title != "A movie" || p.Total != 12 {
		t.Fatalf("bad result %+v %v", p, e)
	}
	wrong = true
	if _, e = c.Requests(context.Background(), server.URL, "key", 42, 10); !errors.Is(e, ErrDenied) {
		t.Fatal("another user's request leaked")
	}
}
func TestRecentUsesExactServerAndUserToken(t *testing.T) {
	expected := "server-a"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/identity":
			fmt.Fprintf(w, `{"MediaContainer":{"machineIdentifier":%q}}`, expected)
		case "/library/recentlyAdded":
			calls++
			if r.Header.Get("X-Plex-Token") != "user-resource-token" {
				t.Error("wrong token")
			}
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"12","title":"Movie","type":"movie","addedAt":1000}]}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := New()
	g := Grant{ClientID: "client", ServerToken: "user-resource-token"}
	items, e := c.Recent(context.Background(), server.URL, "server-a", g, 0)
	if e != nil || len(items) != 1 || strings.Contains(items[0].URL, "token") {
		t.Fatalf("bad recent data %v %v", items, e)
	}
	expected = "server-b"
	if _, e = c.Recent(context.Background(), server.URL, "server-a", g, 0); !errors.Is(e, ErrDenied) {
		t.Fatal("wrong server accepted")
	}
	if calls != 1 {
		t.Fatal("token sent to mismatched server")
	}
}
func TestNoRedirectAndBoundedResponse(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect followed with secrets") }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer source.Close()
	c := New()
	if e := c.JSON(context.Background(), "GET", source.URL, map[string]string{"X-Api-Key": "secret"}, "", nil); e == nil {
		t.Fatal("redirect accepted")
	}
}
