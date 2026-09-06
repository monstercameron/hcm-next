package intentcontrol_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/intentcontrol"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

// controlTables is the closed, named set of tables migration 00024 materializes
// for DB-011. Naming every member rather than counting them is what makes a
// future migration accidentally adding, or a typo silently dropping, one of
// them show up as a one-line diff.
var controlTables = []string{
	"hcm_change_request",
	"intent_certificate",
	"intent_closure",
	"intent_decision",
	"intent_input_snapshot",
	"intent_instance_context",
	"intent_relationship",
	"intent_result",
	"intent_simulation_result",
	"proposal_approval_requirement",
	"proposal_effect_item",
	"proposal_obligation",
	"proposal_write_item",
	"repair_plan",
	"transaction_abort_receipt",
	"transaction_ambiguity",
	"transaction_commit_receipt",
	"transaction_correction",
	"transaction_plan",
	"transaction_plan_binding",
	"transaction_plan_effect",
}

// appendOnlyControlTables are the members that carry the forbid_mutation
// trigger. They are exactly the tables whose storage-disposition row may claim
// append_only: true, so this list and that registry have to agree.
var appendOnlyControlTables = []string{
	"intent_certificate",
	"intent_closure",
	"intent_decision",
	"intent_input_snapshot",
	"intent_instance_context",
	"intent_relationship",
	"intent_result",
	"intent_simulation_result",
	"proposal_approval_requirement",
	"proposal_effect_item",
	"proposal_obligation",
	"proposal_write_item",
	"transaction_abort_receipt",
	"transaction_commit_receipt",
	"transaction_correction",
	"transaction_plan",
	"transaction_plan_effect",
}

// liveControlTables are the four members that are live serving state: they have
// no forbid_mutation trigger and are UPDATE-granted, because their status
// genuinely advances.
var liveControlTables = []string{
	"hcm_change_request",
	"repair_plan",
	"transaction_ambiguity",
	"transaction_plan_binding",
}

// TestTodo_DB_011 is the PRIMARY case. Over one embedded-PostgreSQL database it
// exercises every table migration 00024 creates through this package's stores,
// and each subtest is one clause of DB-011's RED list made executable.
func TestTodo_DB_011(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	t.Run("schema: exactly the twenty-one DB-011 tables exist, seventeen append-only and four live", func(t *testing.T) {
		live := baseTables(t, db)
		for _, want := range controlTables {
			if !slices.Contains(live, want) {
				t.Fatalf("migration 00024 did not create %s (live tables: %v)", want, live)
			}
		}
		if len(appendOnlyControlTables)+len(liveControlTables) != len(controlTables) {
			t.Fatalf("the append-only and live lists (%d + %d) do not partition the %d control tables",
				len(appendOnlyControlTables), len(liveControlTables), len(controlTables))
		}
		for _, table := range appendOnlyControlTables {
			if !hasAppendOnlyTrigger(t, db, table) {
				t.Errorf("%s claims to be append-only but carries no forbid_mutation trigger", table)
			}
		}
		for _, table := range liveControlTables {
			if hasAppendOnlyTrigger(t, db, table) {
				t.Errorf("%s is live serving state but carries a forbid_mutation trigger", table)
			}
		}
		// Row level security is the property a missed ALTER would silently drop,
		// so it is checked per table rather than assumed from the migration's
		// loop having run at all.
		for _, table := range controlTables {
			if !hasTenantIsolation(t, db, table) {
				t.Errorf("%s has no enabled tenant_isolation policy", table)
			}
		}
	})

	t.Run("instance context: an origin is TRUSTED only when a source authority attests it, and the row cannot be relabelled", func(t *testing.T) {
		tenant := insertTenant(t, db, "db011-context")
		intent := insertIntent(t, db, tenant, "idem-context-1")
		conn := appConn(t, db)
		var store intentcontrol.ContextStore

		// A TRUSTED origin with no attestation is refused before any statement
		// runs: this is the caller-spoofing clause of RED.
		spoofed := referenceContext(tenant, intent)
		spoofed.OriginTrust = intentcontrol.OriginTrusted
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return store.Record(ctx, tx, spoofed)
		})
		if !errors.Is(err, intentcontrol.ErrInvalidRow) {
			t.Fatalf("TRUSTED origin without attestation: got %v, want ErrInvalidRow", err)
		}
		// ... and so is an unattested claim dressed the other way round.
		overclaimed := referenceContext(tenant, intent)
		overclaimed.SourceAuthoritySnapshotDigest = digestOf("authority")
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return store.Record(ctx, tx, overclaimed)
		})
		if !errors.Is(err, intentcontrol.ErrInvalidRow) {
			t.Fatalf("ASSERTED origin carrying an attestation: got %v, want ErrInvalidRow", err)
		}
		if n := countRows(t, db, "intent_instance_context", tenant); n != 0 {
			t.Fatalf("a refused context wrote %d row(s)", n)
		}

		want := referenceContext(tenant, intent)
		want.OriginTrust = intentcontrol.OriginTrusted
		want.SourceAuthoritySnapshotDigest = digestOf("authority")
		want.OriginEventRef = "event:hire-approved"
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return store.Record(ctx, tx, want)
		})

		var got intentcontrol.InstanceContext
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			got, err = store.Load(ctx, tx, tenant, intent)
			return err
		})
		if got.OriginTrust != intentcontrol.OriginTrusted ||
			got.SourceAuthoritySnapshotDigest != want.SourceAuthoritySnapshotDigest ||
			got.OriginEventRef != want.OriginEventRef ||
			got.IntentFamily != want.IntentFamily ||
			got.ExecutionMode != want.ExecutionMode ||
			got.CorrelationID != want.CorrelationID {
			t.Fatalf("stored context = %+v, want the recorded one %+v", got, want)
		}
		if got.CausationID != "" {
			t.Errorf("a root intent came back with causation %q", got.CausationID)
		}

		// A second context row is a duplicate, not an update: the table is
		// append-only, so "re-record it differently" has no meaning.
		second := want
		second.OriginTrust = intentcontrol.OriginUnverified
		second.SourceAuthoritySnapshotDigest = ""
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return store.Record(ctx, tx, second)
		})
		if !errors.Is(err, intentcontrol.ErrDuplicate) {
			t.Fatalf("second context row: got %v, want ErrDuplicate", err)
		}
	})

	t.Run("change request: compare-and-swap advances it, a stale version writes nothing, an illegal succession is refused", func(t *testing.T) {
		tenant := insertTenant(t, db, "db011-request")
		intent := insertIntent(t, db, tenant, "idem-request-1")
		conn := appConn(t, db)
		var store intentcontrol.ChangeRequestStore

		var opened intentcontrol.ChangeRequest
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			opened, err = store.Open(ctx, tx, referenceChangeRequest(tenant, intent))
			return err
		})
		if opened.RequestVersion != 1 || opened.RequestStatus != intentcontrol.RequestDraft {
			t.Fatalf("opened request = %s at version %d, want DRAFT at 1", opened.RequestStatus, opened.RequestVersion)
		}

		later := fixedInstant.Add(time.Hour)
		var advanced intentcontrol.ChangeRequest
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			advanced, err = store.Transition(ctx, tx, tenant, opened.ChangeRequestID, 1,
				intentcontrol.RequestPreflighted, later)
			return err
		})
		if advanced.RequestVersion != 2 || advanced.RequestStatus != intentcontrol.RequestPreflighted {
			t.Fatalf("advanced request = %s at version %d, want PREFLIGHTED at 2",
				advanced.RequestStatus, advanced.RequestVersion)
		}

		// The stale writer loses, and loses without writing.
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := store.Transition(ctx, tx, tenant, opened.ChangeRequestID, 1,
				intentcontrol.RequestSubmitted, later)
			return err
		})
		if !errors.Is(err, intentcontrol.ErrVersionConflict) {
			t.Fatalf("stale transition: got %v, want ErrVersionConflict", err)
		}

		// DRAFT -> COMMITTED is not a step the lifecycle has, whatever the
		// version says.
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := store.Transition(ctx, tx, tenant, opened.ChangeRequestID, 2,
				intentcontrol.RequestCommitted, later)
			return err
		})
		if !errors.Is(err, intentcontrol.ErrIllegalTransition) {
			t.Fatalf("PREFLIGHTED -> COMMITTED: got %v, want ErrIllegalTransition", err)
		}

		var reread intentcontrol.ChangeRequest
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			reread, err = store.Load(ctx, tx, tenant, opened.ChangeRequestID)
			return err
		})
		if reread.RequestVersion != 2 || reread.RequestStatus != intentcontrol.RequestPreflighted {
			t.Fatalf("after two refusals the request is %s at version %d, want PREFLIGHTED at 2",
				reread.RequestStatus, reread.RequestVersion)
		}
	})

	t.Run("snapshots and simulations: both are immutable, and a simulation may not borrow another intent's baseline", func(t *testing.T) {
		tenant := insertTenant(t, db, "db011-sim")
		intent := insertIntent(t, db, tenant, "idem-sim-1")
		other := insertIntent(t, db, tenant, "idem-sim-2")
		insertRevision(t, db, tenant, intent, 1)
		conn := appConn(t, db)
		var (
			snapshots   intentcontrol.SnapshotStore
			simulations intentcontrol.SimulationStore
		)

		var first intentcontrol.InputSnapshot
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			first, err = snapshots.Record(ctx, tx, referenceSnapshot(tenant, intent, 1))
			return err
		})

		// Re-reading the baseline adds sequence 2; it never edits sequence 1.
		repeat := referenceSnapshot(tenant, intent, 1)
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := snapshots.Record(ctx, tx, repeat)
			return err
		})
		if !errors.Is(err, intentcontrol.ErrDuplicate) {
			t.Fatalf("re-recording sequence 1: got %v, want ErrDuplicate", err)
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, err := snapshots.Record(ctx, tx, referenceSnapshot(tenant, intent, 2))
			return err
		})

		// A snapshot taken for a different intent explains nothing about this
		// one, and the foreign key alone cannot see that.
		var foreign intentcontrol.InputSnapshot
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			foreign, err = snapshots.Record(ctx, tx, referenceSnapshot(tenant, other, 1))
			return err
		})
		borrowed := referenceSimulation(tenant, intent, 1, first.SnapshotID)
		borrowed.InputSnapshotID = foreign.SnapshotID
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := simulations.RecordResult(ctx, tx, borrowed)
			return err
		})
		if !errors.Is(err, intentcontrol.ErrSnapshotMismatch) {
			t.Fatalf("simulation over another intent's snapshot: got %v, want ErrSnapshotMismatch", err)
		}
		if n := countRows(t, db, "intent_simulation_result", tenant); n != 0 {
			t.Fatalf("a refused simulation wrote %d row(s)", n)
		}

		var stored intentcontrol.SimulationResult
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			stored, err = simulations.RecordResult(ctx, tx, referenceSimulation(tenant, intent, 1, first.SnapshotID))
			return err
		})
		if stored.Status != intentcontrol.SimulationReady || stored.InputSnapshotID != first.SnapshotID {
			t.Fatalf("stored simulation = %+v, want READY over snapshot %s", stored, first.SnapshotID)
		}

		// The published snapshot and simulation are immutable even to a raw
		// UPDATE by the owning role, because the trigger refuses the row.
		if err := db.ExecErr(`UPDATE intent_input_snapshot SET snapshot_digest = $1 WHERE tenant_id = $2`,
			digestOf("tampered"), tenant); err == nil {
			t.Fatal("a published input snapshot was rewritten by UPDATE")
		}
		if err := db.ExecErr(`UPDATE intent_simulation_result SET result_digest = $1 WHERE tenant_id = $2`,
			digestOf("tampered"), tenant); err == nil {
			t.Fatal("a published simulation result was rewritten by UPDATE")
		}
	})

	t.Run("proposal sets: the four sets are stored in the order the digest was computed over and cannot be re-recorded", func(t *testing.T) {
		tenant := insertTenant(t, db, "db011-sets")
		intent := insertIntent(t, db, tenant, "idem-sets-1")
		insertRevision(t, db, tenant, intent, 1)
		conn := appConn(t, db)
		var store intentcontrol.ProposalSetStore

		sets := referenceSets()
		sets.Writes = append(sets.Writes, intentcontrol.WriteItem{
			SubjectKind: "WORKER", SubjectID: "omar-reyes",
			ResourceKey: "assignment/grade", FieldPath: "grade",
			CurrentCanonicalText: "G7", ProposedCanonicalText: "G8",
			ExpectedRevision: "rev-7", SourceAuthorityDecision: "ALLOW",
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return store.Record(ctx, tx, tenant, intent, 1, sets)
		})

		var got intentcontrol.ProposalSets
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			got, err = store.Load(ctx, tx, tenant, intent, 1)
			return err
		})
		if len(got.Writes) != 2 || got.Writes[0].FieldPath != "base_pay" || got.Writes[1].FieldPath != "grade" {
			t.Fatalf("writes came back as %+v, want base_pay then grade", got.Writes)
		}
		if len(got.Effects) != 1 || got.Effects[0].CompensationRef == "" || got.Effects[0].ObservationRef == "" {
			t.Fatalf("effects came back as %+v, want one effect with a compensation and an observation", got.Effects)
		}
		if len(got.Approvals) != 1 || len(got.Obligations) != 1 {
			t.Fatalf("approvals=%d obligations=%d, want 1 and 1", len(got.Approvals), len(got.Obligations))
		}

		// An effect with nothing watching it is refused before any statement runs.
		unwatched := referenceSets()
		unwatched.Effects[0].ObservationRef = ""
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return store.Record(ctx, tx, tenant, intent, 1, unwatched)
		})
		if !errors.Is(err, intentcontrol.ErrInvalidRow) {
			t.Fatalf("effect with no observation: got %v, want ErrInvalidRow", err)
		}

		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return store.Record(ctx, tx, tenant, intent, 1, referenceSets())
		})
		if !errors.Is(err, intentcontrol.ErrDuplicate) {
			t.Fatalf("re-recording a revision's sets: got %v, want ErrDuplicate", err)
		}
		if n := countRows(t, db, "proposal_write_item", tenant); n != 2 {
			t.Fatalf("proposal_write_item holds %d rows, want the original 2", n)
		}
	})

	t.Run("relationships: parentage is single and immutable, and a longer cycle is refused by the ancestor walk", func(t *testing.T) {
		tenant := insertTenant(t, db, "db011-graph")
		a := insertIntent(t, db, tenant, "idem-graph-a")
		b := insertIntent(t, db, tenant, "idem-graph-b")
		c := insertIntent(t, db, tenant, "idem-graph-c")
		d := insertIntent(t, db, tenant, "idem-graph-d")
		conn := appConn(t, db)
		var store intentcontrol.RelationshipStore

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			if err := store.Link(ctx, tx, referenceRelationship(tenant, a, b, 1)); err != nil {
				return err
			}
			return store.Link(ctx, tx, referenceRelationship(tenant, b, c, 1))
		})

		// The one-hop cycle never reaches the database.
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return store.Link(ctx, tx, referenceRelationship(tenant, a, a, 2))
		})
		if !errors.Is(err, intentcontrol.ErrRelationshipCycle) {
			t.Fatalf("self-edge: got %v, want ErrRelationshipCycle", err)
		}

		// The three-hop cycle is three individually legal rows, and only the
		// ancestor walk can see it.
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return store.Link(ctx, tx, referenceRelationship(tenant, c, a, 1))
		})
		if !errors.Is(err, intentcontrol.ErrRelationshipCycle) {
			t.Fatalf("A->B->C->A: got %v, want ErrRelationshipCycle", err)
		}

		// A second parent for the same child is refused by the schema's own
		// single-parent constraint, surfaced as ErrDuplicate.
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return store.Link(ctx, tx, referenceRelationship(tenant, d, b, 1))
		})
		if !errors.Is(err, intentcontrol.ErrDuplicate) {
			t.Fatalf("re-parenting B under D: got %v, want ErrDuplicate", err)
		}

		var children []uuid.UUID
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			children, err = store.Children(ctx, tx, tenant, a, intentcontrol.RelationChildOf)
			return err
		})
		if len(children) != 1 || children[0] != b {
			t.Fatalf("children of A = %v, want [%s]", children, b)
		}
		if n := countRows(t, db, "intent_relationship", tenant); n != 2 {
			t.Fatalf("intent_relationship holds %d rows, want the original 2", n)
		}
	})

	t.Run("results, decisions and certificates: one result per revision, one vote per decider, one certificate per kind", func(t *testing.T) {
		tenant := insertTenant(t, db, "db011-evidence")
		intent := insertIntent(t, db, tenant, "idem-evidence-1")
		insertRevision(t, db, tenant, intent, 1)
		insertRevision(t, db, tenant, intent, 2)
		conn := appConn(t, db)
		var (
			results      intentcontrol.ResultStore
			decisions    intentcontrol.DecisionStore
			certificates intentcontrol.CertificateStore
		)

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return results.Record(ctx, tx, referenceResult(tenant, intent, 1, intentcontrol.ResultSimulated))
		})
		// A second result for the same revision does not overwrite the first --
		// RED's "result overwrites revision".
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return results.Record(ctx, tx, referenceResult(tenant, intent, 1, intentcontrol.ResultCommitted))
		})
		if !errors.Is(err, intentcontrol.ErrDuplicate) {
			t.Fatalf("second result for revision 1: got %v, want ErrDuplicate", err)
		}
		// A new revision gets its own result, which is how a corrected answer is
		// expressed.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return results.Record(ctx, tx, referenceResult(tenant, intent, 2, intentcontrol.ResultCommitted))
		})
		var stored intentcontrol.Result
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			stored, err = results.Load(ctx, tx, tenant, intent, 1)
			return err
		})
		if stored.Kind != intentcontrol.ResultSimulated {
			t.Fatalf("revision 1's result is %s, want the original SIMULATED", stored.Kind)
		}

		approval := referenceDecision(tenant, intent, 1, "req-second-level", "principal:director-1")
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return decisions.Record(ctx, tx, approval)
		})
		second := referenceDecision(tenant, intent, 1, "req-second-level", "principal:director-1")
		second.Outcome = intentcontrol.OutcomeRejected
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return decisions.Record(ctx, tx, second)
		})
		if !errors.Is(err, intentcontrol.ErrDuplicate) {
			t.Fatalf("a second vote by the same decider: got %v, want ErrDuplicate", err)
		}
		// A different decider on the same requirement is a legitimate second
		// vote, not a duplicate.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return decisions.Record(ctx, tx,
				referenceDecision(tenant, intent, 1, "req-second-level", "principal:vp-1"))
		})
		var count int
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			count, err = decisions.CountForRevision(ctx, tx, tenant, intent, 1)
			return err
		})
		if count != 2 {
			t.Fatalf("revision 1 carries %d decisions, want 2", count)
		}

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return certificates.Issue(ctx, tx, referenceCertificate(tenant, intent, 1, intentcontrol.CertificateApproval))
		})
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return certificates.Issue(ctx, tx, referenceCertificate(tenant, intent, 1, intentcontrol.CertificateApproval))
		})
		if !errors.Is(err, intentcontrol.ErrDuplicate) {
			t.Fatalf("a second APPROVAL certificate: got %v, want ErrDuplicate", err)
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return certificates.Issue(ctx, tx, referenceCertificate(tenant, intent, 1, intentcontrol.CertificateCommit))
		})
		if n := countRows(t, db, "intent_certificate", tenant); n != 2 {
			t.Fatalf("intent_certificate holds %d rows, want 2", n)
		}
	})

	t.Run("transaction plan: an immutable plan, a compare-and-swap binding, and a plan that cannot both commit and abort", func(t *testing.T) {
		tenant := insertTenant(t, db, "db011-plan")
		intent := insertIntent(t, db, tenant, "idem-plan-1")
		insertRevision(t, db, tenant, intent, 1)
		conn := appConn(t, db)
		var (
			plans    intentcontrol.PlanStore
			bindings intentcontrol.PlanBindingStore
			receipts intentcontrol.ReceiptStore
		)

		plan := referencePlan(tenant, intent, 1, "plan-a")
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return plans.Compile(ctx, tx, plan, referencePlanEffects())
		})

		var binding intentcontrol.PlanBinding
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			binding, err = bindings.Load(ctx, tx, tenant, plan.PlanID)
			return err
		})
		if binding.State != intentcontrol.BindingDraft || binding.BindingVersion != 1 {
			t.Fatalf("a compiled plan opens at %s/%d, want DRAFT/1", binding.State, binding.BindingVersion)
		}

		later := fixedInstant.Add(time.Minute)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			for i, next := range []string{
				intentcontrol.BindingDomainValidated,
				intentcontrol.BindingGovernanceValidated,
			} {
				var err error
				binding, err = bindings.Transition(ctx, tx, tenant, plan.PlanID,
					uint64(i+1), next, "", later)
				if err != nil {
					return err
				}
			}
			return nil
		})
		// APPROVAL_BOUND without the approval's material digest is refused by the
		// schema; the store carries it because the caller supplies it.
		var bound intentcontrol.PlanBinding
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			bound, err = bindings.Transition(ctx, tx, tenant, plan.PlanID, 3,
				intentcontrol.BindingApprovalBound, digestOf("approval-binding"), later)
			return err
		})
		if bound.ApprovalDigest != digestOf("approval-binding") || bound.BindingVersion != 4 {
			t.Fatalf("bound plan = %+v, want the approval digest at version 4", bound)
		}
		// The binding carries the approval forward rather than dropping it on the
		// next transition.
		var reserved intentcontrol.PlanBinding
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			reserved, err = bindings.Transition(ctx, tx, tenant, plan.PlanID, 4,
				intentcontrol.BindingReserved, "", later)
			return err
		})
		if reserved.ApprovalDigest != digestOf("approval-binding") {
			t.Fatalf("RESERVED dropped the approval binding: %+v", reserved)
		}

		// Stale version, and an illegal jump.
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := bindings.Transition(ctx, tx, tenant, plan.PlanID, 4,
				intentcontrol.BindingReady, "", later)
			return err
		})
		if !errors.Is(err, intentcontrol.ErrVersionConflict) {
			t.Fatalf("stale binding transition: got %v, want ErrVersionConflict", err)
		}
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := bindings.Transition(ctx, tx, tenant, plan.PlanID, 5,
				intentcontrol.BindingCommitted, "", later)
			return err
		})
		if !errors.Is(err, intentcontrol.ErrIllegalTransition) {
			t.Fatalf("RESERVED -> COMMITTED: got %v, want ErrIllegalTransition", err)
		}

		// The plan itself never moves: it is append-only even to the owning role.
		if err := db.ExecErr(`UPDATE transaction_plan SET expires_at = now() WHERE tenant_id = $1`, tenant); err == nil {
			t.Fatal("a compiled plan was mutated in place")
		}

		// One outcome per plan, and never both.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return receipts.RecordCommit(ctx, tx, referenceCommitReceipt(plan))
		})
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return receipts.RecordAbort(ctx, tx, referenceAbortReceipt(plan))
		})
		if !errors.Is(err, intentcontrol.ErrReceiptConflict) {
			t.Fatalf("aborting a committed plan: got %v, want ErrReceiptConflict", err)
		}
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return receipts.RecordCommit(ctx, tx, referenceCommitReceipt(plan))
		})
		if !errors.Is(err, intentcontrol.ErrDuplicate) {
			t.Fatalf("committing a plan twice: got %v, want ErrDuplicate", err)
		}
		if n := countRows(t, db, "transaction_abort_receipt", tenant); n != 0 {
			t.Fatalf("transaction_abort_receipt holds %d rows for a committed plan", n)
		}

		var effects []intentcontrol.PlanEffect
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			effects, err = plans.Effects(ctx, tx, tenant, plan.PlanID)
			return err
		})
		if len(effects) != 1 || effects[0].CompensationStrategy == "" || effects[0].ObservationRef == "" {
			t.Fatalf("plan effects = %+v, want one effect with a compensation and an observation", effects)
		}
	})

	t.Run("ambiguity, correction, repair and closure: every terminal record names its target and its three digests", func(t *testing.T) {
		tenant := insertTenant(t, db, "db011-terminal")
		intent := insertIntent(t, db, tenant, "idem-terminal-1")
		corrector := insertIntent(t, db, tenant, "idem-terminal-2")
		insertRevision(t, db, tenant, intent, 1)
		conn := appConn(t, db)
		var (
			plans       intentcontrol.PlanStore
			receipts    intentcontrol.ReceiptStore
			ambiguities intentcontrol.AmbiguityStore
			corrections intentcontrol.CorrectionStore
			repairs     intentcontrol.RepairPlanStore
			closures    intentcontrol.ClosureStore
		)

		plan := referencePlan(tenant, intent, 1, "plan-terminal")
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return plans.Compile(ctx, tx, plan, referencePlanEffects())
		})

		ambiguity := intentcontrol.Ambiguity{
			TenantID:    tenant,
			AmbiguityID: uuid.New(),
			PlanID:      plan.PlanID,
			DetectedAt:  fixedInstant,
			Detail:      "connection dropped after COMMIT was sent",
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return ambiguities.Open(ctx, tx, ambiguity)
		})
		// A resolution with no evidence is a guess, and this store records no
		// guesses.
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := ambiguities.Resolve(ctx, tx, tenant, ambiguity.AmbiguityID, 1,
				intentcontrol.AmbiguityOutcomeCommitted, "", fixedInstant.Add(time.Minute))
			return err
		})
		if !errors.Is(err, intentcontrol.ErrInvalidRow) {
			t.Fatalf("resolution without evidence: got %v, want ErrInvalidRow", err)
		}
		var resolved intentcontrol.Ambiguity
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			resolved, err = ambiguities.Resolve(ctx, tx, tenant, ambiguity.AmbiguityID, 1,
				intentcontrol.AmbiguityOutcomeCommitted, "receipt:txid-4711", fixedInstant.Add(time.Minute))
			return err
		})
		if resolved.ResolutionState != intentcontrol.AmbiguityResolved || resolved.Version != 2 {
			t.Fatalf("resolved ambiguity = %+v, want RESOLVED at version 2", resolved)
		}
		// Reopening a resolved ambiguity would erase the evidence the first
		// resolution stands on.
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := ambiguities.Resolve(ctx, tx, tenant, ambiguity.AmbiguityID, 2,
				intentcontrol.AmbiguityOutcomeAborted, "receipt:other", fixedInstant.Add(2*time.Minute))
			return err
		})
		if !errors.Is(err, intentcontrol.ErrIllegalTransition) {
			t.Fatalf("re-resolving: got %v, want ErrIllegalTransition", err)
		}

		receipt := referenceCommitReceipt(plan)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return receipts.RecordCommit(ctx, tx, receipt)
		})

		// A correction with no target cannot even be built.
		targetless := referenceCorrection(tenant, receipt.ReceiptID, corrector)
		targetless.CorrectsReceiptID = uuid.Nil
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return corrections.Record(ctx, tx, targetless)
		})
		if !errors.Is(err, intentcontrol.ErrInvalidRow) {
			t.Fatalf("correction with no target: got %v, want ErrInvalidRow", err)
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return corrections.Record(ctx, tx, referenceCorrection(tenant, receipt.ReceiptID, corrector))
		})

		repair := referenceRepairPlan(tenant, plan.PlanID)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return repairs.Open(ctx, tx, repair)
		})
		var advanced intentcontrol.RepairPlan
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			advanced, err = repairs.Transition(ctx, tx, tenant, repair.RepairPlanID, 1,
				intentcontrol.RepairInProgress, fixedInstant.Add(time.Hour))
			return err
		})
		if advanced.Status != intentcontrol.RepairInProgress || advanced.Version != 2 {
			t.Fatalf("advanced repair = %+v, want IN_PROGRESS at version 2", advanced)
		}
		if advanced.IntentDigest == "" || advanced.ProposalDigest == "" || advanced.ControlDigest == "" {
			t.Fatalf("a repair plan lost one of its three digests: %+v", advanced)
		}

		// A withdrawn intent executed nothing, so it may not name a receipt; a
		// committed one must.
		bad := referenceClosure(tenant, intent, intentcontrol.ClosureWithdrawn, uuid.Nil)
		bad.ExecutionReceiptID = receipt.ReceiptID
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return closures.Close(ctx, tx, bad)
		})
		if !errors.Is(err, intentcontrol.ErrInvalidRow) {
			t.Fatalf("WITHDRAWN closure naming a receipt: got %v, want ErrInvalidRow", err)
		}
		missing := referenceClosure(tenant, intent, intentcontrol.ClosureCommitted, uuid.Nil)
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return closures.Close(ctx, tx, missing)
		})
		if !errors.Is(err, intentcontrol.ErrInvalidRow) {
			t.Fatalf("COMMITTED closure naming no receipt: got %v, want ErrInvalidRow", err)
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return closures.Close(ctx, tx, referenceClosure(tenant, intent, intentcontrol.ClosureCommitted, receipt.ReceiptID))
		})
		var closure intentcontrol.Closure
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			closure, err = closures.Load(ctx, tx, tenant, intent)
			return err
		})
		if closure.ExecutionReceiptID != receipt.ReceiptID {
			t.Fatalf("closure points at receipt %s, want %s", closure.ExecutionReceiptID, receipt.ReceiptID)
		}
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return closures.Close(ctx, tx, referenceClosure(tenant, intent, intentcontrol.ClosureCorrected, receipt.ReceiptID))
		})
		if !errors.Is(err, intentcontrol.ErrDuplicate) {
			t.Fatalf("closing an intent twice: got %v, want ErrDuplicate", err)
		}
	})
}

// baseTables returns the schema's own base tables, ordered, excluding Goose's
// version table and the physical partitions of a partitioned table -- the same
// walk internal/data/schema's own suite performs.
func baseTables(t *testing.T, db *pgtest.DB) []string {
	t.Helper()
	rows, err := db.Conn.Query(context.Background(), `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema()
		  AND c.relkind IN ('r', 'p')
		  AND NOT c.relispartition
		  AND c.relname <> 'goose_db_version'
		ORDER BY c.relname`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list tables: %v", err)
	}
	sort.Strings(names)
	return names
}

// hasAppendOnlyTrigger reports whether table carries a row trigger that calls
// forbid_mutation. Reading the catalog rather than trusting the migration text
// is what makes the append-only claim a fact about the live schema.
func hasAppendOnlyTrigger(t *testing.T, db *pgtest.DB, table string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), `
		SELECT count(*)
		FROM pg_trigger tg
		JOIN pg_class c ON c.oid = tg.tgrelid
		JOIN pg_namespace ns ON ns.oid = c.relnamespace
		JOIN pg_proc p ON p.oid = tg.tgfoid
		WHERE ns.nspname = current_schema()
		  AND c.relname = $1
		  AND p.proname = 'forbid_mutation'
		  AND NOT tg.tgisinternal`, table).Scan(&n); err != nil {
		t.Fatalf("read triggers of %s: %v", table, err)
	}
	return n > 0
}

// hasTenantIsolation reports whether table has row level security enabled,
// forced, and a policy named tenant_isolation.
func hasTenantIsolation(t *testing.T, db *pgtest.DB, table string) bool {
	t.Helper()
	var (
		enabled  bool
		forced   bool
		policies int
	)
	if err := db.QueryRow(context.Background(), `
		SELECT c.relrowsecurity, c.relforcerowsecurity,
			(SELECT count(*) FROM pg_policy pol
			 WHERE pol.polrelid = c.oid AND pol.polname = 'tenant_isolation')
		FROM pg_class c
		JOIN pg_namespace ns ON ns.oid = c.relnamespace
		WHERE ns.nspname = current_schema() AND c.relname = $1`,
		table).Scan(&enabled, &forced, &policies); err != nil {
		t.Fatalf("read row level security of %s: %v", table, err)
	}
	return enabled && forced && policies == 1
}

// referenceSimulation is a valid READY simulation result for (intent, revision)
// over snapshot.
func referenceSimulation(tenant, intent uuid.UUID, revision uint64, snapshot uuid.UUID) intentcontrol.SimulationResult {
	return intentcontrol.SimulationResult{
		TenantID:        tenant,
		SimulationID:    uuid.New(),
		IntentID:        intent,
		Revision:        revision,
		Sequence:        1,
		InputSnapshotID: snapshot,
		Status:          intentcontrol.SimulationReady,
		ResultDigest:    digestOf("simulation-result"),
		ProposalDigest:  digestOf("proposal-material"),
		ControlDigest:   digestOf("control"),
		SchemaRef:       "hcmnext.intents.v1.SimulationResult",
		Body:            json.RawMessage(`{"delta":"12000.00"}`),
		SimulatedAt:     fixedInstant,
	}
}

// referenceRelationship is a valid CHILD_OF edge from parent to child.
func referenceRelationship(tenant, parent, child uuid.UUID, ordinal uint32) intentcontrol.Relationship {
	return intentcontrol.Relationship{
		TenantID:            tenant,
		RelationshipID:      uuid.New(),
		Type:                intentcontrol.RelationChildOf,
		Parent:              parent,
		Child:               child,
		Ordinal:             ordinal,
		MaterialInputDigest: digestOf("child-input:" + child.String()),
		EstablishedAt:       fixedInstant,
	}
}

// referenceResult is a valid result for (intent, revision).
func referenceResult(tenant, intent uuid.UUID, revision uint64, kind string) intentcontrol.Result {
	return intentcontrol.Result{
		TenantID:       tenant,
		IntentID:       intent,
		Revision:       revision,
		Kind:           kind,
		ResultDigest:   digestOf("result"),
		ProposalDigest: digestOf("proposal-material"),
		ControlDigest:  digestOf("control"),
		SchemaRef:      "hcmnext.intents.v1.IntentResult",
		Body:           json.RawMessage(`{"outcome":"ok"}`),
		ProducedBy:     "coordinator",
		ProducedAt:     fixedInstant,
	}
}

// referenceDecision is a valid APPROVED human approval on one requirement.
func referenceDecision(tenant, intent uuid.UUID, revision uint64, requirement, decider string) intentcontrol.Decision {
	return intentcontrol.Decision{
		TenantID:         tenant,
		DecisionID:       uuid.New(),
		IntentID:         intent,
		Revision:         revision,
		RequirementID:    requirement,
		Kind:             intentcontrol.DecisionHumanApproval,
		Outcome:          intentcontrol.OutcomeApproved,
		ProposalDigest:   digestOf("proposal-material"),
		ControlDigest:    digestOf("control"),
		MaterialityClass: intentcontrol.Material,
		DecidedBy:        decider,
		AuthorityRef:     "authority:compensation-approver",
		Reason:           "within band",
		DecidedAt:        fixedInstant,
	}
}

// referenceCertificate is a valid certificate of one kind for (intent, revision).
func referenceCertificate(tenant, intent uuid.UUID, revision uint64, kind string) intentcontrol.Certificate {
	return intentcontrol.Certificate{
		TenantID:       tenant,
		CertificateID:  uuid.New(),
		IntentID:       intent,
		Revision:       revision,
		Kind:           kind,
		Digest:         digestOf("certificate:" + kind),
		ProposalDigest: digestOf("proposal-material"),
		ControlDigest:  digestOf("control"),
		ArtifactRef:    "artifact:certificate/" + kind,
		IssuedBy:       "governance",
		IssuedAt:       fixedInstant,
	}
}

// referenceCorrection is a valid correction of one commit receipt.
func referenceCorrection(tenant, receipt, correcting uuid.UUID) intentcontrol.Correction {
	return intentcontrol.Correction{
		TenantID:          tenant,
		CorrectionID:      uuid.New(),
		CorrectsReceiptID: receipt,
		CorrectingIntent:  correcting,
		IntentDigest:      digestOf("intent:" + correcting.String()),
		ProposalDigest:    digestOf("proposal-material"),
		ControlDigest:     digestOf("control"),
		Reason:            "effective date was one month early",
		CorrectedBy:       "principal:hr-ops",
		CorrectedAt:       fixedInstant.Add(48 * time.Hour),
	}
}

// referenceRepairPlan is a valid OPEN repair plan for plan.
func referenceRepairPlan(tenant, plan uuid.UUID) intentcontrol.RepairPlan {
	return intentcontrol.RepairPlan{
		TenantID:       tenant,
		RepairPlanID:   uuid.New(),
		PlanID:         plan,
		IntentDigest:   digestOf("intent-repair"),
		ProposalDigest: digestOf("proposal-material"),
		ControlDigest:  digestOf("control"),
		Status:         intentcontrol.RepairOpen,
		Strategy:       "REVERSE_AND_REAPPLY",
		SchemaRef:      "hcmnext.transactions.v1.RepairPlan",
		Body:           json.RawMessage(`{"steps":["reverse","reapply"]}`),
		OpenedBy:       "coordinator",
		OpenedAt:       fixedInstant,
	}
}

// referenceClosure is a valid closure of one intent.
func referenceClosure(tenant, intent uuid.UUID, kind string, receipt uuid.UUID) intentcontrol.Closure {
	return intentcontrol.Closure{
		TenantID:           tenant,
		IntentID:           intent,
		Kind:               kind,
		Reason:             "terminal",
		IntentDigest:       digestOf("intent:" + intent.String()),
		ProposalDigest:     digestOf("proposal-material"),
		ControlDigest:      digestOf("control"),
		ExecutionReceiptID: receipt,
		ClosedBy:           "coordinator",
		ClosedAt:           fixedInstant.Add(time.Hour),
	}
}
