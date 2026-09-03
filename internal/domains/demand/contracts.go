// Package demand defines versioned workforce demand and coverage contracts.
//
// Demand is descriptive input to matching and planning. These contracts do
// not reserve positions, create assignments, or otherwise grant authority.
package demand

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// DemandSignal is one immutable, versioned assertion of workforce demand.
// Quantity and Unit are both retained: the former carries decimal precision
// and rounding semantics, while the latter makes the contract's unit explicit
// at its boundary.
type DemandSignal struct {
	SignalID   string
	Work       values.EffectiveInterval
	Location   string
	Quantity   values.Quantity
	Unit       string
	Skill      string
	Priority   int
	Source     string
	Confidence values.Decimal
	Scenario   string
	Version    string
}

// Validate checks every field required for a demand assertion. It deliberately
// rejects partial data instead of assigning defaults that could change
// matching results.
func (s DemandSignal) Validate() error {
	if err := s.Work.Validate(); err != nil {
		return fmt.Errorf("demand signal work interval: %w", err)
	}
	if strings.TrimSpace(s.Location) == "" {
		return errors.New("demand signal location is required")
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
	if strings.TrimSpace(s.Skill) == "" {
		return errors.New("demand signal skill is required")
	}
	if s.Priority < 0 {
		return errors.New("demand signal priority must not be negative")
	}
	if strings.TrimSpace(s.Source) == "" {
		return errors.New("demand signal source is required")
	}
	if err := s.Confidence.Validate(); err != nil {
		return fmt.Errorf("demand signal confidence: %w", err)
	}
	if s.Confidence.Sign() < 0 || s.Confidence.Cmp(values.MustDecimal("1", 0, values.RoundingHalfEven)) > 0 {
		return errors.New("demand signal confidence must be between zero and one")
	}
	if strings.TrimSpace(s.Scenario) == "" {
		return errors.New("demand signal scenario is required")
	}
	if strings.TrimSpace(s.Version) == "" {
		return errors.New("demand signal version is required")
	}
	return nil
}

// CoverageRequirement is an immutable composition of one or more demand
// signals. Signals are copied on construction and exposed only as a copy.
type CoverageRequirement struct {
	RequirementID string
	Scenario      string
	Version       string
	Signals       []DemandSignal
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
	for n, signal := range r.Signals {
		if err := signal.Validate(); err != nil {
			return CoverageRequirement{}, fmt.Errorf("coverage requirement signal %d: %w", n, err)
		}
		if signal.Scenario != scenario || signal.Version != version {
			return CoverageRequirement{}, fmt.Errorf("coverage requirement signal %d scenario/version does not match requirement", n)
		}
	}
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
	for n, signal := range r.Signals {
		if err := signal.Validate(); err != nil {
			return fmt.Errorf("coverage requirement signal %d: %w", n, err)
		}
		if signal.Scenario != r.Scenario || signal.Version != r.Version {
			return fmt.Errorf("coverage requirement signal %d scenario/version does not match requirement", n)
		}
	}
	return nil
}
