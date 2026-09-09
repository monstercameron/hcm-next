// Package repair owns the diagnosis and the corrective plan that follow a
// cross-system drift finding, and the pure simulation of that plan.
//
// Semantic owner: operations assurance, repair domain. Phase: P1A.
//
// In P1A the plan is a recommendation and nothing more. There is no Execute
// function in this package, not as a stub, not behind a flag, not guarded by a
// policy check. A plan is a document that says what would have to be true, in
// what order, under whose approval, with what rollback, for a drift to be
// corrected - and the only thing this package will do with it is simulate it
// against an in-memory copy.
//
// That is a structural claim, not a comment. A repair engine that has an
// execution path and a policy check in front of it eventually runs; a repair
// engine with no execution path cannot, whatever a caller believes about its
// own authority.
package repair

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/fielddiff"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Intent identity for REPAIR-001.
const (
	// CreateRepairPlanIntentType is the catalog identifier this function
	// implements.
	CreateRepairPlanIntentType = "hcmnext.operations.create_repair_plan"
	// CreateRepairPlanIntentVersion is the contract version.
	CreateRepairPlanIntentVersion = "v1"
	// PlanRulePackVersion versions the diagnosis and planning rules below.
	PlanRulePackVersion = "repair.plan.rules/1.0.0"
)

const (
	repairSchemaVer = 1
	planSchema      = "hcmnext.domains.repair.RepairPlan"
	stepSchema      = "hcmnext.domains.repair.RepairStep"
	diagnosisSchema = "hcmnext.domains.repair.Diagnosis"
)

// MaxStepAttempts bounds the retry budget a step may declare. An unbounded
// retry against an external system is not a repair, it is a denial of service
// with a business justification attached.
const MaxStepAttempts = 3

// MaxPlanSteps bounds one plan. A plan larger than this is a migration and
// belongs to a different, separately governed contract.
const MaxPlanSteps = 64

// Repair errors. All are matchable with errors.Is.
var (
	// ErrDiagnosisInvalid is returned for a malformed diagnosis.
	ErrDiagnosisInvalid = errors.New("repair: diagnosis is malformed")
	// ErrUnsupportedClaim is returned when a diagnosis asserts more confidence
	// than its evidence supports. A confident diagnosis with no immutable
	// evidence reference is a guess with a job title.
	ErrUnsupportedClaim = errors.New("repair: diagnosis confidence is not supported by its evidence")
	// ErrEvidenceRef is returned for an evidence reference that is not an
	// immutable artifact. A pointer at a log line is not evidence: logs are
	// rotated, redacted and rewritten, and a plan that cites one cannot be
	// revalidated later.
	ErrEvidenceRef = errors.New("repair: evidence reference is not an immutable artifact reference")
	// ErrFindingNotActionable is returned when a plan is asked to correct a
	// finding that is not a decided mismatch. Stale, unknown and redacted
	// findings are exactly the ones a repair queue must not act on.
	ErrFindingNotActionable = errors.New("repair: finding is not a decided mismatch")
	// ErrPlanInvalid is returned for a malformed plan.
	ErrPlanInvalid = errors.New("repair: plan is malformed")
	// ErrStepInvalid is returned for a malformed step.
	ErrStepInvalid = errors.New("repair: step is malformed")
	// ErrUnconstrainedRetry is returned for a step with no idempotency key, no
	// preconditions, or an attempt budget outside 1..MaxStepAttempts.
	ErrUnconstrainedRetry = errors.New("repair: step declares an unconstrained retry")
	// ErrHiddenEffect is returned for a writing step that declares no write
	// set. An effect that is not written down is an effect nobody approved.
	ErrHiddenEffect = errors.New("repair: writing step declares no write set")
	// ErrDestructiveHistory is returned for a step that would rewrite rather
	// than append to history. Retroactive changes create correction lineage;
	// they never erase what was believed before.
	ErrDestructiveHistory = errors.New("repair: step would rewrite history instead of appending a correction")
)

// EvidenceRefPrefixes are the accepted immutable-artifact reference schemes.
// The list is closed so that "evidence" cannot quietly come to mean whatever a
// caller has a string for.
var EvidenceRefPrefixes = []string{"evidence:", "digest:", "sha256:", "receipt:", "ledger:"}

// validEvidenceRef reports whether ref names an immutable artifact.
func validEvidenceRef(ref string) bool {
	for _, prefix := range EvidenceRefPrefixes {
		if strings.HasPrefix(ref, prefix) && len(ref) > len(prefix) {
			return true
		}
	}
	return false
}

// Confidence is how far a diagnosis is prepared to commit to its own
// conclusion.
type Confidence uint8

// Confidence levels.
const (
	// ConfidenceUnspecified is the zero value and is never legal.
	ConfidenceUnspecified Confidence = iota
	// ConfidenceLow means the evidence is suggestive and the unknowns matter.
	ConfidenceLow
	// ConfidenceMedium means the evidence is consistent and bounded.
	ConfidenceMedium
	// ConfidenceHigh means the evidence orders the two sides and no material
	// question is open.
	ConfidenceHigh
)

var confidenceWire = map[Confidence]string{
	ConfidenceLow:    "LOW",
	ConfidenceMedium: "MEDIUM",
	ConfidenceHigh:   "HIGH",
}

// String returns the stable wire token, or "CONFIDENCE_UNSPECIFIED".
func (c Confidence) String() string {
	if s, ok := confidenceWire[c]; ok {
		return s
	}
	return "CONFIDENCE_UNSPECIFIED"
}

// Valid reports whether c is a legal confidence level.
func (c Confidence) Valid() bool { _, ok := confidenceWire[c]; return ok }

// ProblemClass is what kind of problem the diagnosis says this is. The class
// decides which corrective actions are even considered, which is why it is a
// closed vocabulary rather than free text.
type ProblemClass uint8

// Problem classes.
const (
	// ProblemUnspecified is the zero value and is never legal.
	ProblemUnspecified ProblemClass = iota
	// ProblemFieldDrift is a value disagreement on one or more fields where
	// the authority is unambiguous.
	ProblemFieldDrift
	// ProblemUnorderedConflict is a disagreement no evidence orders, or one
	// where the side without authority changed last.
	ProblemUnorderedConflict
	// ProblemMappingDefect is a disagreement about what type the field even
	// is. It is a configuration defect, not a data defect.
	ProblemMappingDefect
	// ProblemCoverageGap is a subject or field the source could not be read
	// for at all.
	ProblemCoverageGap
	// ProblemNone is a diagnosis over a comparison that found nothing to fix.
	ProblemNone
)

var problemWire = map[ProblemClass]string{
	ProblemFieldDrift:        "FIELD_DRIFT",
	ProblemUnorderedConflict: "UNORDERED_CONFLICT",
	ProblemMappingDefect:     "MAPPING_DEFECT",
	ProblemCoverageGap:       "COVERAGE_GAP",
	ProblemNone:              "NONE",
}

// String returns the stable wire token, or "PROBLEM_CLASS_UNSPECIFIED".
func (p ProblemClass) String() string {
	if s, ok := problemWire[p]; ok {
		return s
	}
	return "PROBLEM_CLASS_UNSPECIFIED"
}

// Valid reports whether p is a legal problem class.
func (p ProblemClass) Valid() bool { _, ok := problemWire[p]; return ok }

// Diagnosis is what the platform believes is wrong, how sure it is, what it is
// relying on, and what it does not know.
//
// The unknowns are a first-class field rather than an omission. A diagnosis
// that lists nothing it failed to establish is indistinguishable from one that
// checked everything, and the difference decides whether a human should look
// at the plan.
type Diagnosis struct {
	// ID is the stable identity of this diagnosis. It is supplied by the
	// caller rather than generated, because a generated identity would make
	// two runs over identical evidence produce two different diagnoses.
	ID      string
	Subject values.EntityRef
	Problem ProblemClass
	// Confidence must be supported: HIGH requires evidence and no unknowns.
	Confidence Confidence
	// EvidenceRefs are immutable artifact references backing the conclusion.
	EvidenceRefs []string
	// Unknowns are the questions this diagnosis could not answer, as stable
	// tokens.
	Unknowns []string
	// DiffDigest binds the diagnosis to the exact comparison it was drawn
	// from.
	DiffDigest string
	// Actionable are the fields the diagnosis says could be corrected, and
	// Blocked the fields it says need a human. Both are subsets of the diff.
	Actionable []dataops.FieldID
	Blocked    []dataops.FieldID
}

// Validate reports whether the diagnosis is well formed and supported.
func (d Diagnosis) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("%w: diagnosis has no id", ErrDiagnosisInvalid)
	}
	if err := d.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %w", ErrDiagnosisInvalid, err)
	}
	if !d.Problem.Valid() {
		return fmt.Errorf("%w: %s has no problem class", ErrDiagnosisInvalid, d.ID)
	}
	if !d.Confidence.Valid() {
		return fmt.Errorf("%w: %s has no confidence", ErrDiagnosisInvalid, d.ID)
	}
	if d.DiffDigest == "" {
		return fmt.Errorf("%w: %s is not bound to a comparison", ErrDiagnosisInvalid, d.ID)
	}
	for _, ref := range d.EvidenceRefs {
		if !validEvidenceRef(ref) {
			return fmt.Errorf("%w: %q", ErrEvidenceRef, ref)
		}
	}
	if d.Confidence == ConfidenceHigh {
		if len(d.EvidenceRefs) == 0 {
			return fmt.Errorf("%w: %s claims HIGH with no evidence reference", ErrUnsupportedClaim, d.ID)
		}
		if len(d.Unknowns) > 0 {
			return fmt.Errorf("%w: %s claims HIGH with %d open unknown(s)",
				ErrUnsupportedClaim, d.ID, len(d.Unknowns))
		}
	}
	if d.Problem != ProblemNone && len(d.EvidenceRefs) == 0 {
		return fmt.Errorf("%w: %s asserts %s with no evidence reference",
			ErrUnsupportedClaim, d.ID, d.Problem)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (d Diagnosis) Canonical() []byte {
	if d.Validate() != nil {
		return nil
	}
	actionable := fieldTokens(d.Actionable)
	blocked := fieldTokens(d.Blocked)
	raw, err := canonicalbytes.New(diagnosisSchema, repairSchemaVer).
		String("id", d.ID).
		Value("subject", d.Subject).
		String("problem", d.Problem.String()).
		String("confidence", d.Confidence.String()).
		SortedStrings("evidence_refs", d.EvidenceRefs).
		SortedStrings("unknowns", d.Unknowns).
		String("diff_digest", d.DiffDigest).
		SortedStrings("actionable", actionable).
		SortedStrings("blocked", blocked).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// fieldTokens converts field identifiers to their string tokens.
func fieldTokens(fields []dataops.FieldID) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, string(f))
	}
	return out
}

// Diagnose derives a diagnosis from a completed comparison.
//
// It reads only the diff, never the values behind it. Every conclusion it
// reaches is a restatement of a classification the comparison already made,
// which is what keeps the diagnosis from becoming a second, weaker classifier
// that disagrees with the first one.
func Diagnose(id string, diff dataops.RecordDiff, evidenceRefs []string) (Diagnosis, error) {
	d := Diagnosis{
		ID:           id,
		Subject:      diff.Subject,
		DiffDigest:   diff.Digest,
		EvidenceRefs: append([]string(nil), evidenceRefs...),
	}
	mappingDefect, unordered, drift := false, false, false
	for _, f := range diff.Findings {
		switch {
		case f.Verdict == dataops.VerdictMismatch && f.Relation == fielddiff.RelationTypeMismatch:
			mappingDefect = true
			d.Blocked = append(d.Blocked, f.Field)
		case f.Verdict == dataops.VerdictMismatch && f.Safety == dataops.SafetySafe:
			drift = true
			d.Actionable = append(d.Actionable, f.Field)
		case f.Verdict == dataops.VerdictMismatch:
			unordered = true
			d.Blocked = append(d.Blocked, f.Field)
		case f.Verdict == dataops.VerdictStale || f.Verdict == dataops.VerdictUnknown:
			// An epistemic gap is an unknown, never a problem to be corrected.
			d.Unknowns = append(d.Unknowns, string(f.Field)+":"+f.Reason)
		}
	}
	sort.Slice(d.Actionable, func(i, j int) bool { return d.Actionable[i] < d.Actionable[j] })
	sort.Slice(d.Blocked, func(i, j int) bool { return d.Blocked[i] < d.Blocked[j] })
	sort.Strings(d.Unknowns)

	switch {
	case mappingDefect:
		d.Problem = ProblemMappingDefect
	case unordered:
		d.Problem = ProblemUnorderedConflict
	case drift:
		d.Problem = ProblemFieldDrift
	case len(d.Unknowns) > 0:
		d.Problem = ProblemCoverageGap
	default:
		d.Problem = ProblemNone
	}

	// Confidence is derived, never asserted. It is HIGH only when every
	// mismatch was ordered by evidence and nothing was left unknown.
	switch {
	case d.Problem == ProblemNone:
		d.Confidence = ConfidenceHigh
	case len(d.Unknowns) > 0 || unordered || mappingDefect:
		d.Confidence = ConfidenceLow
	case len(d.EvidenceRefs) == 0:
		d.Confidence = ConfidenceLow
	default:
		d.Confidence = ConfidenceHigh
	}
	if d.Problem == ProblemNone && len(d.EvidenceRefs) == 0 {
		// Nothing to explain and nothing to cite is coherent, but a HIGH
		// confidence with no evidence is not. Report it as medium.
		d.Confidence = ConfidenceMedium
	}

	if err := d.Validate(); err != nil {
		return Diagnosis{}, err
	}
	return d, nil
}

// Action is what a step proposes. The vocabulary contains no delete, no
// truncate and no rewrite: there is deliberately no way to express a
// destructive action, so no policy has to be trusted to forbid one.
type Action uint8

// Actions.
const (
	// ActionUnspecified is the zero value and is never legal.
	ActionUnspecified Action = iota
	// ActionRefreshProjection re-reads an externally mastered field into the
	// local projection. It writes locally and asserts nothing new.
	ActionRefreshProjection
	// ActionProposeExternalUpdate proposes one governed mutation of a locally
	// mastered field in the external system.
	ActionProposeExternalUpdate
	// ActionHumanReview routes the finding to a person. It writes nothing.
	ActionHumanReview
	// ActionMappingReview routes a type disagreement to configuration owners.
	// It writes nothing.
	ActionMappingReview
)

var actionWire = map[Action]string{
	ActionRefreshProjection:     "REFRESH_PROJECTION",
	ActionProposeExternalUpdate: "PROPOSE_EXTERNAL_UPDATE",
	ActionHumanReview:           "HUMAN_REVIEW",
	ActionMappingReview:         "MAPPING_REVIEW",
}

// String returns the stable wire token, or "ACTION_UNSPECIFIED".
func (a Action) String() string {
	if s, ok := actionWire[a]; ok {
		return s
	}
	return "ACTION_UNSPECIFIED"
}

// Valid reports whether a is a legal action.
func (a Action) Valid() bool { _, ok := actionWire[a]; return ok }

// Writes reports whether the action would change state somewhere.
func (a Action) Writes() bool {
	return a == ActionRefreshProjection || a == ActionProposeExternalUpdate
}

// HistoryTreatment is how a step would relate to what is already recorded.
type HistoryTreatment uint8

// History treatments. There is exactly one legal value.
const (
	// TreatmentUnspecified is the zero value and is never legal.
	TreatmentUnspecified HistoryTreatment = iota
	// TreatmentAppendCorrection appends a correction and keeps the superseded
	// assertion. It is the only treatment P1A recognises.
	TreatmentAppendCorrection
)

// String returns the stable wire token.
func (t HistoryTreatment) String() string {
	if t == TreatmentAppendCorrection {
		return "APPEND_CORRECTION"
	}
	return "HISTORY_TREATMENT_UNSPECIFIED"
}

// RiskClass is how much could go wrong if the step were executed.
type RiskClass uint8

// Risk classes.
const (
	// RiskUnspecified is the zero value and is never legal.
	RiskUnspecified RiskClass = iota
	// RiskLow is a reversible local refresh.
	RiskLow
	// RiskMedium is a governed external write with a declared rollback.
	RiskMedium
	// RiskHigh needs a human ruling before anything is attempted.
	RiskHigh
)

var riskWire = map[RiskClass]string{
	RiskLow:    "LOW",
	RiskMedium: "MEDIUM",
	RiskHigh:   "HIGH",
}

// String returns the stable wire token, or "RISK_UNSPECIFIED".
func (r RiskClass) String() string {
	if s, ok := riskWire[r]; ok {
		return s
	}
	return "RISK_UNSPECIFIED"
}

// Valid reports whether r is a legal risk class.
func (r RiskClass) Valid() bool { _, ok := riskWire[r]; return ok }

// atLeast returns the higher of two risk classes.
func (r RiskClass) atLeast(o RiskClass) RiskClass {
	if o > r {
		return o
	}
	return r
}

// PreconditionKind names something that must still be true when a plan is
// revalidated. Every kind pins a digest or a revision; none of them is a
// judgement that could be re-made differently later.
type PreconditionKind uint8

// Precondition kinds.
const (
	// PreconditionUnspecified is the zero value and is never legal.
	PreconditionUnspecified PreconditionKind = iota
	// PreconditionDiffDigest pins the comparison the plan was drawn from.
	PreconditionDiffDigest
	// PreconditionCanonicalRevision pins the canonical revision of the target.
	PreconditionCanonicalRevision
	// PreconditionObservationDigest pins the observation the plan relied on.
	PreconditionObservationDigest
	// PreconditionAuthorityPolicy pins the authority-by-field policy version.
	PreconditionAuthorityPolicy
)

var preconditionWire = map[PreconditionKind]string{
	PreconditionDiffDigest:        "DIFF_DIGEST_UNCHANGED",
	PreconditionCanonicalRevision: "CANONICAL_REVISION_UNCHANGED",
	PreconditionObservationDigest: "OBSERVATION_DIGEST_UNCHANGED",
	PreconditionAuthorityPolicy:   "AUTHORITY_POLICY_UNCHANGED",
}

// String returns the stable wire token, or "PRECONDITION_UNSPECIFIED".
func (k PreconditionKind) String() string {
	if s, ok := preconditionWire[k]; ok {
		return s
	}
	return "PRECONDITION_UNSPECIFIED"
}

// Valid reports whether k is a legal precondition kind.
func (k PreconditionKind) Valid() bool { _, ok := preconditionWire[k]; return ok }

// Precondition is one pinned fact that must still hold at revalidation.
type Precondition struct {
	Kind PreconditionKind
	// Ref names what is pinned; Expected is the pinned value.
	Ref      string
	Expected string
}

// Validate reports whether the precondition is usable.
func (p Precondition) Validate() error {
	if !p.Kind.Valid() {
		return fmt.Errorf("%w: precondition kind is unspecified", ErrStepInvalid)
	}
	if p.Ref == "" || p.Expected == "" {
		return fmt.Errorf("%w: precondition %s pins %q to %q", ErrStepInvalid, p.Kind, p.Ref, p.Expected)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (p Precondition) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.repair.Precondition", repairSchemaVer).
		String("kind", p.Kind.String()).
		String("ref", p.Ref).
		String("expected", p.Expected).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Target is the exact thing a step would touch. It is a single field of a
// single subject in a single named system: a plan that could not say which
// system it would write is a plan nobody can approve.
type Target struct {
	System  string
	Subject values.EntityRef
	Field   dataops.FieldID
}

// Validate reports whether the target is fully stated.
func (t Target) Validate() error {
	if t.System == "" {
		return fmt.Errorf("%w: target names no system", ErrStepInvalid)
	}
	if err := t.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: target subject: %w", ErrStepInvalid, err)
	}
	return t.Field.Validate()
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (t Target) Canonical() []byte {
	if t.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.repair.Target", repairSchemaVer).
		String("system", t.System).
		Value("subject", t.Subject).
		String("field", string(t.Field)).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Step is one ordered corrective action in a plan.
//
// Every field on it exists because a repair that omitted it would be
// unreviewable: what is being changed, from what to what, under which pinned
// evidence, how often it may be attempted, what would prove it worked, and how
// it would be undone.
type Step struct {
	// Ordinal is the step's position in the plan, starting at 1.
	Ordinal int
	Action  Action
	Target  Target
	// ExpectedCurrent is what the target is believed to hold now, and
	// ExpectedPost what it would hold afterwards. Both are presences so that
	// "would be set to nothing" stays distinct from "would be left alone".
	ExpectedCurrent values.Presence[string]
	ExpectedPost    values.Presence[string]
	// ValueKind is the declared type of the value being reconciled.
	ValueKind fielddiff.ValueKind
	// Treatment is how the step relates to recorded history.
	Treatment HistoryTreatment
	Risk      RiskClass
	// Preconditions are the pinned facts that must still hold. A step with
	// none is an unconstrained retry.
	Preconditions []Precondition
	// IdempotencyKey makes a re-attempt a no-op rather than a second write.
	IdempotencyKey string
	// MaxAttempts bounds the retry budget.
	MaxAttempts int
	// WriteSet names what would be written, one entry per touched resource. A
	// writing step with an empty write set is a hidden effect.
	WriteSet []string
	// DownstreamEffects names the recalculations a successful step would make
	// necessary. It is declared, never discovered afterwards.
	DownstreamEffects []string
	// RequiresApproval and ApprovalRoles state the governance requirement.
	RequiresApproval bool
	ApprovalRoles    []string
	// SoDExcludedRoles are roles that may not approve because they raised or
	// caused the finding.
	SoDExcludedRoles []string
	// Observation is what must be observed afterwards to believe the step
	// worked, and Success the criterion applied to it.
	Observation string
	Success     string
	// Rollback is how the step would be undone or compensated.
	Rollback string
	// Reason is the stable token explaining why this step exists.
	Reason string
}

// Validate reports whether the step is complete and constrained.
func (s Step) Validate() error {
	if s.Ordinal < 1 {
		return fmt.Errorf("%w: ordinal %d", ErrStepInvalid, s.Ordinal)
	}
	if !s.Action.Valid() {
		return fmt.Errorf("%w: step %d action is unspecified", ErrStepInvalid, s.Ordinal)
	}
	if err := s.Target.Validate(); err != nil {
		return err
	}
	if !s.Risk.Valid() {
		return fmt.Errorf("%w: step %d has no risk class", ErrStepInvalid, s.Ordinal)
	}
	if s.Treatment != TreatmentAppendCorrection {
		return fmt.Errorf("%w: step %d declares %s", ErrDestructiveHistory, s.Ordinal, s.Treatment)
	}
	if err := s.ExpectedCurrent.Validate(); err != nil {
		return fmt.Errorf("%w: step %d expected current: %w", ErrStepInvalid, s.Ordinal, err)
	}
	if err := s.ExpectedPost.Validate(); err != nil {
		return fmt.Errorf("%w: step %d expected post: %w", ErrStepInvalid, s.Ordinal, err)
	}
	if s.IdempotencyKey == "" {
		return fmt.Errorf("%w: step %d has no idempotency key", ErrUnconstrainedRetry, s.Ordinal)
	}
	if len(s.Preconditions) == 0 {
		return fmt.Errorf("%w: step %d pins nothing", ErrUnconstrainedRetry, s.Ordinal)
	}
	if s.MaxAttempts < 1 || s.MaxAttempts > MaxStepAttempts {
		return fmt.Errorf("%w: step %d allows %d attempt(s), bound is 1..%d",
			ErrUnconstrainedRetry, s.Ordinal, s.MaxAttempts, MaxStepAttempts)
	}
	for _, p := range s.Preconditions {
		if err := p.Validate(); err != nil {
			return err
		}
	}
	if s.Action.Writes() {
		if len(s.WriteSet) == 0 {
			return fmt.Errorf("%w: step %d proposes %s", ErrHiddenEffect, s.Ordinal, s.Action)
		}
		if s.Rollback == "" {
			return fmt.Errorf("%w: step %d writes with no rollback", ErrStepInvalid, s.Ordinal)
		}
	} else if len(s.WriteSet) != 0 {
		return fmt.Errorf("%w: step %d is %s but declares a write set",
			ErrStepInvalid, s.Ordinal, s.Action)
	}
	if s.Observation == "" || s.Success == "" {
		return fmt.Errorf("%w: step %d has no observation or success criterion",
			ErrStepInvalid, s.Ordinal)
	}
	if s.Reason == "" {
		return fmt.Errorf("%w: step %d has no reason token", ErrStepInvalid, s.Ordinal)
	}
	if !s.ValueKind.Valid() {
		return fmt.Errorf("%w: step %d declares no value kind", ErrStepInvalid, s.Ordinal)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (s Step) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	current, err := values.MarshalPresence(s.ExpectedCurrent, values.StringCodec{})
	if err != nil {
		return nil
	}
	post, err := values.MarshalPresence(s.ExpectedPost, values.StringCodec{})
	if err != nil {
		return nil
	}
	w := canonicalbytes.New(stepSchema, repairSchemaVer).
		Int("ordinal", int64(s.Ordinal)).
		String("action", s.Action.String()).
		Value("target", s.Target).
		Field("expected_current", current).
		Field("expected_post", post).
		String("value_kind", s.ValueKind.String()).
		String("treatment", s.Treatment.String()).
		String("risk", s.Risk.String()).
		Count("preconditions", len(s.Preconditions))
	for _, p := range s.Preconditions {
		w.Value("precondition", p)
	}
	raw, err := w.
		String("idempotency_key", s.IdempotencyKey).
		Int("max_attempts", int64(s.MaxAttempts)).
		SortedStrings("write_set", s.WriteSet).
		SortedStrings("downstream_effects", s.DownstreamEffects).
		Bool("requires_approval", s.RequiresApproval).
		SortedStrings("approval_roles", s.ApprovalRoles).
		SortedStrings("sod_excluded_roles", s.SoDExcludedRoles).
		String("observation", s.Observation).
		String("success", s.Success).
		String("rollback", s.Rollback).
		String("reason", s.Reason).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ExecutionState is the only lifecycle value a P1A repair plan may carry.
const ExecutionState = "NOT_PLANNED"

// NotExecutableReason is the token every plan carries to say why it cannot be
// run in this release.
const NotExecutableReason = "p1a_repair_execution_not_authorized"

// RepairPlan is the immutable, non-executable recommendation REPAIR-001
// produces: an ordered set of corrective steps bound to the exact diagnosis
// and comparison they came from.
//
// It has no Execute method and this package provides no function that takes a
// plan and causes an effect. The only thing that consumes a plan here is
// SimulateRepair, which applies it to a copy.
type RepairPlan struct {
	IntentType    string
	IntentVersion string

	// ID is the caller-supplied stable identity of this plan.
	ID      string
	Tenant  values.TenantId
	Subject values.EntityRef

	Diagnosis Diagnosis
	// DiffDigest binds the plan to the exact comparison it corrects.
	DiffDigest string
	Steps      []Step

	// Risk is the highest risk class of any step.
	Risk RiskClass
	// RequiresApproval is true when any step does.
	RequiresApproval bool
	ApprovalRoles    []string
	SoDExcludedRoles []string

	// SkippedFields are the fields the comparison flagged but the plan
	// deliberately did not act on, with the reason token. They are reported
	// so a reader can tell a considered omission from an oversight.
	SkippedFields []SkippedField

	// Executable is always false in P1A and NotExecutableReason says why.
	// The field is stored rather than computed so that a serialized plan
	// carries the claim with it.
	Executable          bool
	NotExecutableReason string
	ExecutionState      string

	PolicyVersion   string
	RulePackVersion string
	InputsDigest    string
	Digest          string
	Effects         evidence.EffectCounters
	Receipt         evidence.ZeroEffectReceipt
}

// SkippedField is one field the plan considered and did not act on.
type SkippedField struct {
	Field  dataops.FieldID
	Reason string
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (s SkippedField) Canonical() []byte {
	raw, err := canonicalbytes.New("hcmnext.domains.repair.SkippedField", repairSchemaVer).
		String("field", string(s.Field)).
		String("reason", s.Reason).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Step returns the step targeting a field.
func (p RepairPlan) Step(field dataops.FieldID) (Step, bool) {
	for _, s := range p.Steps {
		if s.Target.Field == field {
			return s, true
		}
	}
	return Step{}, false
}

// WritingSteps returns the steps that would change state somewhere.
func (p RepairPlan) WritingSteps() []Step {
	out := make([]Step, 0, len(p.Steps))
	for _, s := range p.Steps {
		if s.Action.Writes() {
			out = append(out, s)
		}
	}
	return out
}

// Validate reports whether the plan is complete, ordered and non-executable.
func (p RepairPlan) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("%w: plan has no id", ErrPlanInvalid)
	}
	if err := p.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrPlanInvalid, err)
	}
	if err := p.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %w", ErrPlanInvalid, err)
	}
	if err := p.Diagnosis.Validate(); err != nil {
		return err
	}
	if p.DiffDigest == "" || p.DiffDigest != p.Diagnosis.DiffDigest {
		return fmt.Errorf("%w: plan digest binding %q does not match diagnosis %q",
			ErrPlanInvalid, p.DiffDigest, p.Diagnosis.DiffDigest)
	}
	if len(p.Steps) > MaxPlanSteps {
		return fmt.Errorf("%w: %d steps exceeds %d", ErrPlanInvalid, len(p.Steps), MaxPlanSteps)
	}
	if p.Executable {
		return fmt.Errorf("%w: a P1A repair plan is never executable", ErrPlanInvalid)
	}
	if p.NotExecutableReason == "" {
		return fmt.Errorf("%w: plan does not say why it is not executable", ErrPlanInvalid)
	}
	if p.ExecutionState != ExecutionState {
		return fmt.Errorf("%w: execution state %q, P1A never leaves %s",
			ErrPlanInvalid, p.ExecutionState, ExecutionState)
	}
	if len(p.Steps) > 0 && !p.Risk.Valid() {
		return fmt.Errorf("%w: plan has steps and no risk class", ErrPlanInvalid)
	}
	seenField := make(map[dataops.FieldID]struct{}, len(p.Steps))
	for i, s := range p.Steps {
		if err := s.Validate(); err != nil {
			return err
		}
		if s.Ordinal != i+1 {
			return fmt.Errorf("%w: step at index %d has ordinal %d", ErrPlanInvalid, i, s.Ordinal)
		}
		if s.Target.Subject != p.Subject {
			return fmt.Errorf("%w: step %d targets %s, plan subject is %s",
				ErrPlanInvalid, s.Ordinal, s.Target.Subject, p.Subject)
		}
		if _, dup := seenField[s.Target.Field]; dup {
			return fmt.Errorf("%w: two steps target %s", ErrPlanInvalid, s.Target.Field)
		}
		seenField[s.Target.Field] = struct{}{}
		if s.RequiresApproval && !p.RequiresApproval {
			return fmt.Errorf("%w: step %d requires approval and the plan does not",
				ErrPlanInvalid, s.Ordinal)
		}
	}
	return p.Effects.Validate()
}

// canonicalBody encodes the plan without its own digest or receipt.
func (p RepairPlan) canonicalBody() ([]byte, error) {
	w := canonicalbytes.New(planSchema, repairSchemaVer).
		String("intent_type", p.IntentType).
		String("intent_version", p.IntentVersion).
		String("id", p.ID).
		String("tenant", string(p.Tenant)).
		Value("subject", p.Subject).
		Value("diagnosis", p.Diagnosis).
		String("diff_digest", p.DiffDigest).
		Count("steps", len(p.Steps))
	for _, s := range p.Steps {
		w.Value("step", s)
	}
	w.String("risk", p.Risk.String()).
		Bool("requires_approval", p.RequiresApproval).
		SortedStrings("approval_roles", p.ApprovalRoles).
		SortedStrings("sod_excluded_roles", p.SoDExcludedRoles).
		Count("skipped", len(p.SkippedFields))
	for _, s := range p.SkippedFields {
		w.Value("skipped", s)
	}
	return w.
		Bool("executable", p.Executable).
		String("not_executable_reason", p.NotExecutableReason).
		String("execution_state", p.ExecutionState).
		String("policy_version", p.PolicyVersion).
		String("rule_pack_version", p.RulePackVersion).
		Value("effects", p.Effects).
		Bytes()
}

// Canonical returns the canonical byte encoding including the digest and
// receipt, or nil when the plan is incoherent.
func (p RepairPlan) Canonical() []byte {
	body, err := p.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.repair.RepairPlanEnvelope", repairSchemaVer).
		Field("body", body).
		String("inputs_digest", p.InputsDigest).
		String("digest", p.Digest).
		Value("receipt", p.Receipt).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ApprovalPolicy is the governance input a plan is built under.
type ApprovalPolicy struct {
	// Version pins the approval policy so a plan can be replayed.
	Version string
	// RolesForExternalWrite and RolesForReview are the roles that must
	// approve each class of step.
	RolesForExternalWrite []string
	RolesForReview        []string
	// SoDExcludedRoles may never approve, because they raised the finding.
	SoDExcludedRoles []string
}

// Validate reports whether the policy is usable.
func (a ApprovalPolicy) Validate() error {
	if a.Version == "" {
		return fmt.Errorf("%w: approval policy version is required", ErrPlanInvalid)
	}
	if len(a.RolesForExternalWrite) == 0 || len(a.RolesForReview) == 0 {
		return fmt.Errorf("%w: approval policy names no roles", ErrPlanInvalid)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (a ApprovalPolicy) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.repair.ApprovalPolicy", repairSchemaVer).
		String("version", a.Version).
		SortedStrings("roles_external_write", a.RolesForExternalWrite).
		SortedStrings("roles_review", a.RolesForReview).
		SortedStrings("sod_excluded", a.SoDExcludedRoles).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// CreateRepairPlanRequest is the REPAIR-001 input.
type CreateRepairPlanRequest struct {
	// PlanID is the caller-supplied stable identity of the plan. It is an
	// input rather than a generated value so that two runs over identical
	// evidence produce byte-identical plans.
	PlanID string
	Tenant values.TenantId

	Diagnosis Diagnosis
	Diff      dataops.RecordDiff
	// Canonical and Observed are the two sides the diff compared. They supply
	// the values a step would move, which the diff deliberately does not
	// carry.
	Canonical       dataops.CanonicalRecord
	Observed        dataops.ObservedRecord
	ObservedPresent bool
	// Observation pins the page the external side came from.
	Observation dataops.ObservationWatermark

	// LocalSystem and ExternalSystem name the two sides for the write set.
	LocalSystem    string
	ExternalSystem string

	Approval ApprovalPolicy
	// AuthorityPolicyVersion pins the authority-by-field decision the safety
	// classification rested on.
	AuthorityPolicyVersion string
}

// Validate reports whether the request is well formed.
func (r CreateRepairPlanRequest) Validate() error {
	if r.PlanID == "" {
		return fmt.Errorf("%w: no plan id supplied", ErrPlanInvalid)
	}
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrPlanInvalid, err)
	}
	if err := r.Diagnosis.Validate(); err != nil {
		return err
	}
	if r.Diff.Digest == "" {
		return fmt.Errorf("%w: comparison carries no digest", ErrPlanInvalid)
	}
	if r.Diff.Digest != r.Diagnosis.DiffDigest {
		return fmt.Errorf("%w: diagnosis is bound to %q, comparison is %q",
			ErrPlanInvalid, r.Diagnosis.DiffDigest, r.Diff.Digest)
	}
	if r.Diff.Subject != r.Diagnosis.Subject {
		return fmt.Errorf("%w: diagnosis subject %s, comparison subject %s",
			ErrPlanInvalid, r.Diagnosis.Subject, r.Diff.Subject)
	}
	if err := r.Canonical.Validate(); err != nil {
		return err
	}
	if r.LocalSystem == "" || r.ExternalSystem == "" {
		return fmt.Errorf("%w: plan must name both systems", ErrPlanInvalid)
	}
	if r.AuthorityPolicyVersion == "" {
		return fmt.Errorf("%w: plan must pin the authority policy version", ErrPlanInvalid)
	}
	if err := r.Observation.Validate(); err != nil {
		return err
	}
	return r.Approval.Validate()
}

// inputsDigest digests exactly what the plan depends on.
func (r CreateRepairPlanRequest) inputsDigest() (string, error) {
	return canonicalbytes.New("hcmnext.domains.repair.CreateRepairPlanRequest", repairSchemaVer).
		String("intent_type", CreateRepairPlanIntentType).
		String("intent_version", CreateRepairPlanIntentVersion).
		String("plan_id", r.PlanID).
		String("tenant", string(r.Tenant)).
		Value("diagnosis", r.Diagnosis).
		Value("diff", r.Diff).
		Value("canonical", r.Canonical).
		Bool("observed?", r.ObservedPresent).
		Value("observation", r.Observation).
		String("local_system", r.LocalSystem).
		String("external_system", r.ExternalSystem).
		String("authority_policy_version", r.AuthorityPolicyVersion).
		Value("approval", r.Approval).
		Digest()
}

// CreateRepairPlan is the REPAIR-001 entry point: it turns a diagnosis over a
// completed comparison into an immutable, ordered, non-executable plan.
//
// What it refuses is the point:
//
//   - A finding that is not a decided mismatch never becomes a step. Stale,
//     unknown and redacted findings are recorded as skipped with their reason.
//   - A mismatch the comparison classified as repair-unsafe becomes a review
//     step that writes nothing, never a write step with a warning attached.
//   - Every step pins the comparison, the revision and the observation it
//     rests on, carries an idempotency key and a bounded attempt budget, and
//     declares its write set, rollback and success criterion. A step that
//     cannot say those things does not validate.
//   - The plan is marked non-executable and there is no code path in this
//     package that would execute it.
func CreateRepairPlan(req CreateRepairPlanRequest) (RepairPlan, error) {
	if err := req.Validate(); err != nil {
		return RepairPlan{}, err
	}
	inputsDigest, err := req.inputsDigest()
	if err != nil {
		return RepairPlan{}, err
	}

	plan := RepairPlan{
		IntentType:          CreateRepairPlanIntentType,
		IntentVersion:       CreateRepairPlanIntentVersion,
		ID:                  req.PlanID,
		Tenant:              req.Tenant,
		Subject:             req.Diff.Subject,
		Diagnosis:           req.Diagnosis,
		DiffDigest:          req.Diff.Digest,
		Executable:          false,
		NotExecutableReason: NotExecutableReason,
		ExecutionState:      ExecutionState,
		PolicyVersion:       req.Approval.Version,
		RulePackVersion:     PlanRulePackVersion,
		InputsDigest:        inputsDigest,
		Effects:             evidence.ZeroEffects(),
	}

	ordinal := 0
	roles := map[string]struct{}{}
	for _, finding := range req.Diff.Findings {
		if finding.Verdict != dataops.VerdictMismatch {
			if finding.Verdict != dataops.VerdictMatch && finding.Verdict != dataops.VerdictNotApplicable {
				plan.SkippedFields = append(plan.SkippedFields, SkippedField{
					Field:  finding.Field,
					Reason: finding.Reason,
				})
			}
			continue
		}
		ordinal++
		step, err := buildStep(ordinal, finding, req)
		if err != nil {
			return RepairPlan{}, err
		}
		plan.Steps = append(plan.Steps, step)
		plan.Risk = plan.Risk.atLeast(step.Risk)
		if step.RequiresApproval {
			plan.RequiresApproval = true
			for _, role := range step.ApprovalRoles {
				roles[role] = struct{}{}
			}
		}
	}
	if len(plan.Steps) > 0 {
		plan.ApprovalRoles = sortedKeys(roles)
		plan.SoDExcludedRoles = append([]string(nil), req.Approval.SoDExcludedRoles...)
		sort.Strings(plan.SoDExcludedRoles)
	}

	if err := plan.Validate(); err != nil {
		return RepairPlan{}, err
	}
	body, err := plan.canonicalBody()
	if err != nil {
		return RepairPlan{}, err
	}
	plan.Digest = canonicalbytes.Digest(body)
	receipt, err := evidence.NewZeroEffectReceipt(
		plan.IntentType, plan.IntentVersion,
		evidence.ModePreflight,
		evidence.RequestStatePreflighted,
		[]evidence.ControlVersion{
			{Name: "approval_policy", Version: req.Approval.Version},
			{Name: "authority_policy", Version: req.AuthorityPolicyVersion},
			{Name: "plan_rule_pack", Version: PlanRulePackVersion},
			{Name: "diff_rule_pack", Version: dataops.DiffRulePackVersion},
		},
		plan.InputsDigest, plan.Digest, plan.Effects,
	)
	if err != nil {
		return RepairPlan{}, err
	}
	plan.Receipt = receipt
	return plan, nil
}

// sortedKeys returns the keys of a set in ascending order.
func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// buildStep turns one mismatch finding into a corrective step.
func buildStep(ordinal int, finding dataops.FieldFinding, req CreateRepairPlanRequest) (Step, error) {
	if finding.Verdict != dataops.VerdictMismatch {
		return Step{}, fmt.Errorf("%w: %s is %s", ErrFindingNotActionable, finding.Field, finding.Verdict)
	}
	canonical, hasCanonical := req.Canonical.Lookup(finding.Field)
	var observed dataops.ObservedField
	hasObserved := false
	if req.ObservedPresent {
		observed, hasObserved = req.Observed.Lookup(finding.Field)
	}

	kind := fielddiff.KindString
	switch {
	case hasCanonical:
		kind = canonical.Kind
	case hasObserved:
		kind = observed.Kind
	}

	step := Step{
		Ordinal:          ordinal,
		Treatment:        TreatmentAppendCorrection,
		ValueKind:        kind,
		MaxAttempts:      1,
		Reason:           finding.SafetyReason,
		SoDExcludedRoles: append([]string(nil), req.Approval.SoDExcludedRoles...),
		Preconditions: []Precondition{
			{
				Kind:     PreconditionDiffDigest,
				Ref:      "comparison",
				Expected: req.Diff.Digest,
			},
			{
				Kind:     PreconditionObservationDigest,
				Ref:      req.Observation.Source,
				Expected: req.Observation.Digest,
			},
			{
				Kind:     PreconditionAuthorityPolicy,
				Ref:      "authority_by_field",
				Expected: req.AuthorityPolicyVersion,
			},
		},
	}
	if hasCanonical && canonical.Revision.IsSpecified() {
		step.Preconditions = append(step.Preconditions, Precondition{
			Kind:     PreconditionCanonicalRevision,
			Ref:      string(finding.Field),
			Expected: canonical.Revision.String(),
		})
	}
	sort.Strings(step.SoDExcludedRoles)

	// The action follows from the safety classification, which followed from
	// the authority. Nothing here re-decides authority: a planner that
	// re-derived it would be a second classifier able to disagree with the
	// comparison the plan is bound to.
	switch {
	case finding.Safety == dataops.SafetySafe &&
		finding.CanonicalAuthority.Kind == evidence.AuthorityExternalObservation:
		step.Action = ActionRefreshProjection
		step.Target = Target{System: req.LocalSystem, Subject: req.Diff.Subject, Field: finding.Field}
		step.ExpectedCurrent = presenceOrUnknown(hasCanonical, canonical.Value, "canonical_side_absent")
		step.ExpectedPost = presenceOrUnknown(hasObserved, observed.Value, "external_side_absent")
		step.Risk = RiskLow
		step.WriteSet = []string{req.LocalSystem + ":" + string(finding.Field)}
		step.DownstreamEffects = []string{"projection_recompute:" + string(finding.Field)}
		step.Rollback = "append a further correction restoring the superseded projection value"
		step.Observation = "re-read " + req.Observation.Source + " and re-run " + dataops.CapabilityDetectDrift
		step.Success = "the field compares MATCH against a fresher observation"
	case finding.Safety == dataops.SafetySafe:
		step.Action = ActionProposeExternalUpdate
		step.Target = Target{System: req.ExternalSystem, Subject: req.Diff.Subject, Field: finding.Field}
		step.ExpectedCurrent = presenceOrUnknown(hasObserved, observed.Value, "external_side_absent")
		step.ExpectedPost = presenceOrUnknown(hasCanonical, canonical.Value, "canonical_side_absent")
		step.Risk = RiskMedium
		step.WriteSet = []string{req.ExternalSystem + ":" + string(finding.Field)}
		step.DownstreamEffects = []string{"external_recalculation:" + string(finding.Field)}
		step.RequiresApproval = true
		step.ApprovalRoles = append([]string(nil), req.Approval.RolesForExternalWrite...)
		step.Rollback = "propose a compensating external update restoring the observed value"
		step.Observation = "re-read " + req.Observation.Source + " and re-run " + dataops.CapabilityDetectDrift
		step.Success = "the field compares MATCH against a fresher observation"
	case finding.Relation == fielddiff.RelationTypeMismatch:
		step.Action = ActionMappingReview
		step.Target = Target{System: req.ExternalSystem, Subject: req.Diff.Subject, Field: finding.Field}
		step.ExpectedCurrent = values.Unknown[string](finding.SafetyReason)
		step.ExpectedPost = values.Unknown[string]("mapping_review_outcome_unknown")
		step.Risk = RiskHigh
		step.RequiresApproval = true
		step.ApprovalRoles = append([]string(nil), req.Approval.RolesForReview...)
		step.Observation = "configuration owners record a mapping decision"
		step.Success = "the two sides declare the same value kind"
	default:
		step.Action = ActionHumanReview
		step.Target = Target{System: req.ExternalSystem, Subject: req.Diff.Subject, Field: finding.Field}
		step.ExpectedCurrent = values.Unknown[string](finding.SafetyReason)
		step.ExpectedPost = values.Unknown[string]("review_outcome_unknown")
		step.Risk = RiskHigh
		step.RequiresApproval = true
		step.ApprovalRoles = append([]string(nil), req.Approval.RolesForReview...)
		step.Observation = "a reviewer records which side is correct and why"
		step.Success = "an authority ruling exists for the disputed field"
	}
	sort.Strings(step.ApprovalRoles)

	key, err := canonicalbytes.New("hcmnext.domains.repair.IdempotencyKey", repairSchemaVer).
		String("plan_id", req.PlanID).
		String("diff_digest", req.Diff.Digest).
		String("field", string(finding.Field)).
		String("action", step.Action.String()).
		Digest()
	if err != nil {
		return Step{}, err
	}
	step.IdempotencyKey = key

	if err := step.Validate(); err != nil {
		return Step{}, err
	}
	return step, nil
}

// presenceOrUnknown returns the presence when the side had one, and an
// explicit UNKNOWN with a reason token when it did not. It never substitutes
// an empty value for a missing one.
func presenceOrUnknown(present bool, p values.Presence[string], reason string) values.Presence[string] {
	if present {
		return p
	}
	return values.Unknown[string](reason)
}
