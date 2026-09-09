package custody_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

func TestKeyHandleLifecycle_RejectsInvalidRequestsAndPreservesState(t *testing.T) {
	fake := lifecycleFake(t)
	ctx := lifecycleContext()
	handle := lifecycleHandle(custody.ProviderKMS, "lifecycle-extra")
	if _, err := fake.CreateKeyHandle(ctx, handle); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.CreateKeyHandle(ctx, handle); !errors.Is(err, custody.ErrKeyHandleExists) {
		t.Fatalf("duplicate create = %v, want ErrKeyHandleExists", err)
	}
	if _, err := fake.KeyLifecycle(ctx, lifecycleHandle(custody.ProviderKMS, "missing")); !errors.Is(err, custody.ErrKeyHandleNotFound) {
		t.Fatalf("unknown lifecycle = %v, want ErrKeyHandleNotFound", err)
	}
	if _, err := fake.ActivateKeyHandle(ctx, handle, ""); !errors.Is(err, custody.ErrLifecycleEvidenceRequired) {
		t.Fatalf("missing activation evidence = %v", err)
	}
	if _, err := fake.RotateKeyHandle(ctx, handle, "evidence"); !errors.Is(err, custody.ErrInvalidTransition) {
		t.Fatalf("rotate pending = %v, want ErrInvalidTransition", err)
	}
	if _, err := fake.DisableKeyHandle(ctx, handle, "evidence"); !errors.Is(err, custody.ErrInvalidTransition) {
		t.Fatalf("disable pending = %v, want ErrInvalidTransition", err)
	}
	if _, err := fake.ActivateKeyHandle(ctx, handle, "activation"); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.ActivateKeyHandle(ctx, handle, "again"); !errors.Is(err, custody.ErrInvalidTransition) {
		t.Fatalf("activate active = %v, want ErrInvalidTransition", err)
	}
	if _, err := fake.RotateKeyHandle(ctx, handle, "rotation"); err != nil {
		t.Fatal(err)
	}
	next := handle
	next.Version = "v2"
	if _, err := fake.ActivateKeyHandle(ctx, next, "activation-v2"); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.DisableKeyHandle(ctx, next, "disable"); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.DisableKeyHandle(ctx, next, "again"); !errors.Is(err, custody.ErrKeyHandleDisabled) {
		t.Fatalf("disable disabled = %v, want ErrKeyHandleDisabled", err)
	}
	view, err := fake.KeyLifecycle(ctx, next)
	if err != nil || view.State != custody.StateDisabled || len(view.Events) != 3 {
		t.Fatalf("disabled view = %+v, %v", view, err)
	}
}

func TestImportBYOK_RejectsProofAttestationAndTransitionFailures(t *testing.T) {
	fake := lifecycleFake(t)
	ctx := lifecycleContext()
	byok := lifecycleHandle(custody.ProviderBYOK, "byok-extra")
	unknownRequest := custody.BYOKImportRequest{Handle: byok, WrappingProofDigest: "proof", Attestation: custody.BYOKAttestation{Handle: byok, EvidenceDigest: "attestation", ValidUntil: lifecycleNow.Add(time.Hour)}}
	if _, err := fake.ImportBYOK(ctx, unknownRequest); !errors.Is(err, custody.ErrKeyHandleNotFound) {
		t.Fatalf("import without existing handle = %v, want ErrKeyHandleNotFound", err)
	}
	wrongClass := lifecycleHandle(custody.ProviderKMS, "wrong-class")
	if _, err := fake.ImportBYOK(ctx, custody.BYOKImportRequest{Handle: wrongClass}); !errors.Is(err, custody.ErrInvalidLifecycle) {
		t.Fatalf("non-BYOK import = %v, want ErrInvalidLifecycle", err)
	}
	if _, err := fake.CreateKeyHandle(ctx, byok); err != nil {
		t.Fatal(err)
	}
	base := custody.BYOKImportRequest{Handle: byok, WrappingProofDigest: "proof", Attestation: custody.BYOKAttestation{Handle: byok, EvidenceDigest: "attestation", AttestedAt: lifecycleNow, ValidUntil: lifecycleNow.Add(time.Hour)}}
	missingProof := base
	missingProof.WrappingProofDigest = ""
	if _, err := fake.ImportBYOK(ctx, missingProof); !errors.Is(err, custody.ErrBYOKProofRequired) {
		t.Fatalf("missing proof = %v, want ErrBYOKProofRequired", err)
	}
	for _, mutate := range []func(*custody.BYOKImportRequest){
		func(r *custody.BYOKImportRequest) { r.Attestation.Handle.ID = "other" },
		func(r *custody.BYOKImportRequest) { r.Attestation.EvidenceDigest = "" },
		func(r *custody.BYOKImportRequest) { r.Attestation.ValidUntil = lifecycleNow },
		func(r *custody.BYOKImportRequest) { r.Attestation.ValidUntil = lifecycleNow.Add(-time.Nanosecond) },
	} {
		request := base
		mutate(&request)
		if _, err := fake.ImportBYOK(ctx, request); !errors.Is(err, custody.ErrBYOKAttestationInvalid) {
			t.Fatalf("invalid attestation = %v, want ErrBYOKAttestationInvalid", err)
		}
	}
	if _, err := fake.ImportBYOK(ctx, base); err != nil {
		t.Fatalf("valid import = %v", err)
	}
	active, err := fake.ActivateKeyHandle(ctx, byok, "activate")
	if err != nil || active.State != custody.StateActive {
		t.Fatalf("activate imported key = %+v, %v", active, err)
	}
	if _, err := fake.ImportBYOK(ctx, base); !errors.Is(err, custody.ErrInvalidTransition) {
		t.Fatalf("import active key = %v, want ErrInvalidTransition", err)
	}
}

func TestDestroyKeyHandle_RequiresSeparationAndRetentionEvidence(t *testing.T) {
	fake := lifecycleFake(t)
	ctx := lifecycleContext()
	valid := func(handle custody.KeyHandle) custody.DestroyRequest {
		return custody.DestroyRequest{Handle: handle, RequestedBy: "operator", Approver: "approver", RetentionCheck: custody.RetentionHoldCheck{Declared: true, EvidenceDigest: "retention", CheckedAt: lifecycleNow}}
	}
	for _, request := range []custody.DestroyRequest{
		{Handle: lifecycleHandle(custody.ProviderKMS, "missing"), RequestedBy: "operator", Approver: "approver", RetentionCheck: custody.RetentionHoldCheck{Declared: true, EvidenceDigest: "e", CheckedAt: lifecycleNow}},
	} {
		if _, err := fake.DestroyKeyHandle(ctx, request); !errors.Is(err, custody.ErrKeyHandleNotFound) {
			t.Fatalf("unknown destroy = %v, want ErrKeyHandleNotFound", err)
		}
	}
	handle := lifecycleHandle(custody.ProviderKMS, "destroy-extra")
	if _, err := fake.CreateKeyHandle(ctx, handle); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*custody.DestroyRequest)
		want   error
	}{
		{"missing requester", func(r *custody.DestroyRequest) { r.RequestedBy = "" }, custody.ErrDistinctApprover},
		{"missing approver", func(r *custody.DestroyRequest) { r.Approver = "" }, custody.ErrDistinctApprover},
		{"self approval", func(r *custody.DestroyRequest) { r.Approver = r.RequestedBy }, custody.ErrDistinctApprover},
		{"retention hold", func(r *custody.DestroyRequest) { r.RetentionCheck.OnHold = true }, custody.ErrRetentionHold},
		{"undeclared check", func(r *custody.DestroyRequest) { r.RetentionCheck.Declared = false }, custody.ErrInvalidLifecycle},
		{"missing check evidence", func(r *custody.DestroyRequest) { r.RetentionCheck.EvidenceDigest = "" }, custody.ErrInvalidLifecycle},
		{"missing checked time", func(r *custody.DestroyRequest) { r.RetentionCheck.CheckedAt = time.Time{} }, custody.ErrInvalidLifecycle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := valid(handle)
			tc.mutate(&request)
			if _, err := fake.DestroyKeyHandle(ctx, request); !errors.Is(err, tc.want) {
				t.Fatalf("destroy = %v, want %v", err, tc.want)
			}
			view, err := fake.KeyLifecycle(ctx, handle)
			if err != nil || view.State != custody.StatePending {
				t.Fatalf("failed destroy changed state: %+v, %v", view, err)
			}
		})
	}
	view, err := fake.DestroyKeyHandle(ctx, valid(handle))
	if err != nil || view.State != custody.StateDestroyed || view.Events[len(view.Events)-1].AuthorityDigest == "" {
		t.Fatalf("valid destroy = %+v, %v", view, err)
	}
	if _, err := fake.DestroyKeyHandle(ctx, valid(handle)); !errors.Is(err, custody.ErrKeyHandleDestroyed) {
		t.Fatalf("destroy destroyed key = %v, want ErrKeyHandleDestroyed", err)
	}
}

func TestKeyHandleLifecycle_RejectsScopeAndReportsContract(t *testing.T) {
	fake := lifecycleFake(t)
	handle := lifecycleHandle(custody.ProviderKMS, "scope-extra")
	wrongTenant := lifecycleContext()
	wrongTenant.Tenant = "tenant-b"
	if _, err := fake.CreateKeyHandle(wrongTenant, handle); !errors.Is(err, custody.ErrInvalidLifecycle) {
		t.Fatalf("wrong tenant = %v, want ErrInvalidLifecycle", err)
	}
	badContext := lifecycleContext()
	badContext.Purpose = ""
	if _, err := fake.CreateKeyHandle(badContext, handle); !errors.Is(err, custody.ErrInvalidContext) {
		t.Fatalf("invalid context = %v, want ErrInvalidContext", err)
	}
	badHandle := handle
	badHandle.ProviderClass = "cloud"
	if _, err := fake.CreateKeyHandle(lifecycleContext(), badHandle); !errors.Is(err, custody.ErrInvalidKeyHandle) {
		t.Fatalf("invalid provider class = %v, want ErrInvalidKeyHandle", err)
	}
	if custody.Version() != 1 || strings.TrimSpace(custody.Explain()) == "" || !strings.Contains(custody.Explain(), "DESTROYED") {
		t.Fatalf("lifecycle contract description = version %d, %q", custody.Version(), custody.Explain())
	}
}
