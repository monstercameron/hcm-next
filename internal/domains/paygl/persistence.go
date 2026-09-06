package paygl

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/monstercameron/hcm-next/internal/domains/labor"
)

var (
	// ErrPersistenceNotFound reports a missing tenant-scoped revision or event.
	ErrPersistenceNotFound = errors.New("paygl: persisted row not found")
	// ErrPersistenceDuplicate reports a revision or event identity that already
	// exists. A retry with the same identity is never silently rewritten.
	ErrPersistenceDuplicate = errors.New("paygl: persisted row already exists")
	// ErrPersistenceVersionConflict reports a successor whose predecessor is not
	// the current revision known to the store.
	ErrPersistenceVersionConflict = errors.New("paygl: persisted revision conflict")
)

const (
	// ConflictDuplicateRevision is the stable code for a duplicate immutable
	// revision or append-only event identity.
	ConflictDuplicateRevision = "DUPLICATE_REVISION"
	// ConflictStaleRevision is the stable code for a missing or stale
	// predecessor in a revision chain.
	ConflictStaleRevision = "STALE_REVISION"
)

// ConflictError classifies an immutable-store collision without requiring a
// caller to inspect a database driver's error text.
type ConflictError struct {
	Code string
	Key  string
	Err  error
}

func (e ConflictError) Error() string {
	if e.Key == "" {
		return fmt.Sprintf("paygl: %s", e.Code)
	}
	return fmt.Sprintf("paygl: %s for %s", e.Code, e.Key)
}

func (e ConflictError) Unwrap() error { return e.Err }

// Store is the persistence port for paygl's immutable rules and mapping
// evidence. It deliberately carries only context, tenant identity and domain
// values, so the domain remains independent of PostgreSQL and its driver.
type Store interface {
	PutLaborRule(context.Context, string, labor.LaborRule) error
	GetLaborRule(context.Context, string, string, string) (labor.LaborRule, error)
	PutAccountingRule(context.Context, string, AccountingRule) error
	GetAccountingRule(context.Context, string, string, string) (AccountingRule, error)
	AppendMappingResult(context.Context, string, MappingResult, uint64) error
	GetMappingResult(context.Context, string, string, uint64) (MappingResult, error)
}

// MappingEvent is the in-memory representation of one append-only mapping
// result identity. The database adapter stores the same event_sequence.
type MappingEvent struct {
	Result        MappingResult
	EventSequence uint64
}

// MemoryStore is the reference implementation of Store for kernel-pure tests.
// It preserves every revision and event and refuses identity reuse.
type MemoryStore struct {
	mu         sync.RWMutex
	labor      map[string]labor.LaborRule
	accounting map[string]AccountingRule
	mappings   map[string]MappingEvent
}

// NewMemoryStore returns an empty in-memory persistence port.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		labor:      make(map[string]labor.LaborRule),
		accounting: make(map[string]AccountingRule),
		mappings:   make(map[string]MappingEvent),
	}
}

func tenantKey(tenant, id, version string) string { return tenant + "\x00" + id + "\x00" + version }
func mappingKey(tenant, runID string, eventSequence uint64) string {
	return fmt.Sprintf("%s\x00%s\x00%d", tenant, runID, eventSequence)
}

func validateTenant(ctx context.Context, tenant string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenant == "" {
		return fmt.Errorf("paygl: tenant is required")
	}
	return nil
}

// PutLaborRule records one immutable labor-rule revision.
func (s *MemoryStore) PutLaborRule(ctx context.Context, tenant string, rule labor.LaborRule) error {
	if err := validateTenant(ctx, tenant); err != nil {
		return err
	}
	if err := rule.Validate(); err != nil {
		return err
	}
	key := tenantKey(tenant, rule.ID, rule.Version)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.labor[key]; exists {
		return ConflictError{Code: ConflictDuplicateRevision, Key: key, Err: ErrPersistenceDuplicate}
	}
	if rule.Supersedes != "" {
		if _, exists := s.labor[tenantKey(tenant, rule.ID, rule.Supersedes)]; !exists {
			return ConflictError{Code: ConflictStaleRevision, Key: key, Err: ErrPersistenceVersionConflict}
		}
	}
	s.labor[key] = rule
	return nil
}

// GetLaborRule loads one immutable labor-rule revision.
func (s *MemoryStore) GetLaborRule(ctx context.Context, tenant, ruleID, version string) (labor.LaborRule, error) {
	if err := validateTenant(ctx, tenant); err != nil {
		return labor.LaborRule{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rule, ok := s.labor[tenantKey(tenant, ruleID, version)]
	if !ok {
		return labor.LaborRule{}, fmt.Errorf("%w: labor_rule %s/%s", ErrPersistenceNotFound, ruleID, version)
	}
	rule.Dimensions = append([]labor.DimensionKind(nil), rule.Dimensions...)
	return rule, nil
}

// PutAccountingRule records one immutable payroll-to-GL rule revision.
func (s *MemoryStore) PutAccountingRule(ctx context.Context, tenant string, rule AccountingRule) error {
	if err := validateTenant(ctx, tenant); err != nil {
		return err
	}
	if err := rule.Validate(); err != nil {
		return err
	}
	key := tenantKey(tenant, rule.ID, rule.Version)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.accounting[key]; exists {
		return ConflictError{Code: ConflictDuplicateRevision, Key: key, Err: ErrPersistenceDuplicate}
	}
	if rule.Supersedes != "" {
		if _, exists := s.accounting[tenantKey(tenant, rule.ID, rule.Supersedes)]; !exists {
			return ConflictError{Code: ConflictStaleRevision, Key: key, Err: ErrPersistenceVersionConflict}
		}
	}
	s.accounting[key] = rule
	return nil
}

// GetAccountingRule loads one immutable payroll-to-GL rule revision.
func (s *MemoryStore) GetAccountingRule(ctx context.Context, tenant, ruleID, version string) (AccountingRule, error) {
	if err := validateTenant(ctx, tenant); err != nil {
		return AccountingRule{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rule, ok := s.accounting[tenantKey(tenant, ruleID, version)]
	if !ok {
		return AccountingRule{}, fmt.Errorf("%w: paygl_accounting_rule %s/%s", ErrPersistenceNotFound, ruleID, version)
	}
	rule.Dimension = cloneDimension(rule.Dimension)
	rule.Dimensions = append([]labor.Dimension(nil), rule.Dimensions...)
	rule.LaborDimensions = append([]labor.Dimension(nil), rule.LaborDimensions...)
	return rule, nil
}

// AppendMappingResult records one immutable result event.
func (s *MemoryStore) AppendMappingResult(ctx context.Context, tenant string, result MappingResult, eventSequence uint64) error {
	if err := validateTenant(ctx, tenant); err != nil {
		return err
	}
	if eventSequence == 0 {
		return fmt.Errorf("paygl: event sequence must be positive")
	}
	if err := result.Validate(); err != nil {
		return err
	}
	key := mappingKey(tenant, result.RunID, eventSequence)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.mappings[key]; exists {
		return ConflictError{Code: ConflictDuplicateRevision, Key: key, Err: ErrPersistenceDuplicate}
	}
	s.mappings[key] = MappingEvent{Result: cloneMappingResult(result), EventSequence: eventSequence}
	return nil
}

// GetMappingResult loads one append-only result event.
func (s *MemoryStore) GetMappingResult(ctx context.Context, tenant, runID string, eventSequence uint64) (MappingResult, error) {
	if err := validateTenant(ctx, tenant); err != nil {
		return MappingResult{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	event, ok := s.mappings[mappingKey(tenant, runID, eventSequence)]
	if !ok {
		return MappingResult{}, fmt.Errorf("%w: paygl_mapping_result %s/%d", ErrPersistenceNotFound, runID, eventSequence)
	}
	return cloneMappingResult(event.Result), nil
}

func cloneDimension(d labor.Dimension) labor.Dimension { return d }

func cloneMappingResult(result MappingResult) MappingResult {
	result.Mappings = append([]ComponentMapping(nil), result.Mappings...)
	for i := range result.Mappings {
		result.Mappings[i].Dimension = cloneDimension(result.Mappings[i].Dimension)
	}
	return result
}

var _ Store = (*MemoryStore)(nil)
