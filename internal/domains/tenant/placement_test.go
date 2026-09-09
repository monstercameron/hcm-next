package tenant_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant"
)

var placementKey = [32]byte{1, 2, 3, 4, 5}

func validPlacement() tenant.Placement {
	return tenant.Placement{
		Tenant: "tenant-a", Cell: "cell-east-1", Region: "us-east",
		ResidencyProfile: "us-only", IsolationTier: "dedicated", Epoch: 7,
	}
}

// TestTodo_TENANT_001 is the PRIMARY contract: all material placement
// dimensions round-trip through one deterministic signed value.
func TestTodo_TENANT_001(t *testing.T) {
	p, err := tenant.Sign(validPlacement(), placementKey)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := tenant.Verify(p, placementKey); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if p.Signature == "" {
		t.Fatal("signed placement has no signature")
	}
	digest, err := p.Digest()
	if err != nil || len(digest) != 64 {
		t.Fatalf("Digest = %q, %v; want SHA-256 hex", digest, err)
	}
	copyOf, err := tenant.Sign(validPlacement(), placementKey)
	if err != nil || copyOf.Signature != p.Signature {
		t.Fatalf("signing is not deterministic: %q != %q (err %v)", copyOf.Signature, p.Signature, err)
	}
	if err := tenant.CheckContext(p, copyOf, placementKey); err != nil {
		t.Fatalf("matching context rejected: %v", err)
	}
}

// TestTodo_TENANT_001_Security covers tampering, wrong authority and stale
// fencing epochs. Every mismatch is rejected before a caller can use it.
func TestTodo_TENANT_001_Security(t *testing.T) {
	a, err := tenant.Sign(validPlacement(), placementKey)
	if err != nil {
		t.Fatal(err)
	}
	wrongKey := [32]byte{9}
	if err := tenant.Verify(a, wrongKey); !errors.Is(err, tenant.ErrInvalidSignature) {
		t.Fatalf("wrong key error = %v, want ErrInvalidSignature", err)
	}
	for name, mutate := range map[string]func(*tenant.Placement){
		"tenant":    func(p *tenant.Placement) { p.Tenant = "tenant-b" },
		"cell":      func(p *tenant.Placement) { p.Cell = "cell-west-1" },
		"region":    func(p *tenant.Placement) { p.Region = "eu-west" },
		"residency": func(p *tenant.Placement) { p.ResidencyProfile = "eu-only" },
		"isolation": func(p *tenant.Placement) { p.IsolationTier = "shared" },
		"epoch":     func(p *tenant.Placement) { p.Epoch++ },
	} {
		t.Run(name, func(t *testing.T) {
			incoming := a
			mutate(&incoming)
			// A changed signed field cannot be accepted, even when the
			// placement dimensions would otherwise look plausible.
			if err := tenant.CheckContext(a, incoming, placementKey); !errors.Is(err, tenant.ErrInvalidSignature) {
				t.Fatalf("tampered context error = %v, want ErrInvalidSignature", err)
			}
			fresh, err := tenant.Sign(incoming, placementKey)
			if err != nil {
				t.Fatal(err)
			}
			if err := tenant.CheckContext(a, fresh, placementKey); !errors.Is(err, tenant.ErrPlacementMismatch) {
				t.Fatalf("authorized mismatch error = %v, want ErrPlacementMismatch", err)
			}
		})
	}
}

func TestPlacementValidation(t *testing.T) {
	bad := validPlacement()
	bad.Tenant = ""
	if _, err := tenant.Sign(bad, placementKey); !errors.Is(err, tenant.ErrInvalidPlacement) {
		t.Fatalf("missing tenant error = %v, want ErrInvalidPlacement", err)
	}
	bad = validPlacement()
	bad.Epoch = 0
	if _, err := tenant.Sign(bad, placementKey); !errors.Is(err, tenant.ErrInvalidPlacement) {
		t.Fatalf("zero epoch error = %v, want ErrInvalidPlacement", err)
	}
	if _, err := bad.Digest(); !errors.Is(err, tenant.ErrInvalidPlacement) {
		t.Fatalf("invalid placement digest error = %v, want ErrInvalidPlacement", err)
	}
}

func TestPlacementSignatureAndDigestAreIndependent(t *testing.T) {
	p, err := tenant.Sign(validPlacement(), placementKey)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := p.Digest()
	if err != nil {
		t.Fatal(err)
	}
	// The signature authenticates the canonical placement, while the digest
	// identifies that placement. Neither identity may change merely because
	// the other representation is carried alongside it.
	p.Signature = "00"
	got, err := p.Digest()
	if err != nil || got != digest {
		t.Fatalf("digest changed with signature: got %q (err %v), want %q", got, err, digest)
	}
	if err := tenant.Verify(p, placementKey); !errors.Is(err, tenant.ErrInvalidSignature) {
		t.Fatalf("tampered signature error = %v, want ErrInvalidSignature", err)
	}
}

func TestPlacement_ValidateRejectsEachRequiredField(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*tenant.Placement)
		field  string
	}{
		{"tenant", func(p *tenant.Placement) { p.Tenant = "" }, "tenant"},
		{"cell", func(p *tenant.Placement) { p.Cell = "" }, "cell"},
		{"region", func(p *tenant.Placement) { p.Region = "" }, "region"},
		{"residency", func(p *tenant.Placement) { p.ResidencyProfile = "" }, "residency_profile"},
		{"isolation", func(p *tenant.Placement) { p.IsolationTier = "" }, "isolation_tier"},
		{"epoch", func(p *tenant.Placement) { p.Epoch = 0 }, "epoch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validPlacement()
			tt.mutate(&p)
			err := p.Validate()
			if !errors.Is(err, tenant.ErrInvalidPlacement) || !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("Validate error = %v, want ErrInvalidPlacement mentioning %q", err, tt.field)
			}
			if _, err := tenant.Sign(p, placementKey); !errors.Is(err, tenant.ErrInvalidPlacement) {
				t.Fatalf("Sign error = %v, want ErrInvalidPlacement", err)
			}
		})
	}
}

func TestPlacement_VerifyRejectsMissingMalformedAndWrongLengthSignatures(t *testing.T) {
	p := validPlacement()
	for _, signature := range []string{"", "not-hex", "00"} {
		t.Run(signature, func(t *testing.T) {
			candidate := p
			candidate.Signature = signature
			if err := tenant.Verify(candidate, placementKey); !errors.Is(err, tenant.ErrInvalidSignature) {
				t.Fatalf("Verify(%q) = %v, want ErrInvalidSignature", signature, err)
			}
		})
	}
}

func TestPlacement_MatchesRejectsEveryContextDifference(t *testing.T) {
	a := validPlacement()
	for _, tt := range []struct {
		name   string
		mutate func(*tenant.Placement)
	}{
		{"tenant", func(p *tenant.Placement) { p.Tenant = "tenant-b" }},
		{"cell", func(p *tenant.Placement) { p.Cell = "cell-west" }},
		{"region", func(p *tenant.Placement) { p.Region = "eu-west" }},
		{"residency", func(p *tenant.Placement) { p.ResidencyProfile = "eu-only" }},
		{"isolation", func(p *tenant.Placement) { p.IsolationTier = "shared" }},
		{"epoch", func(p *tenant.Placement) { p.Epoch++ }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := a
			tt.mutate(&b)
			if a.Matches(b) || b.Matches(a) {
				t.Fatalf("Matches treated %s difference as equal: a=%+v b=%+v", tt.name, a, b)
			}
		})
	}
}

func TestPlacement_CheckContextRejectsInvalidAuthoritativePlacement(t *testing.T) {
	a, err := tenant.Sign(validPlacement(), placementKey)
	if err != nil {
		t.Fatal(err)
	}
	a.Signature = "bad"
	b, err := tenant.Sign(validPlacement(), placementKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenant.CheckContext(a, b, placementKey); !errors.Is(err, tenant.ErrInvalidSignature) {
		t.Fatalf("CheckContext = %v, want ErrInvalidSignature", err)
	}
}
