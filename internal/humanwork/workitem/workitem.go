package workitem

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// digestPattern matches the sha256:<hex> shape migration 00017 requires for
// assignment_digest and completed_output_digest.
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ValidDigest reports whether s is a well-formed content digest reference.
func ValidDigest(s string) bool { return digestPattern.MatchString(s) }

// WorkItem is the durable unit of human responsibility: where responsibility
// is right now. Every field here is a column of migrations/00017_work_item.sql
// "work_item" table, in the same order, so a reader can hold the two side by
// side.
type WorkItem struct {
	TenantID    uuid.UUID
	WorkItemID  uuid.UUID
	ItemVersion int64

	Kind     Kind
	WorkType string
	Status   Status

	CorrelationID          string
	WorkflowInstanceID     uuid.UUID
	NodeID                 string
	ApprovalRequirementRef string
	ProposalRef            string

	SubjectRefs []string

	OwnerKind      OwnerKind
	OwnerRef       string
	PolicyRouteRef string

	Visibility          Visibility
	OrganizationScopeID string

	DeadlineAt time.Time

	// Assignment is the WORK-002 evidence: the resolution expression, the
	// candidate set, every exclusion, the directory and policy versions, and
	// the trigger that produced it. It is the zero value until [Store.Route]
	// has run at least once.
	Assignment       Assignment
	AssignmentDigest string

	ClaimID        *uuid.UUID
	ClaimedBy      string
	ClaimedAt      *time.Time
	ClaimExpiresAt *time.Time

	CompletedBy           string
	CompletedAt           *time.Time
	CompletedOutputDigest string

	CreatedAt  time.Time
	RecordedAt time.Time
}

// ClaimExpired reports whether the item carries a claim whose expiry is at or
// before now. It is the read-only verdict [Store.Load] leaves you to compute
// yourself, since a load is not a touch and never releases anything.
func (w WorkItem) ClaimExpired(now time.Time) bool {
	return w.ClaimExpiresAt != nil && !now.Before(*w.ClaimExpiresAt)
}

// NewWorkItemInput is the identity and routing frame a work item is created
// with. CREATED items always carry OwnerKind POLICY_ROUTE against
// PolicyRouteRef -- routing to an actual owner is [Store.Route]'s job, done
// against a compiled [Assignment].
type NewWorkItemInput struct {
	TenantID   uuid.UUID
	WorkItemID uuid.UUID // zero value: minted here

	Kind     Kind
	WorkType string

	CorrelationID          string
	WorkflowInstanceID     uuid.UUID
	NodeID                 string
	ApprovalRequirementRef string
	ProposalRef            string

	SubjectRefs []string

	PolicyRouteRef      string
	Visibility          Visibility
	OrganizationScopeID string

	DeadlineAt time.Time
	CreatedAt  time.Time
}

// NewWorkItem builds a CREATED work item ready for [Store.Create].
func NewWorkItem(in NewWorkItemInput) (WorkItem, error) {
	id := in.WorkItemID
	if id == uuid.Nil {
		id = uuid.New()
	}
	w := WorkItem{
		TenantID:               in.TenantID,
		WorkItemID:             id,
		ItemVersion:            1,
		Kind:                   in.Kind,
		WorkType:               in.WorkType,
		Status:                 StatusCreated,
		CorrelationID:          in.CorrelationID,
		WorkflowInstanceID:     in.WorkflowInstanceID,
		NodeID:                 in.NodeID,
		ApprovalRequirementRef: in.ApprovalRequirementRef,
		ProposalRef:            in.ProposalRef,
		SubjectRefs:            append([]string(nil), in.SubjectRefs...),
		OwnerKind:              OwnerPolicyRoute,
		OwnerRef:               in.PolicyRouteRef,
		PolicyRouteRef:         in.PolicyRouteRef,
		Visibility:             in.Visibility,
		OrganizationScopeID:    in.OrganizationScopeID,
		DeadlineAt:             in.DeadlineAt,
		CreatedAt:              in.CreatedAt,
	}
	if err := w.Validate(); err != nil {
		return WorkItem{}, err
	}
	return w, nil
}

// NewApprovalTask builds a CREATED work item specializing [KindApproval],
// naming the requirement it decides. There is no parallel approval-task
// table: this constructor is the whole of the specialization, exactly as
// WORK-001's REFACTOR clause requires.
func NewApprovalTask(in NewWorkItemInput, approvalRequirementRef string) (WorkItem, error) {
	in.Kind = KindApproval
	in.ApprovalRequirementRef = approvalRequirementRef
	return NewWorkItem(in)
}

// Validate reports whether the item is storable on its own terms: every field
// WORK-001's RED clause names -- correlation, subject, owner, visibility,
// deadline -- present, every enum a declared value, and the claim and
// completion column groups either fully absent or fully present and
// consistent with status. It mirrors migration 00017's CHECK constraints so
// a defect is caught in Go before a statement is even sent, and a test proves
// the two never drift apart.
func (w WorkItem) Validate() error {
	id := w.WorkItemID.String()
	switch {
	case w.TenantID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "tenant id must not be the nil UUID")
	case w.WorkItemID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "work item id must not be the nil UUID")
	case w.ItemVersion < 1:
		return refuse(CodeInvalidRecord, id, "item version must be at least 1")
	case !w.Kind.Valid():
		return refuse(CodeInvalidRecord, id, "kind %q is not declared", string(w.Kind))
	case !semanticKey(w.WorkType):
		return refuse(CodeInvalidRecord, id, "work type is required")
	case !w.Status.Valid():
		return refuse(CodeInvalidRecord, id, "status %q is not declared", string(w.Status))
	case !semanticKey(w.CorrelationID):
		return refuse(CodeInvalidRecord, id, "correlation id is required")
	case w.WorkflowInstanceID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "workflow instance id must not be the nil UUID")
	case !semanticKey(w.NodeID):
		return refuse(CodeInvalidRecord, id, "node id is required")
	case (w.Kind == KindApproval) != (w.ApprovalRequirementRef != ""):
		return refuse(CodeInvalidRecord, id,
			"an APPROVAL item must name the requirement it decides and a TASK must not carry one")
	case len(w.SubjectRefs) == 0:
		return refuse(CodeInvalidRecord, id, "a work item must name at least one subject")
	case !w.OwnerKind.Valid():
		return refuse(CodeInvalidRecord, id, "owner kind %q is not declared", string(w.OwnerKind))
	case !semanticKey(w.OwnerRef):
		return refuse(CodeInvalidRecord, id, "a work item must name an owner")
	case !semanticKey(w.PolicyRouteRef):
		return refuse(CodeInvalidRecord, id, "a work item must name its policy route")
	case !w.Visibility.Valid():
		return refuse(CodeInvalidRecord, id, "visibility %q is not declared", string(w.Visibility))
	case !semanticKey(w.OrganizationScopeID):
		return refuse(CodeInvalidRecord, id, "a work item must name its organization scope")
	case w.DeadlineAt.IsZero():
		return refuse(CodeInvalidRecord, id, "a work item must carry a deadline")
	case w.CreatedAt.IsZero():
		return refuse(CodeInvalidRecord, id, "created_at must be supplied; this package never reads a wall clock")
	}
	for _, ref := range w.SubjectRefs {
		if !semanticKey(ref) {
			return refuse(CodeInvalidRecord, id, "subject refs may not be blank")
		}
	}
	if w.AssignmentDigest != "" && !ValidDigest(w.AssignmentDigest) {
		return refuse(CodeInvalidRecord, id, "assignment digest %q is malformed", w.AssignmentDigest)
	}

	claimSet := w.ClaimID != nil || w.ClaimedBy != "" || w.ClaimedAt != nil || w.ClaimExpiresAt != nil
	claimComplete := w.ClaimID != nil && w.ClaimedBy != "" && w.ClaimedAt != nil && w.ClaimExpiresAt != nil
	if claimSet && !claimComplete {
		return refuse(CodeInvalidRecord, id, "claim id, holder, claimed-at and expiry move together or not at all")
	}
	if claimComplete != w.Status.Claimed() {
		return refuse(CodeInvalidRecord, id, "a claim is present iff status is CLAIMED or IN_PROGRESS")
	}
	if claimComplete && !w.ClaimExpiresAt.After(*w.ClaimedAt) {
		return refuse(CodeInvalidRecord, id, "claim expiry must be after claimed_at")
	}

	completionSet := w.CompletedBy != "" || w.CompletedAt != nil || w.CompletedOutputDigest != ""
	completionComplete := w.CompletedBy != "" && w.CompletedAt != nil && w.CompletedOutputDigest != ""
	if completionSet && !completionComplete {
		return refuse(CodeInvalidRecord, id, "completed-by, completed-at and output digest move together or not at all")
	}
	if completionComplete != (w.Status == StatusCompleted) {
		return refuse(CodeInvalidRecord, id, "a completed output digest is present iff status is COMPLETED")
	}
	if completionComplete && !ValidDigest(w.CompletedOutputDigest) {
		return refuse(CodeInvalidRecord, id, "completed output digest %q is malformed", w.CompletedOutputDigest)
	}
	return nil
}

// semanticKey reports whether s is non-blank and carries no leading or
// trailing whitespace, matching the semantic_key domain migration
// 00002_tenant_primitives.sql declares.
func semanticKey(s string) bool {
	return s != "" && s == strings.TrimSpace(s)
}
