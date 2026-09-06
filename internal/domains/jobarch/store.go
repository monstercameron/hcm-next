package jobarch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Store is the tenant-aware persistence port for immutable architecture
// snapshots. A save appends a new architecture revision and requires the
// caller's expected current revision; an empty expected revision means the
// architecture must not exist yet.
type Store interface {
	Save(context.Context, string, ArchitectureRevision, string) error
	Load(context.Context, string, string, string) (ArchitectureRevision, error)
	Current(context.Context, string, string) (ArchitectureRevision, error)
	List(context.Context, string, string) ([]ArchitectureRevision, error)
}

var (
	ErrStoreInvalid   = errors.New("jobarch: invalid store input")
	ErrStoreNotFound  = errors.New("jobarch: stored architecture not found")
	ErrStoreDuplicate = errors.New("jobarch: duplicate revision")
	ErrStoreStaleCAS  = errors.New("jobarch: stale compare-and-swap")
)

// StoreErrorCode is the stable machine-readable classification of a store
// refusal. Data adapters return these same errors so callers do not inspect
// PostgreSQL constraint text.
type StoreErrorCode string

const (
	StoreInvalidCode   StoreErrorCode = "INVALID"
	StoreNotFoundCode  StoreErrorCode = "NOT_FOUND"
	StoreDuplicateCode StoreErrorCode = "DUPLICATE_REVISION"
	StoreStaleCASCode  StoreErrorCode = "STALE_CAS"
)

// StoreError is a typed store failure. Expected and Actual are populated for
// stale-CAS failures and are intentionally safe revision identifiers.
type StoreError struct {
	Code             StoreErrorCode
	Detail           string
	Expected, Actual string
}

func (e *StoreError) Error() string {
	if e == nil {
		return "jobarch: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("jobarch: %s", e.Code)
	}
	return fmt.Sprintf("jobarch: %s: %s", e.Code, e.Detail)
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

func newStoreError(code StoreErrorCode, detail string) *StoreError {
	return &StoreError{Code: code, Detail: detail}
}

func cloneArchitecture(a ArchitectureRevision) ArchitectureRevision {
	a.Families = cloneFamilies(a.Families)
	a.Levels = cloneLevels(a.Levels)
	a.Grades = cloneGrades(a.Grades)
	a.Profiles = cloneProfiles(a.Profiles)
	return a
}

// MemoryStore is the kernel-pure reference implementation of Store. It keeps
// immutable snapshots and applies the same duplicate and stale-CAS rules as
// the PostgreSQL adapter.
type MemoryStore struct {
	mu            sync.RWMutex
	architectures map[string]map[string]map[string]ArchitectureRevision
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{architectures: make(map[string]map[string]map[string]ArchitectureRevision)}
}

func (s *MemoryStore) Save(_ context.Context, tenantID string, architecture ArchitectureRevision, expectedRevision string) error {
	if s == nil || strings.TrimSpace(tenantID) == "" {
		return newStoreError(StoreInvalidCode, "tenant id is required")
	}
	if architecture.CanonicalDigest == "" {
		var err error
		architecture, err = NewArchitectureRevision(architecture)
		if err != nil {
			return newStoreError(StoreInvalidCode, err.Error())
		}
	} else if err := architecture.Validate(); err != nil {
		return newStoreError(StoreInvalidCode, err.Error())
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	byID := s.architectures[tenantID]
	if byID == nil {
		byID = make(map[string]map[string]ArchitectureRevision)
		s.architectures[tenantID] = byID
	}
	byRevision := byID[architecture.ID]
	if byRevision == nil {
		byRevision = make(map[string]ArchitectureRevision)
		byID[architecture.ID] = byRevision
	}
	if _, exists := byRevision[architecture.Revision]; exists {
		return newStoreError(StoreDuplicateCode, fmt.Sprintf("architecture %s revision %s", architecture.ID, architecture.Revision))
	}
	current, ok := currentMemoryRevision(byRevision)
	if expectedRevision == "" {
		if ok {
			return staleMemory(expectedRevision, current.Revision)
		}
	} else if !ok || current.Revision != expectedRevision || architecture.SupersedesRevision != expectedRevision {
		actual := ""
		if ok {
			actual = current.Revision
		}
		return staleMemory(expectedRevision, actual)
	}
	if expectedRevision == "" && architecture.SupersedesRevision != "" {
		return newStoreError(StoreInvalidCode, "initial architecture cannot supersede a revision")
	}
	byRevision[architecture.Revision] = cloneArchitecture(architecture)
	return nil
}

func staleMemory(expected, actual string) *StoreError {
	err := newStoreError(StoreStaleCASCode, fmt.Sprintf("expected %q, actual %q", expected, actual))
	err.Expected, err.Actual = expected, actual
	return err
}

func currentMemoryRevision(revisions map[string]ArchitectureRevision) (ArchitectureRevision, bool) {
	var current ArchitectureRevision
	var found bool
	for _, candidate := range revisions {
		isTip := true
		for _, other := range revisions {
			if other.SupersedesRevision == candidate.Revision {
				isTip = false
				break
			}
		}
		if isTip && (!found || candidate.Revision > current.Revision) {
			current, found = candidate, true
		}
	}
	return current, found
}

func (s *MemoryStore) Load(_ context.Context, tenantID string, architectureID, revision string) (ArchitectureRevision, error) {
	if s == nil || strings.TrimSpace(tenantID) == "" || architectureID == "" || revision == "" {
		return ArchitectureRevision{}, newStoreError(StoreInvalidCode, "tenant, architecture id and revision are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	byID := s.architectures[tenantID]
	byRevision := byID[architectureID]
	a, ok := byRevision[revision]
	if !ok {
		return ArchitectureRevision{}, newStoreError(StoreNotFoundCode, fmt.Sprintf("architecture %s revision %s", architectureID, revision))
	}
	return cloneArchitecture(a), nil
}

func (s *MemoryStore) Current(ctx context.Context, tenantID string, architectureID string) (ArchitectureRevision, error) {
	if s == nil || strings.TrimSpace(tenantID) == "" || architectureID == "" {
		return ArchitectureRevision{}, newStoreError(StoreInvalidCode, "tenant and architecture id are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	byRevision := s.architectures[tenantID][architectureID]
	a, ok := currentMemoryRevision(byRevision)
	if !ok {
		return ArchitectureRevision{}, newStoreError(StoreNotFoundCode, fmt.Sprintf("architecture %s", architectureID))
	}
	return cloneArchitecture(a), nil
}

func (s *MemoryStore) List(_ context.Context, tenantID string, architectureID string) ([]ArchitectureRevision, error) {
	if s == nil || strings.TrimSpace(tenantID) == "" || architectureID == "" {
		return nil, newStoreError(StoreInvalidCode, "tenant and architecture id are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	byRevision := s.architectures[tenantID][architectureID]
	out := make([]ArchitectureRevision, 0, len(byRevision))
	for _, a := range byRevision {
		out = append(out, cloneArchitecture(a))
	}
	if len(out) == 0 {
		return nil, newStoreError(StoreNotFoundCode, fmt.Sprintf("architecture %s", architectureID))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	return out, nil
}
