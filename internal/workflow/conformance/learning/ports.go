package learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

// digest is a small, deterministic content identity for this fixture's own
// transform outputs. It is not the interpreter's canonical digest (that
// stays internal to package simulate); it only has to be stable across runs
// of the same inputs, which sha256 over a fixed field order gives for free.
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

func boolInput(b simulate.Bag, path string) (bool, error) {
	v, err := b.Get(path)
	if err != nil {
		return false, err
	}
	return v.Bool()
}

// Decisions evaluates the satisfaction DECISION node.
//
// It is the runtime half of the REFACTOR clause and the RED cases CONF-013
// names: duplicate enrollment blocks ahead of every other check, a waiver's
// validity (not merely its presence) decides exemption, a self-reported
// claim never alone satisfies the requirement, and expiry/expiring-soon are
// only meaningful for verified-credential evidence. Precedence matches the
// routes declared on the compiled node.
type Decisions struct{}

func (Decisions) Decide(_ context.Context, req simulate.DecisionRequest) (simulate.DecisionResult, error) {
	if req.Decision.RuleRef != RuleSatisfactionDecision {
		return simulate.DecisionResult{}, fmt.Errorf("learning: no evaluator is bound to rule %q", req.Decision.RuleRef)
	}
	// requirement_active is read (and folded into the trace digest) even
	// though this DECISION does not itself branch on it: whether the
	// requirement is currently active is already baked into
	// evidence_expired/evidence_expiring_soon by the upstream
	// compute_evidence_footprint TRANSFORM (an inactive requirement never
	// reports an expiry concern), so reading it twice for the same branch
	// would make one of the two checks redundant rather than independently
	// load-bearing.
	requirementActive, err := boolInput(req.Inputs, "requirement_active")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	duplicateEnrollment, err := boolInput(req.Inputs, "duplicate_enrollment_detected")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	evidencePresent, err := boolInput(req.Inputs, "evidence_present")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	evidenceExpired, err := boolInput(req.Inputs, "evidence_expired")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	evidenceExpiringSoon, err := boolInput(req.Inputs, "evidence_expiring_soon")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	evidenceKind, err := req.Inputs.Text("evidence_kind")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	waiverPresent, err := boolInput(req.Inputs, "waiver_present")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	waiverValid, err := boolInput(req.Inputs, "waiver_valid")
	if err != nil {
		return simulate.DecisionResult{}, err
	}

	var route, detail string
	switch {
	case duplicateEnrollment:
		route = RouteDuplicateEnrollmentBlocked
		detail = "a duplicate enrollment was detected; this blocks ahead of any evidence or waiver check"
	case waiverPresent && waiverValid:
		route = RouteExemptValidWaiver
		detail = "a present, valid waiver exempts the requirement regardless of evidence state"
	case waiverPresent && !waiverValid:
		route = RouteUnsatisfiedInvalidWaiver
		detail = "a waiver is present but fails authority validation; an invalid waiver is never a silent exemption"
	case evidencePresent && evidenceKind == EvidenceKindSelfClaim:
		route = RouteUnsatisfiedUnverifiedClaim
		detail = "evidence is a self-reported claim, not a verified credential; a claim never alone satisfies the requirement"
	case evidencePresent && evidenceKind == EvidenceKindVerifiedCredential && evidenceExpired:
		route = RouteUnsatisfiedExpired
		detail = "verified-credential evidence has expired as of the effective date"
	case evidencePresent && evidenceKind == EvidenceKindVerifiedCredential && !evidenceExpired && evidenceExpiringSoon:
		route = RouteExpiringSatisfaction
		detail = "verified-credential evidence is valid but expires within the declared reminder window"
	case !evidencePresent:
		route = RouteUnsatisfiedNoEvidence
		detail = "no accessible evidence was found; an inaccessible document resolves the same as no evidence"
	default:
		route = RouteSatisfiedPendingReconciliation
		detail = "verified-credential evidence is present and not expired or expiring; satisfaction is pending provider reconciliation"
	}
	return simulate.DecisionResult{
		RouteKey: route,
		TraceRef: RuleSatisfactionDecision + "#" + digest("trace",
			boolText(requirementActive), boolText(duplicateEnrollment), boolText(evidencePresent),
			boolText(evidenceExpired), boolText(evidenceExpiringSoon), evidenceKind,
			boolText(waiverPresent), boolText(waiverValid))[:16],
		Detail: detail,
	}, nil
}

// Transforms evaluates the two pure TRANSFORM nodes.
type Transforms struct{}

func (Transforms) Transform(_ context.Context, req simulate.TransformRequest) (simulate.TransformResult, error) {
	switch req.Transform.TransformRef {
	case TransformEvidenceFootprint:
		return transformEvidenceFootprint(req)
	case TransformBuildProposal:
		return transformBuildProposal(req)
	default:
		return simulate.TransformResult{}, fmt.Errorf("learning: transform %s has no bound implementation", req.Transform.TransformRef)
	}
}

// transformEvidenceFootprint computes evidence_expired and
// evidence_expiring_soon once, from the evidence's declared expiry date, the
// pinned effective date and the requirement's active state.
//
// It deliberately never looks at whether evidence_kind is SELF_CLAIM or
// VERIFIED_CREDENTIAL to decide satisfaction: it only reports the expiry
// footprint. Whether an unverified claim is acceptable at all is the
// satisfaction DECISION's own job (see [Decisions]), which is what makes the
// REFACTOR clause's route independently load-bearing rather than an
// accident of this transform (proved directly by
// TestTodo_CONF_013_Mutation).
func transformEvidenceFootprint(req simulate.TransformRequest) (simulate.TransformResult, error) {
	evidencePresent, err := boolInput(req.Inputs, "evidence_present")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	requirementActive, err := boolInput(req.Inputs, "requirement_active")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	evidenceKind, err := req.Inputs.Text("evidence_kind")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	expiryV, err := req.Inputs.Get("evidence_expiry_date")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	expiry, err := expiryV.LocalDate()
	if err != nil {
		return simulate.TransformResult{}, err
	}
	effectiveV, err := req.Inputs.Get("effective_date")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	effective, err := effectiveV.LocalDate()
	if err != nil {
		return simulate.TransformResult{}, err
	}

	var expired, expiringSoon bool
	// Expiry only means anything for currently-active requirements backed
	// by verified-credential evidence: an inactive requirement or
	// self-reported claim never creates an expiry concern (whether the
	// claim itself is acceptable is the DECISION's job, not this
	// TRANSFORM's).
	if requirementActive && evidencePresent && evidenceKind == EvidenceKindVerifiedCredential {
		switch {
		case expiry.Compare(effective) < 0:
			expired = true
		case expiry.Compare(effective.AddDays(expiringWindowDays)) <= 0:
			expiringSoon = true
		}
	}

	fp := digest("hcmnext.workflow.conformance.learning.Footprint/v1",
		boolText(evidencePresent), boolText(requirementActive), evidenceKind, expiry.String(), effective.String())

	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"evidence_expired":       simulate.NewBool(expired),
			"evidence_expiring_soon": simulate.NewBool(expiringSoon),
			"footprint_digest":       simulate.NewString(fp),
		},
		Detail: "computed evidence footprint " + fp,
	}, nil
}

func transformBuildProposal(req simulate.TransformRequest) (simulate.TransformResult, error) {
	worker, err := req.Inputs.Text("worker_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	requirement, err := req.Inputs.Text("requirement_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	footprint, err := req.Inputs.Text("footprint_digest")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	waiverValid, err := boolInput(req.Inputs, "waiver_valid")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	effective, err := req.Inputs.Text("effective_date")
	if err != nil {
		return simulate.TransformResult{}, err
	}

	pd := digest("hcmnext.workflow.conformance.learning.Proposal/v1", worker, requirement, footprint, boolText(waiverValid), effective)
	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{"proposal_digest": simulate.NewString(pd)},
		Detail:  "bound learning satisfaction proposal " + pd,
	}, nil
}

// Reads answers the post-decision provider-reconciliation OBSERVE nodes
// (both the satisfied and expiring pathways) from the environment's
// declared observation outcome. [Environment.ObserveOutcome] controls it, so
// a caller can walk the consistent completion, degraded-repair or unknown
// path.
type Reads struct{ Env *Environment }

func (r Reads) Observe(_ context.Context, req simulate.ObservationRequest) (simulate.Observation, error) {
	outcome := r.Env.ObserveOutcome
	if outcome == "" {
		outcome = workflow.OutcomeUnknown
	}
	return simulate.Observation{
		Outcome:   outcome,
		Outputs:   simulate.Bag{"reconciliation_state": simulate.NewString(string(outcome))},
		Watermark: r.Env.ObserveWatermark,
		Detail:    "observed " + req.Observe.SourceAuthority + " for provider reconciliation: " + string(outcome),
	}, nil
}

// Approvals derives the constant approval graph the reference workflow's
// pending terminals declare, without waiting for any of them. Every
// declared requirement ref gets exactly one work item; there is no silent
// narrowing.
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
			ExpressionDigest:  digest("hcmnext.workflow.conformance.learning.ApprovalExpression/v1", ref),
			RequirementDigest: digest("hcmnext.workflow.conformance.learning.ApprovalRequirement/v1", ref),
			Tier:              "STANDARD",
		})
	}
	return items, nil
}
