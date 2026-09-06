package succession

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// TenantID is the small identity capability used by the persistence port. A
// concrete UUID type belongs at the data boundary; the domain only needs its
// stable string representation.
type TenantID interface {
	String() string
}

// Store is the tenant-aware persistence port for the three immutable
// succession revision families. Implementations append revisions and reject a
// fork, an out-of-order revision, or a duplicate identity.
type Store interface {
	SaveCriticalRole(context.Context, TenantID, CriticalRole) error
	LoadCriticalRole(context.Context, TenantID, string, uint64) (CriticalRole, error)
	CurrentCriticalRole(context.Context, TenantID, string) (CriticalRole, error)
	SaveReadiness(context.Context, TenantID, SuccessorReadinessRevision) error
	LoadReadiness(context.Context, TenantID, string, uint64) (SuccessorReadinessRevision, error)
	CurrentReadiness(context.Context, TenantID, string) (SuccessorReadinessRevision, error)
	SaveSlate(context.Context, TenantID, SuccessionSlate) error
	LoadSlate(context.Context, TenantID, string, uint64) (SuccessionSlate, error)
	CurrentSlate(context.Context, TenantID, string) (SuccessionSlate, error)
}

// StoreErrorCode is the stable machine-readable classification of a
// persistence refusal.
type StoreErrorCode string

const (
	StoreInvalidCode   StoreErrorCode = "INVALID"
	StoreNotFoundCode  StoreErrorCode = "NOT_FOUND"
	StoreDuplicateCode StoreErrorCode = "DUPLICATE_REVISION"
	StoreStaleCASCode  StoreErrorCode = "STALE_CAS"
)

var (
	ErrStoreInvalid   = errors.New("succession: invalid store input")
	ErrStoreNotFound  = errors.New("succession: stored succession revision not found")
	ErrStoreDuplicate = errors.New("succession: duplicate revision")
	ErrStoreStaleCAS  = errors.New("succession: stale compare-and-swap")
)

// StoreError preserves the machine-readable refusal code and, for a stale
// append, the expected and actual heads.
type StoreError struct {
	Code             StoreErrorCode
	Detail           string
	Expected, Actual uint64
}

func (e *StoreError) Error() string {
	if e == nil {
		return "succession: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("succession: %s", e.Code)
	}
	return fmt.Sprintf("succession: %s: %s", e.Code, e.Detail)
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

func storeTenant(tenant TenantID) (string, error) {
	if tenant == nil || tenant.String() == "" {
		return "", storeError(StoreInvalidCode, "tenant id is required")
	}
	return tenant.String(), nil
}

func storeContext(ctx context.Context) error {
	if ctx == nil {
		return storeError(StoreInvalidCode, "context is required")
	}
	return ctx.Err()
}

func cloneCriticalRole(in CriticalRole) CriticalRole {
	in.EvidenceRefs = append([]string(nil), in.EvidenceRefs...)
	return in
}

func cloneReadiness(in SuccessorReadinessRevision) SuccessorReadinessRevision {
	in.EvidenceRefs = append([]string(nil), in.EvidenceRefs...)
	return in
}

func cloneSlate(in SuccessionSlate) SuccessionSlate {
	in.DeclaredScopes = append([]string(nil), in.DeclaredScopes...)
	in.Candidates = append([]SuccessorReadinessRevision(nil), in.Candidates...)
	for i := range in.Candidates {
		in.Candidates[i] = cloneReadiness(in.Candidates[i])
	}
	return in
}

func tenantRevisions[T any](root map[string]map[string]map[uint64]T, tenant, id string) map[uint64]T {
	byID := root[tenant]
	if byID == nil {
		byID = make(map[string]map[uint64]T)
		root[tenant] = byID
	}
	revisions := byID[id]
	if revisions == nil {
		revisions = make(map[uint64]T)
		byID[id] = revisions
	}
	return revisions
}

func headRevision[T any](revisions map[uint64]T) uint64 {
	var head uint64
	for revision := range revisions {
		if revision > head {
			head = revision
		}
	}
	return head
}

// SaveCriticalRole implements Store for the kernel-pure reference store.
func (m *MemorySlateStore) SaveCriticalRole(ctx context.Context, tenant TenantID, in CriticalRole) error {
	if err := storeContext(ctx); err != nil {
		return err
	}
	tenantKey, err := storeTenant(tenant)
	if err != nil {
		return err
	}
	if in.CanonicalDigest == "" {
		in, err = NewCriticalRole(in)
	} else {
		err = in.Validate()
	}
	if err != nil {
		return storeError(StoreInvalidCode, err.Error())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	revisions := tenantRevisions(m.critical, tenantKey, in.RoleID)
	return appendCriticalRole(revisions, in)
}

func appendCriticalRole(revisions map[uint64]CriticalRole, in CriticalRole) error {
	if _, exists := revisions[in.Revision]; exists {
		return storeError(StoreDuplicateCode, fmt.Sprintf("critical role %s revision %d", in.RoleID, in.Revision))
	}
	head := headRevision(revisions)
	if head == 0 {
		if in.Revision != 1 {
			return &StoreError{Code: StoreStaleCASCode, Expected: 0, Actual: 0, Detail: "first critical role revision must be 1"}
		}
	} else if in.Revision != head+1 || in.ParentRevision != head {
		return &StoreError{Code: StoreStaleCASCode, Expected: head, Actual: head, Detail: "critical role revision does not extend current head"}
	} else if prior := revisions[head]; in.ParentDigest != prior.CanonicalDigest {
		return &StoreError{Code: StoreStaleCASCode, Expected: head, Actual: head, Detail: "critical role parent digest is stale"}
	}
	revisions[in.Revision] = cloneCriticalRole(in)
	return nil
}

// LoadCriticalRole implements Store for the kernel-pure reference store.
func (m *MemorySlateStore) LoadCriticalRole(ctx context.Context, tenant TenantID, id string, revision uint64) (CriticalRole, error) {
	if err := storeContext(ctx); err != nil {
		return CriticalRole{}, err
	}
	tenantKey, err := storeTenant(tenant)
	if err != nil {
		return CriticalRole{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	in, ok := m.critical[tenantKey][id][revision]
	if !ok {
		return CriticalRole{}, storeError(StoreNotFoundCode, fmt.Sprintf("critical role %s revision %d", id, revision))
	}
	return cloneCriticalRole(in), nil
}

func (m *MemorySlateStore) CurrentCriticalRole(ctx context.Context, tenant TenantID, id string) (CriticalRole, error) {
	if err := storeContext(ctx); err != nil {
		return CriticalRole{}, err
	}
	tenantKey, err := storeTenant(tenant)
	if err != nil {
		return CriticalRole{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	revisions := m.critical[tenantKey][id]
	head := headRevision(revisions)
	in, ok := revisions[head]
	if !ok {
		return CriticalRole{}, storeError(StoreNotFoundCode, fmt.Sprintf("critical role %s", id))
	}
	return cloneCriticalRole(in), nil
}

// SaveReadiness implements Store for the kernel-pure reference store.
func (m *MemorySlateStore) SaveReadiness(ctx context.Context, tenant TenantID, in SuccessorReadinessRevision) error {
	if err := storeContext(ctx); err != nil {
		return err
	}
	tenantKey, err := storeTenant(tenant)
	if err != nil {
		return err
	}
	if in.CanonicalDigest == "" {
		in, err = NewSuccessorReadinessRevision(in)
	} else {
		err = in.Validate()
	}
	if err != nil {
		return storeError(StoreInvalidCode, err.Error())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	revisions := tenantRevisions(m.readiness, tenantKey, in.SuccessorID)
	if _, exists := revisions[in.Revision]; exists {
		return storeError(StoreDuplicateCode, fmt.Sprintf("readiness %s revision %d", in.SuccessorID, in.Revision))
	}
	head := headRevision(revisions)
	if head == 0 {
		if in.Revision != 1 {
			return &StoreError{Code: StoreStaleCASCode, Detail: "first readiness revision must be 1"}
		}
	} else if in.Revision != head+1 || in.ParentRevision != head || in.ParentDigest != revisions[head].CanonicalDigest {
		return &StoreError{Code: StoreStaleCASCode, Expected: head, Actual: head, Detail: "readiness revision does not extend current head"}
	}
	revisions[in.Revision] = cloneReadiness(in)
	return nil
}

func (m *MemorySlateStore) LoadReadiness(ctx context.Context, tenant TenantID, id string, revision uint64) (SuccessorReadinessRevision, error) {
	if err := storeContext(ctx); err != nil {
		return SuccessorReadinessRevision{}, err
	}
	tenantKey, err := storeTenant(tenant)
	if err != nil {
		return SuccessorReadinessRevision{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	in, ok := m.readiness[tenantKey][id][revision]
	if !ok {
		return SuccessorReadinessRevision{}, storeError(StoreNotFoundCode, fmt.Sprintf("readiness %s revision %d", id, revision))
	}
	return cloneReadiness(in), nil
}

func (m *MemorySlateStore) CurrentReadiness(ctx context.Context, tenant TenantID, id string) (SuccessorReadinessRevision, error) {
	if err := storeContext(ctx); err != nil {
		return SuccessorReadinessRevision{}, err
	}
	tenantKey, err := storeTenant(tenant)
	if err != nil {
		return SuccessorReadinessRevision{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	revisions := m.readiness[tenantKey][id]
	head := headRevision(revisions)
	in, ok := revisions[head]
	if !ok {
		return SuccessorReadinessRevision{}, storeError(StoreNotFoundCode, fmt.Sprintf("readiness %s", id))
	}
	return cloneReadiness(in), nil
}

// SaveSlate implements Store for the kernel-pure reference store. Candidate
// readiness revisions are retained as their own immutable records as well.
func (m *MemorySlateStore) SaveSlate(ctx context.Context, tenant TenantID, in SuccessionSlate) error {
	if err := storeContext(ctx); err != nil {
		return err
	}
	tenantKey, err := storeTenant(tenant)
	if err != nil {
		return err
	}
	if in.CanonicalDigest == "" {
		in, err = NewSuccessionSlate(in)
	} else {
		err = in.Validate()
	}
	if err != nil {
		return storeError(StoreInvalidCode, err.Error())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, candidate := range in.Candidates {
		revisions := tenantRevisions(m.readiness, tenantKey, candidate.SuccessorID)
		if existing, exists := revisions[candidate.Revision]; exists {
			if existing.CanonicalDigest != candidate.CanonicalDigest {
				return storeError(StoreDuplicateCode, fmt.Sprintf("readiness %s revision %d", candidate.SuccessorID, candidate.Revision))
			}
			continue
		}
		if err := appendReadiness(revisions, candidate); err != nil {
			return err
		}
	}
	revisions := tenantRevisions(m.slates, tenantKey, in.SlateID)
	if _, exists := revisions[in.Revision]; exists {
		return storeError(StoreDuplicateCode, fmt.Sprintf("slate %s revision %d", in.SlateID, in.Revision))
	}
	head := headRevision(revisions)
	if head == 0 {
		if in.Revision != 1 {
			return &StoreError{Code: StoreStaleCASCode, Detail: "first slate revision must be 1"}
		}
	} else if in.Revision != head+1 || in.ParentRevision != head || in.ParentDigest != revisions[head].CanonicalDigest {
		return &StoreError{Code: StoreStaleCASCode, Expected: head, Actual: head, Detail: "slate revision does not extend current head"}
	}
	revisions[in.Revision] = cloneSlate(in)
	m.current[in.CriticalRoleID] = cloneSlate(in)
	return nil
}

func appendReadiness(revisions map[uint64]SuccessorReadinessRevision, in SuccessorReadinessRevision) error {
	if in.Revision == 1 {
		revisions[in.Revision] = cloneReadiness(in)
		return nil
	}
	head := headRevision(revisions)
	if in.Revision != head+1 || in.ParentRevision != head || in.ParentDigest != revisions[head].CanonicalDigest {
		return &StoreError{Code: StoreStaleCASCode, Expected: head, Actual: head, Detail: "readiness candidate does not extend current head"}
	}
	revisions[in.Revision] = cloneReadiness(in)
	return nil
}

func (m *MemorySlateStore) LoadSlate(ctx context.Context, tenant TenantID, id string, revision uint64) (SuccessionSlate, error) {
	if err := storeContext(ctx); err != nil {
		return SuccessionSlate{}, err
	}
	tenantKey, err := storeTenant(tenant)
	if err != nil {
		return SuccessionSlate{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	in, ok := m.slates[tenantKey][id][revision]
	if !ok {
		return SuccessionSlate{}, storeError(StoreNotFoundCode, fmt.Sprintf("slate %s revision %d", id, revision))
	}
	return cloneSlate(in), nil
}

func (m *MemorySlateStore) CurrentSlate(ctx context.Context, tenant TenantID, id string) (SuccessionSlate, error) {
	if err := storeContext(ctx); err != nil {
		return SuccessionSlate{}, err
	}
	tenantKey, err := storeTenant(tenant)
	if err != nil {
		return SuccessionSlate{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	revisions := m.slates[tenantKey][id]
	head := headRevision(revisions)
	in, ok := revisions[head]
	if !ok {
		return SuccessionSlate{}, storeError(StoreNotFoundCode, fmt.Sprintf("slate %s", id))
	}
	return cloneSlate(in), nil
}

var _ Store = (*MemorySlateStore)(nil)

// SortedRevisions is a small pure helper for callers presenting history.
func SortedRevisions[T any](in map[uint64]T) []uint64 {
	out := make([]uint64, 0, len(in))
	for revision := range in {
		out = append(out, revision)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
