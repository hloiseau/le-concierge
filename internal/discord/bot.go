package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/upstream"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Bot struct {
	Config  config.Config
	Client  *http.Client
	mu      sync.Mutex
	seen    map[string]time.Time
	slots   chan struct{}
	workers sync.WaitGroup
}

func NewBot(c config.Config) *Bot {
	return &Bot{Config: c, Client: upstream.New().HTTP, seen: map[string]time.Time{}, slots: make(chan struct{}, 8)}
}
func (b *Bot) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("POST /interactions", b.interaction)
	return mux
}
func (b *Bot) Wait() { b.workers.Wait() }
func (b *Bot) interaction(w http.ResponseWriter, r *http.Request) {
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if e != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	timestamp, signature := r.Header.Get("X-Signature-Timestamp"), r.Header.Get("X-Signature-Ed25519")
	i, e := Verify(b.Config.DiscordPublicKey, timestamp, signature, body, time.Now())
	if e != nil || i.ApplicationID != b.Config.DiscordClientID {
		http.Error(w, "invalid signature", 401)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if i.Type == 1 {
		_ = json.NewEncoder(w).Encode(map[string]int{"type": 1})
		return
	}
	if i.Type != 2 || i.GuildID != b.Config.DiscordGuildID || !config.Snowflake(i.Member.User.ID) || !config.Snowflake(i.ID) || len(i.Token) < 8 || len(i.Token) > 4096 {
		http.Error(w, "unsupported interaction", 403)
		return
	}
	b.mu.Lock()
	now := time.Now()
	for k, v := range b.seen {
		if now.After(v) {
			delete(b.seen, k)
		}
	}
	_, duplicate := b.seen[i.ID]
	if !duplicate && len(b.seen) < 10000 {
		b.seen[i.ID] = now.Add(5 * time.Minute)
	} else if len(b.seen) >= 10000 {
		duplicate = true
	}
	b.mu.Unlock()
	if duplicate {
		http.Error(w, "duplicate interaction", 409)
		return
	}
	select {
	case b.slots <- struct{}{}:
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{"type": 4, "data": Message("Le Concierge est occupé. Réessayez dans un instant.")})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"type": 5, "data": map[string]int{"flags": 64}})
	b.workers.Add(1)
	go func() {
		defer b.workers.Done()
		defer func() { <-b.slots }()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		req, e := http.NewRequestWithContext(ctx, "POST", b.Config.BotAPIURL+"/internal/discord", bytes.NewReader(body))
		if e != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer "+b.Config.BotAPISecret)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Signature-Timestamp", timestamp)
		req.Header.Set("X-Signature-Ed25519", signature)
		reply := Message("Le service est momentanément indisponible. Réessayez plus tard.")
		resp, e := b.Client.Do(req)
		if e == nil {
			if resp.StatusCode == 200 {
				var m struct {
					Content string `json:"content"`
				}
				if json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(&m) == nil && len(m.Content) > 0 && len([]rune(m.Content)) <= 2000 {
					reply = Message(m.Content)
				}
			}
			resp.Body.Close()
		}
		encoded, _ := json.Marshal(reply)
		// Webhook token is provided in a signed Discord interaction, never in a log.
		for attempt := 0; attempt < 3; attempt++ {
			target := "https://discord.com/api/v10/webhooks/" + i.ApplicationID + "/" + i.Token + "/messages/@original"
			req, e = http.NewRequestWithContext(ctx, "PATCH", target, bytes.NewReader(encoded))
			if e != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, e = b.Client.Do(req)
			if e != nil {
				slog.Warn("Discord reply failed")
				return
			}
			status := resp.StatusCode
			var retry struct {
				After float64 `json:"retry_after"`
			}
			if status == 429 {
				_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&retry)
			}
			resp.Body.Close()
			if status >= 200 && status < 300 {
				return
			}
			if status != 429 || retry.After <= 0 || retry.After > 5 {
				slog.Warn("Discord reply rejected", "status", status)
				return
			}
			select {
			case <-time.After(time.Duration(retry.After * float64(time.Second))):
			case <-ctx.Done():
				return
			}
		}
	}()
}
func (b *Bot) Register(ctx context.Context) error {
	if b.Config.DiscordBotToken == "" {
		return errors.New("DISCORD_BOT_TOKEN is required to register commands")
	}
	commands := []map[string]any{{"name": "aide", "description": "Guides, services et liaison des comptes", "type": 1}, {"name": "mes-demandes", "description": "Consulter vos demandes Seerr", "type": 1}, {"name": "nouveautes", "description": "Voir les derniers ajouts Plex autorisés", "type": 1}}
	raw, _ := json.Marshal(commands)
	req, e := http.NewRequestWithContext(ctx, "PUT", "https://discord.com/api/v10/applications/"+b.Config.DiscordClientID+"/guilds/"+b.Config.DiscordGuildID+"/commands", strings.NewReader(string(raw)))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bot "+b.Config.DiscordBotToken)
	req.Header.Set("Content-Type", "application/json")
	r, e := b.Client.Do(req)
	if e != nil {
		return errors.New("Discord registration unavailable")
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return errors.New("Discord rejected command registration; verify app/guild permissions")
	}
	return nil
}
