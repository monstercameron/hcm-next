package timer_test

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// WF-RUN-004's matrix against a real PostgreSQL.
//
// The ticket's RED clause lists five ways a timer can be wrong -- loss, double
// wake, early wake, clock skew and a dataset revision silently changing an
// execution date -- and each of them has a case below. The dataset one is the
// least obvious and the most important: the promise is keyed on the wake
// requirement's content digest, so a republished tzdb or calendar cannot
// change what an existing promise means, only produce a different promise.

// onTime is the misfire policy for a timer being fired at its own instant.
var onTime = schedule.MisfireConfig{Policy: schedule.MisfireSkip, Grace: time.Hour}

func wantCode(t *testing.T, err error, code, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected a refusal, got nil", what)
	}
	if got := timer.CodeOf(err); got != code {
		t.Fatalf("%s: code = %q (%v), want %q", what, got, err, code)
	}
}

// timerState reads one timer's durable state and version straight from the
// table, so an assertion about "settled once" is about the row and not about
// what the package said it did.
func timerState(t *testing.T, db *pgtest.DB, tenant, timerID uuid.UUID) (string, int64) {
	t.Helper()
	var state string
	var version int64
	db.QueryRow(context.Background(), `
		SELECT timer_state, timer_version FROM workflow_timer WHERE tenant_id = $1 AND timer_id = $2`,
		tenant, timerID).Scan(&state, &version)
	return state, version
}

func readyWorkCount(t *testing.T, db *pgtest.DB, tenant, instance uuid.UUID) int {
	t.Helper()
	var n int
	db.QueryRow(context.Background(), `
		SELECT count(*) FROM workflow_ready_work WHERE tenant_id = $1 AND instance_id = $2`,
		tenant, instance).Scan(&n)
	return n
}

func TestTodo_WF_RUN_004(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newTimerFixture(t, db, "timer-primary")
	var s timer.Scheduler

	fireAt := fixedInstant.Add(24 * time.Hour)
	requirement := wakeRequirement(t, "wait.effective_date", fireAt, testDataset)

	// 1. A WAIT step writes its promise down. The durable key is the wake
	//    requirement's digest, which is what pins the zone, tzdb release,
	//    calendar and reference-update policy behind the instant.
	var scheduled timer.Scheduled
	f.do(t, func(tx dbport.Tx) error {
		var err error
		scheduled, err = s.Schedule(ctx, tx, timer.Request{
			TenantID: f.tenant, InstanceID: f.instance, NodeID: "wait.effective_date",
			Kind: timer.KindUntil, Requirement: requirement, CreatedAt: fixedInstant,
		})
		return err
	})
	if scheduled.Replay {
		t.Fatal("a first schedule reported itself as a replay")
	}
	if scheduled.Timer.Key != requirement.Digest {
		t.Fatalf("timer key = %q, want the wake requirement digest %q", scheduled.Timer.Key, requirement.Digest)
	}
	if !scheduled.Timer.FiresAt.Equal(fireAt) {
		t.Fatalf("fires_at = %s, want %s", scheduled.Timer.FiresAt, fireAt)
	}
	if scheduled.Evidence.Kind != timer.EventScheduled || scheduled.Evidence.Digest() == "" {
		t.Fatalf("schedule evidence = %+v", scheduled.Evidence)
	}
	if state, _ := timerState(t, db, f.tenant, scheduled.Timer.TimerID); state != "PENDING" {
		t.Fatalf("durable timer state = %q, want PENDING", state)
	}

	// 2. A replayed advancement re-makes the same promise and writes nothing.
	var replayed timer.Scheduled
	f.do(t, func(tx dbport.Tx) error {
		var err error
		replayed, err = s.Schedule(ctx, tx, timer.Request{
			TenantID: f.tenant, InstanceID: f.instance, NodeID: "wait.effective_date",
			Kind: timer.KindUntil, Requirement: requirement, CreatedAt: fixedInstant.Add(time.Second),
		})
		return err
	})
	if !replayed.Replay || replayed.Timer.TimerID != scheduled.Timer.TimerID {
		t.Fatalf("a replayed schedule = %+v, want the original timer reported as a replay", replayed)
	}
	if _, version := timerState(t, db, f.tenant, scheduled.Timer.TimerID); version != 1 {
		t.Fatalf("a replayed schedule moved the timer version to %d", version)
	}

	// 3. Nothing is due before the instant: no early wake.
	var due []timer.Timer
	f.do(t, func(tx dbport.Tx) error {
		var err error
		due, err = s.Due(ctx, tx, f.tenant, fireAt.Add(-time.Second), 0)
		return err
	})
	if len(due) != 0 {
		t.Fatalf("%d timers due a second before the instant, want 0", len(due))
	}
	f.do(t, func(tx dbport.Tx) error {
		var err error
		due, err = s.Due(ctx, tx, f.tenant, fireAt, 0)
		return err
	})
	if len(due) != 1 || due[0].TimerID != scheduled.Timer.TimerID {
		t.Fatalf("due at the instant = %+v, want the one promise", due)
	}

	// 4. Firing settles the promise and wakes exactly one unit of work, under
	//    the holder's fence.
	var result timer.FireResult
	f.do(t, func(tx dbport.Tx) error {
		var err error
		result, err = s.Fire(ctx, tx, timer.FireRequest{
			TenantID: f.tenant, Now: fireAt, Fence: f.fence, Misfire: onTime,
		})
		return err
	})
	if len(result.Fired) != 1 || len(result.Skipped) != 0 || len(result.Deferred) != 0 {
		t.Fatalf("fire result = %+v, want exactly one fired promise", result)
	}
	settled := result.Fired[0]
	if settled.Decision != timer.DecisionOnTime {
		t.Fatalf("decision = %q, want ON_TIME", settled.Decision)
	}
	if !settled.EligibleAt.Equal(fireAt) {
		t.Fatalf("woken work is eligible at %s, want the timer's own instant %s", settled.EligibleAt, fireAt)
	}
	if settled.Attempt != 1 || settled.ReadyWorkReplay {
		t.Fatalf("settled = %+v, want a fresh enqueue at attempt 1", settled)
	}
	if settled.Evidence.Kind != timer.EventFired || settled.Evidence.FenceToken != f.fence.Token {
		t.Fatalf("fire evidence = %+v, want FIRED under fence token %d", settled.Evidence, f.fence.Token)
	}
	if state, _ := timerState(t, db, f.tenant, scheduled.Timer.TimerID); state != "FIRED" {
		t.Fatalf("durable timer state after firing = %q, want FIRED", state)
	}
	if n := readyWorkCount(t, db, f.tenant, f.instance); n != 1 {
		t.Fatalf("%d ready-work rows, want 1", n)
	}

	// 5. No double wake: the promise is settled, so a later fire finds nothing.
	f.do(t, func(tx dbport.Tx) error {
		var err error
		result, err = s.Fire(ctx, tx, timer.FireRequest{
			TenantID: f.tenant, Now: fireAt.Add(time.Minute), Fence: f.fence, Misfire: onTime,
		})
		return err
	})
	if len(result.Fired) != 0 {
		t.Fatalf("a settled promise fired again: %+v", result.Fired)
	}
	if n := readyWorkCount(t, db, f.tenant, f.instance); n != 1 {
		t.Fatalf("%d ready-work rows after a second fire, want 1", n)
	}

	// 6. An instance that reaches a terminal withdraws the promises it made on
	//    the way there, so nothing wakes it afterwards.
	second := wakeRequirement(t, "wait.deadline", fireAt.Add(48*time.Hour), testDataset)
	var pendingTimer timer.Scheduled
	f.do(t, func(tx dbport.Tx) error {
		var err error
		pendingTimer, err = s.Schedule(ctx, tx, timer.Request{
			TenantID: f.tenant, InstanceID: f.instance, NodeID: "wait.deadline",
			Kind: timer.KindDeadline, Requirement: second, CreatedAt: fixedInstant,
		})
		return err
	})
	var cancelled []timer.Evidence
	f.do(t, func(tx dbport.Tx) error {
		var err error
		cancelled, err = s.CancelInstance(ctx, tx, f.tenant, f.instance,
			fireAt.Add(time.Hour), "instance reached a terminal")
		return err
	})
	if len(cancelled) != 1 || cancelled[0].Kind != timer.EventCancelled {
		t.Fatalf("cancellation evidence = %+v, want one CANCELLED record", cancelled)
	}
	if state, _ := timerState(t, db, f.tenant, pendingTimer.Timer.TimerID); state != "CANCELLED" {
		t.Fatalf("durable timer state after instance cancellation = %q, want CANCELLED", state)
	}
	f.do(t, func(tx dbport.Tx) error {
		var err error
		due, err = s.Due(ctx, tx, f.tenant, fireAt.Add(72*time.Hour), 0)
		return err
	})
	if len(due) != 0 {
		t.Fatalf("%d timers still due after the instance was cancelled, want 0", len(due))
	}
	// Repeating the cancellation is safe and reports nothing further.
	f.do(t, func(tx dbport.Tx) error {
		again, err := s.CancelInstance(ctx, tx, f.tenant, f.instance, fireAt.Add(2*time.Hour), "repeat")
		if err != nil {
			return err
		}
		if len(again) != 0 {
			t.Fatalf("a repeated instance cancellation withdrew %d more promises", len(again))
		}
		return nil
	})
}

// RACE: a fired timer settles once under concurrent Fire calls.
//
// The two callers hold two different, individually valid queue leases, so
// neither blocks on the other's fence read and both genuinely see the promise
// as pending. What separates them is the timer row's own compare-and-swap.
func TestTodo_WF_RUN_004_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newTimerFixture(t, db, "timer-race")
	var s timer.Scheduler

	fireAt := fixedInstant.Add(time.Hour)
	requirement := wakeRequirement(t, "wait.effective_date", fireAt, testDataset)
	var scheduled timer.Scheduled
	f.do(t, func(tx dbport.Tx) error {
		var err error
		scheduled, err = s.Schedule(ctx, tx, timer.Request{
			TenantID: f.tenant, InstanceID: f.instance, NodeID: "wait.effective_date",
			Kind: timer.KindUntil, Requirement: requirement, CreatedAt: fixedInstant,
		})
		return err
	})

	// Two holders, two queue partitions, two live fences.
	fences := make([]lease.Fence, 0, 2)
	conns := make([]interface {
		Begin(context.Context) (dbport.Tx, error)
	}, 0, 2)
	for i, suffix := range []string{"a", "b"} {
		conn := appConn(t, db)
		holder := lease.Identity{
			WorkloadRef: "workload:hcmnext-workflow-runtime",
			InstanceRef: "replica:timer-" + suffix,
		}
		resource := lease.Resource{Kind: lease.ResourceQueue, ID: "queue:workflow-timer-" + suffix}
		var grant lease.Grant
		inTenantTx(t, conn, f.tenant, func(tx dbport.Tx) error {
			var err error
			grant, err = (lease.Manager{}).Acquire(ctx, tx, lease.AcquireRequest{
				TenantID: f.tenant, Resource: resource, Holder: holder,
				Now: fixedInstant, TTL: 24 * time.Hour,
			})
			return err
		})
		fences = append(fences, grant.Fence)
		conns = append(conns, conn)
		_ = i
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []timer.FireResult
		errs    []error
	)
	start := make(chan struct{})
	for i := range fences {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			var out timer.FireResult
			err := inTenantTxOn(conns[i], f.tenant, func(tx dbport.Tx) error {
				var ferr error
				out, ferr = s.Fire(ctx, tx, timer.FireRequest{
					TenantID: f.tenant, Now: fireAt, Fence: fences[i], Misfire: onTime,
				})
				return ferr
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			results = append(results, out)
		}()
	}
	close(start)
	wg.Wait()

	fired := 0
	for _, r := range results {
		fired += len(r.Fired)
	}
	if fired != 1 {
		t.Fatalf("%d concurrent Fire calls settled the promise (results %+v, errors %v), want exactly 1", fired, results, errs)
	}
	for _, err := range errs {
		if !errors.Is(err, timer.ErrAlreadySettled) {
			t.Fatalf("a losing concurrent Fire failed for the wrong reason: %v", err)
		}
	}
	if state, version := timerState(t, db, f.tenant, scheduled.Timer.TimerID); state != "FIRED" || version != 2 {
		t.Fatalf("durable timer = %s at version %d, want FIRED at version 2 (settled exactly once)", state, version)
	}
	if n := readyWorkCount(t, db, f.tenant, f.instance); n != 1 {
		t.Fatalf("%d ready-work rows, want exactly 1", n)
	}
}

// FAULT: every refusal leaves the promise and the work queue exactly as they
// were.
func TestTodo_WF_RUN_004_Fault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newTimerFixture(t, db, "timer-fault")
	var s timer.Scheduler

	fireAt := fixedInstant.Add(time.Hour)
	overdue := fireAt.Add(72 * time.Hour)

	schedule1 := func(node string, at time.Time) timer.Scheduled {
		t.Helper()
		var out timer.Scheduled
		f.do(t, func(tx dbport.Tx) error {
			var err error
			out, err = s.Schedule(ctx, tx, timer.Request{
				TenantID: f.tenant, InstanceID: f.instance, NodeID: node,
				Kind: timer.KindUntil, Requirement: wakeRequirement(t, node, at, testDataset),
				CreatedAt: fixedInstant,
			})
			return err
		})
		return out
	}

	promise := schedule1("wait.effective_date", fireAt)

	// A fence the holder no longer owns fires nothing. This case runs on its
	// own queue partition, held on a short lease, so the takeover is a real
	// lapse-and-supersede rather than a contrivance.
	partition := lease.Resource{Kind: lease.ResourceQueue, ID: "queue:workflow-timer-fault"}
	var short lease.Grant
	inTenantTx(t, f.conn, f.tenant, func(tx dbport.Tx) error {
		var aerr error
		short, aerr = (lease.Manager{}).Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: f.tenant, Resource: partition, Holder: timerHolder,
			Now: fixedInstant, TTL: time.Hour,
		})
		return aerr
	})
	takeoverAt := fixedInstant.Add(2 * time.Hour)
	var successor lease.Grant
	inTenantTx(t, f.conn, f.tenant, func(tx dbport.Tx) error {
		var aerr error
		successor, aerr = (lease.Manager{}).Acquire(ctx, tx, lease.AcquireRequest{
			TenantID: f.tenant, Resource: partition,
			Holder: lease.Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica:timer-2"},
			Now:    takeoverAt, TTL: 720 * time.Hour,
		})
		return aerr
	})
	err := f.try(func(tx dbport.Tx) error {
		_, ferr := s.Fire(ctx, tx, timer.FireRequest{
			TenantID: f.tenant, Now: takeoverAt.Add(time.Second), Fence: short.Fence, Misfire: onTime,
		})
		return ferr
	})
	wantCode(t, err, timer.CodeFenceRefused, "firing under a superseded fence")
	if !errors.Is(err, lease.ErrFenceStale) {
		t.Fatalf("a superseded fence refusal does not classify as lease.ErrFenceStale: %v", err)
	}
	if state, _ := timerState(t, db, f.tenant, promise.Timer.TimerID); state != "PENDING" {
		t.Fatalf("a refused fire settled the promise anyway: state = %q", state)
	}
	if n := readyWorkCount(t, db, f.tenant, f.instance); n != 0 {
		t.Fatalf("a refused fire enqueued %d ready-work rows", n)
	}

	// The successor's fence is live, so the remaining cases can settle.
	live := successor.Fence

	// A REVIEW policy leaves the overdue promise pending for a human: the one
	// decision that deliberately writes nothing.
	var result timer.FireResult
	f.do(t, func(tx dbport.Tx) error {
		var ferr error
		result, ferr = s.Fire(ctx, tx, timer.FireRequest{
			TenantID: f.tenant, Now: overdue, Fence: live,
			Misfire: schedule.MisfireConfig{Policy: schedule.MisfireReview, Grace: time.Hour},
		})
		return ferr
	})
	if len(result.Deferred) != 1 || len(result.Fired) != 0 {
		t.Fatalf("a REVIEW misfire = %+v, want one deferred promise and nothing fired", result)
	}
	if result.Deferred[0].Evidence.Kind != timer.EventDeferred {
		t.Fatalf("deferred evidence = %q, want DEFERRED", result.Deferred[0].Evidence.Kind)
	}
	if state, version := timerState(t, db, f.tenant, promise.Timer.TimerID); state != "PENDING" || version != 1 {
		t.Fatalf("a deferred promise = %s at version %d, want PENDING at version 1", state, version)
	}
	if n := readyWorkCount(t, db, f.tenant, f.instance); n != 0 {
		t.Fatalf("a deferred promise woke %d units of work", n)
	}

	// A SKIP policy abandons it: cancelled, and still nothing woken.
	f.do(t, func(tx dbport.Tx) error {
		var ferr error
		result, ferr = s.Fire(ctx, tx, timer.FireRequest{
			TenantID: f.tenant, Now: overdue, Fence: live,
			Misfire: schedule.MisfireConfig{Policy: schedule.MisfireSkip, Grace: time.Hour},
		})
		return ferr
	})
	if len(result.Skipped) != 1 || len(result.Fired) != 0 {
		t.Fatalf("a SKIP misfire = %+v, want one skipped promise", result)
	}
	if state, _ := timerState(t, db, f.tenant, promise.Timer.TimerID); state != "CANCELLED" {
		t.Fatalf("a skipped promise = %q, want CANCELLED", state)
	}
	if n := readyWorkCount(t, db, f.tenant, f.instance); n != 0 {
		t.Fatalf("a skipped promise woke %d units of work", n)
	}

	// Settling an already-settled promise is refused rather than repeated.
	err = f.try(func(tx dbport.Tx) error {
		_, cerr := s.Cancel(ctx, tx, f.tenant, promise.Timer.TimerID, overdue, "again")
		return cerr
	})
	wantCode(t, err, timer.CodeAlreadySettled, "cancelling an already-settled promise")

	// A promise that does not exist is not found rather than invented.
	err = f.try(func(tx dbport.Tx) error {
		_, cerr := s.Cancel(ctx, tx, f.tenant, uuid.New(), overdue, "nothing there")
		return cerr
	})
	wantCode(t, err, timer.CodeNotFound, "cancelling a timer that does not exist")

	// A rolled-back schedule persists nothing.
	conn := appConn(t, db)
	tx, berr := conn.Begin(ctx)
	if berr != nil {
		t.Fatalf("begin: %v", berr)
	}
	if terr := tenancy.WithTenant(ctx, tx, f.tenant); terr != nil {
		t.Fatalf("scope tenant: %v", terr)
	}
	doomed, serr := s.Schedule(ctx, tx, timer.Request{
		TenantID: f.tenant, InstanceID: f.instance, NodeID: "wait.doomed",
		Kind: timer.KindWait, Requirement: wakeRequirement(t, "wait.doomed", fireAt, testDataset),
		CreatedAt: fixedInstant,
	})
	if serr != nil {
		t.Fatalf("schedule inside the doomed transaction: %v", serr)
	}
	if rerr := tx.Rollback(ctx); rerr != nil {
		t.Fatalf("rollback: %v", rerr)
	}
	var rows int
	db.QueryRow(ctx, `SELECT count(*) FROM workflow_timer WHERE tenant_id = $1 AND timer_id = $2`,
		f.tenant, doomed.Timer.TimerID).Scan(&rows)
	if rows != 0 {
		t.Fatalf("%d timer rows survived a rolled-back schedule, want 0", rows)
	}
}

// MUTATION: the guards are load-bearing.
func TestTodo_WF_RUN_004_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newTimerFixture(t, db, "timer-mutation")
	var s timer.Scheduler

	fireAt := fixedInstant.Add(24 * time.Hour)
	original := wakeRequirement(t, "wait.effective_date", fireAt, testDataset)
	afterRelease := wakeRequirement(t, "wait.effective_date", fireAt, republished)

	// The dataset clause: a republished tzdb and calendar produce a different
	// requirement, so the two promises are different rows. A dataset revision
	// cannot rewrite an existing promise's meaning; it can only make a new one.
	if original.Digest == afterRelease.Digest {
		t.Fatal("a republished dataset produced the same requirement digest; the whole guard would be vacuous")
	}
	var first, second timer.Scheduled
	f.do(t, func(tx dbport.Tx) error {
		var err error
		first, err = s.Schedule(ctx, tx, timer.Request{
			TenantID: f.tenant, InstanceID: f.instance, NodeID: "wait.effective_date",
			Kind: timer.KindUntil, Requirement: original, CreatedAt: fixedInstant,
		})
		return err
	})
	f.do(t, func(tx dbport.Tx) error {
		var err error
		second, err = s.Schedule(ctx, tx, timer.Request{
			TenantID: f.tenant, InstanceID: f.instance, NodeID: "wait.effective_date",
			Kind: timer.KindUntil, Requirement: afterRelease, CreatedAt: fixedInstant,
		})
		return err
	})
	if second.Replay || first.Timer.TimerID == second.Timer.TimerID {
		t.Fatalf("a requirement computed against a republished dataset reused the original promise: %+v", second)
	}
	// And a caller holding the republished requirement is refused if it tries
	// to settle the original promise.
	if err := s.CheckRequirement(first.Timer, afterRelease); !errors.Is(err, timer.ErrRequirementDrift) {
		t.Fatalf("settling the original promise with a republished requirement: err = %v, want drift", err)
	}
	if err := s.CheckRequirement(first.Timer, original); err != nil {
		t.Fatalf("settling the original promise with its own requirement was refused: %v", err)
	}

	// Early wake and clock skew: an instant that has not passed is not due,
	// however the caller's clock is set relative to the timer's.
	var result timer.FireResult
	f.do(t, func(tx dbport.Tx) error {
		var err error
		result, err = s.Fire(ctx, tx, timer.FireRequest{
			TenantID: f.tenant, Now: fireAt.Add(-time.Nanosecond), Fence: f.fence, Misfire: onTime,
		})
		return err
	})
	if len(result.Fired) != 0 || len(result.Skipped) != 0 || len(result.Deferred) != 0 {
		t.Fatalf("a fire a nanosecond early settled something: %+v", result)
	}
	if n := readyWorkCount(t, db, f.tenant, f.instance); n != 0 {
		t.Fatalf("an early fire woke %d units of work", n)
	}

	// Only the named promise is settled when the caller names one.
	f.do(t, func(tx dbport.Tx) error {
		var err error
		result, err = s.Fire(ctx, tx, timer.FireRequest{
			TenantID: f.tenant, Now: fireAt, Fence: f.fence, Misfire: onTime,
			Only: []uuid.UUID{first.Timer.TimerID},
		})
		return err
	})
	if len(result.Fired) != 1 || result.Fired[0].Timer.TimerID != first.Timer.TimerID {
		t.Fatalf("a targeted fire settled %+v, want only the named promise", result.Fired)
	}
	if state, _ := timerState(t, db, f.tenant, second.Timer.TimerID); state != "PENDING" {
		t.Fatalf("a targeted fire settled the other promise too: %q", state)
	}

	// Two promises on the same node attempt wake one unit of work, not two:
	// the ready-work identity is derived from (instance, node, attempt), so
	// settling the second one finds the row the first one wrote.
	f.do(t, func(tx dbport.Tx) error {
		var err error
		result, err = s.Fire(ctx, tx, timer.FireRequest{
			TenantID: f.tenant, Now: fireAt.Add(time.Hour), Fence: f.fence, Misfire: onTime,
			Only: []uuid.UUID{second.Timer.TimerID},
		})
		return err
	})
	if len(result.Fired) != 1 || !result.Fired[0].ReadyWorkReplay {
		t.Fatalf("settling a second promise for the same node attempt = %+v, want a recognised replay", result.Fired)
	}
	if n := readyWorkCount(t, db, f.tenant, f.instance); n != 1 {
		t.Fatalf("%d ready-work rows for one node attempt, want 1", n)
	}

	// FIRE_NOW and CATCH_UP differ in the eligibility instant they write, and
	// the difference reaches the durable row. This one is on its own node, so
	// it writes its own ready-work row rather than finding an existing one.
	fireNowAt := fireAt.Add(96 * time.Hour)
	var late timer.Scheduled
	f.do(t, func(tx dbport.Tx) error {
		var err error
		late, err = s.Schedule(ctx, tx, timer.Request{
			TenantID: f.tenant, InstanceID: f.instance, NodeID: "wait.late",
			Kind: timer.KindDeadline, Requirement: wakeRequirement(t, "wait.late", fireAt, testDataset),
			CreatedAt: fixedInstant,
		})
		return err
	})
	f.do(t, func(tx dbport.Tx) error {
		var err error
		result, err = s.Fire(ctx, tx, timer.FireRequest{
			TenantID: f.tenant, Now: fireNowAt, Fence: f.fence,
			Misfire: schedule.MisfireConfig{Policy: schedule.MisfireFireNow, Grace: time.Hour},
			Only:    []uuid.UUID{late.Timer.TimerID},
		})
		return err
	})
	if len(result.Fired) != 1 || result.Fired[0].Decision != timer.DecisionFireNow {
		t.Fatalf("an overdue FIRE_NOW = %+v", result)
	}
	if result.Fired[0].ReadyWorkReplay {
		t.Fatal("a promise on its own node reported a ready-work replay")
	}
	var eligible time.Time
	db.QueryRow(ctx, `SELECT eligible_at FROM workflow_ready_work WHERE tenant_id = $1 AND ready_work_id = $2`,
		f.tenant, result.Fired[0].ReadyWorkID).Scan(&eligible)
	if !eligible.UTC().Equal(fireNowAt) {
		t.Fatalf("FIRE_NOW made work eligible at %s, want the caller's instant %s", eligible.UTC(), fireNowAt)
	}
}

// CONFORMANCE (beyond the ticket's declared matrix): the WF-RUN-000 gate
// blocks scheduler code, so this package must contain no clock read, no
// ticker, no sleep and no goroutine of its own.
func TestTodo_WF_RUN_004_Conformance(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	banned := map[string]bool{"Now": true, "Sleep": true, "Tick": true, "NewTicker": true, "NewTimer": true, "After": true}
	fset := token.NewFileSet()
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		scanned++
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.GoStmt:
				t.Fatalf("%s starts a goroutine; this package runs nothing of its own", name)
			case *ast.SelectorExpr:
				ident, ok := node.X.(*ast.Ident)
				if ok && ident.Name == "time" && banned[node.Sel.Name] {
					t.Fatalf("%s calls time.%s; every instant is the caller's", name, node.Sel.Name)
				}
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("scanned no package source files; the conformance check proved nothing")
	}
}

// inTenantTxOn is inTenantTxErr for a connection held behind its Begin
// method, which is what the race case's per-goroutine connections are.
func inTenantTxOn(conn interface {
	Begin(context.Context) (dbport.Tx, error)
}, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if terr := tenancy.WithTenant(ctx, tx, tenant); terr != nil {
		_ = tx.Rollback(ctx)
		return terr
	}
	if ferr := fn(tx); ferr != nil {
		_ = tx.Rollback(ctx)
		return ferr
	}
	return tx.Commit(ctx)
}
