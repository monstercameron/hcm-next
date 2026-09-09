package mobility

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

func digest(profile string, parts ...string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(profile))
	_, _ = h.Write([]byte{0})
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))[:32]
}
func boolInput(b simulate.Bag, path string) (bool, error) {
	v, err := b.Get(path)
	if err != nil {
		return false, err
	}
	return v.Bool()
}

// Decisions is the pure evaluator for the mobility readiness gate. Vendor
// results are deliberately absent: immigration-provider status is observed
// only after this decision and can never authorize the move.
type Decisions struct{}

func (Decisions) Decide(_ context.Context, req simulate.DecisionRequest) (simulate.DecisionResult, error) {
	if req.Decision.RuleRef != RuleMobilityDecision {
		return simulate.DecisionResult{}, fmt.Errorf("mobility: no evaluator is bound to rule %q", req.Decision.RuleRef)
	}
	mobilityActive, err := boolInput(req.Inputs, "mobility_active")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	authorizationBlocked, err := boolInput(req.Inputs, "authorization_blocked")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	milestoneIncomplete, err := boolInput(req.Inputs, "authorization_milestone_incomplete")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	authorizationExpiring, err := boolInput(req.Inputs, "authorization_expiring")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	overlap, err := boolInput(req.Inputs, "overlapping_assignment")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	privacyAllowed, err := boolInput(req.Inputs, "privacy_transfer_allowed")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	taxUnknown, err := boolInput(req.Inputs, "tax_impact_unknown")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	peUnknown, err := boolInput(req.Inputs, "pe_impact_unknown")
	if err != nil {
		return simulate.DecisionResult{}, err
	}

	route, detail := RouteMobilityReady, "dated legs and home/host payroll facts are ready; immigration remains an observation"
	switch {
	case !mobilityActive || authorizationBlocked:
		route, detail = RouteAuthorizationBlocked, "work authorization is invalid or the mobility assignment is inactive"
	case milestoneIncomplete:
		route, detail = RouteAuthorizationMilestoneBlocked, "a required authorization milestone is incomplete"
	case overlap:
		route, detail = RouteOverlappingLegBlocked, "overlapping dated mobility legs are blocked rather than silently merged"
	case !privacyAllowed:
		route, detail = RoutePrivacyTransferBlocked, "the cross-border privacy transfer has no permitted mechanism"
	case taxUnknown || peUnknown:
		route, detail = RouteTaxPEUnknown, "tax or permanent-establishment impact is unknown; execution needs bounded review"
	case authorizationExpiring:
		route, detail = RouteAuthorizationExpiring, "authorization is valid but expiring; renewal review remains open"
	}
	return simulate.DecisionResult{RouteKey: route, TraceRef: RuleMobilityDecision + "#" + digest("trace", boolText(mobilityActive), boolText(authorizationBlocked), boolText(milestoneIncomplete), boolText(authorizationExpiring), boolText(overlap), boolText(privacyAllowed), boolText(taxUnknown), boolText(peUnknown))[:16], Detail: detail}, nil
}

type Transforms struct{}

func (Transforms) Transform(_ context.Context, req simulate.TransformRequest) (simulate.TransformResult, error) {
	switch req.Transform.TransformRef {
	case TransformMobilityFootprint:
		return transformFootprint(req)
	case TransformBuildProposal:
		return transformProposal(req)
	default:
		return simulate.TransformResult{}, fmt.Errorf("mobility: transform %s has no bound implementation", req.Transform.TransformRef)
	}
}
func transformFootprint(req simulate.TransformRequest) (simulate.TransformResult, error) {
	active, err := boolInput(req.Inputs, "mobility_active")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	overlap, err := boolInput(req.Inputs, "overlap_detected")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	valid, err := boolInput(req.Inputs, "authorization_valid")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	milestone, err := boolInput(req.Inputs, "authorization_milestone_complete")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	source, err := req.Inputs.Text("source_jurisdiction")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	destination, err := req.Inputs.Text("destination_jurisdiction")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	homePayroll, err := req.Inputs.Text("home_payroll_group")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	hostPayroll, err := req.Inputs.Text("host_payroll_group")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	model, err := req.Inputs.Text("payroll_model")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	start, err := req.Inputs.Get("leg_start")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	end, err := req.Inputs.Get("leg_end")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	startDate, err := start.LocalDate()
	if err != nil {
		return simulate.TransformResult{}, err
	}
	endDate, err := end.LocalDate()
	if err != nil {
		return simulate.TransformResult{}, err
	}
	authorizationBlocked := !active || !valid
	milestoneBlocked := !milestone
	fp := digest("hcmnext.workflow.conformance.mobility.Footprint/v1", boolText(active), boolText(overlap), source, destination, homePayroll, hostPayroll, model, startDate.String(), endDate.String(), boolText(valid), boolText(milestone))
	return simulate.TransformResult{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{"overlapping_assignment": simulate.NewBool(overlap), "authorization_blocked": simulate.NewBool(authorizationBlocked), "authorization_milestone_incomplete": simulate.NewBool(milestoneBlocked), "payroll_cross_border": simulate.NewBool(source != destination || homePayroll != hostPayroll), "footprint_digest": simulate.NewString(fp)}, Detail: "computed dated mobility footprint " + fp}, nil
}
func transformProposal(req simulate.TransformRequest) (simulate.TransformResult, error) {
	worker, err := req.Inputs.Text("worker_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	mobilityID, err := req.Inputs.Text("mobility_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	footprint, err := req.Inputs.Text("footprint_digest")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	homePayroll, err := req.Inputs.Text("home_payroll_group")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	hostPayroll, err := req.Inputs.Text("host_payroll_group")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	model, err := req.Inputs.Text("payroll_model")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	effective, err := req.Inputs.Text("effective_date")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	pd := digest("hcmnext.workflow.conformance.mobility.Proposal/v1", worker, mobilityID, footprint, homePayroll, hostPayroll, model, effective)
	return simulate.TransformResult{Outcome: workflow.OutcomeSucceeded, Outputs: simulate.Bag{"proposal_digest": simulate.NewString(pd)}, Detail: "bound mobility proposal " + pd}, nil
}

// Reads keeps the immigration vendor response observational and carries its
// watermark into the simulated receipt.
type Reads struct{ Env *Environment }

func (r Reads) Observe(_ context.Context, req simulate.ObservationRequest) (simulate.Observation, error) {
	outcome := r.Env.ObserveOutcome
	if outcome == "" {
		outcome = workflow.OutcomeUnknown
	}
	return simulate.Observation{Outcome: outcome, Outputs: simulate.Bag{"vendor_state": simulate.NewString(string(outcome))}, Watermark: r.Env.ObserveWatermark, Detail: "observed " + req.Observe.SourceAuthority + ": " + string(outcome)}, nil
}

type Approvals struct{}

func (Approvals) WouldAwait(_ context.Context, req simulate.ApprovalRequest) ([]simulate.WorkItem, error) {
	refs := append([]string(nil), req.RequirementRefs...)
	sort.Strings(refs)
	items := make([]simulate.WorkItem, 0, len(refs))
	for i, ref := range refs {
		items = append(items, simulate.WorkItem{NodeID: req.NodeID, Kind: "APPROVAL", State: simulate.WouldAwait, RequirementID: ref, Stage: uint32(i + 1), QuorumMin: 1, Outcome: "RESOLVED", Candidates: []string{"principal:" + ref + "-approver-1 via ROLE"}, ExpressionDigest: digest("hcmnext.workflow.conformance.mobility.ApprovalExpression/v1", ref), RequirementDigest: digest("hcmnext.workflow.conformance.mobility.ApprovalRequirement/v1", ref), Tier: "STANDARD"})
	}
	return items, nil
}
