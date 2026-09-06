package commercial

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// InvoiceStatus is the status copied from an external or manually issued
// invoice. This package records the status; it does not generate, void, or
// collect invoices.
type InvoiceStatus string

const (
	InvoiceIssued InvoiceStatus = "ISSUED"
	InvoicePaid   InvoiceStatus = "PAID"
	InvoiceVoided InvoiceStatus = "VOIDED"
)

// ReconciliationState says whether the invoice evidence has been matched to
// the immutable pilot contract. It is deliberately separate from payment
// status because an unpaid invoice can still be reconciled.
type ReconciliationState string

const (
	ReconciliationPending    ReconciliationState = "PENDING"
	ReconciliationReconciled ReconciliationState = "RECONCILED"
	ReconciliationException  ReconciliationState = "EXCEPTION"
)

const invoiceArtifactHashAlgorithm = "SHA-256-TENANT-BOUND-V1"

var (
	ErrInvalidInvoiceEvidence = errors.New("commercial: invalid invoice evidence")
	ErrInvoiceAmountMismatch  = errors.New("commercial: invoice amount does not match contract")
	ErrInvoiceTenantMismatch  = errors.New("commercial: invoice tenant does not match contract")
)

// InvoiceEvidenceRequest contains facts supplied by the external billing
// system or by a human operator. Artifact is hashed and discarded; raw
// invoice bytes never become part of the commercial kernel's record.
type InvoiceEvidenceRequest struct {
	TenantID          string
	ExternalInvoiceID string
	AmountCents       int64
	Currency          string
	ServiceFrom       time.Time
	ServiceTo         time.Time
	IssuedAt          time.Time
	DueAt             time.Time
	Status            InvoiceStatus
	Reconciliation    ReconciliationState
	Artifact          []byte
	RecordedAt        time.Time
}

// InvoiceEvidence is an immutable, tenant-scoped receipt for an external or
// manual pilot invoice. It contains no invoice payload and is append-only in
// InvoiceEvidenceStore.
type InvoiceEvidence struct {
	RecordID              string              `json:"record_id"`
	TenantID              string              `json:"tenant_id"`
	ContractID            string              `json:"contract_id"`
	ContractFingerprint   string              `json:"contract_fingerprint"`
	ExternalInvoiceID     string              `json:"external_invoice_id"`
	AmountCents           int64               `json:"amount_cents"`
	Currency              string              `json:"currency"`
	ServiceFrom           time.Time           `json:"service_from"`
	ServiceTo             time.Time           `json:"service_to"`
	IssuedAt              time.Time           `json:"issued_at"`
	DueAt                 time.Time           `json:"due_at"`
	Status                InvoiceStatus       `json:"status"`
	ArtifactHash          string              `json:"artifact_hash"`
	ArtifactHashAlgorithm string              `json:"artifact_hash_algorithm"`
	Reconciliation        ReconciliationState `json:"reconciliation_state"`
	RecordedAt            time.Time           `json:"recorded_at"`
}

// ArtifactHashForTenant creates a digest that cannot be replayed as the
// artifact identity of another tenant. The tenant is part of the signed
// hashing domain, while the artifact bytes themselves are not retained.
func ArtifactHashForTenant(tenantID string, artifact []byte) string {
	if strings.TrimSpace(tenantID) == "" || len(artifact) == 0 {
		return ""
	}
	h := sha256.New()
	_, _ = h.Write([]byte("hcmnext.commercial.invoice-artifact/v1\x00"))
	_, _ = h.Write([]byte(tenantID))
	_, _ = h.Write([]byte("\x00"))
	_, _ = h.Write(artifact)
	return hex.EncodeToString(h.Sum(nil))
}

// VerifyArtifactHash checks a tenant-bound artifact digest without exposing
// the artifact through InvoiceEvidence.
func VerifyArtifactHash(tenantID string, artifact []byte, digest string) bool {
	return digest != "" && ArtifactHashForTenant(tenantID, artifact) == digest
}

func (r InvoiceEvidence) Validate() error {
	if !validReference(r.TenantID) || !validReference(r.ContractID) || len(r.ContractFingerprint) != sha256.Size*2 || !validHex(r.ContractFingerprint) {
		return fmt.Errorf("%w: identity", ErrInvalidInvoiceEvidence)
	}
	if !validReference(r.ExternalInvoiceID) || r.AmountCents <= 0 || !validReference(r.Currency) {
		return fmt.Errorf("%w: invoice terms", ErrInvalidInvoiceEvidence)
	}
	if r.ServiceFrom.IsZero() || r.ServiceTo.IsZero() || !r.ServiceTo.After(r.ServiceFrom) || r.IssuedAt.IsZero() || r.DueAt.IsZero() || r.DueAt.Before(r.IssuedAt) {
		return fmt.Errorf("%w: dates", ErrInvalidInvoiceEvidence)
	}
	if r.Status != InvoiceIssued && r.Status != InvoicePaid && r.Status != InvoiceVoided {
		return fmt.Errorf("%w: status", ErrInvalidInvoiceEvidence)
	}
	if r.ArtifactHashAlgorithm != invoiceArtifactHashAlgorithm || len(r.ArtifactHash) != sha256.Size*2 || !validHex(r.ArtifactHash) {
		return fmt.Errorf("%w: tenant-bound artifact hash", ErrInvalidInvoiceEvidence)
	}
	if r.Reconciliation != ReconciliationPending && r.Reconciliation != ReconciliationReconciled && r.Reconciliation != ReconciliationException {
		return fmt.Errorf("%w: reconciliation state", ErrInvalidInvoiceEvidence)
	}
	if r.RecordedAt.IsZero() {
		return fmt.Errorf("%w: recorded_at", ErrInvalidInvoiceEvidence)
	}
	return nil
}

func validHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func (r InvoiceEvidence) Explain() string {
	return fmt.Sprintf("invoice evidence tenant=%s contract=%s external_ref=%s amount_cents=%d currency=%s status=%s reconciliation=%s artifact_hash=%s",
		r.TenantID, r.ContractID, r.ExternalInvoiceID, r.AmountCents, r.Currency, r.Status, r.Reconciliation, r.ArtifactHash)
}

// InvoiceEvidenceStore is a process-local append-only port for the kernel.
// Production persistence can implement the same validation and idempotency
// contract without moving database concerns into internal/commercial.
type InvoiceEvidenceStore struct {
	mu      sync.Mutex
	records []InvoiceEvidence
	byKey   map[string]InvoiceEvidence
}

func NewInvoiceEvidenceStore() *InvoiceEvidenceStore {
	return &InvoiceEvidenceStore{byKey: make(map[string]InvoiceEvidence)}
}

// Record validates and appends one invoice receipt. The bool is false when
// the tenant/external-reference replay returns the original immutable record.
func (s *InvoiceEvidenceStore) Record(snapshot EntitlementSnapshot, req InvoiceEvidenceRequest) (InvoiceEvidence, bool, error) {
	if s == nil {
		return InvoiceEvidence{}, false, fmt.Errorf("%w: store is required", ErrInvalidInvoiceEvidence)
	}
	if err := snapshot.Validate(); err != nil {
		return InvoiceEvidence{}, false, err
	}
	if req.TenantID != snapshot.TenantID() {
		return InvoiceEvidence{}, false, ErrInvoiceTenantMismatch
	}
	if req.AmountCents != snapshot.Contract().PriceCents || req.Currency != snapshot.Contract().Currency {
		return InvoiceEvidence{}, false, ErrInvoiceAmountMismatch
	}
	if !validReference(req.TenantID) || !validReference(req.ExternalInvoiceID) || req.AmountCents <= 0 || !validReference(req.Currency) {
		return InvoiceEvidence{}, false, fmt.Errorf("%w: request terms", ErrInvalidInvoiceEvidence)
	}
	if len(req.Artifact) == 0 {
		return InvoiceEvidence{}, false, fmt.Errorf("%w: artifact is required", ErrInvalidInvoiceEvidence)
	}
	recordedAt := req.RecordedAt
	if recordedAt.IsZero() {
		recordedAt = time.Now().UTC()
	}
	evidence := InvoiceEvidence{
		RecordID:              invoiceRecordID(req.TenantID, req.ExternalInvoiceID),
		TenantID:              req.TenantID,
		ContractID:            snapshot.ContractID(),
		ContractFingerprint:   snapshot.Fingerprint(),
		ExternalInvoiceID:     req.ExternalInvoiceID,
		AmountCents:           req.AmountCents,
		Currency:              req.Currency,
		ServiceFrom:           req.ServiceFrom.UTC(),
		ServiceTo:             req.ServiceTo.UTC(),
		IssuedAt:              req.IssuedAt.UTC(),
		DueAt:                 req.DueAt.UTC(),
		Status:                req.Status,
		ArtifactHash:          ArtifactHashForTenant(req.TenantID, req.Artifact),
		ArtifactHashAlgorithm: invoiceArtifactHashAlgorithm,
		Reconciliation:        req.Reconciliation,
		RecordedAt:            recordedAt.UTC(),
	}
	if err := evidence.Validate(); err != nil {
		return InvoiceEvidence{}, false, err
	}
	key := req.TenantID + "\x00" + req.ExternalInvoiceID
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.byKey[key]; ok {
		return existing, false, nil
	}
	s.byKey[key] = evidence
	s.records = append(s.records, evidence)
	return evidence, true, nil
}

// List returns only the immutable invoice history belonging to tenantID.
func (s *InvoiceEvidenceStore) List(tenantID string) []InvoiceEvidence {
	if s == nil || !validReference(tenantID) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]InvoiceEvidence, 0, len(s.records))
	for _, record := range s.records {
		if record.TenantID == tenantID {
			result = append(result, record)
		}
	}
	return result
}

func invoiceRecordID(tenantID, externalID string) string {
	h := sha256.Sum256([]byte("hcmnext.commercial.invoice-evidence/v1\x00" + tenantID + "\x00" + externalID))
	return hex.EncodeToString(h[:])
}
