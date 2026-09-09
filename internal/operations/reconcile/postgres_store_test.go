package reconcile_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
)

func newJobFixture(tenant uuid.UUID, effectRef, policyRef string, at time.Time) reconcile.Job {
	return reconcile.Job{
		TenantID: tenant, JobID: reconcile.JobID(tenant, effectRef, policyRef),
		EffectRef: effectRef, EffectID: "node.provision_seat", PolicyRef: policyRef,
		IntendedRef: "proposal:1", RequiredFreshness: "FRESH",
		NextCheckAt: at, Deadline: at.Add(24 * time.Hour),
		Owner: "workload:hcmnext-operations-reconcile#replica:reconcile-1", SLARef: "sla:1",
		RepairPolicy: "NONE", Status: reconcile.StatusPending, Version: 1, CreatedAt: at, UpdatedAt: at,
	}
}

func TestPostgresStore_CreateLoadAndDuplicateReturnsExisting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newReconcileFixture(t, db, "recon-store-basic")
	var store reconcile.PostgresStore

	job := newJobFixture(f.tenant, "effect-ref-1", "policy-1", fixedInstant)
	var stored reconcile.Job
	var existing bool
	f.do(t, func(tx dbport.Tx) error {
		var err error
		stored, existing, err = store.Create(ctx, tx, job)
		return err
	})
	if existing {
		t.Fatal("a first create reported itself as existing")
	}
	if stored.JobID != job.JobID || stored.Status != reconcile.StatusPending {
		t.Fatalf("stored job = %+v", stored)
	}

	var loaded reconcile.Job
	f.do(t, func(tx dbport.Tx) error {
		var err error
		loaded, err = store.Load(ctx, tx, f.tenant, job.JobID)
		return err
	})
	if loaded.EffectRef != job.EffectRef || loaded.PolicyRef != job.PolicyRef || loaded.Version != 1 {
		t.Fatalf("loaded job = %+v, want it to match what was created", loaded)
	}
	if !loaded.CreatedAt.Equal(fixedInstant) || !loaded.NextCheckAt.Equal(fixedInstant) {
		t.Fatalf("loaded timestamps = %+v, want %s", loaded, fixedInstant)
	}

	// A duplicate trigger for the same effect and policy returns the existing
	// row, never a parallel one.
	duplicate := newJobFixture(f.tenant, "effect-ref-1", "policy-1", fixedInstant.Add(time.Hour))
	duplicate.IntendedRef = "proposal:2"
	var second reconcile.Job
	var secondExisting bool
	f.do(t, func(tx dbport.Tx) error {
		var err error
		second, secondExisting, err = store.Create(ctx, tx, duplicate)
		return err
	})
	if !secondExisting {
		t.Fatal("a duplicate trigger did not report existing")
	}
	if second.JobID != stored.JobID || second.IntendedRef != stored.IntendedRef {
		t.Fatalf("duplicate create returned %+v, want the original %+v unchanged", second, stored)
	}

	var count int
	db.QueryRow(ctx, `SELECT count(*) FROM effect_reconciliation_job WHERE tenant_id = $1 AND effect_ref = $2`,
		f.tenant, "effect-ref-1").Scan(&count)
	if count != 1 {
		t.Fatalf("%d rows for one effect and policy, want exactly 1", count)
	}
}

func TestPostgresStore_AdvanceIsFencedByVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newReconcileFixture(t, db, "recon-store-cas")
	var store reconcile.PostgresStore

	job := newJobFixture(f.tenant, "effect-ref-2", "policy-1", fixedInstant)
	f.do(t, func(tx dbport.Tx) error {
		_, _, err := store.Create(ctx, tx, job)
		return err
	})

	next := job
	next.Status = reconcile.StatusObserving
	next.ObservationAttempts = 1
	next.UpdatedAt = fixedInstant.Add(time.Minute)
	f.do(t, func(tx dbport.Tx) error {
		return store.Advance(ctx, tx, next, 1)
	})

	var state string
	var version int64
	db.QueryRow(ctx, `SELECT job_state, job_version FROM effect_reconciliation_job WHERE tenant_id = $1 AND job_id = $2`,
		f.tenant, job.JobID).Scan(&state, &version)
	if state != "OBSERVING" || version != 2 {
		t.Fatalf("durable row = %s at version %d, want OBSERVING at version 2", state, version)
	}

	// Presenting the now-stale version 1 again is refused, and the row is
	// unchanged.
	stale := next
	stale.Status = reconcile.StatusPass
	err := f.try(func(tx dbport.Tx) error {
		return store.Advance(ctx, tx, stale, 1)
	})
	if !errors.Is(err, reconcile.ErrVersionConflict) {
		t.Fatalf("advance at a stale version: err = %v, want ErrVersionConflict", err)
	}
	db.QueryRow(ctx, `SELECT job_state, job_version FROM effect_reconciliation_job WHERE tenant_id = $1 AND job_id = $2`,
		f.tenant, job.JobID).Scan(&state, &version)
	if state != "OBSERVING" || version != 2 {
		t.Fatalf("a refused advance changed the row to %s at version %d", state, version)
	}
}

func TestPostgresStore_DueExcludesTerminalAndOrdersBySoonestCheck(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newReconcileFixture(t, db, "recon-store-due")
	var store reconcile.PostgresStore

	sooner := newJobFixture(f.tenant, "effect-sooner", "policy-1", fixedInstant.Add(time.Hour))
	later := newJobFixture(f.tenant, "effect-later", "policy-1", fixedInstant.Add(2*time.Hour))
	settled := newJobFixture(f.tenant, "effect-settled", "policy-1", fixedInstant)
	settled.Status = reconcile.StatusPass
	for _, j := range []reconcile.Job{later, sooner, settled} {
		f.do(t, func(tx dbport.Tx) error {
			_, _, err := store.Create(ctx, tx, j)
			return err
		})
	}

	var due []reconcile.Job
	f.do(t, func(tx dbport.Tx) error {
		var err error
		due, err = store.Due(ctx, tx, f.tenant, fixedInstant.Add(3*time.Hour), 0)
		return err
	})
	if len(due) != 2 {
		t.Fatalf("%d due jobs, want 2 (the settled one must be excluded)", len(due))
	}
	if due[0].EffectRef != "effect-sooner" || due[1].EffectRef != "effect-later" {
		t.Fatalf("due order = [%s, %s], want soonest-check first", due[0].EffectRef, due[1].EffectRef)
	}
}

// TestPostgresStore_TenantIsolation proves the effect_reconciliation_job table's
// RLS tenant_isolation policy actually fences reads, not merely the
// application-level tenant filter this store's queries already carry.
func TestPostgresStore_TenantIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	fa := newReconcileFixture(t, db, "recon-rls-a")
	fb := newReconcileFixture(t, db, "recon-rls-b")
	var store reconcile.PostgresStore

	job := newJobFixture(fa.tenant, "effect-rls", "policy-1", fixedInstant)
	fa.do(t, func(tx dbport.Tx) error {
		_, _, err := store.Create(ctx, tx, job)
		return err
	})

	// Tenant B's own scoped transaction cannot see tenant A's row, even
	// addressed by its exact id.
	err := fb.try(func(tx dbport.Tx) error {
		_, err := store.Load(ctx, tx, fa.tenant, job.JobID)
		return err
	})
	if !errors.Is(err, reconcile.ErrNotFound) {
		t.Fatalf("cross-tenant load: err = %v, want ErrNotFound (RLS must hide the row)", err)
	}
}
