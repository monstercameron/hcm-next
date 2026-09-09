package runtime_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// --- WF-RUN-027 --------------------------------------------------------
//
// These tests share [newPromotionFixture]/[baseStartRequest] with
// wfrun023_test.go: a promotion-reference start binds one proposal revision
// through [runtime.ProposalFacts]/[runtime.ApprovalFacts] rather than a
// caller-asserted Approved/Superseded pair. [approvedProposalFacts] builds
// the "current, approved" shape every GREEN-path case starts from.

// recordingProposalFacts027 records the tenant id [runtime.Start] passes to
// [runtime.ProposalFacts.Supersession], so TestTodo_WF_RUN_027_Security can
// prove the port is called with the caller's own tenant, never a zero value
// or another tenant's.
type recordingProposalFacts027 struct {
	seenTenant uuid.UUID
}

func (f *recordingProposalFacts027) Supersession(
	_ context.Context, _ runtime.Executor, tenantID uuid.UUID, _ intent.ProposalRevision,
) (runtime.ProposalSupersessionFact, error) {
	f.seenTenant = tenantID
	return runtime.ProposalSupersessionFact{}, nil
}

// TestTodo_WF_RUN_027 is the PRIMARY test: Start resolves supersession and
// approval from [runtime.ProposalFacts]/[runtime.ApprovalFacts], never from a
// caller-asserted boolean, and records the approval decision ids it relied on
// in the start receipt.
func TestTodo_WF_RUN_027(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun027")
	pf := newPromotionFixture(t, values.TenantId("wfrun027-tenant"), "intent:wf-run-027-1")
	req := pf.baseStartRequest(tenantID, "start-key-wfrun027")

	var receipt runtime.StartReceipt
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		receipt, err = runtime.Start(context.Background(), tx, req)
		return err
	})
	if len(receipt.ApprovalDecisionIDs) != 1 || receipt.ApprovalDecisionIDs[0] != approvedDecisionID {
		t.Fatalf("receipt.ApprovalDecisionIDs = %v, want exactly [%s]", receipt.ApprovalDecisionIDs, approvedDecisionID)
	}
}

// TestTodo_WF_RUN_027_Golden pins the exact ApprovalDecisionIDs shape a fixed
// approved, non-superseded start produces.
func TestTodo_WF_RUN_027_Golden(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun027-golden")
	pf := newPromotionFixture(t, values.TenantId("wfrun027-golden-tenant"), "intent:wf-run-027-golden")
	req := pf.baseStartRequest(tenantID, "start-key-wfrun027-golden")

	var receipt runtime.StartReceipt
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		receipt, err = runtime.Start(context.Background(), tx, req)
		return err
	})
	want := []string{approvedDecisionID}
	if len(receipt.ApprovalDecisionIDs) != len(want) {
		t.Fatalf("ApprovalDecisionIDs = %v, want %v", receipt.ApprovalDecisionIDs, want)
	}
	for i, id := range want {
		if receipt.ApprovalDecisionIDs[i] != id {
			t.Fatalf("ApprovalDecisionIDs = %v, want %v", receipt.ApprovalDecisionIDs, want)
		}
	}
}

// TestTodo_WF_RUN_027_Fault proves a start refused on approval-binding
// mismatch leaves nothing behind: the caller's transaction rolls back and no
// instance exists under that key.
func TestTodo_WF_RUN_027_Fault(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun027-fault")
	pf := newPromotionFixture(t, values.TenantId("wfrun027-fault-tenant"), "intent:wf-run-027-fault")
	req := pf.baseStartRequest(tenantID, "start-key-wfrun027-fault")
	req.ApprovalFacts = runtime.MemoryApprovalFacts{ByRevisionID: map[string][]runtime.ApprovalDecisionFact{
		pf.Proposal.ProposalRevisionID: {{
			DecisionID: "decision:wrong-binding", Outcome: runtime.ApprovalOutcomeApproved,
			ProposalDigest: "sha256:0000000000000000000000000000000000000000000000000000000000ff",
		}},
	}}

	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		_, err := runtime.Start(context.Background(), tx, req)
		return err
	})
	if runtime.CodeOf(err) != runtime.CodeApprovalBindingMismatch {
		t.Fatalf("code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeApprovalBindingMismatch, err)
	}

	instanceID := deriveInstanceIDForTest(tenantID, pf.Plan.WorkflowID, "start-key-wfrun027-fault")
	_, loadErr := (runtime.Store{}).LoadInstance(context.Background(), conn, tenantID, instanceID)
	if runtime.CodeOf(loadErr) != runtime.CodeInstanceNotFound {
		t.Fatalf("a refused start left a row behind: LoadInstance code = %q, err = %v", runtime.CodeOf(loadErr), loadErr)
	}
}

// TestTodo_WF_RUN_027_Security proves ProposalFacts is always consulted with
// the caller's own tenant id, never a zero value or another tenant's --
// exactly the fact the runtime never re-derives on its own and must pass
// through faithfully.
func TestTodo_WF_RUN_027_Security(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun027-security")
	pf := newPromotionFixture(t, values.TenantId("wfrun027-security-tenant"), "intent:wf-run-027-security")
	req := pf.baseStartRequest(tenantID, "start-key-wfrun027-security")
	recorder := &recordingProposalFacts027{}
	req.ProposalFacts = recorder

	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		_, err := runtime.Start(context.Background(), tx, req)
		return err
	})
	if recorder.seenTenant != tenantID {
		t.Fatalf("ProposalFacts saw tenant %s, want %s", recorder.seenTenant, tenantID)
	}
}

// TestTodo_WF_RUN_027_Mutation proves the approval-binding-mismatch check
// does not short-circuit on the first correctly bound decision: a second
// decision bound to a different digest still refuses the start, even though
// the first alone would have satisfied approval.
func TestTodo_WF_RUN_027_Mutation(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun027-mutation")
	pf := newPromotionFixture(t, values.TenantId("wfrun027-mutation-tenant"), "intent:wf-run-027-mutation")
	req := pf.baseStartRequest(tenantID, "start-key-wfrun027-mutation")
	req.ApprovalFacts = runtime.MemoryApprovalFacts{ByRevisionID: map[string][]runtime.ApprovalDecisionFact{
		pf.Proposal.ProposalRevisionID: {
			{DecisionID: "decision:good", Outcome: runtime.ApprovalOutcomeApproved, ProposalDigest: pf.Proposal.MaterialDigest.Digest},
			{DecisionID: "decision:bad", Outcome: runtime.ApprovalOutcomeApproved, ProposalDigest: "sha256:1234500000000000000000000000000000000000000000000000000000aa"},
		},
	}}

	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		_, err := runtime.Start(context.Background(), tx, req)
		return err
	})
	if runtime.CodeOf(err) != runtime.CodeApprovalBindingMismatch {
		t.Fatalf("code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeApprovalBindingMismatch, err)
	}
}
