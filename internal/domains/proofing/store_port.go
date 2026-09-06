package proofing

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Repository is the tenant-aware persistence port for proofing revisions.
// tenantID is the database tenant identity; the domain's EntityRef.Tenant is
// retained as part of the proofing subject value and is not used as a storage
// partition key.
type Repository interface {
	SaveSession(context.Context, string, ProofingSession, uint64) error
	LoadSession(context.Context, string, string, uint64) (ProofingSession, error)
	CurrentSession(context.Context, string, string) (ProofingSession, error)
	ListSessions(context.Context, string, string) ([]ProofingSession, error)
	SaveAuthorization(context.Context, string, WorkAuthorizationEvidence, uint64) error
	LoadAuthorization(context.Context, string, string, uint64) (WorkAuthorizationEvidence, error)
	CurrentAuthorization(context.Context, string, string) (WorkAuthorizationEvidence, error)
	ListAuthorizations(context.Context, string, string) ([]WorkAuthorizationEvidence, error)
}

// StoreErrorCode is the stable classification of a persistence refusal.
type StoreErrorCode string

const (
	StoreInvalidCode   StoreErrorCode = "INVALID"
	StoreNotFoundCode  StoreErrorCode = "NOT_FOUND"
	StoreDuplicateCode StoreErrorCode = "DUPLICATE_REVISION"
	StoreStaleCASCode  StoreErrorCode = "STALE_CAS"
)

var (
	ErrStoreInvalid   = errors.New("proofing: invalid store input")
	ErrStoreNotFound  = errors.New("proofing: stored record not found")
	ErrStoreDuplicate = errors.New("proofing: duplicate revision")
	ErrStoreStaleCAS  = errors.New("proofing: stale compare-and-swap")
)

// StoreError is a typed repository failure. Expected and Actual are populated
// for stale compare-and-swap failures.
type StoreError struct {
	Code             StoreErrorCode
	Detail           string
	Expected, Actual uint64
}

func (e *StoreError) Error() string {
	if e == nil {
		return "proofing: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("proofing: %s", e.Code)
	}
	return fmt.Sprintf("proofing: %s: %s", e.Code, e.Detail)
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

// MemoryRepository is the kernel-pure reference implementation of Repository.
// It is useful to callers that need tenant-aware persistence semantics without
// opening a database and mirrors the PostgreSQL adapter's CAS rules.
type MemoryRepository struct {
	mu             sync.RWMutex
	sessions       map[string]map[string]map[uint64]ProofingSession
	authorizations map[string]map[string]map[uint64]WorkAuthorizationEvidence
}

var _ Repository = (*MemoryRepository)(nil)

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		sessions:       make(map[string]map[string]map[uint64]ProofingSession),
		authorizations: make(map[string]map[string]map[uint64]WorkAuthorizationEvidence),
	}
}

func (r *MemoryRepository) SaveSession(_ context.Context, tenantID string, value ProofingSession, expected uint64) error {
	if r == nil || strings.TrimSpace(tenantID) == "" {
		return &StoreError{Code: StoreInvalidCode, Detail: "tenant id is required"}
	}
	if err := value.Validate(); err != nil {
		return &StoreError{Code: StoreInvalidCode, Detail: err.Error()}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	byID := r.sessions[tenantID]
	if byID == nil {
		byID = make(map[string]map[uint64]ProofingSession)
		r.sessions[tenantID] = byID
	}
	revisions := byID[value.SessionID]
	if revisions == nil {
		revisions = make(map[uint64]ProofingSession)
		byID[value.SessionID] = revisions
	}
	if _, ok := revisions[value.Revision]; ok {
		return &StoreError{Code: StoreDuplicateCode, Detail: "session revision already exists"}
	}
	actual, ok := latestSession(revisions)
	if expected == 0 {
		if ok {
			return stale(expected, actual.Revision)
		}
		if value.SupersedesRevision != 0 {
			return &StoreError{Code: StoreInvalidCode, Detail: "initial session cannot supersede a revision"}
		}
	} else if !ok || actual.Revision != expected || value.SupersedesRevision != expected {
		actualRevision := uint64(0)
		if ok {
			actualRevision = actual.Revision
		}
		return stale(expected, actualRevision)
	}
	revisions[value.Revision] = value
	return nil
}

func (r *MemoryRepository) LoadSession(_ context.Context, tenantID, id string, revision uint64) (ProofingSession, error) {
	if r == nil || strings.TrimSpace(tenantID) == "" || id == "" || revision == 0 {
		return ProofingSession{}, &StoreError{Code: StoreInvalidCode, Detail: "tenant, session id and revision are required"}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.sessions[tenantID][id][revision]
	if !ok {
		return ProofingSession{}, &StoreError{Code: StoreNotFoundCode, Detail: "session revision does not exist"}
	}
	return value, nil
}

func (r *MemoryRepository) CurrentSession(_ context.Context, tenantID, id string) (ProofingSession, error) {
	if r == nil || strings.TrimSpace(tenantID) == "" || id == "" {
		return ProofingSession{}, &StoreError{Code: StoreInvalidCode, Detail: "tenant and session id are required"}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := latestSession(r.sessions[tenantID][id])
	if !ok {
		return ProofingSession{}, &StoreError{Code: StoreNotFoundCode, Detail: "session does not exist"}
	}
	return value, nil
}

func (r *MemoryRepository) ListSessions(_ context.Context, tenantID, id string) ([]ProofingSession, error) {
	if r == nil || strings.TrimSpace(tenantID) == "" || id == "" {
		return nil, &StoreError{Code: StoreInvalidCode, Detail: "tenant and session id are required"}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	byRevision := r.sessions[tenantID][id]
	out := make([]ProofingSession, 0, len(byRevision))
	for _, value := range byRevision {
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, &StoreError{Code: StoreNotFoundCode, Detail: "session does not exist"}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	return out, nil
}

func (r *MemoryRepository) SaveAuthorization(_ context.Context, tenantID string, value WorkAuthorizationEvidence, expected uint64) error {
	if r == nil || strings.TrimSpace(tenantID) == "" {
		return &StoreError{Code: StoreInvalidCode, Detail: "tenant id is required"}
	}
	if err := value.Validate(); err != nil {
		return &StoreError{Code: StoreInvalidCode, Detail: err.Error()}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	byID := r.authorizations[tenantID]
	if byID == nil {
		byID = make(map[string]map[uint64]WorkAuthorizationEvidence)
		r.authorizations[tenantID] = byID
	}
	revisions := byID[value.EvidenceID]
	if revisions == nil {
		revisions = make(map[uint64]WorkAuthorizationEvidence)
		byID[value.EvidenceID] = revisions
	}
	if _, ok := revisions[value.Revision]; ok {
		return &StoreError{Code: StoreDuplicateCode, Detail: "authorization revision already exists"}
	}
	actual, ok := latestAuthorization(revisions)
	if expected == 0 {
		if ok {
			return stale(expected, actual.Revision)
		}
		if value.SupersedesRevision != 0 {
			return &StoreError{Code: StoreInvalidCode, Detail: "initial authorization cannot supersede a revision"}
		}
	} else if !ok || actual.Revision != expected || value.SupersedesRevision != expected {
		actualRevision := uint64(0)
		if ok {
			actualRevision = actual.Revision
		}
		return stale(expected, actualRevision)
	}
	revisions[value.Revision] = value
	return nil
}

func (r *MemoryRepository) LoadAuthorization(_ context.Context, tenantID, id string, revision uint64) (WorkAuthorizationEvidence, error) {
	if r == nil || strings.TrimSpace(tenantID) == "" || id == "" || revision == 0 {
		return WorkAuthorizationEvidence{}, &StoreError{Code: StoreInvalidCode, Detail: "tenant, evidence id and revision are required"}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.authorizations[tenantID][id][revision]
	if !ok {
		return WorkAuthorizationEvidence{}, &StoreError{Code: StoreNotFoundCode, Detail: "authorization revision does not exist"}
	}
	return value, nil
}

func (r *MemoryRepository) CurrentAuthorization(_ context.Context, tenantID, id string) (WorkAuthorizationEvidence, error) {
	if r == nil || strings.TrimSpace(tenantID) == "" || id == "" {
		return WorkAuthorizationEvidence{}, &StoreError{Code: StoreInvalidCode, Detail: "tenant and evidence id are required"}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := latestAuthorization(r.authorizations[tenantID][id])
	if !ok {
		return WorkAuthorizationEvidence{}, &StoreError{Code: StoreNotFoundCode, Detail: "authorization does not exist"}
	}
	return value, nil
}

func (r *MemoryRepository) ListAuthorizations(_ context.Context, tenantID, id string) ([]WorkAuthorizationEvidence, error) {
	if r == nil || strings.TrimSpace(tenantID) == "" || id == "" {
		return nil, &StoreError{Code: StoreInvalidCode, Detail: "tenant and evidence id are required"}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	byRevision := r.authorizations[tenantID][id]
	out := make([]WorkAuthorizationEvidence, 0, len(byRevision))
	for _, value := range byRevision {
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, &StoreError{Code: StoreNotFoundCode, Detail: "authorization does not exist"}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	return out, nil
}

func stale(expected, actual uint64) *StoreError {
	return &StoreError{Code: StoreStaleCASCode, Expected: expected, Actual: actual, Detail: fmt.Sprintf("expected %d, actual %d", expected, actual)}
}

func latestSession(values map[uint64]ProofingSession) (ProofingSession, bool) {
	var out ProofingSession
	var found bool
	for _, value := range values {
		if !found || value.Revision > out.Revision {
			out, found = value, true
		}
	}
	return out, found
}

func latestAuthorization(values map[uint64]WorkAuthorizationEvidence) (WorkAuthorizationEvidence, bool) {
	var out WorkAuthorizationEvidence
	var found bool
	for _, value := range values {
		if !found || value.Revision > out.Revision {
			out, found = value, true
		}
	}
	return out, found
}
