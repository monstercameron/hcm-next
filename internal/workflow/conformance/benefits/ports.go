package benefits

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

// Decisions evaluates the benefits eligibility DECISION node.
//
// It is the runtime proof of CONF-010's RED clauses: a disputed fact, an
// expired enrollment window, a duplicate life event or an overlapping
// election are each refused with their own distinct route rather than
// collapsed into one generic rejection or, worse, silently approved.
// Precedence matches the routes declared on the compiled node.
type Decisions struct{}

func (Decisions) Decide(_ context.Context, req simulate.DecisionRequest) (simulate.DecisionResult, error) {
	if req.Decision.RuleRef != RuleEligibilityAndElection {
		return simulate.DecisionResult{}, fmt.Errorf("benefits: no evaluator is bound to rule %q", req.Decision.RuleRef)
	}
	disputed, err := boolInput(req.Inputs, "fact_disputed")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	expired, err := boolInput(req.Inputs, "enrollment_window_expired")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	duplicate, err := boolInput(req.Inputs, "life_event_duplicate")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	overlap, err := boolInput(req.Inputs, "election_overlap_detected")
	if err != nil {
		return simulate.DecisionResult{}, err
	}

	var route, detail string
	switch {
	case disputed:
		route = RouteDisputedFactBlocked
		detail = "an eligibility fact is disputed; a disputed fact never becomes a resolved eligibility"
	case expired:
		route = RouteExpiredWindowBlocked
		detail = "the enrollment window has already expired"
	case duplicate:
		route = RouteDuplicateLifeEventBlocked
		detail = "this life event has already been processed once; a duplicate life event is refused, not processed again"
	case overlap:
		route = RouteOverlappingElectionBlocked
		detail = "the proposed election overlaps an existing election"
	default:
		route = RouteRoutineApprovalRequired
		detail = "no blocking condition; routine benefits-admin/HR-partner/payroll-deduction approvals are required"
	}
	return simulate.DecisionResult{
		RouteKey: route,
		TraceRef: RuleEligibilityAndElection + "#" + digest("trace", boolText(disputed), boolText(expired), boolText(duplicate), boolText(overlap))[:16],
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
	case TransformBuildEligibilityTrace:
		return transformBuildEligibilityTrace(req)
	case TransformBuildElectionProposal:
		return transformBuildElectionProposal(req)
	default:
		return simulate.TransformResult{}, fmt.Errorf("benefits: transform %s has no bound implementation", req.Transform.TransformRef)
	}
}

func transformBuildEligibilityTrace(req simulate.TransformRequest) (simulate.TransformResult, error) {
	worker, err := req.Inputs.Text("worker_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	plan, err := req.Inputs.Text("plan_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	watermark, err := req.Inputs.Text("eligibility_watermark")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	trace := digest("hcmnext.workflow.conformance.benefits.EligibilityTrace/v1", worker, plan, watermark)
	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{"eligibility_trace_digest": simulate.NewString(trace)},
		Detail:  "built eligibility trace " + trace,
	}, nil
}

// transformBuildElectionProposal builds the election's revision digest.
//
// The prior election digest is folded into the new digest so that this
// election is provably a new revision naming its predecessor - never the
// same content overwriting the prior election in place, which is what
// CONF-010's "immutable election revisions" GREEN clause requires: an empty
// prior digest and a non-empty one always produce different election
// digests for otherwise identical facts.
func transformBuildElectionProposal(req simulate.TransformRequest) (simulate.TransformResult, error) {
	worker, err := req.Inputs.Text("worker_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	plan, err := req.Inputs.Text("plan_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	prior, err := req.Inputs.Text("prior_election_digest")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	trace, err := req.Inputs.Text("eligibility_trace_digest")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	effective, err := req.Inputs.Text("effective_date")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	ed := digest("hcmnext.workflow.conformance.benefits.ElectionRevision/v1", worker, plan, prior, trace, effective)
	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{"election_digest": simulate.NewString(ed)},
		Detail:  "built election revision " + ed + " superseding prior " + priorLabel(prior),
	}, nil
}

func priorLabel(prior string) string {
	if prior == "" {
		return "<none>"
	}
	return prior
}

// Reads answers both reconciliation OBSERVE nodes from the environment's
// independently declared outcomes. [Environment.CarrierOutcome] and
// [Environment.DeductionOutcome] are controlled separately, so a caller can
// prove one reconciliation degrading never affects the other's answer.
type Reads struct{ Env *Environment }

func (r Reads) Observe(_ context.Context, req simulate.ObservationRequest) (simulate.Observation, error) {
	switch req.NodeID {
	case NodeObserveCarrierReconciliation:
		outcome := r.Env.CarrierOutcome
		if outcome == "" {
			outcome = workflow.OutcomeUnknown
		}
		return simulate.Observation{
			Outcome:   outcome,
			Outputs:   simulate.Bag{"carrier_state": simulate.NewString(string(outcome))},
			Watermark: r.Env.CarrierWatermark,
			Detail:    "observed " + req.Observe.SourceAuthority + " for carrier reconciliation: " + string(outcome),
		}, nil
	case NodeObserveDeductionReconciliation:
		outcome := r.Env.DeductionOutcome
		if outcome == "" {
			outcome = workflow.OutcomeUnknown
		}
		return simulate.Observation{
			Outcome:   outcome,
			Outputs:   simulate.Bag{"deduction_state": simulate.NewString(string(outcome))},
			Watermark: r.Env.DeductionWatermark,
			Detail:    "observed " + req.Observe.SourceAuthority + " for payroll deduction reconciliation: " + string(outcome),
		}, nil
	default:
		return simulate.Observation{}, fmt.Errorf("benefits: no read is bound to observe node %q", req.NodeID)
	}
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
			ExpressionDigest:  digest("hcmnext.workflow.conformance.benefits.ApprovalExpression/v1", ref),
			RequirementDigest: digest("hcmnext.workflow.conformance.benefits.ApprovalRequirement/v1", ref),
			Tier:              "STANDARD",
		})
	}
	return items, nil
}
