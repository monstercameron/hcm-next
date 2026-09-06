package payroll

import (
	"context"
	"errors"
	"testing"
)

type testTenant string

func (t testTenant) String() string { return string(t) }

func TestTodo_PERSIST_PAYROLL_001(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	tenant := testTenant("tenant-a")
	run := validPayrollRun(t)
	if err := store.SaveRun(ctx, tenant, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	got, err := store.LoadRun(ctx, tenant, run.RunID, run.Revision)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if got != run {
		t.Fatalf("loaded run = %+v, want %+v", got, run)
	}
}

func TestTodo_PERSIST_PAYROLL_001_Fault(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	tenant := testTenant("tenant-a")
	run := validPayrollRun(t)
	if err := store.SaveRun(ctx, tenant, run); err != nil {
		t.Fatalf("first SaveRun: %v", err)
	}
	if err := store.SaveRun(ctx, tenant, run); !errors.Is(err, ErrDuplicateRevision) {
		t.Fatalf("duplicate SaveRun = %v, want ErrDuplicateRevision", err)
	}
	next, err := run.Calculate("b" + run.CalculationInputDigest[1:])
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	next.SupersedesRevision = 0
	next.CanonicalDigest = ""
	if err := store.SaveRun(ctx, tenant, next); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale SaveRun = %v, want ErrStaleRevision", err)
	}
}

func TestTodo_PERSIST_PAYROLL_001_Mutation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	tenant := testTenant("tenant-a")
	run := validPayrollRun(t)
	if err := store.SaveRun(ctx, tenant, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	run.State = Released
	got, err := store.LoadRun(ctx, tenant, run.RunID, run.Revision)
	if err != nil {
		t.Fatalf("LoadRun after caller mutation: %v", err)
	}
	if got.State != Draft {
		t.Fatalf("stored run was mutated through caller value: %+v", got)
	}
}
