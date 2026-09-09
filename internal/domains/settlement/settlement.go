// Package settlement owns payment-instruction identity and the pure,
// append-only settlement lifecycle. It never accepts raw bank details and has
// no database or provider side effects.
package settlement

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's stable contract version.
func Version() int { return schemaVersion }

// SettlementRail is a closed vocabulary. Provider-specific details remain
// outside this domain package.
type SettlementRail string

const (
	RailACH      SettlementRail = "ACH"
	RailWire     SettlementRail = "WIRE"
	RailSEPA     SettlementRail = "SEPA"
	RailRTP      SettlementRail = "RTP"
	RailFedNow   SettlementRail = "FEDNOW"
	RailInternal SettlementRail = "INTERNAL"

	ACH      = RailACH
	Wire     = RailWire
	SEPA     = RailSEPA
	RTP      = RailRTP
	FedNow   = RailFedNow
	Internal = RailInternal
)

func (r SettlementRail) Valid() bool {
	switch r {
	case RailACH, RailWire, RailSEPA, RailRTP, RailFedNow, RailInternal:
		return true
	default:
		return false
	}
}

// SettlementState is the immutable instruction lifecycle. Provider
// acceptance is ACKNOWLEDGED; it is not evidence of SETTLED funds.
type SettlementState string

const (
	StateInstructed   SettlementState = "INSTRUCTED"
	StateSubmitted    SettlementState = "SUBMITTED"
	StateAcknowledged SettlementState = "ACKNOWLEDGED"
	StateSettled      SettlementState = "SETTLED"
	StateReturned     SettlementState = "RETURNED"
	StateReversed     SettlementState = "REVERSED"

	Instructed   = StateInstructed
	Submitted    = StateSubmitted
	Acknowledged = StateAcknowledged
	Settled      = StateSettled
	Returned     = StateReturned
	Reversed     = StateReversed
)

func (s SettlementState) Valid() bool {
	switch s {
	case StateInstructed, StateSubmitted, StateAcknowledged, StateSettled, StateReturned, StateReversed:
		return true
	default:
		return false
	}
}

var (
	ErrInvalidPaymentInstruction = errors.New("settlement: invalid payment instruction")
	ErrSettlementRejected        = errors.New("SETTLE_001_REJECTED")
	ErrNoReleasedPayrollRun      = errors.New("settlement: payment requires a released payroll run")
	ErrSettlementTransition      = errors.New("settlement: lifecycle transition is not allowed")
	ErrIdempotencyConflict       = errors.New("settlement: natural-key idempotency conflict")
)

// PaymentInstructionSpec contains the governed input to an instruction. A
// bank detail is represented only by BankDetailRef; RawBankDetail is a
// deliberate rejection probe and is never retained in a valid instruction.
type PaymentInstructionSpec struct {
	InstructionID    string
	PayeeRef         string
	Amount           values.Decimal
	Currency         string
	FundingSourceRef string
	Rail             SettlementRail
	BankDetailRef    string
	ScheduleRef      string
	ValueDate        values.LocalDate
	RawBankDetail    string
}

// PaymentInstruction is one immutable revision of a payment instruction.
// Every lifecycle operation returns another revision and leaves the receiver
// untouched.
type PaymentInstruction struct {
	InstructionID    string
	PayrollRunRef    string
	PayeeRef         string
	Amount           values.Decimal
	Currency         string
	FundingSourceRef string
	Rail             SettlementRail
	BankDetailRef    string
	ScheduleRef      string
	ValueDate        values.LocalDate

	State              SettlementState
	Revision           uint64
	SupersedesRevision uint64
	EvidenceRef        string
	CanonicalDigest    string
}

// NewPaymentInstruction creates revision one only when run is released (or a
// later immutable run state that still carries release evidence).
func NewPaymentInstruction(run payroll.PayrollRun, spec PaymentInstructionSpec) (PaymentInstruction, error) {
	if err := run.Validate(); err != nil {
		return PaymentInstruction{}, err
	}
	if run.State != payroll.PayrollRunStateReleased && run.State != payroll.PayrollRunStateSettled && run.State != payroll.PayrollRunStateReversed {
		return PaymentInstruction{}, fmt.Errorf("%w: run state %s", ErrNoReleasedPayrollRun, run.State)
	}
	if strings.TrimSpace(run.CanonicalDigest) == "" {
		return PaymentInstruction{}, fmt.Errorf("%w: released run digest is required", ErrNoReleasedPayrollRun)
	}
	if err := spec.validate(); err != nil {
		return PaymentInstruction{}, err
	}
	i := PaymentInstruction{
		InstructionID: spec.InstructionID, PayrollRunRef: run.CanonicalDigest,
		PayeeRef: spec.PayeeRef, Amount: spec.Amount, Currency: spec.Currency,
		FundingSourceRef: spec.FundingSourceRef, Rail: spec.Rail, BankDetailRef: spec.BankDetailRef,
		ScheduleRef: spec.ScheduleRef, ValueDate: spec.ValueDate, State: StateInstructed, Revision: 1,
	}
	i.CanonicalDigest = i.computedDigest()
	return i, nil
}

// NewInstruction is a concise alias for NewPaymentInstruction.
func NewInstruction(run payroll.PayrollRun, spec PaymentInstructionSpec) (PaymentInstruction, error) {
	return NewPaymentInstruction(run, spec)
}

func (s PaymentInstructionSpec) validate() error {
	if strings.TrimSpace(s.InstructionID) == "" || strings.TrimSpace(s.PayeeRef) == "" {
		return fmt.Errorf("%w: instruction and payee references are required", ErrInvalidPaymentInstruction)
	}
	if err := s.Amount.Validate(); err != nil {
		return fmt.Errorf("%w: amount: %v", ErrInvalidPaymentInstruction, err)
	}
	if s.Amount.Sign() <= 0 {
		return fmt.Errorf("%w: amount must be positive", ErrInvalidPaymentInstruction)
	}
	if strings.TrimSpace(s.Currency) == "" {
		return fmt.Errorf("%w: currency is required", ErrInvalidPaymentInstruction)
	}
	if strings.TrimSpace(s.FundingSourceRef) == "" {
		return fmt.Errorf("%w: funding source reference is required", ErrInvalidPaymentInstruction)
	}
	if !s.Rail.Valid() {
		return fmt.Errorf("%w: settlement rail %q is not declared", ErrInvalidPaymentInstruction, s.Rail)
	}
	if strings.TrimSpace(s.BankDetailRef) == "" {
		return fmt.Errorf("%w: governed bank detail reference is required", ErrInvalidPaymentInstruction)
	}
	if s.RawBankDetail != "" {
		return fmt.Errorf("%w: raw bank detail is prohibited", ErrInvalidPaymentInstruction)
	}
	if s.ValueDate.IsSet() {
		if err := s.ValueDate.Validate(); err != nil {
			return fmt.Errorf("%w: value date: %v", ErrInvalidPaymentInstruction, err)
		}
	}
	return nil
}

func (i PaymentInstruction) Validate() error {
	if strings.TrimSpace(i.InstructionID) == "" || strings.TrimSpace(i.PayrollRunRef) == "" || strings.TrimSpace(i.PayeeRef) == "" {
		return fmt.Errorf("%w: instruction, released run and payee references are required", ErrInvalidPaymentInstruction)
	}
	if err := (PaymentInstructionSpec{InstructionID: i.InstructionID, PayeeRef: i.PayeeRef, Amount: i.Amount, Currency: i.Currency, FundingSourceRef: i.FundingSourceRef, Rail: i.Rail, BankDetailRef: i.BankDetailRef, ScheduleRef: i.ScheduleRef, ValueDate: i.ValueDate}).validate(); err != nil {
		return err
	}
	if !i.State.Valid() || i.Revision == 0 {
		return fmt.Errorf("%w: state and revision are required", ErrInvalidPaymentInstruction)
	}
	if i.SupersedesRevision >= i.Revision && i.SupersedesRevision != 0 {
		return fmt.Errorf("%w: successor revision must supersede an earlier revision", ErrInvalidPaymentInstruction)
	}
	if i.State != StateInstructed && strings.TrimSpace(i.EvidenceRef) == "" {
		return fmt.Errorf("%w: lifecycle evidence is required after instruction", ErrInvalidPaymentInstruction)
	}
	if i.CanonicalDigest == "" || i.CanonicalDigest != i.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidPaymentInstruction)
	}
	return nil
}

func (i PaymentInstruction) naturalKeyBytes() []byte {
	w := canonicalbytes.New("hcmnext.domains.settlement.PaymentInstructionNaturalKey", schemaVersion).
		String("payroll_run_ref", i.PayrollRunRef).String("payee_ref", i.PayeeRef).
		Value("amount", i.Amount).String("currency", i.Currency).String("funding_source_ref", i.FundingSourceRef).
		String("rail", string(i.Rail)).String("bank_detail_ref", i.BankDetailRef).String("schedule_ref", i.ScheduleRef)
	if i.ValueDate.IsSet() {
		w.Value("value_date", i.ValueDate)
	} else {
		w.Bool("value_date_present", false)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// NaturalKey is the stable idempotency identity, independent of instruction
// ID, lifecycle state, provider evidence and revision.
func (i PaymentInstruction) NaturalKey() string { return canonicalbytes.Digest(i.naturalKeyBytes()) }

// IdempotencyKey is an explicit alias for NaturalKey.
func (i PaymentInstruction) IdempotencyKey() string { return i.NaturalKey() }

func (i PaymentInstruction) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.settlement.PaymentInstruction", schemaVersion).
		String("instruction_id", i.InstructionID).String("payroll_run_ref", i.PayrollRunRef).
		String("payee_ref", i.PayeeRef).Value("amount", i.Amount).String("currency", i.Currency).
		String("funding_source_ref", i.FundingSourceRef).String("rail", string(i.Rail)).
		String("bank_detail_ref", i.BankDetailRef).String("schedule_ref", i.ScheduleRef).
		Bool("value_date_present", i.ValueDate.IsSet()).
		String("state", string(i.State)).Int("revision", int64(i.Revision)).
		Int("supersedes_revision", int64(i.SupersedesRevision)).String("evidence_ref", i.EvidenceRef)
	if i.ValueDate.IsSet() {
		w.Value("value_date", i.ValueDate)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (i PaymentInstruction) computedDigest() string { return canonicalbytes.Digest(i.body()) }

func (i PaymentInstruction) allowed(to SettlementState) bool {
	switch i.State {
	case StateInstructed:
		return to == StateSubmitted
	case StateSubmitted:
		return to == StateAcknowledged || to == StateReturned
	case StateAcknowledged:
		return to == StateSettled || to == StateReturned
	case StateSettled:
		return to == StateReturned || to == StateReversed
	case StateReturned:
		return to == StateReversed
	default:
		return false
	}
}

// Transition appends one lifecycle revision and requires evidence for every
// post-instruction state. The original instruction is never modified.
func (i PaymentInstruction) Transition(to SettlementState, evidence string) (PaymentInstruction, error) {
	if err := i.Validate(); err != nil {
		return PaymentInstruction{}, err
	}
	if !to.Valid() || !i.allowed(to) {
		return PaymentInstruction{}, fmt.Errorf("%w: %s -> %s", ErrSettlementTransition, i.State, to)
	}
	if strings.TrimSpace(evidence) == "" {
		return PaymentInstruction{}, fmt.Errorf("%w: evidence is required", ErrSettlementTransition)
	}
	next := i
	next.State, next.Revision, next.SupersedesRevision, next.EvidenceRef = to, i.Revision+1, i.Revision, evidence
	next.CanonicalDigest = next.computedDigest()
	return next, nil
}

func (i PaymentInstruction) Submit(evidence string) (PaymentInstruction, error) {
	return i.Transition(StateSubmitted, evidence)
}
func (i PaymentInstruction) Acknowledge(evidence string) (PaymentInstruction, error) {
	return i.Transition(StateAcknowledged, evidence)
}
func (i PaymentInstruction) Settle(evidence string) (PaymentInstruction, error) {
	return i.Transition(StateSettled, evidence)
}
func (i PaymentInstruction) Return(evidence string) (PaymentInstruction, error) {
	return i.Transition(StateReturned, evidence)
}
func (i PaymentInstruction) Reverse(evidence string) (PaymentInstruction, error) {
	return i.Transition(StateReversed, evidence)
}

// EnsureIdempotent returns the existing instruction for the same natural key;
// a different natural key is a conflict and must not be silently replaced.
func EnsureIdempotent(existing, candidate PaymentInstruction) (PaymentInstruction, error) {
	if err := existing.Validate(); err != nil {
		return PaymentInstruction{}, err
	}
	if err := candidate.Validate(); err != nil {
		return PaymentInstruction{}, err
	}
	if existing.NaturalKey() != candidate.NaturalKey() {
		return PaymentInstruction{}, ErrIdempotencyConflict
	}
	return existing, nil
}

// Explain renders an audit-safe lifecycle summary.
func (i PaymentInstruction) Explain() string {
	return fmt.Sprintf("payment instruction %s for released run %s: %s revision %d via %s, amount %s %s, natural key %s, digest %s", i.InstructionID, i.PayrollRunRef, i.State, i.Revision, i.Rail, i.Amount.String(), i.Currency, i.NaturalKey(), i.CanonicalDigest)
}

// Explain is the package-level contract spelling.
func Explain(i PaymentInstruction) string { return i.Explain() }
