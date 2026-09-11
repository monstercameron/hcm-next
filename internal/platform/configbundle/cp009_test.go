package configbundle

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"

	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
)

func cp009Setup(t *testing.T, scope Scope, now time.Time) (*Activator, *InMemoryKeyring, ed25519.PrivateKey, ed25519.PublicKey, SignedBundle, SignedBundle) {
	t.Helper()
	bundlePrivate, bundlePublic, receiptPrivate, receiptPublic := cp003Keys(t)
	keyring := NewKeyring()
	if err := keyring.Put(BundleKey{Handle: "bundle-signer", Version: "v1", PublicKey: bundlePublic, TrustProfile: "control-plane"}); err != nil {
		t.Fatal(err)
	}
	registry := platformconfig.NewRegistry()
	rev1 := publishObject(t, registry, scope, KindRule, "pick-rule", 1, `{"limit":10}`)
	bundleA, err := Compile(registry, CompileOptions{BundleID: "cp009-bundle", Roots: []ObjectRef{rev1.Ref()}, TargetScope: scope, MinimumRuntimeVersion: "go1.26.3"})
	if err != nil {
		t.Fatal(err)
	}
	rev2 := publishObject(t, registry, scope, KindRule, "pick-rule", 2, `{"limit":99}`)
	bundleB, err := Compile(registry, CompileOptions{BundleID: "cp009-bundle", Roots: []ObjectRef{rev2.Ref()}, TargetScope: scope, MinimumRuntimeVersion: "go1.26.3"})
	if err != nil {
		t.Fatal(err)
	}
	signedA, err := SignBundle(bundleA, "bundle-signer", "v1", bundlePrivate)
	if err != nil {
		t.Fatal(err)
	}
	signedB, err := SignBundle(bundleB, "bundle-signer", "v1", bundlePrivate)
	if err != nil {
		t.Fatal(err)
	}
	receiptSigner, err := NewEd25519ReceiptSigner("receipt-signer", "v1", receiptPrivate)
	if err != nil {
		t.Fatal(err)
	}
	policy := ActivationPolicy{Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane", AntiRollbackFloor: "go1.26.0"}
	activator := NewActivator(keyring, policy, receiptSigner, func() time.Time { return now })
	_ = bundlePublic
	return activator, keyring, bundlePrivate, receiptPublic, signedA, signedB
}

func cp009Activate(t *testing.T, activator *Activator, signed SignedBundle, scope Scope, epoch uint64, now time.Time) ActivationReceipt {
	t.Helper()
	receipt, err := activator.Activate(ActivationRequest{
		Bundle: signed, Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane",
		Epoch: epoch, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), ReceiptSigner: nil,
	})
	if err != nil {
		t.Fatalf("activate epoch %d: %v", epoch, err)
	}
	return receipt
}

func cp009Rollback(t *testing.T, activator *Activator, prior SignedBundle, scope Scope, epoch uint64, now time.Time, bundlePrivate ed25519.PrivateKey, mutate func(*RollbackRequest)) (ActivationReceipt, error) {
	t.Helper()
	req := RollbackRequest{
		Tenant: scope.TenantID, Prior: prior, PriorDigest: prior.Digest, NewEpoch: epoch,
		Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane",
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		Sign:   func(b Bundle) (SignedBundle, error) { return SignBundle(b, "bundle-signer", "v1", bundlePrivate) },
		Pinned: []PinnedWorkflow{{WorkflowRef: "workflow/pick", Digest: prior.Digest}},
	}
	if mutate != nil {
		mutate(&req)
	}
	return Rollback(activator, req)
}

// TestTodo_CP_009 proves rollback revalidates the exact prior bundle,
// re-signs it for a new epoch, applies it with receipts and leaves prior
// history byte-identical, while pinned live workflows keep their policy.
func TestTodo_CP_009(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	scope := Scope{TenantID: "cp009-tenant", CellID: "cell-a"}
	activator, _, bundlePrivate, receiptPublic, signedA, signedB := cp009Setup(t, scope, now)
	first := cp009Activate(t, activator, signedA, scope, 1, now)
	second := cp009Activate(t, activator, signedB, scope, 2, now)
	if second.BundleDigest == first.BundleDigest {
		t.Fatal("revisions did not diverge")
	}

	receipt, err := cp009Rollback(t, activator, signedA, scope, 3, now, bundlePrivate, nil)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if receipt.Epoch != 3 || receipt.BundleDigest != signedA.Digest {
		t.Fatalf("rollback receipt = %+v, want epoch 3 over the prior digest", receipt)
	}
	if err := receipt.Verify(receiptPublic); err != nil {
		t.Fatalf("rollback receipt Verify: %v", err)
	}
	// History is not rewritten: both prior receipts read back identical.
	kept1, ok := activator.Receipt(scope.TenantID, 1)
	if !ok || kept1 != first {
		t.Fatalf("epoch 1 receipt changed under rollback: %+v", kept1)
	}
	kept2, ok := activator.Receipt(scope.TenantID, 2)
	if !ok || kept2 != second {
		t.Fatalf("epoch 2 receipt changed under rollback: %+v", kept2)
	}
	if activator.CurrentEpoch(scope.TenantID) != 3 {
		t.Fatalf("current epoch = %d, want 3", activator.CurrentEpoch(scope.TenantID))
	}
	// The pinned live workflow still resolves its declared digest.
	live, ok := activator.Receipt(scope.TenantID, 3)
	if !ok || live.BundleDigest != signedA.Digest {
		t.Fatalf("pinned workflow digest = %+v", live)
	}
	if receipt.Explain() == "" {
		t.Fatal("rollback receipt explanation is empty")
	}
}

func TestTodo_CP_009_Golden(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	scope := Scope{TenantID: "cp009-golden", CellID: "cell-a"}
	activator, _, bundlePrivate, receiptPublic, signedA, signedB := cp009Setup(t, scope, now)
	cp009Activate(t, activator, signedA, scope, 1, now)
	cp009Activate(t, activator, signedB, scope, 2, now)
	receipt, err := cp009Rollback(t, activator, signedA, scope, 3, now, bundlePrivate, nil)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Digest != "sha256:f10bea2d58d9fac995afbd477eed0566f96676343e13d4b321450225847cb61c" {
		t.Fatalf("rollback receipt digest = %s, want canonical golden digest", receipt.Digest)
	}
	if err := receipt.Verify(receiptPublic); err != nil {
		t.Fatalf("golden rollback receipt failed verification: %v", err)
	}
	if receipt.Explain() == "" || receipt.Signature.Value == "" {
		t.Fatal("rollback receipt explanation or signature is empty")
	}
}

func TestTodo_CP_009_Fault(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	scope := Scope{TenantID: "cp009-fault", CellID: "cell-a"}
	activator, _, bundlePrivate, _, signedA, signedB := cp009Setup(t, scope, now)
	cp009Activate(t, activator, signedA, scope, 1, now)
	cp009Activate(t, activator, signedB, scope, 2, now)

	faults := []struct {
		name   string
		mutate func(*RollbackRequest)
		code   string
	}{
		{"mutable label", func(r *RollbackRequest) { r.PriorDigest = "latest" }, "MUTABLE_ROLLBACK_LABEL"},
		{"digest mismatch", func(r *RollbackRequest) { r.PriorDigest = signedB.Digest }, "ROLLBACK_DIGEST_MISMATCH"},
		{"nothing to roll back", func(r *RollbackRequest) { r.Prior = signedB; r.PriorDigest = signedB.Digest }, "ROLLBACK_NOT_NEEDED"},
		{"stale epoch", func(r *RollbackRequest) { r.NewEpoch = 2 }, "ROLLBACK_EPOCH_STALE"},
		{"revoked target", func(r *RollbackRequest) { r.Revoked = []string{signedA.Digest} }, "REVOKED_ROLLBACK_TARGET"},
		{"unsigned rollback", func(r *RollbackRequest) { r.Sign = nil }, "ROLLBACK_UNSIGNED"},
		{"pin on current", func(r *RollbackRequest) {
			r.Pinned = []PinnedWorkflow{{WorkflowRef: "workflow/pick", Digest: signedB.Digest}}
		}, "PINNED_VERSION_CONFLICT"},
	}
	for _, tc := range faults {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cp009Rollback(t, activator, signedA, scope, 3, now, bundlePrivate, tc.mutate)
			if err == nil || CodeOf(err) != tc.code {
				t.Fatalf("fault %s = %v (code %q), want %s", tc.name, err, CodeOf(err), tc.code)
			}
		})
	}

	// Tampered prior bytes never reproduce the pinned digest.
	t.Run("tampered bytes", func(t *testing.T) {
		evil := signedA
		evil.Bundle.Objects[0].Digest = "sha256:" + strings.Repeat("e", 64)
		_, err := cp009Rollback(t, activator, evil, scope, 3, now, bundlePrivate, nil)
		if err == nil {
			t.Fatal("tampered prior bytes rolled back")
		}
	})
}
