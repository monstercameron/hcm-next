package time

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

// Decisions evaluates the punch-classification DECISION node.
//
// It is the runtime proof of every RED case CONF-011 names: a duplicate
// punch, a post-lock edit and a stale reopened timecard are each refused
// with a distinct route ahead of any merely-reviewable concern, and a
// shared-device spoof suspicion, an offline replay, a DST-fold ambiguity
// and an out-of-tolerance clock skew each route to their own REVIEW
// terminal rather than being silently accepted or folded into one generic
// "needs review". Precedence matches the routes declared on the compiled
// node exactly.
type Decisions struct{}

func (Decisions) Decide(_ context.Context, req simulate.DecisionRequest) (simulate.DecisionResult, error) {
	if req.Decision.RuleRef != RuleClassifyPunchIntegrity {
		return simulate.DecisionResult{}, fmt.Errorf("time: no evaluator is bound to rule %q", req.Decision.RuleRef)
	}
	duplicate, err := boolInput(req.Inputs, "duplicate_punch_detected")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	postLock, err := boolInput(req.Inputs, "post_lock_edit_attempted")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	staleReopen, err := boolInput(req.Inputs, "timecard_reopened_stale")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	spoof, err := boolInput(req.Inputs, "shared_device_spoof_suspected")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	offlineReplay, err := boolInput(req.Inputs, "offline_replay_detected")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	dstFold, err := boolInput(req.Inputs, "dst_fold_ambiguous")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	skewWithinTolerance, err := boolInput(req.Inputs, "clock_skew_within_tolerance")
	if err != nil {
		return simulate.DecisionResult{}, err
	}

	var route, detail string
	switch {
	case duplicate:
		route = RouteDuplicatePunch
		detail = "this punch duplicates one already recorded; a duplicate punch is refused ahead of every other condition"
	case postLock:
		route = RouteRejectedPostLockEdit
		detail = "an edit was attempted after the timecard locked; a post-lock edit is rejected, not silently applied"
	case staleReopen:
		route = RouteRejectedStaleReopen
		detail = "the timecard was reopened after it had already gone stale relative to the payroll cutoff"
	case spoof:
		route = RouteReviewSpoofSuspected
		detail = "a shared-device spoof is suspected; this requires review, neither silent acceptance nor outright rejection"
	case offlineReplay:
		route = RouteReviewOfflineReplay
		detail = "this punch was replayed from an offline buffer; it requires review before it is trusted"
	case dstFold:
		route = RouteReviewDSTFold
		detail = "the local timestamp falls in a DST fold and is ambiguous as to which instant is meant; it requires review"
	case !skewWithinTolerance:
		route = RouteReviewClockSkew
		detail = "the reported clock skew exceeds the configured tolerance; it requires review"
	default:
		route = RouteAccepted
		detail = "no blocking or reviewable condition; the punch is accepted and proceeds to payroll-bridge ingestion"
	}
	return simulate.DecisionResult{
		RouteKey: route,
		TraceRef: RuleClassifyPunchIntegrity + "#" + digest("trace",
			boolText(duplicate), boolText(postLock), boolText(staleReopen),
			boolText(spoof), boolText(offlineReplay), boolText(dstFold), boolText(skewWithinTolerance))[:16],
		Detail: detail,
	}, nil
}

func boolInput(b simulate.Bag, path string) (bool, error) {
	v, err := b.Get(path)
	if err != nil {
		return false, err
	}
	return v.Bool()
}

// Transforms evaluates the one pure TRANSFORM node.
type Transforms struct{}

func (Transforms) Transform(_ context.Context, req simulate.TransformRequest) (simulate.TransformResult, error) {
	switch req.Transform.TransformRef {
	case TransformBuildPunchProposal:
		return transformBuildPunchProposal(req)
	default:
		return simulate.TransformResult{}, fmt.Errorf("time: transform %s has no bound implementation", req.Transform.TransformRef)
	}
}

// transformBuildPunchProposal binds the punch to its timezone and tzdb
// version pin. Pinning tzdb_version alongside timezone_id is what makes a
// later tzdb update provably not retroactively reinterpret this punch's
// instant, which is the runtime content of CONF-011's "preserves
// timezone/tzdb" GREEN clause.
func transformBuildPunchProposal(req simulate.TransformRequest) (simulate.TransformResult, error) {
	punchID, err := req.Inputs.Text("punch_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	timezoneID, err := req.Inputs.Text("timezone_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	tzdbVersion, err := req.Inputs.Text("tzdb_version")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	pd := digest("hcmnext.workflow.conformance.time.PunchProposal/v1", punchID, timezoneID, tzdbVersion)
	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{"proposal_digest": simulate.NewString(pd)},
		Detail:  "bound punch proposal " + pd + " to timezone " + timezoneID + " under " + tzdbVersion,
	}, nil
}

// Reads answers the payroll-bridge ingestion OBSERVE node from the
// environment's declared observation outcome. [Environment.ObserveOutcome]
// controls it, so a caller can walk the consistent bridge-confirmed path or
// the bounded repair path independently of how the punch itself classified.
type Reads struct{ Env *Environment }

func (r Reads) Observe(_ context.Context, req simulate.ObservationRequest) (simulate.Observation, error) {
	outcome := r.Env.ObserveOutcome
	if outcome == "" {
		outcome = workflow.OutcomeUnknown
	}
	return simulate.Observation{
		Outcome:   outcome,
		Outputs:   simulate.Bag{"bridge_state": simulate.NewString(string(outcome))},
		Watermark: r.Env.ObserveWatermark,
		Detail:    "observed " + req.Observe.SourceAuthority + " for payroll-bridge ingestion: " + string(outcome),
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
			ExpressionDigest:  digest("hcmnext.workflow.conformance.time.ApprovalExpression/v1", ref),
			RequirementDigest: digest("hcmnext.workflow.conformance.time.ApprovalRequirement/v1", ref),
			Tier:              "STANDARD",
		})
	}
	return items, nil
}
