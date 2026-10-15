package discord

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"github.com/hloiseau/le-concierge/internal/config"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSignedInteractions(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	stamp := strconv.FormatInt(now.Unix(), 10)
	body := `{"id":"11111","application_id":"12345","type":1}`
	signature := hex.EncodeToString(ed25519.Sign(private, []byte(stamp+body)))
	key := hex.EncodeToString(public)
	if _, e := Verify(key, stamp, signature, []byte(body), now); e != nil {
		t.Fatal(e)
	}
	if _, e := Verify(key, stamp, signature, []byte(body+" "), now); e == nil {
		t.Fatal("tampered body accepted")
	}
	if _, e := Verify(key, stamp, signature, []byte(body), now.Add(6*time.Minute)); e == nil {
		t.Fatal("expired signature accepted")
	}
	bot := NewBot(config.Config{DiscordClientID: "12345", DiscordPublicKey: key, DiscordGuildID: "22222"})
	req := httptest.NewRequest("POST", "/interactions", strings.NewReader(body))
	req.Header.Set("X-Signature-Timestamp", stamp)
	req.Header.Set("X-Signature-Ed25519", signature)
	w := httptest.NewRecorder()
	bot.Handler().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"type":1`) {
		t.Fatalf("PING failed %d %s", w.Code, w.Body.String())
	}
	body = `{"id":"11111","application_id":"12345","type":2,"guild_id":"99999","token":"not-a-real-token","member":{"user":{"id":"33333"}}}`
	signature = hex.EncodeToString(ed25519.Sign(private, []byte(stamp+body)))
	req = httptest.NewRequest("POST", "/interactions", strings.NewReader(body))
	req.Header.Set("X-Signature-Timestamp", stamp)
	req.Header.Set("X-Signature-Ed25519", signature)
	w = httptest.NewRecorder()
	bot.Handler().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("foreign guild accepted")
	}
}
