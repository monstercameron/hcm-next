// Package service owns the pure service-credit and seniority vocabulary.
// Service facts are immutable revisions; this package computes descriptive
// results only and does not persist or authorize them.
package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's stable contract version.
func Version() int { return schemaVersion }

var (
	ErrInvalidService       = errors.New("service: invalid service model")
	ErrInvalidPeriod        = errors.New("service: invalid service period")
	ErrInvalidCreditSource  = errors.New("service: invalid credit source")
	ErrInvalidSeniorityRule = errors.New("service: invalid seniority rule")
	ErrOverlappingPeriods   = errors.New("service: overlapping periods have no declared policy")
	ErrImplicitSeniority    = errors.New("service: seniority dimension has no declared rule")
	ErrFutureKnownPeriod    = errors.New("service: period is known after the requested as-of")
	ErrRoundingConflict     = errors.New("service: rounded seniority is not exact")
	ErrInvalidAsOf          = errors.New("service: invalid as-of")
)

// CreditSourceKind is the closed vocabulary for how service credit was
// obtained. The evidence reference is mandatory for every source.
type CreditSourceKind string

const (
	CreditEmployment      CreditSourceKind = "EMPLOYMENT"
	CreditAcquiredService CreditSourceKind = "ACQUIRED_SERVICE"
	CreditLeaveCredit     CreditSourceKind = "LEAVE_CREDIT"
	CreditPriorService    CreditSourceKind = "PRIOR_SERVICE"

	Employment      = CreditEmployment
	AcquiredService = CreditAcquiredService
	LeaveCredit     = CreditLeaveCredit
	PriorService    = CreditPriorService
)

func (k CreditSourceKind) Valid() bool {
	switch k {
	case CreditEmployment, CreditAcquiredService, CreditLeaveCredit, CreditPriorService:
		return true
	default:
		return false
	}
}

// BreakType identifies a non-working or non-credit interval between periods.
type BreakType string

const (
	BreakNone        BreakType = "NONE"
	BreakLeave       BreakType = "LEAVE"
	BreakTermination BreakType = "TERMINATION"
	BreakLayoff      BreakType = "LAYOFF"
	BreakUncredited  BreakType = "UN_CREDITED"
)

func (b BreakType) Valid() bool {
	switch b {
	case BreakNone, BreakLeave, BreakTermination, BreakLayoff, BreakUncredited:
		return true
	default:
		return false
	}
}

// SeniorityDimension is the purpose-specific dimension on which service is
// computed. A period must name its dimensions; there is no implicit universal
// seniority date.
type SeniorityDimension string

const (
	DimensionGeneral  SeniorityDimension = "GENERAL"
	DimensionBenefits SeniorityDimension = "BENEFITS"
	DimensionLeave    SeniorityDimension = "LEAVE"
	DimensionPay      SeniorityDimension = "PAY"

	GeneralSeniority  = DimensionGeneral
	BenefitsSeniority = DimensionBenefits
	LeaveSeniority    = DimensionLeave
	PaySeniority      = DimensionPay
)

func (d SeniorityDimension) Valid() bool {
	switch d {
	case DimensionGeneral, DimensionBenefits, DimensionLeave, DimensionPay:
		return true
	default:
		return false
	}
}

// RoundingMode is explicit so a fraction of a declared seniority unit cannot
// silently become a different answer.
type RoundingMode string

const (
	RoundDown    RoundingMode = "DOWN"
	RoundUp      RoundingMode = "UP"
	RoundNearest RoundingMode = "NEAREST"
	RoundExact   RoundingMode = "EXACT"
)

func (m RoundingMode) Valid() bool {
	switch m {
	case RoundDown, RoundUp, RoundNearest, RoundExact:
		return true
	default:
		return false
	}
}

// RoundingUnit names the unit represented by a rounded result. DaysPerUnit is
// declared on SeniorityRule, which avoids hidden calendar assumptions.
type RoundingUnit string

const (
	UnitDays   RoundingUnit = "DAYS"
	UnitMonths RoundingUnit = "MONTHS"
	UnitYears  RoundingUnit = "YEARS"
)

func (u RoundingUnit) Valid() bool { return u == UnitDays || u == UnitMonths || u == UnitYears }

// CreditSource is one immutable attribution of service credit.
type CreditSource struct {
	Kind         CreditSourceKind
	EvidenceRef  string
	AuthorityRef string
	Revision     values.RevisionToken
}

type CreditSourceRevision = CreditSource

func (s CreditSource) Validate() error {
	if !s.Kind.Valid() {
		return fmt.Errorf("%w: field kind: %q is not declared", ErrInvalidCreditSource, s.Kind)
	}
	if strings.TrimSpace(s.EvidenceRef) == "" {
		return fmt.Errorf("%w: field evidence_ref: is required", ErrInvalidCreditSource)
	}
	if s.Kind != CreditEmployment && strings.TrimSpace(s.AuthorityRef) == "" {
		return fmt.Errorf("%w: field authority_ref: is required for %s", ErrInvalidCreditSource, s.Kind)
	}
	if err := s.Revision.Validate(); err != nil {
		return fmt.Errorf("%w: field revision: %v", ErrInvalidCreditSource, err)
	}
	if !s.Revision.IsSpecified() {
		return fmt.Errorf("%w: field revision: is required", ErrInvalidCreditSource)
	}
	return nil
}

func (s CreditSource) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.service.CreditSource", schemaVersion).
		String("kind", string(s.Kind)).String("evidence_ref", s.EvidenceRef).
		String("authority_ref", s.AuthorityRef).Value("revision", s.Revision).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// BridgeRule declares when a gap can be counted for a dimension. A zero
// MaxGapDays disables bridging; the rule remains explicit in the digest.
type BridgeRule struct {
	MaxGapDays  int64
	BreakTypes  []BreakType
	EvidenceRef string
}

func (b BridgeRule) Validate() error {
	if b.MaxGapDays < 0 {
		return fmt.Errorf("%w: field max_gap_days: must not be negative", ErrInvalidSeniorityRule)
	}
	seen := make(map[BreakType]struct{}, len(b.BreakTypes))
	for _, kind := range b.BreakTypes {
		if !kind.Valid() || kind == BreakNone {
			return fmt.Errorf("%w: field break_types: %q is not a bridge break", ErrInvalidSeniorityRule, kind)
		}
		if _, ok := seen[kind]; ok {
			return fmt.Errorf("%w: field break_types: duplicate %q", ErrInvalidSeniorityRule, kind)
		}
		seen[kind] = struct{}{}
	}
	if b.MaxGapDays > 0 && strings.TrimSpace(b.EvidenceRef) == "" {
		return fmt.Errorf("%w: field bridge.evidence_ref: is required when bridging is enabled", ErrInvalidSeniorityRule)
	}
	return nil
}

func (b BridgeRule) Canonical() []byte {
	if b.Validate() != nil {
		return nil
	}
	kinds := append([]BreakType(nil), b.BreakTypes...)
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	w := canonicalbytes.New("hcmnext.domains.service.BridgeRule", schemaVersion).
		Int("max_gap_days", b.MaxGapDays).String("evidence_ref", b.EvidenceRef).Count("break_type", len(kinds))
	for _, kind := range kinds {
		w.String("break_type", string(kind))
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// SeniorityRule binds one purpose to a declared rounding and bridge policy.
type SeniorityRule struct {
	Dimension    SeniorityDimension
	Unit         RoundingUnit
	DaysPerUnit  int64
	Rounding     RoundingMode
	AllowOverlap bool
	Bridge       BridgeRule
	Revision     values.RevisionToken
	EvidenceRef  string
}

type SeniorityDimensionRule = SeniorityRule

func (r SeniorityRule) Validate() error {
	if !r.Dimension.Valid() {
		return fmt.Errorf("%w: field dimension: %q is not declared", ErrInvalidSeniorityRule, r.Dimension)
	}
	if !r.Unit.Valid() {
		return fmt.Errorf("%w: field unit: %q is not declared", ErrInvalidSeniorityRule, r.Unit)
	}
	if r.DaysPerUnit <= 0 {
		return fmt.Errorf("%w: field days_per_unit: must be positive", ErrInvalidSeniorityRule)
	}
	if !r.Rounding.Valid() {
		return fmt.Errorf("%w: field rounding: %q is not declared", ErrInvalidSeniorityRule, r.Rounding)
	}
	if err := r.Bridge.Validate(); err != nil {
		return err
	}
	if !r.Revision.IsSpecified() {
		return fmt.Errorf("%w: field revision: is required", ErrInvalidSeniorityRule)
	}
	if strings.TrimSpace(r.EvidenceRef) == "" {
		return fmt.Errorf("%w: field evidence_ref: is required", ErrInvalidSeniorityRule)
	}
	return nil
}

func (r SeniorityRule) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.service.SeniorityRule", schemaVersion).
		String("dimension", string(r.Dimension)).String("unit", string(r.Unit)).
		Int("days_per_unit", r.DaysPerUnit).String("rounding", string(r.Rounding)).
		Bool("allow_overlap", r.AllowOverlap).Value("bridge", r.Bridge).
		Value("revision", r.Revision).String("evidence_ref", r.EvidenceRef).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// ServicePeriod is an immutable, bitemporal service assertion. Effective is
// retained as an alias for callers using the domain vocabulary directly;
// Interval is preferred and both must agree when supplied.
type ServicePeriod struct {
	ID                   string
	PeriodID             string
	EmploymentID         string
	EmploymentRef        values.EntityRef
	Interval             values.EffectiveInterval
	Effective            values.EffectiveInterval
	Credit               CreditSource
	CreditSource         CreditSource
	Break                BreakType
	BreakType            BreakType
	Dimensions           []SeniorityDimension
	ApplicableDimensions []SeniorityDimension
	KnownAt              values.KnownAt
	Revision             values.RevisionToken
	ParentDigest         string
}

type ServicePeriodRevision = ServicePeriod

func (p ServicePeriod) periodID() string {
	if p.ID != "" {
		return p.ID
	}
	return p.PeriodID
}

func (p ServicePeriod) interval() values.EffectiveInterval {
	if p.Interval.Kind() != values.IntervalKindUnspecified {
		return p.Interval
	}
	return p.Effective
}

func (p ServicePeriod) source() CreditSource {
	if p.Credit.Kind != "" {
		return p.Credit
	}
	return p.CreditSource
}

func (p ServicePeriod) breakType() BreakType {
	if p.Break != "" {
		return p.Break
	}
	if p.BreakType != "" {
		return p.BreakType
	}
	return BreakNone
}

func (p ServicePeriod) dimensions() []SeniorityDimension {
	if len(p.Dimensions) > 0 {
		return p.Dimensions
	}
	return p.ApplicableDimensions
}

func (p ServicePeriod) Validate() error {
	if strings.TrimSpace(p.periodID()) == "" {
		return fmt.Errorf("%w: field period_id: is required", ErrInvalidPeriod)
	}
	if strings.TrimSpace(p.EmploymentID) == "" && p.EmploymentRef.Validate() != nil {
		return fmt.Errorf("%w: field employment: employment_id or employment_ref is required", ErrInvalidPeriod)
	}
	iv := p.interval()
	if err := iv.Validate(); err != nil {
		return fmt.Errorf("%w: field effective: %v", ErrInvalidPeriod, err)
	}
	if iv.Kind() != values.IntervalKindLocalDate {
		return fmt.Errorf("%w: field effective: must be LOCAL_DATE", ErrInvalidPeriod)
	}
	source := p.source()
	if err := source.Validate(); err != nil {
		return fmt.Errorf("%w: field credit_source: %w", ErrInvalidPeriod, err)
	}
	breakType := p.breakType()
	if !breakType.Valid() {
		return fmt.Errorf("%w: field break_type: %q is not declared", ErrInvalidPeriod, breakType)
	}
	dimensions := p.dimensions()
	if len(dimensions) == 0 {
		return fmt.Errorf("%w: field dimensions: at least one dimension is required", ErrInvalidPeriod)
	}
	seen := make(map[SeniorityDimension]struct{}, len(dimensions))
	for _, dimension := range dimensions {
		if !dimension.Valid() {
			return fmt.Errorf("%w: field dimensions: %q is not declared", ErrInvalidPeriod, dimension)
		}
		if _, ok := seen[dimension]; ok {
			return fmt.Errorf("%w: field dimensions: duplicate %q", ErrInvalidPeriod, dimension)
		}
		seen[dimension] = struct{}{}
	}
	if p.KnownAt.Canonical() == nil {
		return fmt.Errorf("%w: field known_at: is required", ErrInvalidPeriod)
	}
	if !p.Revision.IsSpecified() {
		return fmt.Errorf("%w: field revision: is required", ErrInvalidPeriod)
	}
	if p.Interval.Kind() != values.IntervalKindUnspecified && p.Effective.Kind() != values.IntervalKindUnspecified && string(p.Interval.Canonical()) != string(p.Effective.Canonical()) {
		return fmt.Errorf("%w: field effective: interval and effective disagree", ErrInvalidPeriod)
	}
	return nil
}

func (p ServicePeriod) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	dimensions := append([]SeniorityDimension(nil), p.dimensions()...)
	sort.Slice(dimensions, func(i, j int) bool { return dimensions[i] < dimensions[j] })
	w := canonicalbytes.New("hcmnext.domains.service.ServicePeriod", schemaVersion).
		String("period_id", p.periodID()).String("employment_id", p.EmploymentID).
		Value("employment_ref", optionalEntityRef{ref: p.EmploymentRef}).Value("effective", p.interval()).
		Value("credit_source", p.source()).String("break_type", string(p.breakType())).
		Count("dimension", len(dimensions)).Value("known_at", p.KnownAt).Value("revision", p.Revision).
		String("parent_digest", p.ParentDigest)
	for _, dimension := range dimensions {
		w.String("dimension", string(dimension))
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// optionalEntityRef gives canonicalbytes an explicit presence marker without
// making a zero EntityRef look like a valid value.
type optionalEntityRef struct{ ref values.EntityRef }

func (r optionalEntityRef) Canonical() []byte {
	if r.ref.Validate() != nil {
		return []byte{0}
	}
	return append([]byte{1}, r.ref.Canonical()...)
}

// AsOf is the bitemporal cutoff used by seniority calculation.
type AsOf struct {
	EffectiveDate values.LocalDate
	KnownAt       values.KnownAt
}

type SeniorityAsOf = AsOf

func NewAsOf(date values.LocalDate, known values.KnownAt) (AsOf, error) {
	a := AsOf{EffectiveDate: date, KnownAt: known}
	if err := a.Validate(); err != nil {
		return AsOf{}, err
	}
	return a, nil
}

func (a AsOf) Validate() error {
	if err := a.EffectiveDate.Validate(); err != nil {
		return fmt.Errorf("%w: field effective_date: %v", ErrInvalidAsOf, err)
	}
	if a.KnownAt.Canonical() == nil {
		return fmt.Errorf("%w: field known_at: is required", ErrInvalidAsOf)
	}
	return nil
}

// ServiceModel is an immutable set of periods and the explicit rules used to
// interpret them.
type ServiceModel struct {
	Periods         []ServicePeriod
	Rules           []SeniorityRule
	CanonicalDigest string
}

type ServiceRevision = ServiceModel

func NewServiceModel(periods []ServicePeriod, rules []SeniorityRule) (ServiceModel, error) {
	m := ServiceModel{Periods: clonePeriods(periods), Rules: cloneRules(rules)}
	if err := m.Validate(); err != nil {
		return ServiceModel{}, err
	}
	m.CanonicalDigest = canonicalbytes.Digest(m.body())
	return m, nil
}

func NewServiceRevision(periods []ServicePeriod, rules []SeniorityRule) (ServiceModel, error) {
	return NewServiceModel(periods, rules)
}

func (m ServiceModel) Validate() error {
	if len(m.Periods) == 0 {
		return fmt.Errorf("%w: field periods: at least one period is required", ErrInvalidService)
	}
	if len(m.Rules) == 0 {
		return fmt.Errorf("%w: field rules: at least one rule is required", ErrInvalidService)
	}
	rules := make(map[SeniorityDimension]SeniorityRule, len(m.Rules))
	for _, rule := range m.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
		if _, ok := rules[rule.Dimension]; ok {
			return fmt.Errorf("%w: field rules: duplicate dimension %s", ErrInvalidService, rule.Dimension)
		}
		rules[rule.Dimension] = rule
	}
	for i, period := range m.Periods {
		if err := period.Validate(); err != nil {
			return fmt.Errorf("%w: period[%d]: %w", ErrInvalidService, i, err)
		}
		for _, dimension := range period.dimensions() {
			rule, ok := rules[dimension]
			if !ok {
				return fmt.Errorf("%w: field dimension: %s", ErrImplicitSeniority, dimension)
			}
			for j := 0; j < i; j++ {
				other := m.Periods[j]
				if !containsDimension(other.dimensions(), dimension) {
					continue
				}
				overlaps, err := period.interval().Overlaps(other.interval())
				if err != nil {
					return fmt.Errorf("%w: field periods: %v", ErrInvalidService, err)
				}
				if overlaps && !rule.AllowOverlap {
					return fmt.Errorf("%w: field periods: %s and %s for %s", ErrOverlappingPeriods, period.periodID(), other.periodID(), dimension)
				}
			}
		}
	}
	if m.CanonicalDigest != "" && m.CanonicalDigest != canonicalbytes.Digest(m.body()) {
		return fmt.Errorf("%w: field canonical_digest: mismatch", ErrInvalidService)
	}
	return nil
}

func containsDimension(dimensions []SeniorityDimension, wanted SeniorityDimension) bool {
	for _, dimension := range dimensions {
		if dimension == wanted {
			return true
		}
	}
	return false
}

func clonePeriods(in []ServicePeriod) []ServicePeriod {
	out := append([]ServicePeriod(nil), in...)
	for i := range out {
		out[i].Dimensions = append([]SeniorityDimension(nil), out[i].Dimensions...)
		out[i].ApplicableDimensions = append([]SeniorityDimension(nil), out[i].ApplicableDimensions...)
	}
	return out
}

func cloneRules(in []SeniorityRule) []SeniorityRule {
	out := append([]SeniorityRule(nil), in...)
	for i := range out {
		out[i].Bridge.BreakTypes = append([]BreakType(nil), out[i].Bridge.BreakTypes...)
	}
	return out
}

func (m ServiceModel) body() []byte {
	periods := clonePeriods(m.Periods)
	sort.Slice(periods, func(i, j int) bool { return periods[i].periodID() < periods[j].periodID() })
	rules := cloneRules(m.Rules)
	sort.Slice(rules, func(i, j int) bool { return rules[i].Dimension < rules[j].Dimension })
	w := canonicalbytes.New("hcmnext.domains.service.ServiceModel", schemaVersion).
		Count("period", len(periods))
	for _, period := range periods {
		w.Value("period", period)
	}
	w.Count("rule", len(rules))
	for _, rule := range rules {
		w.Value("rule", rule)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (m ServiceModel) Canonical() []byte {
	if m.Validate() != nil {
		return nil
	}
	return m.body()
}

func (m ServiceModel) Digest() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	return canonicalbytes.Digest(m.body()), nil
}

// SeniorityBreak records a gap considered by a calculation. It contains no
// source narrative or sensitive worker values.
type SeniorityBreak struct {
	From      values.LocalDate
	To        values.LocalDate
	Days      int64
	Bridged   bool
	BreakType BreakType
}

func (b SeniorityBreak) Canonical() []byte {
	if b.From.Validate() != nil || b.To.Validate() != nil || b.Days < 0 || !b.BreakType.Valid() {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.service.SeniorityBreak", schemaVersion).
		Value("from", b.From).Value("to", b.To).Int("days", b.Days).
		Bool("bridged", b.Bridged).String("break_type", string(b.BreakType))
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// SeniorityMeasure is one purpose-specific result.
type SeniorityMeasure struct {
	Dimension    SeniorityDimension
	RawDays      int64
	BridgedDays  int64
	RoundedUnits int64
	Unit         RoundingUnit
	DaysPerUnit  int64
	Rounding     RoundingMode
	Breaks       []SeniorityBreak
	InputsDigest string
}

type SeniorityDimensionResult = SeniorityMeasure

func (r SeniorityMeasure) Validate() error {
	if !r.Dimension.Valid() || !r.Unit.Valid() || !r.Rounding.Valid() || r.DaysPerUnit <= 0 || r.RawDays < 0 || r.BridgedDays < 0 {
		return fmt.Errorf("%w: invalid seniority measure", ErrInvalidService)
	}
	return nil
}

// SenioritySnapshot is the deterministic, descriptive calculation result.
type SenioritySnapshot struct {
	AsOf            AsOf
	Measures        []SeniorityMeasure
	InputsDigest    string
	CanonicalDigest string
}

func (s SenioritySnapshot) Validate() error {
	if err := s.AsOf.Validate(); err != nil {
		return err
	}
	if len(s.Measures) == 0 {
		return fmt.Errorf("%w: seniority snapshot has no measures", ErrInvalidService)
	}
	for _, measure := range s.Measures {
		if err := measure.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (s SenioritySnapshot) body() []byte {
	measures := append([]SeniorityMeasure(nil), s.Measures...)
	sort.Slice(measures, func(i, j int) bool { return measures[i].Dimension < measures[j].Dimension })
	w := canonicalbytes.New("hcmnext.domains.service.SenioritySnapshot", schemaVersion).
		Value("as_of_date", s.AsOf.EffectiveDate).Value("as_of_known_at", s.AsOf.KnownAt).
		String("inputs_digest", s.InputsDigest).Count("measure", len(measures))
	for _, measure := range measures {
		w.String("dimension", string(measure.Dimension)).Int("raw_days", measure.RawDays).
			Int("bridged_days", measure.BridgedDays).Int("rounded_units", measure.RoundedUnits).
			String("unit", string(measure.Unit)).Int("days_per_unit", measure.DaysPerUnit).
			String("rounding", string(measure.Rounding)).Count("break", len(measure.Breaks))
		for _, item := range measure.Breaks {
			w.Value("break", item)
		}
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (s SenioritySnapshot) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	return s.body()
}

func (s SenioritySnapshot) Digest() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	return canonicalbytes.Digest(s.body()), nil
}

// Compute calculates every declared dimension at the same bitemporal cutoff.
func (m ServiceModel) Compute(asOf AsOf) (SenioritySnapshot, error) {
	if err := m.Validate(); err != nil {
		return SenioritySnapshot{}, err
	}
	if err := asOf.Validate(); err != nil {
		return SenioritySnapshot{}, err
	}
	inputs := canonicalbytes.New("hcmnext.domains.service.SeniorityInputs", schemaVersion).
		Value("model", m).Value("as_of", asOf)
	inputsDigest, err := inputs.Digest()
	if err != nil {
		return SenioritySnapshot{}, err
	}
	rules := make(map[SeniorityDimension]SeniorityRule, len(m.Rules))
	for _, rule := range m.Rules {
		rules[rule.Dimension] = rule
	}
	measures := make([]SeniorityMeasure, 0, len(rules))
	for _, rule := range m.Rules {
		measure, err := calculateDimension(m.Periods, rule, asOf, inputsDigest)
		if err != nil {
			return SenioritySnapshot{}, err
		}
		measures = append(measures, measure)
	}
	snapshot := SenioritySnapshot{AsOf: asOf, Measures: measures, InputsDigest: inputsDigest}
	snapshot.CanonicalDigest = canonicalbytes.Digest(snapshot.body())
	return snapshot, nil
}

// ComputeSeniority is the direct functional spelling for callers with a
// period/rule slice rather than a retained model.
func ComputeSeniority(periods []ServicePeriod, asOf AsOf, rules []SeniorityRule) (SenioritySnapshot, error) {
	model, err := NewServiceModel(periods, rules)
	if err != nil {
		return SenioritySnapshot{}, err
	}
	return model.Compute(asOf)
}

func CalculateSeniority(periods []ServicePeriod, asOf AsOf, rules []SeniorityRule) (SenioritySnapshot, error) {
	return ComputeSeniority(periods, asOf, rules)
}

type clippedPeriod struct {
	start     values.LocalDate
	end       values.LocalDate
	breakType BreakType
}

func calculateDimension(periods []ServicePeriod, rule SeniorityRule, asOf AsOf, inputsDigest string) (SeniorityMeasure, error) {
	var clipped []clippedPeriod
	for _, period := range periods {
		if !containsDimension(period.dimensions(), rule.Dimension) {
			continue
		}
		if period.KnownAt.Instant().After(asOf.KnownAt.Instant()) {
			return SeniorityMeasure{}, fmt.Errorf("%w: %s", ErrFutureKnownPeriod, period.periodID())
		}
		start, _ := period.interval().StartDate()
		if start.Compare(asOf.EffectiveDate) >= 0 {
			continue
		}
		end, hasEnd := period.interval().EndDate()
		if !hasEnd || end.Compare(asOf.EffectiveDate) > 0 {
			end = asOf.EffectiveDate
		}
		if start.Compare(end) >= 0 {
			continue
		}
		if period.breakType() == BreakLeave && period.source().Kind == CreditLeaveCredit {
			// Leave credit is a positive, explicitly attributed service period.
			clipped = append(clipped, clippedPeriod{start: start, end: end, breakType: BreakNone})
			continue
		}
		if period.breakType() != BreakNone {
			clipped = append(clipped, clippedPeriod{start: start, end: end, breakType: period.breakType()})
			continue
		}
		clipped = append(clipped, clippedPeriod{start: start, end: end, breakType: BreakNone})
	}
	sort.Slice(clipped, func(i, j int) bool {
		if c := clipped[i].start.Compare(clipped[j].start); c != 0 {
			return c < 0
		}
		return clipped[i].end.Compare(clipped[j].end) < 0
	})
	var rawDays, bridgedDays int64
	var breaks []SeniorityBreak
	var covered []clippedPeriod
	for _, current := range clipped {
		if current.breakType != BreakNone {
			continue
		}
		if len(covered) > 0 {
			previous := covered[len(covered)-1]
			if current.start.Compare(previous.end) > 0 {
				gap := daysBetween(previous.end, current.start)
				breakType := gapBreakType(clipped, previous.end, current.start)
				bridge := gap <= rule.Bridge.MaxGapDays && bridgeAllows(rule.Bridge, breakType)
				breaks = append(breaks, SeniorityBreak{From: previous.end, To: current.start, Days: gap, Bridged: bridge, BreakType: breakType})
				if bridge {
					bridgedDays += gap
				}
			}
		}
		if len(covered) == 0 || current.end.Compare(covered[len(covered)-1].end) > 0 {
			covered = append(covered, current)
		}
	}
	for _, period := range covered {
		rawDays += daysBetween(period.start, period.end)
	}
	totalDays := rawDays + bridgedDays
	rounded, err := roundDays(totalDays, rule.DaysPerUnit, rule.Rounding)
	if err != nil {
		return SeniorityMeasure{}, fmt.Errorf("%w: dimension=%s: %v", err, rule.Dimension, err)
	}
	return SeniorityMeasure{Dimension: rule.Dimension, RawDays: rawDays, BridgedDays: bridgedDays, RoundedUnits: rounded, Unit: rule.Unit, DaysPerUnit: rule.DaysPerUnit, Rounding: rule.Rounding, Breaks: breaks, InputsDigest: inputsDigest}, nil
}

func gapBreakType(periods []clippedPeriod, from, to values.LocalDate) BreakType {
	for _, period := range periods {
		if period.breakType == BreakNone {
			continue
		}
		if period.start.Compare(from) >= 0 && period.end.Compare(to) <= 0 {
			return period.breakType
		}
	}
	return BreakUncredited
}

func bridgeAllows(rule BridgeRule, breakType BreakType) bool {
	if rule.MaxGapDays == 0 || len(rule.BreakTypes) == 0 {
		return false
	}
	for _, kind := range rule.BreakTypes {
		if kind == breakType {
			return true
		}
	}
	return false
}

func daysBetween(start, end values.LocalDate) int64 {
	a := time.Date(int(start.Year()), start.Month(), int(start.Day()), 0, 0, 0, 0, time.UTC)
	b := time.Date(int(end.Year()), end.Month(), int(end.Day()), 0, 0, 0, 0, time.UTC)
	return int64(b.Sub(a) / (24 * time.Hour))
}

func roundDays(days, unit int64, mode RoundingMode) (int64, error) {
	quotient, remainder := days/unit, days%unit
	if remainder == 0 {
		return quotient, nil
	}
	switch mode {
	case RoundDown:
		return quotient, nil
	case RoundUp:
		return quotient + 1, nil
	case RoundNearest:
		if remainder*2 >= unit {
			return quotient + 1, nil
		}
		return quotient, nil
	case RoundExact:
		return 0, ErrRoundingConflict
	default:
		return 0, ErrInvalidSeniorityRule
	}
}

// ServiceExplanation intentionally reports structure, policy, and digests;
// it does not repeat employment identifiers, evidence strings, or other
// potentially sensitive source values.
type ServiceExplanation struct {
	PeriodCount     int
	Dimensions      []SeniorityDimension
	RuleCount       int
	CanonicalDigest string
}

func (m ServiceModel) Explain() (ServiceExplanation, error) {
	if err := m.Validate(); err != nil {
		return ServiceExplanation{}, err
	}
	dimensions := make([]SeniorityDimension, 0, len(m.Rules))
	for _, rule := range m.Rules {
		dimensions = append(dimensions, rule.Dimension)
	}
	sort.Slice(dimensions, func(i, j int) bool { return dimensions[i] < dimensions[j] })
	return ServiceExplanation{PeriodCount: len(m.Periods), Dimensions: dimensions, RuleCount: len(m.Rules), CanonicalDigest: canonicalbytes.Digest(m.body())}, nil
}

func Explain(m ServiceModel) (ServiceExplanation, error) { return m.Explain() }

func (a AsOf) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.service.AsOf", schemaVersion).
		Value("effective_date", a.EffectiveDate).Value("known_at", a.KnownAt).Bytes()
	if err != nil {
		return nil
	}
	return b
}
