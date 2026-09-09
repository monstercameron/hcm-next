package intentcontrol_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// raceWidth is how many genuinely separate sessions each race below runs. It is
// small on purpose: the point is contention on one row or one key, and six
// connections all blocked on the same lock demonstrate that as well as sixty
// would, without spending a minute of embedded-PostgreSQL time to do it.
const raceWidth = 6

// TestTodo_DB_011_Race is the RACE case. Every subtest starts several real
// connections -- not several transactions on one connection, which would
// serialize themselves and prove nothing -- and has them attempt the same
// exclusive act at the same moment.
//
// Each of the four is an invariant the PRIMARY case checks sequentially, where
// a store that merely reads-then-writes would still pass. Sequential refusals
// are cheap; the honest question is what the store does when nobody went first.
func TestTodo_DB_011_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	t.Run("compare-and-swap: several coordinators advance one change request, one wins", func(t *testing.T) {
		tenant := insertTenant(t, db, "db011-race-cas")
		intent := insertIntent(t, db, tenant, "idem-race-cas")
		setup := appConn(t, db)
		var requests intentcontrol.ChangeRequestStore

		request := referenceChangeRequest(tenant, intent)
		inTenantTx(t, setup, tenant, func(tx dbport.Tx) error {
			_, err := requests.Open(ctx, tx, request)
			return err
		})

		errs := runConcurrently(t, db, tenant, func(tx dbport.Tx, _ int) error {
			// Every worker expects version 1, which is what each of them would
			// have read before the others existed.
			_, err := requests.Transition(ctx, tx, tenant, request.ChangeRequestID, 1,
				intentcontrol.RequestPreflighted, fixedInstant.Add(time.Minute))
			return err
		})

		won := countNil(errs)
		if won != 1 {
			t.Fatalf("%d of %d workers advanced the change request, want exactly 1 (errors: %v)",
				won, raceWidth, errs)
		}
		for _, err := range errs {
			if err == nil {
				continue
			}
			// A loser that read the row before the winner committed is refused
			// by the compare-and-swap; one that read it afterwards is refused
			// earlier still, by the lifecycle graph, because PREFLIGHTED does
			// not succeed itself. Which of the two a given worker sees depends
			// on scheduling, and both are the same refusal.
			if !errors.Is(err, intentcontrol.ErrVersionConflict) &&
				!errors.Is(err, intentcontrol.ErrIllegalTransition) {
				t.Errorf("a loser got %v, want ErrVersionConflict or ErrIllegalTransition", err)
			}
		}
		var stored intentcontrol.ChangeRequest
		inTenantTx(t, setup, tenant, func(tx dbport.Tx) error {
			var err error
			stored, err = requests.Load(ctx, tx, tenant, request.ChangeRequestID)
			return err
		})
		// The version moved by exactly one. A lost update would show up here as
		// a status that advanced without the version following it.
		if stored.RequestVersion != 2 || stored.RequestStatus != intentcontrol.RequestPreflighted {
			t.Fatalf("after the race the request is %s/%d, want PREFLIGHTED/2",
				stored.RequestStatus, stored.RequestVersion)
		}
	})

	t.Run("receipts: commit and abort race for the same plan, and only one outcome is recorded", func(t *testing.T) {
		tenant := insertTenant(t, db, "db011-race-receipt")
		intent := insertIntent(t, db, tenant, "idem-race-receipt")
		insertRevision(t, db, tenant, intent, 1)
		setup := appConn(t, db)
		var (
			plans    intentcontrol.PlanStore
			receipts intentcontrol.ReceiptStore
		)

		plan := referencePlan(tenant, intent, 1, "race-receipt")
		inTenantTx(t, setup, tenant, func(tx dbport.Tx) error {
			return plans.Compile(ctx, tx, plan, referencePlanEffects())
		})

		// Half the workers claim the plan committed and half claim it aborted.
		// This is the cross-table invariant no UNIQUE constraint can see, so it
		// is the one race where a store that only read before writing would
		// produce two contradictory receipts instead of one answer.
		errs := runConcurrently(t, db, tenant, func(tx dbport.Tx, worker int) error {
			if worker%2 == 0 {
				return receipts.RecordCommit(ctx, tx, referenceCommitReceipt(plan))
			}
			return receipts.RecordAbort(ctx, tx, referenceAbortReceipt(plan))
		})

		if won := countNil(errs); won != 1 {
			t.Fatalf("%d of %d workers recorded a receipt, want exactly 1 (errors: %v)",
				won, raceWidth, errs)
		}
		for _, err := range errs {
			if err == nil {
				continue
			}
			if !errors.Is(err, intentcontrol.ErrReceiptConflict) && !errors.Is(err, intentcontrol.ErrDuplicate) {
				t.Errorf("a loser got %v, want ErrReceiptConflict or ErrDuplicate", err)
			}
		}
		commits := countRows(t, db, "transaction_commit_receipt", tenant)
		aborts := countRows(t, db, "transaction_abort_receipt", tenant)
		if commits+aborts != 1 {
			t.Fatalf("the plan holds %d commit and %d abort receipts, want exactly one receipt in total",
				commits, aborts)
		}
	})

	t.Run("parentage: several parents claim one child, and the child keeps one parent", func(t *testing.T) {
		tenant := insertTenant(t, db, "db011-race-parent")
		child := insertIntent(t, db, tenant, "idem-race-child")
		parents := make([]uuid.UUID, raceWidth)
		for i := range parents {
			parents[i] = insertIntent(t, db, tenant, "idem-race-parent-"+uuid.NewString())
		}
		var relationships intentcontrol.RelationshipStore

		errs := runConcurrently(t, db, tenant, func(tx dbport.Tx, worker int) error {
			return relationships.Link(ctx, tx,
				referenceRelationship(tenant, parents[worker], child, uint32(worker+1)))
		})

		if won := countNil(errs); won != 1 {
			t.Fatalf("%d of %d parents adopted the child, want exactly 1 (errors: %v)",
				won, raceWidth, errs)
		}
		for _, err := range errs {
			if err != nil && !errors.Is(err, intentcontrol.ErrDuplicate) {
				t.Errorf("a loser got %v, want ErrDuplicate", err)
			}
		}
		if n := countRows(t, db, "intent_relationship", tenant); n != 1 {
			t.Fatalf("intent_relationship holds %d edges, want the single surviving parentage", n)
		}
	})

	t.Run("closure: several coordinators close one intent, and it closes once", func(t *testing.T) {
		tenant := insertTenant(t, db, "db011-race-closure")
		intent := insertIntent(t, db, tenant, "idem-race-closure")
		var closures intentcontrol.ClosureStore

		errs := runConcurrently(t, db, tenant, func(tx dbport.Tx, _ int) error {
			return closures.Close(ctx, tx, referenceClosure(tenant, intent, intentcontrol.ClosureWithdrawn, uuid.Nil))
		})

		if won := countNil(errs); won != 1 {
			t.Fatalf("%d of %d workers closed the intent, want exactly 1 (errors: %v)",
				won, raceWidth, errs)
		}
		for _, err := range errs {
			if err != nil && !errors.Is(err, intentcontrol.ErrDuplicate) {
				t.Errorf("a loser got %v, want ErrDuplicate", err)
			}
		}
		if n := countRows(t, db, "intent_closure", tenant); n != 1 {
			t.Fatalf("intent_closure holds %d rows, want 1", n)
		}
	})
}

// runConcurrently opens raceWidth independent connections, releases them into
// fn at the same moment and returns each worker's error in worker order.
//
// The connections are opened before the barrier rather than inside the
// goroutines because connecting is slow enough to stagger the workers by more
// than the window the races are about, and a race test that never actually
// races is worse than no race test at all.
func runConcurrently(t *testing.T, db *pgtest.DB, tenant uuid.UUID, fn func(tx dbport.Tx, worker int) error) []error {
	t.Helper()

	conns := make([]*pgxadapter.Conn, raceWidth)
	for i := range conns {
		conns[i] = appConn(t, db)
	}

	var (
		start sync.WaitGroup
		done  sync.WaitGroup
	)
	start.Add(1)
	done.Add(raceWidth)
	errs := make([]error, raceWidth)
	for i := range conns {
		go func(worker int) {
			defer done.Done()
			start.Wait()
			errs[worker] = inTenantTxErr(conns[worker], tenant, func(tx dbport.Tx) error {
				return fn(tx, worker)
			})
		}(i)
	}
	start.Done()
	done.Wait()
	return errs
}

// countNil reports how many workers succeeded.
func countNil(errs []error) int {
	n := 0
	for _, err := range errs {
		if err == nil {
			n++
		}
	}
	return n
}
