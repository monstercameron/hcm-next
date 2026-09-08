package crm

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Store is the tenant-aware persistence port for governed talent pools and
// their immutable membership revisions.
type Store interface {
	PutPool(context.Context, values.TenantId, TalentPoolRevision, ...uint64) error
	GetPool(context.Context, values.TenantId, string, uint64) (TalentPoolRevision, error)
	ListPoolVersions(context.Context, values.TenantId, string) ([]TalentPoolRevision, error)
	PutMembership(context.Context, values.TenantId, TalentPoolMembershipRevision, ...uint64) error
	GetMembership(context.Context, values.TenantId, string, uint64) (TalentPoolMembershipRevision, error)
	ListMembershipVersions(context.Context, values.TenantId, string) ([]TalentPoolMembershipRevision, error)
}

var (
	ErrPoolNotFound        = errors.New("crm: talent pool revision not found")
	ErrMembershipNotFound  = errors.New("crm: talent pool membership revision not found")
	ErrPoolDuplicate       = errors.New("crm: talent pool revision already exists")
	ErrMembershipDuplicate = errors.New("crm: talent pool membership revision already exists")
	ErrVersionConflict     = errors.New("crm: revision compare-and-swap conflict")
)

// StoreErrorCode is the stable machine-readable classification of a store
// refusal. Adapters return these codes instead of exposing SQL constraint text.
type StoreErrorCode string

const (
	StoreInvalidCode           StoreErrorCode = "INVALID"
	StoreNotFoundCode          StoreErrorCode = "NOT_FOUND"
	StoreDuplicateCode         StoreErrorCode = "DUPLICATE_REVISION"
	StoreStaleCASCode          StoreErrorCode = "STALE_CAS"
	StoreDatabaseCode          StoreErrorCode = "DATABASE"
	StoreReferenceNotFoundCode StoreErrorCode = "REFERENCE_NOT_FOUND"
)

// StoreError is a typed persistence refusal. Expected and Actual are populated
// for stale-CAS errors when the current revision is known.
type StoreError struct {
	Code             StoreErrorCode
	Detail           string
	Expected, Actual uint64
	cause            error
}

func (e *StoreError) Error() string {
	if e == nil {
		return "crm: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("crm: %s", e.Code)
	}
	return fmt.Sprintf("crm: %s: %s", e.Code, e.Detail)
}

func (e *StoreError) Unwrap() error {
	if e == nil {
		return nil
	}
	if e.cause != nil {
		return e.cause
	}
	switch e.Code {
	case StoreNotFoundCode:
		return ErrPoolNotFound
	case StoreDuplicateCode:
		return ErrPoolDuplicate
	case StoreStaleCASCode:
		return ErrVersionConflict
	default:
		return nil
	}
}

// MemoryStore is the kernel-pure reference implementation of Store. It keeps
// every revision and applies the same sequential revision and CAS rules as the
// PostgreSQL adapter.
type MemoryStore struct {
	mu          sync.RWMutex
	pools       map[string]map[string]map[uint64]TalentPoolRevision
	memberships map[string]map[string]map[uint64]TalentPoolMembershipRevision
}

var _ Store = (*MemoryStore)(nil)

// NewMemoryStore returns an empty CRM store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		pools:       make(map[string]map[string]map[uint64]TalentPoolRevision),
		memberships: make(map[string]map[string]map[uint64]TalentPoolMembershipRevision),
	}
}

func (s *MemoryStore) PutPool(ctx context.Context, tenant values.TenantId, pool TalentPoolRevision, expectedVersion ...uint64) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil {
		return storeInvalid("nil memory store")
	}
	if len(expectedVersion) > 1 {
		return storeInvalid("at most one expected version is allowed")
	}
	if err := pool.Validate(); err != nil {
		return storeInvalid(err.Error())
	}
	revision, err := sequence(pool.Revision)
	if err != nil {
		return storeInvalid(err.Error())
	}
	if pool.PoolID.Tenant != tenant {
		return storeInvalid("pool tenant does not match store tenant")
	}
	expected := uint64(0)
	if len(expectedVersion) == 1 {
		expected = expectedVersion[0]
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	byTenant := s.pools[string(tenant)]
	if byTenant == nil {
		byTenant = make(map[string]map[uint64]TalentPoolRevision)
		s.pools[string(tenant)] = byTenant
	}
	byRevision := byTenant[pool.PoolID.Id]
	if byRevision == nil {
		byRevision = make(map[uint64]TalentPoolRevision)
		byTenant[pool.PoolID.Id] = byRevision
	}
	latest := latestPoolRevision(byRevision)
	if _, exists := byRevision[revision]; exists {
		return &StoreError{Code: StoreDuplicateCode, Detail: fmt.Sprintf("pool %s revision %d", pool.PoolID.Id, revision)}
	}
	if expected != latest || revision != latest+1 {
		return stale(expected, latest, fmt.Sprintf("pool %s", pool.PoolID.Id))
	}
	byRevision[revision] = pool
	return nil
}

func (s *MemoryStore) GetPool(ctx context.Context, tenant values.TenantId, id string, revision uint64) (TalentPoolRevision, error) {
	if err := contextError(ctx); err != nil {
		return TalentPoolRevision{}, err
	}
	if s == nil {
		return TalentPoolRevision{}, storeInvalid("nil memory store")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	pool, ok := s.pools[string(tenant)][id][revision]
	if !ok {
		return TalentPoolRevision{}, &StoreError{Code: StoreNotFoundCode, Detail: fmt.Sprintf("pool %s revision %d", id, revision)}
	}
	return pool, nil
}

func (s *MemoryStore) ListPoolVersions(ctx context.Context, tenant values.TenantId, id string) ([]TalentPoolRevision, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, storeInvalid("nil memory store")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	byRevision := s.pools[string(tenant)][id]
	if len(byRevision) == 0 {
		return nil, &StoreError{Code: StoreNotFoundCode, Detail: fmt.Sprintf("pool %s", id)}
	}
	keys := make([]uint64, 0, len(byRevision))
	for revision := range byRevision {
		keys = append(keys, revision)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]TalentPoolRevision, 0, len(keys))
	for _, revision := range keys {
		out = append(out, byRevision[revision])
	}
	return out, nil
}

func (s *MemoryStore) PutMembership(ctx context.Context, tenant values.TenantId, membership TalentPoolMembershipRevision, expectedVersion ...uint64) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil {
		return storeInvalid("nil memory store")
	}
	if len(expectedVersion) > 1 {
		return storeInvalid("at most one expected version is allowed")
	}
	if err := membership.Validate(); err != nil {
		return storeInvalid(err.Error())
	}
	revision, err := sequence(membership.Revision)
	if err != nil {
		return storeInvalid(err.Error())
	}
	if membership.MembershipID.Tenant != tenant {
		return storeInvalid("membership tenant does not match store tenant")
	}
	expected := uint64(0)
	if len(expectedVersion) == 1 {
		expected = expectedVersion[0]
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if latestPoolRevision(s.pools[string(tenant)][membership.Pool.Id]) == 0 {
		return &StoreError{Code: StoreReferenceNotFoundCode, Detail: fmt.Sprintf("pool %s", membership.Pool.Id)}
	}
	byTenant := s.memberships[string(tenant)]
	if byTenant == nil {
		byTenant = make(map[string]map[uint64]TalentPoolMembershipRevision)
		s.memberships[string(tenant)] = byTenant
	}
	byRevision := byTenant[membership.MembershipID.Id]
	if byRevision == nil {
		byRevision = make(map[uint64]TalentPoolMembershipRevision)
		byTenant[membership.MembershipID.Id] = byRevision
	}
	latest := latestMembershipRevision(byRevision)
	if _, exists := byRevision[revision]; exists {
		return &StoreError{Code: StoreDuplicateCode, Detail: fmt.Sprintf("membership %s revision %d", membership.MembershipID.Id, revision), cause: ErrMembershipDuplicate}
	}
	if expected != latest || revision != latest+1 {
		return stale(expected, latest, fmt.Sprintf("membership %s", membership.MembershipID.Id))
	}
	byRevision[revision] = membership
	return nil
}

func (s *MemoryStore) GetMembership(ctx context.Context, tenant values.TenantId, id string, revision uint64) (TalentPoolMembershipRevision, error) {
	if err := contextError(ctx); err != nil {
		return TalentPoolMembershipRevision{}, err
	}
	if s == nil {
		return TalentPoolMembershipRevision{}, storeInvalid("nil memory store")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	membership, ok := s.memberships[string(tenant)][id][revision]
	if !ok {
		return TalentPoolMembershipRevision{}, &StoreError{Code: StoreNotFoundCode, Detail: fmt.Sprintf("membership %s revision %d", id, revision), cause: ErrMembershipNotFound}
	}
	return membership, nil
}

func (s *MemoryStore) ListMembershipVersions(ctx context.Context, tenant values.TenantId, id string) ([]TalentPoolMembershipRevision, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, storeInvalid("nil memory store")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	byRevision := s.memberships[string(tenant)][id]
	if len(byRevision) == 0 {
		return nil, &StoreError{Code: StoreNotFoundCode, Detail: fmt.Sprintf("membership %s", id), cause: ErrMembershipNotFound}
	}
	keys := make([]uint64, 0, len(byRevision))
	for revision := range byRevision {
		keys = append(keys, revision)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]TalentPoolMembershipRevision, 0, len(keys))
	for _, revision := range keys {
		out = append(out, byRevision[revision])
	}
	return out, nil
}

func sequence(token values.RevisionToken) (uint64, error) {
	if !token.IsSpecified() {
		return 0, errors.New("revision is required")
	}
	value, ok := token.Sequence()
	if !ok || value == 0 {
		return 0, errors.New("revision must be a positive sequence")
	}
	return value, nil
}

func latestPoolRevision(revisions map[uint64]TalentPoolRevision) uint64 {
	var latest uint64
	for revision := range revisions {
		if revision > latest {
			latest = revision
		}
	}
	return latest
}

func latestMembershipRevision(revisions map[uint64]TalentPoolMembershipRevision) uint64 {
	var latest uint64
	for revision := range revisions {
		if revision > latest {
			latest = revision
		}
	}
	return latest
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("crm: nil context")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func storeInvalid(detail string) error {
	return &StoreError{Code: StoreInvalidCode, Detail: detail}
}

func stale(expected, actual uint64, subject string) error {
	return &StoreError{Code: StoreStaleCASCode, Detail: fmt.Sprintf("%s expected %d, actual %d", subject, expected, actual), Expected: expected, Actual: actual}
}
