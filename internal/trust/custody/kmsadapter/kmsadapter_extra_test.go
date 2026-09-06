package kmsadapter

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

type faultClient struct {
	*softwareKMS
	createErr, wrapErr, unwrapErr, signErr, rotateErr, listErr error
	createResult                                               func(KeyVersion) KeyVersion
	wrapResult                                                 func(WrappedValue) WrappedValue
	unwrapResult                                               func(UnwrappedValue) UnwrappedValue
	signResult                                                 func(SignedValue) SignedValue
	rotateResult                                               func(KeyVersion) KeyVersion
	listResult                                                 func([]KeyVersion) []KeyVersion
}

func (c *faultClient) CreateKey(req CreateKeyRequest) (KeyVersion, error) {
	if c.createErr != nil {
		return KeyVersion{}, c.createErr
	}
	got, err := c.softwareKMS.CreateKey(req)
	if err == nil && c.createResult != nil {
		got = c.createResult(got)
	}
	return got, err
}
func (c *faultClient) Wrap(req WrapRequest) (WrappedValue, error) {
	if c.wrapErr != nil {
		return WrappedValue{}, c.wrapErr
	}
	got, err := c.softwareKMS.Wrap(req)
	if err == nil && c.wrapResult != nil {
		got = c.wrapResult(got)
	}
	return got, err
}
func (c *faultClient) Unwrap(req UnwrapRequest) (UnwrappedValue, error) {
	if c.unwrapErr != nil {
		return UnwrappedValue{}, c.unwrapErr
	}
	got, err := c.softwareKMS.Unwrap(req)
	if err == nil && c.unwrapResult != nil {
		got = c.unwrapResult(got)
	}
	return got, err
}
func (c *faultClient) Sign(req SignRequest) (SignedValue, error) {
	if c.signErr != nil {
		return SignedValue{}, c.signErr
	}
	got, err := c.softwareKMS.Sign(req)
	if err == nil && c.signResult != nil {
		got = c.signResult(got)
	}
	return got, err
}
func (c *faultClient) Rotate(req RotateRequest) (KeyVersion, error) {
	if c.rotateErr != nil {
		return KeyVersion{}, c.rotateErr
	}
	got, err := c.softwareKMS.Rotate(req)
	if err == nil && c.rotateResult != nil {
		got = c.rotateResult(got)
	}
	return got, err
}
func (c *faultClient) ListVersions(req ListVersionsRequest) ([]KeyVersion, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	got, err := c.softwareKMS.ListVersions(req)
	if err == nil && c.listResult != nil {
		got = c.listResult(got)
	}
	return got, err
}

func faultAdapter(t *testing.T) (*Adapter, *faultClient, custody.Context, custody.Handle) {
	t.Helper()
	adapter, client, ctx, handle := newAdapter(t)
	fault := &faultClient{softwareKMS: client}
	adapter.client = fault
	return adapter, fault, ctx, handle
}

func TestNewAndOption_KeyVersionAndProviderValidation(t *testing.T) {
	if _, err := New(nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("New(nil) = %v, want ErrInvalidRequest", err)
	}
	clock := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	adapter, err := New(newSoftwareKMS(), nil, WithClock(nil), WithClock(func() time.Time { return clock }))
	if err != nil || !adapter.now().Equal(clock) {
		t.Fatalf("New options = %v, now %v", err, adapter.now())
	}
	for _, key := range []KeyVersion{{}, {ID: "id", Tenant: "t", Residency: "r"}} {
		if !errors.Is(key.validate(), ErrInvalidRequest) {
			t.Fatalf("invalid key version accepted: %+v", key)
		}
	}
	valid := KeyVersion{ID: "id", Tenant: "t", Residency: "r", Version: "v1"}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid key version rejected: %v", err)
	}
	if !errors.Is(validateReturned(KeyVersion{}, valid), ErrProviderFailure) || !errors.Is(validateReturned(KeyVersion{ID: "other", Tenant: "t", Residency: "r", Version: "v1"}, valid), ErrTenantIsolation) || !errors.Is(validateReturned(KeyVersion{ID: "id", Tenant: "other", Residency: "r", Version: "v1"}, valid), ErrTenantIsolation) || !errors.Is(validateReturned(KeyVersion{ID: "id", Tenant: "t", Residency: "other", Version: "v1"}, valid), ErrResidencyMismatch) {
		t.Fatal("validateReturned did not reject malformed provider identity")
	}
}

func TestAdapter_RejectsContextHandleAndLookupFailures(t *testing.T) {
	adapter, client, ctx, handle := faultAdapter(t)
	badCtx := ctx
	badCtx.Workload = ""
	if _, _, err := adapter.Encrypt(badCtx, handle, []byte("x")); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid context = %v", err)
	}
	badTenant := ctx
	badTenant.Tenant = "tenant-b"
	if _, _, err := adapter.Encrypt(badTenant, handle, []byte("x")); !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("tenant mismatch = %v", err)
	}
	badRegion := ctx
	badRegion.Region = "eu-west"
	if _, _, err := adapter.Encrypt(badRegion, handle, []byte("x")); !errors.Is(err, ErrResidencyMismatch) {
		t.Fatalf("residency mismatch = %v", err)
	}
	if _, _, err := adapter.Encrypt(ctx, custody.Handle{}, []byte("x")); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid handle = %v", err)
	}
	missing := handle
	missing.ID = "missing"
	if _, _, err := adapter.Encrypt(ctx, missing, []byte("x")); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("missing key = %v", err)
	}
	client.listErr = errors.New("list down")
	if _, _, err := adapter.Encrypt(ctx, handle, []byte("x")); !errors.Is(err, ErrProviderFailure) {
		t.Fatalf("list failure = %v", err)
	}
	client.listErr = nil
	if err := adapter.checkRevoked(handle); err != nil {
		t.Fatalf("unrevoked handle = %v", err)
	}
}

func TestAdapter_EncryptDecryptSignVerify_RejectMalformedProviderResults(t *testing.T) {
	adapter, client, ctx, handle := faultAdapter(t)
	if _, _, err := adapter.Encrypt(ctx, handle, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty plaintext = %v", err)
	}
	client.wrapErr = errors.New("wrap down")
	if _, _, err := adapter.Encrypt(ctx, handle, []byte("plain")); !errors.Is(err, ErrProviderFailure) {
		t.Fatalf("wrap failure = %v", err)
	}
	client.wrapErr = nil
	client.wrapResult = func(v WrappedValue) WrappedValue {
		v.Key.Tenant = "other"
		return v
	}
	if _, _, err := adapter.Encrypt(ctx, handle, []byte("plain")); !errors.Is(err, ErrTenantIsolation) {
		t.Fatalf("bad wrap tenant = %v", err)
	}
	client.wrapResult = func(v WrappedValue) WrappedValue {
		v.Ciphertext = nil
		return v
	}
	if _, _, err := adapter.Encrypt(ctx, handle, []byte("plain")); !errors.Is(err, ErrProviderFailure) {
		t.Fatalf("bad wrap ciphertext = %v", err)
	}
	client.wrapResult = nil
	sealed, _, err := adapter.Encrypt(ctx, handle, []byte("plain"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapter.Decrypt(ctx, handle, custody.Ciphertext{Handle: handle}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty ciphertext = %v", err)
	}
	client.unwrapErr = errors.New("unwrap down")
	if _, _, err := adapter.Decrypt(ctx, handle, sealed); !errors.Is(err, ErrProviderFailure) {
		t.Fatalf("unwrap failure = %v", err)
	}
	client.unwrapErr = nil
	client.unwrapResult = func(v UnwrappedValue) UnwrappedValue {
		v.Key.Residency = "eu-west"
		return v
	}
	if _, _, err := adapter.Decrypt(ctx, handle, sealed); !errors.Is(err, ErrResidencyMismatch) {
		t.Fatalf("bad unwrap residency = %v", err)
	}
	client.unwrapResult = func(v UnwrappedValue) UnwrappedValue {
		v.Plaintext = nil
		return v
	}
	if _, _, err := adapter.Decrypt(ctx, handle, sealed); !errors.Is(err, ErrProviderFailure) {
		t.Fatalf("bad unwrap plaintext = %v", err)
	}
	client.unwrapResult = nil
	if _, _, err := adapter.Decrypt(ctx, handle, sealed); err != nil {
		t.Fatalf("valid decrypt = %v", err)
	}
	if _, _, err := adapter.Sign(ctx, handle, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty message = %v", err)
	}
	client.signErr = errors.New("sign down")
	if _, _, err := adapter.Sign(ctx, handle, []byte("message")); !errors.Is(err, ErrProviderFailure) {
		t.Fatalf("sign failure = %v", err)
	}
	client.signErr = nil
	client.signResult = func(v SignedValue) SignedValue { v.Signature = nil; return v }
	if _, _, err := adapter.Sign(ctx, handle, []byte("message")); !errors.Is(err, ErrProviderFailure) {
		t.Fatalf("empty signature = %v", err)
	}
	client.signResult = nil
	signature, _, err := adapter.Sign(ctx, handle, []byte("message"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapter.Verify(ctx, handle, nil, signature); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty verify message = %v", err)
	}
	if _, _, err := adapter.Verify(ctx, handle, []byte("message"), custody.Signature{Handle: handle}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty verify signature = %v", err)
	}
	ok, _, err := adapter.Verify(ctx, handle, []byte("wrong"), signature)
	if err != nil || ok {
		t.Fatalf("invalid signature = %v, %v", ok, err)
	}
	if _, err := verifyPublic(ed25519.PublicKey{1}, []byte("m"), []byte{1}); !errors.Is(err, ErrUnsupportedKeyType) {
		t.Fatalf("malformed ed25519 public key = %v, want ErrUnsupportedKeyType", err)
	}
	if _, err := verifyPublic((*ecdsa.PublicKey)(nil), []byte("m"), []byte{1}); !errors.Is(err, ErrUnsupportedKeyType) {
		t.Fatalf("malformed ecdsa public key = %v, want ErrUnsupportedKeyType", err)
	}
	if _, err := verifyPublic(struct{}{}, []byte("m"), []byte{1}); !errors.Is(err, ErrUnsupportedKeyType) {
		t.Fatalf("unsupported public key = %v", err)
	}
}

func TestAdapter_LeaseLifecycleAndRotationRejectInvalidStates(t *testing.T) {
	adapter, client, ctx, handle := faultAdapter(t)
	for _, op := range []custody.Operation{custody.Rotate, custody.Revoke, custody.Operation("unknown")} {
		if _, err := adapter.IssueLease(ctx, handle, op, time.Minute); !errors.Is(err, ErrInvalidLease) {
			t.Fatalf("invalid lease operation %q = %v", op, err)
		}
	}
	if _, err := adapter.IssueLease(ctx, handle, custody.Sign, 0); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("zero lease ttl = %v", err)
	}
	lease, err := adapter.IssueLease(ctx, handle, custody.Sign, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.RenewLease(ctx, custody.Lease{ID: "missing", Handle: handle, ContextDigest: custody.ContextDigest(ctx.RequestContext)}, time.Minute); !errors.Is(err, ErrLeaseUnknown) {
		t.Fatalf("unknown lease = %v", err)
	}
	if _, err := adapter.RenewLease(ctx, lease, 0); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("zero renewal ttl = %v", err)
	}
	tampered := lease
	tampered.ContextDigest[0] ^= 1
	if _, err := adapter.RenewLease(ctx, tampered, time.Minute); !errors.Is(err, ErrLeaseTampered) {
		t.Fatalf("tampered context digest = %v", err)
	}
	adapter.leases[lease.ID] = leaseRecord{lease: lease, revoked: true}
	if _, err := adapter.RenewLease(ctx, lease, time.Minute); !errors.Is(err, ErrLeaseRevoked) {
		t.Fatalf("revoked lease = %v", err)
	}
	adapter.leases[lease.ID] = leaseRecord{lease: lease, used: true}
	if _, err := adapter.RenewLease(ctx, lease, time.Minute); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("used lease = %v", err)
	}
	adapter.leases[lease.ID] = leaseRecord{lease: lease}
	renewed, err := adapter.RenewLease(ctx, lease, time.Minute)
	if err != nil || !renewed.ExpiresAt.Equal(lease.ExpiresAt) || renewed.ContextDigest != custody.ContextDigest(ctx.RequestContext) {
		t.Fatalf("renewed lease = %+v, %v", renewed, err)
	}
	changed := ctx
	changed.Purpose = "different-purpose"
	if _, err := adapter.RenewLease(changed, renewed, time.Minute); !errors.Is(err, ErrLeaseTampered) {
		t.Fatalf("renewed lease under different context = %v", err)
	}
	client.rotateErr = errors.New("rotate down")
	if _, _, err := adapter.Rotate(ctx, handle); !errors.Is(err, ErrProviderFailure) {
		t.Fatalf("rotate failure = %v", err)
	}
	client.rotateErr = nil
	client.rotateResult = func(v KeyVersion) KeyVersion { v.Version = "v1"; return v }
	if _, _, err := adapter.Rotate(ctx, handle); !errors.Is(err, ErrProviderFailure) {
		t.Fatalf("non-advancing rotation = %v", err)
	}
	client.rotateResult = nil
	if _, err := adapter.Revoke(ctx, handle, " "); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty revoke reason = %v", err)
	}
	if _, err := adapter.Revoke(ctx, handle, "compromised"); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Revoke(ctx, handle, "again"); !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("duplicate revoke = %v", err)
	}
}

func TestMetadataAdapter_NilAndLifecycleErrorBranches(t *testing.T) {
	var nilMetadata *MetadataAdapter
	ctx := adapterContext("tenant-a", "us-east-1")
	handle := adapterHandle("key-1", "tenant-a", "us-east-1", "v1")
	if _, err := nilMetadata.Get(ctx, handle); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil metadata Get = %v", err)
	}
	if _, _, err := nilMetadata.Rotate(ctx, handle); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil metadata Rotate = %v", err)
	}
	if _, err := nilMetadata.Revoke(ctx, handle, "x"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil metadata Revoke = %v", err)
	}
	if _, err := nilMetadata.Attest(ctx, handle); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil metadata Attest = %v", err)
	}
	var nilAdapter *Adapter
	if nilAdapter.Metadata() != nil {
		t.Fatal("nil Adapter.Metadata returned a view")
	}
	adapter, client, _, _ := faultAdapter(t)
	badCtx := ctx
	badCtx.Workload = ""
	keyHandle := custody.KeyHandle{ID: handle.ID, ProviderClass: custody.ProviderKMS, Tenant: handle.Tenant, Purpose: "test-purpose", Version: handle.Version}
	if _, err := adapter.CreateKeyHandle(badCtx, keyHandle); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid lifecycle context = %v", err)
	}
	wrongTenant := keyHandle
	wrongTenant.Tenant = "tenant-b"
	if _, err := adapter.CreateKeyHandle(ctx, wrongTenant); !errors.Is(err, ErrTenantMismatch) {
		t.Fatalf("lifecycle tenant mismatch = %v", err)
	}
	client.createErr = errors.New("create down")
	if _, err := adapter.CreateKeyHandle(ctx, keyHandle); !errors.Is(err, ErrProviderFailure) {
		t.Fatalf("lifecycle create failure = %v", err)
	}
	client.createErr = nil
	client.createResult = func(v KeyVersion) KeyVersion { v.ID = "wrong"; return v }
	if _, err := adapter.CreateKeyHandle(ctx, custody.KeyHandle{ID: "new-key", ProviderClass: custody.ProviderKMS, Tenant: ctx.Tenant, Purpose: "test-purpose", Version: "v1"}); !errors.Is(err, ErrTenantIsolation) {
		t.Fatalf("invalid lifecycle provider result = %v", err)
	}
	client.createResult = nil
	if _, err := adapter.ActivateKeyHandle(ctx, keyHandle, ""); !errors.Is(err, custody.ErrLifecycleEvidenceRequired) {
		t.Fatalf("missing lifecycle evidence = %v", err)
	}
	if _, err := adapter.RotateKeyHandle(ctx, keyHandle, ""); !errors.Is(err, custody.ErrLifecycleEvidenceRequired) {
		t.Fatalf("missing lifecycle rotation evidence = %v", err)
	}
	if _, err := adapter.DestroyKeyHandle(ctx, custody.DestroyRequest{Handle: keyHandle, RequestedBy: "a", Approver: "b", RetentionCheck: custody.RetentionHoldCheck{OnHold: true}}); !errors.Is(err, custody.ErrRetentionHold) {
		t.Fatalf("held lifecycle destroy = %v", err)
	}
	if Version() != 1 || !strings.Contains(Explain(), "tenant-scoped") {
		t.Fatalf("contract report = %d, %q", Version(), Explain())
	}
}
