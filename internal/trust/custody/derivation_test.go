package custody

import (
	"errors"
	"testing"
	"time"
)

func derivationContext() Context {
	return Context{RequestContext: RequestContext{
		Workload: "derivation-worker", Tenant: "tenant-a", Region: "us-east",
		Purpose: "derive-token", Destination: "token-service",
	}}
}

func derivationHandle(kind Kind, id string) Handle {
	return Handle{ID: id, Kind: kind, Version: "v1", Tenant: "tenant-a", Region: "us-east"}
}

func TestInMemoryFake_Derive_ScopesOutputAndReceipt(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	fake := NewInMemoryFake(func() time.Time { return now })
	handle := derivationHandle(Key, "key-1")
	if err := fake.Register(handle); err != nil {
		t.Fatal(err)
	}

	first, receipt, err := fake.Derive(derivationContext(), handle, []byte("purpose-a"))
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	second, _, err := fake.Derive(derivationContext(), handle, []byte("purpose-a"))
	if err != nil {
		t.Fatalf("Derive (repeat): %v", err)
	}
	if first.Handle != handle || first.Algorithm != "HMAC-SHA256" || len(first.Output) != 32 {
		t.Fatalf("derived value = %+v, want scoped HMAC output", first)
	}
	if string(first.Output) != string(second.Output) {
		t.Fatal("same handle and label did not produce deterministic output")
	}
	if receipt.Handle != handle || receipt.Operation != Sign || receipt.ContextDigest != ContextDigest(derivationContext().RequestContext) || !receipt.At.Equal(now) {
		t.Fatalf("receipt = %+v, want bound sign receipt", receipt)
	}
	other, _, err := fake.Derive(derivationContext(), handle, []byte("purpose-b"))
	if err != nil {
		t.Fatalf("Derive (other label): %v", err)
	}
	if string(other.Output) == string(first.Output) {
		t.Fatal("different derivation labels produced the same output")
	}
	first.Output[0] ^= 0xff
	third, _, err := fake.Derive(derivationContext(), handle, []byte("purpose-a"))
	if err != nil {
		t.Fatalf("Derive after output mutation: %v", err)
	}
	if string(third.Output) != string(second.Output) {
		t.Fatal("mutating returned output changed provider state")
	}
}

func TestInMemoryFake_Derive_RejectsInvalidScopeKindAndLifecycle(t *testing.T) {
	fake := NewInMemoryFake(func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) })
	key := derivationHandle(Key, "key-1")
	if err := fake.Register(key); err != nil {
		t.Fatal(err)
	}
	wrongScope := derivationContext()
	wrongScope.Tenant = "tenant-b"
	if _, _, err := fake.Derive(wrongScope, key, []byte("label")); !errors.Is(err, ErrInvalidTypedRequest) {
		t.Fatalf("wrong tenant = %v, want ErrInvalidTypedRequest", err)
	}
	if _, _, err := fake.Derive(derivationContext(), derivationHandle(Secret, "secret-1"), []byte("label")); !errors.Is(err, ErrWrongObjectKind) {
		t.Fatalf("secret derivation = %v, want ErrWrongObjectKind", err)
	}
	if _, _, err := fake.Derive(derivationContext(), derivationHandle(Key, "missing"), []byte("label")); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("unknown key derivation = %v, want ErrObjectNotFound", err)
	}
	if _, err := fake.Revoke(derivationContext(), key, "compromised"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fake.Derive(derivationContext(), key, []byte("label")); !errors.Is(err, ErrObjectRevoked) {
		t.Fatalf("revoked key derivation = %v, want ErrObjectRevoked", err)
	}
}
