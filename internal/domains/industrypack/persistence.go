package industrypack

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// BindingStore is the small persistence port for a tenant's detached pack
// composition. The port carries only domain values; PostgreSQL and tenant
// session details stay in the data adapter.
type BindingStore interface {
	SaveBinding(context.Context, string, Binding) error
	LoadBinding(context.Context, string, string) (Binding, error)
}

var (
	ErrBindingStoreInvalid   = errors.New("industrypack: invalid binding store input")
	ErrBindingStoreNotFound  = errors.New("industrypack: stored binding not found")
	ErrBindingStoreDuplicate = errors.New("industrypack: stored binding already exists")
)

// MemoryBindingStore is the kernel-pure reference implementation of
// BindingStore. It is useful to exercise promotion and composition callers
// without making PostgreSQL part of the domain package.
type MemoryBindingStore struct {
	mu       sync.RWMutex
	byTenant map[string]map[string]Binding
}

var _ BindingStore = (*MemoryBindingStore)(nil)

// NewMemoryBindingStore returns an empty in-memory binding repository.
func NewMemoryBindingStore() *MemoryBindingStore {
	return &MemoryBindingStore{byTenant: make(map[string]map[string]Binding)}
}

func (s *MemoryBindingStore) SaveBinding(_ context.Context, tenantID string, binding Binding) error {
	if s == nil || tenantID == "" || binding.CanonicalDigest == "" {
		return ErrBindingStoreInvalid
	}
	key := binding.CanonicalDigest
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.byTenant[tenantID]
	if items == nil {
		items = make(map[string]Binding)
		s.byTenant[tenantID] = items
	}
	if _, exists := items[key]; exists {
		return fmt.Errorf("%w: %s", ErrBindingStoreDuplicate, key)
	}
	items[key] = cloneBinding(binding)
	return nil
}

func (s *MemoryBindingStore) LoadBinding(_ context.Context, tenantID, digest string) (Binding, error) {
	if s == nil || tenantID == "" || digest == "" {
		return Binding{}, ErrBindingStoreInvalid
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	binding, ok := s.byTenant[tenantID][digest]
	if !ok {
		return Binding{}, fmt.Errorf("%w: %s", ErrBindingStoreNotFound, digest)
	}
	return cloneBinding(binding), nil
}

func cloneBinding(binding Binding) Binding {
	out := binding
	out.Packs = append([]IndustryPack(nil), binding.Packs...)
	out.Contents = append([]BoundContent(nil), binding.Contents...)
	return out
}
