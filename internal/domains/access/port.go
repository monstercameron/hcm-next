package access

import (
	"context"
	"errors"
	"sync"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Repository is the access domain persistence boundary. Authoritative graph
// revisions and provider observations have separate operations.
type Repository interface {
	Add(ctx context.Context, record Record) error
	Observe(ctx context.Context, observation ExternalAccessObservation) error
	Snapshot(ctx context.Context, tenant values.TenantId) (Graph, error)
}

// MemoryStore is the in-memory repository used by kernel callers and tests.
// Observations remain outside the authoritative Graph by construction.
type MemoryStore struct {
	mu           sync.RWMutex
	graphs       map[values.TenantId]Graph
	observations map[values.TenantId]map[string]ExternalAccessObservation
}

var _ Repository = (*MemoryStore)(nil)

// Add appends one authoritative graph revision.
func (s *MemoryStore) Add(ctx context.Context, record Record) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if record == nil {
		return ErrInvalidGraph
	}
	var tenant values.TenantId
	switch r := record.(type) {
	case WorkforceIdentity:
		tenant = r.Tenant
	case *WorkforceIdentity:
		if r != nil {
			tenant = r.Tenant
		}
	case AccountLink:
		tenant = r.Tenant
	case *AccountLink:
		if r != nil {
			tenant = r.Tenant
		}
	case EntitlementDefinition:
		tenant = r.Tenant
	case *EntitlementDefinition:
		if r != nil {
			tenant = r.Tenant
		}
	case ExpectedEntitlement:
		tenant = r.Tenant
	case *ExpectedEntitlement:
		if r != nil {
			tenant = r.Tenant
		}
	default:
		return ErrInvalidGraph
	}
	if tenant == "" {
		return ErrInvalidGraph
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.graphs == nil {
		s.graphs = make(map[values.TenantId]Graph)
	}
	graph, ok := s.graphs[tenant]
	if !ok {
		var err error
		graph, err = NewGraph(tenant)
		if err != nil {
			return err
		}
	}
	if err := graph.Add(record); err != nil {
		return err
	}
	s.graphs[tenant] = graph
	return nil
}

// Observe appends provider evidence outside the authoritative graph.
func (s *MemoryStore) Observe(ctx context.Context, observation ExternalAccessObservation) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := observation.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observations == nil {
		s.observations = make(map[values.TenantId]map[string]ExternalAccessObservation)
	}
	rows := s.observations[observation.Tenant]
	if rows == nil {
		rows = make(map[string]ExternalAccessObservation)
		s.observations[observation.Tenant] = rows
	}
	key := observation.ID + "\x00" + observation.Revision.String()
	if _, exists := rows[key]; exists {
		return ErrDuplicateRecord
	}
	rows[key] = observation
	return nil
}

// Snapshot returns a defensive copy of the authoritative graph.
func (s *MemoryStore) Snapshot(ctx context.Context, tenant values.TenantId) (Graph, error) {
	if err := contextErr(ctx); err != nil {
		return Graph{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	graph, ok := s.graphs[tenant]
	if !ok {
		return NewGraph(tenant)
	}
	return graph.clone(), nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return errors.New("access: nil context")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
