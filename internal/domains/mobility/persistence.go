package mobility

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TenantPlanStore is the tenant-aware persistence port for durable mobility
// plans and their immigration milestone evidence. The existing PlanStore is
// intentionally retained for callers that only need a pure, unscoped plan
// value; adapters use this port when tenant isolation is part of the contract.
type TenantPlanStore interface {
	PutForTenant(context.Context, values.TenantId, MobilityPlan) error
	GetForTenant(context.Context, values.TenantId, string, uint64) (MobilityPlan, error)
	AppendImmigrationMilestoneForTenant(context.Context, values.TenantId, ImmigrationMilestone, uint64) error
	ListImmigrationMilestonesForTenant(context.Context, values.TenantId, string) ([]ImmigrationMilestone, error)
}

var (
	ErrPlanDuplicate            = errors.New("mobility: plan revision already exists")
	ErrPlanVersionConflict      = errors.New("mobility: plan revision conflict")
	ErrMilestoneDuplicate       = errors.New("mobility: immigration milestone already exists")
	ErrMilestoneVersionConflict = errors.New("mobility: immigration milestone sequence conflict")
)

type tenantMemoryState struct {
	mu         sync.RWMutex
	plans      map[string]MobilityPlan
	milestones map[string][]immigrationRecord
}

var tenantMemoryStates sync.Map // map[*InMemoryPlanStore]*tenantMemoryState

func tenantState(s *InMemoryPlanStore) *tenantMemoryState {
	if state, ok := tenantMemoryStates.Load(s); ok {
		return state.(*tenantMemoryState)
	}
	state := &tenantMemoryState{plans: make(map[string]MobilityPlan), milestones: make(map[string][]immigrationRecord)}
	actual, _ := tenantMemoryStates.LoadOrStore(s, state)
	return actual.(*tenantMemoryState)
}

var _ TenantPlanStore = (*InMemoryPlanStore)(nil)

func (s *InMemoryPlanStore) PutForTenant(ctx context.Context, tenant values.TenantId, p MobilityPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return ErrPlanNotFound
	}
	if err := tenant.Validate(); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	state := tenantState(s)
	state.mu.Lock()
	defer state.mu.Unlock()
	key := tenantPlanKey(tenant, p.id(), p.Revision)
	if _, exists := state.plans[key]; exists {
		return ErrPlanDuplicate
	}
	if p.Revision > 1 {
		latest, ok := latestTenantPlan(state, tenant, p.id())
		if !ok || latest.Revision != p.ParentRevision || latest.CanonicalDigest != p.ParentDigest {
			return ErrPlanVersionConflict
		}
	}
	state.plans[key] = clonePlan(p)
	return nil
}

func (s *InMemoryPlanStore) GetForTenant(ctx context.Context, tenant values.TenantId, id string, revision uint64) (MobilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return MobilityPlan{}, err
	}
	if s == nil {
		return MobilityPlan{}, ErrPlanNotFound
	}
	if err := tenant.Validate(); err != nil {
		return MobilityPlan{}, err
	}
	state := tenantState(s)
	state.mu.RLock()
	defer state.mu.RUnlock()
	p, ok := state.plans[tenantPlanKey(tenant, id, revision)]
	if !ok {
		return MobilityPlan{}, ErrPlanNotFound
	}
	return clonePlan(p), nil
}

func (s *InMemoryPlanStore) AppendImmigrationMilestoneForTenant(ctx context.Context, tenant values.TenantId, m ImmigrationMilestone, sequence uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return ErrMilestoneVersionConflict
	}
	if err := tenant.Validate(); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if sequence == 0 {
		return ErrMilestoneVersionConflict
	}
	state := tenantState(s)
	state.mu.Lock()
	defer state.mu.Unlock()
	key := tenant.String() + "\x00" + m.ProcessRef
	items := state.milestones[key]
	if len(items) > 0 && sequence <= items[len(items)-1].sequence {
		if sequence == items[len(items)-1].sequence {
			return ErrMilestoneDuplicate
		}
		return ErrMilestoneVersionConflict
	}
	if sequence != uint64(len(items)+1) {
		return ErrMilestoneVersionConflict
	}
	state.milestones[key] = append(items, immigrationRecord{sequence: sequence, milestone: m})
	return nil
}

func (s *InMemoryPlanStore) ListImmigrationMilestonesForTenant(ctx context.Context, tenant values.TenantId, processRef string) ([]ImmigrationMilestone, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrPlanNotFound
	}
	if err := tenant.Validate(); err != nil {
		return nil, err
	}
	state := tenantState(s)
	state.mu.RLock()
	defer state.mu.RUnlock()
	items := state.milestones[tenant.String()+"\x00"+processRef]
	if len(items) == 0 {
		return nil, ErrPlanNotFound
	}
	out := make([]ImmigrationMilestone, len(items))
	for i, item := range items {
		out[i] = item.milestone
	}
	return out, nil
}

type immigrationRecord struct {
	sequence  uint64
	milestone ImmigrationMilestone
}

func latestTenantPlan(state *tenantMemoryState, tenant values.TenantId, id string) (MobilityPlan, bool) {
	var latest MobilityPlan
	found := false
	prefix := tenant.String() + "\x00" + id + "\x00"
	for key, p := range state.plans {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix && (!found || p.Revision > latest.Revision) {
			latest, found = p, true
		}
	}
	return latest, found
}

func tenantPlanKey(tenant values.TenantId, id string, revision uint64) string {
	return fmt.Sprintf("%s\x00%s\x00%d", tenant, id, revision)
}

func clonePlan(p MobilityPlan) MobilityPlan {
	p.HostAssignments = append([]AssignmentRevision(nil), p.HostAssignments...)
	p.Legs = append([]MobilityLeg(nil), p.Legs...)
	p.Immigration = append([]ImmigrationMilestone(nil), p.Immigration...)
	p.Obligations = append([]MobilityObligation(nil), p.Obligations...)
	return p
}
