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
		Name          string `json:"name"`
		CustomID      string `json:"custom_id"`
		ComponentType int    `json:"component_type"`
	} `json:"data"`
	Message struct {
		Flags int `json:"flags"`
	} `json:"message"`
}

// Command never takes an identity from a button: its owner must match the
// signed caller, whose current access is checked again by the API.
func Command(i Interaction) (string, int, error) {
	if i.Type == 2 {
		return i.Data.Name, 0, nil
	}
	parts := strings.Split(i.Data.CustomID, ":")
	if i.Type != 3 || i.Data.ComponentType != 2 || i.Message.Flags&64 == 0 || len(parts) != 3 || parts[0] != "requests" || parts[1] != i.Member.User.ID {
		return "", 0, errors.New("invalid pagination interaction")
	}
	offset, err := strconv.Atoi(parts[2])
	if err != nil || offset < 0 || offset > 10000 || offset%10 != 0 || strconv.Itoa(offset) != parts[2] {
		return "", 0, errors.New("invalid pagination offset")
	}
	return "mes-demandes", offset, nil
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
	return "**Le Concierge**\n/mes-demandes — Vos demandes Seerr\n/nouveautes — Les derniers ajouts Plex\n\n[Ouvrir le Hub](" + origin + ")\nConnectez-vous au Hub avec Plex, puis liez Discord dans **Mon compte** pour utiliser les commandes personnelles. Seerr est associé automatiquement via votre identité Plex."
}
