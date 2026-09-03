package dataops

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/engines/effectivedate"
	"github.com/monstercameron/hcm-next/internal/engines/fielddiff"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Operation identity for DATAOPS-007. The effective-date debugger is a
// domain-support operation rather than a catalog BusinessIntent: it is read by
// the DataOps surfaces and by detect_drift, and it is versioned here so a
// stored explanation stays interpretable.
const (
	// ExplainFieldHistoryOperation identifies the debugger operation.
	ExplainFieldHistoryOperation = "hcmnext.dataops.explain_field_history"
	// ExplainFieldHistoryVersion is the operation contract version.
	ExplainFieldHistoryVersion = "v1"
	// HistoryRulePackVersion versions the labelling and redaction rules below.
	// It changes whenever the shape of a timeline, the classification of a
	// change, or the redaction behaviour changes.
	HistoryRulePackVersion = "dataops.history.rules/1.0.0"
)

const (
	assertionSchema   = "hcmnext.domains.dataops.Assertion"
	timelineSchema    = "hcmnext.domains.dataops.FieldTimeline"
	explanationSchema = "hcmnext.domains.dataops.HistoryExplanation"
)

// History errors. All are matchable with errors.Is.
var (
	// ErrAssertionIncomplete is returned when the history port returns an
	// assertion missing its bitemporal coordinates, authority or provenance.
	ErrAssertionIncomplete = errors.New("dataops: assertion is missing effective time, known time, authority or provenance")
	// ErrChangeKindIncoherent is returned when the declared change kind does
	// not match the correction link. A correction that names nothing it
	// corrects, or a first assertion that claims to correct something, would
	// make the lineage unreadable.
	ErrChangeKindIncoherent = errors.New("dataops: change kind and correction link disagree")
	// ErrNarrativeOverflow is returned when a bounded narrative overflows.
	ErrNarrativeOverflow = errors.New("dataops: narrative exceeds its bound")
)

// AssertionClass labels what kind of statement an assertion is. The debugger
// must keep these apart: an observed incumbent value and a locally mastered
// fact can sit on the same timeline, and treating them as interchangeable is
// how an observation quietly becomes a domain fact.
type AssertionClass uint8

// Assertion classes.
const (
	// ClassUnspecified is the zero value and is never legal.
	ClassUnspecified AssertionClass = iota
	// ClassDomainFact is a fact HCM Next holds effective-dated authority over.
	ClassDomainFact
	// ClassExternalObservation is a value observed from a system of record and
	// reported, never owned.
	ClassExternalObservation
	// ClassClaim is an unverified statement, typically from a worker or an
	// importer, that has not been accepted as a fact.
	ClassClaim
	// ClassWorkflowProposal is an intended value carried by a proposal that
	// has not executed.
	ClassWorkflowProposal
	// ClassProjection is a derived value produced by a named projection
	// version. It has no independent authority.
	ClassProjection
)

var classWire = map[AssertionClass]string{
	ClassDomainFact:          "DOMAIN_FACT",
	ClassExternalObservation: "EXTERNAL_OBSERVATION",
	ClassClaim:               "CLAIM",
	ClassWorkflowProposal:    "WORKFLOW_PROPOSAL",
	ClassProjection:          "PROJECTION",
}

// String returns the stable wire token, or "ASSERTION_CLASS_UNSPECIFIED".
func (c AssertionClass) String() string {
	if s, ok := classWire[c]; ok {
		return s
	}
	return "ASSERTION_CLASS_UNSPECIFIED"
}

// Valid reports whether c is a legal assertion class.
func (c AssertionClass) Valid() bool { _, ok := classWire[c]; return ok }

// IsClaim reports whether the class is an unaccepted statement, which a
// request must opt into before it is disclosed.
func (c AssertionClass) IsClaim() bool {
	return c == ClassClaim || c == ClassWorkflowProposal
}

// ChangeKind says why this assertion exists relative to the ones before it.
//
// The distinction between a supersession and a correction is the entire
// product of the effective-date debugger. A supersession means the world
// changed on a date. A correction means the record was wrong about a date it
// had already covered. Presenting the second as the first is how a payroll
// team is told a retroactive fix "never happened".
type ChangeKind uint8

// Change kinds.
const (
	// ChangeUnspecified is the zero value and is never legal.
	ChangeUnspecified ChangeKind = iota
	// ChangeInitial is the first assertion of a field.
	ChangeInitial
	// ChangeSupersession is a new value effective from a later date. It does
	// not correct anything; the earlier value stays true for its own interval.
	ChangeSupersession
	// ChangeCorrection restates an interval the record already covered,
	// because the recorded value was wrong.
	ChangeCorrection
	// ChangeRetraction withdraws an assertion that should never have been
	// made. The withdrawn assertion stays in the history, marked.
	ChangeRetraction
)

var changeWire = map[ChangeKind]string{
	ChangeInitial:      "INITIAL",
	ChangeSupersession: "SUPERSESSION",
	ChangeCorrection:   "CORRECTION",
	ChangeRetraction:   "RETRACTION",
}

// String returns the stable wire token, or "CHANGE_KIND_UNSPECIFIED".
func (k ChangeKind) String() string {
	if s, ok := changeWire[k]; ok {
		return s
	}
	return "CHANGE_KIND_UNSPECIFIED"
}

// Valid reports whether k is a legal change kind.
func (k ChangeKind) Valid() bool { _, ok := changeWire[k]; return ok }

// RewritesHistory reports whether the kind restates something already
// recorded, as opposed to extending the timeline forward.
func (k ChangeKind) RewritesHistory() bool {
	return k == ChangeCorrection || k == ChangeRetraction
}

// Assertion is one effective-dated, recorded statement about one field.
//
// The value is a Presence so that "no value", "explicitly null", "not known",
// "withheld" and "not applicable" stay five distinct answers. ValueKind is
// carried alongside it so the same record can be handed to the comparison
// engine without a second lookup of what type the field is.
type Assertion struct {
	// ID is the stable identity of this assertion within its field history.
	ID    string
	Field FieldID
	Kind  fielddiff.ValueKind
	Value values.Presence[string]

	// Effective is the half-open business interval the assertion covers.
	Effective values.EffectiveInterval
	// KnownAt is when the assertion became available to its authority.
	KnownAt values.KnownAt
	// Revision names the stream position the assertion was read at.
	Revision values.RevisionToken
	// Class labels what kind of statement this is.
	Class AssertionClass
	// Change says why this assertion exists relative to earlier ones.
	Change ChangeKind
	// Corrects is the ID of the assertion this one corrects or retracts, and
	// is empty for an INITIAL or SUPERSESSION.
	Corrects string
	// Authority is the source-authority decision for this assertion.
	Authority evidence.SourceAuthority
	// Provenance is where the assertion came from and when it was recorded.
	Provenance evidence.Provenance
}

// Validate reports whether the assertion is complete enough to disclose.
func (a Assertion) Validate() error {
	if a.ID == "" {
		return fmt.Errorf("%w: assertion has no id", ErrAssertionIncomplete)
	}
	if err := a.Field.Validate(); err != nil {
		return err
	}
	if !a.Kind.Valid() {
		return fmt.Errorf("%w: %s declares no value kind", ErrAssertionIncomplete, a.ID)
	}
	if err := a.Value.Validate(); err != nil {
		return fmt.Errorf("dataops: assertion %s: %w", a.ID, err)
	}
	if err := a.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: %s effective interval: %w", ErrAssertionIncomplete, a.ID, err)
	}
	if a.KnownAt.Canonical() == nil {
		return fmt.Errorf("%w: %s has no known-at", ErrAssertionIncomplete, a.ID)
	}
	if !a.Revision.IsSpecified() {
		return fmt.Errorf("%w: %s has no revision", ErrAssertionIncomplete, a.ID)
	}
	if !a.Class.Valid() {
		return fmt.Errorf("%w: %s has no assertion class", ErrAssertionIncomplete, a.ID)
	}
	if !a.Change.Valid() {
		return fmt.Errorf("%w: %s has no change kind", ErrAssertionIncomplete, a.ID)
	}
	if a.Change.RewritesHistory() != (a.Corrects != "") {
		return fmt.Errorf("%w: %s is %s and corrects %q",
			ErrChangeKindIncoherent, a.ID, a.Change, a.Corrects)
	}
	if err := a.Authority.Validate(); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrAssertionIncomplete, a.ID, err)
	}
	if err := a.Provenance.Validate(); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrAssertionIncomplete, a.ID, err)
	}
	return values.ValidateKnowledgeOrder(a.KnownAt, a.Provenance.RecordedAt, false)
}

// coordinate returns the temporal skeleton the selection engine works on.
func (a Assertion) coordinate() effectivedate.Coordinate {
	return effectivedate.Coordinate{
		ID:         a.ID,
		Effective:  a.Effective,
		KnownAt:    a.KnownAt,
		RecordedAt: a.Provenance.RecordedAt,
		Supersedes: a.Corrects,
	}
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (a Assertion) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	encoded, err := values.MarshalPresence(a.Value, values.StringCodec{})
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New(assertionSchema, dataopsSchemaVer).
		String("id", a.ID).
		String("field", string(a.Field)).
		String("value_kind", a.Kind.String()).
		Field("value", encoded).
		Value("effective", a.Effective).
		Value("known_at", a.KnownAt).
		Value("revision", a.Revision).
		String("class", a.Class.String()).
		String("change", a.Change.String()).
		String("corrects", a.Corrects).
		Value("authority", a.Authority).
		Value("provenance", a.Provenance).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// HistorySet is everything the port could say about one subject's requested
// fields. Exists is separate from len(Assertions) because "this subject does
// not exist" and "this subject exists and has no history for these fields" are
// different answers, and only one is safe to disclose to an unauthorized
// caller.
type HistorySet struct {
	Subject values.EntityRef
	Exists  bool
	// Assertions is the full history for the requested fields, in any order.
	// This package orders it.
	Assertions []Assertion
	// Watermark is the read position the whole set was taken at.
	Watermark values.RevisionToken
}

// Validate reports whether the set is internally consistent.
func (s HistorySet) Validate() error {
	if err := s.Subject.Validate(); err != nil {
		return fmt.Errorf("dataops: history subject: %w", err)
	}
	if !s.Exists {
		if len(s.Assertions) != 0 {
			return fmt.Errorf("dataops: history says the subject does not exist but carries %d assertions",
				len(s.Assertions))
		}
		return nil
	}
	if !s.Watermark.IsSpecified() {
		return fmt.Errorf("%w: history set has no read watermark", ErrAssertionIncomplete)
	}
	seen := make(map[string]struct{}, len(s.Assertions))
	for _, a := range s.Assertions {
		if err := a.Validate(); err != nil {
			return err
		}
		if _, dup := seen[a.ID]; dup {
			return fmt.Errorf("dataops: duplicate assertion id %s", a.ID)
		}
		seen[a.ID] = struct{}{}
	}
	return nil
}

// HistoryQuery is what the field-history port is asked for.
type HistoryQuery struct {
	Tenant  values.TenantId
	Subject values.EntityRef
	// Fields is the exact projection to read. The port must not widen it.
	Fields []FieldID
	// KnownAt is the knowledge cut-off. Assertions that became known after it
	// are invisible, which is how a past belief is reproduced.
	KnownAt values.KnownAt
	// IncludeClaims opts into unaccepted statements: claims and workflow
	// proposals. They are excluded by default because an unaccepted claim
	// displayed next to a domain fact reads as a competing fact.
	IncludeClaims bool
}

// Validate reports whether the query is well formed.
func (q HistoryQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("dataops: history query tenant: %w", err)
	}
	if err := q.Subject.Validate(); err != nil {
		return fmt.Errorf("dataops: history query subject: %w", err)
	}
	if q.Subject.Tenant != q.Tenant {
		return fmt.Errorf("dataops: subject %s is outside tenant %s", q.Subject, q.Tenant)
	}
	if q.KnownAt.Canonical() == nil {
		return fmt.Errorf("dataops: history query known-at is unset")
	}
	_, err := normalizeFields(q.Fields)
	return err
}

// FieldHistory is the bitemporal read port the intent kernel wires to a real
// temporal repository.
//
// The debugger queries this port rather than reconstructing a timeline from
// logs. A timeline rebuilt from logs is a second, weaker temporal
// implementation that will disagree with the first one exactly when it
// matters.
type FieldHistory interface {
	// FieldHistoryAt returns every assertion for the requested fields that was
	// known at or before the query's cut-off. A subject that does not exist is
	// a HistorySet with Exists false, because non-existence is an answer.
	FieldHistoryAt(ctx context.Context, q HistoryQuery) (HistorySet, error)
}

// ExplainedAssertion is one assertion as disclosed, with its position in the
// two timelines made explicit rather than left to be inferred.
type ExplainedAssertion struct {
	Assertion
	// Superseded reports whether a later correction or retraction restates
	// this assertion. It stays in the timeline either way: a correction is
	// lineage, never a deletion.
	Superseded bool
	// SupersededBy is the ID of the correcting assertion, when there is one.
	SupersededBy string
	// EffectiveAtAsOf reports whether this assertion's interval covers the
	// requested business date.
	EffectiveAtAsOf bool
	// KnownAtAsOf reports whether this assertion was already known at the
	// requested knowledge cut-off.
	KnownAtAsOf bool
	// InForce reports whether this is the assertion that wins at the requested
	// bitemporal position.
	InForce bool
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (e ExplainedAssertion) Canonical() []byte {
	body := e.Assertion.Canonical()
	if body == nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.dataops.ExplainedAssertion", dataopsSchemaVer).
		Field("assertion", body).
		Bool("superseded", e.Superseded).
		String("superseded_by", e.SupersededBy).
		Bool("effective_at_as_of", e.EffectiveAtAsOf).
		Bool("known_at_as_of", e.KnownAtAsOf).
		Bool("in_force", e.InForce).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// FieldTimeline is one field's disclosed history.
//
// A denied field carries Access DENIED, a denial reason and no versions at
// all. Effective intervals and provenance name systems, dates and evidence
// artifacts, which is itself information about the withheld value, so they
// travel with the value or not at all.
type FieldTimeline struct {
	Field  FieldID
	Access Access
	// DenialReason is the policy token for a denied field, empty otherwise.
	DenialReason string
	// ValueKind is the declared type of the field, disclosed only when
	// authorized.
	ValueKind fielddiff.ValueKind
	// Versions is the ordered history: effective start, then known-at, then
	// recorded-at, then assertion id.
	Versions []ExplainedAssertion
	// InForceID is the ID of the assertion in force at the requested
	// bitemporal position, empty when the field was not asserted there.
	InForceID string
	// Asserted reports whether any assertion was in force at the position.
	Asserted bool
	// CorrectionCount is how many versions restate an earlier interval.
	CorrectionCount int
}

// InForceAssertion returns the assertion in force at the requested position.
func (t FieldTimeline) InForceAssertion() (ExplainedAssertion, bool) {
	if !t.Asserted {
		return ExplainedAssertion{}, false
	}
	for _, v := range t.Versions {
		if v.ID == t.InForceID {
			return v, true
		}
	}
	return ExplainedAssertion{}, false
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (t FieldTimeline) Canonical() []byte {
	w := canonicalbytes.New(timelineSchema, dataopsSchemaVer).
		String("field", string(t.Field)).
		String("access", t.Access.String()).
		String("denial_reason", t.DenialReason).
		String("value_kind", t.ValueKind.String()).
		Count("versions", len(t.Versions))
	for _, v := range t.Versions {
		w.Value("version", v)
	}
	raw, err := w.
		String("in_force_id", t.InForceID).
		Bool("asserted", t.Asserted).
		Int("correction_count", int64(t.CorrectionCount)).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// HistoryExplanation is the DATAOPS-007 result: ordered assertions with
// effective and recorded intervals, authority class, correction lineage and
// field-level redaction reasons, plus a zero-effect receipt.
type HistoryExplanation struct {
	Operation        string
	OperationVersion string

	Subject    values.EntityRef
	Exists     bool
	Disclosure Disclosure
	// WithheldReason is the policy token when Disclosure is WITHHELD.
	WithheldReason string

	// AsOfEffective is the business date the "what was effective" question was
	// asked about.
	AsOfEffective values.LocalDate
	// AsKnownAt is the knowledge cut-off the "what did we know" question was
	// asked at.
	AsKnownAt values.KnownAt
	// IncludeClaims records whether unaccepted statements were requested.
	IncludeClaims bool

	// Fields is one timeline per requested field, in sorted field order.
	Fields []FieldTimeline
	// Watermark is the read position every disclosed assertion was taken at.
	Watermark values.RevisionToken
	// Narrative is a bounded, value-free description of what was answered.
	Narrative []string

	PolicyVersion   string
	RulePackVersion string
	InputsDigest    string
	ResultDigest    string
	Effects         evidence.EffectCounters
	Receipt         evidence.ZeroEffectReceipt
}

// Timeline returns the disclosed timeline for a field.
func (e HistoryExplanation) Timeline(field FieldID) (FieldTimeline, bool) {
	for _, t := range e.Fields {
		if t.Field == field {
			return t, true
		}
	}
	return FieldTimeline{}, false
}

// canonicalBody encodes everything except the receipt, which cites the digest
// of this body and therefore cannot be inside it.
func (e HistoryExplanation) canonicalBody() ([]byte, error) {
	w := canonicalbytes.New(explanationSchema, dataopsSchemaVer).
		String("operation", e.Operation).
		String("operation_version", e.OperationVersion).
		Value("subject", e.Subject).
		Bool("exists", e.Exists).
		String("disclosure", e.Disclosure.String()).
		String("withheld_reason", e.WithheldReason)
	if e.Disclosure != DisclosureWithheld {
		w.Value("as_of_effective", e.AsOfEffective).
			Value("as_known_at", e.AsKnownAt)
	}
	w.Bool("include_claims", e.IncludeClaims).
		Count("fields", len(e.Fields))
	for _, t := range e.Fields {
		w.Value("field", t)
	}
	w.Bool("watermark?", e.Watermark.IsSpecified())
	if e.Watermark.IsSpecified() {
		w.Value("watermark", e.Watermark)
	}
	w.Count("narrative", len(e.Narrative))
	for _, line := range e.Narrative {
		w.String("narrative", line)
	}
	return w.
		String("policy_version", e.PolicyVersion).
		String("rule_pack_version", e.RulePackVersion).
		Value("effects", e.Effects).
		Bytes()
}

// Canonical returns the canonical byte encoding of the whole explanation
// including its receipt, or nil when the explanation is incoherent.
func (e HistoryExplanation) Canonical() []byte {
	body, err := e.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.dataops.HistoryExplanationEnvelope", dataopsSchemaVer).
		Field("body", body).
		String("inputs_digest", e.InputsDigest).
		Value("receipt", e.Receipt).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ExplainFieldHistoryRequest is the governed debugger request.
//
// The two temporal coordinates are separate parameters because they are
// separate questions. AsOfEffective asks what was true in the business world
// on a date; AsKnownAt asks what the record could have told you at a moment.
// A debugger that accepted one coordinate would silently answer whichever
// question its implementation happened to prefer.
type ExplainFieldHistoryRequest struct {
	Tenant        values.TenantId
	Subject       values.EntityRef
	Fields        []FieldID
	AsOfEffective values.LocalDate
	AsKnownAt     values.KnownAt
	IncludeClaims bool
	Authorization Authorization
}

// Validate reports whether the request is well formed.
func (r ExplainFieldHistoryRequest) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrRequestInvalid, err)
	}
	if err := r.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %w", ErrRequestInvalid, err)
	}
	if r.Subject.Tenant != r.Tenant {
		return fmt.Errorf("%w: subject %s is outside tenant %s", ErrRequestInvalid, r.Subject, r.Tenant)
	}
	if err := r.AsOfEffective.Validate(); err != nil {
		return fmt.Errorf("%w: as-of effective date: %w", ErrRequestInvalid, err)
	}
	if r.AsKnownAt.Canonical() == nil {
		return fmt.Errorf("%w: as-known-at is unset", ErrRequestInvalid)
	}
	if _, err := normalizeFields(r.Fields); err != nil {
		return fmt.Errorf("%w: %w", ErrRequestInvalid, err)
	}
	return r.Authorization.Validate()
}

// inputsDigest digests exactly what the answer depends on.
func (r ExplainFieldHistoryRequest) inputsDigest() (string, error) {
	fields, err := normalizeFields(r.Fields)
	if err != nil {
		return "", err
	}
	w := canonicalbytes.New("hcmnext.domains.dataops.ExplainFieldHistoryRequest", dataopsSchemaVer).
		String("operation", ExplainFieldHistoryOperation).
		String("operation_version", ExplainFieldHistoryVersion).
		String("tenant", string(r.Tenant)).
		Value("subject", r.Subject).
		Value("as_of_effective", r.AsOfEffective).
		Value("as_known_at", r.AsKnownAt).
		Bool("include_claims", r.IncludeClaims).
		Count("fields", len(fields))
	for _, f := range fields {
		w.String("field", string(f))
	}
	return w.Value("authorization", r.Authorization).Digest()
}

// ExplainFieldHistory is the DATAOPS-007 entry point: one governed read that
// returns, per requested field, the ordered assertions with their effective
// and recorded coordinates, authority class, correction lineage, and the
// per-field redaction reason - and writes nothing.
//
// Four refusals are load-bearing:
//
//   - A subject the caller may not know about returns a WITHHELD explanation
//     with no fields and no existence answer.
//   - A field the decision does not rule on is an error, not a default.
//   - A denied field is returned by name with no versions at all.
//   - A correction never removes what it corrects. The superseded assertion
//     stays on the timeline marked Superseded, because "we used to believe
//     this" is the answer the debugger exists to give.
func ExplainFieldHistory(
	ctx context.Context,
	reader FieldHistory,
	req ExplainFieldHistoryRequest,
) (HistoryExplanation, error) {
	if reader == nil {
		return HistoryExplanation{}, fmt.Errorf("%w: no field history reader", ErrRequestInvalid)
	}
	if err := req.Validate(); err != nil {
		return HistoryExplanation{}, err
	}
	fields, err := normalizeFields(req.Fields)
	if err != nil {
		return HistoryExplanation{}, err
	}
	if err := req.Authorization.Covers(fields); err != nil {
		return HistoryExplanation{}, err
	}
	inputsDigest, err := req.inputsDigest()
	if err != nil {
		return HistoryExplanation{}, err
	}

	if !req.Authorization.SubjectDisclosable {
		return finishHistory(HistoryExplanation{
			Operation:        ExplainFieldHistoryOperation,
			OperationVersion: ExplainFieldHistoryVersion,
			Subject:          req.Subject,
			Disclosure:       DisclosureWithheld,
			WithheldReason:   req.Authorization.SubjectDenialReason,
			AsOfEffective:    req.AsOfEffective,
			AsKnownAt:        req.AsKnownAt,
			IncludeClaims:    req.IncludeClaims,
			Narrative: []string{
				"subject is not disclosable to this caller under the evaluated policy",
			},
			PolicyVersion:   req.Authorization.PolicyVersion,
			RulePackVersion: HistoryRulePackVersion,
			InputsDigest:    inputsDigest,
			Effects:         evidence.ZeroEffects(),
		})
	}

	// Only fields the caller may see are ever read. Handing the full
	// projection to the port and filtering afterwards would mean a denied
	// field had already been loaded into this process.
	allowed := req.Authorization.AllowedFields(fields)

	set := HistorySet{Subject: req.Subject, Exists: true, Watermark: values.UnspecifiedRevision()}
	if len(allowed) > 0 {
		set, err = reader.FieldHistoryAt(ctx, HistoryQuery{
			Tenant:        req.Tenant,
			Subject:       req.Subject,
			Fields:        allowed,
			KnownAt:       req.AsKnownAt,
			IncludeClaims: req.IncludeClaims,
		})
		if err != nil {
			return HistoryExplanation{}, fmt.Errorf("%w: %w", ErrReaderFailed, err)
		}
		if err := set.Validate(); err != nil {
			return HistoryExplanation{}, err
		}
		if set.Subject != req.Subject {
			return HistoryExplanation{}, fmt.Errorf("%w: asked %s, answered %s",
				ErrPortWidenedProjection, req.Subject, set.Subject)
		}
		for _, a := range set.Assertions {
			if !req.Authorization.Allows(a.Field) {
				return HistoryExplanation{}, fmt.Errorf("%w: history port returned %s",
					ErrPortWidenedProjection, a.Field)
			}
		}
	}

	byField := make(map[FieldID][]Assertion, len(allowed))
	for _, a := range set.Assertions {
		// A claim the caller did not opt into is not disclosed. It is dropped
		// here rather than trusted to the port, so that a permissive port
		// cannot widen what the request asked for.
		if a.Class.IsClaim() && !req.IncludeClaims {
			continue
		}
		byField[a.Field] = append(byField[a.Field], a)
	}

	timelines := make([]FieldTimeline, 0, len(fields))
	denied := 0
	for _, field := range fields {
		ruling, _ := req.Authorization.RulingFor(field)
		if ruling.Effect == EffectDeny {
			denied++
			timelines = append(timelines, FieldTimeline{
				Field:        field,
				Access:       AccessDenied,
				DenialReason: ruling.Reason,
			})
			continue
		}
		timeline, err := buildTimeline(field, byField[field], req.AsOfEffective, req.AsKnownAt)
		if err != nil {
			return HistoryExplanation{}, err
		}
		timelines = append(timelines, timeline)
	}

	disclosure := DisclosureFull
	if denied > 0 {
		disclosure = DisclosurePartial
	}

	return finishHistory(HistoryExplanation{
		Operation:        ExplainFieldHistoryOperation,
		OperationVersion: ExplainFieldHistoryVersion,
		Subject:          req.Subject,
		Exists:           set.Exists,
		Disclosure:       disclosure,
		AsOfEffective:    req.AsOfEffective,
		AsKnownAt:        req.AsKnownAt,
		IncludeClaims:    req.IncludeClaims,
		Fields:           timelines,
		Watermark:        set.Watermark,
		Narrative:        narrateHistory(set.Exists, req, timelines, denied),
		PolicyVersion:    req.Authorization.PolicyVersion,
		RulePackVersion:  HistoryRulePackVersion,
		InputsDigest:     inputsDigest,
		Effects:          evidence.ZeroEffects(),
	})
}

// buildTimeline orders one field's assertions and labels each one's position
// in both timelines.
func buildTimeline(
	field FieldID,
	assertions []Assertion,
	on values.LocalDate,
	cut values.KnownAt,
) (FieldTimeline, error) {
	timeline := FieldTimeline{Field: field, Access: AccessAuthorized}
	if len(assertions) == 0 {
		return timeline, nil
	}

	coords := make([]effectivedate.Coordinate, 0, len(assertions))
	byID := make(map[string]Assertion, len(assertions))
	for _, a := range assertions {
		coords = append(coords, a.coordinate())
		byID[a.ID] = a
	}
	line, err := effectivedate.NewTimeline(coords)
	if err != nil {
		return FieldTimeline{}, fmt.Errorf("dataops: field %s: %w", field, err)
	}

	// An unaccepted claim or proposal is displayed on the timeline and is
	// never in force. Letting one win the bitemporal selection would turn
	// "somebody said so" into "the record says so", which is the failure the
	// class labels exist to prevent.
	accepted := make([]effectivedate.Coordinate, 0, len(coords))
	acceptedIDs := make(map[string]struct{}, len(coords))
	for _, c := range coords {
		if byID[c.ID].Class.IsClaim() {
			continue
		}
		acceptedIDs[c.ID] = struct{}{}
		accepted = append(accepted, c)
	}
	for i := range accepted {
		if _, ok := acceptedIDs[accepted[i].Supersedes]; !ok {
			accepted[i].Supersedes = ""
		}
	}
	acceptedLine, err := effectivedate.NewTimeline(accepted)
	if err != nil {
		return FieldTimeline{}, fmt.Errorf("dataops: field %s: %w", field, err)
	}
	inForce, asserted, err := acceptedLine.InForce(on, cut)
	if err != nil {
		return FieldTimeline{}, fmt.Errorf("dataops: field %s: %w", field, err)
	}

	kinds := make(map[fielddiff.ValueKind]struct{}, 1)
	versions := make([]ExplainedAssertion, 0, len(assertions))
	corrections := 0
	for _, c := range line.Ordered() {
		a := byID[c.ID]
		kinds[a.Kind] = struct{}{}
		covers, err := a.Effective.ContainsDate(on)
		if err != nil {
			return FieldTimeline{}, fmt.Errorf("dataops: field %s assertion %s: %w", field, a.ID, err)
		}
		supersededBy, superseded := line.SupersededBy(c.ID)
		if a.Change.RewritesHistory() {
			corrections++
		}
		versions = append(versions, ExplainedAssertion{
			Assertion:       a,
			Superseded:      superseded,
			SupersededBy:    supersededBy,
			EffectiveAtAsOf: covers,
			KnownAtAsOf:     !a.KnownAt.Instant().After(cut.Instant()),
			InForce:         asserted && c.ID == inForce.ID,
		})
	}
	if len(kinds) > 1 {
		return FieldTimeline{}, fmt.Errorf("%w: field %s has %d declared value kinds in one history",
			ErrAssertionIncomplete, field, len(kinds))
	}
	for k := range kinds {
		timeline.ValueKind = k
	}

	timeline.Versions = versions
	timeline.Asserted = asserted
	timeline.CorrectionCount = corrections
	if asserted {
		timeline.InForceID = inForce.ID
	}
	return timeline, nil
}

// narrateHistory builds the bounded, value-free narrative. It describes shape
// and coordinates only: no line ever contains a field value, because the
// narrative is not subject to the per-field rulings.
func narrateHistory(
	exists bool,
	req ExplainFieldHistoryRequest,
	timelines []FieldTimeline,
	denied int,
) []string {
	versions, corrections, asserted := 0, 0, 0
	for _, t := range timelines {
		versions += len(t.Versions)
		corrections += t.CorrectionCount
		if t.Asserted {
			asserted++
		}
	}
	lines := []string{
		fmt.Sprintf("subject exists at the requested coordinate: %t", exists),
		fmt.Sprintf("effective on %s, as known at %s", req.AsOfEffective, req.AsKnownAt),
		fmt.Sprintf("%d field(s) requested, %d authorized, %d denied",
			len(timelines), len(timelines)-denied, denied),
		fmt.Sprintf("%d version(s) disclosed, %d in force, %d correction(s) retained",
			versions, asserted, corrections),
	}
	if corrections > 0 {
		lines = append(lines,
			"a correction supersedes an earlier assertion; the superseded version is retained, not deleted")
	}
	if denied > 0 {
		lines = append(lines,
			"denied fields are reported by name with no versions, coordinates or provenance")
	}
	if req.IncludeClaims {
		lines = append(lines, "unaccepted claims and workflow proposals are included and labelled")
	}
	return lines
}

// finishHistory digests the body, mints the zero-effect receipt and returns
// the completed explanation.
func finishHistory(e HistoryExplanation) (HistoryExplanation, error) {
	if len(e.Narrative) > MaxNarrativeLines {
		return HistoryExplanation{}, fmt.Errorf("%w: %d lines", ErrNarrativeOverflow, len(e.Narrative))
	}
	sort.SliceStable(e.Fields, func(i, j int) bool { return e.Fields[i].Field < e.Fields[j].Field })
	body, err := e.canonicalBody()
	if err != nil {
		return HistoryExplanation{}, err
	}
	e.ResultDigest = canonicalbytes.Digest(body)
	receipt, err := evidence.NewZeroEffectReceipt(
		e.Operation, e.OperationVersion,
		evidence.ModeSimulate,
		evidence.RequestStateSimulated,
		[]evidence.ControlVersion{
			{Name: "authorization_policy", Version: e.PolicyVersion},
			{Name: "history_rule_pack", Version: e.RulePackVersion},
		},
		e.InputsDigest, e.ResultDigest, e.Effects,
	)
	if err != nil {
		return HistoryExplanation{}, err
	}
	e.Receipt = receipt
	return e, nil
}
