package appointment

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Repository is the appointment persistence boundary. Revisions are
// append-only: expectedRevision, when supplied, is the current highest
// revision and the submitted revision must be its immediate successor.
type Repository interface {
	PutRequirement(context.Context, values.TenantId, Requirement, ...uint64) error
	GetRequirement(context.Context, values.TenantId, string, uint64) (Requirement, error)
	ListRequirementVersions(context.Context, values.TenantId, string) ([]Requirement, error)
	PutResourceType(context.Context, values.TenantId, ResourceType, ...uint64) error
	GetResourceType(context.Context, values.TenantId, string, uint64) (ResourceType, error)
	ListResourceTypeVersions(context.Context, values.TenantId, string) ([]ResourceType, error)
}

var (
	ErrRequirementNotFound  = errors.New("appointment: requirement not found")
	ErrResourceTypeNotFound = errors.New("appointment: resource type not found")
	ErrDuplicateRevision    = errors.New("appointment: duplicate revision")
	ErrVersionConflict      = errors.New("appointment: version conflict")
	ErrTenantMismatch       = errors.New("appointment: tenant mismatch")
)

// MemoryStore is the kernel-pure repository used by local composition and
// tests. It retains immutable defensive copies and performs no I/O.
type MemoryStore struct {
	mu           sync.RWMutex
	requirements map[string]Requirement
	resources    map[string]ResourceType
}

var _ Repository = (*MemoryStore)(nil)

// NewMemoryStore returns an empty in-memory appointment repository.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		requirements: make(map[string]Requirement),
		resources:    make(map[string]ResourceType),
	}
}

func appointmentKey(tenant, id string, revision uint64) string {
	return tenant + "\x00" + id + fmt.Sprintf("\x00%d", revision)
}

func (s *MemoryStore) PutRequirement(ctx context.Context, tenant values.TenantId, in Requirement, expected ...uint64) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return errors.New("appointment: nil memory store")
	}
	if err := tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrTenantMismatch, err)
	}
	if len(expected) > 1 {
		return fmt.Errorf("%w: one expected revision is allowed", ErrVersionConflict)
	}
	if in.Revision == 0 {
		published, err := Publish(in)
		if err != nil {
			return err
		}
		in = published.Requirement
		in.Revision = 1
	} else if err := in.Validate(); err != nil {
		return err
	}
	if requestTenant := in.RequestTenant(); requestTenant != "tenant-placeholder" && requestTenant != tenant {
		return ErrTenantMismatch
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.requirements == nil {
		s.requirements = make(map[string]Requirement)
	}
	latest := latestRequirementRevision(s.requirements, tenant, in.ID)
	if len(expected) == 1 && expected[0] != latest {
		return fmt.Errorf("%w: requirement %s expected %d, current %d", ErrVersionConflict, in.ID, expected[0], latest)
	}
	if in.Revision <= latest {
		return fmt.Errorf("%w: requirement %s revision %d", ErrDuplicateRevision, in.ID, in.Revision)
	}
	if in.Revision != latest+1 {
		return fmt.Errorf("%w: requirement %s requires revision %d, got %d", ErrVersionConflict, in.ID, latest+1, in.Revision)
	}
	s.requirements[appointmentKey(string(tenant), in.ID, in.Revision)] = cloneRequirement(in)
	return nil
}

func (s *MemoryStore) GetRequirement(ctx context.Context, tenant values.TenantId, id string, revision uint64) (Requirement, error) {
	if err := contextErr(ctx); err != nil {
		return Requirement{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	in, ok := s.requirements[appointmentKey(string(tenant), id, revision)]
	if !ok {
		return Requirement{}, ErrRequirementNotFound
	}
	return cloneRequirement(in), nil
}

func (s *MemoryStore) ListRequirementVersions(ctx context.Context, tenant values.TenantId, id string) ([]Requirement, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Requirement
	for key, in := range s.requirements {
		if len(key) >= len(string(tenant))+1 && key[:len(string(tenant))+1] == string(tenant)+"\x00" && in.ID == id {
			out = append(out, cloneRequirement(in))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	if len(out) == 0 {
		return nil, ErrRequirementNotFound
	}
	return out, nil
}

func (s *MemoryStore) PutResourceType(ctx context.Context, tenant values.TenantId, in ResourceType, expected ...uint64) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return errors.New("appointment: nil memory store")
	}
	if err := tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrTenantMismatch, err)
	}
	if len(expected) > 1 {
		return fmt.Errorf("%w: one expected revision is allowed", ErrVersionConflict)
	}
	normalized, err := NewResourceType(in)
	if err != nil {
		return err
	}
	in = normalized
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resources == nil {
		s.resources = make(map[string]ResourceType)
	}
	latest := latestResourceRevision(s.resources, tenant, in.ID)
	if len(expected) == 1 && expected[0] != latest {
		return fmt.Errorf("%w: resource type %s expected %d, current %d", ErrVersionConflict, in.ID, expected[0], latest)
	}
	if in.Revision <= latest {
		return fmt.Errorf("%w: resource type %s revision %d", ErrDuplicateRevision, in.ID, in.Revision)
	}
	if in.Revision != latest+1 {
		return fmt.Errorf("%w: resource type %s requires revision %d, got %d", ErrVersionConflict, in.ID, latest+1, in.Revision)
	}
	s.resources[appointmentKey(string(tenant), in.ID, in.Revision)] = in
	return nil
}

func (s *MemoryStore) GetResourceType(ctx context.Context, tenant values.TenantId, id string, revision uint64) (ResourceType, error) {
	if err := contextErr(ctx); err != nil {
		return ResourceType{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	in, ok := s.resources[appointmentKey(string(tenant), id, revision)]
	if !ok {
		return ResourceType{}, ErrResourceTypeNotFound
	}
	return in, nil
}

func (s *MemoryStore) ListResourceTypeVersions(ctx context.Context, tenant values.TenantId, id string) ([]ResourceType, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []ResourceType
	for key, in := range s.resources {
		if len(key) >= len(string(tenant))+1 && key[:len(string(tenant))+1] == string(tenant)+"\x00" && in.ID == id {
			out = append(out, in)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	if len(out) == 0 {
		return nil, ErrResourceTypeNotFound
	}
	return out, nil
}

func latestRequirementRevision(rows map[string]Requirement, tenant values.TenantId, id string) uint64 {
	var latest uint64
	for _, in := range rows {
		if in.ID == id && in.RequestTenant() == tenant && in.Revision > latest {
			latest = in.Revision
		}
	}
	return latest
}

func latestResourceRevision(rows map[string]ResourceType, tenant values.TenantId, id string) uint64 {
	var latest uint64
	prefix := string(tenant) + "\x00"
	for key, in := range rows {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix && in.ID == id && in.Revision > latest {
			latest = in.Revision
		}
	}
	return latest
}

func cloneRequirement(in Requirement) Requirement {
	in.Participants = append([]ParticipantRole(nil), in.Participants...)
	in.ParticipantRefs = append([]values.EntityRef(nil), in.ParticipantRefs...)
	in.Resources = append([]ResourceType(nil), in.Resources...)
	in.RequiredResources = append([]ResourceRequirement(nil), in.RequiredResources...)
	in.QualificationRefs = append([]values.EntityRef(nil), in.QualificationRefs...)
	return in
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return errors.New("appointment: nil context")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
