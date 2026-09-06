package pseudonym_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/pseudonym"
	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

var pseudonymNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func pseudonymContext() custody.Context {
	return custody.Context{RequestContext: custody.RequestContext{Workload: "case-worker", Tenant: "tenant-1", Region: "us-east-1", Purpose: "case-intake", Destination: "case"}}
}

func pseudonymService(t *testing.T) *pseudonym.Service {
	t.Helper()
	fake := custody.NewInMemoryFake(func() time.Time { return pseudonymNow })
	key := custody.Handle{ID: "pseudonym-key", Kind: custody.Key, Version: "v1", Tenant: "tenant-1", Region: "us-east-1"}
	if err := fake.Register(key); err != nil {
		t.Fatal(err)
	}
	service, err := pseudonym.New(fake, key)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func request(scope string) pseudonym.GenerateRequest {
	return pseudonym.GenerateRequest{Subject: "subject-123", Scope: scope, Purpose: "case-intake"}
}

// TestTodo_ANON_002 proves stable same-scope identifiers, scope separation,
// generation rotation, and custody-encrypted re-identification.
func TestTodo_ANON_002(t *testing.T) {
	service := pseudonymService(t)
	ctx := pseudonymContext()
	one, err := service.Generate(ctx, request("program-a"))
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := service.Generate(ctx, request("program-a"))
	if err != nil || one.ID != repeat.ID {
		t.Fatalf("same scope was not stable: %+v, %+v, %v", one, repeat, err)
	}
	other, err := service.Generate(ctx, request("program-b"))
	if err != nil || one.ID == other.ID {
		t.Fatalf("cross-scope identifiers correlated: %q and %q", one.ID, other.ID)
	}
	if _, err := service.Rotate(ctx); err != nil {
		t.Fatal(err)
	}
	rotated, err := service.Generate(ctx, request("program-a"))
	if err != nil || rotated.ID == one.ID || rotated.Generation <= one.Generation {
		t.Fatalf("rotation did not create a new generation: %+v, %v", rotated, err)
	}
	subject, evidence, err := service.Reidentify(ctx, pseudonym.ReidentifyRequest{Pseudonym: one, RequestedBy: "worker-a", Approver: "privacy-officer", EvidenceDigest: "sha256:evidence"})
	if err != nil || subject != "subject-123" || evidence.Operation != "reidentify" {
		t.Fatalf("reidentify = %q, %+v, %v", subject, evidence, err)
	}
}

// TestTodo_ANON_002_Security proves the service rejects missing evidence,
// self-approval, tenant confusion, and key-like fields in public results.
func TestTodo_ANON_002_Security(t *testing.T) {
	service := pseudonymService(t)
	ctx := pseudonymContext()
	id, err := service.Generate(ctx, request("program-a"))
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []pseudonym.ReidentifyRequest{
		{Pseudonym: id, RequestedBy: "alice", Approver: "alice", EvidenceDigest: "e"},
		{Pseudonym: id, RequestedBy: "alice", Approver: "bob"},
	} {
		if _, _, err := service.Reidentify(ctx, req); !errors.Is(err, pseudonym.ErrReidentificationDenied) {
			t.Fatalf("request = %+v, err = %v", req, err)
		}
	}
	wrong := ctx
	wrong.Tenant = "tenant-2"
	if _, err := service.Generate(wrong, request("program-a")); !errors.Is(err, pseudonym.ErrInvalidRequest) {
		t.Fatalf("wrong tenant = %v", err)
	}
	for _, typ := range []reflect.Type{reflect.TypeOf(pseudonym.Pseudonym{}), reflect.TypeOf(pseudonym.Evidence{})} {
		for i := 0; i < typ.NumField(); i++ {
			for _, forbidden := range []string{"Subject", "Raw", "Material", "PrivateKey"} {
				if typ.Field(i).Name == forbidden {
					t.Fatalf("public type %s exposes %s", typ.Name(), forbidden)
				}
			}
		}
	}
}

// TestTodo_ANON_002_Mutation ensures a modified identifier cannot open an
// encrypted mapping and cannot fall back to a plaintext lookup.
func TestTodo_ANON_002_Mutation(t *testing.T) {
	service := pseudonymService(t)
	ctx := pseudonymContext()
	id, err := service.Generate(ctx, request("program-a"))
	if err != nil {
		t.Fatal(err)
	}
	id.ID = id.ID + "x"
	if _, _, err := service.Reidentify(ctx, pseudonym.ReidentifyRequest{Pseudonym: id, RequestedBy: "alice", Approver: "bob", EvidenceDigest: "evidence"}); !errors.Is(err, pseudonym.ErrPseudonymNotFound) {
		t.Fatalf("mutated identifier = %v", err)
	}
}
