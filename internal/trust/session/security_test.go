package session_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

// TestTodo_TRUST_003_Security is the TRUST-003 security test. It checks that
// a session's bearer secret never appears in any log-safe rendering this
// package produces, that revocation is idempotent-safe rather than
// panicking or double-auditing, and that concurrent use cannot desynchronize
// the replay index (a race here would mean two callers could both "win" a
// refresh of the same token).
func TestTodo_TRUST_003_Security(t *testing.T) {
	ctx := context.Background()

	t.Run("no rendering ever contains the raw refresh token", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		rec, token := mustCreate(t, m, validSpec())
		rotated, second, err := m.Refresh(ctx, token)
		if err != nil {
			t.Fatalf("Refresh: %v", err)
		}

		rendered := rec.String() + "|" + rotated.String()
		for _, e := range m.Evidence(rec.ID()) {
			rendered += "|" + e.ID + "|" + e.Reason
		}
		if strings.Contains(rendered, string(token)) {
			t.Error("a rendering leaks the original refresh token")
		}
		if strings.Contains(rendered, string(second)) {
			t.Error("a rendering leaks the rotated refresh token")
		}
	})

	t.Run("revoking an already-revoked session fails closed, not idempotently silent", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		rec, _ := mustCreate(t, m, validSpec())
		if _, err := m.Revoke(ctx, rec.ID(), ""); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		if _, err := m.Revoke(ctx, rec.ID(), ""); !errors.Is(err, session.ErrSessionNotActive) {
			t.Fatalf("second Revoke = %v, want ErrSessionNotActive", err)
		}
	})

	t.Run("Refresh never panics on adversarial input", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		for _, tok := range []session.RefreshToken{
			"", "not-hex-at-all", session.RefreshToken(strings.Repeat("A", 10000)), "\x00\x01\x02",
		} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Refresh(%q) panicked: %v", tok, r)
					}
				}()
				if _, _, err := m.Refresh(ctx, tok); err == nil {
					t.Errorf("Refresh(%q) succeeded, want a failure", tok)
				}
			}()
		}
	})

	t.Run("concurrent refresh of the same token allows exactly one winner", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		rec, token := mustCreate(t, m, validSpec())

		const attempts = 20
		var wg sync.WaitGroup
		var mu sync.Mutex
		successes := 0
		wg.Add(attempts)
		for i := 0; i < attempts; i++ {
			go func() {
				defer wg.Done()
				if _, _, err := m.Refresh(ctx, token); err == nil {
					mu.Lock()
					successes++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if successes != 1 {
			t.Fatalf("concurrent refresh of one token succeeded %d times, want exactly 1", successes)
		}
		got, err := m.Get(ctx, rec.ID())
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status() != session.StatusRevoked {
			t.Fatalf("Status after concurrent replay storm = %v, want Revoked", got.Status())
		}
	})
}
