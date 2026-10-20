package httpapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

// Do not persist resource tokens or cached verification: Plex must recheck the
// account and server access before any use after a restart.
type plexAuthorization struct {
	PlexID       string
	AccountToken string
	ClientID     string
}

func (s *Server) authorizationCipher() (cipher.AEAD, error) {
	key, err := hex.DecodeString(s.Config.PlexGrantKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("invalid Plex authorization encryption key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}

func (s *Server) authorizationBinding(u store.User) []byte {
	// Length-safe encoding binds a ciphertext to this account, server and origin.
	b, _ := json.Marshal([]string{"plex-authorization-v1", u.ID, u.PlexID, s.Config.PlexServerID, s.Config.PublicOrigin})
	return b
}

// Called under mu, in the same critical section as login and logout.
func (s *Server) savePlexAuthorization(ctx context.Context, u store.User, g *upstream.Grant) error {
	if s.Config.PlexGrantKey == "" {
		return nil
	}
	aead, err := s.authorizationCipher()
	if err != nil {
		return err
	}
	b, err := json.Marshal(plexAuthorization{g.PlexID, g.AccountToken, g.ClientID})
	if err != nil {
		return err
	}
	if err = s.Store.SavePlexAuthorization(ctx, u.ID, aead.Seal(nil, nil, b, s.authorizationBinding(u))); err != nil {
		return err
	}
	g.Persistent = true
	return nil
}

// RestorePlexAuthorizations runs once, before workers and HTTP are started.
// A missing key with existing records or an authentication failure stops startup;
// silently dropping authorizations would hide a lost key or damaged backup.
func (s *Server) RestorePlexAuthorizations(ctx context.Context) error {
	records, err := s.Store.PlexAuthorizations(ctx)
	if err != nil {
		return errors.New("cannot read saved Plex authorizations")
	}
	if len(records) == 0 {
		return nil
	}
	aead, err := s.authorizationCipher()
	if err != nil {
		return errors.New("saved Plex authorizations require their original encryption key")
	}
	restored := make(map[string]upstream.Grant, len(records))
	for id, ciphertext := range records {
		u, err := s.Store.User(ctx, id)
		if err != nil {
			return errors.New("cannot read saved Plex authorization owner")
		}
		b, err := aead.Open(nil, nil, ciphertext, s.authorizationBinding(u))
		if err != nil {
			return errors.New("cannot decrypt saved Plex authorization; check key, server, origin and backup")
		}
		var saved plexAuthorization
		if json.Unmarshal(b, &saved) != nil || saved.PlexID == "" || saved.PlexID != u.PlexID || saved.AccountToken == "" || saved.ClientID == "" {
			return errors.New("invalid saved Plex authorization")
		}
		restored[id] = upstream.Grant{PlexID: saved.PlexID, AccountToken: saved.AccountToken, ClientID: saved.ClientID, Persistent: true}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, g := range restored {
		s.grants[id] = g
	}
	return nil
}
