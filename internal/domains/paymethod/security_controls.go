package paymethod

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/contact"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/trust/dlp"
	"github.com/monstercameron/hcm-next/internal/trust/sod"
)

const securityControlSchemaVersion = 1

var (
	ErrInvalidBankDetailChange = errors.New("paymethod: invalid bank-detail change")
	ErrConfirmationRequired    = errors.New("paymethod: out-of-band confirmation is required")
	ErrIndependentContact      = errors.New("paymethod: independent contact endpoint is required")
	ErrChangeNotAvailable      = errors.New("paymethod: destination is still in the cooling-off period")
	ErrInvalidValidation       = errors.New("paymethod: invalid account validation record")
	ErrValidationRequired      = errors.New("paymethod: account validation is required before release")
	ErrExceptionRequired       = errors.New("paymethod: approved validation exception is required")
	ErrInvalidProtection       = errors.New("paymethod: account storage protection is invalid")
	ErrAccountNumberEgress     = errors.New("paymethod: account number refused in egress payload")
)

// BankDetailChangeOperation is the named operation governed by the SOD
// decision. Keeping the name stable makes the operation auditable without
// placing account data in an audit record.
const BankDetailChangeOperation = "payment_destination.bank_detail.change"

type ChangeStatus string

const (
	ChangeAwaitingConfirmation ChangeStatus = "AWAITING_CONFIRMATION"
	ChangeConfirmed            ChangeStatus = "CONFIRMED"
)

// BankDetailChangeRequest contains only protected digests for the before and
// after values. Raw account and routing numbers have no representation here.
type BankDetailChangeRequest struct {
	ID              string
	TenantID        string
	DestinationID   string
	WorkerRef       string
	BeforeDigest    string
	AfterDigest     string
	RequestedBy     string
	Approver        string
	RequestedAt     time.Time
	CoolingOff      time.Duration
	Constraints     sod.Constraints
	DecisionContext sod.DecisionContext
}

// ChangeConfirmation is the safe message handed to an out-of-band
// dispatcher. It contains endpoint and value digests only.
type ChangeConfirmation struct {
	Operation              string
	ChangeDigest           string
	TenantID               string
	DestinationID          string
	EndpointID             string
	EndpointRevisionDigest string
	BeforeDigest           string
	AfterDigest            string
	RequestedBy            string
	Approver               string
	DispatchedAt           time.Time
}

// IndependentContactSource is the I/O boundary used to resolve a contact
// endpoint from a source independent of the changed destination record.
type IndependentContactSource interface {
	ResolveIndependentContact(tenantID, workerRef, changedDestinationID string) (contact.ContactEndpointRevision, error)
}

// ConfirmationDispatcher is the I/O boundary for the independent channel.
type ConfirmationDispatcher interface {
	DispatchOutOfBand(ChangeConfirmation) error
}

// BankDetailChange is an immutable, digest-backed revision of the change
// operation. A confirmation produces a successor revision rather than
// mutating this value.
type BankDetailChange struct {
	Operation             string
	ID                    string
	TenantID              string
	DestinationID         string
	WorkerRef             string
	BeforeDigest          string
	AfterDigest           string
	RequestedBy           string
	Approver              string
	ContactEndpointID     string
	ContactRevisionDigest string
	RequestedAt           time.Time
	ConfirmedAt           time.Time
	AvailableAt           time.Time
	CoolingOff            time.Duration
	Status                ChangeStatus
	Revision              uint64
	SupersedesRevision    uint64
	SupersedesDigest      string
	CanonicalDigest       string
}

// StartBankDetailChange evaluates the SOD contract, resolves an independently
// sourced verified endpoint, dispatches the safe confirmation, and returns the
// first immutable operation revision.
func StartBankDetailChange(req BankDetailChangeRequest, source IndependentContactSource, dispatcher ConfirmationDispatcher) (BankDetailChange, error) {
	if err := validateBankDetailRequest(req); err != nil {
		return BankDetailChange{}, err
	}
	if source == nil {
		return BankDetailChange{}, fieldError(ErrIndependentContact, "contact_source", errors.New("contact source is required"))
	}
	if dispatcher == nil {
		return BankDetailChange{}, fieldError(ErrConfirmationRequired, "dispatcher", errors.New("confirmation dispatcher is required"))
	}
	if !req.Constraints.RequesterMayNotApprove {
		return BankDetailChange{}, fieldError(ErrInvalidBankDetailChange, "sod.constraints", errors.New("requester self-approval exclusion is required"))
	}
	if req.DecisionContext.Requester.Subject != req.RequestedBy {
		return BankDetailChange{}, fieldError(ErrInvalidBankDetailChange, "requested_by", errors.New("SOD requester does not match operation requester"))
	}
	result, err := sod.Evaluate(req.DecisionContext, req.Constraints, 1)
	if err != nil {
		return BankDetailChange{}, fieldError(ErrInvalidBankDetailChange, "sod", err)
	}
	if !contains(result.Eligible, req.Approver) {
		return BankDetailChange{}, fieldError(ErrDistinctApproverRequired, "approver", errors.New("approver was excluded by the SOD contract"))
	}

	endpoint, err := source.ResolveIndependentContact(req.TenantID, req.WorkerRef, req.DestinationID)
	if err != nil {
		return BankDetailChange{}, fieldError(ErrIndependentContact, "contact_endpoint", err)
	}
	if err := endpoint.Validate(); err != nil || endpoint.Verification != contact.Verified || strings.TrimSpace(endpoint.Source) == "" || endpoint.Source == req.DestinationID {
		return BankDetailChange{}, fieldError(ErrIndependentContact, "contact_endpoint", errors.New("endpoint must be verified and independently sourced"))
	}

	when := req.RequestedAt
	change := BankDetailChange{
		Operation:             BankDetailChangeOperation,
		ID:                    req.ID,
		TenantID:              req.TenantID,
		DestinationID:         req.DestinationID,
		WorkerRef:             req.WorkerRef,
		BeforeDigest:          req.BeforeDigest,
		AfterDigest:           req.AfterDigest,
		RequestedBy:           req.RequestedBy,
		Approver:              req.Approver,
		ContactEndpointID:     endpoint.EndpointID,
		ContactRevisionDigest: endpoint.CanonicalDigest,
		RequestedAt:           when,
		CoolingOff:            req.CoolingOff,
		Status:                ChangeAwaitingConfirmation,
		Revision:              1,
	}
	change.CanonicalDigest = change.digest()
	message := ChangeConfirmation{
		Operation:              change.Operation,
		ChangeDigest:           change.CanonicalDigest,
		TenantID:               change.TenantID,
		DestinationID:          change.DestinationID,
		EndpointID:             change.ContactEndpointID,
		EndpointRevisionDigest: change.ContactRevisionDigest,
		BeforeDigest:           change.BeforeDigest,
		AfterDigest:            change.AfterDigest,
		RequestedBy:            change.RequestedBy,
		Approver:               change.Approver,
		DispatchedAt:           when,
	}
	if err := dispatcher.DispatchOutOfBand(message); err != nil {
		return BankDetailChange{}, fieldError(ErrConfirmationRequired, "confirmation", err)
	}
	return change, nil
}

// NewBankDetailChange is the constructor-shaped alias for the operation
// workflow; it still performs SOD evaluation and confirmation dispatch.
func NewBankDetailChange(req BankDetailChangeRequest, source IndependentContactSource, dispatcher ConfirmationDispatcher) (BankDetailChange, error) {
	return StartBankDetailChange(req, source, dispatcher)
}

// Confirm returns the next immutable revision after an out-of-band
// confirmation. The confirmation proof is a digest, never a raw token.
func (c BankDetailChange) Confirm(confirmedAt time.Time, confirmationDigest string) (BankDetailChange, error) {
	if err := c.Validate(); err != nil {
		return BankDetailChange{}, err
	}
	if c.Status != ChangeAwaitingConfirmation {
		return BankDetailChange{}, fieldError(ErrConfirmationRequired, "status", errors.New("change is not awaiting confirmation"))
	}
	if confirmedAt.IsZero() {
		return BankDetailChange{}, fieldError(ErrConfirmationRequired, "confirmed_at", errors.New("confirmation instant is required"))
	}
	if confirmedAt.Before(c.RequestedAt) {
		return BankDetailChange{}, fieldError(ErrConfirmationRequired, "confirmed_at", errors.New("confirmation cannot precede the request"))
	}
	if !validProtectedDigest(confirmationDigest) {
		return BankDetailChange{}, fieldError(ErrConfirmationRequired, "confirmation_digest", errors.New("protected confirmation digest is required"))
	}
	next := c
	next.ConfirmedAt = confirmedAt.UTC()
	next.AvailableAt = next.ConfirmedAt.Add(next.CoolingOff)
	next.Status = ChangeConfirmed
	next.Revision++
	next.SupersedesRevision = c.Revision
	next.SupersedesDigest = c.CanonicalDigest
	next.CanonicalDigest = next.digest()
	return next, nil
}

// ConfirmOutOfBand is the explicit form of Confirm for channel adapters.
func (c BankDetailChange) ConfirmOutOfBand(confirmedAt time.Time, confirmationDigest string) (BankDetailChange, error) {
	return c.Confirm(confirmedAt, confirmationDigest)
}

func (c BankDetailChange) Validate() error {
	if c.Operation != BankDetailChangeOperation {
		return fieldError(ErrInvalidBankDetailChange, "operation", errors.New("named operation is required"))
	}
	for field, value := range map[string]string{
		"id": c.ID, "tenant_id": c.TenantID, "destination_id": c.DestinationID,
		"worker_ref": c.WorkerRef, "requested_by": c.RequestedBy, "approver": c.Approver,
		"contact_endpoint_id": c.ContactEndpointID, "contact_revision_digest": c.ContactRevisionDigest,
	} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fieldError(ErrInvalidBankDetailChange, field, errors.New("required and unpadded"))
		}
	}
	if !validProtectedDigest(c.BeforeDigest) || !validProtectedDigest(c.AfterDigest) || c.BeforeDigest == c.AfterDigest {
		return fieldError(ErrInvalidBankDetailChange, "before_after_digest", errors.New("distinct protected digests are required"))
	}
	if c.Approver == c.RequestedBy {
		return fieldError(ErrDistinctApproverRequired, "approver", ErrDistinctApproverRequired)
	}
	if !validProtectedDigest(c.ContactRevisionDigest) || c.RequestedAt.IsZero() || c.CoolingOff < 0 || c.Revision == 0 {
		return fieldError(ErrInvalidBankDetailChange, "revision", errors.New("revision metadata is invalid"))
	}
	if c.Status != ChangeAwaitingConfirmation && c.Status != ChangeConfirmed {
		return fieldError(ErrInvalidBankDetailChange, "status", errors.New("status is not declared"))
	}
	if c.Revision == 1 && (c.SupersedesRevision != 0 || c.SupersedesDigest != "") {
		return fieldError(ErrInvalidBankDetailChange, "supersedes", errors.New("first revision cannot have a predecessor"))
	}
	if c.Revision > 1 && (c.SupersedesRevision == 0 || !validProtectedDigest(c.SupersedesDigest)) {
		return fieldError(ErrInvalidBankDetailChange, "supersedes", errors.New("successor requires predecessor digest"))
	}
	if c.Status == ChangeConfirmed && (c.ConfirmedAt.IsZero() || c.AvailableAt.IsZero()) {
		return fieldError(ErrInvalidBankDetailChange, "confirmed_at", errors.New("confirmed revision requires availability time"))
	}
	if c.CanonicalDigest == "" || c.CanonicalDigest != c.digest() {
		return fieldError(ErrInvalidBankDetailChange, "canonical_digest", errors.New("digest mismatch"))
	}
	return nil
}

// CanReceivePayment is the release gate for a changed destination.
func (c BankDetailChange) CanReceivePayment(at time.Time) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Status != ChangeConfirmed || at.Before(c.AvailableAt) {
		return ErrChangeNotAvailable
	}
	return nil
}

// ReleaseAllowed is the descriptive alias for CanReceivePayment.
func (c BankDetailChange) ReleaseAllowed(at time.Time) error { return c.CanReceivePayment(at) }

// Explain is safe for audit output: it contains policy state and protected
// digests only, never raw account data or contact values.
func (c BankDetailChange) Explain() string {
	return fmt.Sprintf("bank-detail change operation=%s status=%s revision=%d before=%s after=%s contact=%s cooling_off=%s available=%t",
		c.Operation, c.Status, c.Revision, c.BeforeDigest, c.AfterDigest, c.ContactRevisionDigest, c.CoolingOff, c.Status == ChangeConfirmed)
}

func validateBankDetailRequest(req BankDetailChangeRequest) error {
	for field, value := range map[string]string{
		"id": req.ID, "tenant_id": req.TenantID, "destination_id": req.DestinationID,
		"worker_ref": req.WorkerRef, "requested_by": req.RequestedBy, "approver": req.Approver,
	} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fieldError(ErrInvalidBankDetailChange, field, errors.New("required and unpadded"))
		}
	}
	if !validProtectedDigest(req.BeforeDigest) || !validProtectedDigest(req.AfterDigest) || req.BeforeDigest == req.AfterDigest {
		return fieldError(ErrInvalidBankDetailChange, "before_after_digest", errors.New("distinct protected digests are required"))
	}
	if req.RequestedAt.IsZero() || req.CoolingOff < 0 {
		return fieldError(ErrInvalidBankDetailChange, "requested_at", errors.New("timestamp and non-negative delay are required"))
	}
	if req.Approver == req.RequestedBy {
		return fieldError(ErrDistinctApproverRequired, "approver", ErrDistinctApproverRequired)
	}
	return nil
}

func (c BankDetailChange) digest() string {
	w := canonicalbytes.New("hcmnext.domains.paymethod.BankDetailChange", securityControlSchemaVersion).
		String("operation", c.Operation).String("id", c.ID).String("tenant_id", c.TenantID).
		String("destination_id", c.DestinationID).String("worker_ref", c.WorkerRef).
		String("before_digest", c.BeforeDigest).String("after_digest", c.AfterDigest).
		String("requested_by", c.RequestedBy).String("approver", c.Approver).
		String("contact_endpoint_id", c.ContactEndpointID).String("contact_revision_digest", c.ContactRevisionDigest).
		Int("requested_at", c.RequestedAt.UnixNano()).Int("confirmed_at", c.ConfirmedAt.UnixNano()).
		Int("available_at", c.AvailableAt.UnixNano()).Int("cooling_off", int64(c.CoolingOff)).
		String("status", string(c.Status)).Int("revision", int64(c.Revision)).
		Int("supersedes_revision", int64(c.SupersedesRevision)).String("supersedes_digest", c.SupersedesDigest)
	return canonicalbytes.Digest(mustBytes(w))
}

// AccountStorageMode identifies the only two permitted storage forms for a
// DFI account number: envelope ciphertext or an opaque token.
type AccountStorageMode string

const (
	StorageEnvelopeEncrypted AccountStorageMode = "ENVELOPE_ENCRYPTED"
	StorageTokenized         AccountStorageMode = "TOKENIZED"
)

type ProtectedAccount struct {
	Mode            AccountStorageMode
	OpaqueReference string
	ValueDigest     string
	CanonicalDigest string
}

func NewProtectedAccount(mode AccountStorageMode, opaqueReference, valueDigest string) (ProtectedAccount, error) {
	p := ProtectedAccount{Mode: mode, OpaqueReference: opaqueReference, ValueDigest: valueDigest}
	if err := p.Validate(); err != nil {
		return ProtectedAccount{}, err
	}
	p.CanonicalDigest = p.digest()
	return p, nil
}

func (p ProtectedAccount) Validate() error {
	if p.Mode != StorageEnvelopeEncrypted && p.Mode != StorageTokenized {
		return fieldError(ErrInvalidProtection, "storage_mode", errors.New("envelope encryption or tokenization is required"))
	}
	if strings.TrimSpace(p.OpaqueReference) == "" || strings.TrimSpace(p.OpaqueReference) != p.OpaqueReference {
		return fieldError(ErrInvalidProtection, "opaque_reference", errors.New("opaque reference is required"))
	}
	if !validProtectedDigest(p.ValueDigest) {
		return fieldError(ErrInvalidProtection, "value_digest", errors.New("protected value digest is required"))
	}
	if regexp.MustCompile(`^\d{8,17}$`).MatchString(p.OpaqueReference) {
		return fieldError(ErrInvalidProtection, "opaque_reference", ErrRawBankDetailProhibited)
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.digest() {
		return fieldError(ErrInvalidProtection, "canonical_digest", errors.New("digest mismatch"))
	}
	return nil
}

func (p ProtectedAccount) digest() string {
	w := canonicalbytes.New("hcmnext.domains.paymethod.ProtectedAccount", securityControlSchemaVersion).
		String("mode", string(p.Mode)).String("opaque_reference", p.OpaqueReference).String("value_digest", p.ValueDigest)
	return canonicalbytes.Digest(mustBytes(w))
}

type ValidationResult string

const (
	ValidationPassed  ValidationResult = "PASSED"
	ValidationFailed  ValidationResult = "FAILED"
	ValidationPending ValidationResult = "PENDING"
)

type AccountValidationRecord struct {
	Method           string
	ValidatedAt      time.Time
	Result           ValidationResult
	EvidenceDigest   string
	Revision         uint64
	SupersedesDigest string
	CanonicalDigest  string
}

func NewAccountValidationRecord(method string, validatedAt time.Time, result ValidationResult, evidenceDigest string) (AccountValidationRecord, error) {
	r := AccountValidationRecord{Method: method, ValidatedAt: validatedAt, Result: result, EvidenceDigest: evidenceDigest, Revision: 1}
	if err := r.Validate(); err != nil {
		return AccountValidationRecord{}, err
	}
	r.CanonicalDigest = r.digest()
	return r, nil
}

// NewValidationRecord is the concise constructor alias.
func NewValidationRecord(method string, validatedAt time.Time, result ValidationResult, evidenceDigest string) (AccountValidationRecord, error) {
	return NewAccountValidationRecord(method, validatedAt, result, evidenceDigest)
}

func (r AccountValidationRecord) Validate() error {
	if strings.TrimSpace(r.Method) == "" || strings.TrimSpace(r.Method) != r.Method {
		return fieldError(ErrInvalidValidation, "method", errors.New("validation method is required"))
	}
	if r.ValidatedAt.IsZero() {
		return fieldError(ErrInvalidValidation, "validated_at", errors.New("validation instant is required"))
	}
	if r.Result != ValidationPassed && r.Result != ValidationFailed && r.Result != ValidationPending {
		return fieldError(ErrInvalidValidation, "result", errors.New("validation result is not declared"))
	}
	if !validProtectedDigest(r.EvidenceDigest) || r.Revision == 0 {
		return fieldError(ErrInvalidValidation, "evidence_digest", errors.New("evidence digest and revision are required"))
	}
	if r.Revision > 1 && !validProtectedDigest(r.SupersedesDigest) {
		return fieldError(ErrInvalidValidation, "supersedes_digest", errors.New("successor requires predecessor digest"))
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.digest() {
		return fieldError(ErrInvalidValidation, "canonical_digest", errors.New("digest mismatch"))
	}
	return nil
}

type ValidationException struct {
	ApprovedBy      string
	ApprovedAt      time.Time
	ReasonDigest    string
	CanonicalDigest string
}

func (e ValidationException) Validate() error {
	if strings.TrimSpace(e.ApprovedBy) == "" || e.ApprovedAt.IsZero() || !validProtectedDigest(e.ReasonDigest) {
		return fieldError(ErrExceptionRequired, "exception", errors.New("approver, instant and reason digest are required"))
	}
	if e.CanonicalDigest != "" && e.CanonicalDigest != e.digest() {
		return fieldError(ErrExceptionRequired, "exception_digest", errors.New("digest mismatch"))
	}
	return nil
}

func (e ValidationException) digest() string {
	w := canonicalbytes.New("hcmnext.domains.paymethod.ValidationException", securityControlSchemaVersion).
		String("approved_by", e.ApprovedBy).Int("approved_at", e.ApprovedAt.UnixNano()).String("reason_digest", e.ReasonDigest)
	return canonicalbytes.Digest(mustBytes(w))
}

// ReleaseBinding is the payment-release aggregate. It binds a destination to
// one immutable validation record and one protected account reference.
type ReleaseBinding struct {
	Destination     Destination
	Account         ProtectedAccount
	Validation      AccountValidationRecord
	Exception       *ValidationException
	CanonicalDigest string
}

func NewReleaseBinding(destination Destination, account ProtectedAccount, validation AccountValidationRecord, exception *ValidationException) (ReleaseBinding, error) {
	if err := destination.Validate(); err != nil {
		return ReleaseBinding{}, err
	}
	if destination.Rail != RailACH {
		return ReleaseBinding{}, fieldError(ErrInvalidProtection, "rail", errors.New("account protection binding applies to ACH destinations"))
	}
	if err := account.Validate(); err != nil {
		return ReleaseBinding{}, err
	}
	if err := validation.Validate(); err != nil {
		return ReleaseBinding{}, err
	}
	if exception != nil {
		if err := exception.Validate(); err != nil {
			return ReleaseBinding{}, err
		}
	}
	b := ReleaseBinding{Destination: destination, Account: account, Validation: validation, Exception: cloneException(exception)}
	b.CanonicalDigest = b.digest()
	return b, nil
}

func (b ReleaseBinding) CanRelease() error {
	if err := b.Validate(); err != nil {
		return err
	}
	if b.Validation.Result == ValidationPassed {
		return nil
	}
	if b.Exception == nil {
		return ErrValidationRequired
	}
	return nil
}

func (b ReleaseBinding) Validate() error {
	if err := b.Destination.Validate(); err != nil {
		return err
	}
	if err := b.Account.Validate(); err != nil {
		return err
	}
	if err := b.Validation.Validate(); err != nil {
		return err
	}
	if b.Exception != nil {
		if err := b.Exception.Validate(); err != nil {
			return err
		}
	}
	if b.CanonicalDigest == "" || b.CanonicalDigest != b.digest() {
		return fieldError(ErrInvalidProtection, "canonical_digest", errors.New("digest mismatch"))
	}
	return nil
}

// NewAccountNumberInspector wires the real DLP detector constructor to the
// payment boundary. The detector stores locations and classes, never matches.
func NewAccountNumberInspector() (*dlp.Inspector, error) {
	detector, err := dlp.NewDetector("dfi-account-number", dlp.ClassBank, dlp.SeverityCritical, `\b[0-9]{8,17}\b`)
	if err != nil {
		return nil, err
	}
	return dlp.NewInspector(detector)
}

// InspectEgress refuses payloads containing an account-number-shaped value;
// callers must send the protected reference instead.
func InspectEgress(inspector *dlp.Inspector, payload []byte) (dlp.Inspection, error) {
	if inspector == nil {
		return dlp.Inspection{}, fieldError(ErrAccountNumberEgress, "payload", errors.New("DLP inspector is required"))
	}
	inspection, err := inspector.Inspect(payload)
	if err != nil {
		return dlp.Inspection{}, err
	}
	if len(inspection.Findings) > 0 {
		return inspection, fieldError(ErrAccountNumberEgress, "payload", ErrRawBankDetailProhibited)
	}
	return inspection, nil
}

func (r AccountValidationRecord) digest() string {
	w := canonicalbytes.New("hcmnext.domains.paymethod.AccountValidationRecord", securityControlSchemaVersion).
		String("method", r.Method).Int("validated_at", r.ValidatedAt.UnixNano()).String("result", string(r.Result)).
		String("evidence_digest", r.EvidenceDigest).Int("revision", int64(r.Revision)).String("supersedes_digest", r.SupersedesDigest)
	return canonicalbytes.Digest(mustBytes(w))
}

func (b ReleaseBinding) digest() string {
	w := canonicalbytes.New("hcmnext.domains.paymethod.ReleaseBinding", securityControlSchemaVersion).
		String("destination_digest", b.Destination.CanonicalDigest).String("account_digest", b.Account.CanonicalDigest).
		String("validation_digest", b.Validation.CanonicalDigest)
	if b.Exception == nil {
		w.String("exception_digest", "")
	} else {
		w.String("exception_digest", b.Exception.digest())
	}
	return canonicalbytes.Digest(mustBytes(w))
}

func cloneException(in *ValidationException) *ValidationException {
	if in == nil {
		return nil
	}
	copy := *in
	if copy.CanonicalDigest == "" {
		copy.CanonicalDigest = copy.digest()
	}
	return &copy
}

func validProtectedDigest(value string) bool {
	return len(value) == len("sha256:")+64 && strings.HasPrefix(value, "sha256:")
}

func mustBytes(w *canonicalbytes.Writer) []byte {
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
