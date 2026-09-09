package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

// TestTodo_TRUST_003_Mutation is the TRUST-003 mutation test. It asserts
// that [session.NewManager] and [Manager.Create] fail closed on incomplete
// input, that every rotation actually produces a fresh, distinct token and
// a bumped rotation count, and that evidence identifiers are unique per
// transition -- a mutation that reused an identifier or skipped
// incrementing a counter fails here.
func TestTodo_TRUST_003_Mutation(t *testing.T) {
	ctx := context.Background()

	t.Run("NewManager fails closed when idle exceeds absolute", func(t *testing.T) {
		if _, err := session.NewManager(session.ManagerConfig{IdleTimeout: 2 * time.Hour, AbsoluteTimeout: time.Hour}); err == nil {
			t.Fatal("NewManager(idle > absolute) succeeded, want a failure")
		}
	})

	t.Run("Create fails closed on an incomplete spec", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		for name, mutate := range map[string]func(*session.CreateSpec){
			"no tenant":      func(s *session.CreateSpec) { s.Tenant = "" },
			"no subject":     func(s *session.CreateSpec) { s.Subject = "" },
			"no fingerprint": func(s *session.CreateSpec) { s.PrincipalFingerprint = "" },
			"no assurance":   func(s *session.CreateSpec) { s.Assurance = trust.AssuranceUnspecified },
			"idle > absolute": func(s *session.CreateSpec) {
				s.IdleTimeout = 2 * time.Hour
				s.AbsoluteTimeout = time.Hour
			},
		} {
			spec := validSpec()
			mutate(&spec)
			if _, _, err := m.Create(ctx, spec); err == nil {
				t.Errorf("Create(%s) succeeded, want a failure", name)
			} else if !errors.Is(err, session.ErrInvalidCreateSpec) {
				t.Errorf("Create(%s) = %v, want ErrInvalidCreateSpec", name, err)
			}
		}
	})

	t.Run("every rotation produces a distinct token and increments the counter", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		_, token := mustCreate(t, m, validSpec())

		seen := map[session.RefreshToken]bool{token: true}
		for i := 1; i <= 5; i++ {
			rec, next, err := m.Refresh(ctx, token)
			if err != nil {
				t.Fatalf("Refresh #%d: %v", i, err)
			}
			if seen[next] {
				t.Fatalf("rotation #%d reused a previously issued token", i)
			}
			seen[next] = true
			if rec.RotationCount() != i {
				t.Fatalf("rotation #%d: RotationCount = %d, want %d", i, rec.RotationCount(), i)
			}
			token = next
		}
	})

	t.Run("every evidence entry has a unique id, even across identical transitions", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		rec, token := mustCreate(t, m, validSpec())

		for i := 0; i < 3; i++ {
			_, next, err := m.Refresh(ctx, token)
			if err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			token = next
		}

		seen := map[string]bool{}
		for _, e := range m.Evidence(rec.ID()) {
			if seen[e.ID] {
				t.Fatalf("duplicate evidence id %q", e.ID)
			}
			seen[e.ID] = true
		}
		if len(seen) != 4 { // 1 created + 3 rotated
			t.Fatalf("recorded %d evidence entries, want 4", len(seen))
		}
	})

	t.Run("two sessions created back to back never share an id or a token", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		recA, tokenA := mustCreate(t, m, validSpec())
		recB, tokenB := mustCreate(t, m, validSpec())
		if recA.ID() == recB.ID() {
			t.Fatal("two sessions were assigned the same id")
		}
		if tokenA == tokenB {
			t.Fatal("two sessions were assigned the same refresh token")
		}
	})
}
