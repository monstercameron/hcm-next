package inspect

import (
	"sort"
	"time"

	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
)

// WorkItemAuthorization is the already-evaluated authorization decision the
// caller hands [BuildWorkItems], mirroring [Authorization]'s own shape: this
// package never evaluates policy, only renders what one decided. It is
// deliberately simpler than [Authorization] -- one disclosure gate for the
// whole work-item list, no per-field rulings -- because ADMIN-008's operator
// profile is coarse-grained by design (see internal/operations/admin's
// OperatorWorkflowInstanceAuthorization); a future caller needing per-item or
// per-field work-item redaction extends this type then, not speculatively
// now.
type WorkItemAuthorization struct {
	// Disclosed reports whether the caller may see the instance's work items
	// at all. False renders an empty list with DeniedReason set, never a
	// silently omitted section.
	Disclosed bool
	// DeniedReason is the policy token for a denied caller.
	DeniedReason string
}

// TransitionView is one rendered work_item_transition row.
type TransitionView struct {
	TransitionID     string `json:"transition_id"`
	ItemVersion      int64  `json:"item_version"`
	FromStatus       string `json:"from_status,omitempty"`
	ToStatus         string `json:"to_status"`
	ActorPrincipalID string `json:"actor_principal_id"`
	Reason           string `json:"reason"`
	Detail           string `json:"detail,omitempty"`
	// EvidenceRef and its GapKind: most transitions legitimately carry no
	// evidence (GapKindAbsent), which is a different fact from a transition
	// this caller may not see evidence for (GapKindRedacted). Nothing about
	// work-item evidence is ever UNRECORDED: no invariant requires it.
	EvidenceRef Ref       `json:"evidence_ref"`
	EvidenceGap GapKind   `json:"evidence_gap"`
	At          time.Time `json:"at"`
	RecordedAt  time.Time `json:"recorded_at"`
}

// WorkItemView is one rendered work_item row plus its full transition
// chronology.
type WorkItemView struct {
	WorkItemID    string `json:"work_item_id"`
	ItemVersion   int64  `json:"item_version"`
	Kind          string `json:"kind"`
	WorkType      string `json:"work_type"`
	Status        string `json:"status"`
	CorrelationID string `json:"correlation_id"`
	NodeID        string `json:"node_id"`

	// ApprovalRequirementRef is UNRECORDED, not merely ABSENT, when Kind is
	// APPROVAL: migration 00017 requires this column non-null exactly then,
	// so an empty value on an APPROVAL item is a hole the projection expected
	// to fill, not a legitimate "no approval needed" answer.
	ApprovalRequirementRef Ref     `json:"approval_requirement_ref"`
	ApprovalRequirementGap GapKind `json:"approval_requirement_gap"`
	// ProposalRef carries no such invariant: any kind of work item may or may
	// not trace back to a proposal, so an empty value is always ABSENT.
	ProposalRef Ref     `json:"proposal_ref"`
	ProposalGap GapKind `json:"proposal_gap"`

	SubjectRefs RefList `json:"subject_refs"`

	OwnerKind      string `json:"owner_kind"`
	OwnerRef       string `json:"owner_ref,omitempty"`
	PolicyRouteRef string `json:"policy_route_ref"`
	Visibility     string `json:"visibility"`

	DeadlineAt time.Time `json:"deadline_at"`

	ClaimedBy   string `json:"claimed_by,omitempty"`
	CompletedBy string `json:"completed_by,omitempty"`
	// CompletedOutputDigest is ABSENT for any item not yet COMPLETED, never
	// UNRECORDED: completion is what makes it exist.
	CompletedOutputDigest Ref     `json:"completed_output_digest"`
	CompletedOutputGap    GapKind `json:"completed_output_gap"`

	CreatedAt time.Time `json:"created_at"`

	// Transitions is the item's whole evidence chain. TransitionsRecorded is
	// false, and the item is named in the enclosing [WorkItemsResult]'s
	// Completeness.Gaps, when it loaded to zero: [workitem.Store.Create]
	// always appends the CREATED row in the same statement pair as the item
	// itself, so a stored item with no transitions is a gap, not an empty
	// history.
	Transitions         []TransitionView `json:"transitions"`
	TransitionsRecorded bool             `json:"transitions_recorded"`
}

// WorkItemsResult is the whole rendered work-item section: every item the
// frontier created for the instance, its transitions, and what was withheld
// or missing. It reuses [Completeness] rather than inventing a second shape
// for "what does this section not say and why."
type WorkItemsResult struct {
	Disclosed    bool           `json:"disclosed"`
	DeniedReason string         `json:"denied_reason,omitempty"`
	Items        []WorkItemView `json:"items"`
	Completeness Completeness   `json:"completeness"`
}

// BuildWorkItems renders one instance's work items and their transitions
// under auth. It performs no I/O: items and transitionsByItem are already
// loaded (internal/humanwork/workitem.Store.ListForInstance and
// LoadTransitions), exactly like [Build] takes an already-loaded
// [runtime.Instance]. This is the "work-item ... port" [Build]'s own doc
// promises the caller composes alongside it, kept as a sibling function
// rather than folded into [Request]/[View] so this package's existing,
// already-golden-pinned traversal render is untouched by ADMIN-008.
//
// transitionsByItem is keyed by the work item id's string form rather than
// uuid.UUID: this package's callers include internal/transport/admin, which
// LIB-002/LIB-004's dependency-roles.yaml does not admit as an import root
// for "github.com/google/uuid" -- a string key lets that caller build the
// map without ever spelling the uuid type itself.
func BuildWorkItems(
	items []workitem.WorkItem, transitionsByItem map[string][]workitem.TransitionRecord, auth WorkItemAuthorization,
) WorkItemsResult {
	if !auth.Disclosed {
		return WorkItemsResult{
			Disclosed:    false,
			DeniedReason: auth.DeniedReason,
			Items:        []WorkItemView{},
			Completeness: Completeness{Complete: true, Redactions: []string{"WORK_ITEMS"}},
		}
	}

	c := &collector{}
	sorted := append([]workitem.WorkItem(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].NodeID != sorted[j].NodeID {
			return sorted[i].NodeID < sorted[j].NodeID
		}
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})

	out := make([]WorkItemView, 0, len(sorted))
	for _, item := range sorted {
		out = append(out, buildWorkItem(item, transitionsByItem[item.WorkItemID.String()], c))
	}
	return WorkItemsResult{Disclosed: true, Items: out, Completeness: c.completeness()}
}

func buildWorkItem(item workitem.WorkItem, transitions []workitem.TransitionRecord, c *collector) WorkItemView {
	out := WorkItemView{
		WorkItemID:     item.WorkItemID.String(),
		ItemVersion:    item.ItemVersion,
		Kind:           string(item.Kind),
		WorkType:       item.WorkType,
		Status:         string(item.Status),
		CorrelationID:  item.CorrelationID,
		NodeID:         item.NodeID,
		SubjectRefs:    RefListValue(item.SubjectRefs),
		OwnerKind:      string(item.OwnerKind),
		OwnerRef:       item.OwnerRef,
		PolicyRouteRef: item.PolicyRouteRef,
		Visibility:     string(item.Visibility),
		DeadlineAt:     item.DeadlineAt,
		ClaimedBy:      item.ClaimedBy,
		CompletedBy:    item.CompletedBy,
		CreatedAt:      item.CreatedAt,
	}

	out.ApprovalRequirementRef, out.ApprovalRequirementGap = classifyApprovalRef(item, c)
	out.ProposalRef, out.ProposalGap = RefValue(item.ProposalRef), GapKindAbsent
	if item.ProposalRef != "" {
		out.ProposalGap = GapKindNone
	}
	out.CompletedOutputDigest, out.CompletedOutputGap = RefValue(item.CompletedOutputDigest), GapKindAbsent
	if item.CompletedOutputDigest != "" {
		out.CompletedOutputGap = GapKindNone
	}

	sortedTransitions := append([]workitem.TransitionRecord(nil), transitions...)
	sort.SliceStable(sortedTransitions, func(i, j int) bool {
		return sortedTransitions[i].ItemVersion < sortedTransitions[j].ItemVersion
	})
	out.TransitionsRecorded = len(sortedTransitions) > 0
	if !out.TransitionsRecorded {
		c.gap("work_item.%s: no transitions recorded", item.WorkItemID)
	}
	out.Transitions = make([]TransitionView, 0, len(sortedTransitions))
	for _, t := range sortedTransitions {
		evidence, gap := RefValue(t.EvidenceRef), GapKindAbsent
		if t.EvidenceRef != "" {
			gap = GapKindNone
		}
		out.Transitions = append(out.Transitions, TransitionView{
			TransitionID:     t.TransitionID.String(),
			ItemVersion:      t.ItemVersion,
			FromStatus:       string(t.FromStatus),
			ToStatus:         string(t.ToStatus),
			ActorPrincipalID: t.ActorPrincipalID,
			Reason:           t.Reason,
			Detail:           t.Detail,
			EvidenceRef:      evidence,
			EvidenceGap:      gap,
			At:               t.At,
			RecordedAt:       t.RecordedAt,
		})
	}
	return out
}

// classifyApprovalRef renders ApprovalRequirementRef and reports whether its
// emptiness is a legitimate ABSENT or an UNRECORDED gap: migration 00017's
// own constraint makes this column non-null exactly when Kind is APPROVAL, so
// an APPROVAL item with no approval requirement ref is a hole this projection
// must name, and any other item's empty value is simply correct.
func classifyApprovalRef(item workitem.WorkItem, c *collector) (Ref, GapKind) {
	if item.ApprovalRequirementRef != "" {
		return RefValue(item.ApprovalRequirementRef), GapKindNone
	}
	if item.Kind == workitem.KindApproval {
		c.gap("work_item.%s: approval_requirement_ref not recorded for an APPROVAL item", item.WorkItemID)
		return RefAbsent(), GapKindUnrecorded
	}
	return RefAbsent(), GapKindAbsent
}
