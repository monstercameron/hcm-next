package runtimestate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// repoRoot walks up from the current working directory until it finds
// go.mod, so the schema-absence test can read
// definitions/runtime/durable-runtime-decision.yaml without depending on
// tools/policy/internal/repopath, which Go's own internal-package visibility
// rule keeps out of reach of a package outside tools/policy.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (go.mod) starting from %s", dir)
		}
		dir = parent
	}
}

// gateRecord is the handful of fields this suite needs from
// definitions/runtime/durable-runtime-decision.yaml.
type gateRecord struct {
	Status              string `yaml:"status"`
	ReevaluationTrigger struct {
		GatingTodo string `yaml:"gating_todo"`
		Blocks     string `yaml:"blocks"`
	} `yaml:"reevaluation_trigger"`
}

func loadGateRecord(t *testing.T) gateRecord {
	t.Helper()
	path := filepath.Join(repoRoot(t), "definitions", "runtime", "durable-runtime-decision.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var rec gateRecord
	if err := yaml.Unmarshal(data, &rec); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return rec
}

// TestTodo_DB_012 is the PRIMARY case: over one pgtest database, it exercises
// CAS on workflow_instance, dedupe on workflow_node_execution, work item
// claim exclusivity, idempotency reserve/complete, continuation and
// advancement-receipt exactly-once, and atomic advancement -- both the
// single-store rollback WF-RUN-025 already proves and a genuine cross-store
// composition spanning all three packages in one caller transaction.
func TestTodo_DB_012(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	t.Run("schema: exactly the seven DB-012 tables exist, none of the WF-RUN-000-gated ones do, and the gate record still blocks them", func(t *testing.T) {
		conn := appConn(t, db)
		var inv runtimestate.SchemaInventory
		inTx(t, conn, func(tx dbport.Tx) error {
			var err error
			inv, err = runtimestate.Inspect(ctx, tx)
			return err
		})
		if !inv.Exact() {
			t.Fatalf("schema inventory not exact: missing=%v unexpected=%v (present=%v)", inv.Missing, inv.Unexpected, inv.Present)
		}

		gate := loadGateRecord(t)
		if gate.Status != "DECIDED" {
			t.Errorf("gate record status = %q, want DECIDED", gate.Status)
		}
		if gate.ReevaluationTrigger.GatingTodo != "WF-RUN-000" {
			t.Errorf("gate record gating_todo = %q, want WF-RUN-000", gate.ReevaluationTrigger.GatingTodo)
		}
		if !strings.Contains(gate.ReevaluationTrigger.Blocks, "scheduler/timer/lease") {
			t.Errorf("gate record blocks clause = %q, no longer names scheduler/timer/lease code as blocked", gate.ReevaluationTrigger.Blocks)
		}
	})

	t.Run("CAS on workflow_instance: a stale expected version commits nothing, and the correct one advances it", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-cas")
		plan := referencePlan(t)
		conn := appConn(t, db)
		inst := createInstance(t, ctx, conn, tenant, plan)

		staleErr := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := (runtime.Store{}).RecordInstanceState(ctx, tx, runtime.InstanceTransition{
				TenantID: tenant, InstanceID: inst.InstanceID, ExpectedVersion: inst.InstanceVersion - 1,
				Status: runtime.InstanceRunning, CurrentNodeIDs: inst.CurrentNodeIDs, StartedAt: timePtr(fixedInstant),
			})
			return err
		})
		if runtime.CodeOf(staleErr) != runtime.CodeStaleInstance {
			t.Fatalf("stale CAS refusal code = %q, want %q (%v)", runtime.CodeOf(staleErr), runtime.CodeStaleInstance, staleErr)
		}
		var afterStale runtime.Instance
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			afterStale, err = (runtime.Store{}).LoadInstance(ctx, tx, tenant, inst.InstanceID)
			return err
		})
		if afterStale.InstanceVersion != inst.InstanceVersion || afterStale.RuntimeStatus != inst.RuntimeStatus {
			t.Fatalf("a refused CAS mutated the instance: version %d -> %d, status %s -> %s",
				inst.InstanceVersion, afterStale.InstanceVersion, inst.RuntimeStatus, afterStale.RuntimeStatus)
		}

		var updated runtime.Instance
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			updated, err = (runtime.Store{}).RecordInstanceState(ctx, tx, runtime.InstanceTransition{
				TenantID: tenant, InstanceID: inst.InstanceID, ExpectedVersion: inst.InstanceVersion,
				Status: runtime.InstanceRunning, CurrentNodeIDs: inst.CurrentNodeIDs, StartedAt: timePtr(fixedInstant),
			})
			return err
		})
		if updated.InstanceVersion != inst.InstanceVersion+1 || updated.RuntimeStatus != runtime.InstanceRunning {
			t.Fatalf("a correct CAS did not advance the instance: version=%d status=%s", updated.InstanceVersion, updated.RuntimeStatus)
		}
	})

	t.Run("dedupe on workflow_node_execution: a derived id collides on a repeated attempt record, and the attempted rewrite mutates nothing", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-dedupe")
		plan := referencePlan(t)
		conn := appConn(t, db)
		inst := createInstance(t, ctx, conn, tenant, plan)

		var before runtime.Instance
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			before, err = (runtime.Store{}).LoadInstance(ctx, tx, tenant, inst.InstanceID)
			return err
		})

		dup := runtime.NewNodeExecution(tenant, inst.InstanceID, plan.StartNodeID, 1, workflow.StepCapability, runtime.NodeReady)
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, _, txErr := (runtime.Store{}).RecordNodeExecution(ctx, tx, dup, before.InstanceVersion)
			return txErr
		})
		if err == nil {
			t.Fatal("the same node attempt was recorded twice under two rows")
		}
		if runtime.CodeOf(err) != runtime.CodeStorageFailed {
			t.Fatalf("duplicate-attempt refusal code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeStorageFailed, err)
		}

		var after runtime.Instance
		var executions []runtime.NodeExecution
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var loadErr error
			after, loadErr = (runtime.Store{}).LoadInstance(ctx, tx, tenant, inst.InstanceID)
			if loadErr != nil {
				return loadErr
			}
			executions, loadErr = (runtime.Store{}).LoadNodeExecutions(ctx, tx, tenant, inst.InstanceID)
			return loadErr
		})
		if after.InstanceVersion != before.InstanceVersion {
			t.Errorf("instance version moved from %d to %d despite the duplicate attempt being refused",
				before.InstanceVersion, after.InstanceVersion)
		}
		count := 0
		for _, e := range executions {
			if e.NodeID == plan.StartNodeID {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("node executions recorded for %s = %d, want exactly 1", plan.StartNodeID, count)
		}
	})

	t.Run("work item claim exclusivity: a second claim on a live claim is refused and mutates nothing", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-claim")
		plan := referencePlan(t)
		conn := appConn(t, db)
		inst := createInstance(t, ctx, conn, tenant, plan)
		store := workitem.Store{}

		item := availableItem(t, ctx, store, conn, tenant, inst.InstanceID, "approval_node", "principal:owner-1")
		var claimed workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			claimed, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:owner-1", ClaimExpiresAt: fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: workMeta("workitem.claimed"),
			})
			return err
		})
		if claimed.Status != workitem.StatusClaimed {
			t.Fatalf("claim status = %s, want CLAIMED", claimed.Status)
		}

		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: claimed.ItemVersion,
				ClaimantPrincipalID: "principal:owner-2", ClaimExpiresAt: fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: workMeta("workitem.claimed"),
			})
			return txErr
		})
		if workitem.CodeOf(err) != workitem.CodeAlreadyClaimed {
			t.Fatalf("second claim refusal code = %q, want %q (%v)", workitem.CodeOf(err), workitem.CodeAlreadyClaimed, err)
		}
		var unchanged workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			unchanged, err = store.Load(ctx, tx, tenant, item.WorkItemID)
			return err
		})
		if unchanged.ClaimedBy != "principal:owner-1" || unchanged.ItemVersion != claimed.ItemVersion {
			t.Fatalf("a refused second claim changed the item to claimed_by=%s version=%d", unchanged.ClaimedBy, unchanged.ItemVersion)
		}
	})

	t.Run("idempotency_record reserve/complete: a fresh reservation runs the effect once, a same-digest replay does not rerun it, a different digest is a conflict that mutates nothing", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-idem")
		conn := appConn(t, db)
		scope := idempotency.Scope{Tenant: tenant, Capability: "db012.review.approve/v1", EffectScope: "worker:employment_change", Key: "key-1"}
		digestA := digestOf("payload-a")
		digestB := digestOf("payload-b")

		ran := 0
		var first idempotency.Record
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			first, err = idempotency.Guard(ctx, tx, idempotency.PostgresStore{}, scope, digestA, defaultPolicy, fixedInstant,
				func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
					ran++
					return idempotency.ResultIdentity{ResultRef: "ref-1"}, nil
				})
			return err
		})
		if ran != 1 {
			t.Fatalf("effect ran %d times on a fresh reservation, want 1", ran)
		}
		if first.Status != idempotency.StatusCompleted || first.Identity.ResultRef != "ref-1" {
			t.Fatalf("first Guard result = %+v, want COMPLETED with ref-1", first)
		}

		var replay idempotency.Record
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			replay, err = idempotency.Guard(ctx, tx, idempotency.PostgresStore{}, scope, digestA, defaultPolicy, fixedInstant,
				func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
					ran++
					return idempotency.ResultIdentity{ResultRef: "ref-should-not-appear"}, nil
				})
			return err
		})
		if ran != 1 {
			t.Fatalf("effect ran again on a same-digest replay: ran=%d, want still 1", ran)
		}
		if replay.Identity.ResultRef != "ref-1" {
			t.Fatalf("replay identity = %q, want the original ref-1", replay.Identity.ResultRef)
		}

		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, guardErr := idempotency.Guard(ctx, tx, idempotency.PostgresStore{}, scope, digestB, defaultPolicy, fixedInstant,
				func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
					ran++
					return idempotency.ResultIdentity{ResultRef: "ref-conflict"}, nil
				})
			return guardErr
		})
		if idempotency.CodeOf(err) != idempotency.CodeConflict {
			t.Fatalf("different-digest refusal code = %q, want %q (%v)", idempotency.CodeOf(err), idempotency.CodeConflict, err)
		}
		if ran != 1 {
			t.Fatalf("effect ran on a conflicting digest: ran=%d, want still 1", ran)
		}
		var stillA idempotency.Record
		var found bool
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			stillA, found, err = idempotency.PostgresStore{}.Lookup(ctx, tx, scope)
			return err
		})
		if !found || stillA.RequestDigest != digestA || stillA.Identity.ResultRef != "ref-1" {
			t.Fatalf("a refused conflicting digest mutated the stored record: found=%v record=%+v", found, stillA)
		}
	})

	t.Run("continuation exactly-once: a repeated durable write of the same derived identity is a no-op, not a duplicate", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-continuation")
		plan := referencePlan(t)
		conn := appConn(t, db)
		inst := createInstance(t, ctx, conn, tenant, plan)

		store := runtime.ContinuationStore{}
		rec := runtime.ContinuationRecord{
			TenantID: tenant, InstanceID: inst.InstanceID,
			SourceNodeID: plan.StartNodeID, SourceAttempt: 1,
			TargetNodeID: plan.StartNodeID, Kind: frontier.IntentReady,
			RecordedAt: fixedInstant,
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			if err := store.MarkReady(ctx, tx, rec); err != nil {
				return err
			}
			return store.MarkReady(ctx, tx, rec)
		})
		var count int
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM workflow_continuation WHERE tenant_id = $1 AND instance_id = $2 AND target_node_id = $3 AND kind = $4`,
				tenant, inst.InstanceID, plan.StartNodeID, string(frontier.IntentReady)).Scan(&count)
		})
		if count != 1 {
			t.Fatalf("continuation rows for one identity = %d, want exactly 1", count)
		}
	})

	t.Run("workflow_advancement_receipt: exactly one receipt row exists per committed advancement command identity", func(t *testing.T) {
		// internal/workflow/runtime/advancement_receipt.go's own
		// recordAdvancementReceipt/loadAdvancementReceipt are not called from
		// Advance in the current package (verified by inspection: advance.go
		// never references either function), so this proves the table's own
		// schema contract directly rather than through runtime.Advance. See
		// doc.go's "finding this suite surfaces" section.
		tenant := insertTenant(t, db, "db012-receipt")
		plan := referencePlan(t)
		conn := appConn(t, db)
		inst := createInstance(t, ctx, conn, tenant, plan)

		insertReceipt := func(tx dbport.Tx, expected, resulting int64) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO workflow_advancement_receipt (
					tenant_id, instance_id, node_id, attempt, expected_instance_version,
					request_digest, resulting_instance_version, receipt)
				VALUES ($1, $2, $3, 1, $4, $5, $6, '{}'::jsonb)`,
				tenant, inst.InstanceID, plan.StartNodeID, expected, repeatHex("6"), resulting)
			return err
		}

		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return insertReceipt(tx, inst.InstanceVersion, inst.InstanceVersion+1)
		}); err != nil {
			t.Fatalf("first advancement receipt insert: %v", err)
		}

		dupErr := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return insertReceipt(tx, inst.InstanceVersion, inst.InstanceVersion+1)
		})
		if dupErr == nil {
			t.Fatal("a second receipt for the same command identity was not refused")
		}

		nonAdvancingErr := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return insertReceipt(tx, inst.InstanceVersion+10, inst.InstanceVersion+10)
		})
		if nonAdvancingErr == nil {
			t.Fatal("a receipt whose resulting version does not exceed its expected version was not refused")
		}

		var count int
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM workflow_advancement_receipt WHERE tenant_id = $1 AND instance_id = $2`,
				tenant, inst.InstanceID).Scan(&count)
		})
		if count != 1 {
			t.Fatalf("advancement receipt rows for this instance = %d, want exactly 1", count)
		}
	})

	t.Run("atomic advancement: a failing continuation sink rolls the whole advancement back", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-atomic")
		plan := referencePlan(t)
		conn := appConn(t, db)
		inst := createInstance(t, ctx, conn, tenant, plan)

		_, err := advanceOn(t, conn, tenant, runtime.AdvanceRequest{
			TenantID: tenant, InstanceID: inst.InstanceID, ExpectedInstanceVersion: inst.InstanceVersion, Attempt: 1,
			Plan: plan,
			Outcome: frontier.NodeOutcome{
				NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:" + repeatHex("7"),
			},
			RecordedAt: fixedInstant,
			// The successor (simulate_compensation) activates as READY,
			// raising IntentReady -- the failpoint WF-RUN-025's own Fault
			// case injects: after the completing node's own transition is
			// already written inside this same transaction.
			Sink: failingSink{failOn: frontier.IntentReady},
		})
		if err == nil {
			t.Fatal("expected the injected continuation failure to surface")
		}

		var after runtime.Instance
		var executions []runtime.NodeExecution
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var loadErr error
			after, loadErr = (runtime.Store{}).LoadInstance(ctx, tx, tenant, inst.InstanceID)
			if loadErr != nil {
				return loadErr
			}
			executions, loadErr = (runtime.Store{}).LoadNodeExecutions(ctx, tx, tenant, inst.InstanceID)
			return loadErr
		})
		if after.InstanceVersion != inst.InstanceVersion {
			t.Errorf("instance version moved from %d to %d despite the injected failure", inst.InstanceVersion, after.InstanceVersion)
		}
		for _, e := range executions {
			if e.NodeID == workflow.PromotionNodeSnapshotWorker && e.Status != runtime.NodeReady {
				t.Errorf("%s status = %s, want READY (the completing node's own transition was not rolled back)", e.NodeID, e.Status)
			}
			if e.NodeID == workflow.PromotionNodeSimulateComp {
				t.Errorf("successor %s was recorded despite the injected failure", e.NodeID)
			}
		}
	})

	t.Run("cross-store atomic composition: runtime, work-item and idempotency writes in one caller transaction commit or roll back together", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-cross-store")
		plan := referencePlan(t)
		conn := appConn(t, db)
		inst := createInstance(t, ctx, conn, tenant, plan)

		scope := idempotency.Scope{Tenant: tenant, Capability: "db012.cross_store.v1", EffectScope: "instance:" + inst.InstanceID.String(), Key: "attempt-1"}
		neverReserved := idempotency.Scope{Tenant: tenant, Capability: scope.Capability, EffectScope: scope.EffectScope, Key: "never-reserved"}

		var itemID uuid.UUID
		txErr := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			// (a) runtime store: move the instance to RUNNING.
			if _, err := (runtime.Store{}).RecordInstanceState(ctx, tx, runtime.InstanceTransition{
				TenantID: tenant, InstanceID: inst.InstanceID, ExpectedVersion: inst.InstanceVersion,
				Status: runtime.InstanceRunning, CurrentNodeIDs: inst.CurrentNodeIDs, StartedAt: timePtr(fixedInstant),
			}); err != nil {
				return err
			}

			// (b) human-work store: create a real work item against the same
			// instance, inside the same transaction.
			item, err := workitem.NewWorkItem(taskInputFor(tenant, inst.InstanceID, "approval_node"))
			if err != nil {
				return err
			}
			itemID = item.WorkItemID
			if _, err := (workitem.Store{}).Create(ctx, tx, item, workMeta(workitem.ReasonCreated)); err != nil {
				return err
			}

			// (c) idempotency store: reserve a scope, then force a refusal by
			// completing a DIFFERENT scope that was never reserved -- the
			// failure surfaces from inside the same transaction, after (a)
			// and (b) already ran their statements against it.
			if _, _, err := (idempotency.PostgresStore{}).Reserve(ctx, tx, scope, digestOf("cross-store-1"), defaultPolicy, fixedInstant); err != nil {
				return err
			}
			_, err = (idempotency.PostgresStore{}).Complete(ctx, tx, neverReserved, idempotency.ResultIdentity{ResultRef: "x"}, fixedInstant)
			return err
		})
		if idempotency.CodeOf(txErr) != idempotency.CodeNotReserved {
			t.Fatalf("composed-transaction refusal code = %q, want %q (%v)", idempotency.CodeOf(txErr), idempotency.CodeNotReserved, txErr)
		}

		// Nothing from (a), (b) or (c) may be durably visible.
		var afterInst runtime.Instance
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			afterInst, err = (runtime.Store{}).LoadInstance(ctx, tx, tenant, inst.InstanceID)
			return err
		})
		if afterInst.RuntimeStatus != inst.RuntimeStatus || afterInst.InstanceVersion != inst.InstanceVersion {
			t.Errorf("runtime store retained a write from the rolled-back transaction: status=%s version=%d, want %s/%d",
				afterInst.RuntimeStatus, afterInst.InstanceVersion, inst.RuntimeStatus, inst.InstanceVersion)
		}
		itemErr := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := (workitem.Store{}).Load(ctx, tx, tenant, itemID)
			return err
		})
		if workitem.CodeOf(itemErr) != workitem.CodeWorkItemNotFound {
			t.Errorf("human-work store retained a work item from the rolled-back transaction (load error = %v)", itemErr)
		}
		var scopeFound bool
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			_, scopeFound, err = (idempotency.PostgresStore{}).Lookup(ctx, tx, scope)
			return err
		})
		if scopeFound {
			t.Error("idempotency store retained a reservation from the rolled-back transaction")
		}
	})
}
