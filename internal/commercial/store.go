package commercial

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ContractRevisionStore is the persistence-neutral port for fixed-price
// commercial revisions and their frozen entitlement snapshots. Implementations
// append revisions; they never rewrite a revision already observed by a
// consumer.
type ContractRevisionStore interface {
	PutContractRevision(context.Context, ContractRevision, ...uint64) error
	GetContractRevision(context.Context, string, string, uint64) (ContractRevision, error)
	ListContractRevisions(context.Context, string, string) ([]ContractRevision, error)
	PutEntitlementSnapshot(context.Context, EntitlementSnapshot) error
	GetEntitlementSnapshot(context.Context, string, string, uint64) (EntitlementSnapshot, error)
}

var (
	ErrStoreInvalid           = errors.New("commercial: invalid store input")
	ErrStoreNotFound          = errors.New("commercial: stored row not found")
	ErrStoreDuplicateRevision = errors.New("commercial: duplicate revision")
	ErrStoreStaleCAS          = errors.New("commercial: stale compare-and-swap")
	ErrStoreFingerprint       = errors.New("commercial: entitlement fingerprint mismatch")
)

// StoreCode is the stable machine-readable class of a persistence refusal.
type StoreCode string

const (
	StoreInvalidCode           StoreCode = "INVALID"
	StoreNotFoundCode          StoreCode = "NOT_FOUND"
	StoreDuplicateRevisionCode StoreCode = "DUPLICATE_REVISION"
	StoreStaleCASCode          StoreCode = "STALE_CAS"
	StoreFingerprintCode       StoreCode = "FINGERPRINT_MISMATCH"
)

// StoreError carries a typed fault without exposing a PostgreSQL driver error
// to domain callers.
type StoreError struct {
	Code             StoreCode
	Detail           string
	Expected, Actual uint64
	Err              error
}

func (e *StoreError) Error() string {
	if e == nil {
		return "commercial: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("commercial: %s", e.Code)
	}
	return fmt.Sprintf("commercial: %s: %s", e.Code, e.Detail)
}

func (e *StoreError) Unwrap() error {
	if e == nil {
		return nil
	}
	var base error
	switch e.Code {
	case StoreInvalidCode:
		base = ErrStoreInvalid
	case StoreNotFoundCode:
		base = ErrStoreNotFound
	case StoreDuplicateRevisionCode:
		base = ErrStoreDuplicateRevision
	case StoreStaleCASCode:
		base = ErrStoreStaleCAS
	case StoreFingerprintCode:
		base = ErrStoreFingerprint
	}
	if e.Err != nil {
		return errors.Join(base, e.Err)
	}
	return base
}

// MemoryStore is the kernel-pure reference implementation of
// ContractRevisionStore. It is also useful to compose the commercial package
// without granting it database authority.
type MemoryStore struct {
	mu        sync.RWMutex
	contracts map[string]map[uint64]ContractRevision
	snapshots map[string]EntitlementSnapshot
}

var _ ContractRevisionStore = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{contracts: make(map[string]map[uint64]ContractRevision), snapshots: make(map[string]EntitlementSnapshot)}
}

func contractStoreKey(tenant, id string) string { return tenant + "\x00" + id }
func snapshotStoreKey(tenant, id string, revision uint64) string {
	return fmt.Sprintf("%s\x00%s\x00%d", tenant, id, revision)
}

func storeContext(ctx context.Context) error {
	if ctx == nil {
		return &StoreError{Code: StoreInvalidCode, Detail: "context is required"}
	}
	return ctx.Err()
}

func (s *MemoryStore) PutContractRevision(ctx context.Context, c ContractRevision, expected ...uint64) error {
	if err := storeContext(ctx); err != nil {
		return err
	}
	if s == nil || c.TenantID == "" || c.ContractID == "" {
		return &StoreError{Code: StoreInvalidCode, Detail: "tenant and contract id are required"}
	}
	if err := c.Validate(); err != nil {
		return &StoreError{Code: StoreInvalidCode, Detail: err.Error(), Err: err}
	}
	if len(expected) > 1 {
		return &StoreError{Code: StoreInvalidCode, Detail: "at most one expected revision is allowed"}
	}
	want := uint64(0)
	if len(expected) == 1 {
		want = expected[0]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := contractStoreKey(c.TenantID, c.ContractID)
	history := s.contracts[key]
	latest := uint64(0)
	for revision := range history {
		if revision > latest {
			latest = revision
		}
	}
	if c.Revision <= latest {
		return &StoreError{Code: StoreDuplicateRevisionCode, Detail: "contract revision already exists", Expected: want, Actual: latest, Err: ErrStoreDuplicateRevision}
	}
	if want != latest || c.Revision != latest+1 {
		return &StoreError{Code: StoreStaleCASCode, Detail: "contract revision does not follow the current tip", Expected: want, Actual: latest, Err: ErrStoreStaleCAS}
	}
	if history == nil {
		history = make(map[uint64]ContractRevision)
		s.contracts[key] = history
	}
	history[c.Revision] = c.clone()
	return nil
}

func (s *MemoryStore) GetContractRevision(ctx context.Context, tenant, id string, revision uint64) (ContractRevision, error) {
	if err := storeContext(ctx); err != nil {
		return ContractRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.contracts[contractStoreKey(tenant, id)][revision]
	if !ok {
		return ContractRevision{}, &StoreError{Code: StoreNotFoundCode, Detail: "contract revision not found", Err: ErrStoreNotFound}
	}
	return c.clone(), nil
}

func (s *MemoryStore) ListContractRevisions(ctx context.Context, tenant, id string) ([]ContractRevision, error) {
	if err := storeContext(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	history := s.contracts[contractStoreKey(tenant, id)]
	if len(history) == 0 {
		return nil, &StoreError{Code: StoreNotFoundCode, Detail: "contract revisions not found", Err: ErrStoreNotFound}
	}
	out := make([]ContractRevision, 0, len(history))
	for revision := uint64(1); revision <= uint64(len(history)); revision++ {
		if c, ok := history[revision]; ok {
			out = append(out, c.clone())
		}
	}
	return out, nil
}

func (s *MemoryStore) PutEntitlementSnapshot(ctx context.Context, snapshot EntitlementSnapshot) error {
	if err := storeContext(ctx); err != nil {
		return err
	}
	if s == nil {
		return &StoreError{Code: StoreInvalidCode, Detail: "store is nil"}
	}
	if err := snapshot.Validate(); err != nil {
		return &StoreError{Code: StoreInvalidCode, Detail: err.Error(), Err: err}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := contractStoreKey(snapshot.TenantID(), snapshot.ContractID())
	if _, ok := s.contracts[key][snapshot.Revision()]; !ok {
		return &StoreError{Code: StoreNotFoundCode, Detail: "named contract revision not found", Err: ErrStoreNotFound}
	}
	s.snapshots[snapshotStoreKey(snapshot.TenantID(), snapshot.ContractID(), snapshot.Revision())] = snapshot
	return nil
}

func (s *MemoryStore) GetEntitlementSnapshot(ctx context.Context, tenant, id string, revision uint64) (EntitlementSnapshot, error) {
	if err := storeContext(ctx); err != nil {
		return EntitlementSnapshot{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, ok := s.snapshots[snapshotStoreKey(tenant, id, revision)]
	if !ok {
		return EntitlementSnapshot{}, &StoreError{Code: StoreNotFoundCode, Detail: "entitlement snapshot not found", Err: ErrStoreNotFound}
	}
	return snapshot, nil
}
