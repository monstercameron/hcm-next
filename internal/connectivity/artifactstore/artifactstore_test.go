package artifactstore

import (
	"strings"
	"testing"
	"time"
)

func validArtifactPolicy() Policy {
	return Policy{
		Endpoint: "https://objects.cell-a.internal", Region: "us-east-1", Private: true, TLSRequired: true, Versioning: true, ObjectLock: true,
		EncryptionRequired: true, TenantKeyReferences: true, RestoreReadVerification: true, DefaultRetention: 24 * time.Hour,
		Lifecycle: []LifecycleRule{{Class: "standard", ExpireAfter: 30 * 24 * time.Hour, Archive: true}},
	}
}

func validObject() Object {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return Object{TenantID: "tenant-a", Key: "artifact/one", VersionID: "v1", Region: "us-east-1", Digest: Digest([]byte("artifact")), KeyReference: "kms://tenant-a/artifacts", CreatedAt: created, RetainUntil: created.Add(48 * time.Hour)}
}

func TestTodo_IAC_007(t *testing.T) {
	policy := validArtifactPolicy()
	if err := Check(policy); err != nil {
		t.Fatal(err)
	}
	decision := EvaluatePut(policy, PutRequest{Object: validObject()})
	if !decision.Allowed || decision.Code != "OBJECT_WRITE_ALLOWED" {
		t.Fatalf("artifact write = %+v", decision)
	}
}

func TestTodo_IAC_007_Integration(t *testing.T) {
	policy := validArtifactPolicy()
	object := validObject()
	if action, err := Lifecycle(policy, object, object.CreatedAt.Add(time.Hour)); err != nil || action != LifecycleRetain {
		t.Fatalf("retention lifecycle = %s, err=%v", action, err)
	}
	object.Hold = true
	if action, err := Lifecycle(policy, object, object.CreatedAt.Add(90*24*time.Hour)); err != nil || action != LifecycleRetain {
		t.Fatalf("hold lifecycle = %s, err=%v", action, err)
	}
}

func TestTodo_IAC_007_Security(t *testing.T) {
	policy := validArtifactPolicy()
	object := validObject()
	object.KeyReference = "kms://tenant-b/artifacts"
	if decision := EvaluatePut(policy, PutRequest{Object: object}); decision.Allowed || decision.Code != "TENANT_KEY_MISMATCH" {
		t.Fatalf("cross-tenant key was admitted: %+v", decision)
	}
	object = validObject()
	object.Region = "us-west-2"
	if decision := EvaluatePut(policy, PutRequest{Object: object}); decision.Allowed || decision.Code != "REGION_MISMATCH" {
		t.Fatalf("wrong-region object was admitted: %+v", decision)
	}
	object = validObject()
	object.Key = "../other-tenant/secret"
	if decision := EvaluatePut(policy, PutRequest{Object: object}); decision.Allowed {
		t.Fatalf("escaping object key was admitted: %+v", decision)
	}
}

func TestTodo_IAC_007_Recovery(t *testing.T) {
	policy := validArtifactPolicy()
	object := validObject()
	object.RetainUntil = object.CreatedAt.Add(time.Hour)
	now := object.CreatedAt.Add(31 * 24 * time.Hour)
	action, err := Lifecycle(policy, object, now)
	if err != nil || action != LifecycleArchive {
		t.Fatalf("expired object lifecycle = %s, err=%v", action, err)
	}
}

func TestTodo_IAC_007_Mutation(t *testing.T) {
	mutations := []func(*Policy){
		func(p *Policy) { p.Private = false },
		func(p *Policy) { p.ObjectLock = false },
		func(p *Policy) { p.EncryptionRequired = false },
		func(p *Policy) { p.TenantKeyReferences = false },
	}
	for i, mutate := range mutations {
		policy := validArtifactPolicy()
		mutate(&policy)
		if err := Check(policy); err == nil {
			t.Errorf("mutation %d unexpectedly passed", i)
		}
	}
	object := validObject()
	object.RetainUntil = object.CreatedAt.Add(time.Hour)
	if decision := EvaluatePut(validArtifactPolicy(), PutRequest{Object: object}); decision.Allowed || decision.Code != "RETENTION_TOO_SHORT" {
		t.Fatalf("short retention was admitted: %+v", decision)
	}
	if got := Explain(); !strings.Contains(got, "immutable") {
		t.Fatalf("explanation = %q", got)
	}
}
