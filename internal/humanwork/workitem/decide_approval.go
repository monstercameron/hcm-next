// Package workitem: this file is EP-WORK-003's approval half.
// [Store.DecideApproval] composes exactly two things that already exist --
// [Store.CompleteWithAuthorityRecheck] (WORK-006: current session and
// authority, re-checked fresh against the item as it stands right now) and
// the digest formula every write in this package already requires -- rather
// than inventing a second decision mechanism. That is the REFACTOR clause
// literally: an approval decision is a specialized WorkItem completion, not
// a parallel write path, so it shares Complete's item_version compare-and-
// swap and append-only transition insert and performs no write beyond them.
// The business change a decision authorizes is never executed here: it
// happens later, when the workflow reads the COMPLETED transition and the
// decision digest this call appended.
package workitem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ApprovalDecision is the closed vocabulary an approval decision records. It
// mirrors the wire's ApprovalDecisionKind enum by hand rather than importing
// it: this package is domain, the wire enum is transport's concern
// (internal/transport/humanwork maps between the two), and a domain package
// importing a generated wire type would invert that layering.
type ApprovalDecision string

// Declared decisions. ApprovalDecisionUnspecified is the zero value and is
// never legal: a Go zero value must never mean "decided".
const (
	ApprovalDecisionUnspecified            ApprovalDecision = ""
	ApprovalDecisionApprove                ApprovalDecision = "APPROVE"
	ApprovalDecisionReject                 ApprovalDecision = "REJECT"
	ApprovalDecisionRequestMoreInformation ApprovalDecision = "REQUEST_MORE_INFORMATION"
	ApprovalDecisionAbstain                ApprovalDecision = "ABSTAIN"
)

var approvalDecisionWire = map[ApprovalDecision]bool{
	ApprovalDecisionApprove:                true,
	ApprovalDecisionReject:                 true,
	ApprovalDecisionRequestMoreInformation: true,
	ApprovalDecisionAbstain:                true,
}

// Valid reports whether d is a declared decision.
func (d ApprovalDecision) Valid() bool { return approvalDecisionWire[d] }

// CompletionDigest computes a plain (non-approval) completion's
// CompleteInput.CompletedOutputDigest from the caller-supplied output
// references, so transport never invents its own hashing scheme
// (ARCH-GO-023: this is domain logic, not a transport concern). Two
// completions citing the same work item, output artifact, form submission and
// evidence set always produce the same digest; evidence references are
// sorted first so their order at the call site never changes the result.
func CompletionDigest(workItemID uuid.UUID, outputArtifactRef, formSubmissionRef string, evidenceRefs []string) string {
	sorted := append([]string(nil), evidenceRefs...)
	sort.Strings(sorted)
	parts := append([]string{"workitem-completion", workItemID.String(), outputArtifactRef, formSubmissionRef}, sorted...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// DecisionDigest computes an approval decision's
// CompleteInput.CompletedOutputDigest from its typed content: the proposal
// revision it is bound to, the decision itself and its reason. Binding the
// digest to the proposal revision means a decision recorded against one
// proposal revision can never collide with one recorded against another, and
// a caller who replays an idempotency key with a changed decision produces a
// different digest that internal/transport/endpoint's payload-conflict check
// catches before this package is ever reached again.
func DecisionDigest(workItemID uuid.UUID, proposalRevisionRef string, decision ApprovalDecision, reasonRef string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"workitem-decision", workItemID.String(), proposalRevisionRef, string(decision), reasonRef,
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// DecisionResult is DecideApproval's specialized WorkItem result (REFACTOR:
// approval is a specialized WorkItem result, not a second decision
// mechanism): exactly what a caller needs to know about the decision just
// recorded, and nothing else -- no evidence, no candidate set, no assignment
// history, none of which this type even has a field for.
type DecisionResult struct {
	WorkItemID          uuid.UUID
	ProposalRevisionRef string
	Decision            ApprovalDecision
	ReasonRef           string
	DecidingPrincipal   string
	DecidedAt           time.Time
}

// NewDecisionResult builds the typed result from the just-decided work item
// (typically the exact value [Store.DecideApproval] returned) and the
// decision fields it was recorded with. It is a pure projection: nothing here
// re-reads storage, re-derives authority or performs any write.
func NewDecisionResult(item WorkItem, proposalRevisionRef string, decision ApprovalDecision, reasonRef string) DecisionResult {
	var decidedAt time.Time
	if item.CompletedAt != nil {
		decidedAt = *item.CompletedAt
	}
	return DecisionResult{
		WorkItemID:          item.WorkItemID,
		ProposalRevisionRef: proposalRevisionRef,
		Decision:            decision,
		ReasonRef:           reasonRef,
		DecidingPrincipal:   item.CompletedBy,
		DecidedAt:           decidedAt,
	}
}

// DecideApprovalInput is [Store.DecideApproval]'s request:
// [CompleteWithAuthorityRecheckInput]'s current-authority and session shape,
// plus what only an approval decision needs -- the proposal revision the
// caller has in view, the decision and its typed reason.
type DecideApprovalInput struct {
	TenantID        uuid.UUID
	WorkItemID      uuid.UUID
	ExpectedVersion int64

	ProposalRevisionRef string
	Decision            ApprovalDecision
	ReasonRef           string

	DecidingPrincipal string
	SessionRef        string
	Session           SessionRevocationPort
	Authority         AuthorityRecheckPort

	Now  time.Time
	Meta TransitionMeta
}

// DecideApproval is EP-WORK-003's approval write. It refuses a stale proposal
// digest -- the caller's ProposalRevisionRef no longer matching the item's
// current ProposalRef -- and a non-approval item before either current-state
// check runs, then delegates entirely to
// [Store.CompleteWithAuthorityRecheck]: the same current session/authority
// recheck, the same item_version compare-and-swap and the same append-only
// transition insert CompleteWorkItem uses. No domain mutation happens beyond
// that one guarded UPDATE of work_item and one INSERT into
// work_item_transition -- the business change a decision authorizes runs
// later, through the workflow that reads this transition.
func (s Store) DecideApproval(ctx context.Context, ex Executor, in DecideApprovalInput) (WorkItem, error) {
	if in.Session == nil || in.Authority == nil || in.SessionRef == "" {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(),
			"an approval decision requires a session reference and both current-state ports")
	}
	if in.Now.IsZero() {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(), "an approval decision requires Now")
	}
	if err := in.Meta.Validate(); err != nil {
		return WorkItem{}, err
	}
	if !semanticKey(in.DecidingPrincipal) {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(), "an approval decision must name who decided")
	}
	if !semanticKey(in.ProposalRevisionRef) {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(),
			"an approval decision must name the proposal revision it is bound to")
	}
	if !semanticKey(in.ReasonRef) {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(), "an approval decision must carry a typed reason reference")
	}
	if !in.Decision.Valid() {
		return WorkItem{}, refuse(CodeInvalidRecord, in.WorkItemID.String(), "decision %q is not a declared decision", string(in.Decision))
	}

	current, err := s.Load(ctx, ex, in.TenantID, in.WorkItemID)
	if err != nil {
		return WorkItem{}, err
	}
	if current.ItemVersion != in.ExpectedVersion {
		return WorkItem{}, s.explainLost(ctx, ex, in.TenantID, in.WorkItemID, in.ExpectedVersion)
	}
	if current.Kind != KindApproval {
		return WorkItem{}, refuse(CodeIllegalTransition, in.WorkItemID.String(),
			"work item %s is not an approval; DecideApproval does not apply to it", in.WorkItemID)
	}
	if current.ProposalRef == "" || current.ProposalRef != in.ProposalRevisionRef {
		return WorkItem{}, refuse(CodeStaleProposal, in.WorkItemID.String(),
			"the caller's proposal revision no longer matches this item's current one")
	}

	digest := DecisionDigest(in.WorkItemID, in.ProposalRevisionRef, in.Decision, in.ReasonRef)
	return s.CompleteWithAuthorityRecheck(ctx, ex, CompleteWithAuthorityRecheckInput{
		CompleteInput: CompleteInput{
			TenantID: in.TenantID, WorkItemID: in.WorkItemID, ExpectedVersion: in.ExpectedVersion,
			CompletedBy: in.DecidingPrincipal, CompletedOutputDigest: digest, Now: in.Now, Meta: in.Meta,
		},
		SessionRef: in.SessionRef, Session: in.Session, Authority: in.Authority,
	})
}
