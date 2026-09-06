package uow

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// memKey is a MemoryRepository's storage key: one revision slice per
// (tenant, entityID) pair, the same scoping every DB-018 table's primary key
// carries.
type memKey struct{ tenant, entity uuid.UUID }

// MemoryRepository is a storage-free [AggregateRepository] fake: every
// revision lives in an in-process slice guarded by a mutex, with no
// dependency on [dbport] or a real database. It exists so [RunConformance]
// (and any caller exercising [UnitOfWork] coordination in isolation) has
// something to run against besides [PostgresWorkerRepository].
//
// Its ex parameter exists only to satisfy [AggregateRepository]'s signature;
// MemoryRepository never touches it, so nil is a valid argument.
//
// Version here carries no bitemporal meaning: because T is an opaque type
// parameter, MemoryRepository cannot inspect it for effective-dating fields
// the way [PostgresWorkerRepository] does for Worker specifically, so Load
// always returns the most recently appended revision and businessAt is
// ignored.
type MemoryRepository[T any] struct {
	mu   sync.Mutex
	data map[memKey][]T
}

// NewMemoryRepository returns an empty MemoryRepository.
func NewMemoryRepository[T any]() *MemoryRepository[T] {
	return &MemoryRepository[T]{data: make(map[memKey][]T)}
}

var _ AggregateRepository[struct{}] = (*MemoryRepository[struct{}])(nil)

// Load returns the most recently appended revision for entityID and its
// version (the count of revisions appended so far).
func (m *MemoryRepository[T]) Load(_ context.Context, _ dbport.Conn, tenant, entityID uuid.UUID, _ time.Time) (T, Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	revs := m.data[memKey{tenant, entityID}]
	if len(revs) == 0 {
		var zero T
		return zero, 0, fmt.Errorf("uow: memory repository: no revision recorded for %s", entityID)
	}
	return revs[len(revs)-1], Version(len(revs)), nil
}

// Save appends aggregate as entityID's next revision under the same
// compare-and-swap contract [PostgresWorkerRepository.Save] implements over
// PostgreSQL: expectedVersion must equal the count of revisions already
// appended, or Save refuses with [*ErrStaleVersion] naming the actual count.
// The mutex makes this check-then-append atomic with respect to concurrent
// Save calls on the same MemoryRepository, the in-process equivalent of the
// row lock a real UPDATE's WHERE clause re-check provides.
func (m *MemoryRepository[T]) Save(_ context.Context, _ dbport.Conn, tenant, entityID uuid.UUID, aggregate T, expectedVersion Version) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memKey{tenant, entityID}
	current := Version(len(m.data[key]))
	if current != expectedVersion {
		return &ErrStaleVersion{Actual: current}
	}
	m.data[key] = append(m.data[key], aggregate)
	return nil
}

// ListRevisions returns every revision ever appended for entityID, oldest
// first. The returned slice is a copy: mutating it never mutates what this
// repository stores.
func (m *MemoryRepository[T]) ListRevisions(_ context.Context, _ dbport.Conn, tenant, entityID uuid.UUID) ([]T, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	revs := m.data[memKey{tenant, entityID}]
	out := make([]T, len(revs))
	copy(out, revs)
	return out, nil
}
