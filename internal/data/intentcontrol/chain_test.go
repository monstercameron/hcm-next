package intentcontrol_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// controlChain is the identity of one complete DB-011 record set: after
// seedControlChain every one of the twenty-one tables migration 00024 creates
// holds at least one row for this tenant, and every foreign key between them is
// satisfied by a row in the same set.
//
// It exists because two of the matrix cases need the same thing from different
// angles. TestTodo_DB_011_Integration needs a chain written by one transaction
// and then read back by a connection that shares nothing with the writer;
// TestTodo_DB_011_Mutation needs a row in every append-only table, because
// forbid_mutation is a per-row trigger and an UPDATE that matches nothing
// proves nothing. Building the set twice in two shapes would let the two tests
// disagree about what "the whole chain" is.
type controlChain struct {
	Tenant           uuid.UUID
	Intent           uuid.UUID
	ChildIntent      uuid.UUID
	CorrectingIntent uuid.UUID
	Revision         uint64

	ChangeRequest intentcontrol.ChangeRequest
	Snapshot      intentcontrol.InputSnapshot
	Simulation    intentcontrol.SimulationResult
	Sets          intentcontrol.ProposalSets

	// CommitPlan is the plan that reached a commit receipt; AbortPlan is the
	// one that aborted. Two plans are needed because a single plan may hold at
	// most one of the two receipts, and the chain has to populate both tables.
	CommitPlan intentcontrol.Plan
	AbortPlan  intentcontrol.Plan

	CommitReceiptID uuid.UUID
	AmbiguityID     uuid.UUID
	CorrectionID    uuid.UUID
	RepairPlanID    uuid.UUID
}

// seedControlChain writes one whole chain for tenant inside a single
// transaction on conn and commits it.
//
// One transaction is deliberate: it is the claim DB-011's GREEN clause makes
// about these tables -- an intent's context, its proposal's sets, its plan, its
// receipt and its closure are one unit of meaning, and a store that could only
// write them across several transactions would leave a window in which the
// record set is incomplete.
func seedControlChain(t *testing.T, db *pgtest.DB, conn *pgxadapter.Conn, label string) controlChain {
	t.Helper()
	ctx := context.Background()

	chain := controlChain{Revision: 1}
	chain.Tenant = insertTenant(t, db, label)
	chain.Intent = insertIntent(t, db, chain.Tenant, "idem-"+label+"-main")
	chain.ChildIntent = insertIntent(t, db, chain.Tenant, "idem-"+label+"-child")
	chain.CorrectingIntent = insertIntent(t, db, chain.Tenant, "idem-"+label+"-correcting")
	insertRevision(t, db, chain.Tenant, chain.Intent, chain.Revision)

	chain.ChangeRequest = referenceChangeRequest(chain.Tenant, chain.Intent)
	chain.Snapshot = referenceSnapshot(chain.Tenant, chain.Intent, 1)
	chain.Sets = referenceSets()
	chain.CommitPlan = referencePlan(chain.Tenant, chain.Intent, chain.Revision, label+"-commit")
	chain.AbortPlan = referencePlan(chain.Tenant, chain.Intent, chain.Revision, label+"-abort")
	chain.Simulation = referenceSimulation(chain.Tenant, chain.Intent, chain.Revision, chain.Snapshot.SnapshotID)
	chain.AmbiguityID = uuid.New()

	commitReceipt := referenceCommitReceipt(chain.CommitPlan)
	chain.CommitReceiptID = commitReceipt.ReceiptID
	correction := referenceCorrection(chain.Tenant, chain.CommitReceiptID, chain.CorrectingIntent)
	chain.CorrectionID = correction.CorrectionID
	repair := referenceRepairPlan(chain.Tenant, chain.AbortPlan.PlanID)
	chain.RepairPlanID = repair.RepairPlanID

	var (
		contexts      intentcontrol.ContextStore
		requests      intentcontrol.ChangeRequestStore
		snapshots     intentcontrol.SnapshotStore
		simulations   intentcontrol.SimulationStore
		sets          intentcontrol.ProposalSetStore
		relationships intentcontrol.RelationshipStore
		results       intentcontrol.ResultStore
		decisions     intentcontrol.DecisionStore
		certificates  intentcontrol.CertificateStore
		plans         intentcontrol.PlanStore
		receipts      intentcontrol.ReceiptStore
		ambiguities   intentcontrol.AmbiguityStore
		corrections   intentcontrol.CorrectionStore
		repairs       intentcontrol.RepairPlanStore
		closures      intentcontrol.ClosureStore
	)

	inTenantTx(t, conn, chain.Tenant, func(tx dbport.Tx) error {
		if err := contexts.Record(ctx, tx, referenceContext(chain.Tenant, chain.Intent)); err != nil {
			return err
		}
		if _, err := requests.Open(ctx, tx, chain.ChangeRequest); err != nil {
			return err
		}
		stored, err := snapshots.Record(ctx, tx, chain.Snapshot)
		if err != nil {
			return err
		}
		chain.Snapshot = stored
		simulated, err := simulations.RecordResult(ctx, tx, chain.Simulation)
		if err != nil {
			return err
		}
		chain.Simulation = simulated
		if err := sets.Record(ctx, tx, chain.Tenant, chain.Intent, chain.Revision, chain.Sets); err != nil {
			return err
		}
		if err := relationships.Link(ctx, tx,
			referenceRelationship(chain.Tenant, chain.Intent, chain.ChildIntent, 1)); err != nil {
			return err
		}
		if err := results.Record(ctx, tx,
			referenceResult(chain.Tenant, chain.Intent, chain.Revision, intentcontrol.ResultCommitted)); err != nil {
			return err
		}
		if err := decisions.Record(ctx, tx,
			referenceDecision(chain.Tenant, chain.Intent, chain.Revision,
				"req-second-level", "principal:director-1")); err != nil {
			return err
		}
		if err := certificates.Issue(ctx, tx,
			referenceCertificate(chain.Tenant, chain.Intent, chain.Revision,
				intentcontrol.CertificateCommit)); err != nil {
			return err
		}
		if err := plans.Compile(ctx, tx, chain.CommitPlan, referencePlanEffects()); err != nil {
			return err
		}
		if err := plans.Compile(ctx, tx, chain.AbortPlan, referencePlanEffects()); err != nil {
			return err
		}
		if err := receipts.RecordCommit(ctx, tx, commitReceipt); err != nil {
			return err
		}
		if err := receipts.RecordAbort(ctx, tx, referenceAbortReceipt(chain.AbortPlan)); err != nil {
			return err
		}
		if err := ambiguities.Open(ctx, tx, intentcontrol.Ambiguity{
			TenantID:    chain.Tenant,
			AmbiguityID: chain.AmbiguityID,
			PlanID:      chain.CommitPlan.PlanID,
			DetectedAt:  fixedInstant,
			Detail:      "connection dropped after COMMIT was sent",
		}); err != nil {
			return err
		}
		if err := corrections.Record(ctx, tx, correction); err != nil {
			return err
		}
		if err := repairs.Open(ctx, tx, repair); err != nil {
			return err
		}
		return closures.Close(ctx, tx,
			referenceClosure(chain.Tenant, chain.Intent, intentcontrol.ClosureCommitted, chain.CommitReceiptID))
	})

	return chain
}

// assertChainIsComplete fails unless every control table holds at least one row
// for the chain's tenant. It is the check that keeps seedControlChain honest: a
// table added to migration 00024 without a corresponding write here would
// otherwise silently leave the mutation sweep with nothing to mutate.
func assertChainIsComplete(t *testing.T, db *pgtest.DB, chain controlChain) {
	t.Helper()
	for _, table := range controlTables {
		if n := countRows(t, db, table, chain.Tenant); n == 0 {
			t.Errorf("%s holds no row for the seeded chain", table)
		}
	}
}

// laterThan is the chain's clock moved forward, so a transition written after
// the chain never lands on the same instant as the row it advances.
func laterThan(offset time.Duration) time.Time { return fixedInstant.Add(offset) }
