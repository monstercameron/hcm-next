package reconcile

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func memJob(tenant uuid.UUID, effectRef, policyRef string, at time.Time) Job {
	id := JobID(tenant, effectRef, policyRef)
	return Job{
		TenantID: tenant, JobID: id, EffectRef: effectRef, EffectID: "node.x", PolicyRef: policyRef,
		IntendedRef: "proposal:1", RequiredFreshness: "FRESH",
		NextCheckAt: at, Deadline: at.Add(time.Hour), Owner: "workload:x#replica:1", SLARef: "sla:1",
		RepairPolicy: "NONE", Status: StatusPending, Version: 1, CreatedAt: at, UpdatedAt: at,
	}
}

func TestMemoryStore_CreateIsIdempotentByNaturalKey(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	tenant := uuid.New()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	job := memJob(tenant, "effect-1", "policy-1", at)

	first, existing, err := m.Create(ctx, nil, job)
	if err != nil || existing {
		t.Fatalf("first create: job=%+v existing=%v err=%v", first, existing, err)
	}

	duplicate := job
	duplicate.IntendedRef = "proposal:2" // a caller retrying with different in-flight state
	second, existing, err := m.Create(ctx, nil, duplicate)
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if !existing {
		t.Fatal("a duplicate trigger for the same effect and policy did not report existing")
	}
	if second.JobID != first.JobID || second.IntendedRef != first.IntendedRef {
		t.Fatalf("duplicate create returned %+v, want the original %+v unchanged", second, first)
	}
}

func TestMemoryStore_LoadNotFound(t *testing.T) {
	m := NewMemoryStore()
	_, err := m.Load(context.Background(), nil, uuid.New(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("loading a job that was never created: err = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_AdvanceIsCompareAndSwap(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	tenant := uuid.New()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	job := memJob(tenant, "effect-1", "policy-1", at)
	stored, _, err := m.Create(ctx, nil, job)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	next := stored
	next.Status = StatusObserving
	next.ObservationAttempts = 1
	if err := m.Advance(ctx, nil, next, stored.Version); err != nil {
		t.Fatalf("advance at the correct version: %v", err)
	}
	reloaded, err := m.Load(ctx, nil, tenant, stored.JobID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Status != StatusObserving || reloaded.ObservationAttempts != 1 || reloaded.Version != stored.Version+1 {
		t.Fatalf("reloaded job = %+v, want the advanced state at version %d", reloaded, stored.Version+1)
	}

	// A second advance presenting the now-stale version is refused, and the
	// row is unchanged.
	stale := next
	stale.Status = StatusPass
	err = m.Advance(ctx, nil, stale, stored.Version)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("advance at a stale version: err = %v, want ErrVersionConflict", err)
	}
	unchanged, _ := m.Load(ctx, nil, tenant, stored.JobID)
	if unchanged.Status != StatusObserving {
		t.Fatalf("a refused advance mutated the row anyway: %+v", unchanged)
	}
}

func TestMemoryStore_DueOrdersOpenJobsAndExcludesTerminal(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	tenant := uuid.New()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	later := memJob(tenant, "effect-later", "policy-1", base.Add(2*time.Hour))
	sooner := memJob(tenant, "effect-sooner", "policy-1", base.Add(time.Hour))
	settled := memJob(tenant, "effect-settled", "policy-1", base)
	settled.Status = StatusPass

	for _, j := range []Job{later, sooner, settled} {
		if _, _, err := m.Create(ctx, nil, j); err != nil {
			t.Fatalf("create %s: %v", j.EffectRef, err)
		}
	}

	due, err := m.Due(ctx, nil, tenant, base.Add(3*time.Hour), 0)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 2 {
		t.Fatalf("%d due jobs, want 2 (the terminal one must be excluded)", len(due))
	}
	if due[0].EffectRef != "effect-sooner" || due[1].EffectRef != "effect-later" {
		t.Fatalf("due order = [%s, %s], want soonest-check first", due[0].EffectRef, due[1].EffectRef)
	}

	none, err := m.Due(ctx, nil, tenant, base, 0)
	if err != nil {
		t.Fatalf("due before anything is due: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("%d jobs due before their next-check instant, want 0", len(none))
	}
}

func TestMemoryStore_TwoTenantsDoNotShareJobs(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	t1, t2 := uuid.New(), uuid.New()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if _, _, err := m.Create(ctx, nil, memJob(t1, "effect-1", "policy-1", at)); err != nil {
		t.Fatalf("create for tenant 1: %v", err)
	}
	due, err := m.Due(ctx, nil, t2, at.Add(time.Hour), 0)
	if err != nil {
		t.Fatalf("due for tenant 2: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("tenant 2 saw %d jobs belonging to tenant 1", len(due))
	}
}
