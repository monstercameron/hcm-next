package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

// TestTodo_TRUST_003 is the TRUST-003 primary test.
//
// GREEN: create, rotate, expire and revoke transitions are durable and
// produce the record and evidence a durable session store needs; a
// one-time refresh replay revokes the affected session.
//
// RED: an expired session, a revoked session, a replayed refresh
// credential, a claimed assurance downgrade and a claimed tenant switch are
// all denied and audited. Each is a separate subtest so a regression names
// itself.
func TestTodo_TRUST_003(t *testing.T) {
	ctx := context.Background()

	t.Run("create yields an active session with durable evidence", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		rec, token := mustCreate(t, m, validSpec())

		if rec.Status() != session.StatusActive {
			t.Fatalf("Status = %v, want Active", rec.Status())
		}
		if rec.Tenant() != tenantAcme {
			t.Fatalf("Tenant = %v, want %v", rec.Tenant(), tenantAcme)
		}
		if token == "" {
			t.Fatal("Create returned an empty refresh token")
		}
		if rec.RotationCount() != 0 {
			t.Fatalf("RotationCount = %d, want 0", rec.RotationCount())
		}
		evidence := m.Evidence(rec.ID())
		if len(evidence) != 1 || evidence[0].Kind != session.EvidenceCreated {
			t.Fatalf("Evidence = %+v, want exactly one Created entry", evidence)
		}
	})

	t.Run("rotate: refresh retires the presented token and mints a successor", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		rec, first := mustCreate(t, m, validSpec())

		c.advance(time.Minute)
		rotated, second, err := m.Refresh(ctx, first)
		if err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		if rotated.ID() != rec.ID() {
			t.Fatalf("Refresh rotated session id: got %v, want %v", rotated.ID(), rec.ID())
		}
		if rotated.RotationCount() != 1 {
			t.Fatalf("RotationCount = %d, want 1", rotated.RotationCount())
		}
		if second == first {
			t.Fatal("rotation produced the same token")
		}

		// The retired token no longer refreshes.
		if _, _, err := m.Refresh(ctx, first); !errors.Is(err, session.ErrRefreshReplay) {
			t.Fatalf("Refresh(retired token) = %v, want ErrRefreshReplay", err)
		}
	})

	t.Run("GREEN: one-time refresh replay revokes the session", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		rec, first := mustCreate(t, m, validSpec())

		_, second, err := m.Refresh(ctx, first)
		if err != nil {
			t.Fatalf("Refresh: %v", err)
		}

		// Replay the already-retired first token.
		if _, _, err := m.Refresh(ctx, first); !errors.Is(err, session.ErrRefreshReplay) {
			t.Fatalf("Refresh(replay) = %v, want ErrRefreshReplay", err)
		}

		got, err := m.Get(ctx, rec.ID())
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status() != session.StatusRevoked {
			t.Fatalf("Status after replay = %v, want Revoked", got.Status())
		}
		if got.RevokedReason() != session.ReasonRefreshReplay {
			t.Fatalf("RevokedReason = %q, want %q", got.RevokedReason(), session.ReasonRefreshReplay)
		}

		// The current (second, never-used) token is also dead: replay
		// revokes the whole chain, not just the specific replayed link.
		if _, _, err := m.Refresh(ctx, second); err == nil {
			t.Fatal("Refresh(second token after family revocation) succeeded, want a failure")
		}
	})

	t.Run("RED: expired-idle session is denied", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Minute, 24*time.Hour)
		rec, token := mustCreate(t, m, validSpec())

		c.advance(2 * time.Minute)
		if _, err := m.Get(ctx, rec.ID()); err != nil {
			t.Fatalf("Get: %v", err)
		}
		got, _ := m.Get(ctx, rec.ID())
		if got.Status() != session.StatusExpiredIdle {
			t.Fatalf("Status = %v, want ExpiredIdle", got.Status())
		}
		if _, _, err := m.Refresh(ctx, token); !errors.Is(err, session.ErrSessionNotActive) {
			t.Fatalf("Refresh(idle-expired) = %v, want ErrSessionNotActive", err)
		}
	})

	t.Run("RED: expired-absolute session is denied even with recent activity", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 90*time.Minute)
		rec, token := mustCreate(t, m, validSpec())

		c.advance(30 * time.Minute)
		if _, err := m.Touch(ctx, rec.ID()); err != nil {
			t.Fatalf("Touch: %v", err)
		}
		c.advance(65 * time.Minute) // total 95m > 90m absolute, only 65m since last touch (< 1h idle)
		got, err := m.Get(ctx, rec.ID())
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status() != session.StatusExpiredAbsolute {
			t.Fatalf("Status = %v, want ExpiredAbsolute", got.Status())
		}
		if _, _, err := m.Refresh(ctx, token); !errors.Is(err, session.ErrSessionNotActive) {
			t.Fatalf("Refresh(absolute-expired) = %v, want ErrSessionNotActive", err)
		}
	})

	t.Run("RED: revoked session cannot approve or refresh", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		rec, token := mustCreate(t, m, validSpec())

		if _, err := m.Revoke(ctx, rec.ID(), ""); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		got, _ := m.Get(ctx, rec.ID())
		if got.Status() != session.StatusRevoked || got.RevokedReason() != session.ReasonManualRevoke {
			t.Fatalf("got status=%v reason=%q, want Revoked/%q", got.Status(), got.RevokedReason(), session.ReasonManualRevoke)
		}
		if _, err := m.Validate(ctx, rec.ID(), session.Claim{}); !errors.Is(err, session.ErrSessionNotActive) {
			t.Fatalf("Validate(revoked) = %v, want ErrSessionNotActive", err)
		}
		if _, _, err := m.Refresh(ctx, token); !errors.Is(err, session.ErrSessionNotActive) {
			t.Fatalf("Refresh(revoked) = %v, want ErrSessionNotActive", err)
		}
	})

	t.Run("RED: a claimed tenant switch is denied and audited", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		rec, _ := mustCreate(t, m, validSpec())

		_, err := m.Validate(ctx, rec.ID(), session.Claim{Tenant: tenantOther})
		if !errors.Is(err, session.ErrTenantMismatch) {
			t.Fatalf("Validate(tenant switch) = %v, want ErrTenantMismatch", err)
		}
		found := false
		for _, e := range m.Evidence(rec.ID()) {
			if e.Kind == session.EvidenceDenied && e.Reason == session.ReasonTenantMismatch {
				found = true
			}
		}
		if !found {
			t.Error("no audited denial evidence for the tenant switch")
		}
	})

	t.Run("RED: a claimed assurance downgrade is denied and audited", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		rec, _ := mustCreate(t, m, validSpec()) // created at AssuranceSubstantial

		_, err := m.Validate(ctx, rec.ID(), session.Claim{Assurance: trust.AssuranceLow})
		if !errors.Is(err, session.ErrAssuranceMismatch) {
			t.Fatalf("Validate(assurance downgrade) = %v, want ErrAssuranceMismatch", err)
		}
		found := false
		for _, e := range m.Evidence(rec.ID()) {
			if e.Kind == session.EvidenceDenied && e.Reason == session.ReasonAssuranceMismatch {
				found = true
			}
		}
		if !found {
			t.Error("no audited denial evidence for the assurance mismatch")
		}
	})

	t.Run("a matching claim validates and extends the idle window", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		rec, _ := mustCreate(t, m, validSpec())

		got, err := m.Validate(ctx, rec.ID(), session.Claim{Tenant: tenantAcme, Assurance: trust.AssuranceSubstantial})
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if got.Status() != session.StatusActive {
			t.Fatalf("Status = %v, want Active", got.Status())
		}
	})

	t.Run("Refresh on an unknown token is denied without touching any session", func(t *testing.T) {
		c := &clock{at: baseTime}
		m := newManager(t, c, time.Hour, 24*time.Hour)
		if _, _, err := m.Refresh(ctx, "not-a-real-token"); !errors.Is(err, session.ErrRefreshUnknown) {
			t.Fatalf("Refresh(unknown) = %v, want ErrRefreshUnknown", err)
		}
	})
}

func TestManager_BoundariesAndErrorState(t *testing.T) {
	ctx := context.Background()
	c := &clock{at: baseTime}
	m, err := session.NewManager(session.ManagerConfig{Now: c.now, IdleTimeout: time.Minute, AbsoluteTimeout: 2 * time.Minute})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, err := m.Get(ctx, "missing"); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("Get missing = %v, want ErrSessionNotFound", err)
	}
	if _, err := m.Touch(ctx, "missing"); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("Touch missing = %v, want ErrSessionNotFound", err)
	}
	if _, err := m.Validate(ctx, "missing", session.Claim{}); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("Validate missing = %v, want ErrSessionNotFound", err)
	}
	if _, err := m.Revoke(ctx, "missing", "reason"); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("Revoke missing = %v, want ErrSessionNotFound", err)
	}

	spec := validSpec()
	spec.IdleTimeout = time.Minute
	spec.AbsoluteTimeout = 2 * time.Minute
	rec, token, err := m.Create(ctx, spec)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, _, err := m.Refresh(ctx, "unknown"); !errors.Is(err, session.ErrRefreshUnknown) {
		t.Fatalf("Refresh unknown = %v, want ErrRefreshUnknown", err)
	}
	c.advance(time.Minute)
	got, err := m.Get(ctx, rec.ID())
	if err != nil || got.Status() != session.StatusActive {
		t.Fatalf("Get at exact idle boundary = %+v, err=%v, want active", got, err)
	}
	touched, err := m.Touch(ctx, rec.ID())
	if err != nil || !touched.LastActivityAt().Equal(c.at) {
		t.Fatalf("Touch at boundary = %+v, err=%v", touched, err)
	}
	if _, err := m.Validate(ctx, rec.ID(), session.Claim{}); err != nil {
		t.Fatalf("Validate empty claim: %v", err)
	}

	if _, err := m.Revoke(ctx, rec.ID(), "operator"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := m.Revoke(ctx, rec.ID(), "again"); !errors.Is(err, session.ErrSessionNotActive) {
		t.Fatalf("second Revoke = %v, want ErrSessionNotActive", err)
	}
	if _, err := m.Touch(ctx, rec.ID()); !errors.Is(err, session.ErrSessionNotActive) {
		t.Fatalf("Touch revoked = %v, want ErrSessionNotActive", err)
	}
	if _, err := m.Validate(ctx, rec.ID(), session.Claim{}); !errors.Is(err, session.ErrSessionNotActive) {
		t.Fatalf("Validate revoked = %v, want ErrSessionNotActive", err)
	}
	if _, _, err := m.Refresh(ctx, token); !errors.Is(err, session.ErrSessionNotActive) {
		t.Fatalf("Refresh revoked current token = %v, want ErrSessionNotActive", err)
	}
}

func TestManager_AbsoluteExpiryWinsAtDeadlineAndEvidenceIsStable(t *testing.T) {
	ctx := context.Background()
	c := &clock{at: baseTime}
	m := newManager(t, c, 10*time.Second, time.Minute)
	rec, _ := mustCreate(t, m, validSpec())
	c.advance(time.Minute)
	got, err := m.Get(ctx, rec.ID())
	if err != nil || got.Status() != session.StatusExpiredAbsolute || got.RevokedReason() != session.ReasonAbsoluteTimeout {
		t.Fatalf("absolute boundary = %+v, err=%v", got, err)
	}
	first := m.Evidence(rec.ID())
	if len(first) != 2 || first[1].Kind != session.EvidenceExpired || first[1].Reason != session.ReasonAbsoluteTimeout {
		t.Fatalf("absolute evidence = %+v, want one expiry entry", first)
	}
	if _, err := m.Get(ctx, rec.ID()); err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if got := m.Evidence(rec.ID()); len(got) != len(first) {
		t.Fatalf("expiry was recorded %d times, want once", len(got)-1)
	}
}
