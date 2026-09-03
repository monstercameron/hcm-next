package tenant_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/tenant"
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
