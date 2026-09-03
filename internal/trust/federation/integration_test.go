package federation_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/federation"
)

// TestTodo_TRUST_002_Integration is the TRUST-002 integration test. It wires
// one [federation.Validator] to two tenants, each federating with its own
// issuer under its own algorithm, and proves the whole path end to end: a
// validated assertion becomes a [trust.Principal] indistinguishable in kind
// from one the first-party dev verifier produces, it round-trips through
// the frozen package's context helpers, and the two tenants' credentials
// remain fully isolated from one another even though they share one
// [federation.Validator] and one [federation.KeySource].
func TestTodo_TRUST_002_Integration(t *testing.T) {
	keys := newTestKeys(t)
	v, err := federation.NewValidator(federation.Config{
		TenantIssuers: map[values.TenantId][]string{
			tenantAcme:  {issuerAcme},
			tenantOther: {issuerOther},
		},
		Audience: audienceUnder,
		Keys:     keys.source,
		Now:      func() time.Time { return baseTime },
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	ctx := context.Background()

	acmeToken := keys.signRS256(t, validAcmeClaims(), "acme-rsa-1")
	acmePrincipal, err := v.Validate(ctx, acmeToken)
	if err != nil {
		t.Fatalf("Validate(acme): %v", err)
	}
	if acmePrincipal.Tenant() != tenantAcme {
		t.Fatalf("acme principal tenant = %v, want %v", acmePrincipal.Tenant(), tenantAcme)
	}

	otherToken := keys.signEdDSA(t, validOtherClaims(), "other-ed-1")
	otherPrincipal, err := v.Validate(ctx, otherToken)
	if err != nil {
		t.Fatalf("Validate(other): %v", err)
	}
	if otherPrincipal.Tenant() != tenantOther {
		t.Fatalf("other principal tenant = %v, want %v", otherPrincipal.Tenant(), tenantOther)
	}

	if acmePrincipal.Fingerprint() == otherPrincipal.Fingerprint() {
		t.Fatal("two distinct tenants' principals must not collide on fingerprint")
	}
	if acmePrincipal.EvidenceID() == otherPrincipal.EvidenceID() {
		t.Fatal("two distinct tenants' principals must not collide on evidence id")
	}

	// A downstream consumer never sees federation-specific plumbing: only
	// the frozen package's context helpers and accessors.
	ctxWithAcme := trust.WithPrincipal(ctx, acmePrincipal)
	got, err := trust.MustFromContext(ctxWithAcme)
	if err != nil {
		t.Fatalf("MustFromContext: %v", err)
	}
	if got.Subject() != acmePrincipal.Subject() {
		t.Fatalf("context round-trip changed subject: got %q, want %q", got.Subject(), acmePrincipal.Subject())
	}

	// acme's own issuer, correctly signed, still cannot federate for
	// tenant-other: allow-listing is per tenant, not global to the issuer.
	crossToken := keys.signRS256(t, validOtherClaims(), "acme-rsa-1")
	if _, err := v.Validate(ctx, crossToken); err == nil {
		t.Fatal("Validate(acme issuer claiming tenant-other) succeeded, want a failure")
	}

	// A validator scoped to only one tenant never accepts the other
	// tenant's traffic at all, proving TenantIssuers is genuinely scoping
	// behavior and not just informing an error message.
	acmeOnly, err := federation.NewValidator(federation.Config{
		TenantIssuers: map[values.TenantId][]string{tenantAcme: {issuerAcme}},
		Audience:      audienceUnder,
		Keys:          keys.source,
		Now:           func() time.Time { return baseTime },
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	if _, err := acmeOnly.Validate(ctx, otherToken); err == nil {
		t.Fatal("a validator not configured for tenant-other accepted its assertion")
	}
}
