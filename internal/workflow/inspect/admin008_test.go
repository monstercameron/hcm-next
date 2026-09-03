package inspect_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/workflow/inspect"
)

// marshalIndent renders v as indented, deterministic JSON with a trailing
// newline, matching [inspect.View.JSON]'s own convention so the two golden
// files in this package read the same way.
func marshalIndent(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ADMIN-008's RED clause: inspect.Build has no caller outside tests (fixed by
// internal/transport/admin.server.GetWorkflowInstance, exercised end to end
// by TestTodo_ADMIN_008_Integration); a denied field is dropped instead of
// rendered REDACTED; and an empty governance ref is indistinguishable from an
// unrecorded one. This file is the pure, network-free half of that matrix:
// inspect.BuildWorkItems and its [inspect.GapKind] classification, which is
// where the third state -- UNRECORDED, as opposed to correctly ABSENT or
// caller-REDACTED -- actually gets computed for a reference this package can
// state an invariant about.

var (
	admin008Tenant   = uuid.MustParse("44444444-4444-4444-8444-444444444444")
	admin008Instance = uuid.MustParse("55555555-5555-4555-8555-555555555555")

	admin008ItemApproved  = uuid.MustParse("66666666-6666-4666-8666-666666666666")
	admin008ItemTask      = uuid.MustParse("77777777-7777-4777-8777-777777777777")
	admin008ItemNoApprove = uuid.MustParse("88888888-8888-4888-8888-888888888888")

	admin008Created = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
)

// admin008ApprovalItem is a well-formed APPROVAL work item: its
// ApprovalRequirementRef is populated, so its GapKind is NONE.
func admin008ApprovalItem() workitem.WorkItem {
	return workitem.WorkItem{
		TenantID:               admin008Tenant,
		WorkItemID:             admin008ItemApproved,
		ItemVersion:            1,
		Kind:                   workitem.KindApproval,
		WorkType:               "promotion.finance_approval",
		Status:                 workitem.StatusCreated,
		WorkflowInstanceID:     admin008Instance,
		NodeID:                 "finance_approval",
		ApprovalRequirementRef: "approval-req:88191",
		OwnerKind:              workitem.OwnerPolicyRoute,
		PolicyRouteRef:         "route:finance",
		Visibility:             workitem.VisibilityOrganizationScope,
		DeadlineAt:             admin008Created.Add(48 * time.Hour),
		CreatedAt:              admin008Created,
	}
}

// admin008TaskItem is a well-formed TASK item: it correctly has no approval
// requirement, so its GapKind is ABSENT, not UNRECORDED.
func admin008TaskItem() workitem.WorkItem {
	return workitem.WorkItem{
		TenantID:           admin008Tenant,
		WorkItemID:         admin008ItemTask,
		ItemVersion:        1,
		Kind:               workitem.KindTask,
		WorkType:           "promotion.payroll_sync_review",
		Status:             workitem.StatusCreated,
		WorkflowInstanceID: admin008Instance,
		NodeID:             "payroll_sync",
		OwnerKind:          workitem.OwnerPolicyRoute,
		PolicyRouteRef:     "route:payroll",
		Visibility:         workitem.VisibilityOrganizationScope,
		DeadlineAt:         admin008Created.Add(24 * time.Hour),
		CreatedAt:          admin008Created,
	}
}

// admin008AnomalousApprovalItem is an APPROVAL item with no
// ApprovalRequirementRef -- [workitem.WorkItem.Validate] refuses this shape
// at write time, so it can only ever be seen by a projection reading around a
// data anomaly. That is exactly the case GapKindUnrecorded exists for: the
// projection must name the hole rather than render it as an unremarkable
// absence.
func admin008AnomalousApprovalItem() workitem.WorkItem {
	item := admin008ApprovalItem()
	item.WorkItemID = admin008ItemNoApprove
	item.NodeID = "finance_approval_2"
	item.ApprovalRequirementRef = ""
	return item
}

// admin008TransitionNamespace derives deterministic transition ids: a golden
// file must not depend on uuid.New(), matching this package's own fixtures'
// "no clock, no random source" rule.
var admin008TransitionNamespace = uuid.MustParse("99999999-9999-4999-8999-999999999999")

func admin008Transition(item workitem.WorkItem, version int64, from, to workitem.Status, evidenceRef string) workitem.TransitionRecord {
	transitionID := uuid.NewSHA1(admin008TransitionNamespace, []byte(item.WorkItemID.String()+string(to)))
	return workitem.TransitionRecord{
		TenantID:         item.TenantID,
		TransitionID:     transitionID,
		WorkItemID:       item.WorkItemID,
		ItemVersion:      version,
		FromStatus:       from,
		ToStatus:         to,
		ActorPrincipalID: "principal:ops-7",
		Reason:           workitem.ReasonCreated,
		EvidenceRef:      evidenceRef,
		At:               admin008Created,
		RecordedAt:       admin008Created,
	}
}

// TestTodo_ADMIN_008 is the PRIMARY case: BuildWorkItems renders every item
// and every transition under a disclosed authorization, and classifies each
// protected reference's emptiness correctly -- ABSENT for a TASK's
// legitimately-missing approval ref and a transition's legitimately-missing
// evidence, UNRECORDED for an APPROVAL item's missing approval ref, and never
// a bare, unclassified blank.
func TestTodo_ADMIN_008(t *testing.T) {
	t.Parallel()

	approval := admin008ApprovalItem()
	task := admin008TaskItem()
	anomalous := admin008AnomalousApprovalItem()

	items := []workitem.WorkItem{approval, task, anomalous}
	transitions := map[string][]workitem.TransitionRecord{
		approval.WorkItemID.String(): {admin008Transition(approval, 1, "", workitem.StatusCreated, "evidence:approval-created")},
		task.WorkItemID.String():     {admin008Transition(task, 1, "", workitem.StatusCreated, "")},
		// anomalous carries no transitions at all: a second, independent gap.
	}

	result := inspect.BuildWorkItems(items, transitions, inspect.WorkItemAuthorization{Disclosed: true})

	if !result.Disclosed {
		t.Fatalf("result not disclosed: %+v", result)
	}
	if len(result.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(result.Items))
	}

	byID := map[string]inspect.WorkItemView{}
	for _, v := range result.Items {
		byID[v.WorkItemID] = v
	}

	// The well-formed APPROVAL item: a rendered ref, no gap.
	got := byID[approval.WorkItemID.String()]
	if v, ok := got.ApprovalRequirementRef.Get(); !ok || v != "approval-req:88191" {
		t.Errorf("approval item's approval ref = %+v, want a disclosed value", got.ApprovalRequirementRef)
	}
	if got.ApprovalRequirementGap != inspect.GapKindNone {
		t.Errorf("approval item's approval gap = %s, want none", got.ApprovalRequirementGap)
	}
	if !got.TransitionsRecorded || len(got.Transitions) != 1 {
		t.Fatalf("approval item transitions = %+v, want exactly one", got.Transitions)
	}
	if got.Transitions[0].EvidenceGap != inspect.GapKindNone {
		t.Errorf("approval item's CREATED transition evidence gap = %s, want none (evidence was recorded)", got.Transitions[0].EvidenceGap)
	}

	// The well-formed TASK item: ABSENT, not UNRECORDED.
	gotTask := byID[task.WorkItemID.String()]
	if _, ok := gotTask.ApprovalRequirementRef.Get(); ok {
		t.Errorf("task item's approval ref is disclosed, want absent: %+v", gotTask.ApprovalRequirementRef)
	}
	if gotTask.ApprovalRequirementGap != inspect.GapKindAbsent {
		t.Errorf("task item's approval gap = %s, want ABSENT (a task correctly has none)", gotTask.ApprovalRequirementGap)
	}
	if len(gotTask.Transitions) != 1 || gotTask.Transitions[0].EvidenceGap != inspect.GapKindAbsent {
		t.Errorf("task item's CREATED transition evidence gap = %+v, want ABSENT (no evidence was ever expected)", gotTask.Transitions)
	}

	// The anomalous APPROVAL item: UNRECORDED for the ref, and a second gap
	// for the missing transition history -- two independent holes, both named.
	gotAnomalous := byID[anomalous.WorkItemID.String()]
	if gotAnomalous.ApprovalRequirementGap != inspect.GapKindUnrecorded {
		t.Errorf("anomalous approval item's gap = %s, want UNRECORDED", gotAnomalous.ApprovalRequirementGap)
	}
	if gotAnomalous.TransitionsRecorded {
		t.Error("anomalous item reports transitions recorded, want false: it loaded with zero")
	}

	if result.Completeness.Complete {
		t.Fatalf("result reports itself complete despite two named gaps: %+v", result.Completeness)
	}
	wantGaps := []string{
		"work_item." + anomalous.WorkItemID.String() + ": approval_requirement_ref not recorded for an APPROVAL item",
		"work_item." + anomalous.WorkItemID.String() + ": no transitions recorded",
	}
	if len(result.Completeness.Gaps) != len(wantGaps) {
		t.Fatalf("gaps = %v, want %v", result.Completeness.Gaps, wantGaps)
	}
	for i, g := range wantGaps {
		if result.Completeness.Gaps[i] != g {
			t.Errorf("gap[%d] = %q, want %q", i, result.Completeness.Gaps[i], g)
		}
	}

	// A denied caller sees no items at all, named as a redaction rather than
	// silently rendered as an empty (and therefore "nothing exists") list.
	denied := inspect.BuildWorkItems(items, transitions, inspect.WorkItemAuthorization{
		Disclosed: false, DeniedReason: "NOT_IN_SCOPE",
	})
	if denied.Disclosed {
		t.Fatal("denied result reports itself disclosed")
	}
	if len(denied.Items) != 0 {
		t.Fatalf("denied result carries %d items, want 0", len(denied.Items))
	}
	if len(denied.Completeness.Redactions) != 1 || denied.Completeness.Redactions[0] != "WORK_ITEMS" {
		t.Fatalf("denied redactions = %v, want [WORK_ITEMS]", denied.Completeness.Redactions)
	}
}

// TestTodo_ADMIN_008_Golden pins the rendered work-item view byte for byte,
// the same discipline TestTodo_WF_RUN_019_Golden applies to the traversal:
// a stage that quietly stops rendering shows up as removed lines in review.
func TestTodo_ADMIN_008_Golden(t *testing.T) {
	t.Parallel()

	approval := admin008ApprovalItem()
	task := admin008TaskItem()
	items := []workitem.WorkItem{approval, task}
	transitions := map[string][]workitem.TransitionRecord{
		approval.WorkItemID.String(): {admin008Transition(approval, 1, "", workitem.StatusCreated, "evidence:approval-created")},
		task.WorkItemID.String():     {admin008Transition(task, 1, "", workitem.StatusCreated, "")},
	}
	result := inspect.BuildWorkItems(items, transitions, inspect.WorkItemAuthorization{Disclosed: true})

	rendered, err := marshalIndent(result)
	if err != nil {
		t.Fatalf("render result: %v", err)
	}
	golden(t, "admin008_work_items.json", rendered)
}
