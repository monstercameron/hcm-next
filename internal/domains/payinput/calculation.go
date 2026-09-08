package payinput

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var (
	ErrInvalidCalculation = errors.New("payinput: invalid calculation")
	ErrOneTimeConsumed    = errors.New("payinput: one-time assignment already consumed")
)

// CalculationPeriod identifies the payroll period being evaluated. Amounts are
// decimals deliberately: this boundary never converts payroll money to float.
type CalculationPeriod struct {
	ID, Currency                                string
	Effective                                   values.EffectiveInterval
	Base, AvailableForDeduction, ProtectedFloor values.Decimal
}

// CalculationState is caller-owned replay/idempotency state. Calculate does
// not mutate it; persist the returned state after accepting the result.
type CalculationState struct {
	ConsumedOneTime          map[string]bool
	Arrears, RemainingLimits map[string]values.Decimal
}

type PayrollInputLine struct {
	AssignmentID, WorkerRef, DefinitionID, DefinitionDigest, Code string
	Kind                                                          DefinitionKind
	Currency                                                      string
	Amount, Applied, Deferred, RemainingLimit, RemainingArrears   values.Decimal
	// Explicit aliases keep serialized payroll lines readable to consumers.
	AppliedAmount, DeferredAmount, Arrears values.Decimal
	Taxable, Taxability                    map[JurisdictionClass]bool
	Recurrence                             Recurrence
	PeriodID                               string
	Trace                                  []string
	CanonicalDigest                        string
}
type CalculationResult struct {
	Lines        []PayrollInputLine
	State        CalculationState
	ReplayDigest string
}
type CalculationInput struct {
	Definition Definition
	Assignment WorkerAssignment
	Period     CalculationPeriod
	State      CalculationState
}

// Calculate evaluates one pinned assignment for one period. Limits are applied
// before deduction capacity; denied amounts remain explicit arrears.
func Calculate(input CalculationInput) (CalculationResult, error) {
	d, a, p := input.Definition, input.Assignment, input.Period
	if err := d.Validate(); err != nil {
		return CalculationResult{}, err
	}
	if err := a.Validate(); err != nil {
		return CalculationResult{}, err
	}
	r := a.definitionRef()
	if r.ID != d.id() || r.Version != d.Version || r.Digest != d.CanonicalDigest {
		return CalculationResult{}, ErrAssignmentDefinitionRef
	}
	if strings.TrimSpace(p.ID) == "" {
		return CalculationResult{}, fmt.Errorf("%w: period id is required", ErrInvalidCalculation)
	}
	if strings.TrimSpace(p.Currency) == "" || p.Currency != d.Currency {
		return CalculationResult{}, fmt.Errorf("%w: period currency must match definition currency", ErrInvalidCalculation)
	}
	if err := p.Effective.Validate(); err != nil {
		return CalculationResult{}, fmt.Errorf("%w: period effective interval: %v", ErrInvalidCalculation, err)
	}
	if err := admitRecurrence(a); err != nil {
		return CalculationResult{}, err
	}
	active, err := a.effective().Overlaps(p.Effective)
	if err != nil {
		return CalculationResult{}, err
	}
	if !active {
		return CalculationResult{State: cloneState(input.State)}, nil
	}
	if a.Recurrence == RecurrenceOneTime && input.State.ConsumedOneTime[a.id()] {
		return CalculationResult{}, ErrOneTimeConsumed
	}
	requested := a.Amount
	if a.Rate.Validate() == nil {
		if p.Base.Validate() != nil {
			return CalculationResult{}, fmt.Errorf("%w: percentage assignment requires period base", ErrInvalidCalculation)
		}
		requested, err = p.Base.Mul(a.Rate, 2, values.RoundingExactRequired)
		if err != nil {
			return CalculationResult{}, err
		}
	}
	if requested.Validate() != nil {
		return CalculationResult{}, fmt.Errorf("%w: amount is unset", ErrInvalidCalculation)
	}
	limits := d.limits()
	remaining := requested
	if x, ok := input.State.Arrears[a.id()]; ok {
		if x.Validate() != nil || x.Sign() < 0 {
			return CalculationResult{}, fmt.Errorf("%w: invalid arrears", ErrInvalidCalculation)
		}
		remaining, err = remaining.Add(x)
		if err != nil {
			return CalculationResult{}, err
		}
	}
	totalDue := remaining
	maximum := limits.Maximum
	if prior, ok := input.State.RemainingLimits[a.id()]; ok {
		if prior.Validate() != nil || prior.Sign() < 0 {
			return CalculationResult{}, fmt.Errorf("%w: invalid remaining limit", ErrInvalidCalculation)
		}
		if maximum.Validate() != nil || prior.Cmp(maximum) < 0 {
			maximum = prior
		}
	}
	if maximum.Validate() == nil && remaining.Cmp(maximum) > 0 {
		remaining = maximum
	}
	applied, deferred := remaining, zeroLike(remaining)
	if totalDue.Cmp(remaining) > 0 {
		deferred, err = totalDue.Sub(remaining)
		if err != nil {
			return CalculationResult{}, err
		}
	}
	if d.kind() == KindDeduction {
		if p.AvailableForDeduction.Validate() != nil || p.ProtectedFloor.Validate() != nil {
			return CalculationResult{}, fmt.Errorf("%w: deduction capacity and protected floor are required", ErrInvalidCalculation)
		}
		capacity, e := p.AvailableForDeduction.Sub(p.ProtectedFloor)
		if e != nil {
			return CalculationResult{}, e
		}
		if capacity.Sign() < 0 {
			capacity = zeroLike(capacity)
		}
		if applied.Cmp(capacity) > 0 {
			capacityDeferred, subErr := applied.Sub(capacity)
			if subErr != nil {
				return CalculationResult{}, subErr
			}
			applied = capacity
			deferred, err = deferred.Add(capacityDeferred)
			if err != nil {
				return CalculationResult{}, err
			}
		}
	}
	line := PayrollInputLine{AssignmentID: a.id(), WorkerRef: a.WorkerRef, DefinitionID: d.id(), DefinitionDigest: d.CanonicalDigest, Code: d.Code, Kind: d.kind(), Currency: d.Currency, Amount: requested, Applied: applied, Deferred: deferred, RemainingArrears: deferred, Taxable: definitionTaxabilityMap(d), Recurrence: a.Recurrence, PeriodID: p.ID, Trace: []string{"definition:" + d.Version, "assignment:" + a.id(), "period:" + p.ID}}
	line.AppliedAmount, line.DeferredAmount, line.Arrears, line.Taxability = line.Applied, line.Deferred, line.RemainingArrears, line.Taxable
	if maximum.Validate() == nil {
		line.RemainingLimit, err = maximum.Sub(applied)
		if err != nil {
			return CalculationResult{}, err
		}
		if line.RemainingLimit.Sign() < 0 {
			line.RemainingLimit = zeroLike(line.RemainingLimit)
		}
	}
	line.CanonicalDigest = canonicalbytes.Digest(lineCanonical(line))
	result := CalculationResult{Lines: []PayrollInputLine{line}, State: cloneState(input.State), ReplayDigest: line.CanonicalDigest}
	if result.State.ConsumedOneTime == nil {
		result.State.ConsumedOneTime = map[string]bool{}
	}
	if a.Recurrence == RecurrenceOneTime {
		result.State.ConsumedOneTime[a.id()] = true
	}
	if result.State.Arrears == nil {
		result.State.Arrears = map[string]values.Decimal{}
	}
	result.State.Arrears[a.id()] = line.RemainingArrears
	if line.RemainingLimit.Validate() == nil {
		result.State.RemainingLimits[a.id()] = line.RemainingLimit
	}
	return result, nil
}

func admitRecurrence(a WorkerAssignment) error {
	rule := strings.TrimSpace(a.RecurrenceRule)
	if rule == "" {
		rule = strings.TrimSpace(a.PeriodRule)
	}
	switch a.Recurrence {
	case RecurrenceOneTime:
		if rule != "" {
			return fmt.Errorf("%w: one-time assignment cannot declare a period rule", ErrInvalidCalculation)
		}
	case RecurrencePerPayroll:
		if rule != "PAY_PERIOD" {
			return fmt.Errorf("%w: unsupported per-payroll period rule %q", ErrInvalidCalculation, rule)
		}
	case RecurrenceMonthly, RecurrenceAnnual:
		return fmt.Errorf("%w: %s recurrence requires a governed period-admission contract", ErrInvalidCalculation, a.Recurrence)
	default:
		return fmt.Errorf("%w: unsupported recurrence %q", ErrInvalidCalculation, a.Recurrence)
	}
	return nil
}

func zeroLike(d values.Decimal) values.Decimal {
	z, _ := values.NewDecimal("0", d.Scale(), values.RoundingExactRequired)
	return z
}
func cloneState(s CalculationState) CalculationState {
	out := CalculationState{ConsumedOneTime: map[string]bool{}, Arrears: map[string]values.Decimal{}, RemainingLimits: map[string]values.Decimal{}}
	for k, v := range s.ConsumedOneTime {
		out.ConsumedOneTime[k] = v
	}
	for k, v := range s.Arrears {
		out.Arrears[k] = v
	}
	for k, v := range s.RemainingLimits {
		out.RemainingLimits[k] = v
	}
	return out
}
func definitionTaxabilityMap(d Definition) map[JurisdictionClass]bool {
	out := map[JurisdictionClass]bool{}
	flags, _ := d.taxability()
	for _, f := range flags {
		out[f.Jurisdiction] = f.Taxable
	}
	return out
}
func lineCanonical(l PayrollInputLine) []byte {
	w := canonicalbytes.New("hcmnext.domains.payinput.PayrollInputLine", schemaVersion).String("assignment", l.AssignmentID).String("worker", l.WorkerRef).String("definition", l.DefinitionID).String("definition_digest", l.DefinitionDigest).String("code", l.Code).String("kind", string(l.Kind)).String("currency", l.Currency).Value("amount", l.Amount).Value("applied", l.Applied).Value("deferred", l.Deferred).Optional("remaining_limit", l.RemainingLimit.Validate() == nil, l.RemainingLimit).Value("remaining_arrears", l.RemainingArrears).String("period", l.PeriodID).String("recurrence", string(l.Recurrence)).Count("trace", len(l.Trace))
	for _, x := range l.Trace {
		w.String("trace", x)
	}
	keys := make([]string, 0, len(l.Taxable))
	for k := range l.Taxable {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	for _, k := range keys {
		w.String("tax:"+k, fmt.Sprint(l.Taxable[JurisdictionClass(k)]))
	}
	b, _ := w.Bytes()
	return b
}
