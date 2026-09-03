package lifecycle

import (
	"errors"
	"fmt"
	"strings"
)

// Rule names one of the fixed kernel legality rules. There are exactly six;
// see [Rules]. There is no per-definition compatibility lattice: a definition
// may declare which transitions it uses, never a different legality rule set.
type Rule string

// The six fixed legality rules from the kernel contract.
const (
	// RuleTerminalCannotExecute: CANCELLED, SUPERSEDED, REJECTED or WITHDRAWN
	// cannot enter EXECUTING.
	RuleTerminalCannotExecute Rule = "terminal-request-cannot-execute"

	// RuleApprovedRequiresBinding: APPROVED requires one valid
	// requirement-level ApprovalBinding for the current proposal revision,
	// unless the definition declares approval not required.
	RuleApprovedRequiresBinding Rule = "approved-requires-current-revision-binding"

	// RuleMaterialRevisionInvalidatesApproval: a new material proposal revision
	// invalidates every ApprovalBinding whose approved digest differs from the
	// current material digest. A binding left standing across a material change
	// is the violation.
	RuleMaterialRevisionInvalidatesApproval Rule = "material-revision-invalidates-approval"

	// RuleCommittedRequiresReceipt: COMMITTED requires a CommitReceipt;
	// REPAIR_REQUIRED requires a linked RepairPlan or incident.
	RuleCommittedRequiresReceipt Rule = "committed-requires-receipt"

	// RuleClosureRequiresObligations: CLOSED requires ObligationState in
	// {SATISFIED, WAIVED, NOT_APPLICABLE} and ConsistencyState not in
	// {PENDING_OBSERVATION, REPAIRING}, unless the definition's closure policy
	// explicitly permits closing with an open repair.
	RuleClosureRequiresObligations Rule = "closed-requires-discharged-obligations"

	// RuleUnspecifiedInvalid: UNSPECIFIED is invalid for persisted active
	// instances.
	RuleUnspecifiedInvalid Rule = "unspecified-invalid-for-active-instance"
)

// Rules returns the six fixed legality rules, in evaluation order. The list is
// closed: adding a seventh rule is a kernel contract change, not a definition
// or domain decision.
func Rules() []Rule {
	return []Rule{
		RuleTerminalCannotExecute,
		RuleApprovedRequiresBinding,
		RuleMaterialRevisionInvalidatesApproval,
		RuleCommittedRequiresReceipt,
		RuleClosureRequiresObligations,
		RuleUnspecifiedInvalid,
	}
}

// Transition-integrity causes. These are not legality rules: they reject a
// malformed transition attempt before legality is even evaluated.
var (
	// ErrDimensionLoss reports a transition that would drop a dimension that
	// previously carried a declared value.
	ErrDimensionLoss = errors.New("lifecycle: transition loses a prior dimension")

	// ErrUndeclaredTransition reports a transition the dimension's lifecycle
	// profile does not declare.
	ErrUndeclaredTransition = errors.New("lifecycle: undeclared transition")

	// ErrIllegalTuple reports a tuple that fails one or more legality rules.
	ErrIllegalTuple = errors.New("lifecycle: illegal dimension tuple")
)

// ApprovalBindingState is the minimal projection of an ApprovalBinding that the
// legality rules read. The kernel never needs the whole binding to decide
// legality, and keeping the projection narrow stops approval storage details
// from leaking into the rule set.
type ApprovalBindingState struct {
	// RequirementID names the approval requirement the binding discharges.
	RequirementID string

	// RequirementLevel is true when the binding satisfies a requirement rather
	// than recording an advisory or informational decision.
	RequirementLevel bool

	// ApprovedProposalDigest is the material proposal digest the approver
	// actually bound to, never a mutable request id.
	ApprovedProposalDigest string

	// Approved is false for a reject/abstain/more-information decision.
	Approved bool

	// Invalidated records that the binding has already been marked invalid.
	Invalidated bool
}

// Context carries the linked records the fixed rules consult. Every field is
// evidence that lives outside the five dimensions: proposal revisions, approval
// bindings, commit receipts, repair plans and closure policy are records, not
// dimensions.
type Context struct {
	// ApprovalRequired mirrors the definition's declaration. When false,
	// RuleApprovedRequiresBinding does not demand a binding.
	ApprovalRequired bool

	// CurrentMaterialDigest is the material proposal digest of the current
	// revision. An empty value means no proposal revision exists yet.
	CurrentMaterialDigest string

	// Bindings are the approval bindings recorded against the intent.
	Bindings []ApprovalBindingState

	// CommitReceiptRef is the receipt an executed commit produced.
	CommitReceiptRef string

	// RepairRef links a RepairPlan or incident.
	RepairRef string

	// ClosurePolicyPermitsOpenRepair mirrors the definition's closure policy.
	ClosurePolicyPermitsOpenRepair bool

	// Persisted marks an active persisted instance, for which UNSPECIFIED is
	// invalid in every dimension.
	Persisted bool
}

// Violation is one typed legality failure. It names the exact rule and the
// dimension whose value triggered it, so callers never have to match strings.
type Violation struct {
	Rule      Rule
	Dimension Dimension
	Detail    string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s [%s]: %s", v.Rule, v.Dimension, v.Detail)
}

// ViolationSet is the error [Check] returns. It unwraps to [ErrIllegalTuple].
type ViolationSet struct {
	Violations []Violation
}

func (s *ViolationSet) Error() string {
	parts := make([]string, 0, len(s.Violations))
	for _, v := range s.Violations {
		parts = append(parts, v.String())
	}
	return ErrIllegalTuple.Error() + ": " + strings.Join(parts, "; ")
}

// Unwrap exposes the sentinel cause to [errors.Is].
func (s *ViolationSet) Unwrap() error { return ErrIllegalTuple }

// Has reports whether the set contains a violation of r.
func (s *ViolationSet) Has(r Rule) bool {
	for _, v := range s.Violations {
		if v.Rule == r {
			return true
		}
	}
	return false
}

// Check evaluates the six fixed legality rules against one dimension tuple and
// its linked records. It returns nil for a legal tuple and a *[ViolationSet]
// otherwise, and it never mutates anything.
//
// This is the one function that command transitions, projection rebuild,
// replay and repair all call. A projection that finds an illegal tuple must
// surface it, never silently select a preferred status.
func Check(d Dimensions, c Context) error {
	var vs []Violation

	// Rule 6 is evaluated first because an out-of-range or unspecified value
	// makes every later answer meaningless.
	if err := d.Validate(); err != nil {
		vs = append(vs, Violation{
			Rule:      RuleUnspecifiedInvalid,
			Dimension: DimensionRequest,
			Detail:    err.Error(),
		})
		return &ViolationSet{Violations: vs}
	}
	if c.Persisted {
		for _, dim := range AllDimensions() {
			if d.State(dim) == "UNSPECIFIED" {
				vs = append(vs, Violation{
					Rule:      RuleUnspecifiedInvalid,
					Dimension: dim,
					Detail:    "UNSPECIFIED is invalid for a persisted active instance",
				})
			}
		}
	}

	// Rule 1: terminal request states cannot enter EXECUTING.
	if d.Execution == ExecutionExecuting {
		switch d.Request {
		case RequestCancelled, RequestSuperseded, RequestRejected, RequestWithdrawn:
			vs = append(vs, Violation{
				Rule:      RuleTerminalCannotExecute,
				Dimension: DimensionExecution,
				Detail: fmt.Sprintf("RequestState=%s cannot enter ExecutionState=EXECUTING",
					d.Request),
			})
		}
	}

	// Rule 2: APPROVED requires a valid requirement-level binding for the
	// current material proposal digest.
	if d.Request == RequestApproved && c.ApprovalRequired {
		if !hasCurrentApproval(c) {
			vs = append(vs, Violation{
				Rule:      RuleApprovedRequiresBinding,
				Dimension: DimensionRequest,
				Detail: "no valid requirement-level ApprovalBinding for the current " +
					"material proposal digest",
			})
		}
	}

	// Rule 3: a binding whose approved digest differs from the current material
	// digest must already be invalidated.
	if c.CurrentMaterialDigest != "" {
		for _, b := range c.Bindings {
			if b.ApprovedProposalDigest != c.CurrentMaterialDigest && !b.Invalidated {
				vs = append(vs, Violation{
					Rule:      RuleMaterialRevisionInvalidatesApproval,
					Dimension: DimensionRequest,
					Detail: fmt.Sprintf(
						"ApprovalBinding for requirement %q still stands against a superseded material digest",
						b.RequirementID),
				})
			}
		}
	}

	// Rule 4: COMMITTED requires a receipt; REPAIR_REQUIRED requires a linked
	// RepairPlan or incident.
	if d.Execution == ExecutionCommitted && c.CommitReceiptRef == "" {
		vs = append(vs, Violation{
			Rule:      RuleCommittedRequiresReceipt,
			Dimension: DimensionExecution,
			Detail:    "COMMITTED without a CommitReceipt",
		})
	}
	if d.Execution == ExecutionRepairRequired && c.RepairRef == "" {
		vs = append(vs, Violation{
			Rule:      RuleCommittedRequiresReceipt,
			Dimension: DimensionExecution,
			Detail:    "REPAIR_REQUIRED without a linked RepairPlan or incident",
		})
	}

	// Rule 5: closure requires discharged obligations and settled consistency.
	if d.Request == RequestClosed {
		switch d.Obligation {
		case ObligationSatisfied, ObligationWaived, ObligationNotApplicable:
		default:
			vs = append(vs, Violation{
				Rule:      RuleClosureRequiresObligations,
				Dimension: DimensionObligation,
				Detail: fmt.Sprintf("CLOSED requires ObligationState in {SATISFIED, WAIVED, "+
					"NOT_APPLICABLE}, got %s", d.Obligation),
			})
		}
		switch d.Consistency {
		case ConsistencyPendingObservation, ConsistencyRepairing:
			if !c.ClosurePolicyPermitsOpenRepair {
				vs = append(vs, Violation{
					Rule:      RuleClosureRequiresObligations,
					Dimension: DimensionConsistency,
					Detail: fmt.Sprintf("CLOSED with ConsistencyState=%s requires a closure "+
						"policy that permits closing with an open repair", d.Consistency),
				})
			}
		}
	}

	if len(vs) == 0 {
		return nil
	}
	return &ViolationSet{Violations: vs}
}

func hasCurrentApproval(c Context) bool {
	if c.CurrentMaterialDigest == "" {
		return false
	}
	for _, b := range c.Bindings {
		if b.RequirementLevel && b.Approved && !b.Invalidated &&
			b.ApprovedProposalDigest == c.CurrentMaterialDigest {
			return true
		}
	}
	return false
}

// Preserves reports whether to keeps every dimension that from had already
// declared. A transition may advance a dimension; it may never erase one, which
// is what collapsing five dimensions into a single resolved flag would do.
func Preserves(from, to Dimensions) error {
	for _, dim := range AllDimensions() {
		if from.State(dim) != "UNSPECIFIED" && to.State(dim) == "UNSPECIFIED" {
			return fmt.Errorf("%w: %s was %s and would become UNSPECIFIED",
				ErrDimensionLoss, dim, from.State(dim))
		}
	}
	return nil
}
