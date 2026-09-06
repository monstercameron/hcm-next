package demand

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Store is the tenant-aware persistence port for immutable demand and
// coverage revisions. An empty expectedVersion creates the first revision;
// later writes must name the current version so a stale planner cannot fork
// silently.
type Store interface {
	SaveSignal(context.Context, string, DemandSignal, string) error
	LoadSignal(context.Context, string, string, string) (DemandSignal, error)
	ListSignalVersions(context.Context, string, string) ([]DemandSignal, error)
	SaveCoverageRequirement(context.Context, string, CoverageRequirement, string) error
	LoadCoverageRequirement(context.Context, string, string, string) (CoverageRequirement, error)
	ListCoverageVersions(context.Context, string, string) ([]CoverageRequirement, error)
}

var (
	ErrStoreInvalid   = errors.New("demand: invalid store input")
	ErrStoreNotFound  = errors.New("demand: stored revision not found")
	ErrStoreDuplicate = errors.New("demand: duplicate revision")
	ErrStoreStaleCAS  = errors.New("demand: stale compare-and-swap")
)

// StoreErrorCode is the stable classification returned by both the memory
// implementation and database adapters.
type StoreErrorCode string

const (
	StoreInvalidCode   StoreErrorCode = "INVALID"
	StoreNotFoundCode  StoreErrorCode = "NOT_FOUND"
	StoreDuplicateCode StoreErrorCode = "DUPLICATE_REVISION"
	StoreStaleCASCode  StoreErrorCode = "STALE_CAS"
)

// StoreError carries a machine-readable refusal and the expected/current
// version pair for compare-and-set failures.
type StoreError struct {
	Code             StoreErrorCode
	Detail           string
	Expected, Actual string
}

func (e *StoreError) Error() string {
	if e == nil {
		return "demand: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("demand: %s", e.Code)
	}
	return fmt.Sprintf("demand: %s: %s", e.Code, e.Detail)
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

func storeError(code StoreErrorCode, detail string) *StoreError {
	return &StoreError{Code: code, Detail: detail}
}

func staleStoreError(expected, actual string) *StoreError {
	err := storeError(StoreStaleCASCode, fmt.Sprintf("expected %q, actual %q", expected, actual))
	err.Expected, err.Actual = expected, actual
	return err
}

// MemoryStore is the kernel-pure reference implementation of Store. It keeps
// detached immutable revisions and applies the same duplicate and stale-CAS
// rules as the PostgreSQL adapter.
type MemoryStore struct {
	mu        sync.RWMutex
	signals   map[string]map[string]map[string]DemandSignal
	coverages map[string]map[string]map[string]CoverageRequirement
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore returns an empty in-memory demand store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		signals:   make(map[string]map[string]map[string]DemandSignal),
		coverages: make(map[string]map[string]map[string]CoverageRequirement),
	}
}

func validateStoreContext(ctx context.Context, tenant string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(tenant) == "" {
		return storeError(StoreInvalidCode, "tenant id is required")
	}
	return nil
}

func normalizeSignal(signal DemandSignal) (DemandSignal, error) {
	if signal.CanonicalDigest == "" {
		value, err := NewDemandSignal(signal)
		if err != nil {
			return DemandSignal{}, storeError(StoreInvalidCode, err.Error())
		}
		return value, nil
	}
	if err := signal.Validate(); err != nil {
		return DemandSignal{}, storeError(StoreInvalidCode, err.Error())
	}
	return signal, nil
}

func normalizeCoverage(requirement CoverageRequirement) (CoverageRequirement, error) {
	if err := requirement.Validate(); err != nil {
		return CoverageRequirement{}, storeError(StoreInvalidCode, err.Error())
	}
	return cloneCoverage(requirement), nil
}

func (s *MemoryStore) SaveSignal(ctx context.Context, tenant string, signal DemandSignal, expectedVersion string) error {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return err
	}
	if s == nil {
		return storeError(StoreInvalidCode, "nil memory store")
	}
	var err error
	signal, err = normalizeSignal(signal)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	byTenant := s.signals[tenant]
	if byTenant == nil {
		byTenant = make(map[string]map[string]DemandSignal)
		s.signals[tenant] = byTenant
	}
	byID := byTenant[signal.SignalID]
	if byID == nil {
		byID = make(map[string]DemandSignal)
		byTenant[signal.SignalID] = byID
	}
	if _, exists := byID[signal.Version]; exists {
		return storeError(StoreDuplicateCode, fmt.Sprintf("signal %s/%s", signal.SignalID, signal.Version))
	}
	actual := latestSignalVersion(byID)
	if expectedVersion != actual {
		if expectedVersion == "" && actual == "" {
			// First revision.
		} else {
			return staleStoreError(expectedVersion, actual)
		}
	}
	byID[signal.Version] = cloneSignal(signal)
	return nil
}

func (s *MemoryStore) LoadSignal(ctx context.Context, tenant, signalID, version string) (DemandSignal, error) {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return DemandSignal{}, err
	}
	if signalID == "" || version == "" {
		return DemandSignal{}, storeError(StoreInvalidCode, "signal id and version are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.signals[tenant][signalID][version]
	if !ok {
		return DemandSignal{}, storeError(StoreNotFoundCode, fmt.Sprintf("signal %s/%s", signalID, version))
	}
	return cloneSignal(value), nil
}

func (s *MemoryStore) ListSignalVersions(ctx context.Context, tenant, signalID string) ([]DemandSignal, error) {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	byID := s.signals[tenant][signalID]
	if len(byID) == 0 {
		return nil, storeError(StoreNotFoundCode, fmt.Sprintf("signal %s", signalID))
	}
	out := make([]DemandSignal, 0, len(byID))
	for _, value := range byID {
		out = append(out, cloneSignal(value))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func (s *MemoryStore) SaveCoverageRequirement(ctx context.Context, tenant string, requirement CoverageRequirement, expectedVersion string) error {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return err
	}
	if s == nil {
		return storeError(StoreInvalidCode, "nil memory store")
	}
	var err error
	requirement, err = normalizeCoverage(requirement)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	byTenant := s.coverages[tenant]
	if byTenant == nil {
		byTenant = make(map[string]map[string]CoverageRequirement)
		s.coverages[tenant] = byTenant
	}
	byID := byTenant[requirement.RequirementID]
	if byID == nil {
		byID = make(map[string]CoverageRequirement)
		byTenant[requirement.RequirementID] = byID
	}
	if _, exists := byID[requirement.Version]; exists {
		return storeError(StoreDuplicateCode, fmt.Sprintf("coverage requirement %s/%s", requirement.RequirementID, requirement.Version))
	}
	actual := latestCoverageVersion(byID)
	if expectedVersion != actual {
		if expectedVersion == "" && actual == "" {
			// First revision.
		} else {
			return staleStoreError(expectedVersion, actual)
		}
	}
	byID[requirement.Version] = cloneCoverage(requirement)
	return nil
}

func (s *MemoryStore) LoadCoverageRequirement(ctx context.Context, tenant, requirementID, version string) (CoverageRequirement, error) {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return CoverageRequirement{}, err
	}
	if requirementID == "" || version == "" {
		return CoverageRequirement{}, storeError(StoreInvalidCode, "requirement id and version are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.coverages[tenant][requirementID][version]
	if !ok {
		return CoverageRequirement{}, storeError(StoreNotFoundCode, fmt.Sprintf("coverage requirement %s/%s", requirementID, version))
	}
	return cloneCoverage(value), nil
}

func (s *MemoryStore) ListCoverageVersions(ctx context.Context, tenant, requirementID string) ([]CoverageRequirement, error) {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	byID := s.coverages[tenant][requirementID]
	if len(byID) == 0 {
		return nil, storeError(StoreNotFoundCode, fmt.Sprintf("coverage requirement %s", requirementID))
	}
	out := make([]CoverageRequirement, 0, len(byID))
	for _, value := range byID {
		out = append(out, cloneCoverage(value))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func latestSignalVersion(values map[string]DemandSignal) string {
	var latest string
	for version := range values {
		if version > latest {
			latest = version
		}
	}
	return latest
}

func latestCoverageVersion(values map[string]CoverageRequirement) string {
	var latest string
	for version := range values {
		if version > latest {
			latest = version
		}
	}
	return latest
}

func cloneSignal(in DemandSignal) DemandSignal { return in }

func cloneCoverage(in CoverageRequirement) CoverageRequirement {
	in.Signals = append([]DemandSignal(nil), in.Signals...)
	for i := range in.Signals {
		in.Signals[i] = cloneSignal(in.Signals[i])
	}
	in.SupplyRefs = append([]string(nil), in.SupplyRefs...)
	return in
}
