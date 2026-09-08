// Package incentive owns the pure, immutable vocabulary for incentive and
// commission plans. It records inputs and lineage; it does not persist,
// approve, pay, or call an external measure provider.
package incentive

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const schemaVersion = 1

// Version reports the vocabulary version used by canonical encodings.
func Version() int { return schemaVersion }

var (
	ErrInvalidPlan        = errors.New("incentive: invalid plan revision")
	ErrInvalidMeasure     = errors.New("incentive: invalid measure")
	ErrInvalidObservation = errors.New("incentive: invalid attainment observation")
	ErrInvalidAward       = errors.New("incentive: invalid award calculation")
	ErrInvalidClawback    = errors.New("incentive: invalid clawback rule")
	ErrAwardTransition    = errors.New("incentive: award transition is not allowed")
	ErrApprovalRefused    = errors.New("incentive: approval evidence was refused")
)

// MeasureKind is deliberately closed so a provider cannot silently invent a
// new compensation meaning in a plan revision.
type MeasureKind string

const (
	MeasureRevenue        MeasureKind = "REVENUE"
	MeasureBookings       MeasureKind = "BOOKINGS"
	MeasureGrossMargin    MeasureKind = "GROSS_MARGIN"
	MeasureProfit         MeasureKind = "PROFIT"
	MeasureIndividual     MeasureKind = "INDIVIDUAL"
	MeasureTeam           MeasureKind = "TEAM"
	MeasureCustomer       MeasureKind = "CUSTOMER"
	MeasureRetention      MeasureKind = "RETENTION"
	MeasureQuality        MeasureKind = "QUALITY"
	MeasureMilestone      MeasureKind = "MILESTONE"
	MeasureCommissionable MeasureKind = "COMMISSIONABLE"
)

func (k MeasureKind) Valid() bool {
	switch k {
	case MeasureRevenue, MeasureBookings, MeasureGrossMargin, MeasureProfit,
		MeasureIndividual, MeasureTeam, MeasureCustomer, MeasureRetention,
		MeasureQuality, MeasureMilestone, MeasureCommissionable:
		return true
	default:
		return false
	}
}

// PlanMeasure is one weighted, target-bearing measure in a plan.
type PlanMeasure struct {
	ID           string
	Name         string
	Kind         MeasureKind
	Target       values.Decimal
	Weight       values.Decimal
	Threshold    values.Decimal
	Cap          values.Decimal
	HasThreshold bool
	HasCap       bool
	SourceRef    string
}

// Measure is the concise name used by callers.
type Measure = PlanMeasure

func (m PlanMeasure) Validate() error {
	if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("%w: id and name are required", ErrInvalidMeasure)
	}
	if !m.Kind.Valid() {
		return fmt.Errorf("%w: kind %q is not declared", ErrInvalidMeasure, m.Kind)
	}
	for field, value := range map[string]values.Decimal{"target": m.Target, "weight": m.Weight} {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidMeasure, field, err)
		}
		if value.Sign() < 0 || (field == "weight" && value.Sign() == 0) {
			return fmt.Errorf("%w: %s must be positive or zero as declared", ErrInvalidMeasure, field)
		}
	}
	if m.Target.Sign() == 0 {
		return fmt.Errorf("%w: target must be non-zero", ErrInvalidMeasure)
	}
	if m.HasThreshold {
		if err := m.Threshold.Validate(); err != nil {
			return fmt.Errorf("%w: threshold: %v", ErrInvalidMeasure, err)
		}
		if m.Threshold.Sign() < 0 {
			return fmt.Errorf("%w: threshold must not be negative", ErrInvalidMeasure)
		}
	}
	if m.HasCap {
		if err := m.Cap.Validate(); err != nil {
			return fmt.Errorf("%w: cap: %v", ErrInvalidMeasure, err)
		}
		if m.Cap.Sign() < 0 {
			return fmt.Errorf("%w: cap must not be negative", ErrInvalidMeasure)
		}
	}
	return nil
}

func (m PlanMeasure) Canonical() []byte {
	if m.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.incentive.PlanMeasure", schemaVersion).
		String("id", m.ID).String("name", m.Name).String("kind", string(m.Kind)).
		Value("target", m.Target).Value("weight", m.Weight).String("source_ref", m.SourceRef).
		Bool("threshold_present", m.HasThreshold).Bool("cap_present", m.HasCap)
	if m.HasThreshold {
		w.Value("threshold", m.Threshold)
	}
	if m.HasCap {
		w.Value("cap", m.Cap)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// ClawbackTrigger is a closed reason for recovering an award.
type ClawbackTrigger string

const (
	ClawbackMisstatement ClawbackTrigger = "MISSTATEMENT"
	ClawbackIneligible   ClawbackTrigger = "INELIGIBLE"
	ClawbackPolicyBreach ClawbackTrigger = "POLICY_BREACH"
	ClawbackTermination  ClawbackTrigger = "TERMINATION"
)

func (t ClawbackTrigger) Valid() bool {
	switch t {
	case ClawbackMisstatement, ClawbackIneligible, ClawbackPolicyBreach, ClawbackTermination:
		return true
	default:
		return false
	}
}

// ClawbackRule is immutable plan policy. It is a rule reference, not an
// instruction to debit a worker or payroll account.
type ClawbackRule struct {
	ID               string
	Version          string
	Trigger          ClawbackTrigger
	WindowDays       int
	FormulaRef       string
	EvidenceRef      string
	EvidenceRequired bool
}

func (r ClawbackRule) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Version) == "" {
		return fmt.Errorf("%w: id and version are required", ErrInvalidClawback)
	}
	if !r.Trigger.Valid() {
		return fmt.Errorf("%w: trigger %q is not declared", ErrInvalidClawback, r.Trigger)
	}
	if r.WindowDays < 0 {
		return fmt.Errorf("%w: window_days must not be negative", ErrInvalidClawback)
	}
	if strings.TrimSpace(r.FormulaRef) == "" {
		return fmt.Errorf("%w: formula_ref is required", ErrInvalidClawback)
	}
	if r.EvidenceRequired && strings.TrimSpace(r.EvidenceRef) == "" {
		return fmt.Errorf("%w: evidence_ref is required when evidence is required", ErrInvalidClawback)
	}
	return nil
}

func (r ClawbackRule) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.incentive.ClawbackRule", schemaVersion).
		String("id", r.ID).String("version", r.Version).String("trigger", string(r.Trigger)).
		Int("window_days", int64(r.WindowDays)).String("formula_ref", r.FormulaRef).
		String("evidence_ref", r.EvidenceRef).Bool("evidence_required", r.EvidenceRequired).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// IncentivePlanRevision is a complete immutable definition of one plan
// revision. Weightings are exact decimals and must sum to exactly 100 percent.
type IncentivePlanRevision struct {
	PlanID             string
	Revision           uint64
	Name               string
	Currency           string
	PeriodRef          string
	EligibilityRef     string
	FormulaRef         string
	Measures           []PlanMeasure
	ClawbackRules      []ClawbackRule
	ParentDigest       string
	SupersedesRevision uint64
	CanonicalDigest    string
	Digest             string
}

// IncentivePlan is the concise aggregate name.
type IncentivePlan = IncentivePlanRevision

func (p IncentivePlanRevision) Validate() error {
	if strings.TrimSpace(p.PlanID) == "" || p.Revision == 0 {
		return fmt.Errorf("%w: plan_id and non-zero revision are required", ErrInvalidPlan)
	}
	for field, value := range map[string]string{"name": p.Name, "currency": p.Currency, "period_ref": p.PeriodRef, "eligibility_ref": p.EligibilityRef, "formula_ref": p.FormulaRef} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidPlan, field)
		}
	}
	if p.Revision == 1 && (p.ParentDigest != "" || p.SupersedesRevision != 0) {
		return fmt.Errorf("%w: first revision cannot have lineage", ErrInvalidPlan)
	}
	if p.Revision > 1 && (strings.TrimSpace(p.ParentDigest) == "" || p.SupersedesRevision == 0 || p.SupersedesRevision >= p.Revision) {
		return fmt.Errorf("%w: successor requires an earlier parent revision and digest", ErrInvalidPlan)
	}
	if len(p.Measures) == 0 {
		return fmt.Errorf("%w: at least one measure is required", ErrInvalidPlan)
	}
	seen := make(map[string]struct{}, len(p.Measures))
	var total values.Decimal
	for i, measure := range p.Measures {
		if err := measure.Validate(); err != nil {
			return fmt.Errorf("%w: measure %s: %v", ErrInvalidPlan, measure.ID, err)
		}
		if _, ok := seen[measure.ID]; ok {
			return fmt.Errorf("%w: duplicate measure id %q", ErrInvalidPlan, measure.ID)
		}
		seen[measure.ID] = struct{}{}
		if i == 0 {
			total = measure.Weight
		} else {
			if measure.Weight.Scale() != total.Scale() {
				return fmt.Errorf("%w: field weight scale differs for measure %q", ErrInvalidPlan, measure.ID)
			}
			var err error
			total, err = total.Add(measure.Weight)
			if err != nil {
				return fmt.Errorf("%w: weight: %v", ErrInvalidPlan, err)
			}
		}
	}
	expectedText := "100"
	if total.Scale() > 0 {
		expectedText += "." + strings.Repeat("0", int(total.Scale()))
	}
	expected, err := values.NewDecimal(expectedText, total.Scale(), values.RoundingExactRequired)
	if err != nil || !total.Equal(expected) {
		return fmt.Errorf("%w: field weightings must sum exactly to 100 percent", ErrInvalidPlan)
	}
	for _, rule := range p.ClawbackRules {
		if err := rule.Validate(); err != nil {
			return fmt.Errorf("%w: clawback rule: %v", ErrInvalidPlan, err)
		}
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidPlan)
	}
	if p.Digest != "" && p.Digest != p.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidPlan)
	}
	return nil
}

func (p IncentivePlanRevision) canonicalWithoutDigest() []byte {
	measures := append([]PlanMeasure(nil), p.Measures...)
	sort.Slice(measures, func(i, j int) bool { return measures[i].ID < measures[j].ID })
	rules := append([]ClawbackRule(nil), p.ClawbackRules...)
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	w := canonicalbytes.New("hcmnext.domains.incentive.IncentivePlanRevision", schemaVersion).
		String("plan_id", p.PlanID).Int("revision", int64(p.Revision)).String("name", p.Name).
		String("currency", p.Currency).String("period_ref", p.PeriodRef).String("eligibility_ref", p.EligibilityRef).
		String("formula_ref", p.FormulaRef).String("parent_digest", p.ParentDigest).
		Int("supersedes_revision", int64(p.SupersedesRevision)).Count("measures", len(measures))
	for _, measure := range measures {
		w.Value("measure", measure)
	}
	w.Count("clawback_rules", len(rules))
	for _, rule := range rules {
		w.Value("clawback_rule", rule)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (p IncentivePlanRevision) computedDigest() string {
	return canonicalbytes.Digest(p.canonicalWithoutDigest())
}

func (p IncentivePlanRevision) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return p.canonicalWithoutDigest()
}

func (p IncentivePlanRevision) DigestValue() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p.computedDigest(), nil
}

// NewIncentivePlanRevision copies slices, validates all fields, and mints both
// digest spellings used by domain consumers.
func NewIncentivePlanRevision(p IncentivePlanRevision) (IncentivePlanRevision, error) {
	p.Measures = append([]PlanMeasure(nil), p.Measures...)
	p.ClawbackRules = append([]ClawbackRule(nil), p.ClawbackRules...)
	p.CanonicalDigest, p.Digest = "", ""
	if err := p.Validate(); err != nil {
		return IncentivePlanRevision{}, err
	}
	digest := p.computedDigest()
	p.CanonicalDigest, p.Digest = digest, digest
	return p, nil
}

func NewPlanRevision(p IncentivePlanRevision) (IncentivePlanRevision, error) {
	return NewIncentivePlanRevision(p)
}

type PlanExplanation struct {
	PlanID          string
	Revision        uint64
	MeasureIDs      []string
	ClawbackRuleIDs []string
	Digest          string
}

func (p IncentivePlanRevision) Explain() (PlanExplanation, error) {
	if err := p.Validate(); err != nil {
		return PlanExplanation{}, err
	}
	measureIDs := make([]string, 0, len(p.Measures))
	for _, measure := range p.Measures {
		measureIDs = append(measureIDs, measure.ID)
	}
	ruleIDs := make([]string, 0, len(p.ClawbackRules))
	for _, rule := range p.ClawbackRules {
		ruleIDs = append(ruleIDs, rule.ID)
	}
	sort.Strings(measureIDs)
	sort.Strings(ruleIDs)
	return PlanExplanation{PlanID: p.PlanID, Revision: p.Revision, MeasureIDs: measureIDs, ClawbackRuleIDs: ruleIDs, Digest: p.computedDigest()}, nil
}

func ExplainPlan(p IncentivePlanRevision) (PlanExplanation, error) { return p.Explain() }

// Successor returns an immutable revision with lineage bound to the receiver.
func (p IncentivePlanRevision) Successor(next IncentivePlanRevision) (IncentivePlanRevision, error) {
	if err := p.Validate(); err != nil {
		return IncentivePlanRevision{}, err
	}
	next.PlanID, next.ParentDigest, next.SupersedesRevision = p.PlanID, p.computedDigest(), p.Revision
	if next.Revision == 0 {
		next.Revision = p.Revision + 1
	}
	return NewIncentivePlanRevision(next)
}

// AttainmentObservation is a bitemporal, provider-attributed observation.
// AsOfEffective and AsKnownAt are canonical; AsOf and KnownAt are compatible
// concise spellings accepted at construction time.
type AttainmentObservation struct {
	ObservationID   string
	PlanID          string
	PlanRevision    uint64
	MeasureID       string
	WorkerRef       string
	Value           values.Decimal
	ObservedValue   values.Decimal
	AsOfEffective   values.LocalDate
	AsOf            values.LocalDate
	AsKnownAt       values.KnownAt
	KnownAt         values.KnownAt
	SourceRef       string
	Watermark       string
	CanonicalDigest string
	Digest          string
}

func (o AttainmentObservation) normalized() (AttainmentObservation, error) {
	if o.Value.Validate() != nil {
		o.Value = o.ObservedValue
	} else if o.ObservedValue.Validate() == nil && !o.Value.Equal(o.ObservedValue) {
		return AttainmentObservation{}, fmt.Errorf("%w: value and observed_value differ", ErrInvalidObservation)
	}
	if o.AsOfEffective.Validate() != nil {
		o.AsOfEffective = o.AsOf
	}
	if o.AsKnownAt.Canonical() == nil {
		o.AsKnownAt = o.KnownAt
	}
	return o, nil
}

func (o AttainmentObservation) Validate() error {
	n, err := o.normalized()
	if err != nil {
		return err
	}
	for field, value := range map[string]string{"observation_id": n.ObservationID, "plan_id": n.PlanID, "measure_id": n.MeasureID, "worker_ref": n.WorkerRef, "source_ref": n.SourceRef, "watermark": n.Watermark} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: field %s is required", ErrInvalidObservation, field)
		}
	}
	if n.PlanRevision == 0 {
		return fmt.Errorf("%w: field plan_revision is required", ErrInvalidObservation)
	}
	if err := n.Value.Validate(); err != nil {
		return fmt.Errorf("%w: field value: %v", ErrInvalidObservation, err)
	}
	if err := n.AsOfEffective.Validate(); err != nil {
		return fmt.Errorf("%w: field as_of_effective: %v", ErrInvalidObservation, err)
	}
	if n.AsKnownAt.Canonical() == nil {
		return fmt.Errorf("%w: field as_known_at is required", ErrInvalidObservation)
	}
	if n.CanonicalDigest != "" && n.CanonicalDigest != n.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidObservation)
	}
	if n.Digest != "" && n.Digest != n.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidObservation)
	}
	return nil
}

func (o AttainmentObservation) canonicalWithoutDigest() []byte {
	n, err := o.normalized()
	if err != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.incentive.AttainmentObservation", schemaVersion).
		String("observation_id", n.ObservationID).String("plan_id", n.PlanID).
		Int("plan_revision", int64(n.PlanRevision)).String("measure_id", n.MeasureID).
		String("worker_ref", n.WorkerRef).Value("value", n.Value).
		Value("as_of_effective", n.AsOfEffective).Value("as_known_at", n.AsKnownAt).
		String("source_ref", n.SourceRef).String("watermark", n.Watermark).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (o AttainmentObservation) computedDigest() string {
	return canonicalbytes.Digest(o.canonicalWithoutDigest())
}
func (o AttainmentObservation) Canonical() []byte {
	if o.Validate() != nil {
		return nil
	}
	return o.canonicalWithoutDigest()
}

func NewAttainmentObservation(o AttainmentObservation) (AttainmentObservation, error) {
	n, err := o.normalized()
	if err != nil {
		return AttainmentObservation{}, err
	}
	n.ObservedValue = n.Value
	n.AsOf, n.KnownAt = n.AsOfEffective, n.AsKnownAt
	n.CanonicalDigest, n.Digest = "", ""
	if err := n.Validate(); err != nil {
		return AttainmentObservation{}, err
	}
	digest := n.computedDigest()
	n.CanonicalDigest, n.Digest = digest, digest
	return n, nil
}

// AwardInput names every source value used in an award calculation.
type AwardInput struct {
	Name              string
	InputName         string
	MeasureID         string
	ObservationDigest string
	Watermark         string
	Attainment        values.Decimal
	Target            values.Decimal
	Weight            values.Decimal
}

type AwardMeasureInput = AwardInput

func (i AwardInput) normalized() AwardInput {
	if strings.TrimSpace(i.Name) == "" {
		i.Name = i.InputName
	}
	if strings.TrimSpace(i.InputName) == "" {
		i.InputName = i.Name
	}
	return i
}

func (i AwardInput) Validate() error {
	i = i.normalized()
	for field, value := range map[string]string{"name": i.Name, "measure_id": i.MeasureID, "observation_digest": i.ObservationDigest, "watermark": i.Watermark} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: input %s is required", ErrInvalidAward, field)
		}
	}
	for field, value := range map[string]values.Decimal{"attainment": i.Attainment, "target": i.Target, "weight": i.Weight} {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("%w: input %s: %v", ErrInvalidAward, field, err)
		}
		if value.Sign() < 0 {
			return fmt.Errorf("%w: input %s must not be negative", ErrInvalidAward, field)
		}
	}
	return nil
}

func (i AwardInput) Canonical() []byte {
	i = i.normalized()
	if i.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.incentive.AwardInput", schemaVersion).
		String("name", i.Name).String("measure_id", i.MeasureID).
		String("observation_digest", i.ObservationDigest).String("watermark", i.Watermark).
		Value("attainment", i.Attainment).Value("target", i.Target).Value("weight", i.Weight).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// AwardThreshold is a named exact threshold used by a calculation.
type AwardThreshold struct {
	Name       string
	Minimum    values.Decimal
	Multiplier values.Decimal
}

func (t AwardThreshold) Validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("%w: threshold name is required", ErrInvalidAward)
	}
	for field, value := range map[string]values.Decimal{"minimum": t.Minimum, "multiplier": t.Multiplier} {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("%w: threshold %s: %v", ErrInvalidAward, field, err)
		}
		if value.Sign() < 0 {
			return fmt.Errorf("%w: threshold %s must not be negative", ErrInvalidAward, field)
		}
	}
	return nil
}

func (t AwardThreshold) Canonical() []byte {
	if t.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.incentive.AwardThreshold", schemaVersion).
		String("name", t.Name).Value("minimum", t.Minimum).Value("multiplier", t.Multiplier).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// AwardState is the closed approval lifecycle required by payroll consumers.
type AwardState string

const (
	AwardCalculated AwardState = "CALCULATED"
	AwardApproved   AwardState = "APPROVED"
	AwardFinalized  AwardState = "FINALIZED"
)

func (s AwardState) Valid() bool {
	return s == AwardCalculated || s == AwardApproved || s == AwardFinalized
}

// AwardCalculation binds every input required to reproduce an exact award.
type AwardCalculation struct {
	CalculationID          string
	WorkerRef              string
	PlanDigest             string
	PlanRevision           uint64
	PeriodRef              string
	EligibilityRef         string
	FormulaRef             string
	Inputs                 []AwardInput
	MeasureInputs          []AwardInput
	Thresholds             []AwardThreshold
	Cap                    values.Decimal
	HasCap                 bool
	Amount                 values.Decimal
	Currency               string
	State                  AwardState
	ApprovalRef            string
	ApprovalSourceDigest   string
	ApprovalSourceRevision uint64
	FinalizationRef        string
	Revision               uint64
	SupersedesRevision     uint64
	CanonicalDigest        string
	Digest                 string
}

func (a AwardCalculation) allInputs() []AwardInput {
	if len(a.Inputs) > 0 {
		return append([]AwardInput(nil), a.Inputs...)
	}
	return append([]AwardInput(nil), a.MeasureInputs...)
}

func (a AwardCalculation) Validate() error {
	for field, value := range map[string]string{"calculation_id": a.CalculationID, "worker_ref": a.WorkerRef, "plan_digest": a.PlanDigest, "period_ref": a.PeriodRef, "eligibility_ref": a.EligibilityRef, "formula_ref": a.FormulaRef, "currency": a.Currency} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidAward, field)
		}
	}
	if a.PlanRevision == 0 || a.Revision == 0 {
		return fmt.Errorf("%w: plan_revision and revision are required", ErrInvalidAward)
	}
	if a.SupersedesRevision >= a.Revision && a.SupersedesRevision != 0 {
		return fmt.Errorf("%w: supersedes_revision must precede revision", ErrInvalidAward)
	}
	if !a.State.Valid() {
		return fmt.Errorf("%w: state %q is not declared", ErrInvalidAward, a.State)
	}
	if err := a.Amount.Validate(); err != nil {
		return fmt.Errorf("%w: amount: %v", ErrInvalidAward, err)
	}
	if a.Amount.Sign() < 0 {
		return fmt.Errorf("%w: amount must not be negative", ErrInvalidAward)
	}
	if a.HasCap {
		if err := a.Cap.Validate(); err != nil {
			return fmt.Errorf("%w: cap: %v", ErrInvalidAward, err)
		}
		if a.Cap.Sign() < 0 || a.Amount.Cmp(a.Cap) > 0 {
			return fmt.Errorf("%w: amount exceeds declared cap", ErrInvalidAward)
		}
	}
	inputs := a.allInputs()
	if len(inputs) == 0 {
		return fmt.Errorf("%w: at least one named input is required", ErrInvalidAward)
	}
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if err := input.Validate(); err != nil {
			return err
		}
		name := input.normalized().Name
		if _, ok := seen[name]; ok {
			return fmt.Errorf("%w: duplicate input name %q", ErrInvalidAward, name)
		}
		seen[name] = struct{}{}
	}
	for _, threshold := range a.Thresholds {
		if err := threshold.Validate(); err != nil {
			return err
		}
	}
	if a.State != AwardCalculated && (strings.TrimSpace(a.ApprovalRef) == "" || strings.TrimSpace(a.ApprovalSourceDigest) == "" || a.ApprovalSourceRevision == 0 || a.ApprovalSourceRevision >= a.Revision) {
		return fmt.Errorf("%w: approval_ref is required after calculation", ErrInvalidAward)
	}
	if a.State == AwardCalculated && (a.ApprovalRef != "" || a.ApprovalSourceDigest != "" || a.ApprovalSourceRevision != 0 || a.FinalizationRef != "") {
		return fmt.Errorf("%w: calculated award cannot carry approval evidence", ErrInvalidAward)
	}
	if a.State == AwardFinalized && strings.TrimSpace(a.FinalizationRef) == "" {
		return fmt.Errorf("%w: finalization_ref is required", ErrInvalidAward)
	}
	if a.CanonicalDigest != "" && a.CanonicalDigest != a.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidAward)
	}
	if a.Digest != "" && a.Digest != a.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidAward)
	}
	return nil
}

func (a AwardCalculation) canonicalWithoutDigest() []byte {
	inputs := a.allInputs()
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].normalized().Name < inputs[j].normalized().Name })
	thresholds := append([]AwardThreshold(nil), a.Thresholds...)
	sort.Slice(thresholds, func(i, j int) bool { return thresholds[i].Name < thresholds[j].Name })
	w := canonicalbytes.New("hcmnext.domains.incentive.AwardCalculation", schemaVersion).
		String("calculation_id", a.CalculationID).String("worker_ref", a.WorkerRef).
		String("plan_digest", a.PlanDigest).Int("plan_revision", int64(a.PlanRevision)).
		String("period_ref", a.PeriodRef).String("eligibility_ref", a.EligibilityRef).
		String("formula_ref", a.FormulaRef).String("currency", a.Currency).
		Value("amount", a.Amount).String("state", string(a.State)).Int("revision", int64(a.Revision)).
		Int("supersedes_revision", int64(a.SupersedesRevision)).String("approval_ref", a.ApprovalRef).
		String("approval_source_digest", a.ApprovalSourceDigest).Int("approval_source_revision", int64(a.ApprovalSourceRevision)).String("finalization_ref", a.FinalizationRef).
		Bool("cap_present", a.HasCap).Count("inputs", len(inputs))
	if a.HasCap {
		w.Value("cap", a.Cap)
	}
	for _, input := range inputs {
		w.Value("input", input)
	}
	w.Count("thresholds", len(thresholds))
	for _, threshold := range thresholds {
		w.Value("threshold", threshold)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (a AwardCalculation) computedDigest() string {
	return canonicalbytes.Digest(a.canonicalWithoutDigest())
}
func (a AwardCalculation) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	return a.canonicalWithoutDigest()
}

func NewAwardCalculation(a AwardCalculation) (AwardCalculation, error) {
	if a.State != AwardCalculated || strings.TrimSpace(a.ApprovalRef) != "" {
		return AwardCalculation{}, fmt.Errorf("%w: new awards must start calculated without approval", ErrInvalidAward)
	}
	return newAwardCalculation(a)
}

func newAwardCalculation(a AwardCalculation) (AwardCalculation, error) {
	a.Inputs = a.allInputs()
	a.MeasureInputs = append([]AwardInput(nil), a.Inputs...)
	a.Thresholds = append([]AwardThreshold(nil), a.Thresholds...)
	a.CanonicalDigest, a.Digest = "", ""
	if err := a.Validate(); err != nil {
		return AwardCalculation{}, err
	}
	digest := a.computedDigest()
	a.CanonicalDigest, a.Digest = digest, digest
	return a, nil
}

func NewAward(a AwardCalculation) (AwardCalculation, error) { return NewAwardCalculation(a) }

// ApprovalClaim is the exact calculated award payload authorized by an
// approval receipt. Implementations must verify the receipt, not its shape.
type ApprovalClaim struct {
	CalculationID, WorkerRef, PeriodRef, Currency, AwardDigest string
	Amount                                                     values.Decimal
	AwardRevision                                              uint64
}

// ApprovalVerifier is an injected authority boundary backed by the system
// that issued the opaque approval receipt.
type ApprovalVerifier interface {
	VerifyApproval(ApprovalClaim, string) error
}

func (a AwardCalculation) approvalClaim() ApprovalClaim {
	digest, revision := a.Digest, a.Revision
	if a.State != AwardCalculated {
		digest, revision = a.ApprovalSourceDigest, a.ApprovalSourceRevision
	}
	return ApprovalClaim{CalculationID: a.CalculationID, WorkerRef: a.WorkerRef, PeriodRef: a.PeriodRef, Currency: a.Currency, Amount: a.Amount, AwardDigest: digest, AwardRevision: revision}
}

// Approve verifies the opaque receipt before appending an approved revision.
func (a AwardCalculation) Approve(approvalRef string, verifier ApprovalVerifier) (AwardCalculation, error) {
	if verifier == nil {
		return AwardCalculation{}, fmt.Errorf("%w: verifier is required", ErrApprovalRefused)
	}
	if err := a.Validate(); err != nil {
		return AwardCalculation{}, err
	}
	if a.State != AwardCalculated || a.Digest == "" {
		return AwardCalculation{}, fmt.Errorf("%w: approval requires a minted calculated award", ErrApprovalRefused)
	}
	if err := verifier.VerifyApproval(a.approvalClaim(), approvalRef); err != nil {
		return AwardCalculation{}, fmt.Errorf("%w: %v", ErrApprovalRefused, err)
	}
	n := a
	n.State, n.ApprovalRef, n.ApprovalSourceDigest = AwardApproved, approvalRef, a.Digest
	n.ApprovalSourceRevision, n.Revision, n.SupersedesRevision = a.Revision, a.Revision+1, a.Revision
	n.CanonicalDigest, n.Digest = "", ""
	return newAwardCalculation(n)
}
func (a AwardCalculation) Finalize(approvalRef string) (AwardCalculation, error) {
	return a.transition(AwardFinalized, approvalRef)
}
func (a AwardCalculation) transition(state AwardState, approvalRef string) (AwardCalculation, error) {
	if err := a.Validate(); err != nil {
		return AwardCalculation{}, err
	}
	if strings.TrimSpace(approvalRef) == "" {
		return AwardCalculation{}, fmt.Errorf("%w: approval_ref is required", ErrAwardTransition)
	}
	if (state == AwardApproved && a.State != AwardCalculated) || (state == AwardFinalized && a.State != AwardApproved) {
		return AwardCalculation{}, fmt.Errorf("%w: %s -> %s", ErrAwardTransition, a.State, state)
	}
	n := a
	n.State, n.Revision, n.SupersedesRevision = state, a.Revision+1, a.Revision
	if state == AwardApproved {
		n.ApprovalRef = approvalRef
	} else {
		n.FinalizationRef = approvalRef
	}
	n.CanonicalDigest, n.Digest = "", ""
	return newAwardCalculation(n)
}

type AwardExplanation struct {
	CalculationID  string
	PlanDigest     string
	PlanRevision   uint64
	PeriodRef      string
	EligibilityRef string
	FormulaRef     string
	InputNames     []string
	State          AwardState
	ApprovalRef    string
	Revision       uint64
	Digest         string
}

// Explain returns audit-safe structure and intentionally omits worker identity,
// source values, amount, currency, and raw observation data.
func (a AwardCalculation) Explain() (AwardExplanation, error) {
	if err := a.Validate(); err != nil {
		return AwardExplanation{}, err
	}
	names := make([]string, 0, len(a.allInputs()))
	for _, input := range a.allInputs() {
		names = append(names, input.normalized().Name)
	}
	sort.Strings(names)
	return AwardExplanation{CalculationID: a.CalculationID, PlanDigest: a.PlanDigest, PlanRevision: a.PlanRevision, PeriodRef: a.PeriodRef, EligibilityRef: a.EligibilityRef, FormulaRef: a.FormulaRef, InputNames: names, State: a.State, ApprovalRef: a.ApprovalRef, Revision: a.Revision, Digest: a.computedDigest()}, nil
}

func Explain(a AwardCalculation) (AwardExplanation, error) { return a.Explain() }
