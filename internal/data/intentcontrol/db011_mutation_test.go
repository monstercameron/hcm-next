package intentcontrol_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/intentcontrol"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
)

// TestTodo_DB_011_Mutation is the MUTATION case: it attacks every already-written
// control row from the two directions a defect would actually arrive from, and
// requires that the row survive both.
//
// The first direction is a writer that bypasses this package entirely and issues
// raw SQL -- a repair script, a console session, a future store that forgets the
// rule. Against that, "the store never updates evidence" is worth nothing; only
// the trigger and the withheld grant are. The second is a caller that uses the
// stores correctly but asks for a transition the lifecycle does not allow, or
// offers a version it no longer holds.
//
// The sweep runs over every append-only table rather than a representative one
// because forbid_mutation is attached per table: a missed trigger on a single
// table is exactly the kind of gap a spot check does not find, and it is why the
// chain seeds a row everywhere first -- a BEFORE ROW trigger on an UPDATE that
// matches nothing never fires, and a sweep over empty tables would pass while
// proving the opposite of what it claims.
func TestTodo_DB_011_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	conn := appConn(t, db)
	chain := seedControlChain(t, db, conn, "db011-mutation")
	assertChainIsComplete(t, db, chain)

	t.Run("raw SQL cannot rewrite or remove an append-only control row", func(t *testing.T) {
		for _, table := range appendOnlyControlTables {
			before := countRows(t, db, table, chain.Tenant)

			// SET tenant_id = tenant_id changes nothing, which is the point: it
			// is the smallest possible UPDATE, so what refuses it is the
			// append-only rule and not a constraint on the new value.
			err := db.ExecErr(`UPDATE `+table+` SET tenant_id = tenant_id WHERE tenant_id = $1`, chain.Tenant)
			if err == nil {
				t.Errorf("%s accepted an UPDATE", table)
			} else if !strings.Contains(err.Error(), "append-only") {
				t.Errorf("%s refused the UPDATE with %v, want the forbid_mutation message", table, err)
			}

			err = db.ExecErr(`DELETE FROM `+table+` WHERE tenant_id = $1`, chain.Tenant)
			if err == nil {
				t.Errorf("%s accepted a DELETE", table)
			} else if !strings.Contains(err.Error(), "append-only") {
				t.Errorf("%s refused the DELETE with %v, want the forbid_mutation message", table, err)
			}

			if after := countRows(t, db, table, chain.Tenant); after != before {
				t.Errorf("%s went from %d rows to %d across the refused statements", table, before, after)
			}
		}
	})

	t.Run("the application role holds no UPDATE or DELETE it could use anywhere", func(t *testing.T) {
		// The trigger above is the second refusal. This is the first: the role
		// the application actually runs as was never granted the privilege, so
		// the statement is rejected before a trigger is reached. Both are
		// checked because the two protect against different mistakes -- a
		// dropped trigger in a later migration, and a widened grant.
		// One connection is enough for all of them: each attempt runs in its own
		// transaction, and the rollback that follows a permission failure leaves
		// the session usable again.
		attacker := appConn(t, db)
		for _, table := range appendOnlyControlTables {
			assertAppCannot(t, attacker, chain.Tenant,
				`UPDATE `+table+` SET tenant_id = tenant_id WHERE tenant_id = $1`)
		}
		for _, table := range controlTables {
			assertAppCannot(t, attacker, chain.Tenant,
				`DELETE FROM `+table+` WHERE tenant_id = $1`)
		}
	})

	t.Run("the four live tables move only through their compare-and-swap", func(t *testing.T) {
		var (
			requests    intentcontrol.ChangeRequestStore
			bindings    intentcontrol.PlanBindingStore
			ambiguities intentcontrol.AmbiguityStore
			repairs     intentcontrol.RepairPlanStore
		)

		// hcm_change_request: a status two steps ahead is refused by the
		// lifecycle graph, and a stale version by the UPDATE's own predicate.
		err := inTenantTxErr(conn, chain.Tenant, func(tx dbport.Tx) error {
			_, err := requests.Transition(ctx, tx, chain.Tenant, chain.ChangeRequest.ChangeRequestID,
				1, intentcontrol.RequestCommitted, laterThan(time.Hour))
			return err
		})
		if !errors.Is(err, intentcontrol.ErrIllegalTransition) {
			t.Errorf("DRAFT -> COMMITTED: got %v, want ErrIllegalTransition", err)
		}
		err = inTenantTxErr(conn, chain.Tenant, func(tx dbport.Tx) error {
			_, err := requests.Transition(ctx, tx, chain.Tenant, chain.ChangeRequest.ChangeRequestID,
				7, intentcontrol.RequestPreflighted, laterThan(time.Hour))
			return err
		})
		if !errors.Is(err, intentcontrol.ErrVersionConflict) {
			t.Errorf("a version the caller never held: got %v, want ErrVersionConflict", err)
		}

		// transaction_plan_binding: DRAFT does not jump to COMMITTED.
		err = inTenantTxErr(conn, chain.Tenant, func(tx dbport.Tx) error {
			_, err := bindings.Transition(ctx, tx, chain.Tenant, chain.CommitPlan.PlanID, 1,
				intentcontrol.BindingCommitted, "", laterThan(time.Hour))
			return err
		})
		if !errors.Is(err, intentcontrol.ErrIllegalTransition) {
			t.Errorf("DRAFT -> COMMITTED binding: got %v, want ErrIllegalTransition", err)
		}

		// transaction_ambiguity: a resolution with no evidence is a guess.
		err = inTenantTxErr(conn, chain.Tenant, func(tx dbport.Tx) error {
			_, err := ambiguities.Resolve(ctx, tx, chain.Tenant, chain.AmbiguityID, 1,
				intentcontrol.AmbiguityOutcomeCommitted, "", laterThan(time.Hour))
			return err
		})
		if !errors.Is(err, intentcontrol.ErrInvalidRow) {
			t.Errorf("resolution without evidence: got %v, want ErrInvalidRow", err)
		}

		// repair_plan: a terminal repair does not reopen.
		inTenantTx(t, conn, chain.Tenant, func(tx dbport.Tx) error {
			if _, err := repairs.Transition(ctx, tx, chain.Tenant, chain.RepairPlanID, 1,
				intentcontrol.RepairInProgress, laterThan(time.Hour)); err != nil {
				return err
			}
			_, err := repairs.Transition(ctx, tx, chain.Tenant, chain.RepairPlanID, 2,
				intentcontrol.RepairRepaired, laterThan(2*time.Hour))
			return err
		})
		err = inTenantTxErr(conn, chain.Tenant, func(tx dbport.Tx) error {
			_, err := repairs.Transition(ctx, tx, chain.Tenant, chain.RepairPlanID, 3,
				intentcontrol.RepairOpen, laterThan(3*time.Hour))
			return err
		})
		if !errors.Is(err, intentcontrol.ErrIllegalTransition) {
			t.Errorf("REPAIRED -> OPEN: got %v, want ErrIllegalTransition", err)
		}
	})

	t.Run("a refused write leaves the chain exactly as it was", func(t *testing.T) {
		// Every refusal above ran inside a transaction that rolled back, and a
		// store that had written something before deciding to refuse would show
		// up here as a row count that moved.
		assertChainIsComplete(t, db, chain)
		for _, table := range appendOnlyControlTables {
			if n := countRows(t, db, table, chain.Tenant); n != expectedChainRows(table) {
				t.Errorf("%s holds %d rows, want %d", table, n, expectedChainRows(table))
			}
		}
		// The closure still names its receipt, which is the single fact every
		// attack above would have had to break to matter.
		var closures intentcontrol.ClosureStore
		var closure intentcontrol.Closure
		inTenantTx(t, conn, chain.Tenant, func(tx dbport.Tx) error {
			var err error
			closure, err = closures.Load(ctx, tx, chain.Tenant, chain.Intent)
			return err
		})
		if closure.ExecutionReceiptID != chain.CommitReceiptID {
			t.Fatalf("closure now names receipt %s, want %s", closure.ExecutionReceiptID, chain.CommitReceiptID)
		}
	})
}

// expectedChainRows is how many rows seedControlChain writes into one
// append-only table. Everything gets one row except the two tables the chain
// populates twice: it compiles two plans so that both receipt tables can be
// filled, and each plan declares one effect.
func expectedChainRows(table string) int {
	switch table {
	case "transaction_plan", "transaction_plan_effect":
		return 2
	default:
		return 1
	}
}

// assertAppCannot requires that the least-privilege application role be refused
// statement, inside a properly tenant-scoped transaction, so that the refusal is
// the missing grant and not a missing tenant.
func assertAppCannot(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, statement string) {
	t.Helper()
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), statement, tenant)
		return err
	})
	if err == nil {
		t.Errorf("hcmnext_app was allowed to run %q", statement)
		return
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("%q was refused with %v, want a permission denial", statement, err)
	}
}
