// Package demand defines versioned workforce demand and coverage contracts.
//
// Demand is descriptive input to matching and planning. These contracts do
// not reserve positions, create assignments, or otherwise grant authority.
package demand

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's contract version.
func Version() int { return schemaVersion }

// DemandSignal is one immutable, versioned assertion of workforce demand.
// Quantity and Unit are both retained: the former carries decimal precision
// and rounding semantics, while the latter makes the contract's unit explicit
// at its boundary.
type DemandSignal struct {
	SignalID string
	Work     values.EffectiveInterval
	Location string
	// OrgUnit is the organizational scope. Location remains supported because
	// the initial stopped-lane fixture used location as its scope field.
	OrgUnit         string
	Quantity        values.Quantity
	Unit            string
	Skill           string
	Role            string
	RoleOrSkillRef  string
	Priority        int
	Source          string
	SourceRef       string
	Confidence      values.Decimal
	ConfidenceClass ConfidenceClass
	Scenario        string
	Version         string
	CanonicalDigest string
}

// ConfidenceClass is the closed qualitative confidence vocabulary.
type ConfidenceClass string

const (
	ConfidenceLow     ConfidenceClass = "LOW"
	ConfidenceMedium  ConfidenceClass = "MEDIUM"
	ConfidenceHigh    ConfidenceClass = "HIGH"
	ConfidenceUnknown ConfidenceClass = "UNKNOWN"
)

func (c ConfidenceClass) Valid() bool {
	return c == ConfidenceLow || c == ConfidenceMedium || c == ConfidenceHigh || c == ConfidenceUnknown
}

// Validate checks every field required for a demand assertion. It deliberately
// rejects partial data instead of assigning defaults that could change
// matching results.
func (s DemandSignal) Validate() error {
	if strings.TrimSpace(s.SignalID) == "" {
		return errors.New("demand signal id is required")
	}
	if err := s.Work.Validate(); err != nil {
		return fmt.Errorf("demand signal work interval: %w", err)
	}
	if strings.TrimSpace(s.Location) == "" && strings.TrimSpace(s.OrgUnit) == "" {
		return errors.New("demand signal location or org unit is required")
	}
	if s.OrgUnit != "" && strings.TrimSpace(s.OrgUnit) == "" {
		return errors.New("demand signal org unit is invalid")
	}
	if err := s.Quantity.Validate(); err != nil {
		return fmt.Errorf("demand signal quantity: %w", err)
	}
	if strings.TrimSpace(s.Unit) == "" {
		return errors.New("demand signal unit is required")
	}
	if s.Quantity.Unit() != s.Unit {
		return fmt.Errorf("demand signal quantity unit %q does not match unit %q", s.Quantity.Unit(), s.Unit)
	}
	if strings.TrimSpace(s.Skill) == "" && strings.TrimSpace(s.Role) == "" && strings.TrimSpace(s.RoleOrSkillRef) == "" {
		return errors.New("demand signal role or skill ref is required")
	}
	if (s.Role != "" && s.Skill != "") || (s.RoleOrSkillRef != "" && (s.Role != "" || s.Skill != "")) {
		return errors.New("demand signal must declare exactly one role or skill ref")
	}
	if s.RoleOrSkillRef != "" && strings.TrimSpace(s.RoleOrSkillRef) == "" {
		return errors.New("demand signal role-or-skill ref is invalid")
	}
	if s.SourceRef != "" && strings.TrimSpace(s.SourceRef) == "" {
		return errors.New("demand signal source ref is invalid")
	}
	if s.Priority < 0 {
		return errors.New("demand signal priority must not be negative")
	}
	if strings.TrimSpace(s.Source) == "" && strings.TrimSpace(s.SourceRef) == "" {
		return errors.New("demand signal source ref is required")
	}
	confidenceSet := s.Confidence.Validate() == nil
	if s.ConfidenceClass == "" && !confidenceSet {
		return errors.New("demand signal confidence or confidence class is required")
	}
	if confidenceSet && (s.Confidence.Sign() < 0 || s.Confidence.Cmp(values.MustDecimal("1", 0, values.RoundingHalfEven)) > 0) {
		return errors.New("demand signal confidence must be between zero and one")
	}
	if strings.TrimSpace(s.Scenario) == "" {
		return errors.New("demand signal scenario is required")
	}
	if strings.TrimSpace(s.Version) == "" {
		return errors.New("demand signal version is required")
	}
	if s.ConfidenceClass != "" && !s.ConfidenceClass.Valid() {
		return fmt.Errorf("demand signal confidence class %q is not declared", s.ConfidenceClass)
	}
	if s.CanonicalDigest != "" && s.CanonicalDigest != s.computedDigest() {
		return errors.New("demand signal canonical digest mismatch")
	}
	return nil
}

func (s DemandSignal) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.demand.DemandSignal", schemaVersion).
		String("signal_id", s.SignalID).Value("work", s.Work).String("location", s.Location).String("org_unit", s.OrgUnit).
		Value("quantity", s.Quantity).String("unit", s.Unit).String("skill", s.Skill).String("role", s.Role).
		String("role_or_skill_ref", s.RoleOrSkillRef).Int("priority", int64(s.Priority)).String("source", s.Source).
		String("source_ref", s.SourceRef).Optional("confidence", s.Confidence.Validate() == nil, s.Confidence).String("confidence_class", string(s.ConfidenceClass)).
		String("scenario", s.Scenario).String("version", s.Version)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (s DemandSignal) computedDigest() string {
	b := s.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// NewDemandSignal copies and digests a signal.
func NewDemandSignal(s DemandSignal) (DemandSignal, error) {
	s.CanonicalDigest = s.computedDigest()
	if err := s.Validate(); err != nil {
		return DemandSignal{}, err
	}
	return s, nil
}

func (s DemandSignal) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	return s.body()
}
func (s DemandSignal) Digest() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	return s.computedDigest(), nil
}

// CoverageRequirement is an immutable composition of one or more demand
// signals. Signals are copied on construction and exposed only as a copy.
type CoverageRequirement struct {
	RequirementID   string
	Scenario        string
	Version         string
	Signals         []DemandSignal
	SupplyRefs      []string
	CanonicalDigest string
}

// NewCoverageRequirement composes validated signals into one versioned
// requirement. It copies the input slice so callers cannot mutate the
// requirement after publication.
func NewCoverageRequirement(id, scenario, version string, signals []DemandSignal) (CoverageRequirement, error) {
	r := CoverageRequirement{RequirementID: id, Scenario: scenario, Version: version}
	if err := r.validateHeader(); err != nil {
		return CoverageRequirement{}, err
	}
	if len(signals) == 0 {
		return CoverageRequirement{}, errors.New("coverage requirement must compose at least one demand signal")
	}
	r.Signals = append([]DemandSignal(nil), signals...)
	seenSignals := make(map[string]struct{}, len(r.Signals))
	for n, signal := range r.Signals {
		if err := signal.Validate(); err != nil {
			return CoverageRequirement{}, fmt.Errorf("coverage requirement signal %d: %w", n, err)
		}
		if signal.Scenario != scenario || signal.Version != version {
			return CoverageRequirement{}, fmt.Errorf("coverage requirement signal %d scenario/version does not match requirement", n)
		}
		if _, ok := seenSignals[signal.SignalID]; ok {
			return CoverageRequirement{}, fmt.Errorf("coverage requirement duplicate signal %q", signal.SignalID)
		}
		seenSignals[signal.SignalID] = struct{}{}
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}

func (r CoverageRequirement) validateHeader() error {
	if strings.TrimSpace(r.RequirementID) == "" {
		return errors.New("coverage requirement id is required")
	}
	if strings.TrimSpace(r.Scenario) == "" {
		return errors.New("coverage requirement scenario is required")
	}
	if strings.TrimSpace(r.Version) == "" {
		return errors.New("coverage requirement version is required")
	}
	return nil
}

// Validate verifies the published requirement and its explicit composition.
func (r CoverageRequirement) Validate() error {
	if err := r.validateHeader(); err != nil {
		return err
	}
	if len(r.Signals) == 0 {
		return errors.New("coverage requirement must compose at least one demand signal")
	}
	seenSignals := make(map[string]struct{}, len(r.Signals))
	for n, signal := range r.Signals {
		if err := signal.Validate(); err != nil {
			return fmt.Errorf("coverage requirement signal %d: %w", n, err)
		}
		if signal.Scenario != r.Scenario || signal.Version != r.Version {
			return fmt.Errorf("coverage requirement signal %d scenario/version does not match requirement", n)
		}
		if _, ok := seenSignals[signal.SignalID]; ok {
			return fmt.Errorf("coverage requirement duplicate signal %q", signal.SignalID)
		}
		seenSignals[signal.SignalID] = struct{}{}
	}
	for _, ref := range r.SupplyRefs {
		if strings.TrimSpace(ref) == "" {
			return errors.New("coverage requirement supply ref must not be empty")
		}
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return errors.New("coverage requirement canonical digest mismatch")
	}
	return nil
}

func (r CoverageRequirement) body() []byte {
	signals := append([]DemandSignal(nil), r.Signals...)
	sort.Slice(signals, func(i, j int) bool { return signals[i].SignalID < signals[j].SignalID })
	refs := append([]string(nil), r.SupplyRefs...)
	sort.Strings(refs)
	w := canonicalbytes.New("hcmnext.domains.demand.CoverageRequirement", schemaVersion).
		String("requirement_id", r.RequirementID).String("scenario", r.Scenario).String("version", r.Version).
		Count("signals", len(signals))
	for _, s := range signals {
		w.Value("signal", s)
	}
	w.SortedStrings("supply_ref", refs)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r CoverageRequirement) computedDigest() string {
	b := r.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}
func (r CoverageRequirement) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r CoverageRequirement) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

// SupplyReference is a read-only supply fact used by coverage evaluation.
// Supply is supplied by the caller; this package never reads or writes a
// workforce store.
type SupplyReference struct {
	Ref            string
	Location       string
	OrgUnit        string
	Skill          string
	Role           string
	RoleOrSkillRef string
	Work           values.EffectiveInterval
	Quantity       values.Quantity
}

func (s SupplyReference) Validate() error {
	if strings.TrimSpace(s.Ref) == "" || (strings.TrimSpace(s.Location) == "" && strings.TrimSpace(s.OrgUnit) == "") {
		return errors.New("supply reference ref and location or org unit are required")
	}
	if strings.TrimSpace(s.Skill) == "" && strings.TrimSpace(s.Role) == "" && strings.TrimSpace(s.RoleOrSkillRef) == "" {
		return errors.New("supply reference role or skill ref is required")
	}
	if (s.Role != "" && s.Skill != "") || (s.RoleOrSkillRef != "" && (s.Role != "" || s.Skill != "")) {
		return errors.New("supply reference must declare exactly one role or skill ref")
	}
	if err := s.Work.Validate(); err != nil {
		return fmt.Errorf("supply reference work interval: %w", err)
	}
	if err := s.Quantity.Validate(); err != nil {
		return fmt.Errorf("supply reference quantity: %w", err)
	}
	if s.Quantity.Value().Sign() < 0 {
		return errors.New("supply reference quantity must not be negative")
	}
	return nil
}

// CoverageShortfall is a typed gap for one demand signal and its matching
// supply set.
type CoverageShortfall struct {
	SignalID   string
	Work       values.EffectiveInterval
	Location   string
	Skill      string
	TargetRef  string
	Required   values.Quantity
	Covered    values.Quantity
	Shortfall  values.Quantity
	SupplyRefs []string
}

// CoverageEvaluation is the deterministic result of comparing a requirement
// against caller-supplied supply references.
type CoverageEvaluation struct {
	RequirementID   string
	Shortfalls      []CoverageShortfall
	Covered         bool
	CanonicalDigest string
}

// Evaluate returns a shortfall for each signal that is not fully covered by
// overlapping supply with the same location, skill and unit.
func (r CoverageRequirement) Evaluate(supply []SupplyReference) (CoverageEvaluation, error) {
	if err := r.Validate(); err != nil {
		return CoverageEvaluation{}, err
	}
	orderedSignals := append([]DemandSignal(nil), r.Signals...)
	sort.Slice(orderedSignals, func(i, j int) bool { return orderedSignals[i].SignalID < orderedSignals[j].SignalID })
	allowedRefs := make(map[string]bool, len(r.SupplyRefs))
	for _, ref := range r.SupplyRefs {
		allowedRefs[ref] = true
	}
	seenSupply := make(map[string]struct{}, len(supply))
	for i, item := range supply {
		if err := item.Validate(); err != nil {
			return CoverageEvaluation{}, fmt.Errorf("supply %d: %w", i, err)
		}
		if _, ok := seenSupply[item.Ref]; ok {
			return CoverageEvaluation{}, fmt.Errorf("duplicate supply reference %q", item.Ref)
		}
		seenSupply[item.Ref] = struct{}{}
	}
	out := CoverageEvaluation{RequirementID: r.RequirementID, Covered: true}
	for _, signal := range orderedSignals {
		covered, err := values.NewQuantity("0", signal.Unit, signal.Quantity.Value().Scale(), signal.Quantity.Value().Rounding())
		if err != nil {
			return CoverageEvaluation{}, err
		}
		refs := make([]string, 0)
		signalScope, signalTarget := demandScope(signal.Location, signal.OrgUnit), demandTarget(signal)
		for _, item := range supply {
			if len(allowedRefs) > 0 && !allowedRefs[item.Ref] {
				continue
			}
			if supplyScope(item) != signalScope || supplyTarget(item) != signalTarget || item.Quantity.Unit() != signal.Unit {
				continue
			}
			overlaps, err := signal.Work.Overlaps(item.Work)
			if err != nil {
				return CoverageEvaluation{}, err
			}
			if !overlaps {
				continue
			}
			covered, err = covered.Add(item.Quantity)
			if err != nil {
				return CoverageEvaluation{}, fmt.Errorf("signal %s incompatible supply %s: %w", signal.SignalID, item.Ref, err)
			}
			refs = append(refs, item.Ref)
		}
		sort.Strings(refs)
		if covered.Value().Cmp(signal.Quantity.Value()) < 0 {
			shortfall, err := signal.Quantity.Sub(covered)
			if err != nil {
				return CoverageEvaluation{}, err
			}
			out.Covered = false
			out.Shortfalls = append(out.Shortfalls, CoverageShortfall{SignalID: signal.SignalID, Work: signal.Work, Location: signal.Location, Skill: signal.Skill, TargetRef: demandTarget(signal), Required: signal.Quantity, Covered: covered, Shortfall: shortfall, SupplyRefs: append([]string(nil), refs...)})
		}
	}
	out.CanonicalDigest = canonicalbytes.Digest(out.body())
	return out, nil
}

func demandScope(location, orgUnit string) string {
	if orgUnit != "" {
		return orgUnit
	}
	return location
}

func demandTarget(s DemandSignal) string {
	if s.RoleOrSkillRef != "" {
		return s.RoleOrSkillRef
	}
	if s.Role != "" {
		return s.Role
	}
	return s.Skill
}

func supplyScope(s SupplyReference) string { return demandScope(s.Location, s.OrgUnit) }

func supplyTarget(s SupplyReference) string {
	if s.RoleOrSkillRef != "" {
		return s.RoleOrSkillRef
	}
	if s.Role != "" {
		return s.Role
	}
	return s.Skill
}

func Evaluate(r CoverageRequirement, supply []SupplyReference) (CoverageEvaluation, error) {
	return r.Evaluate(supply)
}

func (e CoverageEvaluation) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.demand.CoverageEvaluation", schemaVersion).
		String("requirement_id", e.RequirementID).Bool("covered", e.Covered).Count("shortfalls", len(e.Shortfalls))
	for _, s := range e.Shortfalls {
		w.String("signal_id", s.SignalID).Value("work", s.Work).String("location", s.Location).String("skill", s.Skill).String("target_ref", s.TargetRef).
			Value("required", s.Required).Value("covered", s.Covered).Value("shortfall", s.Shortfall).SortedStrings("supply_ref", s.SupplyRefs)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// CoverageExplanation is a redaction-free summary of a pure coverage result.
type CoverageExplanation struct {
	RequirementID  string
	Covered        bool
	ShortfallCount int
	Digest         string
}

func (r CoverageRequirement) Explain() (CoverageExplanation, error) {
	if err := r.Validate(); err != nil {
		return CoverageExplanation{}, err
	}
	return CoverageExplanation{RequirementID: r.RequirementID, ShortfallCount: len(r.Signals), Digest: r.computedDigest()}, nil
}

func Explain(r CoverageRequirement) (CoverageExplanation, error) { return r.Explain() }
