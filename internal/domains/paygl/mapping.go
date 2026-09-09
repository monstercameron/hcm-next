package paygl

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrMappingRejected is the PAYGL-002 refusal boundary. Mapping is pure;
	// rejecting here means that no authoritative accounting effect exists.
	ErrMappingRejected = errors.New("PAYGL_002_REJECTED")
	// ErrComponentUnmapped identifies a component for which no active rule was
	// found.
	ErrComponentUnmapped = errors.New("paygl: payroll component is unmapped")
	// ErrComponentAmbiguous identifies more than one active rule for a split.
	ErrComponentAmbiguous = errors.New("paygl: payroll component has ambiguous rules")
	// ErrSplitUnbalanced identifies an allocation whose percentages or amounts
	// do not add to the component amount under the declared exact-decimal policy.
	ErrSplitUnbalanced = errors.New("paygl: component splits are unbalanced")
)

// PayrollComponent is one calculated payroll component. Ordinal is the
// stable position assigned by the calculation engine; it is used as the
// primary tie-break when a remainder is shared by otherwise equal candidates.
// A zero Ordinal is accepted and replaced with the input position (one-based).
type PayrollComponent struct {
	ID               string
	ComponentID      string
	Ordinal          int
	ComponentOrdinal int
	Kind             ComponentKind
	Type             ComponentKind
	Code             string
	ComponentCode    string
	Amount           values.Decimal
	Currency         string
	Dimension        labor.Dimension
	LaborDimension   labor.Dimension
	Allocation       labor.Allocation
	LaborAllocation  labor.Allocation
	Splits           []DimensionSplit
	Dimensions       []DimensionSplit
	Allocations      []labor.AllocationEntry
	RuleID           string
	RuleVersion      string
	EffectiveDate    values.LocalDate
	SourceDigest     string
}

// Component is the concise spelling for PayrollComponent.
type Component = PayrollComponent

// CalculatedComponent is a descriptive spelling for PayrollComponent.
type CalculatedComponent = PayrollComponent

// DimensionSplit describes the share of a component assigned to one governed
// labor dimension. Amount is optional on input; when present it is checked
// against the exact amount computed by the mapper.
type DimensionSplit struct {
	Dimension labor.Dimension
	Percent   values.Decimal
	Amount    values.Decimal
}

// LaborSplit and ComponentAllocation are descriptive aliases for
// DimensionSplit.
type LaborSplit = DimensionSplit
type ComponentAllocation = DimensionSplit

func (c PayrollComponent) id() string {
	if c.ID != "" {
		return c.ID
	}
	return c.ComponentID
}

func (c PayrollComponent) ordinal(defaultOrdinal int) int {
	if c.Ordinal > 0 {
		return c.Ordinal
	}
	if c.ComponentOrdinal > 0 {
		return c.ComponentOrdinal
	}
	return defaultOrdinal
}

func (c PayrollComponent) kind() ComponentKind {
	if c.Kind.Valid() {
		return c.Kind
	}
	return c.Type
}

func (c PayrollComponent) code() string {
	if c.Code != "" {
		return c.Code
	}
	return c.ComponentCode
}

func (c PayrollComponent) allocation() (labor.Allocation, error) {
	if len(c.Allocation.Entries) > 0 {
		return c.Allocation, nil
	}
	if len(c.LaborAllocation.Entries) > 0 {
		return c.LaborAllocation, nil
	}
	entries := c.Allocations
	if len(entries) == 0 && len(c.Splits) > 0 {
		entries = make([]labor.AllocationEntry, len(c.Splits))
		for i, split := range c.Splits {
			entries[i] = labor.AllocationEntry{Dimension: split.Dimension, Percent: split.Percent}
		}
	}
	if len(entries) == 0 && len(c.Dimensions) > 0 {
		entries = make([]labor.AllocationEntry, len(c.Dimensions))
		for i, split := range c.Dimensions {
			entries[i] = labor.AllocationEntry{Dimension: split.Dimension, Percent: split.Percent}
		}
	}
	if len(entries) > 0 {
		return labor.Allocation{Entries: entries, RuleID: c.RuleID, RuleVersion: c.RuleVersion}, nil
	}
	if c.Dimension.Kind != "" || c.Dimension.Value != "" || c.Dimension.Version != "" {
		return fullAllocation(c.Dimension, c.RuleID, c.RuleVersion)
	}
	if c.LaborDimension.Kind != "" || c.LaborDimension.Value != "" || c.LaborDimension.Version != "" {
		return fullAllocation(c.LaborDimension, c.RuleID, c.RuleVersion)
	}
	return labor.Allocation{}, nil
}

func fullAllocation(dimension labor.Dimension, ruleID, ruleVersion string) (labor.Allocation, error) {
	percent, err := values.NewDecimal("100", 2, values.RoundingExactRequired)
	if err != nil {
		return labor.Allocation{}, err
	}
	return labor.Allocation{Entries: []labor.AllocationEntry{{
		Dimension: dimension,
		Percent:   percent,
	}}, RuleID: ruleID, RuleVersion: ruleVersion}, nil
}

func (c PayrollComponent) validate(ordinal int) error {
	if strings.TrimSpace(c.id()) == "" {
		return fmt.Errorf("%w: component id is required", ErrMappingRejected)
	}
	if !c.kind().Valid() || strings.TrimSpace(c.code()) == "" {
		return fmt.Errorf("%w: component kind and code are required", ErrMappingRejected)
	}
	if err := c.Amount.Validate(); err != nil {
		return fmt.Errorf("%w: component amount: %v", ErrMappingRejected, err)
	}
	if c.Amount.Sign() < 0 {
		return fmt.Errorf("%w: component amount cannot be negative", ErrMappingRejected)
	}
	if strings.TrimSpace(c.Currency) == "" {
		return fmt.Errorf("%w: component currency is required", ErrMappingRejected)
	}
	if c.RuleID == "" && c.RuleVersion != "" || c.RuleID != "" && c.RuleVersion == "" {
		return fmt.Errorf("%w: component rule id and version must be paired", ErrMappingRejected)
	}
	if c.ordinal(ordinal) <= 0 {
		return fmt.Errorf("%w: component ordinal must be positive", ErrMappingRejected)
	}
	return nil
}

// ComponentMapping is one exact split mapped to exactly one debit and one
// credit account. ComponentAmount is retained so each output can be audited
// back to its source component without reassembling the input.
type ComponentMapping struct {
	ComponentID      string
	ComponentOrdinal int
	ComponentKind    ComponentKind
	ComponentCode    string
	ComponentAmount  values.Decimal
	EffectiveDate    values.LocalDate
	SplitOrdinal     int
	Percent          values.Decimal
	Amount           values.Decimal
	Currency         string
	Dimension        labor.Dimension
	DebitAccount     string
	CreditAccount    string
	RuleID           string
	RuleVersion      string
	RuleDigest       string
	SourceDigest     string
}

// AccountMapping is a descriptive alias for ComponentMapping.
type AccountMapping = ComponentMapping

// MappingResult is the immutable, content-addressed result of mapping every
// component in one calculated run. Mappings are ordered by component ordinal,
// then split ordinal, and never depend on map or goroutine scheduling.
type MappingResult struct {
	RunID                string
	RunRevision          uint64
	RunDigest            string
	Mappings             []ComponentMapping
	TotalComponentAmount values.Decimal
	TotalSplitAmount     values.Decimal
	Digest               string
}

// ComponentMappingResult is a descriptive alias for MappingResult.
type ComponentMappingResult = MappingResult

func (r MappingResult) Validate() error {
	if strings.TrimSpace(r.RunID) == "" || r.RunRevision == 0 || len(r.Mappings) == 0 {
		return fmt.Errorf("%w: run and mappings are required", ErrMappingRejected)
	}
	if strings.TrimSpace(r.RunDigest) == "" {
		return fmt.Errorf("%w: run digest is required", ErrMappingRejected)
	}
	if err := r.TotalComponentAmount.Validate(); err != nil {
		return fmt.Errorf("%w: component total: %v", ErrMappingRejected, err)
	}
	if err := r.TotalSplitAmount.Validate(); err != nil {
		return fmt.Errorf("%w: split total: %v", ErrMappingRejected, err)
	}
	if !r.TotalComponentAmount.Equal(r.TotalSplitAmount) {
		return ErrSplitUnbalanced
	}
	for i, mapping := range r.Mappings {
		if err := validateMapping(mapping); err != nil {
			return fmt.Errorf("%w: mapping %d: %v", ErrMappingRejected, i, err)
		}
	}
	return nil
}

func validateMapping(mapping ComponentMapping) error {
	if strings.TrimSpace(mapping.ComponentID) == "" || mapping.ComponentOrdinal <= 0 || mapping.SplitOrdinal <= 0 {
		return errors.New("component and split ordinals are required")
	}
	if !mapping.ComponentKind.Valid() || strings.TrimSpace(mapping.ComponentCode) == "" {
		return errors.New("component kind and code are required")
	}
	if err := mapping.ComponentAmount.Validate(); err != nil {
		return fmt.Errorf("component amount: %v", err)
	}
	if err := mapping.Amount.Validate(); err != nil {
		return fmt.Errorf("split amount: %v", err)
	}
	if mapping.Amount.Sign() < 0 || mapping.ComponentAmount.Sign() < 0 {
		return errors.New("amount cannot be negative")
	}
	if err := mapping.Percent.Validate(); err != nil {
		return fmt.Errorf("percent: %v", err)
	}
	if mapping.Percent.Sign() < 0 {
		return errors.New("percent cannot be negative")
	}
	if err := mapping.Dimension.Validate(); err != nil {
		return fmt.Errorf("dimension: %v", err)
	}
	if mapping.EffectiveDate.IsSet() {
		if err := mapping.EffectiveDate.Validate(); err != nil {
			return fmt.Errorf("effective date: %v", err)
		}
	}
	if strings.TrimSpace(mapping.Currency) == "" || strings.TrimSpace(mapping.DebitAccount) == "" || strings.TrimSpace(mapping.CreditAccount) == "" {
		return errors.New("currency and both accounts are required")
	}
	if strings.TrimSpace(mapping.RuleID) == "" || strings.TrimSpace(mapping.RuleVersion) == "" || strings.TrimSpace(mapping.RuleDigest) == "" {
		return errors.New("rule lineage is required")
	}
	return nil
}

func (r MappingResult) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.paygl.MappingResult", schemaVersion).
		String("run_id", r.RunID).Int("run_revision", int64(r.RunRevision)).
		String("run_digest", r.RunDigest).Value("total_component_amount", r.TotalComponentAmount).
		Value("total_split_amount", r.TotalSplitAmount).Count("mapping", len(r.Mappings))
	for _, mapping := range r.Mappings {
		w.String("component_id", mapping.ComponentID).
			Int("component_ordinal", int64(mapping.ComponentOrdinal)).
			String("component_kind", string(mapping.ComponentKind)).
			String("component_code", mapping.ComponentCode).
			Value("component_amount", mapping.ComponentAmount).
			Optional("effective_date", mapping.EffectiveDate.IsSet(), mapping.EffectiveDate).
			Int("split_ordinal", int64(mapping.SplitOrdinal)).
			Value("percent", mapping.Percent).Value("amount", mapping.Amount).
			String("currency", mapping.Currency).Value("dimension", mapping.Dimension).
			String("debit_account", mapping.DebitAccount).String("credit_account", mapping.CreditAccount).
			String("rule_id", mapping.RuleID).String("rule_version", mapping.RuleVersion).
			String("rule_digest", mapping.RuleDigest).String("source_digest", mapping.SourceDigest)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Canonical returns the stable result encoding, or nil when the result is
// incomplete or unbalanced.
func (r MappingResult) Canonical() []byte {
	if err := r.Validate(); err != nil {
		return nil
	}
	return r.body()
}

// Digest returns the canonical digest of a valid mapping result.
func (r MappingResult) DigestValue() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return canonicalbytes.Digest(r.body()), nil
}

// Explain returns an audit-safe narrative without exposing any authority or
// persistence behavior.
func (r MappingResult) Explain() string {
	return fmt.Sprintf("payroll GL mapping run %s@%d: %d mappings, total %s, digest %s", r.RunID, r.RunRevision, len(r.Mappings), r.TotalSplitAmount.String(), r.Digest)
}

// ExplainMapping is the functional spelling of MappingResult.Explain.
func ExplainMapping(r MappingResult) string { return r.Explain() }

// MapPayrollComponents maps every component to one account pair per exact
// labor-dimension split. The run must be CALCULATED and every selected rule
// must be valid and uniquely active for the component.
func MapPayrollComponents(run payroll.PayrollRun, rules []AccountingRule, components []PayrollComponent) (MappingResult, error) {
	if err := run.Validate(); err != nil {
		return MappingResult{}, err
	}
	if run.State != payroll.PayrollRunStateCalculated {
		return MappingResult{}, fmt.Errorf("%w: run state must be CALCULATED", ErrMappingRejected)
	}
	if len(rules) == 0 || len(components) == 0 {
		return MappingResult{}, fmt.Errorf("%w: rules and components are required", ErrMappingRejected)
	}
	validatedRules := make([]AccountingRule, len(rules))
	for i, rule := range rules {
		if err := rule.Validate(); err != nil {
			return MappingResult{}, fmt.Errorf("%w: rule %d: %v", ErrMappingRejected, i, err)
		}
		if len(rule.dimensions()) != 1 {
			return MappingResult{}, fmt.Errorf("%w: rule %d must name exactly one labor dimension", ErrMappingRejected, i)
		}
		validatedRules[i] = rule
	}
	runDigest, err := run.Digest()
	if err != nil {
		return MappingResult{}, err
	}
	ordered := make([]PayrollComponent, len(components))
	copy(ordered, components)
	for i := range ordered {
		if err := ordered[i].validate(i + 1); err != nil {
			return MappingResult{}, fmt.Errorf("%w: component %d: %v", ErrMappingRejected, i, err)
		}
		if ordered[i].Ordinal == 0 && ordered[i].ComponentOrdinal == 0 {
			ordered[i].Ordinal = i + 1
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].ordinal(i+1) < ordered[j].ordinal(j+1)
	})
	for i := 1; i < len(ordered); i++ {
		if ordered[i-1].ordinal(i) == ordered[i].ordinal(i+1) {
			return MappingResult{}, fmt.Errorf("%w: duplicate component ordinal %d", ErrMappingRejected, ordered[i].ordinal(i+1))
		}
	}

	result := MappingResult{RunID: run.RunID, RunRevision: run.Revision, RunDigest: runDigest}
	for i, component := range ordered {
		allocation, err := component.allocation()
		if err != nil {
			return MappingResult{}, fmt.Errorf("%w: component %q allocation: %w", ErrMappingRejected, component.id(), err)
		}
		if len(allocation.Entries) == 0 {
			allocation, err = inferSingleRuleAllocation(component, validatedRules)
			if err != nil {
				return MappingResult{}, err
			}
		}
		if allocation.RuleID == "" && component.RuleID != "" {
			allocation.RuleID, allocation.RuleVersion = component.RuleID, component.RuleVersion
		}
		if err := allocation.Validate(); err != nil {
			return MappingResult{}, fmt.Errorf("%w: component %q allocation: %w", ErrMappingRejected, component.id(), err)
		}
		mapped, err := mapComponent(component, i+1, allocation, validatedRules)
		if err != nil {
			return MappingResult{}, err
		}
		result.Mappings = append(result.Mappings, mapped...)
		if err := addTotal(&result.TotalComponentAmount, component.Amount); err != nil {
			return MappingResult{}, err
		}
		for _, mapping := range mapped {
			if err := addTotal(&result.TotalSplitAmount, mapping.Amount); err != nil {
				return MappingResult{}, err
			}
		}
	}
	if !result.TotalComponentAmount.Equal(result.TotalSplitAmount) {
		return MappingResult{}, ErrSplitUnbalanced
	}
	result.Digest, err = result.DigestValue()
	if err != nil {
		return MappingResult{}, err
	}
	return result, nil
}

// MapComponents accepts either (run, rules, components) or
// (run, components, rules). The explicitly typed MapPayrollComponents is the
// preferred spelling; this compatibility form keeps the mapper convenient for
// callers that begin with the component fixture.
func MapComponents(run payroll.PayrollRun, first, second any) (MappingResult, error) {
	switch a := first.(type) {
	case []AccountingRule:
		components, ok := second.([]PayrollComponent)
		if !ok {
			return MappingResult{}, fmt.Errorf("%w: expected []paygl.PayrollComponent", ErrMappingRejected)
		}
		return MapPayrollComponents(run, a, components)
	case []PayrollComponent:
		rules, ok := second.([]AccountingRule)
		if !ok {
			return MappingResult{}, fmt.Errorf("%w: expected []paygl.AccountingRule", ErrMappingRejected)
		}
		return MapPayrollComponents(run, rules, a)
	default:
		return MappingResult{}, fmt.Errorf("%w: first argument must be rules or components", ErrMappingRejected)
	}
}

// MapComponentAccounts is an explicit alias for MapPayrollComponents.
func MapComponentAccounts(run payroll.PayrollRun, rules []AccountingRule, components []PayrollComponent) (MappingResult, error) {
	return MapPayrollComponents(run, rules, components)
}

func inferSingleRuleAllocation(component PayrollComponent, rules []AccountingRule) (labor.Allocation, error) {
	var matches []AccountingRule
	for _, rule := range rules {
		if rule.ComponentKind != component.kind() || rule.componentCode() != component.code() || rule.Currency != component.Currency {
			continue
		}
		if component.RuleID != "" && (rule.ID != component.RuleID || rule.Version != component.RuleVersion) {
			continue
		}
		if component.EffectiveDate.IsSet() {
			inEffect, err := rule.Effective.ContainsDate(component.EffectiveDate)
			if err != nil {
				return labor.Allocation{}, fmt.Errorf("%w: effective date: %v", ErrMappingRejected, err)
			}
			if !inEffect {
				continue
			}
		}
		matches = append(matches, rule)
	}
	if len(matches) == 0 {
		return labor.Allocation{}, fmt.Errorf("%w: %w: %s/%s", ErrMappingRejected, ErrComponentUnmapped, component.id(), component.code())
	}
	if len(matches) != 1 {
		return labor.Allocation{}, fmt.Errorf("%w: %w: %s/%s", ErrMappingRejected, ErrComponentAmbiguous, component.id(), component.code())
	}
	return fullAllocation(matches[0].dimensions()[0], matches[0].ID, matches[0].Version)
}

type splitCandidate struct {
	entry            labor.AllocationEntry
	quotient         *big.Int
	remainder        *big.Int
	ordinal          int
	componentOrdinal int
	rule             AccountingRule
	ruleDigest       string
}

func mapComponent(component PayrollComponent, position int, allocation labor.Allocation, rules []AccountingRule) ([]ComponentMapping, error) {
	amount := component.Amount
	entries := append([]labor.AllocationEntry(nil), allocation.Entries...)
	sort.Slice(entries, func(i, j int) bool {
		return dimensionKey(entries[i].Dimension) < dimensionKey(entries[j].Dimension)
	})
	denominatorBase := new(big.Int).Mul(big.NewInt(100), pow10(entries[0].Percent.Scale()))
	candidates := make([]splitCandidate, 0, len(entries))
	for i, entry := range entries {
		rule, err := activeRule(component, entry.Dimension, allocation.RuleID, allocation.RuleVersion, rules)
		if err != nil {
			return nil, fmt.Errorf("%w: component %q split %d: %w", ErrMappingRejected, component.id(), i+1, err)
		}
		numerator := new(big.Int).Mul(amount.Unscaled(), entry.Percent.Unscaled())
		quotient, remainder := new(big.Int).QuoRem(numerator, denominatorBase, new(big.Int))
		candidates = append(candidates, splitCandidate{entry: entry, quotient: quotient, remainder: remainder, ordinal: i + 1, componentOrdinal: component.ordinal(position), rule: rule})
	}
	baseTotal := big.NewInt(0)
	for _, candidate := range candidates {
		baseTotal.Add(baseTotal, candidate.quotient)
	}
	residual := new(big.Int).Sub(amount.Unscaled(), baseTotal)
	if residual.Sign() < 0 || !residual.IsInt64() || residual.Int64() > int64(len(candidates)) {
		return nil, fmt.Errorf("%w: component %q residual minor units are invalid", ErrMappingRejected, component.id())
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if comparison := candidates[i].remainder.Cmp(candidates[j].remainder); comparison != 0 {
			return comparison > 0
		}
		if candidates[i].componentOrdinal != candidates[j].componentOrdinal {
			return candidates[i].componentOrdinal < candidates[j].componentOrdinal
		}
		if candidates[i].ordinal != candidates[j].ordinal {
			return candidates[i].ordinal < candidates[j].ordinal
		}
		return dimensionKey(candidates[i].entry.Dimension) < dimensionKey(candidates[j].entry.Dimension)
	})
	for i := 0; i < int(residual.Int64()); i++ {
		candidates[i].quotient.Add(candidates[i].quotient, big.NewInt(1))
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return dimensionKey(candidates[i].entry.Dimension) < dimensionKey(candidates[j].entry.Dimension)
	})

	mappings := make([]ComponentMapping, 0, len(candidates))
	var splitTotal values.Decimal
	for i, candidate := range candidates {
		splitAmount, err := values.NewDecimal(minorUnitText(candidate.quotient, amount.Scale()), amount.Scale(), amount.Rounding())
		if err != nil {
			return nil, fmt.Errorf("%w: component %q split amount: %v", ErrMappingRejected, component.id(), err)
		}
		if i == 0 {
			splitTotal = splitAmount
		} else {
			splitTotal, err = splitTotal.Add(splitAmount)
			if err != nil {
				return nil, err
			}
		}
		if split, ok := splitForDimension(component, candidate.entry.Dimension); ok && split.Amount.Validate() == nil && !split.Amount.Equal(splitAmount) {
			return nil, fmt.Errorf("%w: component %q split %d amount is %s, computed %s", ErrMappingRejected, component.id(), i+1, split.Amount, splitAmount)
		}
		if candidate.ruleDigest == "" {
			candidate.ruleDigest, err = candidate.rule.Digest()
			if err != nil {
				return nil, err
			}
		}
		mappings = append(mappings, ComponentMapping{
			ComponentID: component.id(), ComponentOrdinal: component.ordinal(position), ComponentKind: component.kind(), ComponentCode: component.code(),
			ComponentAmount: amount, SplitOrdinal: i + 1, Percent: candidate.entry.Percent, Amount: splitAmount, Currency: component.Currency,
			EffectiveDate: component.EffectiveDate,
			Dimension:     candidate.entry.Dimension, DebitAccount: mustAccount(candidate.rule.DebitAccount, candidate.rule.DebitAccountRef), CreditAccount: mustAccount(candidate.rule.CreditAccount, candidate.rule.CreditAccountRef),
			RuleID: candidate.rule.ID, RuleVersion: candidate.rule.Version, RuleDigest: candidate.ruleDigest, SourceDigest: component.SourceDigest,
		})
	}
	if !splitTotal.Equal(amount) {
		return nil, fmt.Errorf("%w: component %q splits total %s, want %s", ErrMappingRejected, component.id(), splitTotal, amount)
	}
	return mappings, nil
}

func splitForDimension(component PayrollComponent, dimension labor.Dimension) (DimensionSplit, bool) {
	for _, split := range component.Splits {
		if split.Dimension == dimension {
			return split, true
		}
	}
	for _, split := range component.Dimensions {
		if split.Dimension == dimension {
			return split, true
		}
	}
	return DimensionSplit{}, false
}

func activeRule(component PayrollComponent, dimension labor.Dimension, allocationRuleID, allocationRuleVersion string, rules []AccountingRule) (AccountingRule, error) {
	var match *AccountingRule
	for i := range rules {
		rule := &rules[i]
		dimensions := rule.dimensions()
		if len(dimensions) != 1 || rule.ComponentKind != component.kind() || rule.componentCode() != component.code() || rule.Currency != component.Currency || dimensions[0] != dimension {
			continue
		}
		if component.RuleID != "" && (rule.ID != component.RuleID || rule.Version != component.RuleVersion) {
			continue
		}
		if allocationRuleID != "" && (rule.ID != allocationRuleID || rule.Version != allocationRuleVersion) {
			continue
		}
		if component.EffectiveDate.IsSet() {
			inEffect, err := rule.Effective.ContainsDate(component.EffectiveDate)
			if err != nil {
				return AccountingRule{}, fmt.Errorf("effective date: %v", err)
			}
			if !inEffect {
				continue
			}
		}
		if match != nil {
			return AccountingRule{}, fmt.Errorf("%w: %s/%s", ErrComponentAmbiguous, component.id(), component.code())
		}
		match = rule
	}
	if match == nil {
		return AccountingRule{}, fmt.Errorf("%w: %s/%s", ErrComponentUnmapped, component.id(), component.code())
	}
	return *match, nil
}

func dimensionKey(dimension labor.Dimension) string {
	return string(dimension.Kind) + "\x00" + dimension.Value + "\x00" + dimension.Version
}

func pow10(scale int32) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
}

func minorUnitText(units *big.Int, scale int32) string {
	digits := units.String()
	if scale == 0 {
		return digits
	}
	if int32(len(digits)) <= scale {
		digits = strings.Repeat("0", int(scale)-len(digits)+1) + digits
	}
	cut := len(digits) - int(scale)
	return digits[:cut] + "." + digits[cut:]
}

func addTotal(total *values.Decimal, amount values.Decimal) error {
	if total.Validate() != nil {
		var err error
		*total, err = amount.WithRounding(amount.Rounding())
		return err
	}
	var err error
	*total, err = total.Add(amount)
	return err
}

func mustAccount(primary, alias string) string {
	if primary != "" {
		return primary
	}
	return alias
}
