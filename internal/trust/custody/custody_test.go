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

func TestHandleAndContext_ValidateEveryBoundary(t *testing.T) {
	for _, kind := range []Kind{Secret, Key, Certificate} {
		h := testHandle()
		h.Kind = kind
		if err := h.Validate(); err != nil {
			t.Fatalf("kind %q rejected: %v", kind, err)
		}
	}
	for _, field := range []string{"ID", "Version", "Tenant", "Region"} {
		h := testHandle()
		switch field {
		case "ID":
			h.ID = " \t"
		case "Version":
			h.Version = ""
		case "Tenant":
			h.Tenant = " "
		case "Region":
			h.Region = ""
		}
		if !errors.Is(h.Validate(), ErrInvalidHandle) {
			t.Fatalf("empty %s accepted: %+v", field, h)
		}
	}
	base := testContext()
	for _, field := range []string{"Workload", "Tenant", "Region", "Purpose", "Destination"} {
		c := base
		switch field {
		case "Workload":
			c.Workload = ""
		case "Tenant":
			c.Tenant = " "
		case "Region":
			c.Region = ""
		case "Purpose":
			c.Purpose = ""
		case "Destination":
			c.Destination = "\t"
		}
		if !errors.Is(c.Validate(), ErrInvalidContext) {
			t.Fatalf("empty %s accepted: %+v", field, c)
		}
		if !errors.Is((Context{RequestContext: c.RequestContext}).Validate(), ErrInvalidContext) {
			t.Fatalf("Context.Validate accepted empty %s", field)
		}
	}
}

func TestLease_ValidateRejectsMalformedAndExpiredValues(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	base := Lease{ID: "lease-1", Handle: testHandle(), Operation: Encrypt, ExpiresAt: now.Add(time.Minute)}
	for _, op := range []Operation{Encrypt, Decrypt, Sign, Verify, LeaseOperation, Rotate, Revoke} {
		lease := base
		lease.Operation = op
		if err := lease.Validate(now); err != nil {
			t.Fatalf("operation %q rejected: %v", op, err)
		}
	}
	for _, mutate := range []func(*Lease){
		func(l *Lease) { l.ID = "" },
		func(l *Lease) { l.Handle = Handle{} },
		func(l *Lease) { l.Operation = "unknown" },
		func(l *Lease) { l.ExpiresAt = time.Time{} },
	} {
		lease := base
		mutate(&lease)
		if !errors.Is(lease.Validate(now), ErrInvalidLease) {
			t.Fatalf("malformed lease accepted: %+v", lease)
		}
	}
	for _, expiry := range []time.Time{now, now.Add(-time.Nanosecond)} {
		lease := base
		lease.ExpiresAt = expiry
		if !errors.Is(lease.Validate(now), ErrExpired) {
			t.Fatalf("expiry %s returned %v, want ErrExpired", expiry, lease.Validate(now))
		}
	}
}

func TestContextDigest_IsStableAndBindsEveryDimension(t *testing.T) {
	base := testContext().RequestContext
	firstContextDigest, secondContextDigest := ContextDigest(base), ContextDigest(base)
	if firstContextDigest != secondContextDigest {
		t.Fatal("digest is not stable")
	}
	for _, mutate := range []func(*RequestContext){
		func(c *RequestContext) { c.Workload = "other" },
		func(c *RequestContext) { c.Tenant = "other" },
		func(c *RequestContext) { c.Region = "other" },
		func(c *RequestContext) { c.Purpose = "other" },
		func(c *RequestContext) { c.Destination = "other" },
	} {
		changed := base
		mutate(&changed)
		if ContextDigest(base) == ContextDigest(changed) {
			t.Fatalf("digest did not change for %+v", changed)
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
