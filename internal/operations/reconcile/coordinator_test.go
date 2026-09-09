package reconcile_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/effectgraph"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
)

// fixedObserver and fixedComparer are stand-ins for the real
// internal/connectivity/observe reader and RECON-002's not-yet-built
// comparison-policy engine. reconcile.Coordinator only ever reaches an
// external system or a policy decision through these two ports, which is
// exactly what TestTodo_RECON_001_Conformance below checks by scanning the
// package's own imports.
type fixedObserver struct {
	result reconcile.ObservationResult
	err    error
	calls  int
}

func (f *fixedObserver) Observe(ctx context.Context, ex reconcile.Executor, job reconcile.Job, now time.Time) (reconcile.ObservationResult, error) {
	f.calls++
	return f.result, f.err
}

type fixedComparer struct {
	verdict reconcile.Verdict
	err     error
	calls   int
}

func (f *fixedComparer) Compare(ctx context.Context, ex reconcile.Executor, job reconcile.Job, obs reconcile.ObservationResult, now time.Time) (reconcile.Verdict, error) {
	f.calls++
	return f.verdict, f.err
}

// TestTodo_RECON_001 is RECON-001's primary scenario: a committed mandatory
// effect gets exactly one job, a stale observation never ends polling, a
// resting UNKNOWN verdict stays due, a fresh determinate verdict settles the
// job, a settled job is never re-touched, and a restart reloads every
// mutable field from the row exactly as it was left.
func TestTodo_RECON_001(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newReconcileFixture(t, db, "recon-primary")
	var store reconcile.PostgresStore

	observer := &fixedObserver{}
	comparer := &fixedComparer{}
	coord := reconcile.Coordinator{Store: store, Observer: observer, Comparer: comparer, Fences: lease.Manager{}}

	effect := mandatoryEffect("effect/provision-seat-1", "NONE")
	trigger := reconcile.TriggerRequest{
		TenantID: f.tenant, Effect: effect, PolicyRef: "policy/reconcile-v1",
		IntendedRef: "proposal:1", RequiredFreshness: observe.FreshnessFresh,
		SLARef: "sla:seat-provisioning", Deadline: fixedInstant.Add(2 * time.Hour),
		Fence: f.fence, Now: fixedInstant,
	}

	// 1. Triggering a committed mandatory effect creates exactly one job.
	var triggered reconcile.Triggered
	f.do(t, func(tx dbport.Tx) error {
		var err error
		triggered, err = coord.Trigger(ctx, tx, trigger)
		return err
	})
	if triggered.Replay {
		t.Fatal("a first trigger reported itself as a replay")
	}
	if triggered.Job.Status != reconcile.StatusPending || triggered.Job.ObservationAttempts != 0 {
		t.Fatalf("triggered job = %+v, want a fresh PENDING job", triggered.Job)
	}
	jobID := triggered.Job.JobID

	// 2. A duplicate trigger for the same effect and policy returns the
	// existing job untouched, never a parallel one.
	dup := trigger
	dup.IntendedRef = "proposal:2"
	dup.Now = fixedInstant.Add(time.Minute)
	var replay reconcile.Triggered
	f.do(t, func(tx dbport.Tx) error {
		var err error
		replay, err = coord.Trigger(ctx, tx, dup)
		return err
	})
	if !replay.Replay || replay.Job.JobID != jobID || replay.Job.IntendedRef != "proposal:1" {
		t.Fatalf("duplicate trigger = %+v, want a replay of the original job", replay)
	}

	// 3. A stale observation is an attempt, but it never ends polling: the
	// job stays OBSERVING and the comparer is never even asked.
	t1 := fixedInstant.Add(5 * time.Minute)
	observer.result = reconcile.ObservationResult{Found: true, Freshness: observe.FreshnessStale}
	var polled reconcile.Polled
	f.do(t, func(tx dbport.Tx) error {
		var err error
		polled, err = coord.Poll(ctx, tx, reconcile.PollRequest{TenantID: f.tenant, JobID: jobID, Fence: f.fence, Now: t1})
		return err
	})
	if polled.Job.Status != reconcile.StatusObserving || polled.Job.ObservationAttempts != 1 {
		t.Fatalf("after a stale observation, job = %+v, want OBSERVING at attempt 1", polled.Job)
	}
	if comparer.calls != 0 {
		t.Fatalf("comparer was called %d times on a non-conclusive observation, want 0", comparer.calls)
	}
	if !polled.Job.NextCheckAt.After(t1) {
		t.Fatalf("a stale observation did not reschedule the next check: %+v", polled.Job)
	}

	// 4. A fresh observation the comparer cannot classify rests at UNKNOWN,
	// using the comparer's own requested recheck instant, and still counts
	// as an attempt.
	t2 := polled.Job.NextCheckAt
	unknownRecheck := t2.Add(10 * time.Minute)
	observer.result = reconcile.ObservationResult{Found: true, Freshness: observe.FreshnessFresh, CanonicalRef: "external:seat-42"}
	comparer.verdict = reconcile.Verdict{Status: reconcile.StatusUnknown, NextCheckAt: unknownRecheck}
	f.do(t, func(tx dbport.Tx) error {
		var err error
		polled, err = coord.Poll(ctx, tx, reconcile.PollRequest{TenantID: f.tenant, JobID: jobID, Fence: f.fence, Now: t2})
		return err
	})
	if polled.Job.Status != reconcile.StatusUnknown || polled.Job.ObservationAttempts != 2 {
		t.Fatalf("after an inconclusive fresh observation, job = %+v, want UNKNOWN at attempt 2", polled.Job)
	}
	if !polled.Job.NextCheckAt.Equal(unknownRecheck) {
		t.Fatalf("next check = %s, want the comparer's own requested instant %s", polled.Job.NextCheckAt, unknownRecheck)
	}
	if polled.Job.CanonicalRef != "external:seat-42" {
		t.Fatalf("canonical ref = %q, want it updated from the observation", polled.Job.CanonicalRef)
	}

	// 5. A determinate verdict on a fresh observation settles the job.
	t3 := unknownRecheck
	comparer.verdict = reconcile.Verdict{Status: reconcile.StatusPass}
	f.do(t, func(tx dbport.Tx) error {
		var err error
		polled, err = coord.Poll(ctx, tx, reconcile.PollRequest{TenantID: f.tenant, JobID: jobID, Fence: f.fence, Now: t3})
		return err
	})
	if polled.Job.Status != reconcile.StatusPass || polled.Job.ObservationAttempts != 3 {
		t.Fatalf("after a PASS verdict, job = %+v, want PASS at attempt 3", polled.Job)
	}

	// 6. A settled job is never re-touched: a later poll is a no-op and does
	// not reach either port again.
	observerCallsBefore, comparerCallsBefore := observer.calls, comparer.calls
	f.do(t, func(tx dbport.Tx) error {
		var err error
		polled, err = coord.Poll(ctx, tx, reconcile.PollRequest{TenantID: f.tenant, JobID: jobID, Fence: f.fence, Now: t3.Add(time.Hour)})
		return err
	})
	if !polled.NoOp || polled.Job.Status != reconcile.StatusPass {
		t.Fatalf("polling a settled job = %+v, want a no-op still reporting PASS", polled)
	}
	if observer.calls != observerCallsBefore || comparer.calls != comparerCallsBefore {
		t.Fatalf("a no-op poll still reached a port: observer %d->%d, comparer %d->%d",
			observerCallsBefore, observer.calls, comparerCallsBefore, comparer.calls)
	}

	// 7. Restart loses nothing: a brand-new coordinator and store reading the
	// same row reloads every mutable field exactly as it was left.
	restarted := reconcile.Coordinator{Store: reconcile.PostgresStore{}, Fences: lease.Manager{}}
	var reloaded reconcile.Job
	f.do(t, func(tx dbport.Tx) error {
		var err error
		reloaded, err = restarted.Store.Load(ctx, tx, f.tenant, jobID)
		return err
	})
	if reloaded.Status != reconcile.StatusPass || reloaded.ObservationAttempts != 3 ||
		reloaded.CanonicalRef != "external:seat-42" || !reloaded.Deadline.Equal(trigger.Deadline) ||
		reloaded.Owner != f.fence.Holder.HolderID() {
		t.Fatalf("reloaded job after restart = %+v, want the exact state the last advance left", reloaded)
	}

	// 8. An exhausted job settles to EXPIRED or REPAIR_REQUIRED depending on
	// the committed effect's own repair policy, and the row stays as
	// evidence rather than disappearing.
	shortDeadline := fixedInstant.Add(time.Minute)
	noRepair := mandatoryEffect("effect/provision-seat-2", "NONE")
	withRepair := mandatoryEffect("effect/provision-seat-3", "hcmnext.repair.reissue_provider_call/v1")

	for _, tc := range []struct {
		effect effectgraph.EffectNode
		want   reconcile.Status
	}{
		{noRepair, reconcile.StatusExpired},
		{withRepair, reconcile.StatusRepairRequired},
	} {
		var trig reconcile.Triggered
		f.do(t, func(tx dbport.Tx) error {
			var err error
			trig, err = coord.Trigger(ctx, tx, reconcile.TriggerRequest{
				TenantID: f.tenant, Effect: tc.effect, PolicyRef: "policy/reconcile-v1",
				IntendedRef: "proposal:1", RequiredFreshness: observe.FreshnessFresh,
				SLARef: "sla:seat-provisioning", Deadline: shortDeadline,
				Fence: f.fence, Now: fixedInstant,
			})
			return err
		})
		var exhausted reconcile.Polled
		f.do(t, func(tx dbport.Tx) error {
			var err error
			exhausted, err = coord.Poll(ctx, tx, reconcile.PollRequest{
				TenantID: f.tenant, JobID: trig.Job.JobID, Fence: f.fence, Now: shortDeadline.Add(time.Second),
			})
			return err
		})
		if exhausted.Job.Status != tc.want {
			t.Fatalf("exhausted job (repair_policy=%q) = %s, want %s", tc.effect.RepairPolicy, exhausted.Job.Status, tc.want)
		}
		if !exhausted.Job.Status.Terminal() {
			t.Fatalf("exhausted job %s did not report itself terminal", exhausted.Job.Status)
		}
		var rowCount int
		db.QueryRow(ctx, `SELECT count(*) FROM effect_reconciliation_job WHERE tenant_id = $1 AND job_id = $2`,
			f.tenant, trig.Job.JobID).Scan(&rowCount)
		if rowCount != 1 {
			t.Fatalf("exhausted job row count = %d, want 1 (an exhausted job must never disappear)", rowCount)
		}
	}
}

// TestTodo_RECON_001_Golden is a fixed-input, pure-logic vector: the same
// trigger and poll sequence over reconcile.MemoryStore always produces the
// same evidence digests, with no database and no clock of its own.
func TestTodo_RECON_001_Golden(t *testing.T) {
	tenant := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	holder := lease.Identity{WorkloadRef: "workload:golden", InstanceRef: "replica:golden-1"}
	fence := lease.Fence{TenantID: tenant, Resource: lease.Resource{Kind: lease.ResourceQueue, ID: "queue:golden"},
		LeaseID: uuid.MustParse("66666666-6666-4666-8666-666666666666"), Holder: holder, Token: 1}

	fences := memoryFenceVerifier{fence: fence}
	observer := &fixedObserver{result: reconcile.ObservationResult{Found: true, Freshness: observe.FreshnessStale}}
	comparer := &fixedComparer{verdict: reconcile.Verdict{Status: reconcile.StatusPass}}
	coord := reconcile.Coordinator{Store: reconcile.NewMemoryStore(), Observer: observer, Comparer: comparer, Fences: fences}

	at := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	effect := mandatoryEffect("effect/golden-1", "NONE")
	triggered, err := coord.Trigger(context.Background(), nil, reconcile.TriggerRequest{
		TenantID: tenant, Effect: effect, PolicyRef: "policy/golden-v1",
		IntendedRef: "proposal:golden", RequiredFreshness: observe.FreshnessFresh,
		SLARef: "sla:golden", Deadline: at.Add(time.Hour), Fence: fence, Now: at,
	})
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}

	polled, err := coord.Poll(context.Background(), nil, reconcile.PollRequest{
		TenantID: tenant, JobID: triggered.Job.JobID, Fence: fence, Now: at.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("poll: %v", err)
	}

	// The load-bearing golden property: given the identical fixed sequence,
	// re-running it from scratch reproduces byte-identical evidence digests.
	coord2 := reconcile.Coordinator{Store: reconcile.NewMemoryStore(), Observer: observer, Comparer: comparer, Fences: fences}
	triggered2, err := coord2.Trigger(context.Background(), nil, reconcile.TriggerRequest{
		TenantID: tenant, Effect: effect, PolicyRef: "policy/golden-v1",
		IntendedRef: "proposal:golden", RequiredFreshness: observe.FreshnessFresh,
		SLARef: "sla:golden", Deadline: at.Add(time.Hour), Fence: fence, Now: at,
	})
	if err != nil {
		t.Fatalf("trigger (rerun): %v", err)
	}
	polled2, err := coord2.Poll(context.Background(), nil, reconcile.PollRequest{
		TenantID: tenant, JobID: triggered2.Job.JobID, Fence: fence, Now: at.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("poll (rerun): %v", err)
	}
	if triggered.Evidence.Digest() != triggered2.Evidence.Digest() {
		t.Fatalf("trigger evidence digest is not reproducible: %s != %s", triggered.Evidence.Digest(), triggered2.Evidence.Digest())
	}
	if polled.Evidence.Digest() != polled2.Evidence.Digest() {
		t.Fatalf("poll evidence digest is not reproducible: %s != %s", polled.Evidence.Digest(), polled2.Evidence.Digest())
	}
	if triggered.Evidence.Digest() == "" || polled.Evidence.Digest() == "" {
		t.Fatal("evidence digest is empty")
	}
}

// memoryFenceVerifier is a [reconcile.FenceVerifier] for the golden test,
// which has no database to verify a real lease.Manager fence against.
type memoryFenceVerifier struct{ fence lease.Fence }

func (m memoryFenceVerifier) Verify(ctx context.Context, ex reconcile.Executor, fence lease.Fence, now time.Time) (lease.Held, error) {
	if fence.LeaseID != m.fence.LeaseID || fence.Token != m.fence.Token || fence.Holder != m.fence.Holder {
		return lease.Held{}, lease.ErrFenceStale
	}
	return lease.Held{Fence: fence, AcquiredAt: now, ExpiresAt: now.Add(time.Hour), Version: 1}, nil
}

// TestTodo_RECON_001_Race proves a job settles exactly once under concurrent
// polls presenting two independently valid fences: PostgreSQL's own row lock
// (LoadForUpdate) serializes the two callers, so the loser observes the
// already-terminal job and reports a no-op rather than racing the winner's
// write.
func TestTodo_RECON_001_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newReconcileFixture(t, db, "recon-race")
	var store reconcile.PostgresStore

	observer := &fixedObserver{result: reconcile.ObservationResult{Found: true, Freshness: observe.FreshnessFresh}}
	comparer := &fixedComparer{verdict: reconcile.Verdict{Status: reconcile.StatusPass}}
	coord := reconcile.Coordinator{Store: store, Observer: observer, Comparer: comparer, Fences: lease.Manager{}}

	effect := mandatoryEffect("effect/race-1", "NONE")
	var triggered reconcile.Triggered
	f.do(t, func(tx dbport.Tx) error {
		var err error
		triggered, err = coord.Trigger(ctx, tx, reconcile.TriggerRequest{
			TenantID: f.tenant, Effect: effect, PolicyRef: "policy/race-v1",
			IntendedRef: "proposal:1", RequiredFreshness: observe.FreshnessFresh,
			SLARef: "sla:race", Deadline: fixedInstant.Add(time.Hour), Fence: f.fence, Now: fixedInstant,
		})
		return err
	})

	// Two independent, individually valid fences on two different resources,
	// so neither blocks on the other's lease -- what serializes them is the
	// job row's own lock.
	fences := make([]lease.Fence, 0, 2)
	conns := make([]*pgxadapter.Conn, 0, 2)
	for _, suffix := range []string{"a", "b"} {
		conn := appConn(t, db)
		holder := lease.Identity{WorkloadRef: "workload:hcmnext-operations-reconcile", InstanceRef: "replica:race-" + suffix}
		resource := lease.Resource{Kind: lease.ResourceQueue, ID: "queue:reconciliation-race-" + suffix}
		var grant lease.Grant
		inTenantTx(t, conn, f.tenant, func(tx dbport.Tx) error {
			var err error
			grant, err = (lease.Manager{}).Acquire(ctx, tx, lease.AcquireRequest{
				TenantID: f.tenant, Resource: resource, Holder: holder, Now: fixedInstant, TTL: 24 * time.Hour,
			})
			return err
		})
		fences = append(fences, grant.Fence)
		conns = append(conns, conn)
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []reconcile.Polled
		errs    []error
	)
	start := make(chan struct{})
	for i := range fences {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			var out reconcile.Polled
			err := inTenantTxErr(conns[i], f.tenant, func(tx dbport.Tx) error {
				var perr error
				out, perr = coord.Poll(ctx, tx, reconcile.PollRequest{
					TenantID: f.tenant, JobID: triggered.Job.JobID, Fence: fences[i], Now: fixedInstant.Add(time.Minute),
				})
				return perr
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			results = append(results, out)
		}(i)
	}
	close(start)
	wg.Wait()

	if len(errs) != 0 {
		t.Fatalf("concurrent poll errors: %v", errs)
	}
	settled, noOps := 0, 0
	for _, r := range results {
		if r.NoOp {
			noOps++
		} else {
			settled++
		}
		if r.Job.Status != reconcile.StatusPass {
			t.Fatalf("a concurrent poll result did not report PASS: %+v", r.Job)
		}
	}
	if settled != 1 || noOps != 1 {
		t.Fatalf("settled=%d noOps=%d, want exactly one settle and one no-op", settled, noOps)
	}
	var state string
	var version int64
	db.QueryRow(ctx, `SELECT job_state, job_version FROM effect_reconciliation_job WHERE tenant_id = $1 AND job_id = $2`,
		f.tenant, triggered.Job.JobID).Scan(&state, &version)
	if state != "PASS" || version != 2 {
		t.Fatalf("durable row = %s at version %d, want PASS settled exactly once at version 2", state, version)
	}
}

// TestTodo_RECON_001_Fault proves every refusal leaves the durable job row
// exactly as it was.
func TestTodo_RECON_001_Fault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newReconcileFixture(t, db, "recon-fault")
	var store reconcile.PostgresStore

	observer := &fixedObserver{}
	comparer := &fixedComparer{}
	coord := reconcile.Coordinator{Store: store, Observer: observer, Comparer: comparer, Fences: lease.Manager{}}

	effect := mandatoryEffect("effect/fault-1", "NONE")
	var triggered reconcile.Triggered
	f.do(t, func(tx dbport.Tx) error {
		var err error
		triggered, err = coord.Trigger(ctx, tx, reconcile.TriggerRequest{
			TenantID: f.tenant, Effect: effect, PolicyRef: "policy/fault-v1",
			IntendedRef: "proposal:1", RequiredFreshness: observe.FreshnessFresh,
			SLARef: "sla:fault", Deadline: fixedInstant.Add(time.Hour), Fence: f.fence, Now: fixedInstant,
		})
		return err
	})
	jobID := triggered.Job.JobID

	rowState := func() (string, int64, int64) {
		var state string
		var version, attempts int64
		db.QueryRow(ctx, `SELECT job_state, job_version, observation_attempts FROM effect_reconciliation_job
			WHERE tenant_id = $1 AND job_id = $2`, f.tenant, jobID).Scan(&state, &version, &attempts)
		return state, version, attempts
	}

	// A superseded fence is refused, and the row is untouched.
	partition := lease.Resource{Kind: lease.ResourceQueue, ID: "queue:reconciliation-fault"}
	shortHolder := lease.Identity{WorkloadRef: "workload:hcmnext-operations-reconcile", InstanceRef: "replica:fault-short"}
	var short lease.Grant
	inTenantTx(t, f.conn, f.tenant, func(tx dbport.Tx) error {
		var err error
		short, err = (lease.Manager{}).Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: f.tenant, Resource: partition, Holder: shortHolder, Now: fixedInstant, TTL: time.Hour,
		})
		return err
	})
	takeoverAt := fixedInstant.Add(2 * time.Hour)
	var successor lease.Grant
	inTenantTx(t, f.conn, f.tenant, func(tx dbport.Tx) error {
		var err error
		successor, err = (lease.Manager{}).Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: f.tenant, Resource: partition,
			Holder: lease.Identity{WorkloadRef: "workload:hcmnext-operations-reconcile", InstanceRef: "replica:fault-2"},
			Now:    takeoverAt, TTL: 720 * time.Hour,
		})
		return err
	})
	err := f.try(func(tx dbport.Tx) error {
		_, perr := coord.Poll(ctx, tx, reconcile.PollRequest{
			TenantID: f.tenant, JobID: jobID, Fence: short.Fence, Now: takeoverAt.Add(time.Second),
		})
		return perr
	})
	if reconcile.CodeOf(err) != reconcile.CodeFenceRefused || !errors.Is(err, lease.ErrFenceStale) {
		t.Fatalf("polling under a superseded fence: err = %v, want CodeFenceRefused / lease.ErrFenceStale", err)
	}
	if state, version, attempts := rowState(); state != "PENDING" || version != 1 || attempts != 0 {
		t.Fatalf("a refused poll changed the row: state=%s version=%d attempts=%d", state, version, attempts)
	}

	// An observer failure leaves the row untouched.
	observer.err = errors.New("provider timeout")
	err = f.try(func(tx dbport.Tx) error {
		_, perr := coord.Poll(ctx, tx, reconcile.PollRequest{TenantID: f.tenant, JobID: jobID, Fence: successor.Fence, Now: fixedInstant.Add(time.Minute)})
		return perr
	})
	if reconcile.CodeOf(err) != reconcile.CodeObserverFailed {
		t.Fatalf("an observer failure: err = %v, want CodeObserverFailed", err)
	}
	if state, version, attempts := rowState(); state != "PENDING" || version != 1 || attempts != 0 {
		t.Fatalf("an observer failure changed the row: state=%s version=%d attempts=%d", state, version, attempts)
	}
	observer.err = nil

	// A comparer that reports a status this package does not accept is
	// refused, and the row is untouched.
	observer.result = reconcile.ObservationResult{Found: true, Freshness: observe.FreshnessFresh}
	comparer.verdict = reconcile.Verdict{Status: reconcile.StatusPending}
	err = f.try(func(tx dbport.Tx) error {
		_, perr := coord.Poll(ctx, tx, reconcile.PollRequest{TenantID: f.tenant, JobID: jobID, Fence: successor.Fence, Now: fixedInstant.Add(2 * time.Minute)})
		return perr
	})
	if reconcile.CodeOf(err) != reconcile.CodeUnexpectedStatus {
		t.Fatalf("a comparer returning an unaccepted status: err = %v, want CodeUnexpectedStatus", err)
	}
	if state, version, attempts := rowState(); state != "PENDING" || version != 1 || attempts != 0 {
		t.Fatalf("an unexpected verdict status changed the row: state=%s version=%d attempts=%d", state, version, attempts)
	}

	// A rolled-back transaction persists nothing.
	conn := appConn(t, db)
	tx, berr := conn.Begin(ctx)
	if berr != nil {
		t.Fatalf("begin: %v", berr)
	}
	if terr := tenancy.WithTenant(ctx, tx, f.tenant); terr != nil {
		t.Fatalf("scope tenant: %v", terr)
	}
	doomedEffect := mandatoryEffect("effect/fault-doomed", "NONE")
	doomed, serr := coord.Trigger(ctx, tx, reconcile.TriggerRequest{
		TenantID: f.tenant, Effect: doomedEffect, PolicyRef: "policy/fault-v1",
		IntendedRef: "proposal:1", RequiredFreshness: observe.FreshnessFresh,
		SLARef: "sla:fault", Deadline: fixedInstant.Add(time.Hour), Fence: f.fence, Now: fixedInstant,
	})
	if serr != nil {
		t.Fatalf("trigger inside the doomed transaction: %v", serr)
	}
	if rerr := tx.Rollback(ctx); rerr != nil {
		t.Fatalf("rollback: %v", rerr)
	}
	var rows int
	db.QueryRow(ctx, `SELECT count(*) FROM effect_reconciliation_job WHERE tenant_id = $1 AND job_id = $2`,
		f.tenant, doomed.Job.JobID).Scan(&rows)
	if rows != 0 {
		t.Fatalf("%d rows survived a rolled-back trigger, want 0", rows)
	}
}

// TestTodo_RECON_001_Mutation proves the load-bearing guards actually gate
// behaviour, not merely document intent.
func TestTodo_RECON_001_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newReconcileFixture(t, db, "recon-mutation")
	var store reconcile.PostgresStore

	// Guard 1: a stale-relative-to-policy observation never reaches the
	// comparer, however favourable the comparer's own configured verdict is.
	observer := &fixedObserver{result: reconcile.ObservationResult{Found: true, Freshness: observe.FreshnessStale}}
	comparer := &fixedComparer{verdict: reconcile.Verdict{Status: reconcile.StatusPass}}
	coord := reconcile.Coordinator{Store: store, Observer: observer, Comparer: comparer, Fences: lease.Manager{}}

	effect := mandatoryEffect("effect/mutation-1", "NONE")
	var triggered reconcile.Triggered
	f.do(t, func(tx dbport.Tx) error {
		var err error
		triggered, err = coord.Trigger(ctx, tx, reconcile.TriggerRequest{
			TenantID: f.tenant, Effect: effect, PolicyRef: "policy/mutation-v1",
			IntendedRef: "proposal:1", RequiredFreshness: observe.FreshnessFresh,
			SLARef: "sla:mutation", Deadline: fixedInstant.Add(time.Hour), Fence: f.fence, Now: fixedInstant,
		})
		return err
	})
	var polled reconcile.Polled
	f.do(t, func(tx dbport.Tx) error {
		var err error
		polled, err = coord.Poll(ctx, tx, reconcile.PollRequest{TenantID: f.tenant, JobID: triggered.Job.JobID, Fence: f.fence, Now: fixedInstant.Add(time.Minute)})
		return err
	})
	if comparer.calls != 0 {
		t.Fatalf("comparer called %d times on a stale observation, want 0 -- the freshness gate is not load-bearing", comparer.calls)
	}
	if polled.Job.Status == reconcile.StatusPass {
		t.Fatal("a stale observation settled the job to PASS; the freshness gate was bypassed")
	}

	// Guard 2: a terminal job's no-op short-circuit actually stops both
	// ports, not just the store write.
	observer.result = reconcile.ObservationResult{Found: true, Freshness: observe.FreshnessFresh}
	f.do(t, func(tx dbport.Tx) error {
		var err error
		polled, err = coord.Poll(ctx, tx, reconcile.PollRequest{TenantID: f.tenant, JobID: triggered.Job.JobID, Fence: f.fence, Now: fixedInstant.Add(2 * time.Minute)})
		return err
	})
	if polled.Job.Status != reconcile.StatusPass {
		t.Fatalf("job did not settle to PASS on a fresh observation: %+v", polled.Job)
	}
	callsBefore := observer.calls
	f.do(t, func(tx dbport.Tx) error {
		var err error
		polled, err = coord.Poll(ctx, tx, reconcile.PollRequest{TenantID: f.tenant, JobID: triggered.Job.JobID, Fence: f.fence, Now: fixedInstant.Add(3 * time.Minute)})
		return err
	})
	if !polled.NoOp {
		t.Fatal("polling an already-settled job did not report NoOp")
	}
	if observer.calls != callsBefore {
		t.Fatalf("polling a terminal job still called the observer: %d -> %d", callsBefore, observer.calls)
	}

	// Guard 3: the repair-policy branch at exhaustion is not a constant --
	// two effects with different repair policies exhaust to different
	// statuses.
	shortDeadline := fixedInstant.Add(time.Minute)
	e1 := mandatoryEffect("effect/mutation-expired", "NONE")
	e2 := mandatoryEffect("effect/mutation-repair", "hcmnext.repair.reissue_provider_call/v1")
	var got []reconcile.Status
	for _, e := range []effectgraph.EffectNode{e1, e2} {
		var trig reconcile.Triggered
		f.do(t, func(tx dbport.Tx) error {
			var err error
			trig, err = coord.Trigger(ctx, tx, reconcile.TriggerRequest{
				TenantID: f.tenant, Effect: e, PolicyRef: "policy/mutation-v1",
				IntendedRef: "proposal:1", RequiredFreshness: observe.FreshnessFresh,
				SLARef: "sla:mutation", Deadline: shortDeadline, Fence: f.fence, Now: fixedInstant,
			})
			return err
		})
		var exhausted reconcile.Polled
		f.do(t, func(tx dbport.Tx) error {
			var err error
			exhausted, err = coord.Poll(ctx, tx, reconcile.PollRequest{TenantID: f.tenant, JobID: trig.Job.JobID, Fence: f.fence, Now: shortDeadline.Add(time.Second)})
			return err
		})
		got = append(got, exhausted.Job.Status)
	}
	if got[0] == got[1] {
		t.Fatalf("two effects with different repair policies exhausted to the same status %s; the branch is not load-bearing", got[0])
	}
	if got[0] != reconcile.StatusExpired || got[1] != reconcile.StatusRepairRequired {
		t.Fatalf("exhaustion statuses = %v, want [EXPIRED, REPAIR_REQUIRED]", got)
	}

	// Guard 4: the idempotent-create guard is load-bearing under real
	// concurrency, not merely under sequential calls.
	concurrentEffect := mandatoryEffect("effect/mutation-concurrent", "NONE")
	var wg sync.WaitGroup
	var mu sync.Mutex
	var jobIDs []uuid.UUID
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := appConn(t, db)
			var out reconcile.Triggered
			err := inTenantTxErr(conn, f.tenant, func(tx dbport.Tx) error {
				var terr error
				out, terr = coord.Trigger(ctx, tx, reconcile.TriggerRequest{
					TenantID: f.tenant, Effect: concurrentEffect, PolicyRef: "policy/mutation-v1",
					IntendedRef: "proposal:1", RequiredFreshness: observe.FreshnessFresh,
					SLARef: "sla:mutation", Deadline: fixedInstant.Add(time.Hour), Fence: f.fence, Now: fixedInstant,
				})
				return terr
			})
			if err != nil {
				t.Errorf("concurrent trigger: %v", err)
				return
			}
			mu.Lock()
			jobIDs = append(jobIDs, out.Job.JobID)
			mu.Unlock()
		}()
	}
	wg.Wait()
	for i := 1; i < len(jobIDs); i++ {
		if jobIDs[i] != jobIDs[0] {
			t.Fatalf("concurrent triggers for the same effect and policy produced different job ids: %v", jobIDs)
		}
	}
	var rowCount int
	db.QueryRow(ctx, `SELECT count(*) FROM effect_reconciliation_job WHERE tenant_id = $1 AND effect_ref = $2`,
		f.tenant, concurrentEffect.IdempotencyKey).Scan(&rowCount)
	if rowCount != 1 {
		t.Fatalf("%d rows for one effect and policy under concurrent triggers, want exactly 1", rowCount)
	}
}
