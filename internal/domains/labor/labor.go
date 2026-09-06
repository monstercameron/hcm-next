// Package labor owns the conformance vocabulary for labor costing.
//
// The package is deliberately kernel-pure: it validates typed dimensions,
// versions, effective intervals and exact allocations, but it does not read
// or write a database and does not calculate authoritative payroll effects.
package labor

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// DimensionKind is the closed vocabulary of labor-cost dimensions.
type DimensionKind string

const (
	DimensionCostCenter    DimensionKind = "COST_CENTER"
	DimensionProject       DimensionKind = "PROJECT"
	DimensionActivity      DimensionKind = "ACTIVITY"
	DimensionFundingSource DimensionKind = "FUNDING_SOURCE"
	DimensionLocation      DimensionKind = "LOCATION"

	// Concise aliases for the same closed vocabulary.
	CostCenter    = DimensionCostCenter
	Project       = DimensionProject
	Activity      = DimensionActivity
	FundingSource = DimensionFundingSource
	Location      = DimensionLocation
)

func (k DimensionKind) Valid() bool {
	switch k {
	case DimensionCostCenter, DimensionProject, DimensionActivity, DimensionFundingSource, DimensionLocation:
		return true
	default:
		return false
	}
}

func (k DimensionKind) String() string { return string(k) }

var (
	ErrInvalidDimension     = errors.New("labor: invalid labor-cost dimension")
	ErrInvalidLaborRule     = errors.New("labor: invalid labor rule")
	ErrInvalidAllocation    = errors.New("labor: invalid allocation")
	ErrAllocationUnbalanced = errors.New("labor: allocation must sum to exactly 100 percent")
	ErrRuleVersionConflict  = errors.New("labor: rule successor must be a new version")
	ErrConformanceRejected  = errors.New("LABOR_001_REJECTED")
)

// Dimension identifies one governed value in the labor-cost vocabulary.
type Dimension struct {
	Kind    DimensionKind
	Value   string
	Version string
}

// LaborCostDimension is the descriptive spelling of Dimension.
type LaborCostDimension = Dimension

func (d Dimension) Validate() error {
	if !d.Kind.Valid() {
		return fmt.Errorf("%w: kind %q is not declared", ErrInvalidDimension, d.Kind)
	}
	if strings.TrimSpace(d.Value) == "" {
		return fmt.Errorf("%w: value is required for %s", ErrInvalidDimension, d.Kind)
	}
	if strings.TrimSpace(d.Version) == "" {
		return fmt.Errorf("%w: version is required for %s", ErrInvalidDimension, d.Kind)
	}
	return nil
}

func (d Dimension) Canonical() []byte {
	if d.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.labor.Dimension", 1).
		String("kind", d.Kind.String()).String("value", d.Value).String("version", d.Version)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// RuleRef binds a labor rule component to an exact source revision.
type RuleRef struct {
	ID      string
	Version string
	Digest  string
}

// LaborReference is a descriptive alias for RuleRef.
type LaborReference = RuleRef

func (r RuleRef) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Version) == "" || strings.TrimSpace(r.Digest) == "" {
		return fmt.Errorf("labor: component reference requires id, version and digest")
	}
	return nil
}

func (r RuleRef) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.labor.RuleRef", 1).
		String("id", r.ID).String("version", r.Version).String("digest", r.Digest)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// LaborRule is an immutable, effective-dated rule revision. The component
// references and exact decimal rates make omitted currency or source versions
// observable instead of allowing a numerically plausible total to pass.
type LaborRule struct {
	ID        string
	Version   string
	Effective values.EffectiveInterval

	WorkerRef  RuleRef
	TimeRef    RuleRef
	EntityRef  RuleRef
	JobRef     RuleRef
	EarningRef RuleRef

	Dimensions         []DimensionKind
	Currency           string
	BaseRate           values.Decimal
	DifferentialRate   values.Decimal
	OvertimeMultiplier values.Decimal
	EmployerBurdenRate values.Decimal
	Supersedes         string
	CanonicalDigest    string
}

func (r LaborRule) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Version) == "" {
		return fmt.Errorf("%w: id and version are required", ErrInvalidLaborRule)
	}
	if err := r.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidLaborRule, err)
	}
	if len(r.Dimensions) == 0 {
		return fmt.Errorf("%w: at least one dimension is required", ErrInvalidLaborRule)
	}
	seen := make(map[DimensionKind]struct{}, len(r.Dimensions))
	for _, kind := range r.Dimensions {
		if !kind.Valid() {
			return fmt.Errorf("%w: dimension %q is not declared", ErrInvalidLaborRule, kind)
		}
		if _, ok := seen[kind]; ok {
			return fmt.Errorf("%w: duplicate dimension %q", ErrInvalidLaborRule, kind)
		}
		seen[kind] = struct{}{}
	}
	for _, component := range []struct {
		name string
		ref  RuleRef
	}{
		{"worker", r.WorkerRef}, {"time", r.TimeRef}, {"entity", r.EntityRef},
		{"job", r.JobRef}, {"earning", r.EarningRef},
	} {
		if err := component.ref.Validate(); err != nil {
			return fmt.Errorf("%w: %s reference: %v", ErrInvalidLaborRule, component.name, err)
		}
	}
	if strings.TrimSpace(r.Currency) == "" {
		return fmt.Errorf("%w: currency is required", ErrInvalidLaborRule)
	}
	scale, rounding, err := validateRate("base_rate", r.BaseRate)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidLaborRule, err)
	}
	for _, item := range []struct {
		name  string
		value values.Decimal
	}{
		{"differential_rate", r.DifferentialRate},
		{"overtime_multiplier", r.OvertimeMultiplier},
		{"employer_burden_rate", r.EmployerBurdenRate},
	} {
		if err := validateRateAt(item.name, item.value, scale, rounding); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidLaborRule, err)
		}
	}
	if r.Supersedes != "" && r.Supersedes == r.Version {
		return fmt.Errorf("%w: a rule cannot supersede its own version", ErrInvalidLaborRule)
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidLaborRule)
	}
	return nil
}

func validateRate(name string, d values.Decimal) (int32, values.RoundingMode, error) {
	if err := d.Validate(); err != nil {
		return 0, values.RoundingUnspecified, fmt.Errorf("%s: %v", name, err)
	}
	if d.Sign() < 0 {
		return 0, values.RoundingUnspecified, fmt.Errorf("%s must be non-negative", name)
	}
	return d.Scale(), d.Rounding(), nil
}

func validateRateAt(name string, d values.Decimal, scale int32, rounding values.RoundingMode) error {
	if _, _, err := validateRate(name, d); err != nil {
		return err
	}
	if d.Scale() != scale || d.Rounding() != rounding {
		return fmt.Errorf("%s must use scale %d and rounding %s", name, scale, rounding)
	}
	return nil
}

func (r LaborRule) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.labor.LaborRule", 1).
		String("id", r.ID).String("version", r.Version).
		Value("effective", r.Effective).
		Value("worker_ref", r.WorkerRef).Value("time_ref", r.TimeRef).
		Value("entity_ref", r.EntityRef).Value("job_ref", r.JobRef).
		Value("earning_ref", r.EarningRef).
		String("currency", r.Currency).
		Value("base_rate", r.BaseRate).Value("differential_rate", r.DifferentialRate).
		Value("overtime_multiplier", r.OvertimeMultiplier).
		Value("employer_burden_rate", r.EmployerBurdenRate).
		String("supersedes", r.Supersedes).
		Count("dimensions", len(r.Dimensions))
	for _, kind := range r.Dimensions {
		w.String("dimension", kind.String())
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (r LaborRule) computedDigest() string {
	raw := r.body()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

func (r LaborRule) Canonical() []byte {
	if err := r.Validate(); err != nil {
		return nil
	}
	return r.body()
}

// Digest returns the canonical digest of a validated rule revision.
func (r LaborRule) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

// NewLaborRule validates and fills the digest of a new rule revision.
func NewLaborRule(rule LaborRule) (LaborRule, error) {
	rule.CanonicalDigest = ""
	if err := rule.Validate(); err != nil {
		return LaborRule{}, err
	}
	rule.CanonicalDigest = rule.computedDigest()
	return rule, nil
}

// NewVersion returns a new immutable version and leaves r unchanged.
func (r LaborRule) NewVersion(version string, effective values.EffectiveInterval) (LaborRule, error) {
	next := r
	next.Version = version
	next.Effective = effective
	next.Supersedes = r.Version
	next.CanonicalDigest = ""
	return NewLaborRule(next)
}

// Successor accepts a fully described new rule revision only when it points
// back to this version. The old revision is never edited.
func (r LaborRule) Successor(next LaborRule) (LaborRule, error) {
	if err := r.Validate(); err != nil {
		return LaborRule{}, err
	}
	if next.ID != r.ID || next.Version == r.Version || next.Supersedes != r.Version {
		return LaborRule{}, fmt.Errorf("%w: successor must retain id, use a new version and supersede %s", ErrRuleVersionConflict, r.Version)
	}
	return NewLaborRule(next)
}

// AllocationEntry assigns one exact decimal percentage to one dimension.
type AllocationEntry struct {
	Dimension Dimension
	Percent   values.Decimal
}

// DimensionAllocation is a descriptive alias for AllocationEntry.
type DimensionAllocation = AllocationEntry

// Allocation is a complete labor allocation. Percentages are exact fixed
// decimals and must total exactly 100 at their declared scale.
type Allocation struct {
	Entries     []AllocationEntry
	RuleID      string
	RuleVersion string
}

func (a Allocation) Validate() error {
	if len(a.Entries) == 0 {
		return fmt.Errorf("%w: at least one entry is required", ErrInvalidAllocation)
	}
	var total values.Decimal
	var scale int32
	var rounding values.RoundingMode
	seen := make(map[string]struct{}, len(a.Entries))
	for i, entry := range a.Entries {
		if err := entry.Dimension.Validate(); err != nil {
			return fmt.Errorf("%w: entry %d: %v", ErrInvalidAllocation, i, err)
		}
		if err := entry.Percent.Validate(); err != nil {
			return fmt.Errorf("%w: entry %d percent: %v", ErrInvalidAllocation, i, err)
		}
		if entry.Percent.Sign() < 0 {
			return fmt.Errorf("%w: entry %d percent is negative", ErrInvalidAllocation, i)
		}
		if i == 0 {
			scale, rounding = entry.Percent.Scale(), entry.Percent.Rounding()
			total = entry.Percent
		} else {
			if entry.Percent.Scale() != scale || entry.Percent.Rounding() != rounding {
				return fmt.Errorf("%w: entry %d percent precision differs", ErrInvalidAllocation, i)
			}
			var err error
			total, err = total.Add(entry.Percent)
			if err != nil {
				return fmt.Errorf("%w: sum: %v", ErrInvalidAllocation, err)
			}
		}
		key := entry.Dimension.Kind.String() + "\x00" + entry.Dimension.Value + "\x00" + entry.Dimension.Version
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate dimension %q", ErrInvalidAllocation, key)
		}
		seen[key] = struct{}{}
	}
	want, err := values.NewDecimal("100", scale, rounding)
	if err != nil {
		return fmt.Errorf("%w: total: %v", ErrInvalidAllocation, err)
	}
	if !total.Equal(want) {
		return fmt.Errorf("%w: got %s", ErrAllocationUnbalanced, total)
	}
	if (a.RuleID == "") != (a.RuleVersion == "") {
		return fmt.Errorf("%w: rule id and version must be supplied together", ErrInvalidAllocation)
	}
	return nil
}

func (a Allocation) canonicalEntries() []AllocationEntry {
	entries := append([]AllocationEntry(nil), a.Entries...)
	sort.Slice(entries, func(i, j int) bool {
		left, right := entries[i].Dimension, entries[j].Dimension
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Value != right.Value {
			return left.Value < right.Value
		}
		return left.Version < right.Version
	})
	return entries
}

func (a Allocation) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.labor.Allocation", 1).
		String("rule_id", a.RuleID).String("rule_version", a.RuleVersion).
		Count("entries", len(a.Entries))
	for _, entry := range a.canonicalEntries() {
		w.Value("dimension", entry.Dimension).Value("percent", entry.Percent)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (a Allocation) Canonical() []byte {
	if err := a.Validate(); err != nil {
		return nil
	}
	return a.body()
}

func (a Allocation) Digest() (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	return canonicalbytes.Digest(a.body()), nil
}

// AllocationExplanation records the exact total and stable allocation digest.
type AllocationExplanation struct {
	RuleID      string
	RuleVersion string
	Total       values.Decimal
	Entries     []AllocationEntry
	Digest      string
}

// Explain validates an allocation and returns a pure, reproducible explanation.
func (a Allocation) Explain() (AllocationExplanation, error) {
	if err := a.Validate(); err != nil {
		return AllocationExplanation{}, fmt.Errorf("%w: %w", ErrConformanceRejected, err)
	}
	total := a.Entries[0].Percent
	for _, entry := range a.Entries[1:] {
		var err error
		total, err = total.Add(entry.Percent)
		if err != nil {
			return AllocationExplanation{}, err
		}
	}
	digest, err := a.Digest()
	if err != nil {
		return AllocationExplanation{}, err
	}
	return AllocationExplanation{RuleID: a.RuleID, RuleVersion: a.RuleVersion, Total: total, Entries: append([]AllocationEntry(nil), a.canonicalEntries()...), Digest: digest}, nil
}

// Explain is the package-level spelling of Allocation.Explain.
func Explain(a Allocation) (AllocationExplanation, error) { return a.Explain() }

// RuleExplanation exposes the exact revision and effective interval without
// granting authority to evaluate or publish the rule.
type RuleExplanation struct {
	ID         string
	Version    string
	Supersedes string
	Effective  values.EffectiveInterval
	Digest     string
}

func (r LaborRule) Explain() (RuleExplanation, error) {
	if err := r.Validate(); err != nil {
		return RuleExplanation{}, err
	}
	digest, err := r.Digest()
	if err != nil {
		return RuleExplanation{}, err
	}
	return RuleExplanation{ID: r.ID, Version: r.Version, Supersedes: r.Supersedes, Effective: r.Effective, Digest: digest}, nil
}
