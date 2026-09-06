// Package workitem: this file is APPROVAL-007.
// [MaterializeApprovalRequirements] takes one compiled
// [humanwork.RequirementSet] and the proposal it binds, and produces exactly
// one governed WorkItem per required decision slot -- one per
// [humanwork.ApprovalRequirement] in the set -- created and routed in the
// caller's own transaction. It is the store-backed generalization of what
// internal/platform/execution's promotionWorkItems hand-assembles today for
// one fixed requirement: every candidate, exclusion, directory version and
// requirement digest here comes from a real [humanwork.Resolve] call through
// [ResolveAssignment], never fabricated, and it materializes a whole graph of
// required decisions, not a single one.
package workitem

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/humanwork"
)

// Reason codes this file mints for the two transitions materializing one
// requirement always produces: entering existence, and the resolved routing
// outcome.
const (
	// ReasonRequirementMaterialized is stamped on the CREATED row for the
	// work item raised for one required decision slot.
	ReasonRequirementMaterialized = "humanwork.approval.requirement_materialized"
	// ReasonRequirementRouted is stamped on the transition that records one
	// requirement's resolved routing outcome -- ASSIGNED, AVAILABLE or
	// ESCALATED.
	ReasonRequirementRouted = "humanwork.approval.requirement_routed"
)

// ApprovalProposalBinding is the proposal-side identity a requirement set is
// materialized against: the workflow instance and node the approval step
// belongs to, the proposal it decides, and the correlation id and creation
// instant stamped on every work item and transition this call produces.
//
// It is a narrow shape owned by this package rather than
// internal/workflow/runtime.ProposalBinding: tools/policy/libfirewall's
// semantic firewall reserves the internal/workflow import edge to the
// workflow lane, so a caller there adapts its own proposal binding into this
// one field-for-field rather than this package reaching into internal/workflow
// itself.
type ApprovalProposalBinding struct {
	TenantID            uuid.UUID
	WorkflowInstanceID  uuid.UUID
	NodeID              string
	CorrelationID       string
	ProposalRef         string
	SubjectRefs         []string
	OrganizationScopeID string
	// PolicyRouteRef is the one route every requirement in the set escalates
	// to when its own resolution finds nobody: the fixed governance queue
	// behind the whole approval step, not a value that varies per requirement.
	PolicyRouteRef string
	CreatedAt      time.Time
}

// validate rejects a binding that could not produce a storable work item --
// the same completeness WORK-001's RED clause names for a single item,
// checked once here for the fields every requirement's item shares.
func (b ApprovalProposalBinding) validate() error {
	switch {
	case b.TenantID == uuid.Nil:
		return refuse(CodeInvalidRecord, "", "proposal binding names no tenant")
	case b.WorkflowInstanceID == uuid.Nil:
		return refuse(CodeInvalidRecord, "", "proposal binding names no workflow instance")
	case !semanticKey(b.NodeID):
		return refuse(CodeInvalidRecord, "", "proposal binding names no node")
	case !semanticKey(b.CorrelationID):
		return refuse(CodeInvalidRecord, "", "proposal binding names no correlation id")
	case !semanticKey(b.ProposalRef):
		return refuse(CodeInvalidRecord, "", "proposal binding names no proposal reference")
	case len(b.SubjectRefs) == 0:
		return refuse(CodeInvalidRecord, "", "proposal binding names no subject")
	case !semanticKey(b.OrganizationScopeID):
		return refuse(CodeInvalidRecord, "", "proposal binding names no organization scope")
	case !semanticKey(b.PolicyRouteRef):
		return refuse(CodeInvalidRecord, "", "proposal binding names no policy route")
	case b.CreatedAt.IsZero():
		return refuse(CodeInvalidRecord, "", "proposal binding carries no creation instant; this package never reads a wall clock")
	}
	for _, ref := range b.SubjectRefs {
		if !semanticKey(ref) {
			return refuse(CodeInvalidRecord, "", "proposal binding subject refs may not be blank")
		}
	}
	return nil
}

// MaterializationInput is [MaterializeApprovalRequirements]'s whole request.
type MaterializationInput struct {
	Requirements humanwork.RequirementSet
	Proposal     ApprovalProposalBinding

	// Resolution seeds every requirement's [humanwork.ResolutionInput] --
	// requester, subjects, effective time and deadline-passed state. Its
	// ClaimedBy is copied, never mutated in place, and threaded forward
	// within this call as each requirement's chosen owner is decided: a
	// later requirement in the set that shares OneRequirementPerPrincipal
	// excludes a principal already routed to an earlier slot in this very
	// call -- exactly what a caller driving
	// [humanwork.PromotionScenario.ResolveAll] by hand would otherwise have
	// to thread itself, per that method's own doc comment.
	Resolution humanwork.ResolutionInput
	Directory  humanwork.Directory
	Clock      humanwork.Clock

	// WorkItemID mints one requirement's work item identity. When nil, a
	// random identity is minted per requirement, matching [NewWorkItem]'s own
	// default. A caller that must reproduce the same identities -- replaying
	// a materialization that crashed before its transaction committed, or a
	// deterministic test -- supplies a function instead.
	WorkItemID func(requirementID string) uuid.UUID

	ActorPrincipalID string
}

// MaterializeApprovalRequirements resolves, creates and routes exactly one
// governed WorkItem per requirement in in.Requirements, in the set's own
// stage order, entirely through ex -- the caller's own transaction, never one
// this function opens itself.
//
// Each produced item carries: Kind APPROVAL and the exact requirement
// reference ([WorkItem.ApprovalRequirementRef]); the proposal reference every
// slot shares; a routed [Assignment] that is [ResolveAssignment]'s real
// answer, never reconstructed after the fact; a deadline taken from the
// requirement's own compiled Deadline policy
// ([humanwork.ApprovalRequirement.Deadline].Expiry); and a visibility derived
// from how that requirement actually resolved (see
// [VisibilityForAssignment]) rather than a value invented independently of
// the resolution.
//
// A failure at any requirement -- Resolve erroring, Create refusing an
// already-invalid record, Route losing a version race, or any storage
// failure -- returns immediately. This function issues no COMMIT and opens no
// transaction: every write already made through ex is left exactly as it
// stands for the caller's own transaction to roll back, so a partial failure
// never leaves a later requirement in the set silently unmaterialized while
// an earlier one is durably committed on its own -- either the whole set
// gets its accountable work, or the caller's rollback leaves none of it.
func MaterializeApprovalRequirements(
	ctx context.Context, ex Executor, store Port, in MaterializationInput,
) ([]WorkItem, error) {
	if store == nil {
		return nil, refuse(CodeInvalidRecord, "", "no work item store supplied")
	}
	if err := in.Proposal.validate(); err != nil {
		return nil, err
	}
	if len(in.Requirements.Requirements) == 0 {
		return nil, refuse(CodeInvalidRecord, "", "requirement set materializes no requirement")
	}
	if in.Directory == nil {
		return nil, refuse(CodeInvalidRecord, "", "no directory supplied")
	}
	if in.Clock == nil {
		return nil, refuse(CodeInvalidRecord, "", "no clock supplied")
	}
	if !semanticKey(in.ActorPrincipalID) {
		return nil, refuse(CodeInvalidRecord, "", "materialization must name an actor principal")
	}

	claimedBy := make(map[string]string, len(in.Resolution.ClaimedBy))
	for k, v := range in.Resolution.ClaimedBy {
		claimedBy[k] = v
	}

	items := make([]WorkItem, 0, len(in.Requirements.Requirements))
	for _, req := range in.Requirements.Requirements {
		resolutionInput := in.Resolution
		resolutionInput.ClaimedBy = claimedBy

		assignment, err := ResolveAssignment(req, resolutionInput, in.Directory, in.Clock, TriggerInitialRouting)
		if err != nil {
			return nil, err
		}

		workItemID := uuid.New()
		if in.WorkItemID != nil {
			workItemID = in.WorkItemID(req.RequirementID)
		}
		if workItemID == uuid.Nil {
			return nil, refuse(CodeInvalidRecord, "", "work item id function returned the nil UUID for %s", req.RequirementID)
		}

		newItem, err := NewApprovalTask(NewWorkItemInput{
			TenantID:            in.Proposal.TenantID,
			WorkItemID:          workItemID,
			WorkType:            req.RequirementID,
			CorrelationID:       in.Proposal.CorrelationID,
			WorkflowInstanceID:  in.Proposal.WorkflowInstanceID,
			NodeID:              in.Proposal.NodeID,
			ProposalRef:         in.Proposal.ProposalRef,
			SubjectRefs:         in.Proposal.SubjectRefs,
			PolicyRouteRef:      in.Proposal.PolicyRouteRef,
			Visibility:          VisibilityForAssignment(assignment),
			OrganizationScopeID: in.Proposal.OrganizationScopeID,
			DeadlineAt:          req.Deadline.Expiry.Time(),
			CreatedAt:           in.Proposal.CreatedAt,
		}, req.RequirementID)
		if err != nil {
			return nil, err
		}

		created, err := store.Create(ctx, ex, newItem, TransitionMeta{
			ActorPrincipalID: in.ActorPrincipalID,
			Reason:           ReasonRequirementMaterialized,
			Detail:           "requirement " + req.RequirementID,
			EvidenceRef:      req.Digest(),
			At:               in.Proposal.CreatedAt,
		})
		if err != nil {
			return nil, err
		}

		routed, err := store.Route(ctx, ex, created.TenantID, created.WorkItemID, created.ItemVersion, assignment,
			TransitionMeta{
				ActorPrincipalID: in.ActorPrincipalID,
				Reason:           ReasonRequirementRouted,
				Detail:           "resolution outcome " + string(assignment.Resolution.Outcome),
				EvidenceRef:      assignment.Resolution.RequirementDigest,
				At:               in.Proposal.CreatedAt,
			})
		if err != nil {
			return nil, err
		}

		if assignment.ChosenOwner != "" {
			claimedBy[assignment.ChosenOwner] = req.RequirementID
		}

		items = append(items, routed)
	}

	return items, nil
}

// VisibilityForAssignment derives a work item's visibility from its resolved
// assignment. [humanwork.ApprovalRequirement] carries no visibility field of
// its own, so visibility here follows from how the requirement actually
// resolved, mirroring [RouteFromAssignment]'s own three-way split: an item
// routed to exactly one candidate is visible only to that assignee, an item
// left open to a surviving candidate set is visible to that whole set, and an
// item nobody could be authorized for is escalated into tenant-governance
// visibility rather than left invisible to everyone who might act on it.
func VisibilityForAssignment(a Assignment) Visibility {
	if a.Resolution.Outcome != humanwork.OutcomeResolved || len(a.Resolution.Candidates) == 0 {
		return VisibilityTenantGovernance
	}
	if len(a.Resolution.Candidates) == 1 {
		return VisibilityAssigneeOnly
	}
	return VisibilityCandidateSet
}
