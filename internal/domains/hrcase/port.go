package hrcase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Store is the tenant-aware persistence port for the immutable HR case
// revision stream. Implementations append a revision at an explicit event
// sequence; reads never expose a revision from another tenant.
type Store interface {
	AppendRevision(context.Context, string, CaseRevision, uint64) error
	LoadRevision(context.Context, string, string, uint64) (CaseRevision, error)
	Current(context.Context, string, string) (CaseRevision, error)
	ListRevisions(context.Context, string, string) ([]CaseRevision, error)
}

var (
	ErrStoreInvalid   = errors.New("hrcase: invalid store input")
	ErrStoreNotFound  = errors.New("hrcase: stored case revision not found")
	ErrStoreDuplicate = errors.New("hrcase: duplicate revision")
	ErrStoreStaleCAS  = errors.New("hrcase: stale compare-and-swap")
)

// StoreErrorCode is the stable machine-readable classification of a store
// refusal. Adapters return these codes instead of exposing database errors.
type StoreErrorCode string

const (
	StoreInvalidCode   StoreErrorCode = "INVALID"
	StoreNotFoundCode  StoreErrorCode = "NOT_FOUND"
	StoreDuplicateCode StoreErrorCode = "DUPLICATE_REVISION"
	StoreStaleCASCode  StoreErrorCode = "STALE_CAS"
)

// StoreError is a typed persistence refusal. Expected and Actual identify the
// sequence observed by a stale append, without exposing database details.
type StoreError struct {
	Code             StoreErrorCode
	Detail           string
	Expected, Actual uint64
}

func (e *StoreError) Error() string {
	if e == nil {
		return "hrcase: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("hrcase: %s", e.Code)
	}
	return fmt.Sprintf("hrcase: %s: %s", e.Code, e.Detail)
}

func (e *StoreError) Unwrap() error {
	if e == nil {
		return nil
	}
	switch e.Code {
	case StoreInvalidCode:
		return ErrStoreInvalid
	case StoreNotFoundCode:
		return ErrStoreNotFound
	case StoreDuplicateCode:
		return ErrStoreDuplicate
	case StoreStaleCASCode:
		return ErrStoreStaleCAS
	default:
		return nil
	}
}

// MemoryStore is the kernel-pure reference implementation of Store. It is
// useful to domain callers and conformance tests without introducing a
// database dependency into the domain package.
type MemoryStore struct {
	mu        sync.RWMutex
	revisions map[string]map[string]map[uint64]CaseRevision
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore returns an empty in-memory case revision store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{revisions: make(map[string]map[string]map[uint64]CaseRevision)}
}

func storeError(code StoreErrorCode, detail string) *StoreError {
	return &StoreError{Code: code, Detail: detail}
}

func storeStale(expected, actual uint64, detail string) *StoreError {
	return &StoreError{Code: StoreStaleCASCode, Expected: expected, Actual: actual, Detail: detail}
}

func validateStoreContext(ctx context.Context, tenantID string) error {
	if ctx == nil {
		return storeError(StoreInvalidCode, "context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(tenantID) == "" {
		return storeError(StoreInvalidCode, "tenant id is required")
	}
	return nil
}

func normalizeStoredRevision(revision CaseRevision) (CaseRevision, error) {
	if revision.Digest == "" {
		normalized, err := NewCaseRevision(revision)
		if err != nil {
			return CaseRevision{}, storeError(StoreInvalidCode, err.Error())
		}
		return normalized, nil
	}
	if err := revision.Validate(); err != nil {
		return CaseRevision{}, storeError(StoreInvalidCode, err.Error())
	}
	if !revision.Verify() {
		return CaseRevision{}, storeError(StoreInvalidCode, "revision digest does not match its content")
	}
	return cloneRevision(revision), nil
}

// AppendRevision appends one immutable revision. The first revision is
// sequence one; every later append must name the current revision in Previous
// and use the next event sequence.
func (s *MemoryStore) AppendRevision(ctx context.Context, tenantID string, revision CaseRevision, eventSequence uint64) error {
	if s == nil {
		return storeError(StoreInvalidCode, "store is nil")
	}
	if err := validateStoreContext(ctx, tenantID); err != nil {
		return err
	}
	if eventSequence == 0 {
		return storeError(StoreInvalidCode, "event sequence must be positive")
	}
	normalized, err := normalizeStoredRevision(revision)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	byCase := s.revisions[tenantID]
	if byCase == nil {
		byCase = make(map[string]map[uint64]CaseRevision)
		s.revisions[tenantID] = byCase
	}
	bySequence := byCase[normalized.CaseID]
	if bySequence == nil {
		bySequence = make(map[uint64]CaseRevision)
		byCase[normalized.CaseID] = bySequence
	}
	if _, exists := bySequence[eventSequence]; exists {
		return storeError(StoreDuplicateCode, fmt.Sprintf("case %s event sequence %d", normalized.CaseID, eventSequence))
	}

	var current CaseRevision
	var currentSequence uint64
	for sequence, candidate := range bySequence {
		if sequence > currentSequence {
			currentSequence, current = sequence, candidate
		}
		if candidate.Revision == normalized.Revision {
			return storeError(StoreDuplicateCode, fmt.Sprintf("case %s revision %d", normalized.CaseID, normalized.Revision))
		}
	}
	if eventSequence != currentSequence+1 {
		return storeStale(currentSequence+1, eventSequence, fmt.Sprintf("case %s event sequence is not the next sequence", normalized.CaseID))
	}
	if currentSequence == 0 {
		if normalized.Revision != 1 || normalized.Previous != 0 {
			return storeStale(0, normalized.Previous, fmt.Sprintf("case %s must begin at revision one", normalized.CaseID))
		}
	} else {
		if normalized.Revision != current.Revision+1 || normalized.Previous != current.Revision {
			return storeStale(current.Revision, normalized.Previous, fmt.Sprintf("case %s revision does not extend the current revision", normalized.CaseID))
		}
	}
	bySequence[eventSequence] = cloneRevision(normalized)
	return nil
}

// LoadRevision returns one immutable revision by event sequence.
func (s *MemoryStore) LoadRevision(ctx context.Context, tenantID, caseID string, eventSequence uint64) (CaseRevision, error) {
	if s == nil {
		return CaseRevision{}, storeError(StoreInvalidCode, "store is nil")
	}
	if err := validateStoreContext(ctx, tenantID); err != nil {
		return CaseRevision{}, err
	}
	if strings.TrimSpace(caseID) == "" || eventSequence == 0 {
		return CaseRevision{}, storeError(StoreInvalidCode, "case id and event sequence are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	revision, ok := s.revisions[tenantID][caseID][eventSequence]
	if !ok {
		return CaseRevision{}, storeError(StoreNotFoundCode, fmt.Sprintf("case %s event sequence %d", caseID, eventSequence))
	}
	return cloneRevision(revision), nil
}

// Current returns the highest event sequence for a case.
func (s *MemoryStore) Current(ctx context.Context, tenantID, caseID string) (CaseRevision, error) {
	if s == nil {
		return CaseRevision{}, storeError(StoreInvalidCode, "store is nil")
	}
	if err := validateStoreContext(ctx, tenantID); err != nil {
		return CaseRevision{}, err
	}
	if strings.TrimSpace(caseID) == "" {
		return CaseRevision{}, storeError(StoreInvalidCode, "case id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	bySequence := s.revisions[tenantID][caseID]
	var current CaseRevision
	var found bool
	var latest uint64
	for sequence, revision := range bySequence {
		if !found || sequence > latest {
			latest, current, found = sequence, revision, true
		}
	}
	if !found {
		return CaseRevision{}, storeError(StoreNotFoundCode, fmt.Sprintf("case %s", caseID))
	}
	return cloneRevision(current), nil
}

// ListRevisions returns the complete immutable stream in event order.
func (s *MemoryStore) ListRevisions(ctx context.Context, tenantID, caseID string) ([]CaseRevision, error) {
	if s == nil {
		return nil, storeError(StoreInvalidCode, "store is nil")
	}
	if err := validateStoreContext(ctx, tenantID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(caseID) == "" {
		return nil, storeError(StoreInvalidCode, "case id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	bySequence := s.revisions[tenantID][caseID]
	if len(bySequence) == 0 {
		return nil, storeError(StoreNotFoundCode, fmt.Sprintf("case %s", caseID))
	}
	sequences := make([]uint64, 0, len(bySequence))
	for sequence := range bySequence {
		sequences = append(sequences, sequence)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	out := make([]CaseRevision, 0, len(sequences))
	for _, sequence := range sequences {
		out = append(out, cloneRevision(bySequence[sequence]))
	}
	return out, nil
}
