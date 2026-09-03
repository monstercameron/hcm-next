package custody

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func testHandle() Handle {
	return Handle{ID: "obj-1", Kind: Key, Version: "v2", Tenant: "tenant-a", Region: "us-east"}
}
func testContext() Context {
	return Context{RequestContext: RequestContext{Workload: "worker-a", Tenant: "tenant-a", Region: "us-east", Purpose: "ledger-sign", Destination: "ledger"}}
}

// TestTodo_TRUST_026 is the focused contract test for the provider-neutral
// custody port. A fake provider can implement the port without importing any
// cloud SDK or exposing a raw material retrieval method.
func TestTodo_TRUST_026(t *testing.T) {
	var _ Provider = (*contractFake)(nil)
	h := testHandle()
	if err := h.Validate(); err != nil {
		t.Fatal(err)
	}
	c := testContext()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	lease, err := (&contractFake{}).IssueLease(c, h, LeaseOperation, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Validate(time.Now()); err != nil {
		t.Fatal(err)
	}
	if lease.ContextDigest != ContextDigest(c.RequestContext) {
		t.Fatal("lease is not bound to request context")
	}
}

func TestHandleAndContextRejectIncompleteOrUnknownValues(t *testing.T) {
	for _, h := range []Handle{{}, {ID: "x", Kind: "CLOUD_KMS", Version: "v1", Tenant: "t", Region: "r"}} {
		if !errors.Is(h.Validate(), ErrInvalidHandle) {
			t.Fatalf("handle accepted: %+v", h)
		}
	}
	if !errors.Is((RequestContext{}).Validate(), ErrInvalidContext) {
		t.Fatal("empty context accepted")
	}
}

func TestCustodyTypesContainNoMaterialOrProviderLocator(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(Handle{}), reflect.TypeOf(Lease{}), reflect.TypeOf(Receipt{})} {
		for i := 0; i < typ.NumField(); i++ {
			name := typ.Field(i).Name
			for _, forbidden := range []string{"Value", "Secret", "PrivateKey", "Provider", "Locator"} {
				if name == forbidden {
					t.Fatalf("custody type %s exposes forbidden field %s", typ.Name(), name)
				}
			}
		}
	}
}

type contractFake struct{}

func (f *contractFake) Encrypt(c Context, h Handle, _ []byte) (Ciphertext, Receipt, error) {
	return Ciphertext{Handle: h, Algorithm: "test"}, Receipt{Handle: h, Operation: Encrypt, ContextDigest: ContextDigest(c.RequestContext), At: time.Now()}, nil
}
func (f *contractFake) Decrypt(c Context, h Handle, _ Ciphertext) ([]byte, Receipt, error) {
	return []byte("plaintext"), Receipt{Handle: h, Operation: Decrypt, ContextDigest: ContextDigest(c.RequestContext), At: time.Now()}, nil
}
func (f *contractFake) Sign(c Context, h Handle, _ []byte) (Signature, Receipt, error) {
	return Signature{Handle: h, Algorithm: "test"}, Receipt{Handle: h, Operation: Sign, ContextDigest: ContextDigest(c.RequestContext), At: time.Now()}, nil
}
func (f *contractFake) Verify(c Context, h Handle, _ []byte, _ Signature) (bool, Receipt, error) {
	return true, Receipt{Handle: h, Operation: Verify, ContextDigest: ContextDigest(c.RequestContext), At: time.Now()}, nil
}
func (f *contractFake) IssueLease(c Context, h Handle, op Operation, ttl time.Duration) (Lease, error) {
	return Lease{ID: "lease-1", Handle: h, Operation: op, ExpiresAt: time.Now().Add(ttl), ContextDigest: ContextDigest(c.RequestContext)}, nil
}
func (f *contractFake) RenewLease(c Context, l Lease, ttl time.Duration) (Lease, error) {
	l.ExpiresAt = time.Now().Add(ttl)
	l.ContextDigest = ContextDigest(c.RequestContext)
	return l, nil
}
func (f *contractFake) Rotate(c Context, h Handle) (Handle, Receipt, error) {
	h.Version = "v3"
	return h, Receipt{Handle: h, Operation: Rotate, ContextDigest: ContextDigest(c.RequestContext), At: time.Now()}, nil
}
func (f *contractFake) Revoke(c Context, h Handle, _ string) (Receipt, error) {
	return Receipt{Handle: h, Operation: Revoke, ContextDigest: ContextDigest(c.RequestContext), At: time.Now()}, nil
}
