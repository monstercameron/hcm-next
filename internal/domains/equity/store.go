package equity

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Store is the tenant-aware persistence port for equity's immutable plan and
// grant revisions and its acceptance evidence. Implementations append a new
// revision; they never replace an existing revision in place.
type Store interface {
	SavePlan(context.Context, string, EquityPlanRevision) error
	LoadPlan(context.Context, string, string, uint64) (EquityPlanRevision, error)
	ListPlans(context.Context, string, string) ([]EquityPlanRevision, error)
	SaveGrant(context.Context, string, EquityGrant) error
	LoadGrant(context.Context, string, string, uint64) (EquityGrant, error)
	ListGrants(context.Context, string, string) ([]EquityGrant, error)
	RecordAcceptance(context.Context, string, AcceptanceEvent) error
	ListAcceptanceEvents(context.Context, string, string) ([]AcceptanceEvent, error)
}

// StoreErrorCode is the stable machine-readable classification of a store
// refusal. The data adapter returns the same classifications as this pure
// reference implementation, so callers do not inspect a database error.
type StoreErrorCode string

const (
	StoreInvalidCode      StoreErrorCode = "INVALID"
	StoreNotFoundCode     StoreErrorCode = "NOT_FOUND"
	StoreDuplicateCode    StoreErrorCode = "DUPLICATE_REVISION"
	StoreStaleCASCode     StoreErrorCode = "STALE_CAS"
	StoreIntegrityCode    StoreErrorCode = "INTEGRITY"
	StorePlanMismatchCode StoreErrorCode = "PLAN_MISMATCH"
)

var (
	ErrStoreInvalid      = errors.New("equity: invalid store input")
	ErrStoreNotFound     = errors.New("equity: stored equity row not found")
	ErrStoreDuplicate    = errors.New("equity: duplicate revision or event")
	ErrStoreStaleCAS     = errors.New("equity: stale compare-and-swap")
	ErrStoreIntegrity    = errors.New("equity: stored equity row failed integrity")
	ErrStorePlanMismatch = errors.New("equity: grant plan binding mismatch")
)

// StoreError carries a stable code while preserving errors.Is matching to the
// domain sentinel for that class of refusal.
type StoreError struct {
	Code   StoreErrorCode
	Detail string
}

func (e *StoreError) Error() string {
	if e == nil {
		return "equity: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("equity: %s", e.Code)
	}
	return fmt.Sprintf("equity: %s: %s", e.Code, e.Detail)
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
	case StoreIntegrityCode:
		return ErrStoreIntegrity
	case StorePlanMismatchCode:
		return ErrStorePlanMismatch
	default:
		return nil
	}
}

func storeError(code StoreErrorCode, detail string) *StoreError {
	return &StoreError{Code: code, Detail: detail}
}

// MemoryStore is the kernel-pure reference implementation of Store. It is
// useful for domain tests and keeps the domain package usable without a
// database; the PostgreSQL adapter in internal/data/equitystore has the same
// duplicate, lineage, tenant, and plan-binding rules.
type MemoryStore struct {
	mu     sync.RWMutex
	plans  map[string]map[string]map[uint64]EquityPlanRevision
	grants map[string]map[string]map[uint64]EquityGrant
	events map[string]map[string][]AcceptanceEvent
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		plans:  make(map[string]map[string]map[uint64]EquityPlanRevision),
		grants: make(map[string]map[string]map[uint64]EquityGrant),
		events: make(map[string]map[string][]AcceptanceEvent),
	}
}

func (s *MemoryStore) SavePlan(ctx context.Context, tenantID string, plan EquityPlanRevision) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if s == nil || strings.TrimSpace(tenantID) == "" {
		return storeError(StoreInvalidCode, "tenant id is required")
	}
	var err error
	if plan.CanonicalDigest == "" {
		plan, err = NewEquityPlanRevision(plan)
	} else {
		err = plan.Validate()
	}
	if err != nil {
		return storeError(StoreInvalidCode, err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := ensurePlanMap(s.plans, tenantID, plan.PlanID)
	if _, exists := byID[plan.Revision]; exists {
		return storeError(StoreDuplicateCode, fmt.Sprintf("plan %s revision %d", plan.PlanID, plan.Revision))
	}
	current, hasCurrent := latestPlan(byID)
	if !hasCurrent {
		if plan.Revision != 1 || plan.SupersedesRevision != 0 || plan.ParentDigest != "" {
			return storeError(StoreStaleCASCode, "initial plan must be revision 1 without lineage")
		}
	} else if plan.Revision != current.Revision+1 || plan.SupersedesRevision != current.Revision || plan.ParentDigest != current.CanonicalDigest {
		return storeError(StoreStaleCASCode, fmt.Sprintf("plan %s current revision is %d", plan.PlanID, current.Revision))
	}
	byID[plan.Revision] = plan
	return nil
}

func (s *MemoryStore) LoadPlan(ctx context.Context, tenantID, planID string, revision uint64) (EquityPlanRevision, error) {
	if err := contextErr(ctx); err != nil {
		return EquityPlanRevision{}, err
	}
	if s == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(planID) == "" || revision == 0 {
		return EquityPlanRevision{}, storeError(StoreInvalidCode, "tenant, plan id and positive revision are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	plan, ok := s.plans[tenantID][planID][revision]
	if !ok {
		return EquityPlanRevision{}, storeError(StoreNotFoundCode, fmt.Sprintf("plan %s revision %d", planID, revision))
	}
	return plan, nil
}

func (s *MemoryStore) ListPlans(ctx context.Context, tenantID, planID string) ([]EquityPlanRevision, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if s == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(planID) == "" {
		return nil, storeError(StoreInvalidCode, "tenant and plan id are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	byID := s.plans[tenantID][planID]
	out := make([]EquityPlanRevision, 0, len(byID))
	for _, plan := range byID {
		out = append(out, plan)
	}
	if len(out) == 0 {
		return nil, storeError(StoreNotFoundCode, fmt.Sprintf("plan %s", planID))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	return out, nil
}

func (s *MemoryStore) SaveGrant(ctx context.Context, tenantID string, grant EquityGrant) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if s == nil || strings.TrimSpace(tenantID) == "" {
		return storeError(StoreInvalidCode, "tenant id is required")
	}
	var err error
	if grant.CanonicalDigest == "" {
		grant, err = NewEquityGrant(grant)
	} else {
		err = grant.Validate()
	}
	if err != nil {
		return storeError(StoreInvalidCode, err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, ok := findPlanDigest(s.plans[tenantID], grant.PlanDigest, grant.PlanRevision)
	if !ok {
		return storeError(StorePlanMismatchCode, "grant names no plan revision with that digest")
	}
	if err := plan.ValidateGrant(grant); err != nil {
		return storeError(StorePlanMismatchCode, err.Error())
	}
	byID := ensureGrantMap(s.grants, tenantID, grant.GrantID)
	if _, exists := byID[grant.Revision]; exists {
		return storeError(StoreDuplicateCode, fmt.Sprintf("grant %s revision %d", grant.GrantID, grant.Revision))
	}
	current, hasCurrent := latestGrant(byID)
	if !hasCurrent {
		if grant.Revision != 1 || grant.SupersedesRevision != 0 || grant.ParentDigest != "" {
			return storeError(StoreStaleCASCode, "initial grant must be revision 1 without lineage")
		}
	} else if grant.Revision != current.Revision+1 || grant.SupersedesRevision != current.Revision || grant.ParentDigest != current.CanonicalDigest {
		return storeError(StoreStaleCASCode, fmt.Sprintf("grant %s current revision is %d", grant.GrantID, current.Revision))
	}
	byID[grant.Revision] = grant
	return nil
}

func (s *MemoryStore) LoadGrant(ctx context.Context, tenantID, grantID string, revision uint64) (EquityGrant, error) {
	if err := contextErr(ctx); err != nil {
		return EquityGrant{}, err
	}
	if s == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(grantID) == "" || revision == 0 {
		return EquityGrant{}, storeError(StoreInvalidCode, "tenant, grant id and positive revision are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	grant, ok := s.grants[tenantID][grantID][revision]
	if !ok {
		return EquityGrant{}, storeError(StoreNotFoundCode, fmt.Sprintf("grant %s revision %d", grantID, revision))
	}
	return grant, nil
}

func (s *MemoryStore) ListGrants(ctx context.Context, tenantID, grantID string) ([]EquityGrant, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if s == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(grantID) == "" {
		return nil, storeError(StoreInvalidCode, "tenant and grant id are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	byID := s.grants[tenantID][grantID]
	out := make([]EquityGrant, 0, len(byID))
	for _, grant := range byID {
		out = append(out, grant)
	}
	if len(out) == 0 {
		return nil, storeError(StoreNotFoundCode, fmt.Sprintf("grant %s", grantID))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	return out, nil
}

func (s *MemoryStore) RecordAcceptance(ctx context.Context, tenantID string, event AcceptanceEvent) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if s == nil || strings.TrimSpace(tenantID) == "" {
		return storeError(StoreInvalidCode, "tenant id is required")
	}
	var err error
	if event.CanonicalDigest == "" {
		event, err = NewAcceptanceEvent(event)
	} else {
		err = event.Validate()
	}
	if err != nil {
		return storeError(StoreInvalidCode, err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := findPlanDigest(s.plans[tenantID], event.GrantDigest, 0); ok {
		return storeError(StoreIntegrityCode, "acceptance event digest names a plan, not a grant")
	}
	grantID, ok := findGrantDigest(s.grants[tenantID], event.GrantDigest, event.GrantRevision)
	if !ok {
		return storeError(StoreIntegrityCode, "acceptance event names no stored grant revision")
	}
	byDigest := s.events[tenantID]
	if byDigest == nil {
		byDigest = make(map[string][]AcceptanceEvent)
		s.events[tenantID] = byDigest
	}
	for _, existing := range byDigest[event.GrantDigest] {
		if existing.EventID == event.EventID {
			return storeError(StoreDuplicateCode, fmt.Sprintf("acceptance event %s", event.EventID))
		}
	}
	byDigest[event.GrantDigest] = append(byDigest[event.GrantDigest], event)
	_ = grantID
	return nil
}

func (s *MemoryStore) ListAcceptanceEvents(ctx context.Context, tenantID, grantDigest string) ([]AcceptanceEvent, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if s == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(grantDigest) == "" {
		return nil, storeError(StoreInvalidCode, "tenant and grant digest are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	events := append([]AcceptanceEvent(nil), s.events[tenantID][grantDigest]...)
	if len(events) == 0 {
		return nil, storeError(StoreNotFoundCode, "acceptance events are absent")
	}
	return events, nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func ensurePlanMap(all map[string]map[string]map[uint64]EquityPlanRevision, tenant, id string) map[uint64]EquityPlanRevision {
	byTenant := all[tenant]
	if byTenant == nil {
		byTenant = make(map[string]map[uint64]EquityPlanRevision)
		all[tenant] = byTenant
	}
	byID := byTenant[id]
	if byID == nil {
		byID = make(map[uint64]EquityPlanRevision)
		byTenant[id] = byID
	}
	return byID
}

func ensureGrantMap(all map[string]map[string]map[uint64]EquityGrant, tenant, id string) map[uint64]EquityGrant {
	byTenant := all[tenant]
	if byTenant == nil {
		byTenant = make(map[string]map[uint64]EquityGrant)
		all[tenant] = byTenant
	}
	byID := byTenant[id]
	if byID == nil {
		byID = make(map[uint64]EquityGrant)
		byTenant[id] = byID
	}
	return byID
}

func latestPlan(byRevision map[uint64]EquityPlanRevision) (EquityPlanRevision, bool) {
	var out EquityPlanRevision
	var ok bool
	for _, plan := range byRevision {
		if !ok || plan.Revision > out.Revision {
			out, ok = plan, true
		}
	}
	return out, ok
}

func latestGrant(byRevision map[uint64]EquityGrant) (EquityGrant, bool) {
	var out EquityGrant
	var ok bool
	for _, grant := range byRevision {
		if !ok || grant.Revision > out.Revision {
			out, ok = grant, true
		}
	}
	return out, ok
}

func findPlanDigest(byID map[string]map[uint64]EquityPlanRevision, digest string, revision uint64) (EquityPlanRevision, bool) {
	for _, byRevision := range byID {
		for _, plan := range byRevision {
			if plan.CanonicalDigest == digest && (revision == 0 || plan.Revision == revision) {
				return plan, true
			}
		}
	}
	return EquityPlanRevision{}, false
}

func findGrantDigest(byID map[string]map[uint64]EquityGrant, digest string, revision uint64) (string, bool) {
	for grantID, byRevision := range byID {
		for _, grant := range byRevision {
			if grant.CanonicalDigest == digest && grant.Revision == revision {
				return grantID, true
			}
		}
	}
	return "", false
}
