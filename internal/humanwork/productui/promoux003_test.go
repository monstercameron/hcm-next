package productui

// PROMOUX-003: "Enforce and explain separation of duties across promotion
// approvals." TestTodo_PROMOUX_003_Browser is the canonical BROWSER matrix
// entry: it proves the five GREEN facts -- Waiting for <role>, the assigned
// person or protected-group label, the due date, the viewer's acting
// authority and why the action is or is not available -- reach the actual
// rendered markup a People/Person Actions column's queue (the Work page's
// rows and preview) produces, sourced from
// internal/humanwork/workitem.ResolveApprovalDisposition and never
// recomputed here.

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
)

// promoux003Now and promoux003Due anchor every disposition this file builds
// so DueLabel formatting is deterministic across locales.
var (
	promoux003Now = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	promoux003Due = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
)

func promoux003AssignedItem(status workitem.Status, ownerRef string) workitem.WorkItem {
	return workitem.WorkItem{
		WorkItemID: uuid.MustParse("99999999-8888-4777-8666-555555555555"),
		Kind:       workitem.KindApproval, Status: status,
		ApprovalRequirementRef: "approval.promotion.finance_partner/v1",
		OwnerKind:              workitem.OwnerPrincipal, OwnerRef: ownerRef,
		DeadlineAt: promoux003Due,
	}
}

func promoux003ResolutionOf(principals ...string) humanwork.Resolution {
	candidates := make([]humanwork.Candidate, 0, len(principals))
	for _, p := range principals {
		candidates = append(candidates, humanwork.Candidate{PrincipalID: p, Via: humanwork.SourceDirect})
	}
	return humanwork.Resolution{Candidates: candidates}
}

func TestTodo_PROMOUX_003_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	const viewer = "principal:finance-partner-1"

	t.Run("the assigned viewer's card names the role, themself, the due date and their own authority", func(t *testing.T) {
		item := promoux003AssignedItem(workitem.StatusAssigned, viewer)
		disposition := workitem.ResolveApprovalDisposition(item, nil, viewer, promoux003Now)
		projection := ApprovalDispositionProjectionFrom(disposition)

		// Props contract first: approvalDispositionCardProps is the one
		// seam that turns the domain verdict into presentation, and it must
		// not silently drop or alter a fact ResolveApprovalDisposition
		// already decided.
		props := approvalDispositionCardProps(locale, &projection)
		if !props.Show {
			t.Fatal("a resolved disposition must render a card")
		}
		if props.WaitingFor != "Waiting for Finance Partner" {
			t.Fatalf("WaitingFor = %q, want the literal GREEN phrasing", props.WaitingFor)
		}
		if props.AssignedTo != "Assigned to Finance Partner 1" || props.AssignedIsGroup {
			t.Fatalf("AssignedTo/IsGroup = %q/%t, want the assigned person, not a group", props.AssignedTo, props.AssignedIsGroup)
		}
		if props.Due != "Due "+locale.FormatDate(promoux003Due) {
			t.Fatalf("Due = %q, want the formatted deadline", props.Due)
		}
		if props.ViewerAuthority != "You are acting as Finance Partner." {
			t.Fatalf("ViewerAuthority = %q, want the viewer's own resolved authority", props.ViewerAuthority)
		}
		if !props.Available || props.Reason != "You may decide this approval." {
			t.Fatalf("Available/Reason = %t/%q, want an authorized, explained action", props.Available, props.Reason)
		}

		// The rendered result: the Work page's row and preview actually
		// contain these facts, not just the props struct that feeds them.
		rowMarkup, err := ui.RenderToString(WorkRow(WorkRowProps{ID: item.WorkItemID.String(), Title: "Promote Jane Smith", Disposition: props}))
		if err != nil {
			t.Fatalf("render WorkRow: %v", err)
		}
		for _, want := range []string{"Waiting for Finance Partner"} {
			if !strings.Contains(rowMarkup, want) {
				t.Fatalf("WorkRow markup missing %q: %s", want, rowMarkup)
			}
		}

		previewMarkup, err := ui.RenderToString(WorkPreview(WorkPreviewProps{ID: item.WorkItemID.String(), Title: "Promote Jane Smith", Disposition: props, Action: ActionLinkProps{Label: "Open live journey", Href: "/journey"}}))
		if err != nil {
			t.Fatalf("render WorkPreview: %v", err)
		}
		for _, want := range []string{
			"Waiting for Finance Partner",
			"Assigned to Finance Partner 1",
			"Due " + locale.FormatDate(promoux003Due),
			"You are acting as Finance Partner.",
			"You may decide this approval.",
		} {
			if !strings.Contains(previewMarkup, want) {
				t.Fatalf("WorkPreview markup missing %q: %s", want, previewMarkup)
			}
		}
	})

	t.Run("a group-assigned item never renders the raw candidate-set reference", func(t *testing.T) {
		item := promoux003AssignedItem(workitem.StatusAvailable, "")
		item.OwnerKind = workitem.OwnerCandidateSet
		item.OwnerRef = "candidates:approval.promotion.finance_partner/v1@sha256:" + strings.Repeat("a", 64)
		item.Assignment.Resolution = promoux003ResolutionOf("principal:finance-partner-1", "principal:finance-partner-2")

		disposition := workitem.ResolveApprovalDisposition(item, nil, "principal:finance-partner-2", promoux003Now)
		projection := ApprovalDispositionProjectionFrom(disposition)
		props := approvalDispositionCardProps(locale, &projection)

		if !props.AssignedIsGroup {
			t.Fatal("a candidate-set owner must render as a group")
		}
		if strings.Contains(props.AssignedTo, "candidates:") || strings.Contains(props.AssignedTo, "sha256:") {
			t.Fatalf("AssignedTo leaked the internal candidate-set reference: %q", props.AssignedTo)
		}
		if props.AssignedTo != "Assigned to a protected group of approvers" {
			t.Fatalf("AssignedTo = %q, want the generic protected-group phrase", props.AssignedTo)
		}

		markup, err := ui.RenderToString(ui.CreateElement(ApprovalDispositionCard, props))
		if err != nil {
			t.Fatalf("render ApprovalDispositionCard: %v", err)
		}
		if strings.Contains(markup, "candidates:") || strings.Contains(markup, "sha256:") {
			t.Fatalf("rendered card leaked the internal candidate-set reference: %s", markup)
		}
		if !strings.Contains(markup, "Assigned to a protected group of approvers") {
			t.Fatalf("rendered card missing the protected-group label: %s", markup)
		}
	})

	t.Run("an uninvolved viewer's card discloses the queue entry but not an action", func(t *testing.T) {
		item := promoux003AssignedItem(workitem.StatusAssigned, viewer)
		disposition := workitem.ResolveApprovalDisposition(item, nil, "principal:bystander", promoux003Now)
		projection := ApprovalDispositionProjectionFrom(disposition)
		props := approvalDispositionCardProps(locale, &projection)

		if props.Available {
			t.Fatal("an uninvolved viewer must not be offered the action")
		}
		if props.ViewerAuthority != "You hold no acting authority for this approval." {
			t.Fatalf("ViewerAuthority = %q, want the no-authority phrase", props.ViewerAuthority)
		}
		if props.Reason != "You do not hold authority to decide this approval." {
			t.Fatalf("Reason = %q, want the no-authority explanation", props.Reason)
		}
		// Still disclosed: who and when, even without authority to act.
		if props.WaitingFor == "" || props.AssignedTo == "" || props.Due == "" {
			t.Fatalf("an uninvolved viewer's card omitted disclosed facts: %+v", props)
		}

		markup, err := ui.RenderToString(ui.CreateElement(ApprovalDispositionCard, props))
		if err != nil {
			t.Fatalf("render ApprovalDispositionCard: %v", err)
		}
		if !strings.Contains(markup, "disposition-unavailable") {
			t.Fatalf("an unavailable disposition did not render its unavailable tone: %s", markup)
		}
	})

	t.Run("a separation-of-duties refusal explains the rule without naming who completed the conflicting sibling", func(t *testing.T) {
		const conflicted = "principal:someone-else"
		item := promoux003AssignedItem(workitem.StatusInProgress, conflicted)
		item.ClaimedBy = conflicted
		claimExpiry := promoux003Now.Add(time.Hour)
		item.ClaimExpiresAt = &claimExpiry

		sibling := item
		sibling.WorkItemID = uuid.MustParse("99999999-8888-4777-8666-555555555556")
		sibling.ApprovalRequirementRef = "approval.promotion.current_manager/v1"
		sibling.Status = workitem.StatusCompleted
		sibling.CompletedBy = conflicted

		disposition := workitem.ResolveApprovalDisposition(item, []workitem.WorkItem{sibling}, conflicted, promoux003Now)
		if disposition.Reason != workitem.DispositionSeparationConflict {
			t.Fatalf("fixture disposition reason = %q, want %q", disposition.Reason, workitem.DispositionSeparationConflict)
		}
		projection := ApprovalDispositionProjectionFrom(disposition)
		props := approvalDispositionCardProps(locale, &projection)

		if props.Available {
			t.Fatal("a separation-of-duties conflict must not be offered as available")
		}
		wantReason := "You have already decided a different approval requirement on this proposal, so you may not also decide this one."
		if props.Reason != wantReason {
			t.Fatalf("Reason = %q, want %q", props.Reason, wantReason)
		}

		markup, err := ui.RenderToString(ui.CreateElement(ApprovalDispositionCard, props))
		if err != nil {
			t.Fatalf("render ApprovalDispositionCard: %v", err)
		}
		if !strings.Contains(markup, wantReason) {
			t.Fatalf("rendered card missing the separation-conflict reason: %s", markup)
		}
		// The rule is named; the identity of whoever completed the sibling
		// requirement is not, anywhere in the card -- only the conflicted
		// viewer's own principal ever appears (as the assignee, which is
		// already disclosed to them), never a second, different identity.
		if strings.Contains(markup, "someone-else") {
			t.Fatalf("rendered card leaked an identity beyond the viewer's own: %s", markup)
		}
	})

	t.Run("no disposition renders no card", func(t *testing.T) {
		props := approvalDispositionCardProps(locale, nil)
		if props.Show {
			t.Fatal("a nil disposition must not render a card")
		}
		markup, err := ui.RenderToString(ui.CreateElement(ApprovalDispositionCard, props))
		if err != nil {
			t.Fatalf("render ApprovalDispositionCard: %v", err)
		}
		if strings.TrimSpace(markup) != "" {
			t.Fatalf("a hidden disposition rendered markup: %q", markup)
		}
	})
}
