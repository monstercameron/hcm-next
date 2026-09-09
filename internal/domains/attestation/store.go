package attestation

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Store is the persistence port for attestation statements and their binding
// history. Tenant is deliberately a value from the kernel rather than a
// database identifier, so the domain stays independent of PostgreSQL.
type Store interface {
	PutStatement(context.Context, values.TenantId, AttestationStatement, ...uint64) error
	GetStatement(context.Context, values.TenantId, string, uint64) (AttestationStatement, error)
	ListStatementVersions(context.Context, values.TenantId, string) ([]AttestationStatement, error)
	AppendBinding(context.Context, values.TenantId, Binding) error
	ListBindings(context.Context, values.TenantId, string) ([]Binding, error)
}

var (
	ErrStatementNotFound  = errors.New("attestation: statement not found")
	ErrStatementDuplicate = errors.New("attestation: statement revision already exists")
	ErrBindingDuplicate   = errors.New("attestation: binding version already exists")
	ErrVersionConflict    = errors.New("attestation: version conflict")
)

// MemoryStore is a kernel-pure implementation of Store for tests and local
// composition. It retains immutable copies and never performs I/O.
type MemoryStore struct {
	mu       sync.RWMutex
	stmt     map[string]AttestationStatement
	bindings map[string]Binding
}

// NewMemoryStore returns an empty in-memory attestation store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		stmt:     make(map[string]AttestationStatement),
		bindings: make(map[string]Binding),
	}
}

var _ Store = (*MemoryStore)(nil)

func statementKey(tenant, id string, version uint64) string {
	return tenant + "\x00" + id + fmt.Sprintf("\x00%d", version)
}

func bindingKey(tenant, id string, version uint64) string {
	return tenant + "\x00" + id + fmt.Sprintf("\x00%d", version)
}

// PutStatement appends the next statement revision. When expectedVersion is
// supplied, it must equal the currently highest stored revision (zero means
// that no revision exists yet).
func (s *MemoryStore) PutStatement(ctx context.Context, tenant values.TenantId, stmt AttestationStatement, expectedVersion ...uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("attestation: nil memory store")
	}
	if err := stmt.Validate(); err != nil {
		return err
	}
	if tenant == "" || stmt.ID == "" {
		return fmt.Errorf("attestation: tenant and statement id are required")
	}
	want := uint64(0)
	if len(expectedVersion) > 0 {
		want = expectedVersion[0]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	latest := uint64(0)
	for version := uint64(1); version <= stmt.Version; version++ {
		if _, ok := s.stmt[statementKey(string(tenant), stmt.ID, version)]; ok {
			latest = version
		}
	}
	if want != latest {
		return fmt.Errorf("%w: statement %s expected %d, current %d", ErrVersionConflict, stmt.ID, want, latest)
	}
	if stmt.Version != latest+1 {
		if stmt.Version <= latest {
			return fmt.Errorf("%w: statement %s version %d", ErrStatementDuplicate, stmt.ID, stmt.Version)
		}
		return fmt.Errorf("%w: statement %s requires version %d, got %d", ErrVersionConflict, stmt.ID, latest+1, stmt.Version)
	}
	s.stmt[statementKey(string(tenant), stmt.ID, stmt.Version)] = cloneStatement(stmt)
	return nil
}

// GetStatement returns one immutable statement revision.
func (s *MemoryStore) GetStatement(ctx context.Context, tenant values.TenantId, id string, version uint64) (AttestationStatement, error) {
	if err := ctx.Err(); err != nil {
		return AttestationStatement{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	stmt, ok := s.stmt[statementKey(string(tenant), id, version)]
	if !ok {
		return AttestationStatement{}, ErrStatementNotFound
	}
	return cloneStatement(stmt), nil
}

// ListStatementVersions returns revisions in ascending version order.
func (s *MemoryStore) ListStatementVersions(ctx context.Context, tenant values.TenantId, id string) ([]AttestationStatement, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []AttestationStatement
	for version := uint64(1); ; version++ {
		stmt, ok := s.stmt[statementKey(string(tenant), id, version)]
		if !ok {
			break
		}
		out = append(out, cloneStatement(stmt))
	}
	if len(out) == 0 {
		return nil, ErrStatementNotFound
	}
	return out, nil
}

// AppendBinding appends exactly the next binding version for a statement.
func (s *MemoryStore) AppendBinding(ctx context.Context, tenant values.TenantId, binding Binding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("attestation: nil memory store")
	}
	if err := binding.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	latest := uint64(0)
	for version := uint64(1); version <= binding.BindingVersion; version++ {
		if _, ok := s.bindings[bindingKey(string(tenant), binding.StatementID, version)]; ok {
			latest = version
		}
	}
	if binding.BindingVersion != latest+1 {
		if binding.BindingVersion <= latest {
			return fmt.Errorf("%w: binding %s version %d", ErrBindingDuplicate, binding.StatementID, binding.BindingVersion)
		}
		return fmt.Errorf("%w: binding %s requires version %d, got %d", ErrVersionConflict, binding.StatementID, latest+1, binding.BindingVersion)
	}
	s.bindings[bindingKey(string(tenant), binding.StatementID, binding.BindingVersion)] = cloneBinding(binding)
	return nil
}

// ListBindings returns a statement's binding history in version order.
func (s *MemoryStore) ListBindings(ctx context.Context, tenant values.TenantId, id string) ([]Binding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Binding
	for version := uint64(1); ; version++ {
		binding, ok := s.bindings[bindingKey(string(tenant), id, version)]
		if !ok {
			break
		}
		out = append(out, cloneBinding(binding))
	}
	if len(out) == 0 {
		return nil, ErrStatementNotFound
	}
	return out, nil
}

func cloneStatement(in AttestationStatement) AttestationStatement {
	in.EvidenceRefs = append([]EvidenceRef(nil), in.EvidenceRefs...)
	if in.RevocationLink != nil {
		link := *in.RevocationLink
		in.RevocationLink = &link
	}
	return in
}

func cloneBinding(in Binding) Binding {
	in.EvidenceBindings = append([]EvidenceBinding(nil), in.EvidenceBindings...)
	return in
}
