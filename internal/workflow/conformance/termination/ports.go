package termination

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// digest is a small, deterministic content identity for this fixture's own
// transform outputs. It only has to be stable across runs of the same
// inputs, which sha256 over a fixed field order gives for free.
func digest(profile string, parts ...string) string {
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))[:32]
}

// Decisions evaluates the approval-and-separation-of-duties DECISION node.
//
// It is the runtime proof of three of CONF-005's RED clauses at once: a
// legal hold blocks rather than being silently proceeded past, a requester
// who is also the current manager (requester == approver) is refused rather
// than resolved, and a cancellation reaching an employment that has already
// ended takes the reinstatement route rather than mutating the original
// terminated request (the REFACTOR clause). Precedence matches the routes
// declared on the compiled node: reinstatement and already-ended are
// evaluated before legal hold or SoD, because neither of those questions is
// meaningful once the employment has already ended.
type Decisions struct{}

func (Decisions) Decide(_ context.Context, req simulate.DecisionRequest) (simulate.DecisionResult, error) {
	if req.Decision.RuleRef != RuleApprovalAndSoD {
		return simulate.DecisionResult{}, fmt.Errorf("termination: no evaluator is bound to rule %q", req.Decision.RuleRef)
	}
	legalHold, err := boolInput(req.Inputs, "legal_hold_active")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	requesterID, err := req.Inputs.Text("requester_id")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	managerID, err := req.Inputs.Text("current_manager_id")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	cancelRequested, err := boolInput(req.Inputs, "cancel_requested")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	employmentActive, err := boolInput(req.Inputs, "employment_active")
	if err != nil {
		return simulate.DecisionResult{}, err
	}

	var route, detail string
	switch {
	case cancelRequested && !employmentActive:
		route = RouteRequiresReinstatement
		detail = "cancellation was requested against an employment that has already ended; this becomes a reinstatement/correction intent, not a mutation of the original request"
	case !employmentActive:
		route = RouteAlreadyEndedInvalid
		detail = "the target employment has already ended and no cancellation was requested"
	case legalHold:
		route = RouteLegalHoldBlocked
		detail = "an active legal hold blocks the termination"
	case requesterID != "" && requesterID == managerID:
		route = RouteSoDViolationBlocked
		detail = "the requester is the current manager; separation of duties requires requester != approver"
	default:
		route = RouteRoutineApprovalRequired
		detail = "no blocking condition; routine HR/legal/manager/employee-relations/finance approvals are required"
	}
	return simulate.DecisionResult{
		RouteKey: route,
		TraceRef: RuleApprovalAndSoD + "#" + digest("trace", requesterID, managerID, boolText(legalHold), boolText(cancelRequested), boolText(employmentActive))[:16],
		Detail:   detail,
	}, nil
}

func boolInput(b simulate.Bag, path string) (bool, error) {
	v, err := b.Get(path)
	if err != nil {
		return false, err
	}
	return v.Bool()
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Transforms evaluates the two pure TRANSFORM nodes.
type Transforms struct{}

func (Transforms) Transform(_ context.Context, req simulate.TransformRequest) (simulate.TransformResult, error) {
	switch req.Transform.TransformRef {
	case TransformFinalPay:
		return transformFinalPay(req)
	case TransformBuildProposal:
		return transformBuildProposal(req)
	default:
		return simulate.TransformResult{}, fmt.Errorf("termination: transform %s has no bound implementation", req.Transform.TransformRef)
	}
}

// transformFinalPay computes the final-pay deadline and amount.
//
// No native payroll engine is implemented here, in the same spirit CONF-006's
// GREEN clause allows for payroll correction: the deadline is the effective
// date itself (the reference workflow's own obligation is that a deadline
// exists and is bound to the proposal, not that this fixture reproduces a
// jurisdiction's payroll calendar), and the amount is a deterministic,
// declared placeholder.
func transformFinalPay(req simulate.TransformRequest) (simulate.TransformResult, error) {
	effective, err := req.Inputs.Get("effective_date")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	jurisdiction, err := req.Inputs.Text("jurisdiction")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	amount, err := values.NewMoney("0.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		return simulate.TransformResult{}, fmt.Errorf("termination: final pay amount: %w", err)
	}
	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"final_pay_deadline": effective,
			"final_pay_amount":   simulate.NewMoney(amount),
		},
		Detail: "computed final pay deadline " + effective.Text + " under jurisdiction " + jurisdiction,
	}, nil
}

func transformBuildProposal(req simulate.TransformRequest) (simulate.TransformResult, error) {
	worker, err := req.Inputs.Text("worker_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	employment, err := req.Inputs.Text("employment_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	deadline, err := req.Inputs.Text("final_pay_deadline")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	jurisdiction, err := req.Inputs.Text("jurisdiction")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	pd := digest("hcmnext.workflow.conformance.termination.Proposal/v1", worker, employment, deadline, jurisdiction)
	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{"proposal_digest": simulate.NewString(pd)},
		Detail:  "bound termination proposal " + pd,
	}, nil
}

// Reads answers the P0 access-revocation OBSERVE node from the environment's
// declared observation outcome. [Environment.ObserveOutcome] controls it, so
// a caller can walk either the consistent pending-obligations path or the
// bounded repair path.
type Reads struct{ Env *Environment }

func (r Reads) Observe(_ context.Context, req simulate.ObservationRequest) (simulate.Observation, error) {
	outcome := r.Env.ObserveOutcome
	if outcome == "" {
		outcome = workflow.OutcomeUnknown
	}
	return simulate.Observation{
		Outcome:   outcome,
		Outputs:   simulate.Bag{"access_state": simulate.NewString(string(outcome))},
		Watermark: r.Env.ObserveWatermark,
		Detail:    "observed " + req.Observe.SourceAuthority + " for P0 access revocation: " + string(outcome),
	}, nil
}

// Approvals derives the constant approval graph the reference workflow's
// terminals declare, without waiting for any of them. Every declared
// requirement ref gets exactly one work item; there is no silent narrowing.
type Approvals struct{}

func (Approvals) WouldAwait(_ context.Context, req simulate.ApprovalRequest) ([]simulate.WorkItem, error) {
	if len(req.RequirementRefs) == 0 {
		return nil, nil
	}
	refs := append([]string(nil), req.RequirementRefs...)
	sort.Strings(refs)
	items := make([]simulate.WorkItem, 0, len(refs))
	for i, ref := range refs {
		items = append(items, simulate.WorkItem{
			NodeID:            req.NodeID,
			Kind:              "APPROVAL",
			State:             simulate.WouldAwait,
			RequirementID:     ref,
			Stage:             uint32(i + 1),
			QuorumMin:         1,
			Outcome:           "RESOLVED",
			Candidates:        []string{"principal:" + ref + "-approver-1 via ROLE"},
			ExpressionDigest:  digest("hcmnext.workflow.conformance.termination.ApprovalExpression/v1", ref),
			RequirementDigest: digest("hcmnext.workflow.conformance.termination.ApprovalRequirement/v1", ref),
			Tier:              "STANDARD",
		})
	}
	return items, nil
}
