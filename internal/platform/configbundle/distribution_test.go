package configbundle

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant"
	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
)

func cp004Keys(t *testing.T) (ed25519.PrivateKey, *InMemoryKeyring) {
	t.Helper()
	var seed [ed25519.SeedSize]byte
	for i := range seed {
		seed[i] = byte(i + 11)
	}
	private := ed25519.NewKeyFromSeed(seed[:])
	keyring := NewKeyring()
	if err := keyring.Put(BundleKey{Handle: "cp004-signer", Version: "v1", PublicKey: private.Public().(ed25519.PublicKey)}); err != nil {
		t.Fatal(err)
	}
	return private, keyring
}

func cp004PublishedBundle(t *testing.T, scope Scope) (Bundle, *platformconfig.Registry) {
	t.Helper()
	registry := platformconfig.NewRegistry()
	obj, err := platformconfig.Publish(registry, platformconfig.ConfigurationObject{Kind: platformconfig.KindRule, ID: "rule", Revision: 1, Scope: scope, SchemaRef: "rule/v1", PublisherPrincipal: "publisher", PublishedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), Body: []byte(`{"enabled":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := platformconfig.Activate(registry, obj.Ref(), platformconfig.ActivationEvidence{ActivatedBy: "operator", ActivatedAt: time.Date(2026, 9, 5, 12, 1, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	bundle, err := Compile(registry, CompileOptions{BundleID: "cp004-bundle", Roots: []ObjectRef{obj.Ref()}, TargetScope: scope, MinimumRuntimeVersion: "go1.26.3"})
	if err != nil {
		t.Fatal(err)
	}
	return bundle, registry
}

func cp004Placement(scope Scope, epoch uint64) tenant.Placement {
	return tenant.Placement{Tenant: scope.TenantID, Cell: scope.CellID, Region: "us-east", ResidencyProfile: "us", IsolationTier: "standard", Epoch: epoch}
}

// TestTodo_CP_004 proves signed per-cell publication, atomic apply refusal,
// and the bounded offline state machine.
func TestTodo_CP_004(t *testing.T) {
	private, keyring := cp004Keys(t)
	scope := Scope{TenantID: "cp004-tenant", CellID: "cell-a"}
	bundle, registry := cp004PublishedBundle(t, scope)
	placement := cp004Placement(scope, 3)
	clockAt := time.Date(2026, 9, 5, 12, 2, 0, 0, time.UTC)
	distributor, err := NewDistributor("cp004-signer", "v1", private, func() time.Time { return clockAt })
	if err != nil {
		t.Fatal(err)
	}
	record, err := distributor.Publish(bundle, placement, 1)
	if err != nil {
		t.Fatal(err)
	}
	receiver := NewReceiver(keyring, platformconfig.NewRegistry(), ReceiverOptions{Scope: scope, Placement: placement, Freshness: FreshnessPolicy{MaxAge: time.Hour, HardMaxAge: 2 * time.Hour}, Now: func() time.Time { return clockAt }})
	result, err := receiver.Receive(record)
	if !errors.Is(err, ErrUnknownConfig) || result.Decision != ApplyRefused || result.HasSnapshot {
		t.Fatalf("unknown config result=%+v err=%v", result, err)
	}
	if got := receiver.StateAt(clockAt.Add(3 * time.Hour)); got != ServiceBlocked {
		t.Fatalf("empty receiver state=%s, want BLOCKED", got)
	}
	if _, err := receiver.Receive(record, placement); !errors.Is(err, ErrUnknownConfig) {
		t.Fatalf("unknown config with explicit placement=%v", err)
	}
	validReceiver := NewReceiver(keyring, registry, ReceiverOptions{Scope: scope, Placement: placement, Freshness: FreshnessPolicy{MaxAge: time.Hour, HardMaxAge: 2 * time.Hour}, Now: func() time.Time { return clockAt }})
	accepted, err := validReceiver.Receive(record)
	if err != nil || accepted.Decision != ApplyAccepted || accepted.State != ServiceReady || !accepted.HasSnapshot {
		t.Fatalf("valid offline apply result=%+v err=%v", accepted, err)
	}
	if got := validReceiver.StateAt(clockAt.Add(90 * time.Minute)); got != ServiceDegraded {
		t.Fatalf("stale last-known-good state=%s, want DEGRADED", got)
	}
	if got := validReceiver.StateAt(clockAt.Add(3 * time.Hour)); got != ServiceBlocked {
		t.Fatalf("expired last-known-good state=%s, want BLOCKED", got)
	}
}

func TestTodo_CP_004_Golden(t *testing.T) {
	private, keyring := cp004Keys(t)
	aScope := Scope{TenantID: "cp004-tenant", CellID: "cell-a"}
	bundleA, _ := cp004PublishedBundle(t, aScope)
	bScope := Scope{TenantID: aScope.TenantID, CellID: "cell-b"}
	bundleB := bundleA
	bundleB.BundleID = "cp004-cell-b-bundle"
	bundleB.TargetScope, bundleB.Scope = bScope, bScope
	bundleB.Roots = append([]ObjectRef(nil), bundleB.Roots...)
	bundleB.Objects = append([]IncludedObject(nil), bundleB.Objects...)
	for i := range bundleB.Roots {
		bundleB.Roots[i].Scope = bScope
	}
	for i := range bundleB.Objects {
		bundleB.Objects[i].Ref.Scope = bScope
	}
	var digestErr error
	bundleB.Digest, digestErr = BundleDigest(bundleB)
	if digestErr != nil {
		t.Fatalf("cell-b bundle digest: %v", digestErr)
	}
	distributor, err := NewDistributor("cp004-signer", "v1", private, func() time.Time { return time.Date(2026, 9, 5, 12, 2, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	first, err := distributor.Publish(bundleA, cp004Placement(aScope, 3), 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := bundleB.Verify(); err != nil {
		t.Fatalf("cell-b verify after cell-a publish: %v", err)
	}
	second, err := distributor.Publish(bundleB, cp004Placement(bScope, 4), 7)
	if err != nil {
		t.Fatal(err)
	}
	if first.CellID == second.CellID || first.PlacementDigest == second.PlacementDigest || first.Digest == second.Digest {
		t.Fatalf("two-cell desired state collapsed scopes: first=%+v second=%+v", first, second)
	}
	if err := first.Verify(cp004KeyPublic(t, keyring)); err != nil {
		t.Fatalf("cell-a desired state verification: %v", err)
	}
	if first.Explain() == "" || ExplainDistribution() == "" {
		t.Fatal("audit-safe explanation is empty")
	}
}

func TestTodo_CP_004_Security(t *testing.T) {
	private, keyring := cp004Keys(t)
	scope := Scope{TenantID: "cp004-security", CellID: "cell-a"}
	bundle, registry := cp004PublishedBundle(t, scope)
	placement := cp004Placement(scope, 1)
	clockAt := time.Date(2026, 9, 5, 12, 2, 0, 0, time.UTC)
	distributor, err := NewDistributor("cp004-signer", "v1", private, func() time.Time { return clockAt })
	if err != nil {
		t.Fatal(err)
	}
	record, err := distributor.Publish(bundle, placement, 2)
	if err != nil {
		t.Fatal(err)
	}
	receiver := NewReceiver(keyring, registry, ReceiverOptions{Scope: scope, Placement: placement, Freshness: FreshnessPolicy{MaxAge: time.Hour, HardMaxAge: 2 * time.Hour}, Now: func() time.Time { return clockAt }})
	keyring.Revoke("cp004-signer", "v1")
	result, err := receiver.Receive(record)
	if !errors.Is(err, ErrRevokedSigningKey) || result.Decision != ApplyRefused {
		t.Fatalf("revoked-key result=%+v err=%v", result, err)
	}
	if _, ok := receiver.Snapshot(); ok {
		t.Fatal("bundle signed by revoked key applied")
	}

	keyring2 := NewKeyring()
	if err := keyring2.Put(BundleKey{Handle: "cp004-signer", Version: "v1", PublicKey: private.Public().(ed25519.PublicKey)}); err != nil {
		t.Fatal(err)
	}
	receiver2 := NewReceiver(keyring2, registry, ReceiverOptions{Scope: scope, Placement: placement, Freshness: FreshnessPolicy{MaxAge: time.Hour, HardMaxAge: 2 * time.Hour}, Now: func() time.Time { return clockAt }})
	replayed := record
	replayed.Epoch = 1
	replayed.Digest, _ = replayed.DigestValue()
	if _, err := receiver2.Receive(replayed); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("replayed older epoch error=%v, want invalid signature", err)
	}
}

func cp004KeyPublic(t *testing.T, keyring *InMemoryKeyring) ed25519.PublicKey {
	t.Helper()
	key, found, err := keyring.ResolveBundleKey("cp004-signer", "v1")
	if err != nil || !found {
		t.Fatalf("resolve key: found=%t err=%v", found, err)
	}
	return key.PublicKey
}
