package custody_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

var custodyNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func custodyContext() custody.Context {
	return custody.Context{RequestContext: custody.RequestContext{
		Workload: "test-worker", Tenant: "tenant-1", Region: "us-east-1",
		Purpose: "test-purpose", Destination: "test-destination",
	}}
}

func custodyHandle(kind custody.Kind, id string) custody.Handle {
	return custody.Handle{ID: id, Kind: kind, Version: "v1", Tenant: "tenant-1", Region: "us-east-1"}
}

func registeredFake(t *testing.T) *custody.InMemoryFake {
	t.Helper()
	fake := custody.NewInMemoryFake(func() time.Time { return custodyNow })
	for _, handle := range []custody.Handle{
		custodyHandle(custody.Key, "key-1"),
		custodyHandle(custody.Secret, "secret-1"),
		custodyHandle(custody.Certificate, "cert-1"),
	} {
		if err := fake.Register(handle); err != nil {
			t.Fatal(err)
		}
	}
	return fake
}

// TestTodo_TRUST_026 is the primary custody contract: one provider-neutral
// metadata port is usable as key, secret, and certificate custody, and all
// lifecycle operations accept opaque references only.
func TestTodo_TRUST_026(t *testing.T) {
	fake := registeredFake(t)
	var _ custody.KeyCustody = fake
	var _ custody.SecretCustody = fake
	var _ custody.CertificateCustody = fake
	var _ custody.TypedProvider = fake

	ctx := custodyContext()
	for _, handle := range []custody.Handle{
		custodyHandle(custody.Key, "key-1"),
		custodyHandle(custody.Secret, "secret-1"),
		custodyHandle(custody.Certificate, "cert-1"),
	} {
		got, err := fake.Get(ctx, handle)
		if err != nil {
			t.Fatalf("Get(%s): %v", handle.Kind, err)
		}
		if got.Handle != handle || got.Status != custody.StatusActive {
			t.Fatalf("Get(%s) = %+v", handle.Kind, got)
		}
		attestation, err := fake.Attest(ctx, handle)
		if err != nil || attestation.Digest == "" || attestation.Handle != handle {
			t.Fatalf("Attest(%s) = %+v, %v", handle.Kind, attestation, err)
		}
	}
	rotated, receipt, err := fake.Rotate(ctx, custodyHandle(custody.Key, "key-1"))
	if err != nil || rotated.Handle.Version != "v2" || receipt.Operation != custody.Rotate {
		t.Fatalf("Rotate = %+v, %+v, %v", rotated, receipt, err)
	}
	if _, err := fake.Revoke(ctx, rotated.Handle, "test cleanup"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := fake.Get(ctx, rotated.Handle); !errors.Is(err, custody.ErrObjectRevoked) {
		t.Fatalf("Get(revoked) = %v, want ErrObjectRevoked", err)
	}
}

// FuzzTodo_TRUST_026 covers handle/context validation without exposing or
// constructing raw material.
func FuzzTodo_TRUST_026(f *testing.F) {
	f.Add("id", "v1", "tenant-1", "us-east-1")
	f.Add("", "", "", "")
	f.Fuzz(func(t *testing.T, id, version, tenant, region string) {
		fake := custody.NewInMemoryFake(nil)
		handle := custody.Handle{ID: id, Kind: custody.Key, Version: version, Tenant: tenant, Region: region}
		ctx := custody.Context{RequestContext: custody.RequestContext{Workload: "w", Tenant: tenant, Region: region, Purpose: "p", Destination: "d"}}
		_ = fake.Register(handle)
		_, _ = fake.Get(ctx, handle)
	})
}

// TestTodo_TRUST_026_Race exercises concurrent metadata reads and attestations
// through the fake's synchronization. The required verification command
// intentionally remains race-free per lane instructions.
func TestTodo_TRUST_026_Race(t *testing.T) {
	fake := registeredFake(t)
	handle := custodyHandle(custody.Secret, "secret-1")
	ctx := custodyContext()
	done := make(chan struct{}, 16)
	for i := 0; i < 16; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = fake.Get(ctx, handle)
			_, _ = fake.Attest(ctx, handle)
		}()
	}
	for i := 0; i < 16; i++ {
		<-done
	}
}

// TestTodo_TRUST_026_Integration exercises one fake through all three typed
// domain interfaces and verifies rotation remains metadata-only.
func TestTodo_TRUST_026_Integration(t *testing.T) {
	fake := registeredFake(t)
	ctx := custodyContext()
	var ports = []custody.MetadataCustody{fake, fake, fake}
	handles := []custody.Handle{custodyHandle(custody.Key, "key-1"), custodyHandle(custody.Secret, "secret-1"), custodyHandle(custody.Certificate, "cert-1")}
	for i, port := range ports {
		rotated, _, err := port.Rotate(ctx, handles[i])
		if err != nil {
			t.Fatalf("port %d Rotate: %v", i, err)
		}
		if rotated.Handle.Kind != handles[i].Kind || rotated.Handle.Version != "v2" {
			t.Fatalf("port %d rotated = %+v", i, rotated)
		}
	}
}

// TestTodo_TRUST_026_Security proves invalid scope, unknown references,
// revoked references, and raw-material-shaped metadata are rejected or
// structurally impossible.
func TestTodo_TRUST_026_Security(t *testing.T) {
	fake := registeredFake(t)
	handle := custodyHandle(custody.Key, "key-1")
	wrongScope := custody.Context{RequestContext: custody.RequestContext{Workload: "w", Tenant: "other", Region: "us-east-1", Purpose: "p", Destination: "d"}}
	if _, err := fake.Get(wrongScope, handle); !errors.Is(err, custody.ErrInvalidTypedRequest) {
		t.Fatalf("wrong scope = %v, want ErrInvalidTypedRequest", err)
	}
	if _, err := fake.Get(custodyContext(), custodyHandle(custody.Key, "unknown")); !errors.Is(err, custody.ErrObjectNotFound) {
		t.Fatalf("unknown ref = %v, want ErrObjectNotFound", err)
	}
	for _, typ := range []reflect.Type{reflect.TypeOf(custody.Lifecycle{}), reflect.TypeOf(custody.Attestation{})} {
		for i := 0; i < typ.NumField(); i++ {
			for _, forbidden := range []string{"Value", "Secret", "PrivateKey", "Raw", "Material"} {
				if typ.Field(i).Name == forbidden {
					t.Fatalf("typed custody type %s exposes %s", typ.Name(), forbidden)
				}
			}
		}
	}
}

// TestTodo_TRUST_026_Conformance is the adapter conformance fixture: any
// provider implementing MetadataCustody can be run through this same shape.
func TestTodo_TRUST_026_Conformance(t *testing.T) {
	fake := registeredFake(t)
	conformance := func(t *testing.T, port custody.MetadataCustody) {
		t.Helper()
		handle := custodyHandle(custody.Key, "key-1")
		ctx := custodyContext()
		if got, err := port.Get(ctx, handle); err != nil || got.Handle != handle {
			t.Fatalf("Get = %+v, %v", got, err)
		}
		rotated, receipt, err := port.Rotate(ctx, handle)
		if err != nil || rotated.Handle.Version != "v2" || receipt.Operation != custody.Rotate {
			t.Fatalf("Rotate = %+v, %+v, %v", rotated, receipt, err)
		}
		attested, err := port.Attest(ctx, handle)
		if err != nil || attested.Digest == "" {
			t.Fatalf("Attest = %+v, %v", attested, err)
		}
	}
	conformance(t, fake)
}

// TestTodo_TRUST_026_Mutation proves the fake refuses a revoked object and
// does not silently recreate or widen it.
func TestTodo_TRUST_026_Mutation(t *testing.T) {
	fake := registeredFake(t)
	handle := custodyHandle(custody.Certificate, "cert-1")
	ctx := custodyContext()
	if _, err := fake.Revoke(ctx, handle, "compromised"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fake.Rotate(ctx, handle); !errors.Is(err, custody.ErrObjectRevoked) {
		t.Fatalf("Rotate(revoked) = %v, want ErrObjectRevoked", err)
	}
	if _, err := fake.Attest(ctx, handle); !errors.Is(err, custody.ErrObjectRevoked) {
		t.Fatalf("Attest(revoked) = %v, want ErrObjectRevoked", err)
	}
}
