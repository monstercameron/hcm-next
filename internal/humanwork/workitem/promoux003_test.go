package workitem_test

// PROMOUX-003: "Enforce and explain separation of duties across promotion
// approvals." This file supplies the INTEGRATION and BROWSER matrix
// entries.
//
// TestTodo_PROMOUX_003_Integration proves GREEN's absence claim --
// "reassignment preserves the proposal digest" -- against real PostgreSQL:
// [workitem.Store.Reassign] must never mutate what is being approved, only
// who is approving it.
//
// TestTodo_PROMOUX_003_Browser proves the presentation contract a
// People/Person Actions column renders from: everything hydrated
// client-side is read from the server-computed
// [workitem.ApprovalDisposition] props, never from a client-side guess, so
// this test asserts on those props directly rather than on any Render()
// output -- there is no SSR document to compare against for a value that is
// only ever hydrated client-side.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
)

// TestTodo_PROMOUX_003_Integration reaches a real store (pgtest) end to end:
// Create, Route to the HRBP, capture the full durable record, force a
// reassignment (the HRBP departs, exactly [TestTodo_WORK_004]'s own
// scenario) and require every field that is not part of "who is approving
// this" to compare identical -- named field by field, not merely
// non-empty -- while the owner itself does change.
func TestTodo_PROMOUX_003_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "promoux003-integration")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}
	conn := appConn(t, db)

	req, scenario := promotionRequirement(t, humanwork.RequirementHRBP)
	proposalRef := "sha256:" + repeatDigit("3")

	in := newTaskInput(tenant, instance)
	in.ProposalRef = proposalRef
	created, err := workitem.NewApprovalTask(in, req.RequirementID)
	if err != nil {
		t.Fatalf("NewApprovalTask: %v", err)
	}
	var item workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Create(ctx, tx, created, meta(workitem.ReasonCreated))
		return err
	})
	assignment, err := workitem.ResolveAssignment(req, scenario.Resolution, scenario.Directory, scenario.Clock, workitem.TriggerInitialRouting)
	if err != nil {
		t.Fatalf("ResolveAssignment: %v", err)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, assignment, meta("workitem.routed"))
		return err
	})
	if item.Status != workitem.StatusAssigned || item.OwnerRef != humanwork.PrincipalHRBP || item.ProposalRef != proposalRef {
		t.Fatalf("fixture item = %+v, want ASSIGNED/%s with proposal_ref %q", item, humanwork.PrincipalHRBP, proposalRef)
	}

	before := item // the full durable record, as PostgreSQL returned it from Route.

	scenario.Directory.SetActive(humanwork.PrincipalHRBP, false)
	var after workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		after, err = store.Reassign(ctx, tx, workitem.ReassignInput{
			TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
			Requirement: req, Resolution: scenario.Resolution, Directory: scenario.Directory, Clock: scenario.Clock,
			Meta: workitem.TransitionMeta{ActorPrincipalID: "system:directory-change", Reason: "work.reassignment.principal_departed", At: fixedInstant},
		})
		return err
	})

	// GREEN's own clause: reassignment happened (the owner really did move
	// to the deputy fallback) -- otherwise "the digest survived" would be
	// true only because nothing else did either.
	if after.OwnerRef != humanwork.PrincipalDeputy || after.OwnerKind != workitem.OwnerPrincipal {
		t.Fatalf("reassigned owner = %s/%s, want ASSIGNED/PRINCIPAL to the deputy fallback", after.OwnerKind, after.OwnerRef)
	}
	if after.ItemVersion == before.ItemVersion {
		t.Fatalf("reassignment left item_version at %d, unchanged", before.ItemVersion)
	}

	// The absence claim, proven by equality, not by non-emptiness: the
	// proposal's own canonical digest -- ProposalRef, the exact reference
	// [stepapproval.Complete] binds a decision against -- is identical
	// before and after.
	if before.ProposalRef == "" {
		t.Fatal("fixture carries no proposal digest to prove preserved")
	}
	if after.ProposalRef != before.ProposalRef {
		t.Fatalf("reassignment changed the proposal digest: before=%q after=%q", before.ProposalRef, after.ProposalRef)
	}

	// Field by field: every fact about *what* is being approved must be
	// bit-identical. A reassignment that quietly rewrote any of these -- the
	// subject, the correlation, the deadline, the requirement it decides --
	// would still pass a bare "ProposalRef is non-empty" check; it does not
	// pass this one.
	type immutableFact struct {
		name          string
		before, after any
	}
	facts := []immutableFact{
		{"TenantID", before.TenantID, after.TenantID},
		{"WorkItemID", before.WorkItemID, after.WorkItemID},
		{"Kind", before.Kind, after.Kind},
		{"WorkType", before.WorkType, after.WorkType},
		{"CorrelationID", before.CorrelationID, after.CorrelationID},
		{"WorkflowInstanceID", before.WorkflowInstanceID, after.WorkflowInstanceID},
		{"NodeID", before.NodeID, after.NodeID},
		{"ApprovalRequirementRef", before.ApprovalRequirementRef, after.ApprovalRequirementRef},
		{"ProposalRef", before.ProposalRef, after.ProposalRef},
		{"PolicyRouteRef", before.PolicyRouteRef, after.PolicyRouteRef},
		{"Visibility", before.Visibility, after.Visibility},
		{"OrganizationScopeID", before.OrganizationScopeID, after.OrganizationScopeID},
	}
	for _, f := range facts {
		if f.before != f.after {
			t.Errorf("reassignment changed %s: before=%v after=%v", f.name, f.before, f.after)
		}
	}
	if !before.DeadlineAt.Equal(after.DeadlineAt) {
		t.Errorf("reassignment changed DeadlineAt: before=%v after=%v", before.DeadlineAt, after.DeadlineAt)
	}
	if !before.CreatedAt.Equal(after.CreatedAt) {
		t.Errorf("reassignment changed CreatedAt: before=%v after=%v", before.CreatedAt, after.CreatedAt)
	}
	if len(before.SubjectRefs) != len(after.SubjectRefs) {
		t.Fatalf("reassignment changed SubjectRefs: before=%v after=%v", before.SubjectRefs, after.SubjectRefs)
	}
	for i := range before.SubjectRefs {
		if before.SubjectRefs[i] != after.SubjectRefs[i] {
			t.Fatalf("reassignment changed SubjectRefs[%d]: before=%q after=%q", i, before.SubjectRefs[i], after.SubjectRefs[i])
		}
	}
}

// TestResolveApprovalDispositionCoversEveryGreenState is the domain-level
// proof behind the BROWSER matrix entry: it exercises
// [workitem.ResolveApprovalDisposition] itself for every state GREEN names
// (waiting-for role, the assigned person or group, the due date, the
// viewer's acting authority, and why the action is or is not available),
// which is what internal/humanwork/productui's ApprovalDispositionProjectionFrom
// and approvalDispositionCardProps then render without recomputing any of
// it. The canonical TestTodo_PROMOUX_003_Browser -- asserting the rendered
// result, not just this props contract -- lives in
// internal/humanwork/productui/promoux003_test.go, which is where a
// People/Person Actions column's actual markup and hydrated props are.
func TestResolveApprovalDispositionCoversEveryGreenState(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	due := now.Add(48 * time.Hour)
	item := workitem.WorkItem{
		WorkItemID: uuid.New(), Kind: workitem.KindApproval, Status: workitem.StatusAssigned,
		ApprovalRequirementRef: "approval.promotion.finance_partner/v1",
		OwnerKind:              workitem.OwnerPrincipal, OwnerRef: "principal:finance-partner-1",
		DeadlineAt: due,
		Assignment: workitem.Assignment{Resolution: humanwork.Resolution{
			Candidates: []humanwork.Candidate{{PrincipalID: "principal:finance-partner-1", Via: humanwork.SourceDirect}},
		}},
	}

	t.Run("the assigned viewer sees their own authority and an available action", func(t *testing.T) {
		d := workitem.ResolveApprovalDisposition(item, nil, "principal:finance-partner-1", now)
		if d.WaitingForRef != item.ApprovalRequirementRef {
			t.Errorf("WaitingForRef = %q, want %q", d.WaitingForRef, item.ApprovalRequirementRef)
		}
		if d.AssignedRef != "principal:finance-partner-1" || d.AssignedIsGroup {
			t.Errorf("AssignedRef/IsGroup = %q/%t, want the assigned person, not a group", d.AssignedRef, d.AssignedIsGroup)
		}
		if !d.DueAt.Equal(due) {
			t.Errorf("DueAt = %v, want %v", d.DueAt, due)
		}
		if d.ViewerMembership != workitem.MembershipAssignee || d.ViewerAuthorityRef != item.ApprovalRequirementRef {
			t.Errorf("viewer authority = %v/%q, want MembershipAssignee/%q", d.ViewerMembership, d.ViewerAuthorityRef, item.ApprovalRequirementRef)
		}
		if !d.Available || d.Reason != workitem.DispositionAuthorized {
			t.Errorf("Available/Reason = %t/%q, want true/%q", d.Available, d.Reason, workitem.DispositionAuthorized)
		}
	})

	t.Run("an uninvolved viewer sees no authority and an explained unavailable action", func(t *testing.T) {
		d := workitem.ResolveApprovalDisposition(item, nil, "principal:bystander", now)
		if d.ViewerMembership != workitem.MembershipNone || d.ViewerAuthorityRef != "" {
			t.Errorf("bystander authority = %v/%q, want MembershipNone/empty", d.ViewerMembership, d.ViewerAuthorityRef)
		}
		if d.Available || d.Reason != workitem.DispositionNoAuthority {
			t.Errorf("Available/Reason = %t/%q, want false/%q", d.Available, d.Reason, workitem.DispositionNoAuthority)
		}
		// The waiting-for role, assignee and due date are still disclosed: a
		// viewer with no authority still sees the queue entry, just not the
		// action.
		if d.WaitingForRef == "" || d.AssignedRef == "" || d.DueAt.IsZero() {
			t.Errorf("disposition for an uninvolved viewer omitted disclosed facts: %+v", d)
		}
	})

	t.Run("a candidate-set owner is disclosed as a protected group, not a person", func(t *testing.T) {
		group := item
		group.OwnerKind = workitem.OwnerCandidateSet
		group.OwnerRef = "candidates:approval.promotion.finance_partner/v1@sha256:expr"
		group.Assignment.Resolution.Candidates = []humanwork.Candidate{
			{PrincipalID: "principal:finance-partner-1", Via: humanwork.SourceDirect},
			{PrincipalID: "principal:finance-partner-2", Via: humanwork.SourceDirect},
		}
		d := workitem.ResolveApprovalDisposition(group, nil, "principal:finance-partner-2", now)
		if !d.AssignedIsGroup || d.AssignedRef != group.OwnerRef {
			t.Errorf("group disclosure = isGroup=%t ref=%q, want true/%q", d.AssignedIsGroup, d.AssignedRef, group.OwnerRef)
		}
		if d.ViewerMembership != workitem.MembershipCandidate {
			t.Errorf("a surviving candidate's membership = %v, want MembershipCandidate", d.ViewerMembership)
		}
	})

	t.Run("a viewer who already decided the sibling requirement is refused with the separation reason, never a bare unavailable", func(t *testing.T) {
		sibling := item
		sibling.WorkItemID = uuid.New()
		sibling.ApprovalRequirementRef = "approval.promotion.current_manager/v1"
		sibling.Status = workitem.StatusCompleted
		sibling.CompletedBy = "principal:finance-partner-1"

		claimed := item
		claimed.Status = workitem.StatusInProgress
		claimed.ClaimedBy = "principal:finance-partner-1"
		claimExpiry := now.Add(time.Hour)
		claimed.ClaimExpiresAt = &claimExpiry

		d := workitem.ResolveApprovalDisposition(claimed, []workitem.WorkItem{sibling}, "principal:finance-partner-1", now)
		if d.Available {
			t.Fatalf("a principal who already decided %q must not be offered %q too: %+v", sibling.ApprovalRequirementRef, item.ApprovalRequirementRef, d)
		}
		if d.Reason != workitem.DispositionSeparationConflict {
			t.Fatalf("Reason = %q, want %q", d.Reason, workitem.DispositionSeparationConflict)
		}
		if ref := workitem.ConflictingSiblingRequirement([]workitem.WorkItem{sibling}, claimed.WorkItemID, claimed.ApprovalRequirementRef, "principal:finance-partner-1"); ref != sibling.ApprovalRequirementRef {
			t.Fatalf("ConflictingSiblingRequirement = %q, want %q so the UI can name which role was already decided", ref, sibling.ApprovalRequirementRef)
		}
	})
}
