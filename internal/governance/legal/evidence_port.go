package legal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
)

// Evidence storage errors are stable semantic classifications shared by the
// kernel-friendly in-memory implementation and durable adapters.
var (
	ErrEvidenceInvalid   = errors.New("legal: invalid evidence entry")
	ErrEvidenceDuplicate = errors.New("legal: duplicate evidence entry")
)

// ReviewRecordEntry adds the tenant and append-only identity that the
// persistence boundary needs to a domain ReviewRecord.
type ReviewRecordEntry struct {
	TenantID      string
	RowID         string
	EventSequence int64
	Record        ReviewRecord
	Digest        string
}

// Validate checks the invariants that do not depend on PostgreSQL.
func (e ReviewRecordEntry) Validate() error {
	if e.TenantID == "" || e.RowID == "" || e.EventSequence < 1 {
		return fmt.Errorf("%w: tenant, row and positive event sequence are required", ErrEvidenceInvalid)
	}
	if err := e.Record.Validate(); err != nil {
		return fmt.Errorf("%w: review record: %v", ErrEvidenceInvalid, err)
	}
	if e.Digest == "" || e.Digest != e.Record.ComputeDigest() {
		return fmt.Errorf("%w: review digest does not match the record", ErrEvidenceInvalid)
	}
	return nil
}

// CompositionReceiptEntry adds the tenant and append-only identity that the
// persistence boundary needs to a domain CompositionReceipt.
type CompositionReceiptEntry struct {
	TenantID      string
	RowID         string
	EventSequence int64
	Receipt       CompositionReceipt
	Digest        string
}

// Validate checks the receipt identity and digest before storage.
func (e CompositionReceiptEntry) Validate() error {
	if e.TenantID == "" || e.RowID == "" || e.EventSequence < 1 {
		return fmt.Errorf("%w: tenant, row and positive event sequence are required", ErrEvidenceInvalid)
	}
	if e.Digest == "" || e.Digest != e.Receipt.Digest {
		return fmt.Errorf("%w: composition receipt digest is missing or mismatched", ErrEvidenceInvalid)
	}
	return nil
}

// ReviewRecordStore is the semantic port for immutable review evidence.
type ReviewRecordStore interface {
	AppendReviewRecord(context.Context, ReviewRecordEntry) (ReviewRecordEntry, error)
	ListReviewRecords(context.Context, string, string) ([]ReviewRecordEntry, error)
}

// CompositionReceiptStore is the semantic port for immutable composition
// evidence.
type CompositionReceiptStore interface {
	AppendCompositionReceipt(context.Context, CompositionReceiptEntry) (CompositionReceiptEntry, error)
	ListCompositionReceipts(context.Context, string) ([]CompositionReceiptEntry, error)
}

// MemoryEvidenceStore is a concurrency-safe append-only implementation of the
// two legal evidence ports. It is intended for kernel composition and tests;
// every returned slice and nested domain slice is detached from stored state.
type MemoryEvidenceStore struct {
	mu           sync.RWMutex
	reviews      map[string]ReviewRecordEntry
	receipts     map[string]CompositionReceiptEntry
	reviewOrder  []string
	receiptOrder []string
}

func NewMemoryEvidenceStore() *MemoryEvidenceStore {
	return &MemoryEvidenceStore{
		reviews:  make(map[string]ReviewRecordEntry),
		receipts: make(map[string]CompositionReceiptEntry),
	}
}

func reviewKey(e ReviewRecordEntry) string {
	return e.TenantID + ":" + e.Record.PackDigest + ":" + fmt.Sprint(e.EventSequence)
}

func receiptKey(e CompositionReceiptEntry) string {
	return e.TenantID + ":" + e.Digest
}

func cloneReviewEntry(in ReviewRecordEntry) ReviewRecordEntry {
	in.Record.Findings = slices.Clone(in.Record.Findings)
	return in
}

func cloneReceiptEntry(in CompositionReceiptEntry) CompositionReceiptEntry {
	in.Receipt.Jurisdictions = slices.Clone(in.Receipt.Jurisdictions)
	in.Receipt.Inputs = slices.Clone(in.Receipt.Inputs)
	in.Receipt.Obligations = slices.Clone(in.Receipt.Obligations)
	in.Receipt.Traces = slices.Clone(in.Receipt.Traces)
	in.Receipt.Contradictions = slices.Clone(in.Receipt.Contradictions)
	for i := range in.Receipt.Traces {
		in.Receipt.Traces[i].Inputs = slices.Clone(in.Receipt.Traces[i].Inputs)
	}
	return in
}

func (s *MemoryEvidenceStore) AppendReviewRecord(ctx context.Context, in ReviewRecordEntry) (ReviewRecordEntry, error) {
	if err := ctx.Err(); err != nil {
		return ReviewRecordEntry{}, err
	}
	if err := in.Validate(); err != nil {
		return ReviewRecordEntry{}, err
	}
	if s == nil {
		return ReviewRecordEntry{}, fmt.Errorf("%w: nil store", ErrEvidenceInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := reviewKey(in)
	if _, ok := s.reviews[key]; ok {
		return ReviewRecordEntry{}, fmt.Errorf("%w: review %s", ErrEvidenceDuplicate, key)
	}
	in = cloneReviewEntry(in)
	s.reviews[key] = in
	s.reviewOrder = append(s.reviewOrder, key)
	return cloneReviewEntry(in), nil
}

func (s *MemoryEvidenceStore) ListReviewRecords(ctx context.Context, tenantID string, packDigest string) ([]ReviewRecordEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenantID == "" || packDigest == "" {
		return nil, fmt.Errorf("%w: tenant and pack digest are required", ErrEvidenceInvalid)
	}
	if s == nil {
		return nil, fmt.Errorf("%w: nil store", ErrEvidenceInvalid)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []ReviewRecordEntry
	for _, key := range s.reviewOrder {
		entry, ok := s.reviews[key]
		if ok && entry.TenantID == tenantID && entry.Record.PackDigest == packDigest {
			out = append(out, cloneReviewEntry(entry))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EventSequence < out[j].EventSequence })
	return out, nil
}

func (s *MemoryEvidenceStore) AppendCompositionReceipt(ctx context.Context, in CompositionReceiptEntry) (CompositionReceiptEntry, error) {
	if err := ctx.Err(); err != nil {
		return CompositionReceiptEntry{}, err
	}
	if err := in.Validate(); err != nil {
		return CompositionReceiptEntry{}, err
	}
	if s == nil {
		return CompositionReceiptEntry{}, fmt.Errorf("%w: nil store", ErrEvidenceInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := receiptKey(in)
	if _, ok := s.receipts[key]; ok {
		return CompositionReceiptEntry{}, fmt.Errorf("%w: receipt %s", ErrEvidenceDuplicate, key)
	}
	in = cloneReceiptEntry(in)
	s.receipts[key] = in
	s.receiptOrder = append(s.receiptOrder, key)
	return cloneReceiptEntry(in), nil
}

func (s *MemoryEvidenceStore) ListCompositionReceipts(ctx context.Context, tenantID string) ([]CompositionReceiptEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenantID == "" {
		return nil, fmt.Errorf("%w: tenant is required", ErrEvidenceInvalid)
	}
	if s == nil {
		return nil, fmt.Errorf("%w: nil store", ErrEvidenceInvalid)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []CompositionReceiptEntry
	for _, key := range s.receiptOrder {
		entry, ok := s.receipts[key]
		if ok && entry.TenantID == tenantID {
			out = append(out, cloneReceiptEntry(entry))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EventSequence < out[j].EventSequence })
	return out, nil
}
