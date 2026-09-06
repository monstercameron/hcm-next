package payroll

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
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

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Decisions evaluates the payroll release DECISION node.
//
// It is the runtime proof of CONF-009's RED clauses at once: a stale
// cutoff or a duplicate run is refused rather than released, and a
// reversal reaching an already-settled run takes a distinct
// reversal-intent route rather than mutating the original settled run
// (the same pattern CONF-005 established for termination cancellation).
// Precedence matches the routes declared on the compiled node.
type Decisions struct{}

func (Decisions) Decide(_ context.Context, req simulate.DecisionRequest) (simulate.DecisionResult, error) {
	if req.Decision.RuleRef != RuleReleaseAndIntegrity {
		return simulate.DecisionResult{}, fmt.Errorf("payroll: no evaluator is bound to rule %q", req.Decision.RuleRef)
	}
	cutoffActive, err := boolInput(req.Inputs, "cutoff_active")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	duplicateRun, err := boolInput(req.Inputs, "duplicate_run_detected")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	alreadySettled, err := boolInput(req.Inputs, "already_settled")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	reversalRequested, err := boolInput(req.Inputs, "reversal_requested")
	if err != nil {
		return simulate.DecisionResult{}, err
	}

	var route, detail string
	switch {
	case reversalRequested && alreadySettled:
		route = RouteReversalIntent
		detail = "a reversal was requested against a run that has already settled; this becomes a reversal/correction intent, not a mutation of the original run"
	case alreadySettled:
		route = RouteAlreadySettledInvalid
		detail = "the run has already settled and no reversal was requested"
	case duplicateRun:
		route = RouteDuplicateRunBlocked
		detail = "this run id has already been processed; a duplicate run is refused, not released again"
	case !cutoffActive:
		route = RouteStaleCutoffBlocked
		detail = "the population/cutoff snapshot is no longer active"
	default:
		route = RouteRoutineReleaseRequired
		detail = "no blocking condition; routine controller/finance/tax-compliance approvals are required"
	}
	return simulate.DecisionResult{
		RouteKey: route,
		TraceRef: RuleReleaseAndIntegrity + "#" + digest("trace", boolText(cutoffActive), boolText(duplicateRun), boolText(alreadySettled), boolText(reversalRequested))[:16],
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

// Transforms evaluates the two pure TRANSFORM nodes.
type Transforms struct{}

func (Transforms) Transform(_ context.Context, req simulate.TransformRequest) (simulate.TransformResult, error) {
	switch req.Transform.TransformRef {
	case TransformComputeCalculation:
		return transformComputeCalculation(req)
	case TransformBuildRunProposal:
		return transformBuildRunProposal(req)
	default:
		return simulate.TransformResult{}, fmt.Errorf("payroll: transform %s has no bound implementation", req.Transform.TransformRef)
	}
}

// flatTaxRate is a fixed, declared rate this fixture applies to gross pay.
// It exists only to prove the calculation is exact decimal arithmetic with
// no float rounding drift, never a real jurisdiction's tax table.
const flatTaxRate = "22"

// transformComputeCalculation computes tax and net pay from a Money gross
// input using exact decimal arithmetic (internal/kernel/values), never a
// float. CONF-009's "float rounding" RED case is what this proves against:
// the same gross input always produces the exact same net/tax amounts, byte
// for byte.
func transformComputeCalculation(req simulate.TransformRequest) (simulate.TransformResult, error) {
	grossV, err := req.Inputs.Get("gross_pay_input")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	gross, err := grossV.Money()
	if err != nil {
		return simulate.TransformResult{}, fmt.Errorf("payroll: gross_pay_input: %w", err)
	}
	watermark, err := req.Inputs.Text("population_watermark")
	if err != nil {
		return simulate.TransformResult{}, err
	}

	rate, err := values.NewPercentageFromPercent(flatTaxRate, 6, values.RoundingHalfEven)
	if err != nil {
		return simulate.TransformResult{}, fmt.Errorf("payroll: tax rate: %w", err)
	}
	tax, err := rate.ApplyTo(gross, gross.Amount().Scale(), values.RoundingHalfEven)
	if err != nil {
		return simulate.TransformResult{}, fmt.Errorf("payroll: apply tax rate: %w", err)
	}
	net, err := gross.Sub(tax)
	if err != nil {
		return simulate.TransformResult{}, fmt.Errorf("payroll: gross minus tax: %w", err)
	}

	trace := digest("hcmnext.workflow.conformance.payroll.CalculationTrace/v1", gross.String(), tax.String(), net.String(), watermark)
	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"net_pay_amount":           simulate.NewMoney(net),
			"tax_amount":               simulate.NewMoney(tax),
			"calculation_trace_digest": simulate.NewString(trace),
		},
		Detail: "computed net pay " + net.String() + " and tax " + tax.String() + " from gross " + gross.String() + " under trace " + trace,
	}, nil
}

func transformBuildRunProposal(req simulate.TransformRequest) (simulate.TransformResult, error) {
	runID, err := req.Inputs.Text("run_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	cutoff, err := req.Inputs.Text("cutoff_date")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	trace, err := req.Inputs.Text("calculation_trace_digest")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	watermark, err := req.Inputs.Text("population_watermark")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	pd := digest("hcmnext.workflow.conformance.payroll.RunProposal/v1", runID, cutoff, trace, watermark)
	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{"proposal_digest": simulate.NewString(pd)},
		Detail:  "bound payroll run proposal " + pd,
	}, nil
}

// Reads answers the provider-settlement OBSERVE node from the environment's
// declared observation outcome. [Environment.ObserveOutcome] controls it, so
// a caller can walk the consistent pending-obligations path, the rejected
// repair path or the distinct ambiguous repair path.
type Reads struct{ Env *Environment }

func (r Reads) Observe(_ context.Context, req simulate.ObservationRequest) (simulate.Observation, error) {
	outcome := r.Env.ObserveOutcome
	if outcome == "" {
		outcome = workflow.OutcomeUnknown
	}
	return simulate.Observation{
		Outcome:   outcome,
		Outputs:   simulate.Bag{"settlement_state": simulate.NewString(string(outcome))},
		Watermark: r.Env.ObserveWatermark,
		Detail:    "observed " + req.Observe.SourceAuthority + " for provider settlement: " + string(outcome),
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
			ExpressionDigest:  digest("hcmnext.workflow.conformance.payroll.ApprovalExpression/v1", ref),
			RequirementDigest: digest("hcmnext.workflow.conformance.payroll.ApprovalRequirement/v1", ref),
			Tier:              "STANDARD",
		})
	}
	return items, nil
}
