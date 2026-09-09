package configbundle

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
)

func cp003Keys(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey, ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	var bundleSeed [ed25519.SeedSize]byte
	var receiptSeed [ed25519.SeedSize]byte
	for i := range bundleSeed {
		bundleSeed[i] = byte(i + 1)
		receiptSeed[i] = byte(100 + i)
	}
	bundlePrivate := ed25519.NewKeyFromSeed(bundleSeed[:])
	receiptPrivate := ed25519.NewKeyFromSeed(receiptSeed[:])
	return bundlePrivate, bundlePrivate.Public().(ed25519.PublicKey), receiptPrivate, receiptPrivate.Public().(ed25519.PublicKey)
}

func cp003SignedBundle(t *testing.T, bundle Bundle, keyHandle, keyVersion string, privateKey ed25519.PrivateKey) SignedBundle {
	t.Helper()
	signed, err := SignBundle(bundle, keyHandle, keyVersion, privateKey)
	if err != nil {
		t.Fatalf("SignBundle: %v", err)
	}
	return signed
}

func cp003Request(signed SignedBundle, scope Scope, epoch uint64, at time.Time, receiptSigner ReceiptSigner) ActivationRequest {
	return ActivationRequest{
		Bundle: signed, Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane",
		Epoch: epoch, IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Minute), ReceiptSigner: receiptSigner,
	}
}

func cp003Activator(t *testing.T, scope Scope, now time.Time) (*Activator, *InMemoryKeyring, ed25519.PublicKey, ed25519.PublicKey, ed25519.PrivateKey, SignedBundle) {
	t.Helper()
	bundlePrivate, bundlePublic, receiptPrivate, receiptPublic := cp003Keys(t)
	keyring := NewKeyring()
	if err := keyring.Put(BundleKey{Handle: "bundle-signer", Version: "v1", PublicKey: bundlePublic, TrustProfile: "control-plane"}); err != nil {
		t.Fatal(err)
	}
	registry := platformconfig.NewRegistry()
	root := publishObject(t, registry, scope, KindRule, "activation-rule", 1, `{"enabled":true}`)
	bundle, err := Compile(registry, CompileOptions{BundleID: "cp003-bundle", Roots: []ObjectRef{root.Ref()}, TargetScope: scope, MinimumRuntimeVersion: "go1.26.3"})
	if err != nil {
		t.Fatal(err)
	}
	signed := cp003SignedBundle(t, bundle, "bundle-signer", "v1", bundlePrivate)
	receiptSigner, err := NewEd25519ReceiptSigner("receipt-signer", "v1", receiptPrivate)
	if err != nil {
		t.Fatal(err)
	}
	policy := ActivationPolicy{Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane", AntiRollbackFloor: "go1.26.0"}
	activator := NewActivator(keyring, policy, receiptSigner, func() time.Time { return now })
	return activator, keyring, bundlePublic, receiptPublic, receiptPrivate, signed
}

// TestTodo_CP_003 proves signed activation, trust-context binding, receipt
// signing, and same-epoch idempotency.
func TestTodo_CP_003(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	scope := Scope{TenantID: "cp003-tenant", CellID: "cell-a"}
	activator, _, _, receiptPublic, _, signed := cp003Activator(t, scope, now)
	req := cp003Request(signed, scope, 1, now, nil)
	first, err := activator.Activate(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Verify(receiptPublic); err != nil {
		t.Fatalf("receipt Verify: %v", err)
	}
	second, err := activator.Activate(req)
	if err != nil {
		t.Fatalf("same-epoch retry: %v", err)
	}
	if second != first || activator.CurrentEpoch(scope.TenantID) != 1 || activator.ActivationCount(scope.TenantID) != 1 {
		t.Fatalf("retry was not idempotent: first=%+v second=%+v", first, second)
	}
}

func TestTodo_CP_003_Golden(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	scope := Scope{TenantID: "cp003-golden", CellID: "cell-a"}
	activator, _, _, receiptPublic, _, signed := cp003Activator(t, scope, now)
	receipt, err := activator.Activate(cp003Request(signed, scope, 1, now, nil))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Digest != "sha256:e0cae58301549bbf9253678ca3ea5eab2de202fd512cd40246bcec408ad1ed15" {
		t.Fatalf("receipt digest = %s, want canonical golden digest", receipt.Digest)
	}
	if err := receipt.Verify(receiptPublic); err != nil {
		t.Fatalf("golden receipt failed verification: %v", err)
	}
	if receipt.Explain() == "" || receipt.Signature.Value == "" {
		t.Fatal("receipt explanation or signature is empty")
	}
}

func TestTodo_CP_003_Security(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	scope := Scope{TenantID: "cp003-security", CellID: "cell-a"}
	newCase := func(t *testing.T) (*Activator, *InMemoryKeyring, ed25519.PublicKey, SignedBundle) {
		a, keyring, bundlePublic, _, _, signed := cp003Activator(t, scope, now)
		return a, keyring, bundlePublic, signed
	}
	tests := []struct {
		name   string
		mutate func(ActivationRequest, *InMemoryKeyring, ed25519.PublicKey) ActivationRequest
		want   error
	}{
		{name: "unsigned", mutate: func(r ActivationRequest, _ *InMemoryKeyring, _ ed25519.PublicKey) ActivationRequest {
			r.Bundle.Signature = BundleSignature{}
			return r
		}, want: ErrInvalidSignature},
		{name: "wrong signature", mutate: func(r ActivationRequest, _ *InMemoryKeyring, _ ed25519.PublicKey) ActivationRequest {
			r.Bundle.Signature.Value = r.Bundle.Signature.Value[:len(r.Bundle.Signature.Value)-2] + "00"
			return r
		}, want: ErrInvalidSignature},
		{name: "revoked key", mutate: func(r ActivationRequest, keyring *InMemoryKeyring, _ ed25519.PublicKey) ActivationRequest {
			_ = keyring.Revoke("bundle-signer", "v1")
			return r
		}, want: ErrRevokedSigningKey},
		{name: "wrong scope", mutate: func(r ActivationRequest, _ *InMemoryKeyring, _ ed25519.PublicKey) ActivationRequest {
			r.Scope = Scope{TenantID: "other-tenant", CellID: "cell-a"}
			return r
		}, want: ErrActivationScopeMismatch},
		{name: "expired", mutate: func(r ActivationRequest, _ *InMemoryKeyring, _ ed25519.PublicKey) ActivationRequest {
			r.IssuedAt = now.Add(-2 * time.Hour)
			r.ExpiresAt = now.Add(-time.Hour)
			return r
		}, want: ErrExpiredBundle},
		{name: "below floor", mutate: func(r ActivationRequest, _ *InMemoryKeyring, _ ed25519.PublicKey) ActivationRequest {
			r.AntiRollbackFloor = "go1.27.0"
			return r
		}, want: ErrBelowRollbackFloor},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, keyring, bundlePublic, signed := newCase(t)
			_, _, receiptPrivate, _ := cp003Keys(t)
			receiptSigner, err := NewEd25519ReceiptSigner("receipt-signer", "v1", receiptPrivate)
			if err != nil {
				t.Fatal(err)
			}
			r := cp003Request(signed, scope, 1, now, receiptSigner)
			r = tc.mutate(r, keyring, bundlePublic)
			if _, err := a.Activate(r); !errors.Is(err, tc.want) {
				t.Fatalf("Activate error = %v, want %v", err, tc.want)
			}
			if got := a.ActivationCount(scope.TenantID); got != 0 {
				t.Fatalf("denied activation changed state: count=%d", got)
			}
		})
	}
}

func TestTodo_CP_003_Recovery(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	scope := Scope{TenantID: "cp003-recovery", CellID: "cell-a"}
	a, _, _, _, receiptPrivate, signed := cp003Activator(t, scope, now)
	receiptSigner, err := NewEd25519ReceiptSigner("receipt-signer", "v1", receiptPrivate)
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.Activate(cp003Request(signed, scope, 1, now, receiptSigner))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		got, replayErr := a.Activate(cp003Request(signed, scope, 1, now, receiptSigner))
		if replayErr != nil || got != first {
			t.Fatalf("replay %d changed idempotent result: receipt=%+v err=%v", i, got, replayErr)
		}
	}
	if _, err := a.Activate(cp003Request(signed, scope, 2, now, receiptSigner)); err != nil {
		t.Fatalf("next epoch activation: %v", err)
	}
	stale := cp003Request(signed, scope, 1, now, receiptSigner)
	if _, err := a.Activate(stale); !errors.Is(err, ErrEpochReplay) {
		t.Fatalf("stale epoch error = %v, want ErrEpochReplay", err)
	}
	if a.CurrentEpoch(scope.TenantID) != 2 || a.ActivationCount(scope.TenantID) != 2 {
		t.Fatalf("replays altered epoch state: epoch=%d count=%d", a.CurrentEpoch(scope.TenantID), a.ActivationCount(scope.TenantID))
	}
}
