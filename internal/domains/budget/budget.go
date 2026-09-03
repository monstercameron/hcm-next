// Package budget owns typed observations of workforce budget authority.
//
// An observation is evidence of what an incumbent finance or planning system
// reported; it is not a reservation and never grants spending authority.
package budget

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var (
	ErrInvalidObservation = errors.New("budget: invalid authority observation")
	ErrWrongUnit          = errors.New("budget: unit is incompatible with budget type")
	ErrWrongCurrency      = errors.New("budget: currency is incompatible with unit")
	ErrStale              = errors.New("budget: source watermark is stale")
	ErrExhausted          = errors.New("budget: available quantity is exhausted")
)

type BudgetType string

const (
	HeadcountCapacity BudgetType = "HEADCOUNT_CAPACITY"
	CompensationPool  BudgetType = "COMPENSATION_POOL"
	FinanceCostBudget BudgetType = "FINANCE_COST_BUDGET"
)

func (t BudgetType) Valid() bool {
	return t == HeadcountCapacity || t == CompensationPool || t == FinanceCostBudget
}

type Unit string

const (
	UnitHead    Unit = "HEAD"
	UnitFTE     Unit = "FTE"
	UnitMoney   Unit = "MONEY"
	UnitPercent Unit = "PERCENT"
)

func (u Unit) Valid() bool {
	return u == UnitHead || u == UnitFTE || u == UnitMoney || u == UnitPercent
}

// ObservationEvidence identifies the source reading and its freshness.
type ObservationEvidence struct {
	ObservationID   string
	SourceWatermark values.Instant
	RetrievedAt     values.Instant
	Digest          string
}

func (e ObservationEvidence) Validate() error {
	if e.ObservationID == "" || e.Digest == "" {
		return fmt.Errorf("%w: evidence requires observation id and digest", ErrInvalidObservation)
	}
	if err := e.SourceWatermark.Validate(); err != nil {
		return fmt.Errorf("%w: source watermark: %w", ErrInvalidObservation, err)
	}
	if err := e.RetrievedAt.Validate(); err != nil {
		return fmt.Errorf("%w: retrieved at: %w", ErrInvalidObservation, err)
	}
	if e.RetrievedAt.Before(e.SourceWatermark) {
		return fmt.Errorf("%w: retrieved at precedes source watermark", ErrInvalidObservation)
	}
	return nil
}

// BudgetAuthorityRef is a typed, immutable answer from a budget authority.
// AvailableQuantity is an exact fixed-decimal value; Currency is required only
// for MONEY and is deliberately not inferred for other units.
type BudgetAuthorityRef struct {
	BudgetType        BudgetType
	OwnerSystem       string
	Scope             string
	Period            string
	Currency          string
	Unit              Unit
	BaselineVersion   string
	AvailableQuantity values.Decimal
	Evidence          ObservationEvidence
}

func (b BudgetAuthorityRef) Validate() error {
	if !b.BudgetType.Valid() {
		return fmt.Errorf("%w: unknown budget type %q", ErrInvalidObservation, b.BudgetType)
	}
	if b.OwnerSystem == "" || b.Scope == "" || b.Period == "" || b.BaselineVersion == "" {
		return fmt.Errorf("%w: owner, scope, period and baseline are required", ErrInvalidObservation)
	}
	if !b.Unit.Valid() {
		return fmt.Errorf("%w: unknown unit %q", ErrInvalidObservation, b.Unit)
	}
	if err := b.AvailableQuantity.Validate(); err != nil {
		return fmt.Errorf("%w: available quantity: %w", ErrInvalidObservation, err)
	}
	if b.AvailableQuantity.Sign() < 0 {
		return fmt.Errorf("%w: negative available quantity", ErrInvalidObservation)
	}
	if b.AvailableQuantity.IsZero() {
		return ErrExhausted
	}
	if b.Unit == UnitMoney {
		if b.Currency == "" {
			return ErrWrongCurrency
		}
		// NewMoney validates the ISO-4217 shape without using floating point.
		if _, err := values.NewMoneyFromDecimal(b.AvailableQuantity, b.Currency); err != nil {
			return fmt.Errorf("%w: %v", ErrWrongCurrency, err)
		}
	} else if b.Currency != "" {
		return ErrWrongCurrency
	}
	switch b.BudgetType {
	case HeadcountCapacity:
		if b.Unit != UnitHead && b.Unit != UnitFTE {
			return ErrWrongUnit
		}
	case CompensationPool, FinanceCostBudget:
		if b.Unit != UnitMoney {
			return ErrWrongUnit
		}
	}
	return b.Evidence.Validate()
}

// Fresh reports whether the source reading is within maxAge of asOf.
func (b BudgetAuthorityRef) Fresh(asOf time.Time, maxAge time.Duration) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if maxAge < 0 {
		return fmt.Errorf("%w: negative freshness window", ErrInvalidObservation)
	}
	// A reading cannot be fresh relative to an as-of instant that predates the
	// reading itself. Treat this as invalid observation data rather than a
	// negative age (which would otherwise pass the freshness check).
	watermark := b.Evidence.SourceWatermark.Time()
	if asOf.Before(watermark) {
		return fmt.Errorf("%w: as-of precedes source watermark", ErrInvalidObservation)
	}
	if maxAge > 0 && asOf.Sub(watermark) > maxAge {
		return ErrStale
	}
	return nil
}

// Canonical returns a stable digest input, or nil when invalid.
func (b BudgetAuthorityRef) Canonical() []byte {
	if b.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.budget.BudgetAuthorityRef", 1).
		String("budget_type", string(b.BudgetType)).String("owner_system", b.OwnerSystem).
		String("scope", b.Scope).String("period", b.Period).String("currency", b.Currency).
		String("unit", string(b.Unit)).String("baseline_version", b.BaselineVersion).
		Value("available_quantity", b.AvailableQuantity).String("observation_id", b.Evidence.ObservationID).
		Value("source_watermark", b.Evidence.SourceWatermark).
		Value("retrieved_at", b.Evidence.RetrievedAt).
		String("digest", b.Evidence.Digest).Bytes()
	if err != nil {
		return nil
	}
	return raw
}
