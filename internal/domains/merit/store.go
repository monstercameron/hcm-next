package merit

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Store is the tenant-aware persistence port for immutable merit-cycle
// revisions. Implementations preserve every revision and never rewrite a
// stored snapshot or recommendation.
type Store interface {
	Save(context.Context, string, MeritCycle) error
	Load(context.Context, string, string, uint64) (MeritCycle, error)
	Current(context.Context, string, string) (MeritCycle, error)
}

var (
	ErrStoreInvalid   = errors.New("merit: invalid store input")
	ErrStoreNotFound  = errors.New("merit: stored merit cycle not found")
	ErrStoreDuplicate = errors.New("merit: duplicate revision")
	ErrStoreStaleCAS  = errors.New("merit: stale compare-and-swap")
)

// StoreErrorCode is the stable machine-readable classification of a store
// refusal.
type StoreErrorCode string

const (
	StoreInvalidCode   StoreErrorCode = "INVALID"
	StoreNotFoundCode  StoreErrorCode = "NOT_FOUND"
	StoreDuplicateCode StoreErrorCode = "DUPLICATE_REVISION"
	StoreStaleCASCode  StoreErrorCode = "STALE_CAS"
)

// StoreError is a typed store failure. Expected and Actual identify the
// compare-and-swap values when the refusal is a stale successor.
type StoreError struct {
	Code             StoreErrorCode
	Detail           string
	Expected, Actual uint64
}

func (e *StoreError) Error() string {
	if e == nil {
		return "merit: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("merit: %s", e.Code)
	}
	return fmt.Sprintf("merit: %s: %s", e.Code, e.Detail)
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

func cloneCycle(c MeritCycle) MeritCycle {
	c.Population.Members = append([]PopulationMember(nil), c.Population.Members...)
	c.Guidelines.Rules = append([]GuidelineRule(nil), c.Guidelines.Rules...)
	c.Recommendations = append([]MeritRecommendation(nil), c.Recommendations...)
	for i := range c.Recommendations {
		c.Recommendations[i].Adjustments = append([]CalibrationAdjustment(nil), c.Recommendations[i].Adjustments...)
	}
	return c
}

// MemoryStore is the kernel-pure reference implementation of Store. It uses
// the same immutable revision and stale-parent rules as the PostgreSQL store.
type MemoryStore struct {
	mu     sync.RWMutex
	cycles map[string]map[string]map[uint64]MeritCycle
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore returns an empty tenant-aware merit store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{cycles: make(map[string]map[string]map[uint64]MeritCycle)}
}

func normalizeCycle(cycle MeritCycle) (MeritCycle, error) {
	population, err := NewPopulationSnapshot(cycle.Population)
	if err != nil {
		return MeritCycle{}, newStoreError(StoreInvalidCode, err.Error())
	}
	guidelines, err := NewGuidelineMatrix(cycle.Guidelines)
	if err != nil {
		return MeritCycle{}, newStoreError(StoreInvalidCode, err.Error())
	}
	recommendations := make([]MeritRecommendation, len(cycle.Recommendations))
	for i, recommendation := range cycle.Recommendations {
		recommendations[i], err = NewMeritRecommendation(recommendation)
		if err != nil {
			return MeritCycle{}, newStoreError(StoreInvalidCode, err.Error())
		}
	}
	cycle.Population, cycle.Guidelines, cycle.Recommendations = population, guidelines, recommendations
	cycle, err = NewMeritCycle(cycle)
	if err != nil {
		return MeritCycle{}, newStoreError(StoreInvalidCode, err.Error())
	}
	return cycle, nil
}

func validateStoreContext(ctx context.Context, tenant string) error {
	if ctx == nil {
		return newStoreError(StoreInvalidCode, "context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(tenant) == "" {
		return newStoreError(StoreInvalidCode, "tenant id is required")
	}
	return nil
}

func currentMemoryRevision(revisions map[uint64]MeritCycle) (MeritCycle, bool) {
	var current MeritCycle
	var found bool
	for revision, cycle := range revisions {
		if !found || revision > current.Revision {
			current, found = cycle, true
		}
	}
	return current, found
}

func validateNextMemoryRevision(revisions map[uint64]MeritCycle, cycle MeritCycle) error {
	if _, exists := revisions[cycle.Revision]; exists {
		return newStoreError(StoreDuplicateCode, fmt.Sprintf("cycle %s revision %d", cycle.CycleID, cycle.Revision))
	}
	current, exists := currentMemoryRevision(revisions)
	if !exists {
		if cycle.Revision != 1 || cycle.ParentRevision != 0 || cycle.ParentDigest != "" {
			return &StoreError{Code: StoreStaleCASCode, Detail: "initial cycle must be revision 1 without a parent", Expected: 0, Actual: 0}
		}
		return nil
	}
	if cycle.Revision != current.Revision+1 || cycle.ParentRevision != current.Revision || cycle.ParentDigest != current.CanonicalDigest {
		err := &StoreError{Code: StoreStaleCASCode, Detail: "cycle successor does not extend the current revision", Expected: current.Revision, Actual: cycle.ParentRevision}
		return err
	}
	return nil
}

// Save appends one immutable cycle revision for tenant.
func (s *MemoryStore) Save(ctx context.Context, tenant string, cycle MeritCycle) error {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return err
	}
	cycle, err := normalizeCycle(cycle)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cycles == nil {
		s.cycles = make(map[string]map[string]map[uint64]MeritCycle)
	}
	byID := s.cycles[tenant]
	if byID == nil {
		byID = make(map[string]map[uint64]MeritCycle)
		s.cycles[tenant] = byID
	}
	revisions := byID[cycle.CycleID]
	if revisions == nil {
		revisions = make(map[uint64]MeritCycle)
		byID[cycle.CycleID] = revisions
	}
	if err := validateNextMemoryRevision(revisions, cycle); err != nil {
		return err
	}
	revisions[cycle.Revision] = cloneCycle(cycle)
	return nil
}

// Load returns one immutable cycle revision.
func (s *MemoryStore) Load(ctx context.Context, tenant, cycleID string, revision uint64) (MeritCycle, error) {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return MeritCycle{}, err
	}
	if strings.TrimSpace(cycleID) == "" || revision == 0 {
		return MeritCycle{}, newStoreError(StoreInvalidCode, "cycle id and positive revision are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	cycle, ok := s.cycles[tenant][cycleID][revision]
	if !ok {
		return MeritCycle{}, newStoreError(StoreNotFoundCode, fmt.Sprintf("cycle %s revision %d", cycleID, revision))
	}
	return cloneCycle(cycle), nil
}

// Current returns the highest stored revision for a cycle.
func (s *MemoryStore) Current(ctx context.Context, tenant, cycleID string) (MeritCycle, error) {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return MeritCycle{}, err
	}
	if strings.TrimSpace(cycleID) == "" {
		return MeritCycle{}, newStoreError(StoreInvalidCode, "cycle id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	cycle, ok := currentMemoryRevision(s.cycles[tenant][cycleID])
	if !ok {
		return MeritCycle{}, newStoreError(StoreNotFoundCode, fmt.Sprintf("cycle %s", cycleID))
	}
	return cloneCycle(cycle), nil
}

// Revisions is a small diagnostic helper for the in-memory reference store.
func (s *MemoryStore) Revisions(ctx context.Context, tenant, cycleID string) ([]uint64, error) {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	revisions := s.cycles[tenant][cycleID]
	if len(revisions) == 0 {
		return nil, newStoreError(StoreNotFoundCode, fmt.Sprintf("cycle %s", cycleID))
	}
	out := make([]uint64, 0, len(revisions))
	for revision := range revisions {
		out = append(out, revision)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}
