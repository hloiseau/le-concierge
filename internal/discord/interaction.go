package discord

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

type Interaction struct {
	ID            string `json:"id"`
	ApplicationID string `json:"application_id"`
	Type          int    `json:"type"`
	GuildID       string `json:"guild_id"`
	ChannelID     string `json:"channel_id"`
	Token         string `json:"token"`
	Member        struct {
		Nick string `json:"nick"`
		User struct {
			ID         string `json:"id"`
			Username   string `json:"username"`
			GlobalName string `json:"global_name"`
		} `json:"user"`
	} `json:"member"`
	Data struct {
		Name          string              `json:"name"`
		CustomID      string              `json:"custom_id"`
		ComponentType int                 `json:"component_type"`
		Options       []InteractionOption `json:"options"`
		Values        []string            `json:"values"`
	} `json:"data"`
	Message struct {
		Flags  int    `json:"flags"`
		ID     string `json:"id"`
		Author struct {
			ID string `json:"id"`
		} `json:"author"`
	} `json:"message"`
}

type InteractionOption struct {
	Name  string          `json:"name"`
	Type  int             `json:"type"`
	Value json.RawMessage `json:"value"`
}

type Options struct {
	ChatMessage string
	Forget      bool
	Public      bool
	Query       string
	RequestID   int
	Kind        string
	TMDBID      int
	Season      *int
	Page        int
	Status      string
}

func PublicCommand(command string) bool {
	return command == "nouveautes" || command == "surprise" || command == "recap"
}

// Validate values again after signature verification; Discord registration is not an authorization boundary.
func CommandOptions(i Interaction) (Options, error) {
	options := Options{Page: 1}
	if i.Type == 3 {
		_, _, status, err := paginationCommand(i)
		options.Status = status
		return options, err
	}
	if i.Type != 2 {
		return options, nil
	}
	invalid := errors.New("invalid command options")
	seen := map[string]bool{}
	for _, option := range i.Data.Options {
		if seen[option.Name] {
			return Options{}, invalid
		}
		seen[option.Name] = true
		switch option.Name {
		case "message":
			if i.Data.Name != "eris" || option.Type != 3 || string(option.Value) == "null" || json.Unmarshal(option.Value, &options.ChatMessage) != nil || strings.TrimSpace(options.ChatMessage) == "" || utf8.RuneCountInString(options.ChatMessage) > 500 || strings.ContainsFunc(options.ChatMessage, unicode.IsControl) {
				return Options{}, invalid
			}
		case "oublier":
			if i.Data.Name != "eris" || option.Type != 5 || string(option.Value) == "null" || json.Unmarshal(option.Value, &options.Forget) != nil {
				return Options{}, invalid
			}
		case "etat":
			if i.Data.Name != "mes-demandes" || option.Type != 3 || json.Unmarshal(option.Value, &options.Status) != nil || (options.Status != "all" && !upstream.ValidRequestStatus(options.Status)) {
				return Options{}, invalid
			}
			if options.Status == "all" {
				options.Status = ""
			}
		case "public":
			if !PublicCommand(i.Data.Name) || option.Type != 5 || string(option.Value) == "null" || json.Unmarshal(option.Value, &options.Public) != nil {
				return Options{}, invalid
			}
		case "recherche":
			if (i.Data.Name != "livres" && i.Data.Name != "demander-livre") || option.Type != 3 || string(option.Value) == "null" || json.Unmarshal(option.Value, &options.Query) != nil || strings.TrimSpace(options.Query) == "" || utf8.RuneCountInString(options.Query) > 200 || strings.ContainsFunc(options.Query, unicode.IsControl) {
				return Options{}, invalid
			}
			options.Query = strings.TrimSpace(options.Query)
		case "type":
			if (i.Data.Name != "surprise" && i.Data.Name != "suivre") || option.Type != 3 || json.Unmarshal(option.Value, &options.Kind) != nil {
				return Options{}, invalid
			}
			if (i.Data.Name == "surprise" && options.Kind != "movie" && options.Kind != "show") || (i.Data.Name == "suivre" && options.Kind != "movie" && options.Kind != "tv") {
				return Options{}, invalid
			}
		case "tmdb-id", "season", "page", "demande":
			var number int
			if option.Type != 4 || string(option.Value) == "null" || json.Unmarshal(option.Value, &number) != nil {
				return Options{}, invalid
			}
			switch option.Name {
			case "demande":
				if i.Data.Name != "diagnostic" || number < 1 || number > 2147483647 {
					return Options{}, invalid
				}
				options.RequestID = number
			case "tmdb-id":
				if i.Data.Name != "suivre" || number < 1 || number > 2147483647 {
					return Options{}, invalid
				}
				options.TMDBID = number
			case "season":
				if i.Data.Name != "suivre" || number < 0 || number > 10000 {
					return Options{}, invalid
				}
				options.Season = &number
			case "page":
				if (i.Data.Name != "mes-suivis" && i.Data.Name != "livres") || number < 1 || number > 1001 {
					return Options{}, invalid
				}
				options.Page = number
			}
		default:
			return Options{}, invalid
		}
	}
	if i.Data.Name == "suivre" && (options.Kind == "" || options.TMDBID == 0 || (options.Kind == "movie" && options.Season != nil)) {
		return Options{}, invalid
	}
	if i.Data.Name == "demander-livre" && options.Query == "" {
		return Options{}, invalid
	}
	if i.Data.Name == "diagnostic" && options.RequestID == 0 {
		return Options{}, invalid
	}
	return options, nil
}

// Command never takes an identity from a button: its owner must match the
// signed caller, whose current access is checked again by the API.
func Command(i Interaction) (string, int, error) {
	if i.Type == 3 && strings.HasPrefix(i.Data.CustomID, "eris:") {
		if !ValidErisButton(i.Data.CustomID) || i.Data.ComponentType != 2 || (i.Message.Flags&64 == 0 && !config.Snowflake(i.ChannelID)) || len(i.Data.Options) != 0 || len(i.Data.Values) != 0 {
			return "", 0, errors.New("invalid Eris confirmation")
		}
		return "eris", 0, nil
	}
	if i.Type == 2 {
		_, err := CommandOptions(i)
		return i.Data.Name, 0, err
	}
	command, offset, _, err := paginationCommand(i)
	return command, offset, err
}

func ValidErisButton(id string) bool {
	parts := strings.Split(id, ":")
	if len(parts) != 3 || parts[0] != "eris" || (parts[1] != "choose" && parts[1] != "confirm" && parts[1] != "cancel") || len(parts[2]) != 64 {
		return false
	}
	_, err := hex.DecodeString(parts[2])
	return err == nil
}

func paginationCommand(i Interaction) (string, int, string, error) {
	parts := strings.Split(i.Data.CustomID, ":")
	if i.Type != 3 || i.Data.ComponentType != 2 || i.Message.Flags&64 == 0 || (len(parts) != 3 && len(parts) != 4) || (parts[0] != "requests" && parts[0] != "recent") || parts[1] != i.Member.User.ID || len(i.Data.Options) != 0 {
		return "", 0, "", errors.New("invalid pagination interaction")
	}
	status := ""
	if len(parts) == 4 {
		if parts[0] != "requests" || !upstream.ValidRequestStatus(parts[3]) {
			return "", 0, "", errors.New("invalid pagination status")
		}
		status = parts[3]
	}
	offset, err := strconv.Atoi(parts[2])
	if err != nil || offset < 0 || offset > 10000 || (parts[0] == "requests" && offset%10 != 0) || strconv.Itoa(offset) != parts[2] {
		return "", 0, "", errors.New("invalid pagination offset")
	}
	if parts[0] == "recent" {
		return "nouveautes", offset, "", nil
	}
	return "mes-demandes", offset, status, nil
}

type RecentPagination struct {
	Offset     int  `json:"offset"`
	NextOffset *int `json:"nextOffset"`
}

func (p RecentPagination) Components(userID string) []map[string]any {
	rows := []map[string]any{}
	if p.Offset < 0 || p.Offset > 10000 {
		return rows
	}
	buttons := []map[string]any{}
	button := func(label string, offset int) map[string]any {
		return map[string]any{"type": 2, "style": 2, "label": label, "custom_id": "recent:" + userID + ":" + strconv.Itoa(offset)}
	}
	if p.Offset > 0 {
		buttons = append(buttons, button("Début", 0))
	}
	if p.NextOffset != nil && *p.NextOffset > p.Offset && *p.NextOffset <= p.Offset+6 && *p.NextOffset <= 10000 {
		buttons = append(buttons, button("Suivant", *p.NextOffset))
	}
	if len(buttons) > 0 {
		rows = append(rows, map[string]any{"type": 1, "components": buttons})
	}
	return rows
}

type RequestPagination struct {
	Offset int    `json:"offset"`
	Total  int    `json:"total"`
	Status string `json:"status,omitempty"`
}

func (p RequestPagination) Components(userID string) []map[string]any {
	rows := []map[string]any{}
	if p.Offset < 0 || p.Offset > 10000 || p.Offset%10 != 0 || p.Total < 0 || (p.Status != "" && !upstream.ValidRequestStatus(p.Status)) {
		return rows
	}
	buttons := []map[string]any{}
	button := func(label string, offset int) map[string]any {
		id := "requests:" + userID + ":" + strconv.Itoa(offset)
		if p.Status != "" {
			id += ":" + p.Status
		}
		return map[string]any{"type": 2, "style": 2, "label": label, "custom_id": id}
	}
	if p.Offset > 0 {
		buttons = append(buttons, button("Précédent", p.Offset-10))
	}
	if p.Offset < 10000 && p.Offset+10 < p.Total {
		buttons = append(buttons, button("Suivant", p.Offset+10))
	}
	if len(buttons) > 0 {
		rows = append(rows, map[string]any{"type": 1, "components": buttons})
	}
	return rows
}

func Verify(publicKey, timestamp, signature string, body []byte, now time.Time) (Interaction, error) {
	var i Interaction
	pk, e := hex.DecodeString(publicKey)
	if e != nil || len(pk) != ed25519.PublicKeySize {
		return i, errors.New("invalid key")
	}
	sig, e := hex.DecodeString(signature)
	if e != nil || len(sig) != ed25519.SignatureSize {
		return i, errors.New("invalid signature")
	}
	t, e := strconv.ParseInt(timestamp, 10, 64)
	if e != nil || now.Sub(time.Unix(t, 0)) > 5*time.Minute || time.Unix(t, 0).Sub(now) > 30*time.Second {
		return i, errors.New("expired signature")
	}
	payload := append([]byte(timestamp), body...)
	if !ed25519.Verify(pk, payload, sig) {
		return i, errors.New("invalid signature")
	}
	if json.Unmarshal(body, &i) != nil {
		return i, errors.New("invalid interaction")
	}
	return i, nil
}
func Message(content string) map[string]any {
	return map[string]any{"content": content, "allowed_mentions": map[string]any{"parse": []string{}}, "flags": 64, "components": []map[string]any{}}
}

func Help(origin string, services []config.Service) string {
	text := "**Eris**\n@Eris + votre message, puis Répondre — Discuter dans ce salon\n/eris message:… — Discuter en privé ; oublier:true pour repartir de zéro\n/mes-demandes — Vos demandes Seerr et leurs notifications dans le Hub\n/a-voir — Vos contenus avant leur départ estimé de Plex (privé)\n/nouveautes — Les derniers ajouts Plex\n/surprise — Un film ou une série de votre catalogue\n/recap — Les ajouts des sept jours terminés (Europe/Paris)\n/livres — Votre catalogue Grimmory (association OPDS dans Le Hub)\n/demander-livre — Choisir et confirmer une édition dans Shelfmark\n/diagnostic — Diagnostic d’une demande Seerr (administrateur)\n\nAjoutez public:true à /nouveautes, /surprise ou /recap pour partager le résultat dans ce salon. Les réponses sont privées par défaut.\n\n"
	text += "Ajoutez etat à /mes-demandes pour filtrer toutes vos demandes Seerr (par exemple Disponible ou En cours).\n\n"
	// Encode Markdown delimiters without changing the configured public destination.
	link := func(label, target string) string {
		return "[" + label + "](" + strings.NewReplacer("(", "%28", ")", "%29", "[", "%5B", "]", "%5D", "<", "%3C", ">", "%3E").Replace(target) + ")"
	}
	origin = strings.TrimRight(origin, "/")
	footer := link("Services du Hub", origin) + " · " + link("FAQ et guides", origin+"/aide") + "\nConnectez-vous au Hub avec Plex, puis liez Discord dans " + link("Mon compte", origin+"/compte") + " pour les commandes personnelles. Seerr est associé automatiquement via votre identité Plex."
	if len(text)+len(footer) > 1900 {
		footer = "Ouvrez le Hub pour les services, la FAQ et les guides. Connectez-vous avec Plex, puis liez Discord dans Mon compte pour les commandes personnelles."
	}
	labels := map[string]string{"plex": "Plex", "seerr": "Seerr", "downloadarr": "Downloadarr", "torrent-watch": "Torrent Watch", "grimmory": "Grimmory", "shelfmark": "Shelfmark"}
	seen := map[string]bool{}
	for _, service := range services {
		u, err := url.Parse(service.URL)
		label := labels[service.ID]
		if label == "" || seen[service.ID] || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			continue
		}
		line := link(label, u.String()) + "\n"
		if len(text)+len(line)+len(footer) <= 1900 {
			text += line
			seen[service.ID] = true
		}
	}
	return text + footer
}
