package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// digest is a small, deterministic content identity for this fixture's own
// transform outputs. It is not the interpreter's canonical digest (that stays
// internal to package simulate); it only has to be stable across runs of the
// same inputs, which sha256 over a fixed field order gives for free.
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

// Decisions evaluates the authority-and-conflict DECISION node.
//
// It is the runtime half of the REFACTOR clause: a transfer whose source and
// destination authority scopes have collapsed to the same value, or whose
// footprint already reports a conflict, is blocked rather than routed
// through as a same-company move dressed up as a cross-company transfer.
type Decisions struct{}

func (Decisions) Decide(_ context.Context, req simulate.DecisionRequest) (simulate.DecisionResult, error) {
	if req.Decision.RuleRef != RuleAuthorityConflict {
		return simulate.DecisionResult{}, fmt.Errorf("transfer: no evaluator is bound to rule %q", req.Decision.RuleRef)
	}
	conflict, err := req.Inputs.Get("conflict_detected")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	conflictDetected, err := conflict.Bool()
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	sourceScope, err := req.Inputs.Text("source_authority_scope")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	destScope, err := req.Inputs.Text("destination_authority_scope")
	if err != nil {
		return simulate.DecisionResult{}, err
	}

	sameScope := sourceScope != "" && sourceScope == destScope
	route := RouteCrossCompanyOK
	detail := "source and destination authority scopes remain separate and no conflict was detected"
	if sameScope {
		route = RouteConflictBlocked
		detail = "source and destination authority scopes have collapsed to one value; a transfer requires two separate company authority scopes"
	} else if conflictDetected {
		route = RouteConflictBlocked
		detail = "the conflict footprint reports a detected conflict"
	}
	return simulate.DecisionResult{
		RouteKey: route,
		TraceRef: RuleAuthorityConflict + "#" + digest("trace", sourceScope, destScope, conflict.Text)[:16],
		Detail:   detail,
	}, nil
}

// Transforms evaluates the two pure TRANSFORM nodes.
type Transforms struct{}

func (Transforms) Transform(_ context.Context, req simulate.TransformRequest) (simulate.TransformResult, error) {
	switch req.Transform.TransformRef {
	case TransformConflictFootprint:
		return transformConflictFootprint(req)
	case TransformBuildProposal:
		return transformBuildProposal(req)
	default:
		return simulate.TransformResult{}, fmt.Errorf("transfer: transform %s has no bound implementation", req.Transform.TransformRef)
	}
}

func transformConflictFootprint(req simulate.TransformRequest) (simulate.TransformResult, error) {
	sourceScope, err := req.Inputs.Text("source_authority_scope")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	destScope, err := req.Inputs.Text("destination_authority_scope")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	sourceActiveV, err := req.Inputs.Get("source_employment_active")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	sourceActive, err := sourceActiveV.Bool()
	if err != nil {
		return simulate.TransformResult{}, err
	}
	destAvailableV, err := req.Inputs.Get("destination_position_available")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	destAvailable, err := destAvailableV.Bool()
	if err != nil {
		return simulate.TransformResult{}, err
	}
	destBudgetV, err := req.Inputs.Get("destination_budget_available")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	destBudget, err := destBudgetV.Bool()
	if err != nil {
		return simulate.TransformResult{}, err
	}

	// Scope equality is deliberately not folded into this footprint: it is
	// the authority-and-conflict DECISION's own job (see [Decisions]) to
	// prove the source and destination remain separate authority scopes.
	// Keeping the two checks apart is what makes each one independently
	// load-bearing rather than one silently covering for the other.
	conflict := !sourceActive || !destAvailable || !destBudget
	fp := digest("hcmnext.workflow.conformance.transfer.Footprint/v1", sourceScope, destScope,
		boolText(sourceActive), boolText(destAvailable), boolText(destBudget))

	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"conflict_detected": simulate.NewBool(conflict),
			"footprint_digest":  simulate.NewString(fp),
		},
		Detail: "computed conflict footprint " + fp,
	}, nil
}

func transformBuildProposal(req simulate.TransformRequest) (simulate.TransformResult, error) {
	worker, err := req.Inputs.Text("worker_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	sourceCo, err := req.Inputs.Text("source_company_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	destCo, err := req.Inputs.Text("destination_company_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	jurisdiction, err := req.Inputs.Text("destination_jurisdiction")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	footprint, err := req.Inputs.Text("footprint_digest")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	effective, err := req.Inputs.Text("effective_date")
	if err != nil {
		return simulate.TransformResult{}, err
	}

	pd := digest("hcmnext.workflow.conformance.transfer.Proposal/v1", worker, sourceCo, destCo, jurisdiction, footprint, effective)
	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{"proposal_digest": simulate.NewString(pd)},
		Detail:  "bound cross-company transfer proposal " + pd,
	}, nil
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Reads answers the post-decision effective-date-conflict OBSERVE node from
// the environment's declared observation outcome. A caller controls it
// through [Environment.ObserveOutcome] to walk either the consistent
// pending-approvals path or the bounded repair path.
type Reads struct{ Env *Environment }

func (r Reads) Observe(_ context.Context, req simulate.ObservationRequest) (simulate.Observation, error) {
	outcome := r.Env.ObserveOutcome
	if outcome == "" {
		outcome = workflow.OutcomeUnknown
	}
	detail := "observed " + req.Observe.SourceAuthority + " for effective-date conflict: " + string(outcome)
	return simulate.Observation{
		Outcome:   outcome,
		Outputs:   simulate.Bag{"conflict_state": simulate.NewString(string(outcome))},
		Watermark: r.Env.ObserveWatermark,
		Detail:    detail,
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
			ExpressionDigest:  digest("hcmnext.workflow.conformance.transfer.ApprovalExpression/v1", ref),
			RequirementDigest: digest("hcmnext.workflow.conformance.transfer.ApprovalRequirement/v1", ref),
			Tier:              "STANDARD",
		})
	}
	return items, nil
}
