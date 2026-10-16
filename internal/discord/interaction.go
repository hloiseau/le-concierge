package discord

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

type Interaction struct {
	ID            string `json:"id"`
	ApplicationID string `json:"application_id"`
	Type          int    `json:"type"`
	GuildID       string `json:"guild_id"`
	Token         string `json:"token"`
	Member        struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"member"`
	Data struct {
		Name          string              `json:"name"`
		CustomID      string              `json:"custom_id"`
		ComponentType int                 `json:"component_type"`
		Options       []InteractionOption `json:"options"`
	} `json:"data"`
	Message struct {
		Flags int `json:"flags"`
	} `json:"message"`
}

type InteractionOption struct {
	Name  string          `json:"name"`
	Type  int             `json:"type"`
	Value json.RawMessage `json:"value"`
}

type Options struct {
	Kind   string
	TMDBID int
	Season *int
	Page   int
}

// Validate values again after signature verification; Discord registration is not an authorization boundary.
func CommandOptions(i Interaction) (Options, error) {
	options := Options{Page: 1}
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
		case "type":
			if (i.Data.Name != "surprise" && i.Data.Name != "suivre") || option.Type != 3 || json.Unmarshal(option.Value, &options.Kind) != nil {
				return Options{}, invalid
			}
			if (i.Data.Name == "surprise" && options.Kind != "movie" && options.Kind != "show") || (i.Data.Name == "suivre" && options.Kind != "movie" && options.Kind != "tv") {
				return Options{}, invalid
			}
		case "tmdb-id", "season", "page":
			var number int
			if option.Type != 4 || string(option.Value) == "null" || json.Unmarshal(option.Value, &number) != nil {
				return Options{}, invalid
			}
			switch option.Name {
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
				if i.Data.Name != "mes-suivis" || number < 1 || number > 1001 {
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
	return options, nil
}

// Command never takes an identity from a button: its owner must match the
// signed caller, whose current access is checked again by the API.
func Command(i Interaction) (string, int, error) {
	if i.Type == 2 {
		_, err := CommandOptions(i)
		return i.Data.Name, 0, err
	}
	parts := strings.Split(i.Data.CustomID, ":")
	if i.Type != 3 || i.Data.ComponentType != 2 || i.Message.Flags&64 == 0 || len(parts) != 3 || (parts[0] != "requests" && parts[0] != "recent") || parts[1] != i.Member.User.ID {
		return "", 0, errors.New("invalid pagination interaction")
	}
	offset, err := strconv.Atoi(parts[2])
	if err != nil || offset < 0 || offset > 10000 || (parts[0] == "requests" && offset%10 != 0) || strconv.Itoa(offset) != parts[2] {
		return "", 0, errors.New("invalid pagination offset")
	}
	if parts[0] == "recent" {
		return "nouveautes", offset, nil
	}
	return "mes-demandes", offset, nil
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
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

func (p RequestPagination) Components(userID string) []map[string]any {
	rows := []map[string]any{}
	if p.Offset < 0 || p.Offset > 10000 || p.Offset%10 != 0 || p.Total < 0 {
		return rows
	}
	buttons := []map[string]any{}
	button := func(label string, offset int) map[string]any {
		return map[string]any{"type": 2, "style": 2, "label": label, "custom_id": "requests:" + userID + ":" + strconv.Itoa(offset)}
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

func Help(origin string) string {
	return "**Le Concierge**\n/mes-demandes — Vos demandes Seerr\n/nouveautes — Les derniers ajouts Plex\n/surprise — Un film ou une série de votre catalogue\n/recap — Les ajouts des sept jours terminés (Europe/Paris)\n/suivre — Suivre un film ou une série par identifiant TMDB\n/mes-suivis — Consulter vos suivis\n\n[Ouvrir le Hub](" + origin + ")\nConnectez-vous au Hub avec Plex, puis liez Discord dans **Mon compte** pour utiliser les commandes personnelles. Seerr est associé automatiquement via votre identité Plex."
}
