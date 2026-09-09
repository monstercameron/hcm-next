package dsr

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// --- shared test fixtures --------------------------------------------------
//
// These build one consistent PRIV-005 intake/verification scenario (worker
// "alex.example@example.com" filing an ACCESS request against tenant
// "tenant-acme", under US-CA jurisdiction) that every matrix test in this
// package starts from, then perturbs one thing at a time.

func mustInstant(t *testing.T, unixSec int64) values.Instant {
	t.Helper()
	i, err := values.NewInstantFromUnix(unixSec, 0)
	if err != nil {
		t.Fatalf("NewInstantFromUnix(%d): %v", unixSec, err)
	}
	return i
}

const (
	fxTenant      = values.TenantId("tenant-acme")
	fxOtherTenant = values.TenantId("tenant-globex")
	fxReceivedAt  = 1_800_000_000
	fxVerifiedAt  = 1_800_000_100
	fxWindow      = 30 * 24 * time.Hour
)

func fxJurisdiction() legal.Jurisdiction {
	return legal.Jurisdiction{Country: "US", State: "CA"}
}

func fxClaims() SubjectClaims {
	return SubjectClaims{
		FullName: "Alex Example",
		Email:    "alex.example@example.com",
	}
}

func fixtureIntakeSpec(t *testing.T, id string, kind Kind) IntakeSpec {
	t.Helper()
	return IntakeSpec{
		ID:           id,
		Tenant:       fxTenant,
		Kind:         kind,
		Claims:       fxClaims(),
		Jurisdiction: fxJurisdiction(),
		ReceivedAt:   mustInstant(t, fxReceivedAt),
		Channel:      ChannelPortal,
	}
}

func fixtureRequest(t *testing.T) DataSubjectRequest {
	t.Helper()
	req, err := Intake(fixtureIntakeSpec(t, "dsr-1", KindAccess), DefaultClockTable(), nil, fxWindow)
	if err != nil {
		t.Fatalf("Intake: %v", err)
	}
	return req
}

func fixtureEvidence(t *testing.T, assurance trust.Assurance) IdentityEvidence {
	t.Helper()
	return IdentityEvidence{
		Ref:        "ev:trust:federation:principal-1",
		Tenant:     fxTenant,
		Assurance:  assurance,
		VerifiedAt: mustInstant(t, fxVerifiedAt),
	}
}
