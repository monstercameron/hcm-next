package runtimestate_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestTodo_DB_012_Integration is the restart test: state committed across
// all four tables on one connection is fully reloadable, purely from durable
// rows, on a brand new connection opened afterward -- and an advancement can
// continue from that reloaded state alone, with nothing carried over from
// the connection that committed it.
func TestTodo_DB_012_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "db012-integration")
	plan := referencePlan(t)
	setupConn := appConn(t, db)
	inst := createInstance(t, ctx, setupConn, tenant, plan)

	// Commit a work item and an idempotency record tied to this instance,
	// alongside the runtime state, so the restart proves every table -- not
	// just the runtime one -- survives and reloads.
	item := availableItem(t, ctx, workitem.Store{}, setupConn, tenant, inst.InstanceID, "approval_node", "principal:reviewer-1")

	idemScope := idempotency.Scope{Tenant: tenant, Capability: "db012.integration.v1", EffectScope: "instance:" + inst.InstanceID.String(), Key: "attempt-1"}
	var idemBefore idempotency.Record
	inTenantTx(t, setupConn, tenant, func(tx dbport.Tx) error {
		var err error
		idemBefore, err = idempotency.Guard(ctx, tx, idempotency.PostgresStore{}, idemScope, digestOf("integration-1"), defaultPolicy, fixedInstant,
			func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
				return idempotency.ResultIdentity{ResultRef: "ref-integration-1"}, nil
			})
		return err
	})

	// The durable ContinuationStore, not MemorySink: this test proves the
	// continuation row itself survives a restart, and MemorySink never
	// touches storage at all.
	sink := runtime.ContinuationStore{}
	first, err := advanceOn(t, setupConn, tenant, runtime.AdvanceRequest{
		TenantID: tenant, InstanceID: inst.InstanceID, ExpectedInstanceVersion: inst.InstanceVersion, Attempt: 1,
		Plan: plan,
		Outcome: frontier.NodeOutcome{
			NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:" + repeatHex("8"),
		},
		RecordedAt: fixedInstant, Sink: sink,
	})
	if err != nil {
		t.Fatalf("pre-restart Advance: %v", err)
	}

	// The restart: a brand new connection, a brand new session, nothing in
	// memory from the writes above. db.NewConn (via appConn) opens an
	// independent TCP session against the same schema, which is what a
	// process restart looks like from the database's point of view.
	restarted := appConn(t, db)

	var reloadedInst runtime.Instance
	var reloadedNodes []runtime.NodeExecution
	var reloadedItem workitem.WorkItem
	var reloadedIdem idempotency.Record
	var idemFound bool
	var continuationCount int
	inTenantTx(t, restarted, tenant, func(tx dbport.Tx) error {
		var loadErr error
		reloadedInst, loadErr = (runtime.Store{}).LoadInstance(ctx, tx, tenant, inst.InstanceID)
		if loadErr != nil {
			return loadErr
		}
		reloadedNodes, loadErr = (runtime.Store{}).LoadNodeExecutions(ctx, tx, tenant, inst.InstanceID)
		if loadErr != nil {
			return loadErr
		}
		reloadedItem, loadErr = (workitem.Store{}).Load(ctx, tx, tenant, item.WorkItemID)
		if loadErr != nil {
			return loadErr
		}
		reloadedIdem, idemFound, loadErr = (idempotency.PostgresStore{}).Lookup(ctx, tx, idemScope)
		if loadErr != nil {
			return loadErr
		}
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM workflow_continuation WHERE tenant_id = $1 AND instance_id = $2`,
			tenant, inst.InstanceID).Scan(&continuationCount)
	})

	if reloadedInst.InstanceVersion != first.NewInstanceVersion {
		t.Fatalf("reloaded instance version = %d, want %d", reloadedInst.InstanceVersion, first.NewInstanceVersion)
	}
	if len(reloadedNodes) == 0 {
		t.Fatal("no node executions survived the restart")
	}
	if reloadedItem.WorkItemID != item.WorkItemID || reloadedItem.Status != item.Status {
		t.Fatalf("reloaded work item = %+v, want id=%s status=%s", reloadedItem, item.WorkItemID, item.Status)
	}
	if !idemFound || reloadedIdem.Identity.ResultRef != idemBefore.Identity.ResultRef {
		t.Fatalf("reloaded idempotency record found=%v identity=%+v, want the pre-restart identity %+v", idemFound, reloadedIdem.Identity, idemBefore.Identity)
	}
	if continuationCount == 0 {
		t.Fatal("no continuation rows survived the restart")
	}

	// Continue the same workflow's advancement using ONLY the reloaded
	// instance version: nothing cached from the pre-restart connection.
	second, err := advanceOn(t, restarted, tenant, runtime.AdvanceRequest{
		TenantID: tenant, InstanceID: inst.InstanceID, ExpectedInstanceVersion: reloadedInst.InstanceVersion, Attempt: 1,
		Plan: plan,
		Outcome: frontier.NodeOutcome{
			NodeID: workflow.PromotionNodeSimulateComp, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:" + repeatHex("9"),
		},
		RecordedAt: fixedInstant, Sink: sink,
	})
	if err != nil {
		t.Fatalf("post-restart Advance (continued from durable state alone): %v", err)
	}
	if second.NewInstanceVersion <= reloadedInst.InstanceVersion {
		t.Fatalf("post-restart advancement did not move the instance forward: %d -> %d", reloadedInst.InstanceVersion, second.NewInstanceVersion)
	}
}
