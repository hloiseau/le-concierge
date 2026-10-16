package httpapi

import (
	"context"
	"testing"
	"time"
)

func TestDownloadarrCleanupCancelsAbandonedCredentials(t *testing.T) {
	for _, reason := range []string{"expired", "missing-session", "changed-owner", "cancelled"} {
		t.Run(reason, func(t *testing.T) {
			s, cookie := downloadarrAPIFixture(t)
			key := digest(cookie)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			g := downloadarrGrant{SessionKey: key, ExpiresAt: time.Now().Add(time.Minute), Context: ctx, Cancel: cancel}
			s.mu.Lock()
			s.downloadarrGrants = map[string]downloadarrGrant{"11111": g}
			s.mu.Unlock()
			s.cleanup()
			if len(s.downloadarrGrants) != 1 || ctx.Err() != nil {
				t.Fatal("cleanup removed a valid association")
			}
			s.mu.Lock()
			switch reason {
			case "expired":
				g.ExpiresAt = time.Now().Add(-time.Second)
				s.downloadarrGrants["11111"] = g
			case "missing-session":
				delete(s.sessions, key)
			case "changed-owner":
				sess := s.sessions[key]
				sess.UserID = "22222"
				s.sessions[key] = sess
			case "cancelled":
				cancel()
			}
			s.mu.Unlock()
			s.cleanup()
			if len(s.downloadarrGrants) != 0 || ctx.Err() == nil {
				t.Fatal("cleanup retained an unusable credential or its context")
			}
		})
	}
}
