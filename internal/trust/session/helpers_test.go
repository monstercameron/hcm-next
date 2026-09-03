package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/session"
)

const (
	tenantAcme   = values.TenantId("acme-corp")
	tenantOther  = values.TenantId("other-corp")
	subjectA     = "user-a"
	fingerprintA = "fingerprint-a"
)

// baseTime is the fixed instant every test in this package treats as "now".
var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// clock is a mutable injected clock: tests advance it explicitly rather
// than sleeping, so timeout behavior is deterministic.
type clock struct{ at time.Time }

func (c *clock) now() time.Time          { return c.at }
func (c *clock) advance(d time.Duration) { c.at = c.at.Add(d) }

func newManager(t *testing.T, c *clock, idle, absolute time.Duration) *session.Manager {
	t.Helper()
	m, err := session.NewManager(session.ManagerConfig{
		Now:             c.now,
		IdleTimeout:     idle,
		AbsoluteTimeout: absolute,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func validSpec() session.CreateSpec {
	return session.CreateSpec{
		Tenant:               tenantAcme,
		Subject:              subjectA,
		PrincipalFingerprint: fingerprintA,
		Assurance:            trust.AssuranceSubstantial,
	}
}

func mustCreate(t *testing.T, m *session.Manager, spec session.CreateSpec) (session.Record, session.RefreshToken) {
	t.Helper()
	rec, tok, err := m.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return rec, tok
}
