package departures

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func TestWatchNeverInventsUnstartedOrTransfersEditions(t *testing.T) {
	m := upstream.PlexItem{ID: "42", Kind: "movie", GUID: "plex://movie/one"}
	p := upstream.Playback{MediaID: "42", Kind: "movie", GUID: m.GUID}
	if Watch(m, nil) != "unknown" {
		t.Fatal("absence means unknown")
	}
	if Watch(m, []upstream.Playback{p}) != "in_progress" {
		t.Fatal("recorded play missing")
	}
	p.Completed = true
	m.ViewOffset = 100
	if Watch(m, []upstream.Playback{p}) != "completed" {
		t.Fatal("a rewatch erased completion")
	}
	m.ViewOffset = 0
	m.Edition = "Different edition"
	if Watch(m, []upstream.Playback{p}) != "unknown" {
		t.Fatal("history transferred to an ambiguous edition")
	}
	m.ViewCount = 1
	if Watch(m, []upstream.Playback{p}) != "completed" {
		t.Fatal("current per-user Plex state ignored")
	}
	m.Kind = "season"
	if Watch(m, []upstream.Playback{p}) != "unknown" {
		t.Fatal("child completed a season")
	}
}

func TestSourceCacheExpiresAndNeverFallsBackAfterFailure(t *testing.T) {
	calls := 0
	broken := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if broken {
			w.WriteHeader(500)
			return
		}
		bodies := map[string]string{"/api/app/status": `{"status":1,"version":"3.30.1"}`, "/api/media-server/type": `{"type":"plex"}`, "/api/media-server": `{"machineId":"server-a"}`, "/api/collections/overlay-data": `[]`, "/api/rules": `[]`}
		fmt.Fprint(w, bodies[r.URL.Path])
	}))
	defer server.Close()
	s := New(upstream.New())
	c := config.Config{MaintainerrURL: server.URL, PlexServerID: "server-a"}
	if _, _, err := s.sources(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	first := calls
	if _, _, err := s.sources(context.Background(), c); err != nil || calls != first {
		t.Fatal("fresh cache unused")
	}
	s.snapshot.UpdatedAt = time.Now().Add(-time.Minute)
	broken = true
	if snap, _, err := s.sources(context.Background(), c); err == nil || !snap.UpdatedAt.IsZero() || !s.snapshot.UpdatedAt.IsZero() {
		t.Fatal("expired snapshot served or retained")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.gate <- struct{}{}
	if _, _, err := s.sources(ctx, c); err == nil {
		t.Fatal("canceled waiter kept waiting")
	}
	<-s.gate
}
