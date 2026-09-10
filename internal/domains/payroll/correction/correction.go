// Package correction proves payroll correction and retroactive repair
// semantics (CONF-006) with bitemporal fixtures and exact decimals.
//
// The RED contract: the current rule version must never rewrite history.
// A run is reproduced only under its pinned rule version; a correction
// computes the exact delta, is simulated with zero effect, approved, and
// appended as reconciled pay/accounting/tax/filing evidence. The original
// run is immutable: corrections supersede, never overwrite.
//
// The REFACTOR contract: no native payroll engine is required; this
// package is a deterministic test adapter over exact decimal arithmetic.
package correction

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Sentinel causes. Classify with errors.Is.
var (
	// ErrUnknownRuleVersion reports a run pinned to a rule version the
	// registry does not know.
	ErrUnknownRuleVersion = errors.New("correction: unknown rule version")

	// ErrHistoryMismatch reports a reproduction that does not exactly
	// match the stored original: something rewrote history.
	ErrHistoryMismatch = errors.New("correction: reproduction does not match original")

	// ErrUnbalancedDelta reports a correction delta whose gross/net/tax
	// or accounting legs fail to balance.
	ErrUnbalancedDelta = errors.New("correction: delta does not balance")

	// ErrOriginalTampered reports a correction naming an original whose
	// digest no longer matches the stored run: the original was
	// overwritten instead of superseded.
	ErrOriginalTampered = errors.New("correction: original run was tampered")

	// ErrAlreadyAppended reports a second append of the same correction.
	ErrAlreadyAppended = errors.New("correction: correction already appended")

	// ErrCorrectionState reports a lifecycle step taken out of order.
	ErrCorrectionState = errors.New("correction: correction in wrong state")
)

// RuleVersion is one pinned calculation rule set. The registry keeps every
// released version so historical runs reproduce exactly.
type RuleVersion struct {
	ID                 string
	OvertimeMultiplier values.Decimal
	TaxRate            values.Decimal
}

// Inputs are the worked facts behind one pay run.
type Inputs struct {
	EmployeeID    string
	RegularHours  values.Decimal
	OvertimeHours values.Decimal
	HourlyRate    values.Decimal
}

// PayRun is one immutable bitemporal pay run: effective date fixes the pay
// period, known time fixes when the system learned it, and the rule version
// fixes the calculation that produced it.
type PayRun struct {
	RunID         string
	EmployeeID    string
	EffectiveDate string
	KnownAt       time.Time
	RuleVersionID string
	RegularHours  values.Decimal
	OvertimeHours values.Decimal
	HourlyRate    values.Decimal
	RegularPay    values.Decimal
	OvertimePay   values.Decimal
	Gross         values.Decimal
	Tax           values.Decimal
	Net           values.Decimal
	DebitWages    values.Decimal
	CreditCash    values.Decimal
	CreditTax     values.Decimal
	Digest        string
}

// Delta is the exact corrected-minus-original difference with balanced
// gross/net/tax and accounting legs.
type Delta struct {
	DGross      values.Decimal
	DTax        values.Decimal
	DNet        values.Decimal
	DDebitWages values.Decimal
	DCreditCash values.Decimal
	DCreditTax  values.Decimal
}

// CorrectionState is the closed correction lifecycle vocabulary.
type CorrectionState string

const (
	CorrectionProposed  CorrectionState = "PROPOSED"
	CorrectionSimulated CorrectionState = "SIMULATED"
	CorrectionApproved  CorrectionState = "APPROVED"
	CorrectionAppended  CorrectionState = "APPENDED"
)

// Evidence is the reconciled pay/accounting/tax/filing record appended for
// one correction. Filing always references the original run it supersedes.
type Evidence struct {
	CorrectionID  string
	OriginalRunID string
	Corrected     PayRun
	Delta         Delta
	PayDigest     string
	FilingRef     string
}

// Correction is one in-flight correction.
type Correction struct {
	ID             string
	OriginalRunID  string
	OriginalDigest string
	Corrected      PayRun
	Delta          Delta
	State          CorrectionState
	Approvals      []string
}

// payRunDigest binds every money and identity field of one run.
func payRunDigest(run PayRun) string {
	parts := []string{
		"payrun", run.RunID, run.EmployeeID, run.EffectiveDate,
		run.KnownAt.UTC().Format(time.RFC3339Nano), run.RuleVersionID,
		run.RegularPay.String(), run.OvertimePay.String(), run.Gross.String(),
		run.Tax.String(), run.Net.String(),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Calculate runs one pay calculation under exactly the given rule version:
// regular plus overtime at the pinned multiplier, flat tax on gross, net
// as gross minus tax, and balanced wage-expense/cash/tax-payable legs.
func Calculate(runID string, in Inputs, rule RuleVersion, effectiveDate string, knownAt time.Time) (PayRun, error) {
	rounding, err := values.ParseRoundingMode("HALF_UP")
	if err != nil {
		return PayRun{}, err
	}
	regularPay, err := in.RegularHours.Mul(in.HourlyRate, 2, rounding)
	if err != nil {
		return PayRun{}, fmt.Errorf("correction: Calculate regular: %w", err)
	}
	otRate, err := in.HourlyRate.Mul(rule.OvertimeMultiplier, 2, rounding)
	if err != nil {
		return PayRun{}, fmt.Errorf("correction: Calculate overtime rate: %w", err)
	}
	overtimePay, err := in.OvertimeHours.Mul(otRate, 2, rounding)
	if err != nil {
		return PayRun{}, fmt.Errorf("correction: Calculate overtime: %w", err)
	}
	gross, err := regularPay.Add(overtimePay)
	if err != nil {
		return PayRun{}, fmt.Errorf("correction: Calculate gross: %w", err)
	}
	tax, err := gross.Mul(rule.TaxRate, 2, rounding)
	if err != nil {
		return PayRun{}, fmt.Errorf("correction: Calculate tax: %w", err)
	}
	net, err := gross.Sub(tax)
	if err != nil {
		return PayRun{}, fmt.Errorf("correction: Calculate net: %w", err)
	}
	run := PayRun{
		RunID: runID, EmployeeID: in.EmployeeID, EffectiveDate: effectiveDate,
		KnownAt: knownAt, RuleVersionID: rule.ID,
		RegularHours: in.RegularHours, OvertimeHours: in.OvertimeHours, HourlyRate: in.HourlyRate,
		RegularPay: regularPay, OvertimePay: overtimePay, Gross: gross, Tax: tax, Net: net,
		DebitWages: gross, CreditCash: net, CreditTax: tax,
	}
	run.Digest = payRunDigest(run)
	return run, nil
}

// Registry is the pinned rule-version table: history resolves versions,
// never the current rules.
type Registry struct {
	versions map[string]RuleVersion
}

// NewRegistry builds the pinned table from released versions.
func NewRegistry(versions ...RuleVersion) Registry {
	registry := Registry{versions: make(map[string]RuleVersion, len(versions))}
	for _, version := range versions {
		registry.versions[version.ID] = version
	}
	return registry
}

// Lookup resolves one pinned version or refuses.
func (r Registry) Lookup(id string) (RuleVersion, error) {
	version, ok := r.versions[id]
	if !ok {
		return RuleVersion{}, fmt.Errorf("correction: Lookup %s: %w", id, ErrUnknownRuleVersion)
	}
	return version, nil
}

// Reproduce recalculates the original run from its inputs under its pinned
// rule version and requires an exact digest match: the current rule version
// rewriting history is the RED this refuses.
func Reproduce(original PayRun, in Inputs, registry Registry) error {
	// The presented run must first be internally consistent: edited
	// amounts riding on the original digest are tampering, not history.
	if payRunDigest(original) != original.Digest {
		return fmt.Errorf("correction: Reproduce %s: %w", original.RunID, ErrHistoryMismatch)
	}
	version, err := registry.Lookup(original.RuleVersionID)
	if err != nil {
		return err
	}
	recalculated, err := Calculate(original.RunID, in, version, original.EffectiveDate, original.KnownAt)
	if err != nil {
		return err
	}
	if recalculated.Digest != original.Digest {
		return fmt.Errorf("correction: Reproduce %s: %w", original.RunID, ErrHistoryMismatch)
	}
	return nil
}

// ComputeDelta differences the corrected run against the original. The
// corrected run must cover the same employee and period under a new rule
// version learned later; every leg must balance exactly.
func ComputeDelta(original, corrected PayRun) (Delta, error) {
	if corrected.EmployeeID != original.EmployeeID || corrected.EffectiveDate != original.EffectiveDate {
		return Delta{}, fmt.Errorf("correction: ComputeDelta identity: %w", ErrUnbalancedDelta)
	}
	if corrected.RuleVersionID == original.RuleVersionID {
		return Delta{}, fmt.Errorf("correction: ComputeDelta same version: %w", ErrUnbalancedDelta)
	}
	if !corrected.KnownAt.After(original.KnownAt) {
		return Delta{}, fmt.Errorf("correction: ComputeDelta known time: %w", ErrUnbalancedDelta)
	}
	var err error
	delta := Delta{}
	if delta.DGross, err = corrected.Gross.Sub(original.Gross); err != nil {
		return Delta{}, err
	}
	if delta.DTax, err = corrected.Tax.Sub(original.Tax); err != nil {
		return Delta{}, err
	}
	if delta.DNet, err = corrected.Net.Sub(original.Net); err != nil {
		return Delta{}, err
	}
	if delta.DDebitWages, err = corrected.DebitWages.Sub(original.DebitWages); err != nil {
		return Delta{}, err
	}
	if delta.DCreditCash, err = corrected.CreditCash.Sub(original.CreditCash); err != nil {
		return Delta{}, err
	}
	if delta.DCreditTax, err = corrected.CreditTax.Sub(original.CreditTax); err != nil {
		return Delta{}, err
	}
	balanced, err := delta.DNet.Add(delta.DTax)
	if err != nil {
		return Delta{}, err
	}
	if !balanced.Equal(delta.DGross) {
		return Delta{}, fmt.Errorf("correction: ComputeDelta gross/net/tax: %w", ErrUnbalancedDelta)
	}
	if !delta.DDebitWages.Equal(delta.DGross) || !delta.DCreditCash.Equal(delta.DNet) || !delta.DCreditTax.Equal(delta.DTax) {
		return Delta{}, fmt.Errorf("correction: ComputeDelta accounting: %w", ErrUnbalancedDelta)
	}
	return delta, nil
}

// Propose binds one corrected run to its original digest.
func Propose(id string, original, corrected PayRun, delta Delta) Correction {
	return Correction{
		ID: id, OriginalRunID: original.RunID, OriginalDigest: original.Digest,
		Corrected: corrected, Delta: delta, State: CorrectionProposed,
	}
}

// Simulate validates the proposal with zero effect: the original binding
// must hold and the delta must recompute balanced from the two runs,
// advancing PROPOSED to SIMULATED.
func Simulate(correction Correction, original PayRun) (Correction, error) {
	if correction.State != CorrectionProposed {
		return Correction{}, fmt.Errorf("correction: Simulate %s: %w", correction.ID, ErrCorrectionState)
	}
	if original.RunID != correction.OriginalRunID || original.Digest != correction.OriginalDigest {
		return Correction{}, fmt.Errorf("correction: Simulate %s: %w", correction.ID, ErrOriginalTampered)
	}
	recomputed, err := ComputeDelta(original, correction.Corrected)
	if err != nil {
		return Correction{}, fmt.Errorf("correction: Simulate %s: %w", correction.ID, err)
	}
	for _, leg := range [][2]values.Decimal{
		{recomputed.DGross, correction.Delta.DGross},
		{recomputed.DTax, correction.Delta.DTax},
		{recomputed.DNet, correction.Delta.DNet},
		{recomputed.DDebitWages, correction.Delta.DDebitWages},
		{recomputed.DCreditCash, correction.Delta.DCreditCash},
		{recomputed.DCreditTax, correction.Delta.DCreditTax},
	} {
		if !leg[0].Equal(leg[1]) {
			return Correction{}, fmt.Errorf("correction: Simulate %s: %w", correction.ID, ErrUnbalancedDelta)
		}
	}
	correction.State = CorrectionSimulated
	return correction, nil
}

// Approve records governed approval on a simulated correction.
func Approve(correction Correction, approvals ...string) (Correction, error) {
	if correction.State != CorrectionSimulated {
		return Correction{}, fmt.Errorf("correction: Approve %s: %w", correction.ID, ErrCorrectionState)
	}
	if len(approvals) == 0 {
		return Correction{}, fmt.Errorf("correction: Approve %s: %w", correction.ID, ErrCorrectionState)
	}
	correction.Approvals = append([]string(nil), approvals...)
	correction.State = CorrectionApproved
	return correction, nil
}

// Store is the append-only run and evidence ledger. Runs are immutable:
// AppendRun refuses a duplicate ID, and corrections supersede originals.
type Store struct {
	runs        map[string]PayRun
	corrections map[string]Evidence
}

// NewStore builds an empty ledger.
func NewStore() *Store {
	return &Store{runs: make(map[string]PayRun), corrections: make(map[string]Evidence)}
}

// AppendRun files one original run; a duplicate ID is an overwrite attempt.
func (s *Store) AppendRun(run PayRun) error {
	if _, ok := s.runs[run.RunID]; ok {
		return fmt.Errorf("correction: AppendRun %s: %w", run.RunID, ErrOriginalTampered)
	}
	s.runs[run.RunID] = run
	return nil
}

// AppendCorrection files one approved correction with reconciled evidence.
// It refuses unknown originals, digest drift on the original, and replays.
func (s *Store) AppendCorrection(correction Correction) (Evidence, error) {
	if correction.State != CorrectionApproved {
		return Evidence{}, fmt.Errorf("correction: AppendCorrection %s: %w", correction.ID, ErrCorrectionState)
	}
	original, ok := s.runs[correction.OriginalRunID]
	if !ok {
		return Evidence{}, fmt.Errorf("correction: AppendCorrection %s: %w", correction.ID, ErrOriginalTampered)
	}
	// The stored original must be self-consistent and match the bound
	// digest: either drift means the original was overwritten.
	if payRunDigest(original) != original.Digest || original.Digest != correction.OriginalDigest {
		return Evidence{}, fmt.Errorf("correction: AppendCorrection %s: %w", correction.ID, ErrOriginalTampered)
	}
	if _, ok := s.corrections[correction.ID]; ok {
		return Evidence{}, fmt.Errorf("correction: AppendCorrection %s: %w", correction.ID, ErrAlreadyAppended)
	}
	sum := sha256.Sum256([]byte("pay-evidence\x00" + correction.ID + "\x00" + correction.Corrected.Digest))
	evidence := Evidence{
		CorrectionID: correction.ID, OriginalRunID: original.RunID,
		Corrected: correction.Corrected, Delta: correction.Delta,
		PayDigest: "sha256:" + hex.EncodeToString(sum[:]),
		FilingRef: "filing/" + original.RunID + "/superseded-by/" + correction.ID,
	}
	s.corrections[correction.ID] = evidence
	correction.State = CorrectionAppended
	return evidence, nil
}

// Original returns the stored run for one ID.
func (s *Store) Original(id string) (PayRun, bool) {
	run, ok := s.runs[id]
	return run, ok
}
