package configbundle

import (
	"crypto/ed25519"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

// ValidationResult is the consumer's result after validating the desired
// state locally. A receipt with any result is useful evidence, but only VALID
// receipts advance an adoption watermark.
type ValidationResult string

const (
	ValidationValid    ValidationResult = "VALID"
	ValidationInvalid  ValidationResult = "INVALID"
	ValidationDegraded ValidationResult = "DEGRADED"
)

// ApplicationReceipt is immutable, signed evidence from one service and cell.
// DesiredDigest and AppliedDigest are both retained so a stale or partial
// application cannot be mistaken for adoption of the desired state.
type ApplicationReceipt struct {
	TenantID       string           `json:"tenant_id"`
	CellID         string           `json:"cell_id"`
	Service        string           `json:"service"`
	Build          string           `json:"build"`
	DesiredDigest  string           `json:"desired_digest"`
	AppliedDigest  string           `json:"applied_digest"`
	Epoch          uint64           `json:"epoch"`
	Validation     ValidationResult `json:"validation"`
	ValidationCode string           `json:"validation_code,omitempty"`
	AppliedAt      time.Time        `json:"applied_at"`
	Digest         string           `json:"digest"`
	Signature      BundleSignature  `json:"signature"`
}

// ApplicationReceiptRecord is a concise compatibility spelling for
// ApplicationReceipt.
type ApplicationReceiptRecord = ApplicationReceipt

// Explain returns only adoption facts, never signature bytes or configuration
// values.
func (r ApplicationReceipt) Explain() string {
	return "application receipt service=" + r.Service + " build=" + r.Build +
		" tenant=" + r.TenantID + " cell=" + r.CellID +
		" epoch=" + formatUint(r.Epoch) + " validation=" + string(r.Validation)
}

func (r ApplicationReceipt) canonical() (*canonicalbytes.Writer, error) {
	if err := validateApplicationReceipt(r); err != nil {
		return nil, err
	}
	return canonicalbytes.New("hcmnext.platform.configbundle.ApplicationReceipt", 1).
		String("tenant_id", r.TenantID).
		String("cell_id", r.CellID).
		String("service", r.Service).
		String("build", r.Build).
		String("desired_digest", r.DesiredDigest).
		String("applied_digest", r.AppliedDigest).
		Int("epoch", int64(r.Epoch)).
		String("validation", string(r.Validation)).
		String("validation_code", r.ValidationCode).
		String("applied_at", r.AppliedAt.UTC().Format(time.RFC3339Nano)), nil
}

// DigestValue computes the receipt digest without trusting Digest or
// Signature.
func (r ApplicationReceipt) DigestValue() (string, error) {
	w, err := r.canonical()
	if err != nil {
		return "", err
	}
	return w.Digest()
}

// Verify checks the canonical receipt digest and detached Ed25519 signature.
func (r ApplicationReceipt) Verify(publicKey ed25519.PublicKey) error {
	if err := validateSignatureIdentity(r.Signature); err != nil {
		return applicationReceiptRefusal("INVALID_RECEIPT_SIGNATURE", "signature", "receipt signature identity is invalid", err)
	}
	digest, err := r.DigestValue()
	if err != nil {
		return err
	}
	if digest != r.Digest {
		return applicationReceiptRefusal("RECEIPT_MUTATED", "digest", "recorded receipt digest differs from canonical receipt", ErrApplicationReceiptInvalid)
	}
	raw, err := digestBytes(r.Digest)
	if err != nil {
		return err
	}
	signature, err := decodeSignature(r.Signature.Value)
	if err != nil || !ed25519.Verify(publicKey, raw, signature) {
		return applicationReceiptRefusal("INVALID_RECEIPT_SIGNATURE", "signature.value", "receipt signature verification failed", ErrApplicationReceiptInvalid)
	}
	return nil
}

type receiptKey struct {
	tenant  string
	cell    string
	service string
	epoch   uint64
}

// ReceiptStore is the provider-neutral, in-memory receipt port. A database
// adapter can persist the same values without changing the signing or
// watermark semantics.
type ReceiptStore struct {
	signer ReceiptSigner
	now    func() time.Time

	mu       sync.RWMutex
	receipts map[receiptKey]ApplicationReceipt
}

// NewReceiptStore creates a receipt store. Receipt signing is mandatory.
func NewReceiptStore(signer ReceiptSigner, clocks ...func() time.Time) *ReceiptStore {
	now := func() time.Time { return time.Now().UTC() }
	if len(clocks) > 0 && clocks[0] != nil {
		now = clocks[0]
	}
	return &ReceiptStore{signer: signer, now: now, receipts: make(map[receiptKey]ApplicationReceipt)}
}

// NewApplicationReceiptStore is the explicit constructor spelling used by
// callers that keep activation receipts and application receipts separate.
func NewApplicationReceiptStore(signer ReceiptSigner, clocks ...func() time.Time) *ReceiptStore {
	return NewReceiptStore(signer, clocks...)
}

// Record signs and records an application receipt. Replaying the exact same
// service/epoch is idempotent; a different receipt at that identity is a
// conflict and does not alter the store.
func (s *ReceiptStore) Record(receipt ApplicationReceipt) (ApplicationReceipt, error) {
	if s == nil {
		return ApplicationReceipt{}, applicationReceiptRefusal("NIL_RECEIPT_STORE", "store", "receipt store is nil", ErrApplicationReceiptInvalid)
	}
	if s.signer == nil {
		return ApplicationReceipt{}, applicationReceiptRefusal("MISSING_RECEIPT_SIGNER", "signature", "a receipt signer is required", ErrReceiptSignerRequired)
	}
	if receipt.AppliedAt.IsZero() {
		receipt.AppliedAt = s.now().UTC()
	} else {
		receipt.AppliedAt = receipt.AppliedAt.UTC()
	}
	if err := validateApplicationReceipt(receipt); err != nil {
		return ApplicationReceipt{}, err
	}
	digest, err := receipt.DigestValue()
	if err != nil {
		return ApplicationReceipt{}, err
	}
	receipt.Digest = digest
	signature, err := s.signer.SignDigest(receipt.Digest)
	if err != nil {
		return ApplicationReceipt{}, applicationReceiptRefusal("RECEIPT_SIGNING_FAILED", "signature", "receipt signing failed", err)
	}
	receipt.Signature = signature
	key := receiptKey{tenant: receipt.TenantID, cell: receipt.CellID, service: receipt.Service, epoch: receipt.Epoch}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.receipts[key]; ok {
		if prior.Digest == receipt.Digest {
			return cloneApplicationReceipt(prior), nil
		}
		return ApplicationReceipt{}, applicationReceiptRefusal("RECEIPT_CONFLICT", "epoch", "service already recorded a different receipt for this epoch", ErrApplicationReceiptConflict)
	}
	s.receipts[key] = cloneApplicationReceipt(receipt)
	return cloneApplicationReceipt(receipt), nil
}

// RecordApplication is the explicit operation spelling for Record.
func (s *ReceiptStore) RecordApplication(receipt ApplicationReceipt) (ApplicationReceipt, error) {
	return s.Record(receipt)
}

// Get returns a defensive copy of a receipt for the exact consumer identity.
func (s *ReceiptStore) Get(tenantID, cellID, service string, epoch uint64) (ApplicationReceipt, bool) {
	if s == nil {
		return ApplicationReceipt{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	receipt, ok := s.receipts[receiptKey{tenant: tenantID, cell: cellID, service: service, epoch: epoch}]
	return cloneApplicationReceipt(receipt), ok
}

// List returns deterministic receipts for one tenant/cell.
func (s *ReceiptStore) List(tenantID, cellID string) []ApplicationReceipt {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ApplicationReceipt, 0)
	for key, receipt := range s.receipts {
		if key.tenant == tenantID && key.cell == cellID {
			out = append(out, cloneApplicationReceipt(receipt))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Epoch != out[j].Epoch {
			return out[i].Epoch < out[j].Epoch
		}
		return out[i].Service < out[j].Service
	})
	return out
}

// AdoptionStatus describes whether every required consumer adopted one exact
// desired digest at one epoch.
type AdoptionStatus string

const (
	AdoptionComplete AdoptionStatus = "ADOPTED"
	AdoptionPending  AdoptionStatus = "PENDING"
)

// AdoptionWatermark is the deterministic aggregate used by rollout and
// schema-retirement decisions.
type AdoptionWatermark struct {
	TenantID      string         `json:"tenant_id"`
	CellID        string         `json:"cell_id"`
	DesiredDigest string         `json:"desired_digest"`
	Epoch         uint64         `json:"epoch"`
	Required      []string       `json:"required"`
	Adopted       []string       `json:"adopted"`
	Missing       []string       `json:"missing"`
	Stale         []string       `json:"stale"`
	Invalid       []string       `json:"invalid"`
	Status        AdoptionStatus `json:"status"`
	Digest        string         `json:"digest"`
}

// Watermark aggregates exact application receipts. Missing, stale, or
// invalid consumers keep the watermark pending.
func (s *ReceiptStore) Watermark(tenantID, cellID, desiredDigest string, epoch uint64, required []string) (AdoptionWatermark, error) {
	if s == nil {
		return AdoptionWatermark{}, applicationReceiptRefusal("NIL_RECEIPT_STORE", "store", "receipt store is nil", ErrApplicationReceiptInvalid)
	}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(cellID) == "" || epoch == 0 {
		return AdoptionWatermark{}, applicationReceiptRefusal("INVALID_WATERMARK_SCOPE", "scope", "tenant, cell, and positive epoch are required", ErrApplicationReceiptInvalid)
	}
	if _, err := digestBytes(desiredDigest); err != nil {
		return AdoptionWatermark{}, applicationReceiptRefusal("INVALID_DESIRED_DIGEST", "desired_digest", "desired digest must be sha256:hex", err)
	}
	services := append([]string(nil), required...)
	sort.Strings(services)
	if len(services) == 0 {
		return AdoptionWatermark{}, applicationReceiptRefusal("INVALID_REQUIRED_SERVICES", "required", "at least one required service is needed", ErrApplicationReceiptInvalid)
	}
	for i, service := range services {
		if strings.TrimSpace(service) == "" || (i > 0 && services[i-1] == service) {
			return AdoptionWatermark{}, applicationReceiptRefusal("INVALID_REQUIRED_SERVICES", "required", "required services must be non-empty and unique", ErrApplicationReceiptInvalid)
		}
	}
	watermark := AdoptionWatermark{TenantID: tenantID, CellID: cellID, DesiredDigest: desiredDigest, Epoch: epoch, Required: services, Status: AdoptionPending}
	s.mu.RLock()
	for _, service := range services {
		receipt, exact := s.receipts[receiptKey{tenant: tenantID, cell: cellID, service: service, epoch: epoch}]
		if !exact {
			if s.hasOtherEpochLocked(tenantID, cellID, service, epoch) {
				watermark.Stale = append(watermark.Stale, service)
			} else {
				watermark.Missing = append(watermark.Missing, service)
			}
			continue
		}
		if receipt.DesiredDigest != desiredDigest || receipt.AppliedDigest != desiredDigest || receipt.Validation != ValidationValid {
			watermark.Invalid = append(watermark.Invalid, service)
			continue
		}
		watermark.Adopted = append(watermark.Adopted, service)
	}
	s.mu.RUnlock()
	if len(watermark.Adopted) == len(watermark.Required) {
		watermark.Status = AdoptionComplete
	}
	digest, err := watermark.digestValue()
	if err != nil {
		return AdoptionWatermark{}, err
	}
	watermark.Digest = digest
	return watermark, nil
}

// AdoptionWatermark is an explicit alias for Watermark for callers that use
// the noun from the control-plane contract.
func (s *ReceiptStore) AdoptionWatermark(tenantID, cellID, desiredDigest string, epoch uint64, required []string) (AdoptionWatermark, error) {
	return s.Watermark(tenantID, cellID, desiredDigest, epoch, required)
}

func (s *ReceiptStore) hasOtherEpochLocked(tenantID, cellID, service string, epoch uint64) bool {
	for key := range s.receipts {
		if key.tenant == tenantID && key.cell == cellID && key.service == service && key.epoch != epoch {
			return true
		}
	}
	return false
}

func (w AdoptionWatermark) digestValue() (string, error) {
	c := canonicalbytes.New("hcmnext.platform.configbundle.AdoptionWatermark", 1).
		String("tenant_id", w.TenantID).
		String("cell_id", w.CellID).
		String("desired_digest", w.DesiredDigest).
		Int("epoch", int64(w.Epoch)).
		SortedStrings("required", w.Required).
		SortedStrings("adopted", w.Adopted).
		SortedStrings("missing", w.Missing).
		SortedStrings("stale", w.Stale).
		SortedStrings("invalid", w.Invalid).
		String("status", string(w.Status))
	return c.Digest()
}

// DigestValue computes the aggregate watermark digest without trusting Digest.
func (w AdoptionWatermark) DigestValue() (string, error) { return w.digestValue() }

// Explain returns the adoption facts used by operators and rollout policy.
func (w AdoptionWatermark) Explain() string {
	return "adoption watermark tenant=" + w.TenantID + " cell=" + w.CellID +
		" epoch=" + formatUint(w.Epoch) + " status=" + string(w.Status)
}

func validateApplicationReceipt(r ApplicationReceipt) error {
	if strings.TrimSpace(r.TenantID) == "" || strings.TrimSpace(r.CellID) == "" || strings.TrimSpace(r.Service) == "" || strings.TrimSpace(r.Build) == "" {
		return applicationReceiptRefusal("INVALID_RECEIPT_IDENTITY", "identity", "tenant, cell, service, and build are required", ErrApplicationReceiptInvalid)
	}
	if r.Epoch == 0 || r.AppliedAt.IsZero() {
		return applicationReceiptRefusal("INVALID_RECEIPT_TIME", "epoch", "positive epoch and applied time are required", ErrApplicationReceiptInvalid)
	}
	if _, err := digestBytes(r.DesiredDigest); err != nil {
		return applicationReceiptRefusal("INVALID_DESIRED_DIGEST", "desired_digest", "desired digest must be sha256:hex", err)
	}
	if _, err := digestBytes(r.AppliedDigest); err != nil {
		return applicationReceiptRefusal("INVALID_APPLIED_DIGEST", "applied_digest", "applied digest must be sha256:hex", err)
	}
	switch r.Validation {
	case ValidationValid, ValidationInvalid, ValidationDegraded:
		return nil
	default:
		return applicationReceiptRefusal("INVALID_VALIDATION_RESULT", "validation", "validation must be VALID, INVALID, or DEGRADED", ErrApplicationReceiptInvalid)
	}
}

func cloneApplicationReceipt(r ApplicationReceipt) ApplicationReceipt { return r }

func applicationReceiptRefusal(code, field, detail string, cause error) error {
	return &Error{Code: code, Ref: field, Detail: detail, Cause: cause}
}

func formatUint(value uint64) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}

var (
	ErrApplicationReceiptInvalid  = errors.New("configbundle: application receipt is invalid")
	ErrApplicationReceiptConflict = errors.New("configbundle: application receipt conflicts with an existing receipt")
)
