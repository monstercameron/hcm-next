package equity

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidLotEvent    = errors.New("equity: invalid vesting lot event")
	ErrInvalidEffect      = errors.New("equity: invalid tax payroll effect")
	ErrInvalidObservation = errors.New("equity: invalid provider observation")
	ErrCorrectionMismatch = errors.New("equity: correction is not an append-only successor")
	ErrForfeitureVested   = errors.New("equity: vested quantity cannot be forfeited")
)

// LotEventKind is an append-only fact about one scheduled lot.
type LotEventKind string

const (
	LotVested    LotEventKind = "VESTED"
	LotForfeited LotEventKind = "FORFEITED"
)

type VestingLotEvent struct {
	GrantDigest     string
	GrantRevision   uint64
	Sequence        int
	Kind            LotEventKind
	Quantity        values.Decimal
	EffectiveDate   values.LocalDate
	EvidenceRef     string
	CanonicalDigest string
	Digest          string
}

func (e VestingLotEvent) Validate() error {
	if strings.TrimSpace(e.GrantDigest) == "" || e.GrantRevision == 0 || e.Sequence <= 0 {
		return fmt.Errorf("%w: grant binding and positive sequence are required", ErrInvalidLotEvent)
	}
	if e.Kind != LotVested && e.Kind != LotForfeited {
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidLotEvent, e.Kind)
	}
	if err := e.Quantity.Validate(); err != nil || e.Quantity.Sign() <= 0 {
		return fmt.Errorf("%w: quantity must be positive: %v", ErrInvalidLotEvent, err)
	}
	if err := e.EffectiveDate.Validate(); err != nil {
		return fmt.Errorf("%w: effective date: %v", ErrInvalidLotEvent, err)
	}
	if strings.TrimSpace(e.EvidenceRef) == "" {
		return fmt.Errorf("%w: evidence_ref is required", ErrInvalidLotEvent)
	}
	if e.CanonicalDigest != "" && e.CanonicalDigest != e.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidLotEvent)
	}
	if e.Digest != "" && e.Digest != e.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidLotEvent)
	}
	return nil
}
func (e VestingLotEvent) canonical() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.equity.VestingLotEvent", schemaVersion).
		String("grant_digest", e.GrantDigest).Int("grant_revision", int64(e.GrantRevision)).Int("sequence", int64(e.Sequence)).
		String("kind", string(e.Kind)).Value("quantity", e.Quantity).Value("effective_date", e.EffectiveDate).String("evidence_ref", e.EvidenceRef).Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (e VestingLotEvent) computedDigest() string { return canonicalbytes.Digest(e.canonical()) }
func (e VestingLotEvent) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	return e.canonical()
}
func NewVestingLotEvent(e VestingLotEvent) (VestingLotEvent, error) {
	e.CanonicalDigest, e.Digest = "", ""
	if err := e.Validate(); err != nil {
		return VestingLotEvent{}, err
	}
	e.CanonicalDigest = e.computedDigest()
	e.Digest = e.CanonicalDigest
	return e, nil
}

type VestingLot struct {
	Sequence    int
	VestsOn     values.LocalDate
	Quantity    values.Decimal
	Vested      bool
	Forfeited   bool
	EventDigest string
}
type VestingPosition struct {
	GrantDigest       string
	AsOf              values.LocalDate
	Lots              []VestingLot
	VestedQuantity    values.Decimal
	UnvestedQuantity  values.Decimal
	ForfeitedQuantity values.Decimal
}

func addQuantity(total values.Decimal, quantity values.Decimal) (values.Decimal, error) {
	next, err := total.Add(quantity)
	if err != nil {
		return values.Decimal{}, fmt.Errorf("%w: quantity scale mismatch: %v", ErrInvalidLotEvent, err)
	}
	return next, nil
}

// CalculateVestingPosition evaluates scheduled tranches and append-only lot events. Events cannot alter a vested lot.
func CalculateVestingPosition(grant EquityGrant, asOf values.LocalDate, events []VestingLotEvent) (VestingPosition, error) {
	if err := grant.Validate(); err != nil {
		return VestingPosition{}, err
	}
	if err := asOf.Validate(); err != nil {
		return VestingPosition{}, fmt.Errorf("%w: as_of: %v", ErrInvalidLotEvent, err)
	}
	tranches, err := grant.Vesting.Compute(grant.GrantDate, grant.Quantity)
	if err != nil {
		return VestingPosition{}, err
	}
	pos := VestingPosition{GrantDigest: grant.CanonicalDigest, AsOf: asOf, Lots: make([]VestingLot, 0, len(tranches))}
	zero, zerr := values.NewDecimal("0", grant.Quantity.Scale(), grant.Quantity.Rounding())
	if zerr != nil {
		return VestingPosition{}, zerr
	}
	pos.VestedQuantity, pos.UnvestedQuantity, pos.ForfeitedQuantity = zero, zero, zero
	for _, t := range tranches {
		l := VestingLot{Sequence: t.Sequence, VestsOn: t.VestsOn, Quantity: t.Quantity}
		if t.VestsOn.Compare(asOf) <= 0 {
			l.Vested = true
		}
		pos.Lots = append(pos.Lots, l)
	}
	seenLots := make(map[int]struct{}, len(events))
	for _, e := range events {
		if err := e.Validate(); err != nil {
			return VestingPosition{}, err
		}
		if e.GrantDigest != grant.CanonicalDigest || e.GrantRevision != grant.Revision {
			return VestingPosition{}, fmt.Errorf("%w: event is not bound to grant", ErrInvalidLotEvent)
		}
		if e.Sequence > len(pos.Lots) {
			return VestingPosition{}, fmt.Errorf("%w: sequence %d is absent", ErrInvalidLotEvent, e.Sequence)
		}
		l := &pos.Lots[e.Sequence-1]
		if e.Quantity.Scale() != l.Quantity.Scale() || !e.Quantity.Equal(l.Quantity) {
			return VestingPosition{}, fmt.Errorf("%w: event quantity must equal the scheduled lot exactly", ErrInvalidLotEvent)
		}
		if _, duplicate := seenLots[e.Sequence]; duplicate {
			return VestingPosition{}, fmt.Errorf("%w: duplicate event for sequence %d", ErrInvalidLotEvent, e.Sequence)
		}
		seenLots[e.Sequence] = struct{}{}
		if e.EffectiveDate.Compare(asOf) > 0 {
			continue
		}
		if e.Kind == LotForfeited {
			if e.EffectiveDate.Compare(l.VestsOn) >= 0 {
				return VestingPosition{}, ErrForfeitureVested
			}
			l.Vested = false
			l.Forfeited = true
			l.EventDigest = e.computedDigest()
		} else {
			l.Vested = true
			l.EventDigest = e.computedDigest()
		}
	}
	for _, l := range pos.Lots {
		var addErr error
		switch {
		case l.Forfeited:
			pos.ForfeitedQuantity, addErr = addQuantity(pos.ForfeitedQuantity, l.Quantity)
		case l.Vested:
			pos.VestedQuantity, addErr = addQuantity(pos.VestedQuantity, l.Quantity)
		default:
			pos.UnvestedQuantity, addErr = addQuantity(pos.UnvestedQuantity, l.Quantity)
		}
		if addErr != nil {
			return VestingPosition{}, addErr
		}
	}
	return pos, nil
}

type ForfeitureResult struct {
	Position VestingPosition
	Events   []VestingLotEvent
}

func ForfeitUnvestedLots(grant EquityGrant, asOf values.LocalDate, evidence string) (ForfeitureResult, error) {
	if strings.TrimSpace(evidence) == "" {
		return ForfeitureResult{}, fmt.Errorf("%w: evidence_ref is required", ErrInvalidLotEvent)
	}
	p, err := CalculateVestingPosition(grant, asOf, nil)
	if err != nil {
		return ForfeitureResult{}, err
	}
	out := ForfeitureResult{Position: p}
	for _, l := range p.Lots {
		if !l.Vested {
			e, er := NewVestingLotEvent(VestingLotEvent{GrantDigest: grant.CanonicalDigest, GrantRevision: grant.Revision, Sequence: l.Sequence, Kind: LotForfeited, Quantity: l.Quantity, EffectiveDate: asOf, EvidenceRef: evidence})
			if er != nil {
				return ForfeitureResult{}, er
			}
			out.Events = append(out.Events, e)
		}
	}
	if len(out.Events) > 0 {
		out.Position, err = CalculateVestingPosition(grant, asOf, out.Events)
		if err != nil {
			return ForfeitureResult{}, err
		}
	}
	return out, nil
}

// CorrectGrant creates a successor and preserves the original revision.
func CorrectGrant(original, replacement EquityGrant, evidence string) (EquityGrant, error) {
	if err := original.Validate(); err != nil {
		return EquityGrant{}, err
	}
	if strings.TrimSpace(evidence) == "" {
		return EquityGrant{}, fmt.Errorf("%w: evidence is required", ErrCorrectionMismatch)
	}
	replacement.GrantID = original.GrantID
	replacement.PlanDigest = original.PlanDigest
	replacement.PlanRevision = original.PlanRevision
	replacement.PoolRef = original.PoolRef
	replacement.WorkerRef = original.WorkerRef
	replacement.ParentDigest = original.computedDigest()
	replacement.SupersedesRevision = original.Revision
	replacement.Revision = original.Revision + 1
	replacement.EvidenceRef = evidence
	replacement.CanonicalDigest, replacement.Digest = "", ""
	if replacement.Quantity.Equal(original.Quantity) && replacement.StrikePrice.Equal(original.StrikePrice) && replacement.Vesting.Canonical() != nil && string(replacement.Vesting.Canonical()) == string(original.Vesting.Canonical()) {
		return EquityGrant{}, fmt.Errorf("%w: successor does not differ", ErrCorrectionMismatch)
	}
	return NewEquityGrant(replacement)
}

// TaxPayrollEffect carries caller-supplied amounts. No statutory rate or tax treatment is inferred here.
type TaxPayrollEffect struct {
	GrantDigest     string
	GrantRevision   uint64
	WorkerRef       string
	TaxAmount       values.Decimal
	PayrollAmount   values.Decimal
	Currency        string
	EffectiveDate   values.LocalDate
	SourceRef       string
	CanonicalDigest string
	Digest          string
}

func (e TaxPayrollEffect) Validate() error {
	if strings.TrimSpace(e.GrantDigest) == "" || e.GrantRevision == 0 || strings.TrimSpace(e.WorkerRef) == "" || strings.TrimSpace(e.Currency) == "" || strings.TrimSpace(e.SourceRef) == "" {
		return fmt.Errorf("%w: binding, currency and source are required", ErrInvalidEffect)
	}
	for n, v := range map[string]values.Decimal{"tax_amount": e.TaxAmount, "payroll_amount": e.PayrollAmount} {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidEffect, n, err)
		}
		if v.Sign() < 0 {
			return fmt.Errorf("%w: %s must not be negative", ErrInvalidEffect, n)
		}
	}
	if e.TaxAmount.Scale() != e.PayrollAmount.Scale() {
		return fmt.Errorf("%w: tax and payroll amounts require one currency scale", ErrInvalidEffect)
	}
	if err := e.EffectiveDate.Validate(); err != nil {
		return fmt.Errorf("%w: effective_date: %v", ErrInvalidEffect, err)
	}
	if e.CanonicalDigest != "" && e.CanonicalDigest != e.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidEffect)
	}
	if e.Digest != "" && e.Digest != e.computedDigest() {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidEffect)
	}
	return nil
}
func (e TaxPayrollEffect) canonical() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.equity.TaxPayrollEffect", schemaVersion).String("grant_digest", e.GrantDigest).Int("grant_revision", int64(e.GrantRevision)).String("worker_ref", e.WorkerRef).Value("tax_amount", e.TaxAmount).Value("payroll_amount", e.PayrollAmount).String("currency", e.Currency).Value("effective_date", e.EffectiveDate).String("source_ref", e.SourceRef).Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (e TaxPayrollEffect) computedDigest() string { return canonicalbytes.Digest(e.canonical()) }
func (e TaxPayrollEffect) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	return e.canonical()
}
func NewTaxPayrollEffect(e TaxPayrollEffect) (TaxPayrollEffect, error) {
	e.CanonicalDigest, e.Digest = "", ""
	if err := e.Validate(); err != nil {
		return TaxPayrollEffect{}, err
	}
	e.CanonicalDigest = e.computedDigest()
	e.Digest = e.CanonicalDigest
	return e, nil
}

type ProviderReconciliationStatus string

const (
	ProviderReconciled     ProviderReconciliationStatus = "RECONCILED"
	ProviderUnknown        ProviderReconciliationStatus = "UNKNOWN"
	ProviderRepairRequired ProviderReconciliationStatus = "REPAIR_REQUIRED"
)

type ProviderObservation struct {
	Provider          string
	ExternalRef       string
	GrantDigest       string
	GrantRevision     uint64
	Quantity          values.Decimal
	TaxAmount         values.Decimal
	PayrollAmount     values.Decimal
	Currency          string
	ObservedAt        values.Instant
	ObservationDigest string
}

func (o ProviderObservation) canonical() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.equity.ProviderObservation", schemaVersion).
		String("provider", o.Provider).String("external_ref", o.ExternalRef).String("grant_digest", o.GrantDigest).
		Int("grant_revision", int64(o.GrantRevision)).Value("quantity", o.Quantity).Value("tax_amount", o.TaxAmount).
		Value("payroll_amount", o.PayrollAmount).String("currency", o.Currency).Value("observed_at", o.ObservedAt).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (o ProviderObservation) computedDigest() string { return canonicalbytes.Digest(o.canonical()) }

func (o ProviderObservation) Validate() error {
	if strings.TrimSpace(o.Provider) == "" || strings.TrimSpace(o.ExternalRef) == "" {
		return fmt.Errorf("%w: provider and external_ref are required", ErrInvalidObservation)
	}
	if o.GrantRevision == 0 || strings.TrimSpace(o.GrantDigest) == "" {
		return fmt.Errorf("%w: grant binding is required", ErrInvalidObservation)
	}
	if strings.TrimSpace(o.Currency) == "" {
		return fmt.Errorf("%w: currency is required", ErrInvalidObservation)
	}
	for name, value := range map[string]values.Decimal{"quantity": o.Quantity, "tax_amount": o.TaxAmount, "payroll_amount": o.PayrollAmount} {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidObservation, name, err)
		}
		if value.Sign() < 0 {
			return fmt.Errorf("%w: %s must not be negative", ErrInvalidObservation, name)
		}
	}
	if o.TaxAmount.Scale() != o.PayrollAmount.Scale() {
		return fmt.Errorf("%w: tax and payroll amounts require one currency scale", ErrInvalidObservation)
	}
	if err := o.ObservedAt.Validate(); err != nil {
		return fmt.Errorf("%w: observed_at: %v", ErrInvalidObservation, err)
	}
	if o.ObservationDigest != "" && o.ObservationDigest != o.computedDigest() {
		return fmt.Errorf("%w: observation_digest mismatch", ErrInvalidObservation)
	}
	return nil
}

func NewProviderObservation(o ProviderObservation) (ProviderObservation, error) {
	o.ObservationDigest = ""
	if err := o.Validate(); err != nil {
		return ProviderObservation{}, err
	}
	// The observation digest is an evidence identifier, not an authority claim.
	o.ObservationDigest = o.computedDigest()
	return o, nil
}

func UnknownProviderEffect(reason string) ProviderReconciliation {
	return ProviderReconciliation{Status: ProviderUnknown, Reason: strings.TrimSpace(reason)}
}

type ProviderReconciliation struct {
	Status         ProviderReconciliationStatus
	Observation    ProviderObservation
	RepairRequired bool
	Reason         string
}

func ReconcileProviderEffect(expected TaxPayrollEffect, observed ProviderObservation) (ProviderReconciliation, error) {
	if err := expected.Validate(); err != nil {
		return ProviderReconciliation{}, err
	}
	if err := observed.Validate(); err != nil {
		return ProviderReconciliation{}, err
	}
	if observed.GrantDigest != expected.GrantDigest || observed.GrantRevision != expected.GrantRevision {
		return ProviderReconciliation{Status: ProviderRepairRequired, RepairRequired: true, Observation: observed, Reason: "grant binding mismatch"}, nil
	}
	if observed.Currency != expected.Currency || observed.TaxAmount.Cmp(expected.TaxAmount) != 0 || observed.PayrollAmount.Cmp(expected.PayrollAmount) != 0 {
		return ProviderReconciliation{Status: ProviderRepairRequired, RepairRequired: true, Observation: observed, Reason: "amount or currency mismatch"}, nil
	}
	return ProviderReconciliation{Status: ProviderReconciled, Observation: observed}, nil
}
