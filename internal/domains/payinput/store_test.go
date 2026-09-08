package payinput

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryStoreImplementsPayInputStore(t *testing.T) {
	var _ Store = NewMemoryStore()
	if _, err := NewMemoryStore().LoadDefinition(context.Background(), "tenant", "missing", 1); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("missing definition error = %v", err)
	}
}

func TestMemoryStoreDefinitionLifecycleAndTenantIsolation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	first := validDefinition(t, StatePublished)
	if err := store.SaveDefinition(ctx, "tenant-a", first, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDefinition(ctx, "tenant-a", first, 0); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate err=%v", err)
	}
	if _, err := store.LoadDefinition(ctx, "tenant-b", first.id(), 1); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("cross-tenant load err=%v", err)
	}
	second, err := first.NewVersion("v2", payInputInterval(t, "2026-02-01", "2026-03-01"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SaveDefinition(ctx, "tenant-a", second, 99); !errors.Is(err, ErrStoreStaleCAS) {
		t.Fatalf("stale err=%v", err)
	} else {
		var typed *StoreError
		if !errors.As(err, &typed) || typed.Expected != "99" || typed.Actual != "1" {
			t.Fatalf("stale detail=%+v", typed)
		}
	}
	if err = store.SaveDefinition(ctx, "tenant-a", second, 1); err != nil {
		t.Fatal(err)
	}
	current, err := store.CurrentDefinition(ctx, "tenant-a", first.id())
	if err != nil || current.Revision != 2 {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	revisions, err := store.ListDefinitionRevisions(ctx, "tenant-a", first.id())
	if err != nil || len(revisions) != 2 || revisions[0].Revision != 1 || revisions[1].Revision != 2 {
		t.Fatalf("revisions=%+v err=%v", revisions, err)
	}
	loaded, err := store.LoadDefinition(ctx, "tenant-a", first.id(), 1)
	if err != nil || loaded.CanonicalDigest != first.CanonicalDigest {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func TestMemoryStoreAssignmentBindingListingAndRefusals(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	d := validDefinition(t, StatePublished)
	if err := store.SaveDefinition(ctx, "tenant-a", d, 0); err != nil {
		t.Fatal(err)
	}
	a2 := calcAssignment(t, d, "a-2", "20.00", RecurrencePerPayroll)
	a1 := calcAssignment(t, d, "a-1", "10.00", RecurrencePerPayroll)
	for _, assignment := range []WorkerAssignment{a2, a1} {
		if err := store.SaveAssignment(ctx, "tenant-a", assignment); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveAssignment(ctx, "tenant-a", a1); !errors.Is(err, ErrStoreDuplicate) {
		t.Fatalf("duplicate assignment err=%v", err)
	}
	if err := store.SaveAssignment(ctx, "tenant-b", a1); !errors.Is(err, ErrStoreReference) {
		t.Fatalf("cross-tenant reference err=%v", err)
	}
	loaded, err := store.LoadAssignment(ctx, "tenant-a", "a-1")
	if err != nil || loaded.DefinitionDigest != d.CanonicalDigest {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	listed, err := store.ListAssignments(ctx, "tenant-a", "worker-1")
	if err != nil || len(listed) != 2 || listed[0].id() != "a-1" || listed[1].id() != "a-2" {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	if _, err := store.LoadAssignment(ctx, "tenant-b", "a-1"); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("cross-tenant assignment err=%v", err)
	}
	if _, err := store.ListAssignments(ctx, "tenant-a", "missing"); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("missing list err=%v", err)
	}
}

func TestMemoryStoreRejectsInvalidCalls(t *testing.T) {
	ctx := context.Background()
	var nilStore *MemoryStore
	d := validDefinition(t, StatePublished)
	a := calcAssignment(t, d, "a-1", "10.00", RecurrencePerPayroll)
	checks := []error{
		nilStore.SaveDefinition(ctx, "tenant", d, 0),
		NewMemoryStore().SaveDefinition(ctx, "", d, 0),
		NewMemoryStore().SaveAssignment(ctx, "", a),
	}
	for _, err := range checks {
		if !errors.Is(err, ErrStoreInvalid) {
			t.Fatalf("invalid call err=%v", err)
		}
	}
	store := NewMemoryStore()
	if _, err := store.CurrentDefinition(ctx, "", d.id()); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("current invalid err=%v", err)
	}
	if _, err := store.ListDefinitionRevisions(ctx, "tenant", "missing"); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("revision list err=%v", err)
	}
	if _, err := store.LoadAssignment(ctx, "tenant", ""); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("load assignment err=%v", err)
	}
	if _, err := store.ListAssignments(ctx, "tenant", ""); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("list assignment err=%v", err)
	}
	if got := (*StoreError)(nil).Error(); got == "" {
		t.Fatal("nil error has no diagnostic")
	}
	if got := (&StoreError{Code: "UNKNOWN"}).Unwrap(); got != nil {
		t.Fatalf("unknown code unwrap=%v", got)
	}
}
