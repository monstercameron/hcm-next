package runtime_test

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// WF-RUN-027's boundary, pinned as tests rather than left to the doc comment
// on [runtime.ProposalBinding].
//
// Two things have to stay true while the deprecated caller-asserted
// Approved/ApprovalRef/Superseded fields still exist:
//
//  1. A caller that supplies the facts ports is resolved from stored facts
//     exclusively. Its own flags are inert in both directions -- they can
//     neither admit a start the facts refuse nor refuse one the facts admit.
//     That is what makes internal/intent/app's migration off them meaningful.
//  2. A caller that supplies neither port is still resolved from its own
//     flags. That fallback is the only reason internal/workflow/execute,
//     internal/workflow/migrate, internal/workflow/replay and test/workflow
//     still compile against bindings carrying Approved/ApprovalRef, and it is
//     the remaining work this todo could not finish inside its own file
//     roots. If that fallback is ever deleted, this test is the one that says
//     which packages have to be migrated in the same change.

// TestTodo_WF_RUN_027_FactsOutrankCallerFlags proves the flags are inert once
// the ports are supplied, in both directions.
func TestTodo_WF_RUN_027_FactsOutrankCallerFlags(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun027-outranks")
	pf := newPromotionFixture(t, values.TenantId("wfrun027-outranks-tenant"), "intent:wf-run-027-outranks")

	t.Run("a caller denying its own approval still starts on the stored facts", func(t *testing.T) {
		req := pf.baseStartRequest(tenantID, "start-key-wfrun027-outranks-1")
		req.Proposal.Approved = false
		req.Proposal.ApprovalRef = ""
		req.Proposal.Superseded = true

		var receipt runtime.StartReceipt
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			var err error
			receipt, err = runtime.Start(context.Background(), tx, req)
			return err
		})
		if len(receipt.ApprovalDecisionIDs) != 1 || receipt.ApprovalDecisionIDs[0] != approvedDecisionID {
			t.Fatalf("ApprovalDecisionIDs = %v, want the stored decision [%s]",
				receipt.ApprovalDecisionIDs, approvedDecisionID)
		}
	})

	t.Run("a caller asserting approval is refused when the facts record none", func(t *testing.T) {
		req := pf.baseStartRequest(tenantID, "start-key-wfrun027-outranks-2")
		req.Proposal.Approved = true
		req.Proposal.ApprovalRef = "approval:invented-by-the-caller"
		req.ApprovalFacts = runtime.MemoryApprovalFacts{}

		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, startErr := runtime.Start(context.Background(), tx, req)
			return startErr
		})
		if runtime.CodeOf(err) != runtime.CodeUnapprovedProposal {
			t.Fatalf("code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeUnapprovedProposal, err)
		}
	})

	t.Run("a caller asserting currency is refused when the facts record a supersession", func(t *testing.T) {
		req := pf.baseStartRequest(tenantID, "start-key-wfrun027-outranks-3")
		req.Proposal.Superseded = false
		req.ProposalFacts = runtime.MemoryProposalFacts{
			Facts: map[string]runtime.ProposalSupersessionFact{
				pf.Proposal.ProposalRevisionID: {
					Superseded: true, SupersededByRevisionID: "revision:later",
				},
			},
		}

		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, startErr := runtime.Start(context.Background(), tx, req)
			return startErr
		})
		if runtime.CodeOf(err) != runtime.CodeSupersededProposal {
			t.Fatalf("code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeSupersededProposal, err)
		}
	})
}

// TestTodo_WF_RUN_027_LegacyFallbackSurface pins what the deprecated fallback
// still does for a caller that supplies neither port, so the residual gap is a
// stated fact rather than an assumption.
func TestTodo_WF_RUN_027_LegacyFallbackSurface(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun027-legacy")
	pf := newPromotionFixture(t, values.TenantId("wfrun027-legacy-tenant"), "intent:wf-run-027-legacy")

	legacy := func(key string) runtime.StartRequest {
		req := pf.baseStartRequest(tenantID, key)
		req.ProposalFacts = nil
		req.ApprovalFacts = nil
		return req
	}

	t.Run("caller-asserted approval admits the start and is the receipt's decision id", func(t *testing.T) {
		req := legacy("start-key-wfrun027-legacy-1")
		req.Proposal.Approved = true
		req.Proposal.ApprovalRef = "approval:caller-asserted"

		var receipt runtime.StartReceipt
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			var err error
			receipt, err = runtime.Start(context.Background(), tx, req)
			return err
		})
		if len(receipt.ApprovalDecisionIDs) != 1 || receipt.ApprovalDecisionIDs[0] != "approval:caller-asserted" {
			t.Fatalf("ApprovalDecisionIDs = %v, want the caller's own reference", receipt.ApprovalDecisionIDs)
		}
	})

	t.Run("caller-asserted denial and supersession still refuse", func(t *testing.T) {
		unapproved := legacy("start-key-wfrun027-legacy-2")
		unapproved.Proposal.Approved = false
		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, startErr := runtime.Start(context.Background(), tx, unapproved)
			return startErr
		})
		if runtime.CodeOf(err) != runtime.CodeUnapprovedProposal {
			t.Fatalf("code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeUnapprovedProposal, err)
		}

		superseded := legacy("start-key-wfrun027-legacy-3")
		superseded.Proposal.Approved = true
		superseded.Proposal.ApprovalRef = "approval:caller-asserted"
		superseded.Proposal.Superseded = true
		err = inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, startErr := runtime.Start(context.Background(), tx, superseded)
			return startErr
		})
		if runtime.CodeOf(err) != runtime.CodeSupersededProposal {
			t.Fatalf("code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeSupersededProposal, err)
		}
	})

	t.Run("supplying one port without the other is refused rather than half-resolved", func(t *testing.T) {
		req := legacy("start-key-wfrun027-legacy-4")
		req.Proposal.Approved = true
		req.Proposal.ApprovalRef = "approval:caller-asserted"
		proposalFacts, _ := approvedProposalFacts(pf.Proposal)
		req.ProposalFacts = proposalFacts

		err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
			_, startErr := runtime.Start(context.Background(), tx, req)
			return startErr
		})
		if runtime.CodeOf(err) != runtime.CodeInvalidRecord {
			t.Fatalf("code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeInvalidRecord, err)
		}
	})
}
