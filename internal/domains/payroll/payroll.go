// Package payroll owns the conformance vocabulary for a payroll run.
//
// PayrollRun is an immutable revision value. Lifecycle operations return a new
// revision and never mutate the receiver or imply persistence, events, human
// work, or provider requests.
package payroll

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

const payrollRunSchema = "hcmnext.domains.payroll.PayrollRun"

// PayrollRunState is the closed lifecycle vocabulary for one run revision.
type PayrollRunState string

const (
	PayrollRunStateDraft      PayrollRunState = "DRAFT"
	PayrollRunStateCalculated PayrollRunState = "CALCULATED"
	PayrollRunStateReleased   PayrollRunState = "RELEASED"
	PayrollRunStateSettled    PayrollRunState = "SETTLED"
	PayrollRunStateReversed   PayrollRunState = "REVERSED"

	// Short names keep callers concise while retaining the explicit type.
	Draft      = PayrollRunStateDraft
	Calculated = PayrollRunStateCalculated
	Released   = PayrollRunStateReleased
	Settled    = PayrollRunStateSettled
	Reversed   = PayrollRunStateReversed
)

// Valid reports whether s is a declared payroll-run state.
func (s PayrollRunState) Valid() bool {
	switch s {
	case PayrollRunStateDraft, PayrollRunStateCalculated, PayrollRunStateReleased,
		PayrollRunStateSettled, PayrollRunStateReversed:
		return true
	default:
		return false
	}
}

// String returns the stable wire token.
func (s PayrollRunState) String() string { return string(s) }

// PeriodRef binds a run to one published pay period revision and its digest.
type PeriodRef struct {
	ID      string
	Version string
	Digest  string
}

func (r PeriodRef) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("payroll: period id is required")
	}
	if strings.TrimSpace(r.Version) == "" {
		return fmt.Errorf("payroll: period version is required")
	}
	if strings.TrimSpace(r.Digest) == "" {
		return fmt.Errorf("payroll: period digest is required")
	}
	return nil
}

func (r PeriodRef) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.payroll.PeriodRef", 1).
		String("id", r.ID).String("version", r.Version).String("digest", r.Digest)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// PopulationBindingRef binds a run to an exact frozen population snapshot.
type PopulationBindingRef struct {
	DefinitionID    string
	RevisionVersion string
	Digest          string
}

func (r PopulationBindingRef) Validate() error {
	if strings.TrimSpace(r.DefinitionID) == "" {
		return fmt.Errorf("payroll: population definition id is required")
	}
	if strings.TrimSpace(r.RevisionVersion) == "" {
		return fmt.Errorf("payroll: population revision version is required")
	}
	if strings.TrimSpace(r.Digest) == "" {
		return fmt.Errorf("payroll: population digest is required")
	}
	return nil
}

func (r PopulationBindingRef) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.payroll.PopulationBindingRef", 1).
		String("definition_id", r.DefinitionID).
		String("revision_version", r.RevisionVersion).
		String("digest", r.Digest)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

var (
	// ErrInvalidPayrollRun identifies an incomplete or incoherent revision.
	ErrInvalidPayrollRun = errors.New("payroll: invalid payroll run")
	// ErrTransitionRejected is the typed PAYRUN-001 refusal boundary.
	ErrTransitionRejected = errors.New("PAYRUN_001_REJECTED")
	// ErrSettlementWithoutRelease distinguishes settlement attempted before release.
	ErrSettlementWithoutRelease = errors.New("payroll: settlement requires a release")
)

// TransitionError reports the offending state, target and revision without
// creating an authoritative side effect.
type TransitionError struct {
	Code     string
	Field    string
	From     PayrollRunState
	To       PayrollRunState
	Revision uint64
	Reason   string
	Cause    error
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("%s: field=%s from=%s to=%s revision=%d: %s", e.Code, e.Field, e.From, e.To, e.Revision, e.Reason)
}

func (e *TransitionError) Is(target error) bool {
	return target == ErrTransitionRejected || target == e.Cause
}

func transitionError(run PayrollRun, to PayrollRunState, field, reason string, cause error) error {
	return &TransitionError{Code: ErrTransitionRejected.Error(), Field: field, From: run.State, To: to, Revision: run.Revision, Reason: reason, Cause: cause}
}

// PayrollRun is one immutable revision of a run's conformance record.
type PayrollRun struct {
	RunID                  string
	Revision               uint64
	State                  PayrollRunState
	PayGroupRef            string
	Period                 PeriodRef
	Population             PopulationBindingRef
	CalculationInputDigest string
	CalculationDigest      string
	ReleaseDigest          string
	ReversalDigest         string
	SupersedesRevision     uint64
	CanonicalDigest        string
}

// NewPayrollRun creates revision one in DRAFT. It only constructs a value.
func NewPayrollRun(runID, payGroupRef string, period PeriodRef, population PopulationBindingRef, calculationInputDigest string) (PayrollRun, error) {
	run := PayrollRun{
		RunID: runID, Revision: 1, State: PayrollRunStateDraft, PayGroupRef: payGroupRef,
		Period: period, Population: population, CalculationInputDigest: calculationInputDigest,
	}
	run.CanonicalDigest = run.computedDigest()
	if err := run.Validate(); err != nil {
		return PayrollRun{}, err
	}
	return run, nil
}

// NewDraft is a descriptive alias for NewPayrollRun.
func NewDraft(runID, payGroupRef string, period PeriodRef, population PopulationBindingRef, calculationInputDigest string) (PayrollRun, error) {
	return NewPayrollRun(runID, payGroupRef, period, population, calculationInputDigest)
}

// Validate reports whether the revision is complete and internally coherent.
func (r PayrollRun) Validate() error {
	if strings.TrimSpace(r.RunID) == "" {
		return fmt.Errorf("%w: run_id is required", ErrInvalidPayrollRun)
	}
	if r.Revision == 0 {
		return fmt.Errorf("%w: revision is required", ErrInvalidPayrollRun)
	}
	if !r.State.Valid() {
		return fmt.Errorf("%w: state %q is not declared", ErrInvalidPayrollRun, r.State)
	}
	if err := r.Period.Validate(); err != nil {
		return fmt.Errorf("%w: period: %v", ErrInvalidPayrollRun, err)
	}
	if err := r.Population.Validate(); err != nil {
		return fmt.Errorf("%w: population: %v", ErrInvalidPayrollRun, err)
	}
	if strings.TrimSpace(r.CalculationInputDigest) == "" {
		return fmt.Errorf("%w: calculation input digest is required", ErrInvalidPayrollRun)
	}
	if r.SupersedesRevision >= r.Revision {
		return fmt.Errorf("%w: supersedes revision must precede revision", ErrInvalidPayrollRun)
	}
	if r.State == PayrollRunStateReleased || r.State == PayrollRunStateSettled || r.State == PayrollRunStateReversed {
		if strings.TrimSpace(r.ReleaseDigest) == "" {
			return fmt.Errorf("%w: release digest is required for %s", ErrInvalidPayrollRun, r.State)
		}
	}
	if r.State == PayrollRunStateReversed && strings.TrimSpace(r.ReversalDigest) == "" {
		return fmt.Errorf("%w: reversal digest is required", ErrInvalidPayrollRun)
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidPayrollRun)
	}
	return nil
}

func (r PayrollRun) body() []byte {
	w := canonicalbytes.New(payrollRunSchema, 1).
		String("run_id", r.RunID).
		Int("revision", int64(r.Revision)).
		String("state", r.State.String()).
		String("pay_group_ref", r.PayGroupRef).
		Value("period", r.Period).
		Value("population", r.Population).
		String("calculation_input_digest", r.CalculationInputDigest).
		String("calculation_digest", r.CalculationDigest).
		String("release_digest", r.ReleaseDigest).
		String("reversal_digest", r.ReversalDigest).
		Int("supersedes_revision", int64(r.SupersedesRevision))
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (r PayrollRun) computedDigest() string {
	raw := r.body()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Canonical returns the canonical revision encoding, or nil when invalid.
func (r PayrollRun) Canonical() []byte {
	if err := r.Validate(); err != nil {
		return nil
	}
	return r.body()
}

// Digest returns the canonical digest of the revision.
func (r PayrollRun) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

func (r PayrollRun) transition(to PayrollRunState, evidence string) (PayrollRun, error) {
	if err := r.Validate(); err != nil {
		return PayrollRun{}, err
	}
	if !to.Valid() {
		return PayrollRun{}, transitionError(r, to, "state", "target state is not declared", nil)
	}
	allowed := (r.State == PayrollRunStateDraft && to == PayrollRunStateCalculated) ||
		(r.State == PayrollRunStateCalculated && to == PayrollRunStateReleased) ||
		(r.State == PayrollRunStateReleased && to == PayrollRunStateSettled) ||
		(r.State == PayrollRunStateSettled && to == PayrollRunStateReversed)
	if !allowed {
		cause := error(nil)
		if to == PayrollRunStateSettled {
			cause = ErrSettlementWithoutRelease
		}
		return PayrollRun{}, transitionError(r, to, "state", "transition is out of order", cause)
	}
	next := r
	next.Revision++
	next.SupersedesRevision = r.Revision
	next.State = to
	switch to {
	case PayrollRunStateCalculated:
		if evidence != "" {
			next.CalculationDigest = evidence
		}
	case PayrollRunStateReleased:
		if evidence != "" {
			next.ReleaseDigest = evidence
		} else {
			next.ReleaseDigest = r.computedDigest()
		}
	case PayrollRunStateReversed:
		if evidence != "" {
			next.ReversalDigest = evidence
		} else {
			next.ReversalDigest = r.computedDigest()
		}
	}
	next.CanonicalDigest = next.computedDigest()
	if err := next.Validate(); err != nil {
		return PayrollRun{}, err
	}
	return next, nil
}

// Transition returns the next lifecycle revision. Evidence is optional and is
// used as the calculation, release, or reversal digest when supplied.
func (r PayrollRun) Transition(to PayrollRunState, evidence ...string) (PayrollRun, error) {
	var detail string
	if len(evidence) > 0 {
		detail = evidence[0]
	}
	return r.transition(to, detail)
}

// TransitionTo is the explicit spelling of Transition.
func (r PayrollRun) TransitionTo(to PayrollRunState, evidence ...string) (PayrollRun, error) {
	return r.Transition(to, evidence...)
}

func (r PayrollRun) Calculate(evidence ...string) (PayrollRun, error) {
	return r.Transition(PayrollRunStateCalculated, evidence...)
}

func (r PayrollRun) Release(evidence ...string) (PayrollRun, error) {
	return r.Transition(PayrollRunStateReleased, evidence...)
}

func (r PayrollRun) Settle() (PayrollRun, error) { return r.Transition(PayrollRunStateSettled) }

func (r PayrollRun) Reverse(evidence ...string) (PayrollRun, error) {
	return r.Transition(PayrollRunStateReversed, evidence...)
}

// PayrollRunExplanation is the read-only explanation of a revision's position
// in the lifecycle. It contains no authority decision or side effect.
type PayrollRunExplanation struct {
	RunID              string
	Revision           uint64
	State              PayrollRunState
	SupersedesRevision uint64
	Digest             string
	AllowedNext        []PayrollRunState
}

// Explain returns the lifecycle facts and legal next states for a run.
func (r PayrollRun) Explain() (PayrollRunExplanation, error) {
	if err := r.Validate(); err != nil {
		return PayrollRunExplanation{}, err
	}
	allowed := map[PayrollRunState][]PayrollRunState{
		PayrollRunStateDraft:      {PayrollRunStateCalculated},
		PayrollRunStateCalculated: {PayrollRunStateReleased},
		PayrollRunStateReleased:   {PayrollRunStateSettled},
		PayrollRunStateSettled:    {PayrollRunStateReversed},
		PayrollRunStateReversed:   nil,
	}
	next := append([]PayrollRunState(nil), allowed[r.State]...)
	return PayrollRunExplanation{RunID: r.RunID, Revision: r.Revision, State: r.State, SupersedesRevision: r.SupersedesRevision, Digest: r.computedDigest(), AllowedNext: next}, nil
}

// Explain is the package-level spelling for callers that do not need a method.
func Explain(r PayrollRun) (PayrollRunExplanation, error) { return r.Explain() }
