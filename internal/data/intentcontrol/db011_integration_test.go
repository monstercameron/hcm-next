package intentcontrol_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/intentcontrol"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

// TestTodo_DB_011_Integration is the INTEGRATION case: one whole control chain
// is written by a single transaction, committed, and then answered for entirely
// by a connection that shares no session, no cache and no in-process state with
// the writer.
//
// The PRIMARY case proves each store refuses what it should. This one proves
// the complementary claim, which no per-store test can make: that what the
// stores wrote is a complete, self-consistent record set on disk -- every
// foreign key satisfied, every version readable, every digest still there --
// and that a coordinator restarted after a crash could pick the work up from
// the database alone. The reader is a second connection rather than a second
// transaction on the first, because a transaction on the writer's connection
// would still inherit its session state and prove less than it appears to.
func TestTodo_DB_011_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	writer := appConn(t, db)
	chain := seedControlChain(t, db, writer, "db011-integration")
	assertChainIsComplete(t, db, chain)

	// Everything below runs on a connection opened after the write committed.
	reader := appConn(t, db)

	var (
		contexts      intentcontrol.ContextStore
		requests      intentcontrol.ChangeRequestStore
		snapshots     intentcontrol.SnapshotStore
		simulations   intentcontrol.SimulationStore
		sets          intentcontrol.ProposalSetStore
		relationships intentcontrol.RelationshipStore
		results       intentcontrol.ResultStore
		decisions     intentcontrol.DecisionStore
		plans         intentcontrol.PlanStore
		bindings      intentcontrol.PlanBindingStore
		receipts      intentcontrol.ReceiptStore
		ambiguities   intentcontrol.AmbiguityStore
		repairs       intentcontrol.RepairPlanStore
		closures      intentcontrol.ClosureStore
	)

	t.Run("a fresh connection reloads every aggregate the chain wrote", func(t *testing.T) {
		var (
			instanceContext intentcontrol.InstanceContext
			request         intentcontrol.ChangeRequest
			snapshot        intentcontrol.InputSnapshot
			simulation      intentcontrol.SimulationResult
			proposalSets    intentcontrol.ProposalSets
			children        []uuid.UUID
			result          intentcontrol.Result
			decisionCount   int
			plan            intentcontrol.Plan
			effects         []intentcontrol.PlanEffect
			binding         intentcontrol.PlanBinding
			commitReceipt   intentcontrol.CommitReceipt
			ambiguity       intentcontrol.Ambiguity
			repair          intentcontrol.RepairPlan
			closure         intentcontrol.Closure
		)
		inTenantTx(t, reader, chain.Tenant, func(tx dbport.Tx) error {
			var err error
			if instanceContext, err = contexts.Load(ctx, tx, chain.Tenant, chain.Intent); err != nil {
				return err
			}
			if request, err = requests.Load(ctx, tx, chain.Tenant, chain.ChangeRequest.ChangeRequestID); err != nil {
				return err
			}
			if snapshot, err = snapshots.Load(ctx, tx, chain.Tenant, chain.Snapshot.SnapshotID); err != nil {
				return err
			}
			if simulation, err = simulations.Load(ctx, tx, chain.Tenant, chain.Simulation.SimulationID); err != nil {
				return err
			}
			if proposalSets, err = sets.Load(ctx, tx, chain.Tenant, chain.Intent, chain.Revision); err != nil {
				return err
			}
			if children, err = relationships.Children(ctx, tx, chain.Tenant, chain.Intent,
				intentcontrol.RelationChildOf); err != nil {
				return err
			}
			if result, err = results.Load(ctx, tx, chain.Tenant, chain.Intent, chain.Revision); err != nil {
				return err
			}
			if decisionCount, err = decisions.CountForRevision(ctx, tx, chain.Tenant, chain.Intent,
				chain.Revision); err != nil {
				return err
			}
			if plan, err = plans.Load(ctx, tx, chain.Tenant, chain.CommitPlan.PlanID); err != nil {
				return err
			}
			if effects, err = plans.Effects(ctx, tx, chain.Tenant, chain.CommitPlan.PlanID); err != nil {
				return err
			}
			if binding, err = bindings.Load(ctx, tx, chain.Tenant, chain.CommitPlan.PlanID); err != nil {
				return err
			}
			if commitReceipt, err = receipts.LoadCommit(ctx, tx, chain.Tenant, chain.CommitPlan.PlanID); err != nil {
				return err
			}
			if ambiguity, err = ambiguities.Load(ctx, tx, chain.Tenant, chain.AmbiguityID); err != nil {
				return err
			}
			if repair, err = repairs.Load(ctx, tx, chain.Tenant, chain.RepairPlanID); err != nil {
				return err
			}
			closure, err = closures.Load(ctx, tx, chain.Tenant, chain.Intent)
			return err
		})

		if instanceContext.CorrelationID != "corr-"+chain.Intent.String() ||
			instanceContext.OriginTrust != intentcontrol.OriginAsserted {
			t.Errorf("reloaded context = %+v, want the correlation and ASSERTED origin the chain wrote", instanceContext)
		}
		if request.RequestStatus != intentcontrol.RequestDraft || request.RequestVersion != 1 {
			t.Errorf("reloaded change request = %s/%d, want DRAFT/1", request.RequestStatus, request.RequestVersion)
		}
		if snapshot.Digest != digestOf("snapshot") || snapshot.Purpose != intentcontrol.PurposePreflight {
			t.Errorf("reloaded snapshot = %+v, want the PREFLIGHT baseline the chain wrote", snapshot)
		}
		if simulation.InputSnapshotID != chain.Snapshot.SnapshotID {
			t.Errorf("reloaded simulation cites snapshot %s, want %s",
				simulation.InputSnapshotID, chain.Snapshot.SnapshotID)
		}
		// The four sets are the part of a proposal that only exists in these
		// tables: a digest alone could not be reopened into what was proposed.
		if len(proposalSets.Writes) != 1 || len(proposalSets.Effects) != 1 ||
			len(proposalSets.Approvals) != 1 || len(proposalSets.Obligations) != 1 {
			t.Fatalf("reloaded proposal sets = %+v, want one member in each of the four", proposalSets)
		}
		if proposalSets.Writes[0].ProposedCanonicalText != "132000.00" {
			t.Errorf("reloaded write item = %+v, want the proposed 132000.00", proposalSets.Writes[0])
		}
		if proposalSets.Effects[0].CompensationRef == "" || proposalSets.Effects[0].ObservationRef == "" {
			t.Errorf("reloaded effect lost its compensation or observation: %+v", proposalSets.Effects[0])
		}
		if len(children) != 1 || children[0] != chain.ChildIntent {
			t.Errorf("reloaded children = %v, want [%s]", children, chain.ChildIntent)
		}
		if result.Kind != intentcontrol.ResultCommitted {
			t.Errorf("reloaded result kind = %s, want COMMITTED", result.Kind)
		}
		if decisionCount != 1 {
			t.Errorf("reloaded decision count = %d, want 1", decisionCount)
		}
		if plan.PlanDigest != chain.CommitPlan.PlanDigest ||
			plan.IntentDigest != chain.CommitPlan.IntentDigest ||
			plan.ProposalDigest != chain.CommitPlan.ProposalDigest ||
			plan.ControlDigest != chain.CommitPlan.ControlDigest {
			t.Errorf("reloaded plan lost a digest: %+v", plan)
		}
		if len(effects) != 1 || effects[0].IdempotencyKey == "" || effects[0].RepairPlanRef == "" {
			t.Errorf("reloaded plan effects = %+v, want the one declared effect intact", effects)
		}
		if binding.State != intentcontrol.BindingDraft || binding.BindingVersion != 1 {
			t.Errorf("reloaded binding = %s/%d, want DRAFT/1", binding.State, binding.BindingVersion)
		}
		if commitReceipt.ReceiptID != chain.CommitReceiptID ||
			commitReceipt.DatabaseTransactionID != "txid:4711" {
			t.Errorf("reloaded commit receipt = %+v, want the one the chain wrote", commitReceipt)
		}
		if ambiguity.ResolutionState != intentcontrol.AmbiguityUnresolved || ambiguity.Version != 1 {
			t.Errorf("reloaded ambiguity = %s/%d, want UNRESOLVED/1", ambiguity.ResolutionState, ambiguity.Version)
		}
		if repair.Status != intentcontrol.RepairOpen || repair.PlanID != chain.AbortPlan.PlanID {
			t.Errorf("reloaded repair plan = %+v, want an OPEN repair of the aborted plan", repair)
		}
		if closure.ExecutionReceiptID != chain.CommitReceiptID || closure.Kind != intentcontrol.ClosureCommitted {
			t.Errorf("reloaded closure = %+v, want a COMMITTED closure naming the commit receipt", closure)
		}
	})

	t.Run("work continues from the durable versions alone, with nothing carried over from the writer", func(t *testing.T) {
		// Each compare-and-swap below uses only the version the reader loaded a
		// moment earlier. That is the restart case: a coordinator that woke up
		// holding nothing but a tenant and an id.
		var advancedRequest intentcontrol.ChangeRequest
		inTenantTx(t, reader, chain.Tenant, func(tx dbport.Tx) error {
			current, err := requests.Load(ctx, tx, chain.Tenant, chain.ChangeRequest.ChangeRequestID)
			if err != nil {
				return err
			}
			advancedRequest, err = requests.Transition(ctx, tx, chain.Tenant, current.ChangeRequestID,
				current.RequestVersion, intentcontrol.RequestPreflighted, laterThan(time.Hour))
			return err
		})
		if advancedRequest.RequestStatus != intentcontrol.RequestPreflighted || advancedRequest.RequestVersion != 2 {
			t.Fatalf("continued change request = %s/%d, want PREFLIGHTED/2",
				advancedRequest.RequestStatus, advancedRequest.RequestVersion)
		}

		var advancedRepair intentcontrol.RepairPlan
		inTenantTx(t, reader, chain.Tenant, func(tx dbport.Tx) error {
			current, err := repairs.Load(ctx, tx, chain.Tenant, chain.RepairPlanID)
			if err != nil {
				return err
			}
			advancedRepair, err = repairs.Transition(ctx, tx, chain.Tenant, current.RepairPlanID,
				current.Version, intentcontrol.RepairInProgress, laterThan(2*time.Hour))
			return err
		})
		if advancedRepair.Status != intentcontrol.RepairInProgress || advancedRepair.Version != 2 {
			t.Fatalf("continued repair plan = %s/%d, want IN_PROGRESS/2",
				advancedRepair.Status, advancedRepair.Version)
		}
		// The three digests survive a transition. A repair that forgets what it
		// is repairing is RED's sixth defect, and a status update is exactly
		// where it would be lost.
		if advancedRepair.IntentDigest != digestOf("intent-repair") ||
			advancedRepair.ProposalDigest == "" || advancedRepair.ControlDigest == "" {
			t.Fatalf("a transition dropped one of the repair plan's digests: %+v", advancedRepair)
		}

		// The writer's connection now holds a stale view, and the database says
		// so rather than letting it overwrite the reader's work.
		err := inTenantTxErr(writer, chain.Tenant, func(tx dbport.Tx) error {
			_, err := requests.Transition(ctx, tx, chain.Tenant, chain.ChangeRequest.ChangeRequestID,
				1, intentcontrol.RequestWithdrawn, laterThan(3*time.Hour))
			return err
		})
		if !errors.Is(err, intentcontrol.ErrVersionConflict) {
			t.Fatalf("the writer's stale transition: got %v, want ErrVersionConflict", err)
		}
	})

	t.Run("another tenant's connection sees none of it", func(t *testing.T) {
		// The chain is on disk and the reader above proved it readable. Row
		// level security is what makes "readable" mean "readable by its own
		// tenant", and it is checked here through the same app role rather than
		// through the migration role, because the migration role bypasses it.
		other := insertTenant(t, db, "db011-integration-other")
		stranger := appConn(t, db)
		inTenantTx(t, stranger, other, func(tx dbport.Tx) error {
			for _, table := range controlTables {
				var n int
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
					return err
				}
				if n != 0 {
					t.Errorf("tenant %s sees %d rows in %s", other, n, table)
				}
			}
			return nil
		})
		// And the stores agree with the raw counts: a cross-tenant read is a
		// miss, not a leak.
		err := inTenantTxErr(stranger, other, func(tx dbport.Tx) error {
			_, err := closures.Load(ctx, tx, other, chain.Intent)
			return err
		})
		if !errors.Is(err, intentcontrol.ErrNotFound) {
			t.Fatalf("cross-tenant closure load: got %v, want ErrNotFound", err)
		}
	})
}
