package simcontract

import (
	"context"
	"fmt"
	"sync"
)

// Persist is the port through which one assembled [SimulationResult] is
// stored exactly once. It is a port, not a database: an implementation
// decides the storage technology; this package only requires idempotent-by-
// digest semantics.
//
// Store must validate r before persisting it, and it must never mark a
// [SideEffect] executed -- there is no field on this contract that could
// carry such a mark, so a conforming implementation cannot do so even if it
// tried.
type Persist interface {
	// Store persists r under r.Digest. A call for a digest already recorded
	// with byte-identical content returns the originally stored artifact and
	// true for "already stored", without persisting again. A call for a
	// digest already recorded under *different* content is refused with
	// [ErrDigestConflict]: a digest is a content identity, and a collision is
	// a bug this port never papers over silently.
	Store(ctx context.Context, r SimulationResult) (stored SimulationResult, alreadyStored bool, err error)

	// Load returns the artifact recorded under a digest, if any.
	Load(ctx context.Context, digest string) (SimulationResult, bool, error)
}

// MemoryStore is a concurrency-safe, in-memory reference [Persist]
// implementation. It is the reference the primary test and the race test use;
// a production caller may back the same port with durable storage without
// this package's contract changing.
type MemoryStore struct {
	mu    sync.Mutex
	items map[string]SimulationResult
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{items: make(map[string]SimulationResult)}
}

// Store implements [Persist].
func (s *MemoryStore) Store(_ context.Context, r SimulationResult) (SimulationResult, bool, error) {
	if err := r.Validate(); err != nil {
		return SimulationResult{}, false, err
	}
	if r.Digest == "" {
		return SimulationResult{}, false, fmt.Errorf("%w: contract carries no digest", ErrInvalidInput)
	}
	if err := r.VerifyDigest(); err != nil {
		return SimulationResult{}, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.items[r.Digest]; ok {
		if existing.Canonical() != nil && r.Canonical() != nil && string(existing.Canonical()) != string(r.Canonical()) {
			return SimulationResult{}, false, fmt.Errorf("%w: %s", ErrDigestConflict, r.Digest)
		}
		return existing, true, nil
	}
	s.items[r.Digest] = r
	return r, false, nil
}

// Load implements [Persist].
func (s *MemoryStore) Load(_ context.Context, digest string) (SimulationResult, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[digest]
	return r, ok, nil
}

// Len reports how many distinct artifacts are stored. It exists for tests
// that need to prove a concurrent burst of identical Store calls persisted
// exactly once.
func (s *MemoryStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}
