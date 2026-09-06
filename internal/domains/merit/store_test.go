package merit

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryStoreImplementsTenantStoreAndRejectsStaleRevision(t *testing.T) {
	var _ Store = new(MemoryStore)
	cycle := meritCycle(t)
	store := NewMemoryStore()
	if err := store.Save(context.Background(), "tenant-a", cycle); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "tenant-a", cycle); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate = %v, want ErrStoreDuplicate", err)
	}
	next, _, err := cycle.Propose("worker-1", meritDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "tenant-a", next); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Current(context.Background(), "tenant-b", cycle.CycleID); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("cross-tenant current = %v, want ErrStoreNotFound", err)
	}
}
