package dataops

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/fielddiff"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Rule pack identity for DATAOPS-008. The comparison rules and the repair-safe
// classification are versioned together, because a stored finding must remain
// interpretable by the rules that produced it.
const (
	// DiffRulePackVersion versions the comparison and safety rules below.
	DiffRulePackVersion = "dataops.diff.rules/1.0.0"
)

const (
	findingSchema  = "hcmnext.domains.dataops.FieldFinding"
	diffSchema     = "hcmnext.domains.dataops.RecordDiff"
	canonFldSchema = "hcmnext.domains.dataops.CanonicalField"
	canonRecSchema = "hcmnext.domains.dataops.CanonicalRecord"
	freshSchema    = "hcmnext.domains.dataops.FreshnessPolicy"
)

// Diff errors. All are matchable with errors.Is.
var (
	// ErrCanonicalIncomplete is returned when the canonical side of a
	// comparison is missing its authority, provenance or revision.
	ErrCanonicalIncomplete = errors.New("dataops: canonical record is missing authority, provenance or revision")
	// ErrFreshnessPolicy is returned for an unusable freshness policy.
	ErrFreshnessPolicy = errors.New("dataops: freshness policy is invalid")
	// ErrEvaluationTime is returned for a missing or incoherent evaluation
	// time. The comparison never reads a clock; the caller supplies the
	// instant the comparison is judged at, and it must not precede the
	// observation it is judging.
	ErrEvaluationTime = errors.New("dataops: evaluation time is unset or precedes the observation")
	// ErrSubjectMismatch is returned when the two sides of a comparison are
	// about different subjects.
	ErrSubjectMismatch = errors.New("dataops: canonical and observed records are about different subjects")
)

// Verdict is the classification of one field comparison. The vocabulary is
// closed and every field always gets exactly one.
//
// The distinction that matters commercially is between MISMATCH and the three
// epistemic verdicts. A stale, unknown or redacted value is not a
// disagreement; reporting it as one produces a repair queue full of work that
// consists of overwriting good data with old data.
type Verdict uint8

// Verdicts.
const (
	// VerdictUnspecified is the zero value and is never a legal result.
	VerdictUnspecified Verdict = iota
	// VerdictMatch means both sides agree.
	VerdictMatch
	// VerdictMismatch means both sides were readable, fresh and comparable,
	// and they disagree.
	VerdictMismatch
	// VerdictStale means the observation is older than the freshness policy
	// allows, so any disagreement may already have been resolved.
	VerdictStale
	// VerdictUnknown means at least one side could not be read: the source
	// said nothing, or reported the value as unknown or unavailable.
	VerdictUnknown
	// VerdictRedacted means the caller is not authorized to see the field, so
	// no comparison was performed.
	VerdictRedacted
	// VerdictNotApplicable means the field does not apply to this subject on
	// at least one side.
	VerdictNotApplicable
)

var verdictWire = map[Verdict]string{
	VerdictMatch:         "MATCH",
	VerdictMismatch:      "MISMATCH",
	VerdictStale:         "STALE",
	VerdictUnknown:       "UNKNOWN",
	VerdictRedacted:      "REDACTED",
	VerdictNotApplicable: "NOT_APPLICABLE",
}

// String returns the stable wire token, or "VERDICT_UNSPECIFIED".
func (v Verdict) String() string {
	if s, ok := verdictWire[v]; ok {
		return s
	}
	return "VERDICT_UNSPECIFIED"
}

// Valid reports whether v is a legal verdict.
func (v Verdict) Valid() bool { _, ok := verdictWire[v]; return ok }

// Decided reports whether the verdict rests on a completed comparison of two
// readable, fresh values.
func (v Verdict) Decided() bool { return v == VerdictMatch || v == VerdictMismatch }

// AllVerdicts returns every legal verdict in canonical order.
func AllVerdicts() []Verdict {
	return []Verdict{
		VerdictMatch, VerdictMismatch, VerdictStale,
		VerdictUnknown, VerdictRedacted, VerdictNotApplicable,
	}
}

// RepairSafety is whether a mismatch could be corrected mechanically without a
// human ruling on it. It is a recommendation attached to a finding. Nothing in
// this package acts on it, and REPAIR_SAFE never means "repaired".
type RepairSafety uint8

// Repair safety classes.
const (
	// SafetyUnspecified is the zero value and is never a legal result.
	SafetyUnspecified RepairSafety = iota
	// SafetyNotRequired means there is nothing to repair.
	SafetyNotRequired
	// SafetySafe means the authority is unambiguous, the direction of the
	// correction follows from it, and the evidence orders the two sides.
	SafetySafe
	// SafetyUnsafe means a correction would need a human ruling: the authority
	// points the other way, the sides are unordered, or the two sides do not
	// even agree on the type of the value.
	SafetyUnsafe
	// SafetyUndecidable means the comparison itself did not complete, so there
	// is nothing to be safe or unsafe about yet.
	SafetyUndecidable
)

var safetyWire = map[RepairSafety]string{
	SafetyNotRequired: "NO_REPAIR_REQUIRED",
	SafetySafe:        "REPAIR_SAFE",
	SafetyUnsafe:      "REPAIR_UNSAFE",
	SafetyUndecidable: "REPAIR_UNDECIDABLE",
}

// String returns the stable wire token, or "REPAIR_SAFETY_UNSPECIFIED".
func (s RepairSafety) String() string {
	if w, ok := safetyWire[s]; ok {
		return w
	}
	return "REPAIR_SAFETY_UNSPECIFIED"
}

// Valid reports whether s is a legal safety class.
func (s RepairSafety) Valid() bool { _, ok := safetyWire[s]; return ok }

// Capability identifiers a finding may point at as the allowed next step.
// They are the capability names the intent catalog already declares; a
// diagnostic never invents a capability, because a name nobody grants is a
// dead end dressed up as an action.
const (
	// CapabilityDetectDrift re-runs the comparison over fresher observations.
	CapabilityDetectDrift = "reconciliation.drift.detect/v1"
	// CapabilityCreateRepairPlan produces a non-executable repair plan.
	CapabilityCreateRepairPlan = "repair.plan.create/v1"
	// CapabilitySimulateRepair simulates a repair plan.
	CapabilitySimulateRepair = "repair.plan.simulate/v1"
	// CapabilityExplainTransaction reconstructs the authorized chronology
	// behind a disputed value.
	CapabilityExplainTransaction = "provenance.transaction.explain/v1"
)

// Reason tokens for verdicts and safety classes. Stable identifiers, never
// prose, and never containing a compared value.
const (
	// ReasonNoObservation is a source that has no record for the subject.
	ReasonNoObservation = "source_has_no_record_for_subject"
	// ReasonFieldNotObserved is a source record that omits the field.
	ReasonFieldNotObserved = "source_record_omits_field"
	// ReasonFieldNotCanonical is a canonical record with no assertion for the
	// field at the requested coordinate.
	ReasonFieldNotCanonical = "canonical_record_omits_field"
	// ReasonFieldDenied is a field the caller may not see.
	ReasonFieldDenied = "field_denied_to_this_caller"
	// ReasonObservationStale is an observation older than the policy allows.
	ReasonObservationStale = "observation_older_than_freshness_policy"
	// ReasonSideUnreadable is a side reported as unknown or unavailable.
	ReasonSideUnreadable = "side_value_is_not_readable"
	// ReasonNotApplicable is a field that does not apply to the subject.
	ReasonNotApplicable = "field_not_applicable_to_subject"
	// ReasonSidesAgree is a completed comparison that matched.
	ReasonSidesAgree = "sides_agree"
	// ReasonAuthorityLocalExternalAhead is an external system that changed a
	// locally mastered field.
	ReasonAuthorityLocalExternalAhead = "external_changed_locally_mastered_field"
	// ReasonAuthorityLocalCanonicalAhead is a locally mastered field the
	// external system has not caught up with.
	ReasonAuthorityLocalCanonicalAhead = "external_behind_locally_mastered_field"
	// ReasonAuthorityExternalCanonicalAhead is a projection of an externally
	// mastered field that is somehow newer than its master.
	ReasonAuthorityExternalCanonicalAhead = "projection_ahead_of_external_master"
	// ReasonAuthorityExternalAhead is an externally mastered field whose
	// projection is behind.
	ReasonAuthorityExternalAhead = "projection_behind_external_master"
	// ReasonDerivedNotRepairable is a derived value, which is recomputed
	// rather than repaired.
	ReasonDerivedNotRepairable = "derived_value_is_recomputed_not_repaired"
	// ReasonUnordered is a disagreement no evidence orders.
	ReasonUnordered = "disagreement_without_update_ordering"
	// ReasonTypeMismatch is a declared-kind disagreement, which is a mapping
	// defect rather than a data defect.
	ReasonTypeMismatch = "declared_value_kinds_differ"
	// ReasonMissingSide is a value present on exactly one side.
	ReasonMissingSide = "value_present_on_one_side_only"
)

// FreshnessPolicy is the versioned rule that decides when an observation is
// too old to found a mismatch on.
//
// The age bound is in seconds rather than a duration type so the policy has
// one canonical encoding and can be pinned in a receipt as a control version.
type FreshnessPolicy struct {
	Version string
	// MaxAgeSeconds is the largest age an observation may have and still
	// support a MISMATCH. Zero means no observation is ever fresh enough,
	// which is rejected: a policy that can never pass is a policy nobody
	// noticed was misconfigured.
	MaxAgeSeconds int64
}

// Validate reports whether the policy is usable.
func (p FreshnessPolicy) Validate() error {
	if p.Version == "" {
		return fmt.Errorf("%w: version is required", ErrFreshnessPolicy)
	}
	if p.MaxAgeSeconds <= 0 {
		return fmt.Errorf("%w: max age %ds must be positive", ErrFreshnessPolicy, p.MaxAgeSeconds)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (p FreshnessPolicy) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New(freshSchema, dataopsSchemaVer).
		String("version", p.Version).
		Int("max_age_seconds", p.MaxAgeSeconds).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// fresh reports whether an observation retrieved at retrieved is still fresh
// when judged at evaluated.
func (p FreshnessPolicy) fresh(retrieved values.RecordedAt, evaluated values.Instant) (bool, error) {
	if err := p.Validate(); err != nil {
		return false, err
	}
	if !evaluated.IsSet() {
		return false, fmt.Errorf("%w: evaluation instant is unset", ErrEvaluationTime)
	}
	retrievedSec, _ := retrieved.Instant().Unix()
	evaluatedSec, _ := evaluated.Unix()
	if evaluatedSec < retrievedSec {
		return false, fmt.Errorf("%w: evaluated %s before retrieval %s",
			ErrEvaluationTime, evaluated, retrieved)
	}
	return evaluatedSec-retrievedSec <= p.MaxAgeSeconds, nil
}

// CanonicalField is one field of the Human Capital Management Suite side of a comparison, carrying
// the evidence that makes it citable: which authority owns it, where it came
// from, and which revision it was read at.
type CanonicalField struct {
	Field FieldID
	Kind  fielddiff.ValueKind
	Value values.Presence[string]
	// UpdatedAt is when the canonical side last changed the field.
	UpdatedAt values.Instant
	// Effective is the business interval the value covers.
	Effective values.EffectiveInterval
	// KnownAt is when the value became known to its authority.
	KnownAt  values.KnownAt
	Revision values.RevisionToken
	// Authority is the source-authority decision for this field. It is what
	// decides which direction a repair could even point.
	Authority evidence.SourceAuthority
	// Provenance is where the value came from and when it was recorded.
	Provenance evidence.Provenance
}

// Validate reports whether the canonical field is complete.
func (f CanonicalField) Validate() error {
	if err := f.Field.Validate(); err != nil {
		return err
	}
	if !f.Kind.Valid() {
		return fmt.Errorf("%w: field %s declares no value kind", ErrCanonicalIncomplete, f.Field)
	}
	if err := f.Value.Validate(); err != nil {
		return fmt.Errorf("dataops: canonical field %s: %w", f.Field, err)
	}
	if err := f.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: field %s effective interval: %w", ErrCanonicalIncomplete, f.Field, err)
	}
	if f.KnownAt.Canonical() == nil {
		return fmt.Errorf("%w: field %s has no known-at", ErrCanonicalIncomplete, f.Field)
	}
	if !f.Revision.IsSpecified() {
		return fmt.Errorf("%w: field %s has no revision", ErrCanonicalIncomplete, f.Field)
	}
	if err := f.Authority.Validate(); err != nil {
		return fmt.Errorf("%w: field %s: %w", ErrCanonicalIncomplete, f.Field, err)
	}
	if err := f.Provenance.Validate(); err != nil {
		return fmt.Errorf("%w: field %s: %w", ErrCanonicalIncomplete, f.Field, err)
	}
	if f.UpdatedAt.IsSet() {
		if err := f.UpdatedAt.Validate(); err != nil {
			return fmt.Errorf("%w: field %s updated_at: %w", ErrCanonicalIncomplete, f.Field, err)
		}
	}
	return nil
}

// side returns the comparison side for this canonical value.
func (f CanonicalField) side() fielddiff.Side {
	return fielddiff.Side{Kind: f.Kind, Value: f.Value, UpdatedAt: f.UpdatedAt}
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (f CanonicalField) Canonical() []byte {
	if f.Validate() != nil {
		return nil
	}
	encoded, err := values.MarshalPresence(f.Value, values.StringCodec{})
	if err != nil {
		return nil
	}
	w := canonicalbytes.New(canonFldSchema, dataopsSchemaVer).
		String("field", string(f.Field)).
		String("value_kind", f.Kind.String()).
		Field("value", encoded).
		Bool("updated_at?", f.UpdatedAt.IsSet())
	if f.UpdatedAt.IsSet() {
		w.Value("updated_at", f.UpdatedAt)
	}
	raw, err := w.
		Value("effective", f.Effective).
		Value("known_at", f.KnownAt).
		Value("revision", f.Revision).
		Value("authority", f.Authority).
		Value("provenance", f.Provenance).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// CanonicalRecord is the Human Capital Management Suite side of a comparison for one subject at one
// bitemporal coordinate.
type CanonicalRecord struct {
	Subject values.EntityRef
	Exists  bool
	Fields  []CanonicalField
	// Watermark is the read position the whole record was taken at.
	Watermark values.RevisionToken
	// AsOfEffective and AsKnownAt are the coordinate the record was projected
	// at. They travel with it so a finding can say which "now" it compared.
	AsOfEffective values.LocalDate
	AsKnownAt     values.KnownAt
}

// Validate reports whether the record is complete.
func (r CanonicalRecord) Validate() error {
	if err := r.Subject.Validate(); err != nil {
		return fmt.Errorf("dataops: canonical record subject: %w", err)
	}
	if err := r.AsOfEffective.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrCanonicalIncomplete, err)
	}
	if r.AsKnownAt.Canonical() == nil {
		return fmt.Errorf("%w: canonical record has no known-at coordinate", ErrCanonicalIncomplete)
	}
	if !r.Exists {
		if len(r.Fields) != 0 {
			return fmt.Errorf("dataops: canonical record says the subject is absent but carries %d fields",
				len(r.Fields))
		}
		return nil
	}
	if !r.Watermark.IsSpecified() {
		return fmt.Errorf("%w: canonical record has no read watermark", ErrCanonicalIncomplete)
	}
	seen := make(map[FieldID]struct{}, len(r.Fields))
	for _, f := range r.Fields {
		if err := f.Validate(); err != nil {
			return err
		}
		if _, dup := seen[f.Field]; dup {
			return fmt.Errorf("%w: canonical field %s", ErrObservationDuplicate, f.Field)
		}
		seen[f.Field] = struct{}{}
	}
	return nil
}

// Lookup returns the canonical field, if the record carries one.
func (r CanonicalRecord) Lookup(field FieldID) (CanonicalField, bool) {
	for _, f := range r.Fields {
		if f.Field == field {
			return f, true
		}
	}
	return CanonicalField{}, false
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (r CanonicalRecord) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	sorted := append([]CanonicalField(nil), r.Fields...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Field < sorted[j].Field })
	w := canonicalbytes.New(canonRecSchema, dataopsSchemaVer).
		Value("subject", r.Subject).
		Bool("exists", r.Exists).
		Value("as_of_effective", r.AsOfEffective).
		Value("as_known_at", r.AsKnownAt).
		Count("fields", len(sorted))
	for _, f := range sorted {
		w.Value("field", f)
	}
	w.Bool("watermark?", r.Watermark.IsSpecified())
	if r.Watermark.IsSpecified() {
		w.Value("watermark", r.Watermark)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// ProjectCanonicalRecord turns a disclosed field-history explanation into the
// canonical side of a comparison, taking each field's in-force assertion at
// the coordinate the explanation was produced for.
//
// This is why DATAOPS-008 depends on DATAOPS-007 rather than reading a
// projection of its own: the value a diff compares must be the same value the
// debugger would explain, selected by the same bitemporal rule. Two selection
// implementations would disagree exactly on corrections, which is the case
// operators care about.
//
// Denied fields are not projected. Comparing a field the caller may not see
// would leak whether it matches.
func ProjectCanonicalRecord(e HistoryExplanation) (CanonicalRecord, error) {
	if e.Disclosure == DisclosureWithheld {
		return CanonicalRecord{}, fmt.Errorf("%w: explanation is withheld", ErrCanonicalIncomplete)
	}
	record := CanonicalRecord{
		Subject:       e.Subject,
		Exists:        e.Exists,
		Watermark:     e.Watermark,
		AsOfEffective: e.AsOfEffective,
		AsKnownAt:     e.AsKnownAt,
	}
	if !e.Exists {
		return record, record.Validate()
	}
	for _, t := range e.Fields {
		if t.Access != AccessAuthorized {
			continue
		}
		version, ok := t.InForceAssertion()
		if !ok {
			continue
		}
		record.Fields = append(record.Fields, CanonicalField{
			Field:      t.Field,
			Kind:       version.Kind,
			Value:      version.Value,
			UpdatedAt:  version.Provenance.RecordedAt.Instant(),
			Effective:  version.Effective,
			KnownAt:    version.KnownAt,
			Revision:   version.Revision,
			Authority:  version.Authority,
			Provenance: version.Provenance,
		})
	}
	sort.Slice(record.Fields, func(i, j int) bool { return record.Fields[i].Field < record.Fields[j].Field })
	return record, record.Validate()
}

// FieldFinding is the comparison of one field, with everything a reader needs
// to decide whether to believe it: the verdict, the relation the engine
// found, the sources and timestamps behind both sides, the repair-safe
// classification with its reason, and the capabilities that may legitimately
// be invoked next.
//
// No finding carries a compared value. A drift report travels to dashboards,
// tickets and exports; the values it compared do not have to travel with it,
// and a field's authorization ruling would not survive the trip.
type FieldFinding struct {
	Subject values.EntityRef
	Field   FieldID
	Access  Access
	Verdict Verdict
	// Relation is what the comparison engine found. It is
	// RELATION_UNSPECIFIED whenever the comparison did not complete.
	Relation fielddiff.Relation
	// Reason is the stable token explaining the verdict.
	Reason string
	// Safety is the repair-safe classification, and SafetyReason the token
	// explaining it. Neither authorizes anything.
	Safety       RepairSafety
	SafetyReason string

	// CanonicalAuthority is the source-authority decision for the field, empty
	// when the canonical side had nothing to say.
	CanonicalAuthority evidence.SourceAuthority
	// CanonicalUpdatedAt and ExternalUpdatedAt are the two update times the
	// ordering rested on, when they exist.
	CanonicalUpdatedAt values.Instant
	ExternalUpdatedAt  values.Instant
	// CanonicalRevision pins the revision the canonical side was read at.
	CanonicalRevision values.RevisionToken
	// Observation is the page provenance the external side came from.
	Observation ObservationWatermark
	// AllowedNext names the capabilities that may legitimately be invoked
	// about this finding. It is a list of capability identifiers, not a
	// permission: the caller still has to hold them.
	AllowedNext []string
}

// Validate reports whether the finding is internally coherent.
func (f FieldFinding) Validate() error {
	if err := f.Field.Validate(); err != nil {
		return err
	}
	if !f.Verdict.Valid() {
		return fmt.Errorf("dataops: finding for %s has no verdict", f.Field)
	}
	if !f.Safety.Valid() {
		return fmt.Errorf("dataops: finding for %s has no repair-safety class", f.Field)
	}
	if f.Reason == "" || f.SafetyReason == "" {
		return fmt.Errorf("dataops: finding for %s carries no reason token", f.Field)
	}
	if f.Verdict == VerdictMismatch && f.Relation.Agrees() {
		return fmt.Errorf("dataops: finding for %s is a mismatch on an agreeing relation", f.Field)
	}
	if f.Safety == SafetySafe && f.Verdict != VerdictMismatch {
		return fmt.Errorf("dataops: finding for %s is repair-safe without a mismatch", f.Field)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (f FieldFinding) Canonical() []byte {
	if f.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New(findingSchema, dataopsSchemaVer).
		Value("subject", f.Subject).
		String("field", string(f.Field)).
		String("access", f.Access.String()).
		String("verdict", f.Verdict.String()).
		String("relation", f.Relation.String()).
		String("reason", f.Reason).
		String("safety", f.Safety.String()).
		String("safety_reason", f.SafetyReason).
		Bool("authority?", f.CanonicalAuthority.Validate() == nil)
	if f.CanonicalAuthority.Validate() == nil {
		w.Value("authority", f.CanonicalAuthority)
	}
	w.Bool("canonical_updated_at?", f.CanonicalUpdatedAt.IsSet())
	if f.CanonicalUpdatedAt.IsSet() {
		w.Value("canonical_updated_at", f.CanonicalUpdatedAt)
	}
	w.Bool("external_updated_at?", f.ExternalUpdatedAt.IsSet())
	if f.ExternalUpdatedAt.IsSet() {
		w.Value("external_updated_at", f.ExternalUpdatedAt)
	}
	w.Bool("canonical_revision?", f.CanonicalRevision.IsSpecified())
	if f.CanonicalRevision.IsSpecified() {
		w.Value("canonical_revision", f.CanonicalRevision)
	}
	w.Bool("observation?", f.Observation.Validate() == nil)
	if f.Observation.Validate() == nil {
		w.Value("observation", f.Observation)
	}
	raw, err := w.SortedStrings("allowed_next", f.AllowedNext).Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// VerdictCounts partitions a set of findings by verdict. Every finding is
// counted exactly once, so the counts always sum to the number of findings -
// a report whose parts do not add up is a report nobody can act on.
type VerdictCounts struct {
	Match         int
	Mismatch      int
	Stale         int
	Unknown       int
	Redacted      int
	NotApplicable int
}

// Total returns the number of findings counted.
func (c VerdictCounts) Total() int {
	return c.Match + c.Mismatch + c.Stale + c.Unknown + c.Redacted + c.NotApplicable
}

// add counts one verdict.
func (c *VerdictCounts) add(v Verdict) {
	switch v {
	case VerdictMatch:
		c.Match++
	case VerdictMismatch:
		c.Mismatch++
	case VerdictStale:
		c.Stale++
	case VerdictUnknown:
		c.Unknown++
	case VerdictRedacted:
		c.Redacted++
	case VerdictNotApplicable:
		c.NotApplicable++
	}
}

// merge folds another partition into this one.
func (c *VerdictCounts) merge(o VerdictCounts) {
	c.Match += o.Match
	c.Mismatch += o.Mismatch
	c.Stale += o.Stale
	c.Unknown += o.Unknown
	c.Redacted += o.Redacted
	c.NotApplicable += o.NotApplicable
}

// Canonical returns the canonical byte encoding.
func (c VerdictCounts) Canonical() []byte {
	raw, err := canonicalbytes.New("hcmnext.domains.dataops.VerdictCounts", dataopsSchemaVer).
		Int("match", int64(c.Match)).
		Int("mismatch", int64(c.Mismatch)).
		Int("stale", int64(c.Stale)).
		Int("unknown", int64(c.Unknown)).
		Int("redacted", int64(c.Redacted)).
		Int("not_applicable", int64(c.NotApplicable)).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// SafetyCounts partitions findings by repair-safe classification.
type SafetyCounts struct {
	NotRequired int
	Safe        int
	Unsafe      int
	Undecidable int
}

// Total returns the number of findings counted.
func (c SafetyCounts) Total() int {
	return c.NotRequired + c.Safe + c.Unsafe + c.Undecidable
}

// add counts one safety class.
func (c *SafetyCounts) add(s RepairSafety) {
	switch s {
	case SafetyNotRequired:
		c.NotRequired++
	case SafetySafe:
		c.Safe++
	case SafetyUnsafe:
		c.Unsafe++
	case SafetyUndecidable:
		c.Undecidable++
	}
}

// merge folds another partition into this one.
func (c *SafetyCounts) merge(o SafetyCounts) {
	c.NotRequired += o.NotRequired
	c.Safe += o.Safe
	c.Unsafe += o.Unsafe
	c.Undecidable += o.Undecidable
}

// Canonical returns the canonical byte encoding.
func (c SafetyCounts) Canonical() []byte {
	raw, err := canonicalbytes.New("hcmnext.domains.dataops.SafetyCounts", dataopsSchemaVer).
		Int("not_required", int64(c.NotRequired)).
		Int("safe", int64(c.Safe)).
		Int("unsafe", int64(c.Unsafe)).
		Int("undecidable", int64(c.Undecidable)).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// RecordDiff is the comparison of one subject across two systems.
type RecordDiff struct {
	Subject values.EntityRef
	// CanonicalExists and ObservedExists are reported separately: a subject
	// present on one side and absent on the other is a finding about the
	// population, not about any one field.
	CanonicalExists bool
	ObservedExists  bool
	// Findings is one entry per requested field, in sorted field order.
	Findings []FieldFinding
	Verdicts VerdictCounts
	Safety   SafetyCounts
	// Digest is the digest of this diff, so a repair plan can bind to the
	// exact comparison it was derived from.
	Digest string
}

// Mismatches returns the findings that are actual disagreements.
func (d RecordDiff) Mismatches() []FieldFinding {
	out := make([]FieldFinding, 0, len(d.Findings))
	for _, f := range d.Findings {
		if f.Verdict == VerdictMismatch {
			out = append(out, f)
		}
	}
	return out
}

// Finding returns the finding for a field.
func (d RecordDiff) Finding(field FieldID) (FieldFinding, bool) {
	for _, f := range d.Findings {
		if f.Field == field {
			return f, true
		}
	}
	return FieldFinding{}, false
}

// canonicalBody encodes the diff without its own digest.
func (d RecordDiff) canonicalBody() ([]byte, error) {
	w := canonicalbytes.New(diffSchema, dataopsSchemaVer).
		Value("subject", d.Subject).
		Bool("canonical_exists", d.CanonicalExists).
		Bool("observed_exists", d.ObservedExists).
		Count("findings", len(d.Findings))
	for _, f := range d.Findings {
		w.Value("finding", f)
	}
	return w.
		Value("verdicts", d.Verdicts).
		Value("safety", d.Safety).
		Bytes()
}

// Canonical returns the canonical byte encoding including the digest, or nil
// when the diff is incoherent.
func (d RecordDiff) Canonical() []byte {
	body, err := d.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.dataops.RecordDiffEnvelope", dataopsSchemaVer).
		Field("body", body).
		String("digest", d.Digest).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// DiffRecordRequest is one subject's comparison input.
//
// EvaluatedAt is an input rather than a clock read. A comparison that reads
// the wall clock cannot be replayed, and freshness is exactly the judgement
// that would silently change between two runs over identical data.
type DiffRecordRequest struct {
	Canonical CanonicalRecord
	Observed  ObservedRecord
	// ObservedPresent reports whether the observation source returned a record
	// for this subject at all. False means the source said nothing, which is
	// not the same as the source saying the subject is absent.
	ObservedPresent bool
	// Observation is the page provenance the observed record came from.
	Observation ObservationWatermark
	// Fields is the exact projection to compare.
	Fields []FieldID
	// Authorization is the already-evaluated per-field decision.
	Authorization Authorization
	// Freshness is the versioned observation-age policy.
	Freshness FreshnessPolicy
	// EvaluatedAt is the instant the comparison is judged at.
	EvaluatedAt values.Instant
}

// Validate reports whether the request is well formed.
func (r DiffRecordRequest) Validate() error {
	if err := r.Canonical.Validate(); err != nil {
		return err
	}
	if err := r.Observation.Validate(); err != nil {
		return err
	}
	if r.ObservedPresent {
		if err := r.Observed.Validate(); err != nil {
			return err
		}
		if r.Observed.Subject != r.Canonical.Subject {
			return fmt.Errorf("%w: canonical %s observed %s",
				ErrSubjectMismatch, r.Canonical.Subject, r.Observed.Subject)
		}
	}
	if err := r.Freshness.Validate(); err != nil {
		return err
	}
	if !r.EvaluatedAt.IsSet() {
		return fmt.Errorf("%w: no evaluation instant", ErrEvaluationTime)
	}
	fields, err := normalizeFields(r.Fields)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRequestInvalid, err)
	}
	if err := r.Authorization.Validate(); err != nil {
		return err
	}
	return r.Authorization.Covers(fields)
}

// DiffRecord compares the canonical and observed views of one subject.
//
// It is a pure function: no clock, no I/O, no map iteration. The same inputs
// always produce the same findings in the same order with the same digest,
// which is what lets a repair plan bind to a diff by digest and a later run
// prove the diff has not moved.
//
// The classification order is the contract, and each step exists to stop a
// specific false positive:
//
//  1. A denied field is REDACTED and never compared.
//  2. A field missing from either side is UNKNOWN or NOT_APPLICABLE, because
//     silence is not disagreement.
//  3. A stale observation is STALE even when the values differ, because the
//     source may already have been corrected.
//  4. An unreadable side is UNKNOWN, because nothing true can be said about a
//     value nobody could read.
//  5. Only what survives all four is a MATCH or a MISMATCH.
func DiffRecord(req DiffRecordRequest) (RecordDiff, error) {
	if err := req.Validate(); err != nil {
		return RecordDiff{}, err
	}
	fields, err := normalizeFields(req.Fields)
	if err != nil {
		return RecordDiff{}, err
	}
	fresh, err := req.Freshness.fresh(req.Observation.RetrievedAt, req.EvaluatedAt)
	if err != nil {
		return RecordDiff{}, err
	}

	diff := RecordDiff{
		Subject:         req.Canonical.Subject,
		CanonicalExists: req.Canonical.Exists,
		ObservedExists:  req.ObservedPresent && req.Observed.Exists,
		Findings:        make([]FieldFinding, 0, len(fields)),
	}

	for _, field := range fields {
		finding := classifyField(field, req, fresh)
		if err := finding.Validate(); err != nil {
			return RecordDiff{}, err
		}
		diff.Verdicts.add(finding.Verdict)
		diff.Safety.add(finding.Safety)
		diff.Findings = append(diff.Findings, finding)
	}

	body, err := diff.canonicalBody()
	if err != nil {
		return RecordDiff{}, err
	}
	diff.Digest = canonicalbytes.Digest(body)
	return diff, nil
}

// classifyField produces the finding for one field.
func classifyField(field FieldID, req DiffRecordRequest, fresh bool) FieldFinding {
	finding := FieldFinding{
		Subject:     req.Canonical.Subject,
		Field:       field,
		Access:      AccessAuthorized,
		Observation: req.Observation,
	}

	ruling, _ := req.Authorization.RulingFor(field)
	if ruling.Effect == EffectDeny {
		finding.Access = AccessDenied
		finding.Verdict = VerdictRedacted
		finding.Reason = ReasonFieldDenied
		finding.Safety = SafetyUndecidable
		finding.SafetyReason = ReasonFieldDenied
		finding.AllowedNext = nil
		// A denied field discloses nothing else: no authority, no revision, no
		// timestamps. Those name systems and dates, which is information about
		// the withheld value.
		finding.Observation = ObservationWatermark{}
		return finding
	}

	canonical, hasCanonical := req.Canonical.Lookup(field)
	if hasCanonical {
		finding.CanonicalAuthority = canonical.Authority
		finding.CanonicalUpdatedAt = canonical.UpdatedAt
		finding.CanonicalRevision = canonical.Revision
	}

	if !req.ObservedPresent || !req.Observed.Exists {
		finding.Verdict = VerdictUnknown
		finding.Reason = ReasonNoObservation
		finding.Safety = SafetyUndecidable
		finding.SafetyReason = ReasonNoObservation
		finding.AllowedNext = []string{CapabilityDetectDrift}
		return finding
	}

	observed, hasObserved := req.Observed.Lookup(field)
	if hasObserved {
		finding.ExternalUpdatedAt = observed.UpdatedAt
	}

	switch {
	case !hasCanonical && !hasObserved:
		finding.Verdict = VerdictNotApplicable
		finding.Reason = ReasonNotApplicable
		finding.Safety = SafetyNotRequired
		finding.SafetyReason = ReasonNotApplicable
		return finding
	case !hasCanonical:
		finding.Verdict = VerdictUnknown
		finding.Reason = ReasonFieldNotCanonical
		finding.Safety = SafetyUndecidable
		finding.SafetyReason = ReasonFieldNotCanonical
		finding.AllowedNext = []string{CapabilityExplainTransaction}
		return finding
	case !hasObserved:
		finding.Verdict = VerdictUnknown
		finding.Reason = ReasonFieldNotObserved
		finding.Safety = SafetyUndecidable
		finding.SafetyReason = ReasonFieldNotObserved
		finding.AllowedNext = []string{CapabilityDetectDrift}
		return finding
	}

	// NOT_APPLICABLE is decided before the comparison, on either side. A field
	// that does not apply to this worker is not a disagreement about its value.
	if canonical.Value.State() == values.PresenceNotApplicable ||
		observed.Value.State() == values.PresenceNotApplicable {
		finding.Verdict = VerdictNotApplicable
		finding.Reason = ReasonNotApplicable
		finding.Safety = SafetyNotRequired
		finding.SafetyReason = ReasonNotApplicable
		return finding
	}

	outcome, err := fielddiff.Compare(canonical.side(), observed.side())
	if err != nil {
		// The engine refuses on UNKNOWN, REDACTED and UNAVAILABLE sides. That
		// refusal is the correct answer here, not a failure: the caller is
		// told the comparison did not complete and why.
		finding.Verdict = VerdictUnknown
		finding.Reason = ReasonSideUnreadable
		finding.Safety = SafetyUndecidable
		finding.SafetyReason = ReasonSideUnreadable
		finding.AllowedNext = []string{CapabilityDetectDrift}
		return finding
	}
	finding.Relation = outcome.Relation

	if outcome.Relation.Agrees() {
		finding.Verdict = VerdictMatch
		finding.Reason = ReasonSidesAgree
		finding.Safety = SafetyNotRequired
		finding.SafetyReason = ReasonSidesAgree
		return finding
	}

	// The sides differ. Freshness is checked here rather than earlier so that
	// a stale observation that agrees is still reported as a MATCH: agreement
	// does not go out of date, disagreement does.
	if !fresh {
		finding.Verdict = VerdictStale
		finding.Reason = ReasonObservationStale
		finding.Safety = SafetyUndecidable
		finding.SafetyReason = ReasonObservationStale
		finding.AllowedNext = []string{CapabilityDetectDrift}
		return finding
	}

	finding.Verdict = VerdictMismatch
	finding.Reason = mismatchReason(outcome.Relation)
	finding.Safety, finding.SafetyReason = classifySafety(canonical.Authority.Kind, outcome.Relation)
	switch finding.Safety {
	case SafetySafe:
		finding.AllowedNext = []string{CapabilityCreateRepairPlan, CapabilitySimulateRepair}
	default:
		finding.AllowedNext = []string{CapabilityExplainTransaction, CapabilityCreateRepairPlan}
	}
	return finding
}

// mismatchReason maps a relation to the reason token for a mismatch.
func mismatchReason(r fielddiff.Relation) string {
	switch r {
	case fielddiff.RelationTypeMismatch:
		return ReasonTypeMismatch
	case fielddiff.RelationMissingLeft, fielddiff.RelationMissingRight:
		return ReasonMissingSide
	case fielddiff.RelationConflict:
		return ReasonUnordered
	case fielddiff.RelationCanonicalAhead:
		return ReasonAuthorityLocalCanonicalAhead
	case fielddiff.RelationExternalAhead:
		return ReasonAuthorityExternalAhead
	default:
		return ReasonUnordered
	}
}

// classifySafety decides whether a mismatch could be corrected mechanically.
//
// The rule is authority first, direction second. Who owns the field decides
// which way a correction could point at all; only then does the evidence
// ordering decide whether that direction is supported. A mismatch on a field
// whose master changed it more recently than our copy is safe to refresh; the
// same mismatch on a field we master means an external system changed
// something it does not own, and no amount of timestamp evidence makes
// overwriting that a mechanical decision.
func classifySafety(kind evidence.AuthorityKind, r fielddiff.Relation) (RepairSafety, string) {
	if r == fielddiff.RelationTypeMismatch {
		return SafetyUnsafe, ReasonTypeMismatch
	}
	switch kind {
	case evidence.AuthorityLocal:
		switch r {
		case fielddiff.RelationCanonicalAhead, fielddiff.RelationMissingRight:
			return SafetySafe, ReasonAuthorityLocalCanonicalAhead
		case fielddiff.RelationExternalAhead:
			return SafetyUnsafe, ReasonAuthorityLocalExternalAhead
		case fielddiff.RelationMissingLeft:
			return SafetyUnsafe, ReasonMissingSide
		default:
			return SafetyUnsafe, ReasonUnordered
		}
	case evidence.AuthorityExternalObservation:
		switch r {
		case fielddiff.RelationExternalAhead, fielddiff.RelationMissingLeft:
			return SafetySafe, ReasonAuthorityExternalAhead
		case fielddiff.RelationCanonicalAhead:
			return SafetyUnsafe, ReasonAuthorityExternalCanonicalAhead
		case fielddiff.RelationMissingRight:
			return SafetyUnsafe, ReasonMissingSide
		default:
			return SafetyUnsafe, ReasonUnordered
		}
	default:
		// A derived value has no authority of its own. Repairing it would mean
		// writing over the output of a computation instead of fixing an input.
		return SafetyUnsafe, ReasonDerivedNotRepairable
	}
}
