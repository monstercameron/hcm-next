package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

// TestTodo_TRUST_003_Recovery is the TRUST-003 recovery test. TRUST-003's
// GREEN requires an "auditable reauthentication route" once a session is
// denied: a caller must be able to tell exactly why a session stopped being
// usable (idle timeout, absolute timeout, replay, manual revoke) from
// durable evidence alone, and opening a brand-new session must never be
// blocked or contaminated by a prior session's revocation, expiry, or
// retired-token history.
func TestTodo_TRUST_003_Recovery(t *testing.T) {
	ctx := context.Background()

	t.Run("a fresh session after replay-revocation is fully independent", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		oldRec, oldToken := mustCreate(t, m, validSpec())

		_, _, err := m.Refresh(ctx, oldToken)
		if err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		if _, _, err := m.Refresh(ctx, oldToken); !errors.Is(err, session.ErrRefreshReplay) {
			t.Fatalf("Refresh(replay) = %v, want ErrRefreshReplay", err)
		}

		newRec, newToken := mustCreate(t, m, validSpec())
		if newRec.ID() == oldRec.ID() {
			t.Fatal("a fresh session reused the revoked session's identifier")
		}
		if _, err := m.Get(ctx, newRec.ID()); err != nil {
			t.Fatalf("Get(new session): %v", err)
		}
		rotated, _, err := m.Refresh(ctx, newToken)
		if err != nil {
			t.Fatalf("Refresh(new session) = %v, want success", err)
		}
		if rotated.Status() != session.StatusActive {
			t.Fatalf("new session status = %v, want Active", rotated.Status())
		}
	})

	t.Run("the denial reason is recoverable from evidence for every terminal path", func(t *testing.T) {
		cases := []struct {
			name   string
			build  func(t *testing.T, c *clock, m *session.Manager) session.ID
			reason string
		}{
			{
				name: "idle timeout",
				build: func(t *testing.T, c *clock, m *session.Manager) session.ID {
					rec, _ := mustCreate(t, m, validSpec())
					c.advance(2 * time.Minute)
					_, _ = m.Get(ctx, rec.ID())
					return rec.ID()
				},
				reason: session.ReasonIdleTimeout,
			},
			{
				name: "manual revoke",
				build: func(t *testing.T, c *clock, m *session.Manager) session.ID {
					rec, _ := mustCreate(t, m, validSpec())
					if _, err := m.Revoke(ctx, rec.ID(), ""); err != nil {
						t.Fatalf("Revoke: %v", err)
					}
					return rec.ID()
				},
				reason: session.ReasonManualRevoke,
			},
			{
				name: "refresh replay",
				build: func(t *testing.T, c *clock, m *session.Manager) session.ID {
					rec, token := mustCreate(t, m, validSpec())
					if _, _, err := m.Refresh(ctx, token); err != nil {
						t.Fatalf("Refresh: %v", err)
					}
					if _, _, err := m.Refresh(ctx, token); err == nil {
						t.Fatal("expected replay to fail")
					}
					return rec.ID()
				},
				reason: session.ReasonRefreshReplay,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				c := &clock{at: baseTime}
				m := newManager(t, c, time.Minute, 24*time.Hour)
				id := tc.build(t, c, m)

				rec, err := m.Get(ctx, id)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				if rec.RevokedReason() != tc.reason {
					t.Fatalf("RevokedReason = %q, want %q", rec.RevokedReason(), tc.reason)
				}

				var found session.Evidence
				for _, e := range m.Evidence(id) {
					if e.Reason == tc.reason {
						found = e
					}
				}
				if found.ID == "" {
					t.Fatalf("no evidence entry recorded reason %q", tc.reason)
				}
			})
		}
	})

	t.Run("Get on an unknown session reports not-found rather than panicking, so a caller can route to reauthentication", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		if _, err := m.Get(ctx, session.ID("sess_does_not_exist")); !errors.Is(err, session.ErrSessionNotFound) {
			t.Fatalf("Get(unknown) = %v, want ErrSessionNotFound", err)
		}
	})
}
