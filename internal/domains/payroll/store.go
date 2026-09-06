package payroll

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrStoreRefused is the common sentinel for persistence-boundary refusals.
var ErrStoreRefused = errors.New("payroll: store operation refused")

var (
	// ErrDuplicateRevision means that the tenant already owns the requested
	// revision identity. Revisions are immutable, so the caller must not retry
	// the same identity with different content.
	ErrDuplicateRevision = errors.New("PAYROLL_DUPLICATE_REVISION")
	// ErrStaleRevision means that a new revision did not extend the currently
	// stored revision chain.
	ErrStaleRevision = errors.New("PAYROLL_STALE_REVISION")
	// ErrDuplicateAmendment means that an event sequence is already occupied.
	ErrDuplicateAmendment = errors.New("PAYROLL_DUPLICATE_AMENDMENT")
	// ErrNotFound means that the requested tenant-owned revision is absent.
	ErrNotFound = errors.New("PAYROLL_NOT_FOUND")
)

// RefusalError preserves the machine-readable refusal code and the domain
// field involved in it while still allowing errors.Is classification.
type RefusalError struct {
	Code   string
	Field  string
	Reason string
	Cause  error
}

func (e *RefusalError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("payroll: %s: %s", e.Code, e.Reason)
	}
	return fmt.Sprintf("payroll: %s: %s: %s", e.Code, e.Field, e.Reason)
}

func (e *RefusalError) Unwrap() error { return e.Cause }

func (e *RefusalError) Is(target error) bool {
	return target == ErrStoreRefused || target == e.Cause
}

func refuse(code, field, reason string, cause error) error {
	return &RefusalError{Code: code, Field: field, Reason: reason, Cause: cause}
}

// Store is the tenant-aware semantic persistence port for payroll lifecycle
// revisions and population amendments. Implementations must preserve every
// revision and every amendment; a later write supersedes a value only by
// naming it, never by modifying the stored row.
type Store interface {
	SaveRun(context.Context, TenantID, PayrollRun) error
	LoadRun(context.Context, TenantID, string, uint64) (PayrollRun, error)
	SavePopulation(context.Context, TenantID, FrozenPopulation) error
	LoadPopulation(context.Context, TenantID, string, uint64) (FrozenPopulation, error)
	AppendAmendment(context.Context, TenantID, string, uint64, PopulationAmendment) error
	ListAmendments(context.Context, TenantID, string) ([]PopulationAmendment, error)
}

// TenantID is the small identity capability the domain port needs. The
// concrete UUID type belongs to the data boundary; callers pass it here
// without making the kernel package import a persistence mechanic.
type TenantID interface{ String() string }

// MemoryStore is a concurrency-safe reference implementation of [Store]. It
// is intentionally kernel-pure: it has no clock, database, or side effect
// beyond retaining the immutable values supplied by its caller.
type MemoryStore struct {
	mu          sync.RWMutex
	runs        map[string]map[uint64]PayrollRun
	populations map[string]map[uint64]FrozenPopulation
	amendments  map[string]map[uint64]PopulationAmendment
}

// NewMemoryStore returns an empty payroll store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		runs:        make(map[string]map[uint64]PayrollRun),
		populations: make(map[string]map[uint64]FrozenPopulation),
		amendments:  make(map[string]map[uint64]PopulationAmendment),
	}
}

func tenantKey(tenantID TenantID, id string) string { return tenantID.String() + "\x00" + id }

func checkTenant(tenantID TenantID) error {
	if tenantID == nil || tenantID.String() == "" {
		return refuse("PAYROLL_INVALID_TENANT", "tenant_id", "tenant id is required", ErrStoreRefused)
	}
	return nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return refuse("PAYROLL_INVALID_CONTEXT", "context", "context is required", ErrStoreRefused)
	}
	return ctx.Err()
}

func normalizeRun(run PayrollRun) (PayrollRun, error) {
	if err := run.Validate(); err != nil {
		return PayrollRun{}, err
	}
	if run.CanonicalDigest == "" {
		digest, err := run.Digest()
		if err != nil {
			return PayrollRun{}, err
		}
		run.CanonicalDigest = digest
	}
	return run, nil
}

func normalizePopulation(population FrozenPopulation) (FrozenPopulation, error) {
	if err := population.Validate(); err != nil {
		return FrozenPopulation{}, err
	}
	population.Members = append([]PopulationMember(nil), population.Members...)
	return population, nil
}

func normalizeAmendment(amendment PopulationAmendment) (PopulationAmendment, error) {
	canonical, err := NewPopulationAmendment(amendment.Kind, amendment.Member, amendment.Reason, amendment.EffectiveAsOf)
	if err != nil {
		return PopulationAmendment{}, err
	}
	if amendment.Digest != "" && amendment.Digest != canonical.Digest {
		return PopulationAmendment{}, refuse("PAYROLL_DIGEST_MISMATCH", "digest", "amendment digest does not match its content", ErrStoreRefused)
	}
	return canonical, nil
}

func validateNextRun(revisions map[uint64]PayrollRun, run PayrollRun) error {
	if _, exists := revisions[run.Revision]; exists {
		return refuse(ErrDuplicateRevision.Error(), "revision", "revision identity is already stored", ErrDuplicateRevision)
	}
	if len(revisions) == 0 {
		if run.Revision != 1 || run.SupersedesRevision != 0 {
			return refuse(ErrStaleRevision.Error(), "revision", "the first revision must be revision 1", ErrStaleRevision)
		}
		return nil
	}
	var latest uint64
	for revision := range revisions {
		if revision > latest {
			latest = revision
		}
	}
	if run.Revision != latest+1 || run.SupersedesRevision != latest {
		return refuse(ErrStaleRevision.Error(), "supersedes_revision", "revision does not extend the current run chain", ErrStaleRevision)
	}
	return nil
}

func validateNextPopulation(revisions map[uint64]FrozenPopulation, population FrozenPopulation) error {
	if _, exists := revisions[population.Revision]; exists {
		return refuse(ErrDuplicateRevision.Error(), "revision", "revision identity is already stored", ErrDuplicateRevision)
	}
	if len(revisions) == 0 {
		if population.Revision != 1 || population.SupersedesDigest != "" {
			return refuse(ErrStaleRevision.Error(), "supersedes_digest", "the first population revision has no predecessor", ErrStaleRevision)
		}
		return nil
	}
	var latest FrozenPopulation
	for _, candidate := range revisions {
		if candidate.Revision > latest.Revision {
			latest = candidate
		}
	}
	if population.Revision != latest.Revision+1 || population.SupersedesDigest != latest.Digest {
		return refuse(ErrStaleRevision.Error(), "supersedes_digest", "population revision does not extend the current chain", ErrStaleRevision)
	}
	return nil
}

// SaveRun implements [Store].
func (s *MemoryStore) SaveRun(ctx context.Context, tenantID TenantID, run PayrollRun) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := checkTenant(tenantID); err != nil {
		return err
	}
	run, err := normalizeRun(run)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runs == nil {
		s.runs = make(map[string]map[uint64]PayrollRun)
	}
	key := tenantKey(tenantID, run.RunID)
	if s.runs[key] == nil {
		s.runs[key] = make(map[uint64]PayrollRun)
	}
	if err := validateNextRun(s.runs[key], run); err != nil {
		return err
	}
	s.runs[key][run.Revision] = run
	return nil
}

// LoadRun implements [Store].
func (s *MemoryStore) LoadRun(ctx context.Context, tenantID TenantID, runID string, revision uint64) (PayrollRun, error) {
	if err := checkContext(ctx); err != nil {
		return PayrollRun{}, err
	}
	if err := checkTenant(tenantID); err != nil {
		return PayrollRun{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[tenantKey(tenantID, runID)][revision]
	if !ok {
		return PayrollRun{}, refuse(ErrNotFound.Error(), "run_id", "payroll run revision was not found", ErrNotFound)
	}
	return run, nil
}

// SavePopulation implements [Store].
func (s *MemoryStore) SavePopulation(ctx context.Context, tenantID TenantID, population FrozenPopulation) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := checkTenant(tenantID); err != nil {
		return err
	}
	population, err := normalizePopulation(population)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.populations == nil {
		s.populations = make(map[string]map[uint64]FrozenPopulation)
	}
	key := tenantKey(tenantID, population.RunID)
	if s.populations[key] == nil {
		s.populations[key] = make(map[uint64]FrozenPopulation)
	}
	if err := validateNextPopulation(s.populations[key], population); err != nil {
		return err
	}
	s.populations[key][population.Revision] = population
	return nil
}

// LoadPopulation implements [Store].
func (s *MemoryStore) LoadPopulation(ctx context.Context, tenantID TenantID, runID string, revision uint64) (FrozenPopulation, error) {
	if err := checkContext(ctx); err != nil {
		return FrozenPopulation{}, err
	}
	if err := checkTenant(tenantID); err != nil {
		return FrozenPopulation{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	population, ok := s.populations[tenantKey(tenantID, runID)][revision]
	if !ok {
		return FrozenPopulation{}, refuse(ErrNotFound.Error(), "run_id", "frozen population revision was not found", ErrNotFound)
	}
	population.Members = append([]PopulationMember(nil), population.Members...)
	return population, nil
}

// AppendAmendment implements [Store].
func (s *MemoryStore) AppendAmendment(ctx context.Context, tenantID TenantID, runID string, eventSequence uint64, amendment PopulationAmendment) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := checkTenant(tenantID); err != nil {
		return err
	}
	if eventSequence == 0 {
		return refuse("PAYROLL_INVALID_EVENT_SEQUENCE", "event_sequence", "event sequence must be positive", ErrStoreRefused)
	}
	amendment, err := normalizeAmendment(amendment)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.amendments == nil {
		s.amendments = make(map[string]map[uint64]PopulationAmendment)
	}
	key := tenantKey(tenantID, runID)
	if s.amendments[key] == nil {
		s.amendments[key] = make(map[uint64]PopulationAmendment)
	}
	if _, exists := s.amendments[key][eventSequence]; exists {
		return refuse(ErrDuplicateAmendment.Error(), "event_sequence", "event sequence is already stored", ErrDuplicateAmendment)
	}
	s.amendments[key][eventSequence] = amendment
	return nil
}

// ListAmendments implements [Store].
func (s *MemoryStore) ListAmendments(ctx context.Context, tenantID TenantID, runID string) ([]PopulationAmendment, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := checkTenant(tenantID); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	bySequence := s.amendments[tenantKey(tenantID, runID)]
	sequences := make([]uint64, 0, len(bySequence))
	for sequence := range bySequence {
		sequences = append(sequences, sequence)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	out := make([]PopulationAmendment, 0, len(sequences))
	for _, sequence := range sequences {
		out = append(out, bySequence[sequence])
	}
	return out, nil
}

var _ Store = (*MemoryStore)(nil)
