package custody_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

var lifecycleNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func lifecycleContext() custody.Context {
	return custody.Context{RequestContext: custody.RequestContext{Workload: "key-admin", Tenant: "tenant-1", Region: "us-east-1", Purpose: "pseudonym-key", Destination: "privacy"}}
}

func lifecycleHandle(class custody.ProviderClass, id string) custody.KeyHandle {
	return custody.KeyHandle{ID: id, ProviderClass: class, Tenant: "tenant-1", Purpose: "pseudonym-key", Version: "v1"}
}

func lifecycleFake(t *testing.T) *custody.InMemoryFake {
	t.Helper()
	return custody.NewInMemoryFake(func() time.Time { return lifecycleNow })
}

func activeLifecycleKey(t *testing.T, fake *custody.InMemoryFake, class custody.ProviderClass) custody.KeyHandle {
	t.Helper()
	handle := lifecycleHandle(class, "lifecycle-key")
	if _, err := fake.CreateKeyHandle(lifecycleContext(), handle); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.ActivateKeyHandle(lifecycleContext(), handle, "sha256:activation"); err != nil {
		t.Fatal(err)
	}
	return handle
}

// TestTodo_TRUST_031 is the primary lifecycle contract: opaque handles can be
// created, activated, rotated, disabled, and destroyed with digest evidence.
func TestTodo_TRUST_031(t *testing.T) {
	fake := lifecycleFake(t)
	var _ custody.KeyLifecycleProvider = fake
	ctx := lifecycleContext()
	byok := lifecycleHandle(custody.ProviderBYOK, "imported-key")
	if _, err := fake.CreateKeyHandle(ctx, byok); err != nil {
		t.Fatal(err)
	}
	imported, err := fake.ImportBYOK(ctx, custody.BYOKImportRequest{Handle: byok, WrappingProofDigest: "sha256:wrapping-proof", Attestation: custody.BYOKAttestation{Handle: byok, EvidenceDigest: "sha256:attestation", AttestedAt: lifecycleNow, ValidUntil: lifecycleNow.Add(time.Hour)}})
	if err != nil || imported.State != custody.StatePending || len(imported.Events) != 2 {
		t.Fatalf("ImportBYOK = %+v, %v", imported, err)
	}
	handle := activeLifecycleKey(t, fake, custody.ProviderKMS)
	next, err := fake.RotateKeyHandle(ctx, handle, "sha256:rotation")
	if err != nil || next.Version != "v2" {
		t.Fatalf("RotateKeyHandle = %+v, %v", next, err)
	}
	rotated, err := fake.KeyLifecycle(ctx, handle)
	if err != nil || rotated.State != custody.StateRotating {
		t.Fatalf("old lifecycle = %+v, %v", rotated, err)
	}
	if _, err := fake.ActivateKeyHandle(ctx, next, "sha256:activation-v2"); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.DisableKeyHandle(ctx, next, "sha256:disable"); err != nil {
		t.Fatal(err)
	}
	view, err := fake.DestroyKeyHandle(ctx, custody.DestroyRequest{Handle: next, RequestedBy: "operator-a", Approver: "approver-b", RetentionCheck: custody.RetentionHoldCheck{Declared: true, EvidenceDigest: "sha256:hold-check", CheckedAt: lifecycleNow}})
	if err != nil || view.State != custody.StateDestroyed {
		t.Fatalf("DestroyKeyHandle = %+v, %v", view, err)
	}
	if len(view.Events) < 4 || view.Events[len(view.Events)-1].Digest == "" {
		t.Fatalf("lifecycle evidence = %+v", view.Events)
	}
	if view.Events[len(view.Events)-1].AuthorityDigest == "" {
		t.Fatal("destroy event omitted authority digest")
	}
}

// FuzzTodo_TRUST_031 covers validation of opaque lifecycle coordinates.
func FuzzTodo_TRUST_031(f *testing.F) {
	f.Add("id", "tenant-1", "purpose", "v1")
	f.Add("", "", "", "")
	f.Fuzz(func(t *testing.T, id, tenant, purpose, version string) {
		handle := custody.KeyHandle{ID: id, ProviderClass: custody.ProviderKMS, Tenant: tenant, Purpose: purpose, Version: version}
		_ = handle.Validate()
	})
}

// TestTodo_TRUST_031_Integration is the conformance fixture: an adapter is
// exercised through the lifecycle interface rather than fake-specific state.
func TestTodo_TRUST_031_Integration(t *testing.T) {
	fake := lifecycleFake(t)
	conformance := func(t *testing.T, provider custody.KeyLifecycleProvider) {
		t.Helper()
		ctx := lifecycleContext()
		handle := lifecycleHandle(custody.ProviderHSM, "hsm-key")
		created, err := provider.CreateKeyHandle(ctx, handle)
		if err != nil || created.State != custody.StatePending || len(created.Events) != 1 {
			t.Fatalf("create = %+v, %v", created, err)
		}
		active, err := provider.ActivateKeyHandle(ctx, handle, "sha256:activate")
		if err != nil || active.State != custody.StateActive {
			t.Fatalf("activate = %+v, %v", active, err)
		}
		next, err := provider.RotateKeyHandle(ctx, handle, "sha256:rotate")
		if err != nil || next.Version != "v2" {
			t.Fatalf("rotate = %+v, %v", next, err)
		}
	}
	conformance(t, fake)
}

// TestTodo_TRUST_031_Security proves provider/tenant/purpose confusion,
// incomplete BYOK proof, and invalid attestation are rejected.
func TestTodo_TRUST_031_Security(t *testing.T) {
	fake := lifecycleFake(t)
	ctx := lifecycleContext()
	handle := lifecycleHandle(custody.ProviderBYOK, "byok-key")
	if _, err := fake.CreateKeyHandle(ctx, handle); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.ImportBYOK(ctx, custody.BYOKImportRequest{Handle: handle}); !errors.Is(err, custody.ErrBYOKProofRequired) {
		t.Fatalf("missing proof = %v", err)
	}
	if _, err := fake.ImportBYOK(ctx, custody.BYOKImportRequest{Handle: handle, WrappingProofDigest: "sha256:proof", Attestation: custody.BYOKAttestation{Handle: lifecycleHandle(custody.ProviderKMS, "byok-key"), EvidenceDigest: "sha256:attest", ValidUntil: lifecycleNow.Add(time.Hour)}}); !errors.Is(err, custody.ErrBYOKAttestationInvalid) {
		t.Fatalf("mismatched attestation = %v", err)
	}
	wrong := ctx
	wrong.Tenant = "tenant-2"
	if _, err := fake.CreateKeyHandle(wrong, handle); !errors.Is(err, custody.ErrInvalidLifecycle) {
		t.Fatalf("tenant confusion = %v", err)
	}
}

// TestTodo_TRUST_031_Mutation proves destruction cannot bypass separation of
// duties or a declared retention hold.
func TestTodo_TRUST_031_Mutation(t *testing.T) {
	fake := lifecycleFake(t)
	ctx := lifecycleContext()
	handle := activeLifecycleKey(t, fake, custody.ProviderKMS)
	self := custody.DestroyRequest{Handle: handle, RequestedBy: "same", Approver: "same", RetentionCheck: custody.RetentionHoldCheck{Declared: true, EvidenceDigest: "e", CheckedAt: lifecycleNow}}
	if _, err := fake.DestroyKeyHandle(ctx, self); !errors.Is(err, custody.ErrDistinctApprover) {
		t.Fatalf("self approval = %v", err)
	}
	hold := self
	hold.RequestedBy, hold.Approver = "operator", "approver"
	hold.RetentionCheck.OnHold = true
	if _, err := fake.DestroyKeyHandle(ctx, hold); !errors.Is(err, custody.ErrRetentionHold) {
		t.Fatalf("retention hold = %v", err)
	}
}
