package repair

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/domains/dataops"
	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Intent identity for REPAIR-003.
const (
	// SimulateRepairIntentType is the catalog identifier this function
	// implements.
	SimulateRepairIntentType = "hcmnext.operations.simulate_repair"
	// SimulateRepairIntentVersion is the contract version.
	SimulateRepairIntentVersion = "v1"
	// SimulationRulePackVersion versions the projection and status rules.
	SimulationRulePackVersion = "repair.simulation.rules/1.0.0"
)

// NoGuaranteeToken is carried by every simulation. A projection is what the
// comparison would say if every step succeeded exactly as written against
// exactly this evidence; it is not a promise that any of that will happen.
const NoGuaranteeToken = "projection_is_not_a_guarantee_of_correction"

// Simulation errors. All are matchable with errors.Is.
var (
	// ErrSimulationInvalid is returned for a malformed simulation request.
	ErrSimulationInvalid = errors.New("repair: simulation request is invalid")
	// ErrTargetBroadened is returned when a plan targets a subject, field or
	// system outside what the simulation was given. A simulation that widened
	// its own scope would be simulating a different plan than the one it
	// claims to bind.
	ErrTargetBroadened = errors.New("repair: plan targets something outside the simulated scope")
)

// Status is the simulation verdict.
type Status uint8

// Statuses.
const (
	// StatusUnspecified is the zero value and is never a legal result.
	StatusUnspecified Status = iota
	// StatusValid means the plan still binds its evidence and every step is
	// projectable.
	StatusValid
	// StatusReplanRequired means something the plan pinned has moved.
	StatusReplanRequired
	// StatusNoLongerRequired means the comparison no longer shows the problem.
	StatusNoLongerRequired
	// StatusBlocked means the plan contains a step no automation may take.
	StatusBlocked
	// StatusUnknown means the current evidence cannot decide.
	StatusUnknown
)

var statusWire = map[Status]string{
	StatusValid:            "VALID",
	StatusReplanRequired:   "REPLAN_REQUIRED",
	StatusNoLongerRequired: "NO_LONGER_REQUIRED",
	StatusBlocked:          "BLOCKED",
	StatusUnknown:          "UNKNOWN",
}

// String returns the stable wire token, or "STATUS_UNSPECIFIED".
func (s Status) String() string {
	if w, ok := statusWire[s]; ok {
		return w
	}
	return "STATUS_UNSPECIFIED"
}

// Valid reports whether s is a legal status.
func (s Status) Valid() bool { _, ok := statusWire[s]; return ok }

// Status reason tokens.
const (
	// ReasonEvidenceBinds is a plan whose pinned evidence still holds.
	ReasonEvidenceBinds = "pinned_evidence_still_binds"
	// ReasonDiffMoved is a comparison that has changed since the plan.
	ReasonDiffMoved = "comparison_digest_changed_since_plan"
	// ReasonObservationMoved is an observation that has been replaced.
	ReasonObservationMoved = "observation_digest_changed_since_plan"
	// ReasonAuthorityMoved is an authority policy version that has changed.
	ReasonAuthorityMoved = "authority_policy_changed_since_plan"
	// ReasonAlreadySatisfied is a comparison with nothing left to correct.
	ReasonAlreadySatisfied = "targets_already_agree"
	// ReasonNeedsHumanRuling is a plan containing a review step.
	ReasonNeedsHumanRuling = "plan_contains_a_step_requiring_a_human_ruling"
	// ReasonEvidenceIncomplete is a target the current comparison cannot
	// decide.
	ReasonEvidenceIncomplete = "current_comparison_cannot_decide_a_target"
	// ReasonNoSteps is a plan with nothing in it.
	ReasonNoSteps = "plan_contains_no_steps"
)

// ProjectedField is one field's before and after under the plan.
//
// Before and After are presences, so "would be set to nothing" and "would be
// left as it is" stay distinguishable, and a step whose post-state the plan
// could not state stays UNKNOWN rather than becoming an empty string.
type ProjectedField struct {
	Field dataops.FieldID
	// System is the side the step would write.
	System  string
	Action  Action
	Before  values.Presence[string]
	After   values.Presence[string]
	Changed bool
	// Reason is the token explaining why the projection did or did not move.
	Reason string
}

// Canonical returns the canonical byte encoding, or nil when incoherent.
func (p ProjectedField) Canonical() []byte {
	before, err := values.MarshalPresence(p.Before, values.StringCodec{})
	if err != nil {
		return nil
	}
	after, err := values.MarshalPresence(p.After, values.StringCodec{})
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.repair.ProjectedField", repairSchemaVer).
		String("field", string(p.Field)).
		String("system", p.System).
		String("action", p.Action.String()).
		Field("before", before).
		Field("after", after).
		Bool("changed", p.Changed).
		String("reason", p.Reason).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// RiskNote is one declared risk of the plan, attached to the step that carries
// it.
type RiskNote struct {
	Field dataops.FieldID
	Class RiskClass
	Token string
}

// Canonical returns the canonical byte encoding.
func (r RiskNote) Canonical() []byte {
	raw, err := canonicalbytes.New("hcmnext.domains.repair.RiskNote", repairSchemaVer).
		String("field", string(r.Field)).
		String("class", r.Class.String()).
		String("token", r.Token).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Cost is what the plan would cost if it were executed. Every count is
// proposed, not performed: the simulation's own effect counters are separate
// and are all zero.
type Cost struct {
	Steps          int
	LocalWrites    int
	ExternalWrites int
	HumanReviews   int
	MappingReviews int
	// Approvals is how many steps would need an approval decision.
	Approvals int
	// MaxAttempts is the total attempt budget the plan reserves.
	MaxAttempts int
}

// Canonical returns the canonical byte encoding.
func (c Cost) Canonical() []byte {
	raw, err := canonicalbytes.New("hcmnext.domains.repair.Cost", repairSchemaVer).
		Int("steps", int64(c.Steps)).
		Int("local_writes", int64(c.LocalWrites)).
		Int("external_writes", int64(c.ExternalWrites)).
		Int("human_reviews", int64(c.HumanReviews)).
		Int("mapping_reviews", int64(c.MappingReviews)).
		Int("approvals", int64(c.Approvals)).
		Int("max_attempts", int64(c.MaxAttempts)).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// RepairSimulation is the REPAIR-003 result.
type RepairSimulation struct {
	IntentType    string
	IntentVersion string

	// PlanID and PlanDigest bind the simulation to the exact plan.
	PlanID     string
	PlanDigest string
	// DiffDigest is the comparison the plan was drawn from, and
	// CurrentDiffDigest the one the simulation was run against. They differ
	// exactly when the world moved under the plan.
	DiffDigest        string
	CurrentDiffDigest string

	Tenant  values.TenantId
	Subject values.EntityRef

	Status Status
	// StatusReason is the stable token explaining the status.
	StatusReason string

	// Projected is the in-memory post-state, one entry per step.
	Projected []ProjectedField
	// ResidualFindings are the comparisons that would still not agree after
	// every step succeeded, and ResidualCounts partitions the whole projected
	// comparison. A simulation that reported only successes would be a
	// brochure.
	ResidualFindings []dataops.FieldFinding
	ResidualCounts   dataops.VerdictCounts
	// ProjectedDiffDigest is the digest of the comparison over the projected
	// post-state.
	ProjectedDiffDigest string

	Risks            []RiskNote
	RequiresApproval bool
	ApprovalRoles    []string
	SoDExcludedRoles []string
	Cost             Cost

	// Observation, Success and Rollback expectations are collected from the
	// steps so a reviewer sees the whole verification story in one place.
	ObservationExpectations []string
	SuccessCriteria         []string
	RollbackExpectations    []string
	// Caveats always includes NoGuaranteeToken.
	Caveats []string

	PolicyVersion   string
	RulePackVersion string
	InputsDigest    string
	ResultDigest    string
	// Effects are the effects this simulation caused. They are always zero,
	// and the receipt refuses to exist otherwise.
	Effects evidence.EffectCounters
	Receipt evidence.ZeroEffectReceipt
}

// canonicalBody encodes everything except the receipt.
func (s RepairSimulation) canonicalBody() ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.repair.RepairSimulation", repairSchemaVer).
		String("intent_type", s.IntentType).
		String("intent_version", s.IntentVersion).
		String("plan_id", s.PlanID).
		String("plan_digest", s.PlanDigest).
		String("diff_digest", s.DiffDigest).
		String("current_diff_digest", s.CurrentDiffDigest).
		String("tenant", string(s.Tenant)).
		Value("subject", s.Subject).
		String("status", s.Status.String()).
		String("status_reason", s.StatusReason).
		Count("projected", len(s.Projected))
	for _, p := range s.Projected {
		w.Value("projected", p)
	}
	w.Count("residual", len(s.ResidualFindings))
	for _, f := range s.ResidualFindings {
		w.Value("residual", f)
	}
	w.Value("residual_counts", s.ResidualCounts).
		String("projected_diff_digest", s.ProjectedDiffDigest).
		Count("risks", len(s.Risks))
	for _, r := range s.Risks {
		w.Value("risk", r)
	}
	return w.
		Bool("requires_approval", s.RequiresApproval).
		SortedStrings("approval_roles", s.ApprovalRoles).
		SortedStrings("sod_excluded_roles", s.SoDExcludedRoles).
		Value("cost", s.Cost).
		SortedStrings("observation_expectations", s.ObservationExpectations).
		SortedStrings("success_criteria", s.SuccessCriteria).
		SortedStrings("rollback_expectations", s.RollbackExpectations).
		SortedStrings("caveats", s.Caveats).
		String("policy_version", s.PolicyVersion).
		String("rule_pack_version", s.RulePackVersion).
		Value("effects", s.Effects).
		Bytes()
}

// Canonical returns the canonical byte encoding including the receipt, or nil
// when the simulation is incoherent.
func (s RepairSimulation) Canonical() []byte {
	body, err := s.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.repair.RepairSimulationEnvelope", repairSchemaVer).
		Field("body", body).
		String("inputs_digest", s.InputsDigest).
		Value("receipt", s.Receipt).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// SimulateRepairRequest is the REPAIR-003 input: the plan, and the evidence as
// it stands right now.
//
// The current comparison is supplied rather than recomputed from a repository,
// for the same reason the plan is: this is a pure calculation, and a function
// that could go and read state would be able to produce a different answer on
// a second run over the same inputs.
type SimulateRepairRequest struct {
	Tenant values.TenantId
	Plan   RepairPlan

	// CurrentDiff is the comparison as it stands now.
	CurrentDiff dataops.RecordDiff
	// Canonical and Observed are the two sides the plan would act on.
	Canonical       dataops.CanonicalRecord
	Observed        dataops.ObservedRecord
	ObservedPresent bool
	Observation     dataops.ObservationWatermark

	Fields        []dataops.FieldID
	Authorization dataops.Authorization
	Freshness     dataops.FreshnessPolicy
	// EvaluatedAt is the instant the projection is dated at. It is an input,
	// never a clock read.
	EvaluatedAt values.Instant
	// AuthorityPolicyVersion is the authority policy in force now. A plan
	// pinned to a different one is replanned, not simulated.
	AuthorityPolicyVersion string
	// LocalSystem and ExternalSystem are the only two systems a plan may
	// target here.
	LocalSystem    string
	ExternalSystem string
}

// Validate reports whether the request is well formed.
func (r SimulateRepairRequest) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrSimulationInvalid, err)
	}
	if err := r.Plan.Validate(); err != nil {
		return err
	}
	if r.Plan.Digest == "" {
		return fmt.Errorf("%w: plan carries no digest", ErrSimulationInvalid)
	}
	if r.Plan.Tenant != r.Tenant {
		return fmt.Errorf("%w: plan tenant %s, request tenant %s",
			ErrSimulationInvalid, r.Plan.Tenant, r.Tenant)
	}
	if err := r.Canonical.Validate(); err != nil {
		return err
	}
	if r.Canonical.Subject != r.Plan.Subject {
		return fmt.Errorf("%w: canonical subject %s, plan subject %s",
			ErrTargetBroadened, r.Canonical.Subject, r.Plan.Subject)
	}
	if r.CurrentDiff.Subject != r.Plan.Subject {
		return fmt.Errorf("%w: comparison subject %s, plan subject %s",
			ErrTargetBroadened, r.CurrentDiff.Subject, r.Plan.Subject)
	}
	if r.ObservedPresent {
		if err := r.Observed.Validate(); err != nil {
			return err
		}
		if r.Observed.Subject != r.Plan.Subject {
			return fmt.Errorf("%w: observed subject %s, plan subject %s",
				ErrTargetBroadened, r.Observed.Subject, r.Plan.Subject)
		}
	}
	if err := r.Observation.Validate(); err != nil {
		return err
	}
	if err := r.Freshness.Validate(); err != nil {
		return err
	}
	if !r.EvaluatedAt.IsSet() {
		return fmt.Errorf("%w: no evaluation instant", ErrSimulationInvalid)
	}
	if r.LocalSystem == "" || r.ExternalSystem == "" {
		return fmt.Errorf("%w: simulation must name both systems", ErrSimulationInvalid)
	}
	if r.AuthorityPolicyVersion == "" {
		return fmt.Errorf("%w: simulation must pin the authority policy version", ErrSimulationInvalid)
	}
	if err := r.Authorization.Validate(); err != nil {
		return err
	}
	fields, err := dataopsFields(r.Fields)
	if err != nil {
		return err
	}
	if err := r.Authorization.Covers(fields); err != nil {
		return err
	}
	// The plan may not reach outside the scope the simulation was handed.
	inScope := make(map[dataops.FieldID]struct{}, len(fields))
	for _, f := range fields {
		inScope[f] = struct{}{}
	}
	for _, step := range r.Plan.Steps {
		if _, ok := inScope[step.Target.Field]; !ok {
			return fmt.Errorf("%w: step %d targets %s, outside the simulated projection",
				ErrTargetBroadened, step.Ordinal, step.Target.Field)
		}
		if step.Target.Subject != r.Plan.Subject {
			return fmt.Errorf("%w: step %d targets subject %s",
				ErrTargetBroadened, step.Ordinal, step.Target.Subject)
		}
		if step.Target.System != r.LocalSystem && step.Target.System != r.ExternalSystem {
			return fmt.Errorf("%w: step %d targets system %q",
				ErrTargetBroadened, step.Ordinal, step.Target.System)
		}
		if !r.Authorization.Allows(step.Target.Field) {
			return fmt.Errorf("%w: step %d targets %s, which this caller may not see",
				ErrTargetBroadened, step.Ordinal, step.Target.Field)
		}
	}
	return nil
}

// dataopsFields validates and sorts a projection through the DataOps rules.
func dataopsFields(fields []dataops.FieldID) ([]dataops.FieldID, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("%w: no fields to simulate over", ErrSimulationInvalid)
	}
	seen := make(map[dataops.FieldID]struct{}, len(fields))
	out := make([]dataops.FieldID, 0, len(fields))
	for _, f := range fields {
		if err := f.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrSimulationInvalid, err)
		}
		if _, dup := seen[f]; dup {
			return nil, fmt.Errorf("%w: %s requested twice", ErrSimulationInvalid, f)
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// inputsDigest digests exactly what the simulation depends on.
func (r SimulateRepairRequest) inputsDigest() (string, error) {
	fields, err := dataopsFields(r.Fields)
	if err != nil {
		return "", err
	}
	w := canonicalbytes.New("hcmnext.domains.repair.SimulateRepairRequest", repairSchemaVer).
		String("intent_type", SimulateRepairIntentType).
		String("intent_version", SimulateRepairIntentVersion).
		String("tenant", string(r.Tenant)).
		String("plan_digest", r.Plan.Digest).
		Value("current_diff", r.CurrentDiff).
		Value("canonical", r.Canonical).
		Bool("observed?", r.ObservedPresent)
	if r.ObservedPresent {
		w.Value("observed", r.Observed)
	}
	w.Value("observation", r.Observation).
		Count("fields", len(fields))
	for _, f := range fields {
		w.String("field", string(f))
	}
	return w.
		Value("authorization", r.Authorization).
		Value("freshness", r.Freshness).
		Value("evaluated_at", r.EvaluatedAt).
		String("authority_policy_version", r.AuthorityPolicyVersion).
		String("local_system", r.LocalSystem).
		String("external_system", r.ExternalSystem).
		Digest()
}

// SimulateRepair is the REPAIR-003 entry point: a pure calculation that
// applies a repair plan to an in-memory copy of both sides and reports the
// projected post-state, the residual disagreements, the risks, the approvals,
// the cost, and a status.
//
// It is pure in the strict sense. It reads no clock, no repository and no
// connector; it copies the two records it was given, edits the copies, and
// re-runs the same comparison the plan was built from. The caller's records
// are never touched, and the intent's own effect counters are all zero -
// which is what the receipt certifies.
//
// The status is decided before the projection is trusted:
//
//   - Targets that already agree make the plan NO_LONGER_REQUIRED. Executing
//     a satisfied repair is how a correct value gets overwritten with an old
//     one.
//   - A moved comparison, observation or authority policy makes it
//     REPLAN_REQUIRED. The plan pinned those; if they moved, the plan is
//     about a world that no longer exists.
//   - A step needing a human ruling makes the whole plan BLOCKED, not
//     partially valid. A plan is approved and executed as a unit.
//   - A target the current comparison cannot decide makes it UNKNOWN.
func SimulateRepair(req SimulateRepairRequest) (RepairSimulation, error) {
	if err := req.Validate(); err != nil {
		return RepairSimulation{}, err
	}
	fields, err := dataopsFields(req.Fields)
	if err != nil {
		return RepairSimulation{}, err
	}
	inputsDigest, err := req.inputsDigest()
	if err != nil {
		return RepairSimulation{}, err
	}

	sim := RepairSimulation{
		IntentType:        SimulateRepairIntentType,
		IntentVersion:     SimulateRepairIntentVersion,
		PlanID:            req.Plan.ID,
		PlanDigest:        req.Plan.Digest,
		DiffDigest:        req.Plan.DiffDigest,
		CurrentDiffDigest: req.CurrentDiff.Digest,
		Tenant:            req.Tenant,
		Subject:           req.Plan.Subject,
		RequiresApproval:  req.Plan.RequiresApproval,
		ApprovalRoles:     append([]string(nil), req.Plan.ApprovalRoles...),
		SoDExcludedRoles:  append([]string(nil), req.Plan.SoDExcludedRoles...),
		Caveats:           []string{NoGuaranteeToken},
		PolicyVersion:     req.Plan.PolicyVersion,
		RulePackVersion:   SimulationRulePackVersion,
		InputsDigest:      inputsDigest,
		Effects:           evidence.ZeroEffects(),
	}

	sim.Status, sim.StatusReason = decideStatus(req)

	canonical, observed := copySides(req)
	for _, step := range req.Plan.Steps {
		projected := applyStep(step, &canonical, &observed, req.EvaluatedAt)
		sim.Projected = append(sim.Projected, projected)

		sim.Cost.Steps++
		sim.Cost.MaxAttempts += step.MaxAttempts
		if step.RequiresApproval {
			sim.Cost.Approvals++
		}
		switch step.Action {
		case ActionRefreshProjection:
			sim.Cost.LocalWrites++
		case ActionProposeExternalUpdate:
			sim.Cost.ExternalWrites++
		case ActionHumanReview:
			sim.Cost.HumanReviews++
		case ActionMappingReview:
			sim.Cost.MappingReviews++
		}
		sim.Risks = append(sim.Risks, RiskNote{
			Field: step.Target.Field,
			Class: step.Risk,
			Token: step.Reason,
		})
		if step.Observation != "" {
			sim.ObservationExpectations = append(sim.ObservationExpectations, step.Observation)
		}
		if step.Success != "" {
			sim.SuccessCriteria = append(sim.SuccessCriteria, step.Success)
		}
		if step.Rollback != "" {
			sim.RollbackExpectations = append(sim.RollbackExpectations, step.Rollback)
		}
	}

	post, err := dataops.DiffRecord(dataops.DiffRecordRequest{
		Canonical:       canonical,
		Observed:        observed,
		ObservedPresent: req.ObservedPresent,
		Observation:     req.Observation,
		Fields:          fields,
		Authorization:   req.Authorization,
		Freshness:       req.Freshness,
		EvaluatedAt:     req.EvaluatedAt,
	})
	if err != nil {
		return RepairSimulation{}, err
	}
	sim.ProjectedDiffDigest = post.Digest
	sim.ResidualCounts = post.Verdicts
	for _, f := range post.Findings {
		if f.Verdict == dataops.VerdictMatch || f.Verdict == dataops.VerdictNotApplicable {
			continue
		}
		sim.ResidualFindings = append(sim.ResidualFindings, f)
	}
	if len(sim.ResidualFindings) > 0 {
		sim.Caveats = append(sim.Caveats, "residual_disagreements_remain_after_every_step")
	}
	sort.Strings(sim.Caveats)

	body, err := sim.canonicalBody()
	if err != nil {
		return RepairSimulation{}, err
	}
	sim.ResultDigest = canonicalbytes.Digest(body)
	receipt, err := evidence.NewZeroEffectReceipt(
		sim.IntentType, sim.IntentVersion,
		evidence.ModeSimulate,
		evidence.RequestStateSimulated,
		[]evidence.ControlVersion{
			{Name: "approval_policy", Version: req.Plan.PolicyVersion},
			{Name: "authority_policy", Version: req.AuthorityPolicyVersion},
			{Name: "plan_rule_pack", Version: req.Plan.RulePackVersion},
			{Name: "simulation_rule_pack", Version: SimulationRulePackVersion},
			{Name: "diff_rule_pack", Version: dataops.DiffRulePackVersion},
			{Name: "freshness_policy", Version: req.Freshness.Version},
		},
		sim.InputsDigest, sim.ResultDigest, sim.Effects,
	)
	if err != nil {
		return RepairSimulation{}, err
	}
	sim.Receipt = receipt
	return sim, nil
}

// decideStatus evaluates the plan against the evidence as it stands now.
func decideStatus(req SimulateRepairRequest) (Status, string) {
	if len(req.Plan.Steps) == 0 {
		return StatusNoLongerRequired, ReasonNoSteps
	}

	// Already satisfied outranks everything. A plan whose targets now agree is
	// finished, whatever else has moved.
	satisfied := true
	for _, step := range req.Plan.Steps {
		finding, ok := req.CurrentDiff.Finding(step.Target.Field)
		if !ok || finding.Verdict == dataops.VerdictMismatch {
			satisfied = false
			break
		}
		if finding.Verdict != dataops.VerdictMatch && finding.Verdict != dataops.VerdictNotApplicable {
			satisfied = false
			break
		}
	}
	if satisfied {
		return StatusNoLongerRequired, ReasonAlreadySatisfied
	}

	if req.CurrentDiff.Digest != req.Plan.DiffDigest {
		return StatusReplanRequired, ReasonDiffMoved
	}
	for _, step := range req.Plan.Steps {
		for _, pre := range step.Preconditions {
			switch pre.Kind {
			case PreconditionObservationDigest:
				if pre.Expected != req.Observation.Digest {
					return StatusReplanRequired, ReasonObservationMoved
				}
			case PreconditionAuthorityPolicy:
				if pre.Expected != req.AuthorityPolicyVersion {
					return StatusReplanRequired, ReasonAuthorityMoved
				}
			case PreconditionDiffDigest:
				if pre.Expected != req.CurrentDiff.Digest {
					return StatusReplanRequired, ReasonDiffMoved
				}
			}
		}
	}

	for _, step := range req.Plan.Steps {
		if !step.Action.Writes() {
			return StatusBlocked, ReasonNeedsHumanRuling
		}
	}

	for _, step := range req.Plan.Steps {
		finding, ok := req.CurrentDiff.Finding(step.Target.Field)
		if !ok || !finding.Verdict.Decided() {
			return StatusUnknown, ReasonEvidenceIncomplete
		}
	}
	return StatusValid, ReasonEvidenceBinds
}

// copySides returns independent copies of both records. The copies are what
// the projection edits; the caller's slices are never aliased, which is why a
// simulation can be run twice over the same inputs and get the same answer.
func copySides(req SimulateRepairRequest) (dataops.CanonicalRecord, dataops.ObservedRecord) {
	canonical := req.Canonical
	canonical.Fields = append([]dataops.CanonicalField(nil), req.Canonical.Fields...)

	observed := req.Observed
	observed.Fields = append([]dataops.ObservedField(nil), req.Observed.Fields...)
	return canonical, observed
}

// applyStep edits the in-memory copies and reports what moved.
//
// A step whose post-state the plan could not state - a review step, or a write
// whose intended value is UNKNOWN - moves nothing. Projecting an unknown as an
// empty value is exactly the error that makes a simulation look successful and
// an execution destructive.
func applyStep(
	step Step,
	canonical *dataops.CanonicalRecord,
	observed *dataops.ObservedRecord,
	at values.Instant,
) ProjectedField {
	projected := ProjectedField{
		Field:  step.Target.Field,
		System: step.Target.System,
		Action: step.Action,
		Before: step.ExpectedCurrent,
		After:  step.ExpectedPost,
	}
	if !step.Action.Writes() {
		projected.After = step.ExpectedCurrent
		projected.Reason = "step_writes_nothing"
		return projected
	}
	if !step.ExpectedPost.IsValue() {
		projected.After = step.ExpectedCurrent
		projected.Reason = "post_state_is_not_a_readable_value"
		return projected
	}

	switch step.Action {
	case ActionRefreshProjection:
		for i := range canonical.Fields {
			if canonical.Fields[i].Field != step.Target.Field {
				continue
			}
			projected.Before = canonical.Fields[i].Value
			canonical.Fields[i].Value = step.ExpectedPost
			canonical.Fields[i].UpdatedAt = at
			projected.Changed = true
			projected.Reason = "local_projection_refreshed_from_the_external_master"
			return projected
		}
		projected.Reason = "canonical_record_has_no_such_field_to_refresh"
	case ActionProposeExternalUpdate:
		for i := range observed.Fields {
			if observed.Fields[i].Field != step.Target.Field {
				continue
			}
			projected.Before = observed.Fields[i].Value
			observed.Fields[i].Value = step.ExpectedPost
			observed.Fields[i].UpdatedAt = at
			projected.Changed = true
			projected.Reason = "external_record_updated_from_the_locally_mastered_value"
			return projected
		}
		projected.Reason = "observed_record_has_no_such_field_to_update"
	}
	return projected
}
