package promotion

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/platform/config"
)

func testPackage(t *testing.T, id, env string) Package {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	b := config.Bundle{BundleID: id, ManifestVersion: 1, CompatibilityRange: "^1", Signer: "release", Provenance: "test",
		Dependencies: []config.Dependency{{Kind: config.DependencySchema, Name: "worker", Version: "1.0.0", Digest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}}
	sb, err := config.SignBundle(b, "release", priv)
	if err != nil {
		t.Fatal(err)
	}
	return Package{ID: id, Environment: env, Bundle: sb, PublicKey: pub}
}

func TestTodo_CONFIG_003(t *testing.T) {
	p := testPackage(t, "pkg-1", "production")
	v, err := Validate(p)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Simulate(p, v)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Approve(p, v, s, "operator", time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != v.Digest || !s.Passed {
		t.Fatal("promotion records are not digest-bound")
	}
	r := NewRegistry()
	if err := r.Put(p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Simulate(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Approve(p.ID, "operator", time.Unix(11, 0)); err != nil {
		t.Fatal(err)
	}
	active, err := r.Activate(p.ID, time.Unix(12, 0))
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != StatusActive || active.RollbackTo != "" {
		t.Fatalf("active = %#v", active)
	}
}

func TestTodo_CONFIG_003_MutationAndStale(t *testing.T) {
	p := testPackage(t, "pkg-2", "production")
	v, _ := Validate(p)
	s, _ := Simulate(p, v)
	s.Digest = "changed"
	if _, err := Approve(p, v, s, "operator", time.Unix(1, 0)); !errors.Is(err, ErrStale) {
		t.Fatalf("stale simulation error = %v", err)
	}
	r := NewRegistry()
	if err := r.Put(p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Activate(p.ID, time.Unix(1, 0)); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("unapproved activation = %v", err)
	}
}

func TestTodo_CONFIG_003_RollbackTargetAndPins(t *testing.T) {
	r := NewRegistry()
	first := testPackage(t, "pkg-a", "prod")
	second := testPackage(t, "pkg-b", "prod")
	for _, p := range []Package{first, second} {
		if err := r.Put(p); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Simulate(p.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Approve(p.ID, "operator", time.Unix(2, 0)); err != nil {
			t.Fatal(err)
		}
	}
	one, err := r.Activate(first.ID, time.Unix(3, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Activate(second.ID, time.Unix(4, 0)); err != nil {
		t.Fatal(err)
	}
	if one.Status != StatusActive {
		t.Fatal("returned record was not active")
	}
	old, _ := r.Get(first.ID)
	if old.Status != StatusActive {
		t.Fatal("history mutated on replacement")
	}
	current, _ := r.Active("prod")
	if current.RollbackTo != first.ID {
		t.Fatalf("rollback target = %q", current.RollbackTo)
	}
	reverted, err := r.Rollback("prod", time.Unix(5, 0))
	if err != nil {
		t.Fatal(err)
	}
	if reverted.Package.ID != first.ID || !reverted.Evidence.Rollback || reverted.Evidence.PreviousPackageID != second.ID {
		t.Fatalf("rollback activation = %+v", reverted)
	}
	original, _ := r.Get(first.ID)
	if !original.ActivatedAt.Equal(time.Unix(3, 0).UTC()) || original.Evidence.Rollback {
		t.Fatalf("rollback rewrote original activation evidence: %+v", original)
	}
}
