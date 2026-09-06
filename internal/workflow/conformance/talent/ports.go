package talent

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

// Decisions evaluates the calibration-and-correction DECISION node.
//
// It is the runtime half of CONF-012's RED and REFACTOR clauses at once: a
// correction against a prior assessment digest always becomes a new revision
// (never a mutation of the original, precedence 10, evaluated before every
// other question is even meaningful), a stale population is routed to an
// explicit unknown rather than proceeding, a rating author who is not the
// rater of record is blocked, and a contested calibration is blocked. Only
// once none of those hold is the calibration accepted as OK.
type Decisions struct{}

func (Decisions) Decide(_ context.Context, req simulate.DecisionRequest) (simulate.DecisionResult, error) {
	if req.Decision.RuleRef != RuleCalibrationAndCorrection {
		return simulate.DecisionResult{}, fmt.Errorf("talent: no evaluator is bound to rule %q", req.Decision.RuleRef)
	}
	populationActive, err := boolInput(req.Inputs, "population_active")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	authorityMismatch, err := boolInput(req.Inputs, "authority_mismatch")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	calibrationConflict, err := boolInput(req.Inputs, "calibration_conflict")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	isCorrection, err := boolInput(req.Inputs, "is_correction")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	priorDigest, err := req.Inputs.Text("prior_assessment_digest")
	if err != nil {
		return simulate.DecisionResult{}, err
	}

	var route, detail string
	switch {
	case isCorrection && priorDigest != "":
		route = RouteCorrectionRequiresNewRevision
		detail = "a correction was requested against prior assessment digest " + priorDigest + "; this becomes a new, distinct revision that references the prior digest, never a mutation of it"
	case !populationActive:
		route = RouteStalePopulationUnknown
		detail = "the population snapshot is not active for this worker/review cycle; a stale population is unknown, never a silent success"
	case authorityMismatch:
		route = RouteUnauthorizedRatingBlocked
		detail = "the rating author is not the rater of record; an unauthorized rating is blocked"
	case calibrationConflict:
		route = RouteCalibrationConflictBlocked
		detail = "the calibration committee outcome is CONTESTED; a contested calibration is blocked, never silently accepted as agreed"
	default:
		route = RouteCalibrationOK
		detail = "no blocking condition; the calibration is accepted and proceeds to record observation"
	}
	return simulate.DecisionResult{
		RouteKey: route,
		TraceRef: RuleCalibrationAndCorrection + "#" + digest("trace", boolText(populationActive), boolText(authorityMismatch), boolText(calibrationConflict), boolText(isCorrection), priorDigest)[:16],
		Detail:   detail,
	}, nil
}

// Transforms evaluates the two pure TRANSFORM nodes.
type Transforms struct{}

func (Transforms) Transform(_ context.Context, req simulate.TransformRequest) (simulate.TransformResult, error) {
	switch req.Transform.TransformRef {
	case TransformCalibrationFootprint:
		return transformCalibrationFootprint(req)
	case TransformBuildProposal:
		return transformBuildProposal(req)
	default:
		return simulate.TransformResult{}, fmt.Errorf("talent: transform %s has no bound implementation", req.Transform.TransformRef)
	}
}

// transformCalibrationFootprint reports authority mismatch and calibration
// conflict as plain booleans; it never itself decides that either one blocks
// the workflow. That is the calibration-and-correction DECISION's own job
// (see [Decisions]) - keeping the two apart is what makes
// TestTodo_CONF_012_Mutation's proof possible: a mutation that deleted the
// DECISION's own check could not hide behind this transform quietly blocking
// on its behalf, because this transform never blocks anything.
func transformCalibrationFootprint(req simulate.TransformRequest) (simulate.TransformResult, error) {
	populationActive, err := boolInput(req.Inputs, "population_active")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	ratingAuthorID, err := req.Inputs.Text("rating_author_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	raterID, err := req.Inputs.Text("rater_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	ratingValue, err := req.Inputs.Text("rating_value")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	calibrationAdjustedRating, err := req.Inputs.Text("calibration_adjusted_rating")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	calibrationStatus, err := req.Inputs.Text("calibration_status")
	if err != nil {
		return simulate.TransformResult{}, err
	}

	authorityMismatch := ratingAuthorID != raterID
	calibrationConflict := calibrationStatus == "CONTESTED"
	fp := digest("hcmnext.workflow.conformance.talent.Footprint/v1",
		boolText(populationActive), ratingAuthorID, raterID, ratingValue, calibrationAdjustedRating, calibrationStatus)

	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"authority_mismatch":   simulate.NewBool(authorityMismatch),
			"calibration_conflict": simulate.NewBool(calibrationConflict),
			"footprint_digest":     simulate.NewString(fp),
		},
		Detail: "computed calibration footprint " + fp,
	}, nil
}

// transformBuildProposal binds the model recommendation for reference (never
// for the DECISION's own inputs - see NodeBuildAssessmentProposal) and, when
// this is a correction, folds the prior assessment digest into the new
// proposal digest rather than discarding it: changing prior_assessment_digest
// always changes proposal_digest, which is the data-level proof that a
// correction references, never overwrites, the original assessment.
func transformBuildProposal(req simulate.TransformRequest) (simulate.TransformResult, error) {
	worker, err := req.Inputs.Text("worker_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	reviewCycle, err := req.Inputs.Text("review_cycle_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	footprint, err := req.Inputs.Text("footprint_digest")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	ratingValue, err := req.Inputs.Text("rating_value")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	calibrationAdjustedRating, err := req.Inputs.Text("calibration_adjusted_rating")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	inferenceRecommendation, err := req.Inputs.Text("inference_recommendation")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	effective, err := req.Inputs.Text("effective_date")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	isCorrection, err := boolInput(req.Inputs, "is_correction")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	priorDigest, err := req.Inputs.Text("prior_assessment_digest")
	if err != nil {
		return simulate.TransformResult{}, err
	}

	lineage := "original"
	if isCorrection && priorDigest != "" {
		lineage = "correction-of:" + priorDigest
	}
	pd := digest("hcmnext.workflow.conformance.talent.Proposal/v1",
		worker, reviewCycle, footprint, ratingValue, calibrationAdjustedRating, inferenceRecommendation, effective, lineage)

	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{"proposal_digest": simulate.NewString(pd)},
		Detail:  "bound talent assessment proposal " + pd,
	}, nil
}

// Reads answers the post-decision calibration-record OBSERVE node from the
// environment's declared observation outcome. A caller controls it through
// [Environment.ObserveOutcome] to walk either the consistent
// pending-approvals path or the bounded repair path.
type Reads struct{ Env *Environment }

func (r Reads) Observe(_ context.Context, req simulate.ObservationRequest) (simulate.Observation, error) {
	outcome := r.Env.ObserveOutcome
	if outcome == "" {
		outcome = workflow.OutcomeUnknown
	}
	return simulate.Observation{
		Outcome:   outcome,
		Outputs:   simulate.Bag{"calibration_record_state": simulate.NewString(string(outcome))},
		Watermark: r.Env.ObserveWatermark,
		Detail:    "observed " + req.Observe.SourceAuthority + " for calibration record: " + string(outcome),
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
			ExpressionDigest:  digest("hcmnext.workflow.conformance.talent.ApprovalExpression/v1", ref),
			RequirementDigest: digest("hcmnext.workflow.conformance.talent.ApprovalRequirement/v1", ref),
			Tier:              "STANDARD",
		})
	}
	return items, nil
}
