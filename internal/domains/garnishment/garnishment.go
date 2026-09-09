// Package garnishment owns the pure generation and authorization contract for
// a payroll garnishment remittance. It consumes payroll withholding evidence
// and a governed settlement instruction, but has no persistence or provider
// side effects.
package garnishment

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const schemaVersion = 1

// Version reports this package's stable contract version.
func Version() int { return schemaVersion }

var (
	ErrInvalidRemittance    = errors.New("garnishment: invalid remittance")
	ErrRemittanceRejected   = errors.New("GARN_005_REJECTED")
	ErrWithholdingMismatch  = errors.New("garnishment: withholding total mismatch")
	ErrDestinationMismatch  = errors.New("garnishment: destination does not match approval")
	ErrPayeeMismatch        = errors.New("garnishment: payee does not match approval")
	ErrBatchChanged         = errors.New("garnishment: remittance batch changed after approval")
	ErrInstructionChanged   = errors.New("garnishment: settlement instruction changed after approval")
	ErrDualControlRequired  = errors.New("garnishment: distinct requester and approver are required")
	ErrStepUpRequired       = errors.New("garnishment: step-up is required for remittance authorization")
	ErrStepUpProofInvalid   = errors.New("garnishment: step-up proof is invalid")
	ErrAuthorizationInvalid = errors.New("garnishment: authorization is invalid")
)

// Withholding is the minimum payroll evidence needed to generate one
// remittance line. References identify governed records; no worker or bank
// payload is accepted here.
type Withholding struct {
	TenantID       string
	ID             string
	OrderRef       string
	PayeeRef       string
	DestinationRef string
	PayrollLineRef string
	Amount         values.Decimal
	Currency       string
}

// WithholdingLine and RemittanceLine are descriptive aliases for callers that
// use either the payroll or remittance vocabulary.
type WithholdingLine = Withholding
type RemittanceLine = Withholding

// RemittanceSpec is the immutable-input envelope for Generate. The expected
// total is supplied by the payroll withholding calculation and is checked
// against the line sum rather than silently recomputed over a bad source.
type RemittanceSpec struct {
	BatchID                  string
	PayrollRunRef            string
	PayrollWithholdingDigest string
	PayrollWithholdingTotal  values.Decimal
	Currency                 string
	Lines                    []Withholding
}

// GenerateInput is a descriptive alias for RemittanceSpec.
type GenerateInput = RemittanceSpec

// RemittanceBatch is one immutable, digest-addressed remittance proposal.
// Every line in a batch targets the same payee and tokenized destination so
// it can be bound to one settlement.PaymentInstruction.
type RemittanceBatch struct {
	TenantID                 string
	BatchID                  string
	PayrollRunRef            string
	PayrollWithholdingDigest string
	PayrollWithholdingTotal  values.Decimal
	Total                    values.Decimal
	Currency                 string
	PayeeRef                 string
	DestinationRef           string
	Lines                    []Withholding
	Revision                 uint64
	CanonicalDigest          string
}

// NewRemittance generates a canonical remittance batch from payroll
// withholding evidence. It writes nothing and preserves caller-owned slices.
func NewRemittance(spec RemittanceSpec) (RemittanceBatch, error) {
	if err := validateSpec(spec); err != nil {
		return RemittanceBatch{}, reject(err)
	}

	lines := slices.Clone(spec.Lines)
	sort.Slice(lines, func(i, j int) bool {
		return withholdingKey(lines[i]) < withholdingKey(lines[j])
	})

	total := zeroLike(spec.PayrollWithholdingTotal)
	for _, line := range lines {
		if line.PayeeRef != lines[0].PayeeRef || line.DestinationRef != lines[0].DestinationRef {
			return RemittanceBatch{}, reject(fmt.Errorf("%w: every line must share one payee and destination", ErrInvalidRemittance))
		}
		if line.TenantID != lines[0].TenantID {
			return RemittanceBatch{}, reject(fmt.Errorf("%w: every line must share one tenant", ErrInvalidRemittance))
		}
		var err error
		total, err = total.Add(line.Amount)
		if err != nil {
			return RemittanceBatch{}, reject(err)
		}
	}
	if !total.Equal(spec.PayrollWithholdingTotal) {
		return RemittanceBatch{}, reject(ErrWithholdingMismatch)
	}

	batch := RemittanceBatch{
		TenantID:                 lines[0].TenantID,
		BatchID:                  spec.BatchID,
		PayrollRunRef:            spec.PayrollRunRef,
		PayrollWithholdingDigest: spec.PayrollWithholdingDigest,
		PayrollWithholdingTotal:  spec.PayrollWithholdingTotal,
		Total:                    total,
		Currency:                 spec.Currency,
		PayeeRef:                 lines[0].PayeeRef,
		DestinationRef:           lines[0].DestinationRef,
		Lines:                    lines,
		Revision:                 1,
	}
	batchDigest, err := batch.computedDigest()
	if err != nil {
		return RemittanceBatch{}, reject(err)
	}
	batch.CanonicalDigest = batchDigest
	return batch, nil
}

// Generate is the concise package-level spelling for NewRemittance.
func Generate(spec RemittanceSpec) (RemittanceBatch, error) { return NewRemittance(spec) }

// NewRemittanceBatch is an explicit constructor alias.
func NewRemittanceBatch(spec RemittanceSpec) (RemittanceBatch, error) {
	return NewRemittance(spec)
}

func validateSpec(spec RemittanceSpec) error {
	if strings.TrimSpace(spec.BatchID) == "" || strings.TrimSpace(spec.PayrollRunRef) == "" || strings.TrimSpace(spec.PayrollWithholdingDigest) == "" {
		return fmt.Errorf("%w: batch, payroll run and withholding digest are required", ErrInvalidRemittance)
	}
	if err := spec.PayrollWithholdingTotal.Validate(); err != nil {
		return fmt.Errorf("%w: payroll withholding total: %v", ErrInvalidRemittance, err)
	}
	if spec.PayrollWithholdingTotal.Sign() <= 0 {
		return fmt.Errorf("%w: payroll withholding total must be positive", ErrInvalidRemittance)
	}
	if strings.TrimSpace(spec.Currency) == "" {
		return fmt.Errorf("%w: currency is required", ErrInvalidRemittance)
	}
	if len(spec.Lines) == 0 {
		return fmt.Errorf("%w: at least one withholding line is required", ErrInvalidRemittance)
	}

	seen := make(map[string]struct{}, len(spec.Lines))
	for _, line := range spec.Lines {
		if err := line.Validate(spec.Currency); err != nil {
			return err
		}
		key := withholdingKey(line)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: duplicate withholding line %s", ErrInvalidRemittance, line.ID)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// Validate checks one payroll withholding line against the batch currency.
func (w Withholding) Validate(currency string) error {
	for name, value := range map[string]string{
		"tenant_id": w.TenantID, "id": w.ID, "order_ref": w.OrderRef, "payee_ref": w.PayeeRef,
		"destination_ref": w.DestinationRef, "payroll_line_ref": w.PayrollLineRef,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: withholding %s is required", ErrInvalidRemittance, name)
		}
	}
	if err := w.Amount.Validate(); err != nil {
		return fmt.Errorf("%w: withholding amount: %v", ErrInvalidRemittance, err)
	}
	if w.Amount.Sign() <= 0 {
		return fmt.Errorf("%w: withholding amount must be positive", ErrInvalidRemittance)
	}
	if strings.TrimSpace(w.Currency) == "" || w.Currency != currency {
		return fmt.Errorf("%w: withholding currency must equal batch currency", ErrInvalidRemittance)
	}
	return nil
}

// Validate reports whether a batch is intact and internally reconciled.
func (b RemittanceBatch) Validate() error {
	if strings.TrimSpace(b.BatchID) == "" || strings.TrimSpace(b.PayrollRunRef) == "" || strings.TrimSpace(b.PayrollWithholdingDigest) == "" {
		return fmt.Errorf("%w: batch, payroll run and withholding digest are required", ErrInvalidRemittance)
	}
	if b.Revision == 0 || strings.TrimSpace(b.TenantID) == "" || strings.TrimSpace(b.Currency) == "" || len(b.Lines) == 0 {
		return fmt.Errorf("%w: revision, currency and lines are required", ErrInvalidRemittance)
	}
	if err := b.PayrollWithholdingTotal.Validate(); err != nil {
		return fmt.Errorf("%w: payroll withholding total: %v", ErrInvalidRemittance, err)
	}
	if err := b.Total.Validate(); err != nil {
		return fmt.Errorf("%w: total: %v", ErrInvalidRemittance, err)
	}
	if !b.Total.Equal(b.PayrollWithholdingTotal) || b.Total.Sign() <= 0 {
		return fmt.Errorf("%w: %w", ErrInvalidRemittance, ErrWithholdingMismatch)
	}
	if strings.TrimSpace(b.PayeeRef) == "" || strings.TrimSpace(b.DestinationRef) == "" {
		return fmt.Errorf("%w: payee and destination are required", ErrInvalidRemittance)
	}
	if err := validateSpec(RemittanceSpec{
		BatchID: b.BatchID, PayrollRunRef: b.PayrollRunRef,
		PayrollWithholdingDigest: b.PayrollWithholdingDigest,
		PayrollWithholdingTotal:  b.PayrollWithholdingTotal, Currency: b.Currency, Lines: b.Lines,
	}); err != nil {
		return err
	}
	sum := zeroLike(b.PayrollWithholdingTotal)
	for _, line := range b.Lines {
		var err error
		sum, err = sum.Add(line.Amount)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidRemittance, ErrWithholdingMismatch)
		}
	}
	if !sum.Equal(b.Total) {
		return fmt.Errorf("%w: %w", ErrInvalidRemittance, ErrWithholdingMismatch)
	}
	for _, line := range b.Lines {
		if line.TenantID != b.TenantID || line.PayeeRef != b.PayeeRef || line.DestinationRef != b.DestinationRef {
			return fmt.Errorf("%w: line target differs from batch target", ErrInvalidRemittance)
		}
	}
	digest, err := b.computedDigest()
	if err != nil {
		return fmt.Errorf("%w: canonical digest: %v", ErrInvalidRemittance, err)
	}
	if b.CanonicalDigest == "" || b.CanonicalDigest != digest {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidRemittance)
	}
	return nil
}

func withholdingKey(w Withholding) string {
	return w.OrderRef + "\x00" + w.PayeeRef + "\x00" + w.DestinationRef + "\x00" + w.ID
}

func zeroLike(d values.Decimal) values.Decimal {
	return values.MustDecimal("0", d.Scale(), d.Rounding())
}

func (b RemittanceBatch) body() ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.garnishment.RemittanceBatch", schemaVersion).
		String("tenant_id", b.TenantID).String("batch_id", b.BatchID).
		String("payroll_run_ref", b.PayrollRunRef).
		String("payroll_withholding_digest", b.PayrollWithholdingDigest).
		Value("payroll_withholding_total", b.PayrollWithholdingTotal).
		Value("total", b.Total).
		String("currency", b.Currency).
		String("payee_ref", b.PayeeRef).
		String("destination_ref", b.DestinationRef).
		Int("revision", int64(b.Revision)).
		Count("lines", len(b.Lines))
	for _, line := range b.Lines {
		w.String("line.tenant_id", line.TenantID).String("line.id", line.ID).
			String("line.order_ref", line.OrderRef).
			String("line.payee_ref", line.PayeeRef).
			String("line.destination_ref", line.DestinationRef).
			String("line.payroll_line_ref", line.PayrollLineRef).
			Value("line.amount", line.Amount).
			String("line.currency", line.Currency)
	}
	return w.Bytes()
}

func (b RemittanceBatch) computedDigest() (string, error) {
	w := canonicalbytes.New("hcmnext.domains.garnishment.RemittanceBatch", schemaVersion).
		String("tenant_id", b.TenantID).String("batch_id", b.BatchID).
		String("payroll_run_ref", b.PayrollRunRef).
		String("payroll_withholding_digest", b.PayrollWithholdingDigest).
		Value("payroll_withholding_total", b.PayrollWithholdingTotal).
		Value("total", b.Total).String("currency", b.Currency).
		String("payee_ref", b.PayeeRef).String("destination_ref", b.DestinationRef).
		Int("revision", int64(b.Revision)).Count("lines", len(b.Lines))
	for _, line := range b.Lines {
		w.String("line.tenant_id", line.TenantID).String("line.id", line.ID).
			String("line.order_ref", line.OrderRef).String("line.payee_ref", line.PayeeRef).
			String("line.destination_ref", line.DestinationRef).String("line.payroll_line_ref", line.PayrollLineRef).
			Value("line.amount", line.Amount).String("line.currency", line.Currency)
	}
	return w.Digest()
}

// Digest returns the canonical identity of a validated batch.
func (b RemittanceBatch) Digest() (string, error) {
	if err := b.Validate(); err != nil {
		return "", err
	}
	return b.CanonicalDigest, nil
}

// SettlementInstructionState is the small state projection needed at the
// garnishment boundary. The settlement domain owns the full lifecycle.
type SettlementInstructionState string

const SettlementInstructionInstructed SettlementInstructionState = "INSTRUCTED"

// SettlementInstruction is the provider-neutral projection consumed by this
// package from SETTLE-003. Its CanonicalDigest must be produced by the
// settlement owner; the duplicated shape keeps this kernel package buildable
// while the settlement package may evolve independently.
type SettlementInstruction struct {
	TenantID        string
	InstructionID   string
	PayrollRunRef   string
	PayeeRef        string
	DestinationRef  string
	Amount          values.Decimal
	Currency        string
	State           SettlementInstructionState
	CanonicalDigest string
}

// PaymentInstruction is a descriptive alias for callers using settlement
// terminology at this boundary.
type PaymentInstruction = SettlementInstruction

// Validate checks the exact instruction projection required for authorization.
func (i SettlementInstruction) Validate() error {
	for name, value := range map[string]string{
		"tenant_id": i.TenantID, "instruction_id": i.InstructionID, "payroll_run_ref": i.PayrollRunRef,
		"payee_ref": i.PayeeRef, "destination_ref": i.DestinationRef,
		"currency": i.Currency,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: settlement instruction %s is required", ErrInvalidRemittance, name)
		}
	}
	if i.State != SettlementInstructionInstructed {
		return fmt.Errorf("%w: settlement instruction is not instructed", ErrInvalidRemittance)
	}
	if err := i.Amount.Validate(); err != nil || i.Amount.Sign() <= 0 {
		return fmt.Errorf("%w: settlement instruction amount is invalid", ErrInvalidRemittance)
	}
	digest, err := i.computedDigest()
	if err != nil {
		return fmt.Errorf("%w: instruction digest: %v", ErrInvalidRemittance, err)
	}
	if i.CanonicalDigest == "" || i.CanonicalDigest != digest {
		return fmt.Errorf("%w: settlement instruction digest mismatch", ErrInvalidRemittance)
	}
	return nil
}

func (i SettlementInstruction) body() ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.garnishment.SettlementInstruction", schemaVersion).
		String("tenant_id", i.TenantID).String("instruction_id", i.InstructionID).
		String("payroll_run_ref", i.PayrollRunRef).
		String("payee_ref", i.PayeeRef).
		String("destination_ref", i.DestinationRef).
		Value("amount", i.Amount).
		String("currency", i.Currency).
		String("state", string(i.State))
	return w.Bytes()
}

func (i SettlementInstruction) computedDigest() (string, error) {
	w := canonicalbytes.New("hcmnext.domains.garnishment.SettlementInstruction", schemaVersion).
		String("tenant_id", i.TenantID).String("instruction_id", i.InstructionID).
		String("payroll_run_ref", i.PayrollRunRef).String("payee_ref", i.PayeeRef).
		String("destination_ref", i.DestinationRef).Value("amount", i.Amount).
		String("currency", i.Currency).String("state", string(i.State))
	return w.Digest()
}

// NewSettlementInstruction constructs the projection used in pure tests and
// adapters. Production callers should supply the settlement owner's digest.
func NewSettlementInstruction(tenantID, id, runRef, payeeRef, destinationRef string, amount values.Decimal, currency string) (SettlementInstruction, error) {
	i := SettlementInstruction{TenantID: tenantID, InstructionID: id, PayrollRunRef: runRef, PayeeRef: payeeRef, DestinationRef: destinationRef, Amount: amount, Currency: currency, State: SettlementInstructionInstructed}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(id) == "" || strings.TrimSpace(runRef) == "" || strings.TrimSpace(payeeRef) == "" || strings.TrimSpace(destinationRef) == "" || strings.TrimSpace(currency) == "" || amount.Validate() != nil || amount.Sign() <= 0 {
		return SettlementInstruction{}, fmt.Errorf("%w: invalid settlement instruction", ErrInvalidRemittance)
	}
	var err error
	i.CanonicalDigest, err = i.computedDigest()
	if err != nil {
		return SettlementInstruction{}, fmt.Errorf("%w: instruction digest: %v", ErrInvalidRemittance, err)
	}
	return i, nil
}

// AuthorizationRequest is the decision-time control evidence for a
// garnishment remittance. Distinct identities and step-up are mandatory for
// this money-moving operation.
type AuthorizationRequest struct {
	TenantID         string
	Instruction      SettlementInstruction
	Requester        *trust.Principal
	Approver         *trust.Principal
	StepUpProof      StepUpProof
	AuthorizationRef string
	At               time.Time
}

// StepUpProof is the typed, operation-bound evidence required for a
// garnishment authorization. Its digest covers the tenant, subject, exact
// batch and instruction identities, assurance and validity window.
type StepUpProof struct {
	TenantID          string
	Assurance         trust.Assurance
	Subject           string
	BatchDigest       string
	InstructionDigest string
	IssuedAt          time.Time
	ExpiresAt         time.Time
	Digest            string
}

// NewStepUpProof constructs canonical proof evidence for one batch and
// settlement instruction.
func NewStepUpProof(tenantID string, assurance trust.Assurance, subject, batchDigest, instructionDigest string, issuedAt, expiresAt time.Time) (StepUpProof, error) {
	p := StepUpProof{TenantID: tenantID, Assurance: assurance, Subject: subject, BatchDigest: batchDigest, InstructionDigest: instructionDigest, IssuedAt: issuedAt.UTC(), ExpiresAt: expiresAt.UTC()}
	if err := p.validateShape(); err != nil {
		return StepUpProof{}, err
	}
	digest, err := p.computedDigest()
	if err != nil {
		return StepUpProof{}, err
	}
	p.Digest = digest
	return p, nil
}

// Validate verifies the proof's canonical digest, identity binding, assurance
// floor and freshness at the authorization instant.
func (p StepUpProof) Validate(at time.Time, tenantID, subject, batchDigest, instructionDigest string) error {
	if err := p.validateShape(); err != nil {
		return err
	}
	digest, err := p.computedDigest()
	if err != nil || p.Digest == "" || p.Digest != digest {
		return fmt.Errorf("%w: canonical proof digest mismatch", ErrStepUpProofInvalid)
	}
	if p.TenantID != tenantID || p.Subject != subject || p.BatchDigest != batchDigest || p.InstructionDigest != instructionDigest {
		return fmt.Errorf("%w: proof binding mismatch", ErrStepUpProofInvalid)
	}
	if at.IsZero() || at.Before(p.IssuedAt) || !at.Before(p.ExpiresAt) {
		return fmt.Errorf("%w: proof is expired or premature", ErrStepUpProofInvalid)
	}
	return nil
}

func (p StepUpProof) validateShape() error {
	if strings.TrimSpace(p.TenantID) == "" || strings.TrimSpace(p.Subject) == "" || !validDigest(p.BatchDigest) || !validDigest(p.InstructionDigest) || p.IssuedAt.IsZero() || p.ExpiresAt.IsZero() || !p.ExpiresAt.After(p.IssuedAt) || !p.Assurance.AtLeast(trust.AssuranceHigh) {
		return fmt.Errorf("%w: assurance, subject, binding and validity are required", ErrStepUpProofInvalid)
	}
	return nil
}

func (p StepUpProof) computedDigest() (string, error) {
	w := canonicalbytes.New("hcmnext.domains.garnishment.StepUpProof", schemaVersion).
		String("tenant_id", p.TenantID).String("assurance", p.Assurance.String()).String("subject", p.Subject).
		String("batch_digest", p.BatchDigest).String("instruction_digest", p.InstructionDigest).
		Int("issued_at", p.IssuedAt.UnixNano()).Int("expires_at", p.ExpiresAt.UnixNano())
	return w.Digest()
}

func validDigest(value string) bool {
	return len(value) == len("sha256:")+64 && strings.HasPrefix(value, "sha256:")
}

// RemittanceAuthorization is bound to the exact batch and settlement
// instruction that were reviewed. It is not reusable for a successor batch,
// a different destination, or a different settlement instruction.
type RemittanceAuthorization struct {
	TenantID             string
	BatchDigest          string
	InstructionDigest    string
	PayrollRunRef        string
	PayeeRef             string
	DestinationRef       string
	Total                values.Decimal
	Currency             string
	Requester            string
	Approver             string
	RequesterFingerprint string
	ApproverFingerprint  string
	StepUpProofDigest    string
	AuthorizationRef     string
	Rule                 string
	CanonicalDigest      string
}

// Authorization is a descriptive alias for RemittanceAuthorization.
type Authorization = RemittanceAuthorization

const authorizationRule = "garnishment.remittance.exact-batch-destination-dual-control"

// Authorize binds a generated batch to a governed settlement instruction.
// It refuses a wrong payee/destination, changed amount/run, self-approval or
// missing step-up before any side effect can be attempted.
func Authorize(batch RemittanceBatch, req AuthorizationRequest) (RemittanceAuthorization, error) {
	if err := batch.Validate(); err != nil {
		return RemittanceAuthorization{}, reject(err)
	}
	if err := req.Instruction.Validate(); err != nil {
		return RemittanceAuthorization{}, reject(ErrInstructionChanged)
	}
	if strings.TrimSpace(req.TenantID) == "" || batch.TenantID != req.TenantID || req.Instruction.TenantID != req.TenantID {
		return RemittanceAuthorization{}, reject(ErrAuthorizationInvalid)
	}
	if req.Requester == nil || req.Approver == nil || req.Requester.Tenant().String() != req.TenantID || req.Approver.Tenant().String() != req.TenantID || req.Requester.Fingerprint() == req.Approver.Fingerprint() {
		return RemittanceAuthorization{}, reject(ErrDualControlRequired)
	}
	if req.Instruction.State != SettlementInstructionInstructed {
		return RemittanceAuthorization{}, reject(fmt.Errorf("%w: instruction must still be instructed", ErrInstructionChanged))
	}
	if req.Instruction.PayrollRunRef != batch.PayrollRunRef || !req.Instruction.Amount.Equal(batch.Total) || req.Instruction.Currency != batch.Currency {
		return RemittanceAuthorization{}, reject(ErrWithholdingMismatch)
	}
	if req.Instruction.PayeeRef != batch.PayeeRef {
		return RemittanceAuthorization{}, reject(ErrPayeeMismatch)
	}
	if req.Instruction.DestinationRef != batch.DestinationRef {
		return RemittanceAuthorization{}, reject(ErrDestinationMismatch)
	}
	if err := req.StepUpProof.Validate(req.At, req.TenantID, req.Requester.Subject(), batch.CanonicalDigest, req.Instruction.CanonicalDigest); err != nil {
		return RemittanceAuthorization{}, reject(ErrStepUpRequired)
	}

	a := RemittanceAuthorization{
		TenantID:    req.TenantID,
		BatchDigest: batch.CanonicalDigest, InstructionDigest: req.Instruction.CanonicalDigest,
		PayrollRunRef: batch.PayrollRunRef, PayeeRef: batch.PayeeRef,
		DestinationRef: batch.DestinationRef, Total: batch.Total, Currency: batch.Currency,
		Requester: req.Requester.Subject(), Approver: req.Approver.Subject(),
		RequesterFingerprint: req.Requester.Fingerprint(), ApproverFingerprint: req.Approver.Fingerprint(),
		StepUpProofDigest: req.StepUpProof.Digest,
		AuthorizationRef:  req.AuthorizationRef, Rule: authorizationRule,
	}
	aDigest, err := a.computedDigest()
	if err != nil {
		return RemittanceAuthorization{}, reject(err)
	}
	a.CanonicalDigest = aDigest
	return a, nil
}

// AuthorizeRemittance is the explicit operation spelling for callers that
// prefer the domain noun in the function name.
func AuthorizeRemittance(batch RemittanceBatch, req AuthorizationRequest) (RemittanceAuthorization, error) {
	return Authorize(batch, req)
}

// ValidateFor revalidates the approval at material execution time. A batch
// or instruction must be byte-identical to what the requester and approver
// reviewed.
func (a RemittanceAuthorization) ValidateFor(batch RemittanceBatch, instruction SettlementInstruction) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if err := batch.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrBatchChanged, err)
	}
	if err := instruction.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInstructionChanged, err)
	}
	if batch.TenantID != a.TenantID || instruction.TenantID != a.TenantID {
		return ErrAuthorizationInvalid
	}
	if batch.CanonicalDigest != a.BatchDigest {
		return ErrBatchChanged
	}
	if instruction.CanonicalDigest != a.InstructionDigest {
		return ErrInstructionChanged
	}
	if instruction.DestinationRef != a.DestinationRef {
		return ErrDestinationMismatch
	}
	if instruction.PayeeRef != a.PayeeRef {
		return ErrPayeeMismatch
	}
	if instruction.PayrollRunRef != a.PayrollRunRef || !instruction.Amount.Equal(a.Total) || instruction.Currency != a.Currency {
		return ErrWithholdingMismatch
	}
	return nil
}

// Revalidate is an alias for ValidateFor used by execution boundaries.
func (a RemittanceAuthorization) Revalidate(batch RemittanceBatch, instruction SettlementInstruction) error {
	return a.ValidateFor(batch, instruction)
}

// Validate checks the authorization evidence and its own digest.
func (a RemittanceAuthorization) Validate() error {
	for name, value := range map[string]string{
		"tenant_id": a.TenantID, "batch_digest": a.BatchDigest, "instruction_digest": a.InstructionDigest,
		"payroll_run_ref": a.PayrollRunRef, "payee_ref": a.PayeeRef,
		"destination_ref": a.DestinationRef, "currency": a.Currency,
		"requester": a.Requester, "approver": a.Approver, "requester_fingerprint": a.RequesterFingerprint,
		"approver_fingerprint": a.ApproverFingerprint, "step_up_proof_digest": a.StepUpProofDigest, "rule": a.Rule,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrAuthorizationInvalid, name)
		}
	}
	if a.Requester == a.Approver {
		return fmt.Errorf("%w: %w", ErrAuthorizationInvalid, ErrDualControlRequired)
	}
	if err := a.Total.Validate(); err != nil {
		return fmt.Errorf("%w: total: %v", ErrAuthorizationInvalid, err)
	}
	digest, err := a.computedDigest()
	if err != nil {
		return fmt.Errorf("%w: canonical digest: %v", ErrAuthorizationInvalid, err)
	}
	if a.CanonicalDigest == "" || a.CanonicalDigest != digest {
		return fmt.Errorf("%w: canonical digest mismatch", ErrAuthorizationInvalid)
	}
	return nil
}

func (a RemittanceAuthorization) body() ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.garnishment.RemittanceAuthorization", schemaVersion).
		String("tenant_id", a.TenantID).String("batch_digest", a.BatchDigest).
		String("instruction_digest", a.InstructionDigest).
		String("payroll_run_ref", a.PayrollRunRef).
		String("payee_ref", a.PayeeRef).
		String("destination_ref", a.DestinationRef).
		Value("total", a.Total).
		String("currency", a.Currency).
		String("requester", a.Requester).
		String("approver", a.Approver).
		String("requester_fingerprint", a.RequesterFingerprint).
		String("approver_fingerprint", a.ApproverFingerprint).
		String("step_up_proof_digest", a.StepUpProofDigest).
		String("authorization_ref", a.AuthorizationRef).
		String("rule", a.Rule)
	return w.Bytes()
}

func (a RemittanceAuthorization) computedDigest() (string, error) {
	w := canonicalbytes.New("hcmnext.domains.garnishment.RemittanceAuthorization", schemaVersion).
		String("tenant_id", a.TenantID).String("batch_digest", a.BatchDigest).
		String("instruction_digest", a.InstructionDigest).String("payroll_run_ref", a.PayrollRunRef).
		String("payee_ref", a.PayeeRef).String("destination_ref", a.DestinationRef).
		Value("total", a.Total).String("currency", a.Currency).
		String("requester", a.Requester).String("approver", a.Approver).
		String("requester_fingerprint", a.RequesterFingerprint).String("approver_fingerprint", a.ApproverFingerprint).
		String("step_up_proof_digest", a.StepUpProofDigest).String("authorization_ref", a.AuthorizationRef).
		String("rule", a.Rule)
	return w.Digest()
}

// Explain renders audit-safe metadata without reproducing payroll amounts or
// protected destination material.
func (a RemittanceAuthorization) Explain() string {
	return fmt.Sprintf("garnishment remittance authorization rule=%s requester=%s approver=%s step_up_proof=%s batch=%s instruction=%s", a.Rule, a.Requester, a.Approver, a.StepUpProofDigest, a.BatchDigest, a.InstructionDigest)
}

// Explain is the package-level Explain-shaped symbol.
func Explain(b RemittanceBatch) string {
	return fmt.Sprintf("garnishment remittance batch=%s payroll_run=%s lines=%d total_currency=%s destination_bound=true digest=%s", b.BatchID, b.PayrollRunRef, len(b.Lines), b.Currency, b.CanonicalDigest)
}

func reject(err error) error {
	return fmt.Errorf("%w: %w", ErrRemittanceRejected, err)
}
