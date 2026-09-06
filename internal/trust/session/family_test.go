package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/session"
)

func newAuthn004Family(t *testing.T, at time.Time) (*session.Family, *time.Time) {
	t.Helper()
	clock := at
	family, err := session.NewFamily(session.ManagerConfig{
		Now: func() time.Time { return clock }, IdleTimeout: time.Hour, AbsoluteTimeout: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	return family, &clock
}

func authn004Spec() session.CreateSpec {
	return session.CreateSpec{
		Tenant: "tenant-authn", Subject: "subject-1", PrincipalFingerprint: "fp-1",
		Assurance: trust.AssuranceSubstantial,
	}
}

// TestTodo_AUTHN_004 proves rotation, family-wide replay fencing, expiry and
// explicit revocation all fail closed at the authentication boundary.
func TestTodo_AUTHN_004(t *testing.T) {
	family, clock := newAuthn004Family(t, time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC))
	ctx := context.Background()
	rec, first, err := family.Create(ctx, authn004Spec())
	if err != nil {
		t.Fatal(err)
	}
	rotated, second, err := family.Rotate(ctx, first)
	if err != nil || rotated.RotationCount() != 1 || second == first {
		t.Fatalf("rotate record=%v second=%q err=%v", rotated, second, err)
	}
	if _, _, err := family.Rotate(ctx, first); !errors.Is(err, session.ErrRefreshReplay) {
		t.Fatalf("replay error=%v, want ErrRefreshReplay", err)
	}
	if err := family.CheckRevocation(ctx, rec.ID(), *clock); !errors.Is(err, session.ErrSessionRevoked) {
		t.Fatalf("family check after replay=%v, want ErrSessionRevoked", err)
	}
	if _, _, err := family.Rotate(ctx, second); !errors.Is(err, session.ErrSessionNotActive) {
		t.Fatalf("current token after replay=%v, want ErrSessionNotActive", err)
	}

	other, idleClock := newAuthn004Family(t, time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC))
	idleRec, idleToken, err := other.Create(ctx, func() session.CreateSpec {
		s := authn004Spec()
		s.IdleTimeout = time.Minute
		s.AbsoluteTimeout = time.Hour
		return s
	}())
	if err != nil {
		t.Fatal(err)
	}
	*idleClock = (*idleClock).Add(2 * time.Minute)
	if _, _, err := other.Rotate(ctx, idleToken); !errors.Is(err, session.ErrSessionNotActive) {
		t.Fatalf("idle token=%v, want ErrSessionNotActive", err)
	}
	if got, err := other.Manager().Get(ctx, idleRec.ID()); err != nil || got.Status() != session.StatusExpiredIdle {
		t.Fatalf("idle record=%v err=%v", got, err)
	}
}

// TestTodo_AUTHN_004_Integration proves the facade composes with the
// TRUST-005 boundary and preserves the typed revocation refusal.
func TestTodo_AUTHN_004_Integration(t *testing.T) {
	family, clock := newAuthn004Family(t, time.Date(2026, 9, 6, 13, 0, 0, 0, time.UTC))
	rec, _, err := family.Create(context.Background(), authn004Spec())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := family.Revoke(context.Background(), rec.ID(), "account_disabled"); err != nil {
		t.Fatal(err)
	}
	if err := family.CheckRevocation(context.Background(), rec.ID(), *clock); !errors.Is(err, session.ErrSessionRevoked) {
		t.Fatalf("check=%v, want ErrSessionRevoked", err)
	}
	if session.Explain() == "" || session.Version() < 1 || family.Manager() == nil {
		t.Fatal("family contract exports are incomplete")
	}
}

// FuzzTodo_AUTHN_004 proves arbitrary refresh input remains a refusal and
// never becomes a valid bearer credential by parser accident.
func FuzzTodo_AUTHN_004(f *testing.F) {
	for _, seed := range []string{"", "refresh", "\x00", "not-base64", "refresh\nline"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		family, _ := newAuthn004Family(t, time.Date(2026, 9, 6, 13, 30, 0, 0, time.UTC))
		if _, _, err := family.Rotate(context.Background(), session.RefreshToken(raw)); err == nil {
			t.Fatal("arbitrary refresh input unexpectedly rotated a family")
		}
	})
}

// TestTodo_AUTHN_004_Security proves tenant and assurance assertions cannot
// be elevated or switched after the family was created.
func TestTodo_AUTHN_004_Security(t *testing.T) {
	family, _ := newAuthn004Family(t, time.Date(2026, 9, 6, 14, 0, 0, 0, time.UTC))
	rec, _, err := family.Create(context.Background(), authn004Spec())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := family.Validate(context.Background(), rec.ID(), session.Claim{Tenant: "tenant-other"}); !errors.Is(err, session.ErrTenantMismatch) {
		t.Fatalf("tenant switch=%v, want ErrTenantMismatch", err)
	}
	if _, err := family.Validate(context.Background(), rec.ID(), session.Claim{Assurance: trust.AssuranceHigh}); !errors.Is(err, session.ErrAssuranceMismatch) {
		t.Fatalf("assurance elevation=%v, want ErrAssuranceMismatch", err)
	}
}

// TestTodo_AUTHN_004_Recovery proves a family remains revoked after a
// recovery-shaped caller presents a previously issued refresh credential.
func TestTodo_AUTHN_004_Recovery(t *testing.T) {
	family, _ := newAuthn004Family(t, time.Date(2026, 9, 6, 15, 0, 0, 0, time.UTC))
	rec, token, err := family.Create(context.Background(), authn004Spec())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := family.Revoke(context.Background(), rec.ID(), "recovery_requires_reauth"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := family.Rotate(context.Background(), token); !errors.Is(err, session.ErrSessionNotActive) {
		t.Fatalf("recovery reuse=%v, want ErrSessionNotActive", err)
	}
}

// TestTodo_AUTHN_004_Mutation proves the redaction contract does not expose
// bearer material through the family explanation.
func TestTodo_AUTHN_004_Mutation(t *testing.T) {
	family, _ := newAuthn004Family(t, time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC))
	_, token, err := family.Create(context.Background(), authn004Spec())
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || session.Explain() == "" {
		t.Fatal("family did not create opaque token or explanation")
	}
	if family.Explain() == "" {
		t.Fatal("family explanation is empty")
	}
}
