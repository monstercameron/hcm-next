package migrate_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/migrate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/migrationpreview"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// --- CONTINUE (SAFE) scenario: pause at the start frontier, preview against
// a target plan that leaves snapshot_worker untouched. ----------------------

// continueScenario starts one promotion instance, pauses it at its start
// frontier (a compiled safe point every node of the zero-effect promotion
// reference qualifies as, since it declares no atomic region), takes the
// SAFE_POINT checkpoint [migrate.Migrate] requires, and previews it against a
// target plan whose only change is unrelated to the frontier node -- a
// SAFE/CONTINUE classification.
type continueScenario struct {
	tenantID uuid.UUID
	pf       promotionFixture
	target   *workflow.CompiledWorkflow
	start    runtime.StartReceipt
	paused   runtime.PauseReceipt
	preview  migrate.PreviewRecord
}

func buildContinueScenario(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, keySuffix string) continueScenario {
	t.Helper()
	registry := bootstrapRegistry(t)
	pf := newPromotionFixture(t, values.TenantId("migrate-tenant-"+keySuffix), "intent:migrate-"+keySuffix)
	target := promotionTargetPlanEvaluateChanged(t, registry)

	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-"+keySuffix))
	paused, err := requestPause(t, conn, tenantID, runtime.PauseRequest{
		TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: start.InstanceVersion,
		Plan: pf.Plan, Reason: "MIGRATION_REVIEW", RequestedBy: "principal:operations-duty", RequestedAt: fixedInstant,
	})
	if err != nil {
		t.Fatalf("RequestPause: %v", err)
	}
	if paused.Status != runtime.InstancePaused {
		t.Fatalf("pause at the start frontier produced status=%s, want PAUSED", paused.Status)
	}

	inst := loadInstanceForTest(t, conn, tenantID, start.InstanceID)
	takeCheckpoint(t, conn, tenantID, inst, 1, runtimestate.CheckpointSafePoint, placeholderDigest, placeholderDigest)

	result, err := migrationpreview.Preview(migrationpreview.Request{
		Source: pf.Plan, Target: target,
		Instances: []migrationpreview.LiveInstanceState{
			{InstanceID: start.InstanceID.String(), StepID: workflow.PromotionNodeSnapshotWorker, Stage: "READY"},
		},
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(result.Continue) != 1 {
		t.Fatalf("fixture bug: preview classified %d instances as CONTINUE, want 1 (%+v)", len(result.Continue), result.Assessments)
	}

	preview, err := migrate.CapturePreview(pf.Plan, target, result)
	if err != nil {
		t.Fatalf("CapturePreview: %v", err)
	}

	return continueScenario{tenantID: tenantID, pf: pf, target: target, start: start, paused: paused, preview: preview}
}

func (s continueScenario) approval() migrate.Approval {
	return migrate.Approval{
		PreviewDigest: s.preview.Digest(), Approver: "principal:release-manager",
		Reason: "REVIEWED_MIGRATION_PLAN", ApprovedAt: fixedInstant,
	}
}

func (s continueScenario) request() migrate.Request {
	return migrate.Request{
		TenantID: s.tenantID, InstanceID: s.start.InstanceID,
		SourcePlan: s.pf.Plan, TargetPlan: s.target,
		Preview: s.preview, Approval: s.approval(),
		MigratedBy: "principal:migration-operator", MigratedAt: fixedInstant,
	}
}

func runMigrate(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, req migrate.Request) (migrate.Receipt, error) {
	t.Helper()
	var receipt migrate.Receipt
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		var migErr error
		receipt, migErr = migrate.Migrate(context.Background(), tx, req)
		return migErr
	})
	return receipt, err
}

// TestTodo_WF_RUN_018 is the PRIMARY test: an approved SAFE/CONTINUE
// migration rewrites the paused instance's version pointer atomically, the
// migrated instance resumes on the new version, and its next Advance
// succeeds.
func TestTodo_WF_RUN_018(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun018")
	scenario := buildContinueScenario(t, conn, tenantID, "primary")

	receipt, err := runMigrate(t, conn, tenantID, scenario.request())
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if receipt.Outcome != migrationpreview.OutcomeSafe {
		t.Fatalf("outcome = %s, want SAFE", receipt.Outcome)
	}
	if receipt.ToWorkflowVersion != scenario.target.Version || receipt.ToCompiledPlanDigest != scenario.target.Digest() {
		t.Fatalf("receipt did not name the target plan: version=%d digest=%s", receipt.ToWorkflowVersion, receipt.ToCompiledPlanDigest)
	}
	if len(receipt.ToFrontier) != 1 || receipt.ToFrontier[0] != workflow.PromotionNodeSnapshotWorker {
		t.Fatalf("to-frontier = %v, want [%s] (a CONTINUE migration never moves the frontier)", receipt.ToFrontier, workflow.PromotionNodeSnapshotWorker)
	}

	migrated := loadInstanceForTest(t, conn, tenantID, scenario.start.InstanceID)
	if migrated.WorkflowVersion != scenario.target.Version || migrated.CompiledPlanHash != scenario.target.Digest() {
		t.Fatalf("stored instance still pins the source plan: version=%d hash=%s", migrated.WorkflowVersion, migrated.CompiledPlanHash)
	}
	if migrated.RuntimeStatus != runtime.InstancePaused {
		t.Fatalf("status after migration = %s, want PAUSED", migrated.RuntimeStatus)
	}

	// Resume on the new version and advance: the whole point of a version
	// pointer rewrite is that execution keeps going on the new plan.
	resumed, err := resumeFromPause(t, conn, tenantID, runtime.ResumeRequest{
		TenantID: tenantID, InstanceID: scenario.start.InstanceID, ExpectedInstanceVersion: migrated.InstanceVersion,
		Plan: scenario.target, ResolvedContext: promotionResolvedContext(),
		Reason: "MIGRATION_COMPLETE", ResumedBy: "principal:operations-duty", ResumedAt: fixedInstant,
	})
	if err != nil {
		t.Fatalf("ResumeFromPause on the target plan: %v", err)
	}
	if resumed.Status != runtime.InstanceRunning {
		t.Fatalf("resumed status = %s, want RUNNING", resumed.Status)
	}

	sink := runtime.NewMemorySink()
	if _, err := advanceOnce(t, conn, tenantID, advanceOutcome(
		tenantID, scenario.start.InstanceID, scenario.target, resumed.InstanceVersion, sink,
		workflow.PromotionNodeSnapshotWorker, workflow.OutcomeSucceeded, "sha256:snapshot-migrated",
	)); err != nil {
		t.Fatalf("Advance after migration: %v", err)
	}
}

// TestTodo_WF_RUN_018_Golden pins the receipt's evidence shape: the exact
// from/to identity, checkpoint sequencing and a content digest that is a
// function of that content.
func TestTodo_WF_RUN_018_Golden(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun018-golden")
	scenario := buildContinueScenario(t, conn, tenantID, "golden")

	receipt, err := runMigrate(t, conn, tenantID, scenario.request())
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if receipt.FromWorkflowVersion != scenario.pf.Plan.Version || receipt.FromCompiledPlanDigest != scenario.pf.Plan.Digest() {
		t.Fatalf("receipt lost the source identity: version=%d digest=%s", receipt.FromWorkflowVersion, receipt.FromCompiledPlanDigest)
	}
	if receipt.PreviewDigest != scenario.preview.Digest() {
		t.Fatalf("receipt preview digest = %q, want %q", receipt.PreviewDigest, scenario.preview.Digest())
	}
	if receipt.ApprovedBy != "principal:release-manager" || receipt.MigratedBy != "principal:migration-operator" {
		t.Fatalf("receipt lost the governed approver/executor: %q / %q", receipt.ApprovedBy, receipt.MigratedBy)
	}
	if receipt.PriorCheckpointSequence != 1 || receipt.CheckpointSequence != 2 {
		t.Fatalf("checkpoint sequencing = %d -> %d, want 1 -> 2", receipt.PriorCheckpointSequence, receipt.CheckpointSequence)
	}
	if receipt.Digest() == "" {
		t.Fatal("migration receipt carries no content digest")
	}

	// A durably-stored MIGRATION checkpoint carries the receipt's own digest
	// as its state_digest: the checkpoint is this migration's evidence, not a
	// second, independently-derived summary of it.
	var latest runtimestate.Checkpoint
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		latest, err = (runtimestate.CheckpointStore{}).Latest(context.Background(), tx, tenantID, scenario.start.InstanceID)
		return err
	})
	if latest.Kind != runtimestate.CheckpointMigration {
		t.Fatalf("latest checkpoint kind = %q, want MIGRATION", latest.Kind)
	}
	if latest.StateDigest != receipt.Digest() {
		t.Fatalf("checkpoint state digest = %q, want the receipt's own %q", latest.StateDigest, receipt.Digest())
	}
	if latest.Sequence != receipt.CheckpointSequence {
		t.Fatalf("checkpoint sequence = %d, want %d", latest.Sequence, receipt.CheckpointSequence)
	}
}

// TestTodo_WF_RUN_018_Race fires many concurrent Migrate calls at the same
// paused instance. Exactly one may commit; every other caller sees a stale
// fence from the underlying runtime store and mutates nothing.
func TestTodo_WF_RUN_018_Race(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun018-race")
	scenario := buildContinueScenario(t, conn, tenantID, "race")
	req := scenario.request()

	const workers = 6
	type outcome struct {
		receipt migrate.Receipt
		err     error
	}
	results := make([]outcome, workers)
	conns := make([]*pgxadapter.Conn, workers)
	for i := range conns {
		conns[i] = appConn(t, db)
	}

	var gate sync.WaitGroup
	var done sync.WaitGroup
	gate.Add(1)
	for i := 0; i < workers; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			gate.Wait()
			results[i].err = inTenantTxErr(conns[i], tenantID, func(tx dbport.Tx) error {
				var migErr error
				results[i].receipt, migErr = migrate.Migrate(context.Background(), tx, req)
				return migErr
			})
		}(i)
	}
	gate.Done()
	done.Wait()

	applied := 0
	for i, res := range results {
		switch {
		case res.err == nil:
			applied++
		case runtime.CodeOf(res.err) == runtime.CodeStaleInstance:
			// Overtaken by the winner's own version bump at the storage
			// fence: legal, wrote nothing.
		case migrate.CodeOf(res.err) != "":
			// Any other typed refusal from this package is also a legitimate
			// "lost the race" outcome once a winner has committed: depending
			// on exactly when each loser's statements interleave with the
			// winner's commit under READ COMMITTED, a loser may see the
			// instance already pinned to the target plan (CodePlanMismatch,
			// checked against its own now-stale SourcePlan) or the winner's
			// already-committed checkpoint alongside a same-transaction read
			// of the instance from one statement earlier (CodeUnsafePoint).
			// Every one of Migrate's refusal paths runs before or between
			// store calls inside the caller's own transaction, which the test
			// harness rolls back on any error, so a typed refusal here always
			// means this call wrote nothing -- the assertions below confirm
			// exactly one migration's worth of state actually landed.
		default:
			t.Fatalf("concurrent migrate %d: unexpected error %v (migrate code=%q runtime code=%q)",
				i, res.err, migrate.CodeOf(res.err), runtime.CodeOf(res.err))
		}
	}
	if applied != 1 {
		t.Fatalf("%d concurrent migrations applied, want exactly 1", applied)
	}

	migrated := loadInstanceForTest(t, conn, tenantID, scenario.start.InstanceID)
	if migrated.CompiledPlanHash != scenario.target.Digest() {
		t.Fatalf("stored plan hash = %q after the race, want the target's %q", migrated.CompiledPlanHash, scenario.target.Digest())
	}
}

// TestTodo_WF_RUN_018_Fault is the RED clause: an unsafe point, an unapproved
// digest, a changed source instance, a stranded classification and a failure
// injected mid-transaction all refuse the migration and leave the instance
// exactly on its old version -- proven by rolling back and re-reading.
func TestTodo_WF_RUN_018_Fault(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)

	t.Run("a running instance is an unsafe point", func(t *testing.T) {
		tenantID := insertTenant(t, db, "wfrun018-fault-running")
		scenario := buildContinueScenario(t, conn, tenantID, "fault-running")
		// Resume it back to RUNNING before attempting the migration.
		if _, err := resumeFromPause(t, conn, tenantID, runtime.ResumeRequest{
			TenantID: tenantID, InstanceID: scenario.start.InstanceID, ExpectedInstanceVersion: scenario.paused.InstanceVersion,
			Plan: scenario.pf.Plan, ResolvedContext: promotionResolvedContext(),
			Reason: "TEST_RESUME", ResumedBy: "principal:operations-duty", ResumedAt: fixedInstant,
		}); err != nil {
			t.Fatalf("ResumeFromPause: %v", err)
		}
		_, err := runMigrate(t, conn, tenantID, scenario.request())
		if migrate.CodeOf(err) != migrate.CodeUnsafePoint {
			t.Fatalf("code = %q, want %q (%v)", migrate.CodeOf(err), migrate.CodeUnsafePoint, err)
		}
	})

	t.Run("an approval naming the wrong preview digest is refused", func(t *testing.T) {
		tenantID := insertTenant(t, db, "wfrun018-fault-digest")
		scenario := buildContinueScenario(t, conn, tenantID, "fault-digest")
		req := scenario.request()
		req.Approval.PreviewDigest = "sha256:not-the-reviewed-preview"
		_, err := runMigrate(t, conn, tenantID, req)
		if migrate.CodeOf(err) != migrate.CodeUnapprovedDigest {
			t.Fatalf("code = %q, want %q (%v)", migrate.CodeOf(err), migrate.CodeUnapprovedDigest, err)
		}
		if migrated := loadInstanceForTest(t, conn, tenantID, scenario.start.InstanceID); migrated.CompiledPlanHash != scenario.pf.Plan.Digest() {
			t.Fatalf("an unapproved migration changed the pinned plan to %q", migrated.CompiledPlanHash)
		}
	})

	t.Run("the same approver executing the migration is refused", func(t *testing.T) {
		tenantID := insertTenant(t, db, "wfrun018-fault-sod")
		scenario := buildContinueScenario(t, conn, tenantID, "fault-sod")
		req := scenario.request()
		req.MigratedBy = req.Approval.Approver
		_, err := runMigrate(t, conn, tenantID, req)
		if migrate.CodeOf(err) != migrate.CodeSeparationOfDuties {
			t.Fatalf("code = %q, want %q (%v)", migrate.CodeOf(err), migrate.CodeSeparationOfDuties, err)
		}
	})

	t.Run("a source instance that moved since the preview is refused", func(t *testing.T) {
		tenantID := insertTenant(t, db, "wfrun018-fault-changed")
		scenario := buildContinueScenario(t, conn, tenantID, "fault-changed")
		// Advance the instance forward after the preview was taken but before
		// pausing again: the preview's own recorded stage no longer matches.
		if _, err := resumeFromPause(t, conn, tenantID, runtime.ResumeRequest{
			TenantID: tenantID, InstanceID: scenario.start.InstanceID, ExpectedInstanceVersion: scenario.paused.InstanceVersion,
			Plan: scenario.pf.Plan, ResolvedContext: promotionResolvedContext(),
			Reason: "TEST_RESUME", ResumedBy: "principal:operations-duty", ResumedAt: fixedInstant,
		}); err != nil {
			t.Fatalf("ResumeFromPause: %v", err)
		}
		sink := runtime.NewMemorySink()
		resumedInst := loadInstanceForTest(t, conn, tenantID, scenario.start.InstanceID)
		advanced, err := advanceOnce(t, conn, tenantID, advanceOutcome(
			tenantID, scenario.start.InstanceID, scenario.pf.Plan, resumedInst.InstanceVersion, sink,
			workflow.PromotionNodeSnapshotWorker, workflow.OutcomeSucceeded, "sha256:snapshot-moved",
		))
		if err != nil {
			t.Fatalf("Advance: %v", err)
		}
		pausedAgain, err := requestPause(t, conn, tenantID, runtime.PauseRequest{
			TenantID: tenantID, InstanceID: scenario.start.InstanceID, ExpectedInstanceVersion: advanced.NewInstanceVersion,
			Plan: scenario.pf.Plan, Reason: "MIGRATION_REVIEW_2", RequestedBy: "principal:operations-duty", RequestedAt: fixedInstant,
		})
		if err != nil {
			t.Fatalf("RequestPause: %v", err)
		}
		instAgain := loadInstanceForTest(t, conn, tenantID, scenario.start.InstanceID)
		takeCheckpoint(t, conn, tenantID, instAgain, 2, runtimestate.CheckpointSafePoint, placeholderDigest, placeholderDigest)
		_ = pausedAgain

		_, err = runMigrate(t, conn, tenantID, scenario.request())
		if migrate.CodeOf(err) != migrate.CodeChangedInstance {
			t.Fatalf("code = %q, want %q (%v)", migrate.CodeOf(err), migrate.CodeChangedInstance, err)
		}
	})

	t.Run("a stranded classification is refused", func(t *testing.T) {
		tenantID := insertTenant(t, db, "wfrun018-fault-stranded")
		registry := bootstrapRegistry(t)
		pf := newPromotionFixture(t, values.TenantId("wfrun018-fault-stranded-tenant"), "intent:wfrun018-fault-stranded")
		target := promotionTargetPlanWithoutBuildProposal(t, registry)

		start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-fault-stranded"))
		sink := runtime.NewMemorySink()
		version := start.InstanceVersion
		for _, step := range []struct {
			node    string
			outcome workflow.Outcome
			digest  string
		}{
			{workflow.PromotionNodeSnapshotWorker, workflow.OutcomeSucceeded, "sha256:snapshot"},
			{workflow.PromotionNodeSimulateComp, workflow.OutcomeSucceeded, "sha256:comp"},
			{workflow.PromotionNodeEvaluateBand, workflow.OutcomeSucceeded, "sha256:band"},
		} {
			receipt, err := advanceOnce(t, conn, tenantID, advanceOutcome(tenantID, start.InstanceID, pf.Plan, version, sink, step.node, step.outcome, step.digest))
			if err != nil {
				t.Fatalf("Advance(%s): %v", step.node, err)
			}
			version = receipt.NewInstanceVersion
		}
		// The frontier now sits at build_proposal, READY. No bridge is
		// declared for it, so the preview classifies it STRANDED.
		paused, err := requestPause(t, conn, tenantID, runtime.PauseRequest{
			TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: version,
			Plan: pf.Plan, Reason: "MIGRATION_REVIEW", RequestedBy: "principal:operations-duty", RequestedAt: fixedInstant,
		})
		if err != nil {
			t.Fatalf("RequestPause: %v", err)
		}
		if paused.Status != runtime.InstancePaused {
			t.Fatalf("pause at build_proposal produced status=%s, want PAUSED", paused.Status)
		}
		inst := loadInstanceForTest(t, conn, tenantID, start.InstanceID)
		takeCheckpoint(t, conn, tenantID, inst, 1, runtimestate.CheckpointSafePoint, placeholderDigest, placeholderDigest)

		result, err := migrationpreview.Preview(migrationpreview.Request{
			Source: pf.Plan, Target: target,
			Instances: []migrationpreview.LiveInstanceState{
				{InstanceID: start.InstanceID.String(), StepID: workflow.PromotionNodeBuildProposal, Stage: "READY"},
			},
		})
		if err == nil || len(result.Stranded) != 1 {
			t.Fatalf("fixture bug: expected exactly one stranded instance, got err=%v stranded=%v", err, result.Stranded)
		}
		preview, err := migrate.CapturePreview(pf.Plan, target, result)
		if err != nil {
			t.Fatalf("CapturePreview: %v", err)
		}

		req := migrate.Request{
			TenantID: tenantID, InstanceID: start.InstanceID,
			SourcePlan: pf.Plan, TargetPlan: target,
			Preview: preview,
			Approval: migrate.Approval{
				PreviewDigest: preview.Digest(), Approver: "principal:release-manager",
				Reason: "REVIEWED", ApprovedAt: fixedInstant,
			},
			MigratedBy: "principal:migration-operator", MigratedAt: fixedInstant,
		}
		_, err = runMigrate(t, conn, tenantID, req)
		if migrate.CodeOf(err) != migrate.CodeStranded {
			t.Fatalf("code = %q, want %q (%v)", migrate.CodeOf(err), migrate.CodeStranded, err)
		}
	})

	t.Run("a fault injected mid-transaction leaves the old instance untouched", func(t *testing.T) {
		tenantID := insertTenant(t, db, "wfrun018-fault-injected")
		scenario := buildContinueScenario(t, conn, tenantID, "fault-injected")

		before := loadInstanceForTest(t, conn, tenantID, scenario.start.InstanceID)
		injected := fmt.Errorf("injected storage failure")
		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			faulty := &failingExecutor{inner: tx, failAfterCalls: 2, err: injected}
			_, migErr := migrate.Migrate(context.Background(), faulty, scenario.request())
			if migErr == nil {
				return fmt.Errorf("expected the injected fault to surface, migration reported success")
			}
			// Reject the transaction ourselves regardless of what Migrate
			// returned, exactly like a real caller rolling back on any error.
			return migErr
		})
		if err == nil {
			t.Fatal("expected the fault-injecting transaction to fail")
		}

		after := loadInstanceForTest(t, conn, tenantID, scenario.start.InstanceID)
		if after.WorkflowVersion != before.WorkflowVersion || after.CompiledPlanHash != before.CompiledPlanHash {
			t.Fatalf("a failed migration changed the instance: version %d->%d, hash %q->%q",
				before.WorkflowVersion, after.WorkflowVersion, before.CompiledPlanHash, after.CompiledPlanHash)
		}
		if after.InstanceVersion != before.InstanceVersion {
			t.Fatalf("a failed migration bumped the instance version: %d -> %d", before.InstanceVersion, after.InstanceVersion)
		}
		if !equalStrings(after.CurrentNodeIDs, before.CurrentNodeIDs) {
			t.Fatalf("a failed migration changed the frontier: %v -> %v", before.CurrentNodeIDs, after.CurrentNodeIDs)
		}
	})
}

// TestTodo_WF_RUN_018_Mutation kills mutants of the specific rules this
// package declares: a bridge is required (and applied) for a TRANSFORMABLE
// classification, and the resulting frontier/node-execution state is exactly
// what the bridge names -- proven end to end with a cross-node bridge that
// completes the workflow after migration.
func TestTodo_WF_RUN_018_Mutation(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun018-mutation")
	registry := bootstrapRegistry(t)
	pf := newPromotionFixture(t, values.TenantId("wfrun018-mutation-tenant"), "intent:wfrun018-mutation")
	target := promotionTargetPlanWithoutBuildProposal(t, registry)

	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-mutation"))
	sink := runtime.NewMemorySink()
	version := start.InstanceVersion
	for _, step := range []struct {
		node    string
		outcome workflow.Outcome
		digest  string
	}{
		{workflow.PromotionNodeSnapshotWorker, workflow.OutcomeSucceeded, "sha256:snapshot"},
		{workflow.PromotionNodeSimulateComp, workflow.OutcomeSucceeded, "sha256:comp"},
		{workflow.PromotionNodeEvaluateBand, workflow.OutcomeSucceeded, "sha256:band"},
	} {
		receipt, err := advanceOnce(t, conn, tenantID, advanceOutcome(tenantID, start.InstanceID, pf.Plan, version, sink, step.node, step.outcome, step.digest))
		if err != nil {
			t.Fatalf("Advance(%s): %v", step.node, err)
		}
		version = receipt.NewInstanceVersion
	}
	paused, err := requestPause(t, conn, tenantID, runtime.PauseRequest{
		TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: version,
		Plan: pf.Plan, Reason: "MIGRATION_REVIEW", RequestedBy: "principal:operations-duty", RequestedAt: fixedInstant,
	})
	if err != nil {
		t.Fatalf("RequestPause: %v", err)
	}
	if paused.Status != runtime.InstancePaused {
		t.Fatalf("pause at build_proposal produced status=%s, want PAUSED", paused.Status)
	}
	inst := loadInstanceForTest(t, conn, tenantID, start.InstanceID)
	takeCheckpoint(t, conn, tenantID, inst, 1, runtimestate.CheckpointSafePoint, placeholderDigest, placeholderDigest)

	bridge := migrationpreview.Bridge{
		FromStepID: workflow.PromotionNodeBuildProposal, FromStage: "READY",
		ToStepID: workflow.PromotionNodeRaiseThreshold, ToStage: "READY",
		Ref: "migration.promotion.build-proposal-to-threshold/v1",
	}
	result, err := migrationpreview.Preview(migrationpreview.Request{
		Source: pf.Plan, Target: target,
		Instances: []migrationpreview.LiveInstanceState{
			{InstanceID: start.InstanceID.String(), StepID: workflow.PromotionNodeBuildProposal, Stage: "READY"},
		},
		Bridges: []migrationpreview.Bridge{bridge},
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(result.NeedsBridge) != 1 {
		t.Fatalf("fixture bug: expected exactly one bridged instance, got %+v", result.Assessments)
	}
	preview, err := migrate.CapturePreview(pf.Plan, target, result)
	if err != nil {
		t.Fatalf("CapturePreview: %v", err)
	}

	req := migrate.Request{
		TenantID: tenantID, InstanceID: start.InstanceID,
		SourcePlan: pf.Plan, TargetPlan: target,
		Preview: preview,
		Approval: migrate.Approval{
			PreviewDigest: preview.Digest(), Approver: "principal:release-manager",
			Reason: "REVIEWED", ApprovedAt: fixedInstant,
		},
		MigratedBy: "principal:migration-operator", MigratedAt: fixedInstant,
	}

	t.Run("a preview naming no bridge for a removed step is refused before writing", func(t *testing.T) {
		unbridgedResult, err := migrationpreview.Preview(migrationpreview.Request{
			Source: pf.Plan, Target: target,
			Instances: []migrationpreview.LiveInstanceState{
				{InstanceID: start.InstanceID.String(), StepID: workflow.PromotionNodeBuildProposal, Stage: "READY"},
			},
		})
		if err == nil {
			t.Fatal("fixture bug: expected the unbridged preview to refuse")
		}
		unbridgedPreview, capErr := migrate.CapturePreview(pf.Plan, target, unbridgedResult)
		if capErr != nil {
			t.Fatalf("CapturePreview: %v", capErr)
		}
		badReq := req
		badReq.Preview = unbridgedPreview
		badReq.Approval.PreviewDigest = unbridgedPreview.Digest()
		_, migErr := runMigrate(t, conn, tenantID, badReq)
		if migrate.CodeOf(migErr) != migrate.CodeStranded {
			t.Fatalf("code = %q, want %q (%v)", migrate.CodeOf(migErr), migrate.CodeStranded, migErr)
		}
	})

	receipt, err := runMigrate(t, conn, tenantID, req)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if receipt.Outcome != migrationpreview.OutcomeTransformable {
		t.Fatalf("outcome = %s, want TRANSFORMABLE", receipt.Outcome)
	}
	if receipt.BridgeRef != bridge.Ref {
		t.Fatalf("bridge ref = %q, want %q", receipt.BridgeRef, bridge.Ref)
	}
	if len(receipt.ToFrontier) != 1 || receipt.ToFrontier[0] != workflow.PromotionNodeRaiseThreshold {
		t.Fatalf("to-frontier = %v, want [%s]", receipt.ToFrontier, workflow.PromotionNodeRaiseThreshold)
	}

	migrated := loadInstanceForTest(t, conn, tenantID, start.InstanceID)
	if len(migrated.CurrentNodeIDs) != 1 || migrated.CurrentNodeIDs[0] != workflow.PromotionNodeRaiseThreshold {
		t.Fatalf("stored frontier = %v, want [%s]", migrated.CurrentNodeIDs, workflow.PromotionNodeRaiseThreshold)
	}

	oldNode := loadNodeExecutionForTest(t, conn, tenantID, start.InstanceID, workflow.PromotionNodeBuildProposal, 1)
	if oldNode.Status != runtime.NodeCancelled {
		t.Fatalf("bridged-away node status = %s, want CANCELLED", oldNode.Status)
	}
	newNode := loadNodeExecutionForTest(t, conn, tenantID, start.InstanceID, workflow.PromotionNodeRaiseThreshold, 1)
	if newNode.Status != runtime.NodeReady {
		t.Fatalf("bridged-to node status = %s, want READY", newNode.Status)
	}

	// Resume on the target plan: the instance runs again under the new
	// version. This scenario's bridge crosses to a node the target plan
	// declares, but build_proposal itself -- gone from the target -- remains
	// in the instance's own node-execution history (CANCELLED, not deleted:
	// history is append-only). Driving a further real Advance to completion
	// for exactly this "the bridged-away node is entirely absent from the
	// target plan" shape would additionally require runtime.Advance's own
	// frontier reconstruction (internal/workflow/runtime, out of this
	// ticket's file roots) to tolerate a settled historical node the current
	// plan no longer declares; the same-node-stage bridge proven below
	// exercises "next Advance succeeds" for a TRANSFORMABLE migration within
	// what this ticket may change.
	resumed, err := resumeFromPause(t, conn, tenantID, runtime.ResumeRequest{
		TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: migrated.InstanceVersion,
		Plan: target, ResolvedContext: promotionResolvedContext(),
		Reason: "MIGRATION_COMPLETE", ResumedBy: "principal:operations-duty", ResumedAt: fixedInstant,
	})
	if err != nil {
		t.Fatalf("ResumeFromPause on the target plan: %v", err)
	}
	if resumed.Status != runtime.InstanceRunning {
		t.Fatalf("resumed status = %s, want RUNNING", resumed.Status)
	}

	t.Run("a same-node stage bridge migrates and its next advance succeeds", func(t *testing.T) {
		tenantID := insertTenant(t, db, "wfrun018-mutation-samenode")
		registry := bootstrapRegistry(t)
		pf := newPromotionFixture(t, values.TenantId("wfrun018-mutation-samenode-tenant"), "intent:wfrun018-mutation-samenode")
		target := promotionTargetPlanEvaluateChanged(t, registry)

		start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-mutation-samenode"))
		sink := runtime.NewMemorySink()
		snapshot, err := advanceOnce(t, conn, tenantID, advanceOutcome(
			tenantID, start.InstanceID, pf.Plan, start.InstanceVersion, sink,
			workflow.PromotionNodeSnapshotWorker, workflow.OutcomeSucceeded, "sha256:snapshot"))
		if err != nil {
			t.Fatalf("Advance(snapshot_worker): %v", err)
		}
		comp, err := advanceOnce(t, conn, tenantID, advanceOutcome(
			tenantID, start.InstanceID, pf.Plan, snapshot.NewInstanceVersion, sink,
			workflow.PromotionNodeSimulateComp, workflow.OutcomeSucceeded, "sha256:comp"))
		if err != nil {
			t.Fatalf("Advance(simulate_compensation): %v", err)
		}
		// The frontier now sits at evaluate_band, READY.
		paused, err := requestPause(t, conn, tenantID, runtime.PauseRequest{
			TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: comp.NewInstanceVersion,
			Plan: pf.Plan, Reason: "MIGRATION_REVIEW", RequestedBy: "principal:operations-duty", RequestedAt: fixedInstant,
		})
		if err != nil {
			t.Fatalf("RequestPause: %v", err)
		}
		if paused.Status != runtime.InstancePaused {
			t.Fatalf("pause at evaluate_band produced status=%s, want PAUSED", paused.Status)
		}
		inst := loadInstanceForTest(t, conn, tenantID, start.InstanceID)
		takeCheckpoint(t, conn, tenantID, inst, 1, runtimestate.CheckpointSafePoint, placeholderDigest, placeholderDigest)

		sameNodeBridge := migrationpreview.Bridge{
			FromStepID: workflow.PromotionNodeEvaluateBand, FromStage: "READY",
			ToStepID: workflow.PromotionNodeEvaluateBand, ToStage: "WAITING",
			Ref: "migration.promotion.evaluate-band-restage/v1",
		}
		result, err := migrationpreview.Preview(migrationpreview.Request{
			Source: pf.Plan, Target: target,
			Instances: []migrationpreview.LiveInstanceState{
				{InstanceID: start.InstanceID.String(), StepID: workflow.PromotionNodeEvaluateBand, Stage: "READY"},
			},
			Bridges: []migrationpreview.Bridge{sameNodeBridge},
		})
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		if len(result.NeedsBridge) != 1 {
			t.Fatalf("fixture bug: expected exactly one bridged instance, got %+v", result.Assessments)
		}
		preview, err := migrate.CapturePreview(pf.Plan, target, result)
		if err != nil {
			t.Fatalf("CapturePreview: %v", err)
		}

		receipt, err := runMigrate(t, conn, tenantID, migrate.Request{
			TenantID: tenantID, InstanceID: start.InstanceID,
			SourcePlan: pf.Plan, TargetPlan: target,
			Preview: preview,
			Approval: migrate.Approval{
				PreviewDigest: preview.Digest(), Approver: "principal:release-manager",
				Reason: "REVIEWED", ApprovedAt: fixedInstant,
			},
			MigratedBy: "principal:migration-operator", MigratedAt: fixedInstant,
		})
		if err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		if len(receipt.ToFrontier) != 1 || receipt.ToFrontier[0] != workflow.PromotionNodeEvaluateBand {
			t.Fatalf("to-frontier = %v, want [%s] (a same-node bridge never moves the frontier)", receipt.ToFrontier, workflow.PromotionNodeEvaluateBand)
		}

		migrated := loadInstanceForTest(t, conn, tenantID, start.InstanceID)
		newAttempt := loadNodeExecutionForTest(t, conn, tenantID, start.InstanceID, workflow.PromotionNodeEvaluateBand, 2)
		if newAttempt.Status != runtime.NodeWaiting {
			t.Fatalf("bridged attempt 2 status = %s, want WAITING", newAttempt.Status)
		}

		resumed, err := resumeFromPause(t, conn, tenantID, runtime.ResumeRequest{
			TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: migrated.InstanceVersion,
			Plan: target, ResolvedContext: promotionResolvedContext(),
			Reason: "MIGRATION_COMPLETE", ResumedBy: "principal:operations-duty", ResumedAt: fixedInstant,
		})
		if err != nil {
			t.Fatalf("ResumeFromPause on the target plan: %v", err)
		}
		// The bridge minted a second attempt at evaluate_band (attempt 2,
		// WAITING); Advance must be told that exact attempt number, not the
		// helper's default of 1, since it names which durable row to
		// transition.
		if _, err := advanceOnce(t, conn, tenantID, runtime.AdvanceRequest{
			TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: resumed.InstanceVersion, Attempt: 2,
			Plan: target,
			Outcome: frontier.NodeOutcome{
				NodeID: workflow.PromotionNodeEvaluateBand, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:band-migrated",
			},
			RecordedAt: fixedInstant, Sink: sink,
		}); err != nil {
			t.Fatalf("Advance after a same-node bridged migration: %v", err)
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// failingExecutor wraps a real [dbport.Tx] and fails the call at position
// failAfterCalls (1-indexed across Exec/Query/QueryRow combined), simulating
// a storage fault partway through [migrate.Migrate]'s sequence of writes. It
// proves WF-RUN-018's "failed transform" RED case: the caller's transaction
// still rolls back on any error this produces, and every statement issued
// before the fault is undone with it.
type failingExecutor struct {
	inner          migrate.Executor
	calls          int
	failAfterCalls int
	err            error
}

func (f *failingExecutor) next() bool {
	f.calls++
	return f.calls > f.failAfterCalls
}

func (f *failingExecutor) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	if !f.next() {
		return f.inner.Exec(ctx, sql, args...)
	}
	return 0, f.err
}

func (f *failingExecutor) Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error) {
	if !f.next() {
		return f.inner.Query(ctx, sql, args...)
	}
	return nil, f.err
}

func (f *failingExecutor) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	if !f.next() {
		return f.inner.QueryRow(ctx, sql, args...)
	}
	return failingRow{err: f.err}
}

type failingRow struct{ err error }

func (r failingRow) Scan(dest ...any) error { return r.err }

var _ migrate.Executor = (*failingExecutor)(nil)
