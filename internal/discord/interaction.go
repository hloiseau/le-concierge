package discord

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
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
		Name string `json:"name"`
	} `json:"data"`
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
	return map[string]any{"content": content, "allowed_mentions": map[string]any{"parse": []string{}}, "flags": 64}
}

func Help(origin string) string {
	return "**Le Concierge**\n/mes-demandes — Vos demandes Seerr\n/nouveautes — Les derniers ajouts Plex\n\n[Ouvrir le Hub](" + origin + ")\nConnectez-vous au Hub avec Plex, puis liez Discord dans **Mon compte** pour utiliser les commandes personnelles. Seerr est associé automatiquement via votre identité Plex."
}
