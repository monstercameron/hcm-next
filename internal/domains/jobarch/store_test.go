package jobarch

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestTodo_PERSIST_JOBARCH_001_MemoryStore(t *testing.T) {
	tenant := uuid.New()
	store := NewMemoryStore()
	architecture := validArchitecture(t)
	if err := store.Save(context.Background(), tenant.String(), architecture, ""); err != nil {
		t.Fatalf("Save initial architecture: %v", err)
	}
	if err := store.Save(context.Background(), tenant.String(), architecture, "r1"); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("Save duplicate = %v, want ErrStoreDuplicate", err)
	}

	current, err := store.Current(context.Background(), tenant.String(), architecture.ID)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current.CanonicalDigest != architecture.CanonicalDigest || current.Profiles[0].Title != "Engineer" {
		t.Fatalf("Current = %+v, want the saved graph", current)
	}

	next := architecture
	next.Revision = "r2"
	next.SupersedesRevision = "r1"
	next, err = NewArchitectureRevision(next)
	if err != nil {
		t.Fatalf("NewArchitectureRevision successor: %v", err)
	}
	if err := store.Save(context.Background(), tenant.String(), next, "wrong"); !errors.Is(err, ErrStoreStaleCAS) {
		t.Fatalf("Save stale = %v, want ErrStoreStaleCAS", err)
	}
	if err := store.Save(context.Background(), tenant.String(), next, "r1"); err != nil {
		t.Fatalf("Save successor: %v", err)
	}
	list, err := store.List(context.Background(), tenant.String(), architecture.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("List = %d rows, err=%v; want two revisions", len(list), err)
	}
}

func TestTodo_PERSIST_JOBARCH_001_MemoryStore_TenantIsolation(t *testing.T) {
	store := NewMemoryStore()
	architecture := validArchitecture(t)
	tenantA, tenantB := uuid.New(), uuid.New()
	if err := store.Save(context.Background(), tenantA.String(), architecture, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), tenantB.String(), architecture.ID, architecture.Revision); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("cross-tenant Load = %v, want ErrStoreNotFound", err)
	}
}
