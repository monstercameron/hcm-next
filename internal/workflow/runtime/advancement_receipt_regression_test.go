package runtime_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// TestAdvanceRejectsAnOldReceiptAfterLaterProgress guards the distinction
// between replaying the most recently committed command and replaying an old
// node outcome merely because its execution row still matches. The latter
// would let obsolete callers observe false success after the workflow moved on.
func TestAdvanceRejectsAnOldReceiptAfterLaterProgress(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "advance-old-receipt")
	pf := newPromotionFixture(t, values.TenantId("advance-old-receipt-tenant"), "intent:advance-old-receipt")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-old-receipt"))
	sink := runtime.NewMemorySink()

	firstReq := runtime.AdvanceRequest{
		TenantID: tenantID, InstanceID: start.InstanceID,
		ExpectedInstanceVersion: start.InstanceVersion, Attempt: 1,
		Plan: pf.Plan,
		Outcome: frontier.NodeOutcome{
			NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded,
			OutputDigest: "sha256:old-receipt-snapshot",
		},
		RecordedAt: fixedInstant, Sink: sink,
	}
	first, err := advanceOnce(t, conn, tenantID, firstReq)
	if err != nil {
		t.Fatalf("first Advance: %v", err)
	}
	secondReq := runtime.AdvanceRequest{
		TenantID: tenantID, InstanceID: start.InstanceID,
		ExpectedInstanceVersion: first.NewInstanceVersion, Attempt: 1,
		Plan: pf.Plan,
		Outcome: frontier.NodeOutcome{
			NodeID: workflow.PromotionNodeSimulateComp, Outcome: workflow.OutcomeSucceeded,
			OutputDigest: "sha256:old-receipt-compensation",
		},
		RecordedAt: fixedInstant, Sink: sink,
	}
	second, err := advanceOnce(t, conn, tenantID, secondReq)
	if err != nil {
		t.Fatalf("second Advance: %v", err)
	}

	if _, err := advanceOnce(t, conn, tenantID, firstReq); runtime.CodeOf(err) != runtime.CodeStaleInstance {
		t.Fatalf("old request after later progress: code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeStaleInstance, err)
	}

	var storedVersion int64
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(t.Context(), `
			SELECT instance_version FROM workflow_instance
			WHERE tenant_id = $1 AND instance_id = $2`, tenantID, start.InstanceID).Scan(&storedVersion)
	})
	if storedVersion != second.NewInstanceVersion {
		t.Fatalf("old replay moved instance version to %d, want %d", storedVersion, second.NewInstanceVersion)
	}
}
