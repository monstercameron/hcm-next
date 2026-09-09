// Package eligibility is the pure eligibility engine: given a typed request
// naming a subject, a program/opportunity/action revision, a requested and
// effective interval, a jurisdiction, a population/fact/rule snapshot, a
// purpose and an authority, it compiles bounded criteria, evaluates them
// deterministically against caller-supplied fact and rule ports, and explains
// the result without ever collapsing missing evidence into a false ELIGIBLE
// or INELIGIBLE default.
//
// Semantic owner: shared-engines. Phase: deferred (no P1A/P1B consumer as of
// 2026-09-02; built per lane assignment so a funded consumer has a ready
// contract). Eligibility describes qualification under rules; it neither
// grants authorization nor records a human approval, and it never mutates
// enrollment, approval or any other domain state - Evaluate takes no writer,
// only read ports, and returns a value.
//
// All arithmetic and comparison is exact: text, values.Decimal and
// values.LocalDate/Instant. There is no float64 anywhere in this package.
package eligibility

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	requestSchema = "hcmnext.engines.eligibility.Request"
	resultSchema  = "hcmnext.engines.eligibility.Result"
	schemaVersion = 1
)

func writerFor(schema string) *canonicalbytes.Writer {
	return canonicalbytes.New(schema, schemaVersion)
}

// Version reports this engine's own package contract version: the schema
// version every canonical encoder in this package agrees on (see
// writerFor above). It is part of the ARCH-GO-009 engine package contract,
// not a business-facing evaluation input.
func Version() int { return schemaVersion }

// Engine errors. All are matchable with errors.Is.
var (
	// ErrRequestSubject is returned when a request has no valid subject.
	ErrRequestSubject = errors.New("eligibility: request requires a valid subject")
	// ErrRequestSubjectMatter is returned when the program/opportunity/action
	// revision is not identified.
	ErrRequestSubjectMatter = errors.New("eligibility: request requires a program/opportunity/action id and revision")
	// ErrRequestInterval is returned when the requested or effective interval
	// is missing or invalid.
	ErrRequestInterval = errors.New("eligibility: request requires a valid requested and effective interval")
	// ErrRequestJurisdiction is returned when no jurisdiction is declared.
	ErrRequestJurisdiction = errors.New("eligibility: request requires a jurisdiction")
	// ErrRequestSnapshots is returned when the population, fact or rule
	// snapshot reference is missing.
	ErrRequestSnapshots = errors.New("eligibility: request requires population, fact and rule snapshot references")
	// ErrRequestPurpose is returned when no purpose is declared.
	ErrRequestPurpose = errors.New("eligibility: request requires a purpose")
	// ErrRequestAuthority is returned when no requesting authority is cited.
	ErrRequestAuthority = errors.New("eligibility: request requires an authority")
	// ErrResultStatus is returned when a result's status is not one of the
	// four legal values.
	ErrResultStatus = errors.New("eligibility: result status must be ELIGIBLE, INELIGIBLE, CONDITIONAL or UNKNOWN")
	// ErrResultVersions is returned when a result does not bind program, fact
	// and rule snapshot versions.
	ErrResultVersions = errors.New("eligibility: result requires program, fact and rule snapshot versions")
	// ErrResultReasons is returned when a result carries no reasons: a result
	// must always explain itself, whatever its status.
	ErrResultReasons = errors.New("eligibility: result requires at least one reason")
	// ErrResultEvidence is returned when a result cites no evidence: even an
	// UNKNOWN result must say which facts and rules it attempted to read.
	ErrResultEvidence = errors.New("eligibility: result requires at least one evidence reference")
	// ErrResultObligations is returned when a CONDITIONAL result carries no
	// obligations describing what remains pending.
	ErrResultObligations = errors.New("eligibility: a CONDITIONAL result requires at least one obligation")
	// ErrResultMissingFacts is returned when an UNKNOWN result does not name
	// which facts or rules could not be determined.
	ErrResultMissingFacts = errors.New("eligibility: an UNKNOWN result requires at least one missing-fact reference")
	// ErrBindingMismatch means a result was not produced for the exact pinned
	// request context. Such a result must not be treated as a restatement of
	// the request.
	ErrBindingMismatch = errors.New("eligibility: result binding does not match request")
)

// SubjectMatterKind names what an eligibility request is asking about.
type SubjectMatterKind uint8

// Subject matter kinds.
const (
	SubjectMatterUnspecified SubjectMatterKind = iota
	SubjectMatterProgram
	SubjectMatterOpportunity
	SubjectMatterAction
)

var subjectMatterWire = map[SubjectMatterKind]string{
	SubjectMatterProgram:     "PROGRAM",
	SubjectMatterOpportunity: "OPPORTUNITY",
	SubjectMatterAction:      "ACTION",
}

// Valid reports whether k is a legal subject-matter kind.
func (k SubjectMatterKind) Valid() bool { return subjectMatterWire[k] != "" }

// String returns the wire token.
func (k SubjectMatterKind) String() string {
	if s, ok := subjectMatterWire[k]; ok {
		return s
	}
	return "SUBJECT_MATTER_UNSPECIFIED"
}

// SubjectMatterRef identifies the exact revision of the program, opportunity
// or action a request is asking about. Eligibility always cites a revision,
// never "the current program": a result computed today must remain
// interpretable after the program is revised tomorrow.
type SubjectMatterRef struct {
	Kind     SubjectMatterKind
	ID       string
	Revision string
}

// Validate reports whether the reference names a kind, id and revision.
func (r SubjectMatterRef) Validate() error {
	if !r.Kind.Valid() {
		return fmt.Errorf("%w: kind %q", ErrRequestSubjectMatter, string(r.Kind.String()))
	}
	if r.ID == "" || r.Revision == "" {
		return fmt.Errorf("%w: id %q revision %q", ErrRequestSubjectMatter, r.ID, r.Revision)
	}
	return nil
}

// String returns a compact "KIND:id@revision" form.
func (r SubjectMatterRef) String() string {
	return fmt.Sprintf("%s:%s@%s", r.Kind, r.ID, r.Revision)
}

// Snapshots cites the exact population, fact and rule snapshots a resolution
// was computed against, so a result can be replayed and audited without
// re-resolving "whatever the population/facts/rules are today".
type Snapshots struct {
	PopulationSnapshotRef string
	FactSnapshotRef       string
	RuleSnapshotRef       string
}

// Validate reports whether all three snapshot references are cited.
func (s Snapshots) Validate() error {
	if s.PopulationSnapshotRef == "" || s.FactSnapshotRef == "" || s.RuleSnapshotRef == "" {
		return ErrRequestSnapshots
	}
	return nil
}

// Request is the immutable, fully-specified question: is Subject eligible for
// SubjectMatter, requested over RequestedInterval, evaluated as of
// EffectiveInterval, in Jurisdiction, against Snapshots, for Purpose, on
// Authority's behalf.
type Request struct {
	Subject           values.EntityRef
	SubjectMatter     SubjectMatterRef
	RequestedInterval values.EffectiveInterval
	EffectiveInterval values.EffectiveInterval
	// KnownAt is optional for backwards-compatible callers. When supplied it
	// pins the knowledge-time at which the population/fact/rule snapshot was
	// assembled; it is carried into every result and its digest.
	KnownAt      values.KnownAt
	Jurisdiction string
	Snapshots    Snapshots
	Purpose      string
	Authority    string
}

// Validate reports whether the request is complete. Every field the RED
// clause of ELIG-001 names is checked independently, so a caller sees exactly
// which one is missing rather than one generic error.
func (r Request) Validate() error {
	if err := r.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrRequestSubject, err)
	}
	if err := r.SubjectMatter.Validate(); err != nil {
		return err
	}
	if err := r.RequestedInterval.Validate(); err != nil {
		return fmt.Errorf("%w: requested: %v", ErrRequestInterval, err)
	}
	if err := r.EffectiveInterval.Validate(); err != nil {
		return fmt.Errorf("%w: effective: %v", ErrRequestInterval, err)
	}
	if r.Jurisdiction == "" {
		return ErrRequestJurisdiction
	}
	if err := r.Snapshots.Validate(); err != nil {
		return err
	}
	if r.Purpose == "" {
		return ErrRequestPurpose
	}
	if r.Authority == "" {
		return ErrRequestAuthority
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when Validate fails.
func (r Request) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	w := writerFor(requestSchema).
		Value("subject", r.Subject).
		String("subject_matter", r.SubjectMatter.String()).
		Value("requested_interval", r.RequestedInterval).
		Value("effective_interval", r.EffectiveInterval).
		String("jurisdiction", r.Jurisdiction).
		String("population_snapshot", r.Snapshots.PopulationSnapshotRef).
		String("fact_snapshot", r.Snapshots.FactSnapshotRef).
		String("rule_snapshot", r.Snapshots.RuleSnapshotRef).
		String("purpose", r.Purpose).
		String("authority", r.Authority)
	if r.KnownAt.Instant().Validate() == nil {
		w.Value("known_at", r.KnownAt.Instant())
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Status is the public eligibility outcome. It is exactly one of four values;
// there is no fifth, and there is no boolean beneath it a caller could read
// instead.
type Status uint8

// Statuses.
const (
	StatusUnspecified Status = iota
	StatusEligible
	StatusIneligible
	StatusConditional
	StatusUnknown
)

var statusWire = map[Status]string{
	StatusEligible:    "ELIGIBLE",
	StatusIneligible:  "INELIGIBLE",
	StatusConditional: "CONDITIONAL",
	StatusUnknown:     "UNKNOWN",
}

// Valid reports whether s is one of the four legal statuses.
func (s Status) Valid() bool { return statusWire[s] != "" }

// String returns the wire token.
func (s Status) String() string {
	if w, ok := statusWire[s]; ok {
		return w
	}
	return "STATUS_UNSPECIFIED"
}

// ReasonKind classifies one entry in a result's explanation.
type ReasonKind uint8

// Reason kinds. Each names both what a leaf contributed (PASSED/FAILED) and,
// for a leaf whose evidence was not determinable, the exact input-level
// provenance (PARTIAL, UNKNOWN or DENIED) rather than one generic gap marker:
// ELIG-004 requires that provenance survive even though the public Status
// only ever collapses to CONDITIONAL or UNKNOWN.
const (
	ReasonUnspecified ReasonKind = iota
	ReasonRuleFailed
	ReasonRulePassed
	ReasonRulePartial
	ReasonRuleUnknown
	ReasonRuleDenied
	ReasonFactFailed
	ReasonFactPassed
	ReasonFactUnknown
	ReasonFactDenied
	ReasonNotApplicable
)

var reasonKindWire = map[ReasonKind]string{
	ReasonRuleFailed:    "RULE_FAILED",
	ReasonRulePassed:    "RULE_PASSED",
	ReasonRulePartial:   "RULE_PARTIAL",
	ReasonRuleUnknown:   "RULE_UNKNOWN",
	ReasonRuleDenied:    "RULE_DENIED",
	ReasonFactFailed:    "FACT_FAILED",
	ReasonFactPassed:    "FACT_PASSED",
	ReasonFactUnknown:   "FACT_UNKNOWN",
	ReasonFactDenied:    "FACT_DENIED",
	ReasonNotApplicable: "NOT_APPLICABLE",
}

// String returns the wire token.
func (k ReasonKind) String() string {
	if s, ok := reasonKindWire[k]; ok {
		return s
	}
	return "REASON_UNSPECIFIED"
}

// Reason is one entry in a result's safe explanation: what contributed to the
// status, without exposing the raw fact value it was computed from.
type Reason struct {
	Kind ReasonKind
	Ref  string // the field name or rule id this reason is about
}

// ObligationReason names why a CONDITIONAL result remains pending.
type ObligationReason uint8

// Obligation reasons.
const (
	ObligationUnspecified ObligationReason = iota
	// ObligationRulePartial means a contributing rule returned PARTIAL.
	ObligationRulePartial
)

// String returns the wire token.
func (o ObligationReason) String() string {
	if o == ObligationRulePartial {
		return "RULE_PARTIAL"
	}
	return "OBLIGATION_UNSPECIFIED"
}

// Obligation is one condition a CONDITIONAL result remains pending on.
type Obligation struct {
	Reason ObligationReason
	Ref    string
}

// Result is the immutable, deterministic answer to one Request.
type Result struct {
	Status                Status
	ProgramVersion        string
	PopulationSnapshotRef string
	FactSnapshotRef       string
	RuleSnapshotRef       string
	EffectiveInterval     values.EffectiveInterval
	KnownAt               values.KnownAt
	Reasons               []Reason
	Obligations           []Obligation
	Evidence              []string
	MissingFacts          []string
	Digest                string
}

// ValidateBinding verifies the immutable provenance carried by a result. It
// is useful when replaying a stored result after a late population entrant,
// removal, or retroactive correction: the old result remains valid evidence,
// but cannot be mistaken for a result for a new request context.
func ValidateBinding(req Request, result Result) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if result.PopulationSnapshotRef != req.Snapshots.PopulationSnapshotRef ||
		result.FactSnapshotRef != req.Snapshots.FactSnapshotRef ||
		result.RuleSnapshotRef != req.Snapshots.RuleSnapshotRef ||
		result.EffectiveInterval != req.EffectiveInterval {
		return ErrBindingMismatch
	}
	// A supplied knowledge time is part of the binding. An unset request time
	// intentionally accepts legacy results that predate this field.
	if req.KnownAt.Instant().Validate() == nil && result.KnownAt != req.KnownAt {
		return ErrBindingMismatch
	}
	return nil
}

// Validate reports whether the result is a complete, well-formed answer. It
// never omits missing facts, reasons, obligations or evidence: the RED clause
// of ELIG-001 is enforced here, not left to a caller's convention.
func (r Result) Validate() error {
	if !r.Status.Valid() {
		return ErrResultStatus
	}
	if r.ProgramVersion == "" || r.FactSnapshotRef == "" || r.RuleSnapshotRef == "" {
		return ErrResultVersions
	}
	if err := r.EffectiveInterval.Validate(); err != nil {
		return fmt.Errorf("eligibility: result effective interval: %w", err)
	}
	if len(r.Reasons) == 0 {
		return ErrResultReasons
	}
	if len(r.Evidence) == 0 {
		return ErrResultEvidence
	}
	if r.Status == StatusConditional && len(r.Obligations) == 0 {
		return ErrResultObligations
	}
	if r.Status == StatusUnknown && len(r.MissingFacts) == 0 {
		return ErrResultMissingFacts
	}
	return nil
}

// Canonical returns the canonical byte encoding of the result, or nil when
// Validate fails.
func (r Result) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	w := writerFor(resultSchema).
		String("status", r.Status.String()).
		String("program_version", r.ProgramVersion).
		String("population_snapshot", r.PopulationSnapshotRef).
		String("fact_snapshot", r.FactSnapshotRef).
		String("rule_snapshot", r.RuleSnapshotRef).
		Value("effective_interval", r.EffectiveInterval).
		Count("reasons", len(r.Reasons))
	for _, reason := range r.Reasons {
		w.String("reason.kind", reason.Kind.String())
		w.String("reason.ref", reason.Ref)
	}
	w.Count("obligations", len(r.Obligations))
	for _, ob := range r.Obligations {
		w.String("obligation.reason", ob.Reason.String())
		w.String("obligation.ref", ob.Ref)
	}
	if r.KnownAt.Instant().Validate() == nil {
		w.Value("known_at", r.KnownAt.Instant())
	}
	w.SortedStrings("evidence", r.Evidence)
	w.SortedStrings("missing_facts", r.MissingFacts)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
