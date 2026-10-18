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
	"net/url"
	"strings"
	"sync"
	"time"
)

type Bot struct {
	Config          config.Config
	Client          *http.Client
	mu              sync.Mutex
	seen            map[string]time.Time
	interestBusy    map[string]bool
	interestRetryAt time.Time
	slots           chan struct{}
	workers         sync.WaitGroup
}

func NewBot(c config.Config) *Bot {
	return &Bot{Config: c, Client: upstream.New().HTTP, seen: map[string]time.Time{}, interestBusy: map[string]bool{}, slots: make(chan struct{}, 8)}
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
	command, offset, commandErr := Command(i)
	if (i.Type == 2 && i.Data.Name == "salons") || (i.Type == 3 && strings.HasPrefix(i.Data.CustomID, "interests:")) {
		_, commandErr = b.interestAction(i)
		command = "salons"
	}
	if commandErr != nil || i.GuildID != b.Config.DiscordGuildID || !config.Snowflake(i.Member.User.ID) || !config.Snowflake(i.ID) || len(i.Token) < 8 || len(i.Token) > 4096 {
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
	if command == "salons" {
		b.handleInterests(w, i)
		return
	}
	// Static help must work even when the API is unavailable.
	if command == "aide" {
		help := Help(b.Config.PublicOrigin, b.Config.Services)
		if len(b.Config.DiscordInterests) > 0 {
			help = "/salons — Choisir ou quitter vos salons thématiques, sans compte Plex.\n\n" + help
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"type": 4, "data": Message(help)})
		return
	}
	select {
	case b.slots <- struct{}{}:
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{"type": 4, "data": Message("Le Concierge est occupé. Réessayez dans un instant.")})
		return
	}
	// Discord supports an empty 202 when the callback is sent separately.
	// Wait for that callback's success before editing @original: flushing an
	// inline type-5 response does not prove Discord has created the message.
	w.WriteHeader(http.StatusAccepted)
	b.workers.Add(1)
	go func() {
		defer b.workers.Done()
		defer func() { <-b.slots }()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if !b.acknowledge(ctx, i) {
			return
		}
		req, e := http.NewRequestWithContext(ctx, "POST", b.Config.BotAPIURL+"/internal/discord", bytes.NewReader(body))
		if e != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer "+b.Config.BotAPISecret)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Signature-Timestamp", timestamp)
		req.Header.Set("X-Signature-Ed25519", signature)
		reply := Message("Le service est momentanément indisponible. Réessayez plus tard.")
		publicContent := ""
		// Personal source reads can take 20 seconds; keep Discord's shorter timeout.
		apiClient := *b.Client
		apiClient.Timeout = 22 * time.Second
		resp, e := apiClient.Do(req)
		if e == nil {
			if resp.StatusCode == 200 {
				var m struct {
					Content          string             `json:"content"`
					PublicContent    string             `json:"publicContent"`
					Pagination       *RequestPagination `json:"pagination"`
					RecentPagination *RecentPagination  `json:"recentPagination"`
				}
				if json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(&m) == nil && len(m.Content) > 0 && len([]rune(m.Content)) <= 2000 {
					reply = Message(m.Content)
					options, _ := CommandOptions(i)
					if options.Public && PublicCommand(command) && len([]rune(m.PublicContent)) <= 2000 {
						publicContent = m.PublicContent
					}
					if command == "mes-demandes" && m.Pagination != nil && m.Pagination.Offset == offset && m.Pagination.Status == options.Status {
						reply["components"] = m.Pagination.Components(i.Member.User.ID)
					}
					if command == "nouveautes" && m.RecentPagination != nil && m.RecentPagination.Offset == offset {
						reply["components"] = m.RecentPagination.Components(i.Member.User.ID)
					}
				}
			}
			resp.Body.Close()
		}
		if publicContent != "" {
			reply = Message("Résultat prêt à partager dans ce salon.")
		}
		// Complete the private defer before creating a public followup: Discord
		// otherwise edits the deferred reply and preserves its private visibility.
		if !b.webhook(ctx, i, "PATCH", "/messages/@original", reply) || publicContent == "" {
			return
		}
		publicReply := Message(publicContent)
		publicReply["flags"] = 0
		confirmation := "Publication non confirmée. Vérifiez le salon avant de réessayer."
		if b.webhook(ctx, i, "POST", "", publicReply) {
			confirmation = "Résultat partagé dans ce salon."
		}
		b.webhook(ctx, i, "PATCH", "/messages/@original", Message(confirmation))
	}()
}

func (b *Bot) webhook(ctx context.Context, i Interaction, method, path string, message map[string]any) bool {
	encoded, _ := json.Marshal(message)
	// Webhook token is provided in a signed Discord interaction, never in a log.
	target := "https://discord.com/api/v10/webhooks/" + i.ApplicationID + "/" + url.PathEscape(i.Token) + path
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(encoded))
		if err != nil {
			return false
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := b.Client.Do(req)
		if err != nil {
			slog.Warn("Discord reply failed")
			return false
		}
		status := resp.StatusCode
		var retry struct {
			After float64 `json:"retry_after"`
			Code  int     `json:"code"`
		}
		if status >= 400 {
			_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&retry)
		}
		resp.Body.Close()
		if status >= 200 && status < 300 {
			return true
		}
		if status != 429 || retry.After <= 0 || retry.After > 5 {
			slog.Warn("Discord reply rejected", "status", status, "discord_code", retry.Code)
			return false
		}
		select {
		case <-time.After(time.Duration(retry.After * float64(time.Second))):
		case <-ctx.Done():
			return false
		}
	}
	return false
}

// acknowledge has its own short deadline to respect Discord's initial-response window.
func (b *Bot) acknowledge(ctx context.Context, i Interaction) bool {
	ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	target := "https://discord.com/api/v10/interactions/" + i.ID + "/" + url.PathEscape(i.Token) + "/callback"
	callback := `{"type":5,"data":{"flags":64}}`
	if i.Type == 3 && i.Data.CustomID != "interests:open" {
		callback = `{"type":6}`
	}
	req, err := http.NewRequestWithContext(ctx, "POST", target, strings.NewReader(callback))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.Client.Do(req)
	if err != nil {
		slog.Warn("Discord acknowledgement failed")
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return true
	}
	var detail struct {
		Code int `json:"code"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&detail)
	slog.Warn("Discord acknowledgement rejected", "status", resp.StatusCode, "discord_code", detail.Code)
	return false
}

func (b *Bot) Register(ctx context.Context) error {
	if b.Config.DiscordBotToken == "" {
		return errors.New("DISCORD_BOT_TOKEN is required to register commands")
	}
	commands := []map[string]any{
		{"name": "aide", "description": "Guides, services et liaison des comptes", "type": 1},
		{"name": "mes-demandes", "description": "Consulter vos demandes Seerr", "type": 1, "options": []map[string]any{
			{"name": "etat", "description": "Filtrer toutes vos demandes par état", "type": 3, "choices": []map[string]any{
				{"name": "Tous les états", "value": "all"},
				{"name": "En attente", "value": "pending"},
				{"name": "Approuvée", "value": "approved"},
				{"name": "En cours", "value": "processing"},
				{"name": "Disponibilité partielle", "value": "partial"},
				{"name": "Disponible", "value": "available"},
				{"name": "Refusée", "value": "declined"},
				{"name": "En erreur", "value": "error"},
				{"name": "État inconnu", "value": "unknown"},
			}},
		}},
		{"name": "a-voir", "description": "Voir vos contenus avant leur départ estimé de Plex", "type": 1},
		{"name": "nouveautes", "description": "Voir les derniers ajouts Plex autorisés", "type": 1},
		{"name": "surprise", "description": "Tirer un titre dans votre catalogue Plex", "type": 1, "options": []map[string]any{
			{"name": "type", "description": "Films ou séries (les deux par défaut)", "type": 3, "choices": []map[string]any{{"name": "Film", "value": "movie"}, {"name": "Série", "value": "show"}}},
		}},
		{"name": "recap", "description": "Voir les ajouts des sept jours terminés, en heure de Paris", "type": 1},
		{"name": "diagnostic", "description": "Diagnostiquer une demande Seerr (administrateur)", "type": 1, "default_member_permissions": "8", "options": []map[string]any{
			{"name": "demande", "description": "Identifiant de la demande Seerr", "type": 4, "required": true, "min_value": 1, "max_value": 2147483647},
		}},
		{"name": "livres", "description": "Chercher dans votre catalogue Grimmory", "type": 1, "options": []map[string]any{
			{"name": "recherche", "description": "Titre, auteur, série ou ISBN", "type": 3, "required": false, "min_length": 1, "max_length": 200},
			{"name": "page", "description": "Page du catalogue", "type": 4, "required": false, "min_value": 1, "max_value": 1001},
		}},
		{"name": "demander-livre", "description": "Choisir et confirmer une édition dans Shelfmark", "type": 1, "options": []map[string]any{
			{"name": "recherche", "description": "Titre, auteur ou ISBN à rechercher dans Shelfmark", "type": 3, "required": true, "min_length": 1, "max_length": 200},
		}},
	}
	if len(b.Config.DiscordInterests) > 0 {
		commands = append(commands, map[string]any{"name": "salons", "description": "Choisir ou quitter vos salons par centre d’intérêt", "type": 1})
	}
	for _, command := range commands {
		if PublicCommand(command["name"].(string)) {
			options, _ := command["options"].([]map[string]any)
			command["options"] = append(options, map[string]any{"name": "public", "description": "Partager le résultat dans ce salon (privé par défaut)", "type": 5})
		}
	}
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
