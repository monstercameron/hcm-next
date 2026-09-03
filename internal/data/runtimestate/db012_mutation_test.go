package runtimestate_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// TestTodo_DB_012_Mutation asserts that an invalid state transition, a
// mutable completed work result, a duplicate approval slot and a raw UPDATE
// against a completed output are all refused by the schema or the stores.
func TestTodo_DB_012_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	t.Run("an invalid workflow instance state transition is refused and mutates nothing", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-mut-instance")
		plan := referencePlan(t)
		conn := appConn(t, db)
		inst := createInstance(t, ctx, conn, tenant, plan)

		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := (runtime.Store{}).RecordInstanceState(ctx, tx, runtime.InstanceTransition{
				TenantID: tenant, InstanceID: inst.InstanceID, ExpectedVersion: inst.InstanceVersion,
				Status: runtime.InstanceCompleted, CurrentNodeIDs: nil, CompletedAt: timePtr(fixedInstant),
			})
			return err
		})
		if runtime.CodeOf(err) != runtime.CodeIllegalTransition {
			t.Fatalf("refusal code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeIllegalTransition, err)
		}
		var after runtime.Instance
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var loadErr error
			after, loadErr = (runtime.Store{}).LoadInstance(ctx, tx, tenant, inst.InstanceID)
			return loadErr
		})
		if after.InstanceVersion != inst.InstanceVersion || after.RuntimeStatus != inst.RuntimeStatus {
			t.Fatalf("a refused illegal transition mutated the instance: version %d -> %d, status %s -> %s",
				inst.InstanceVersion, after.InstanceVersion, inst.RuntimeStatus, after.RuntimeStatus)
		}
	})

	t.Run("an invalid work item state transition (claiming an unrouted item) is refused", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-mut-workitem-transition")
		plan := referencePlan(t)
		conn := appConn(t, db)
		inst := createInstance(t, ctx, conn, tenant, plan)
		store := workitem.Store{}

		in, err := workitem.NewWorkItem(taskInputFor(tenant, inst.InstanceID, "approval_node"))
		if err != nil {
			t.Fatalf("NewWorkItem: %v", err)
		}
		var item workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var createErr error
			item, createErr = store.Create(ctx, tx, in, workMeta(workitem.ReasonCreated))
			return createErr
		})

		claimErr := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:claim-before-route", ClaimExpiresAt: fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: workMeta("workitem.claimed"),
			})
			return txErr
		})
		if workitem.CodeOf(claimErr) != workitem.CodeIllegalTransition {
			t.Fatalf("claim-before-route refusal code = %q, want %q (%v)", workitem.CodeOf(claimErr), workitem.CodeIllegalTransition, claimErr)
		}
		var unchanged workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var loadErr error
			unchanged, loadErr = store.Load(ctx, tx, tenant, item.WorkItemID)
			return loadErr
		})
		if unchanged.Status != workitem.StatusCreated || unchanged.ItemVersion != item.ItemVersion {
			t.Fatalf("a refused claim mutated the item: status=%s version=%d", unchanged.Status, unchanged.ItemVersion)
		}
	})

	t.Run("a completed work item's output is immutable, both at the Go store layer and against a raw UPDATE", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-mut-immutable-output")
		plan := referencePlan(t)
		conn := appConn(t, db)
		inst := createInstance(t, ctx, conn, tenant, plan)
		store := workitem.Store{}

		item := availableItem(t, ctx, store, conn, tenant, inst.InstanceID, "approval_node", "principal:completer-1")
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var claimErr error
			item, claimErr = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:completer-1", ClaimExpiresAt: fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: workMeta("workitem.claimed"),
			})
			return claimErr
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var startErr error
			item, startErr = store.Start(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, fixedInstant, workMeta("workitem.started"))
			return startErr
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var completeErr error
			item, completeErr = store.Complete(ctx, tx, workitem.CompleteInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				CompletedBy: "principal:completer-1", CompletedOutputDigest: "sha256:" + repeatHex("a"), Now: fixedInstant,
				Meta: workMeta("workitem.completed"),
			})
			return completeErr
		})
		if item.Status != workitem.StatusCompleted {
			t.Fatalf("fixture item status = %s, want COMPLETED", item.Status)
		}

		// (a) a second Complete call at the Go store layer is refused: a
		// completed item has no outgoing transition, so a second completion
		// -- even with the correct current version -- cannot produce a
		// different stored output.
		secondErr := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.Complete(ctx, tx, workitem.CompleteInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				CompletedBy: "principal:completer-2", CompletedOutputDigest: "sha256:" + repeatHex("b"), Now: fixedInstant,
				Meta: workMeta("workitem.completed"),
			})
			return txErr
		})
		if workitem.CodeOf(secondErr) != workitem.CodeIllegalTransition {
			t.Fatalf("second Complete refusal code = %q, want %q (%v)", workitem.CodeOf(secondErr), workitem.CodeIllegalTransition, secondErr)
		}

		// (b) a raw UPDATE bypassing the Go layer entirely is refused by
		// migration 00017's work_item_forbid_rewrite trigger, which fires
		// regardless of caller.
		rawErr := db.ExecErr(`UPDATE work_item SET completed_output_digest = $1 WHERE tenant_id = $2 AND work_item_id = $3`,
			"sha256:"+repeatHex("c"), tenant, item.WorkItemID)
		if rawErr == nil {
			t.Fatal("a raw UPDATE against a completed output digest was not refused")
		}

		var unchanged workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var loadErr error
			unchanged, loadErr = store.Load(ctx, tx, tenant, item.WorkItemID)
			return loadErr
		})
		if unchanged.CompletedOutputDigest != item.CompletedOutputDigest {
			t.Fatalf("completed output digest changed from %q to %q", item.CompletedOutputDigest, unchanged.CompletedOutputDigest)
		}
	})

	t.Run("duplicate approval slot: dispatching the same WORK_ITEM_REQUIRED intent twice creates exactly one work item", func(t *testing.T) {
		tenant := insertTenant(t, db, "db012-mut-dup-approval")
		plan := referencePlan(t)
		conn := appConn(t, db)
		inst := createInstance(t, ctx, conn, tenant, plan)

		sink := crossStoreSink{items: workitem.Store{}, at: fixedInstant}
		rec := runtime.ContinuationRecord{
			TenantID: tenant, InstanceID: inst.InstanceID,
			SourceNodeID: plan.StartNodeID, SourceAttempt: 1,
			TargetNodeID: "approval_node", Kind: frontier.IntentWorkItemRequired,
			RecordedAt: fixedInstant,
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return sink.RequireWorkItem(ctx, tx, rec)
		})
		// The same intent dispatched a second time -- the shape a retried
		// Advance call or a re-delivered message could produce.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return sink.RequireWorkItem(ctx, tx, rec)
		})

		var itemCount, contCount int
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			if err := tx.QueryRow(ctx,
				`SELECT count(*) FROM work_item WHERE tenant_id = $1 AND workflow_instance_id = $2 AND node_id = $3`,
				tenant, inst.InstanceID, "approval_node").Scan(&itemCount); err != nil {
				return err
			}
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM workflow_continuation WHERE tenant_id = $1 AND instance_id = $2 AND target_node_id = $3 AND kind = $4`,
				tenant, inst.InstanceID, "approval_node", string(frontier.IntentWorkItemRequired)).Scan(&contCount)
		})
		if itemCount != 1 {
			t.Fatalf("work items for one approval slot = %d, want exactly 1", itemCount)
		}
		if contCount != 1 {
			t.Fatalf("continuation rows for one approval slot = %d, want exactly 1", contCount)
		}
	})
}
