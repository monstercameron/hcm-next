package appointment

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMemoryStoreRepository(t *testing.T) {
	store := NewMemoryStore()
	tenant := values.TenantId("tenant-a")
	requirement := typedRepositoryRequirement(t)
	if err := store.PutRequirement(context.Background(), tenant, requirement); err != nil {
		t.Fatal(err)
	}
	if err := store.PutRequirement(context.Background(), tenant, requirement); !errors.Is(err, ErrDuplicateRevision) {
		t.Fatalf("duplicate requirement = %v", err)
	}
	loaded, err := store.GetRequirement(context.Background(), tenant, requirement.ID, requirement.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CanonicalDigest != requirement.CanonicalDigest {
		t.Fatalf("digest = %q, want %q", loaded.CanonicalDigest, requirement.CanonicalDigest)
	}
	resource := typedAppointmentRequirement(t).RequiredResources[0]
	resourceType := ResourceType{ID: resource.ResourceTypeRef.Id, Version: "v1", Revision: 1, Name: "video room", Kind: ResourceCapability, Qualification: "video", PrivacyClass: "candidate-confidential", CapacityMode: CapacityExclusive}
	if err := store.PutResourceType(context.Background(), tenant, resourceType); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetResourceType(context.Background(), tenant, "missing", 1); !errors.Is(err, ErrResourceTypeNotFound) {
		t.Fatalf("missing resource = %v", err)
	}
}

func typedRepositoryRequirement(t *testing.T) Requirement {
	t.Helper()
	requirement := typedAppointmentRequirement(t)
	requirement, err := NewAppointmentRequirement(requirement)
	if err != nil {
		t.Fatal(err)
	}
	return requirement
}
