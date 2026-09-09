package hrcase

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

// Decisions evaluates the case-disposition DECISION node.
//
// It is the runtime proof of every RED clause CONF-014 names at once: an
// unauthorized participant, an investigator investigating themselves, an
// active matter wall, a racing legal hold and an already-disposed case are
// all blocked rather than resolved, a detected retaliation signal is routed
// to a mandatory escalation rather than silently folded into routine
// disposition, and an appeal against an already-disposed case takes a
// distinct reopen route rather than mutating the original disposition (the
// REFACTOR clause). Precedence matches the routes declared on the compiled
// node: the appeal/already-disposed questions are evaluated first, because
// none of the other questions is meaningful once the case is no longer
// active for a routine disposition.
type Decisions struct{}

func (Decisions) Decide(_ context.Context, req simulate.DecisionRequest) (simulate.DecisionResult, error) {
	if req.Decision.RuleRef != RuleCaseDisposition {
		return simulate.DecisionResult{}, fmt.Errorf("hrcase: no evaluator is bound to rule %q", req.Decision.RuleRef)
	}
	caseActive, err := boolInput(req.Inputs, "case_active")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	participantAuthorized, err := boolInput(req.Inputs, "participant_authorized")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	investigatorConflict, err := boolInput(req.Inputs, "investigator_conflict")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	matterWallActive, err := boolInput(req.Inputs, "matter_wall_active")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	legalHoldActive, err := boolInput(req.Inputs, "legal_hold_active")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	retaliationSignalDetected, err := boolInput(req.Inputs, "retaliation_signal_detected")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	appealRequested, err := boolInput(req.Inputs, "appeal_requested")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	caseAlreadyDisposed, err := boolInput(req.Inputs, "case_already_disposed")
	if err != nil {
		return simulate.DecisionResult{}, err
	}

	// A case that is no longer active or is explicitly flagged as disposed is
	// the same "already closed" condition either way; only whether an appeal
	// was requested distinguishes a reopen intent from an invalid request.
	closed := !caseActive || caseAlreadyDisposed

	var route, detail string
	switch {
	case appealRequested && closed:
		route = RouteAppealRequiresReopen
		detail = "an appeal was requested against a case that is already disposed; this becomes a reopen intent, not a mutation of the original disposition"
	case closed:
		route = RouteCaseAlreadyDisposedInvalid
		detail = "the case is already disposed and no appeal was requested"
	case !participantAuthorized:
		route = RouteUnauthorizedParticipant
		detail = "the requesting participant failed the purpose-scoped case-access authorization read"
	case investigatorConflict:
		route = RouteInvestigatorConflictBlocked
		detail = "the investigator is the case subject; an investigator may never investigate themselves"
	case matterWallActive:
		route = RouteMatterWallBreachBlocked
		detail = "an active matter wall blocks disposition"
	case legalHoldActive:
		route = RouteLegalHoldRaceBlocked
		detail = "an active legal hold contends with disposition and blocks it"
	case retaliationSignalDetected:
		route = RouteRetaliationEscalation
		detail = "a retaliation signal was detected; disposition requires a mandatory Employee Relations escalation review"
	default:
		route = RouteRoutineDispositionRequired
		detail = "no blocking condition; routine HR case manager/Employee Relations/legal approvals are required"
	}
	return simulate.DecisionResult{
		RouteKey: route,
		TraceRef: RuleCaseDisposition + "#" + digest("trace",
			boolText(caseActive), boolText(participantAuthorized), boolText(investigatorConflict),
			boolText(matterWallActive), boolText(legalHoldActive), boolText(retaliationSignalDetected),
			boolText(appealRequested), boolText(caseAlreadyDisposed))[:16],
		Detail: detail,
	}, nil
}

// Transforms evaluates the two pure TRANSFORM nodes.
type Transforms struct{}

func (Transforms) Transform(_ context.Context, req simulate.TransformRequest) (simulate.TransformResult, error) {
	switch req.Transform.TransformRef {
	case TransformCaseFootprint:
		return transformCaseFootprint(req)
	case TransformBuildProposal:
		return transformBuildProposal(req)
	default:
		return simulate.TransformResult{}, fmt.Errorf("hrcase: transform %s has no bound implementation", req.Transform.TransformRef)
	}
}

// transformCaseFootprint computes the investigator-conflict flag and a
// deterministic footprint digest. The investigator-equals-subject check lives
// here, not in the DECISION: the DECISION only routes off this already-
// computed boolean, exactly as the reference design assigns it.
func transformCaseFootprint(req simulate.TransformRequest) (simulate.TransformResult, error) {
	investigatorScope, err := req.Inputs.Text("investigator_authority_scope")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	subjectScope, err := req.Inputs.Text("subject_relationship_scope")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	caseActiveV, err := req.Inputs.Get("case_active")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	caseActive, err := caseActiveV.Bool()
	if err != nil {
		return simulate.TransformResult{}, err
	}
	investigatorID, err := req.Inputs.Text("investigator_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	subjectWorkerID, err := req.Inputs.Text("subject_worker_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}

	conflict := investigatorID != "" && investigatorID == subjectWorkerID
	fp := digest("hcmnext.workflow.conformance.hrcase.Footprint/v1",
		investigatorScope, subjectScope, boolText(caseActive), investigatorID, subjectWorkerID)

	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"investigator_conflict": simulate.NewBool(conflict),
			"footprint_digest":      simulate.NewString(fp),
		},
		Detail: "computed case footprint " + fp,
	}, nil
}

func transformBuildProposal(req simulate.TransformRequest) (simulate.TransformResult, error) {
	caseID, err := req.Inputs.Text("case_id")
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
	finding, err := req.Inputs.Text("finding_summary")
	if err != nil {
		return simulate.TransformResult{}, err
	}

	pd := digest("hcmnext.workflow.conformance.hrcase.Proposal/v1", caseID, footprint, effective, finding)
	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{"proposal_digest": simulate.NewString(pd)},
		Detail:  "bound hr case disposition proposal " + pd,
	}, nil
}

// Reads answers the post-decision record-custody OBSERVE nodes from the
// environment's declared observation outcome. Both [NodeObserveRecordCustody]
// and [NodeObserveRecordCustodyEscalat] are served by the same port: a
// caller controls the outcome through [Environment.ObserveOutcome] to walk
// either the consistent pending path or the bounded repair path, regardless
// of which route reached the observation.
type Reads struct{ Env *Environment }

func (r Reads) Observe(_ context.Context, req simulate.ObservationRequest) (simulate.Observation, error) {
	outcome := r.Env.ObserveOutcome
	if outcome == "" {
		outcome = workflow.OutcomeUnknown
	}
	detail := "observed " + req.Observe.SourceAuthority + " for case record custody: " + string(outcome)
	return simulate.Observation{
		Outcome:   outcome,
		Outputs:   simulate.Bag{"custody_state": simulate.NewString(string(outcome))},
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
			ExpressionDigest:  digest("hcmnext.workflow.conformance.hrcase.ApprovalExpression/v1", ref),
			RequirementDigest: digest("hcmnext.workflow.conformance.hrcase.ApprovalRequirement/v1", ref),
			Tier:              "STANDARD",
		})
	}
	return items, nil
}
