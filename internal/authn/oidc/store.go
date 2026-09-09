package oidc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PendingAuthorization is the server-side record of one in-flight
// authorization attempt, keyed by its own State. It never carries the raw
// PKCE code_verifier -- only [CodeChallengeDigest], the RFC 7636 digest
// [deriveVerifier] and [codeChallengeS256] recompute at callback time. See
// doc.go's "Code verifier custody" section.
type PendingAuthorization struct {
	Tenant      values.TenantId
	IssuerURL   string
	ClientID    string
	RedirectURI string

	State               string
	Nonce               string
	CodeChallengeDigest string

	CreatedAt time.Time
	ExpiresAt time.Time
}

// StateStore is the pending-authorization persistence port. It is
// deliberately a plain key-value contract with one-time consumption: it
// carries no notion of "now" or TTL itself (that policy lives in [Flow],
// which is the only caller that ever needs a clock), and it never risks two
// callbacks both observing the same pending authorization as present.
type StateStore interface {
	// Put inserts pending, keyed by pending.State. It refuses to insert
	// over an existing, unconsumed entry for the same state -- a state
	// collision would mean [randomToken] produced a duplicate, which
	// [Flow] treats as an error rather than silently overwriting another
	// attempt's pending authorization.
	Put(ctx context.Context, pending PendingAuthorization) error
	// Take atomically returns and removes the pending authorization for
	// state, or found=false if none exists -- because none was ever put,
	// because an earlier call to Take already consumed it, or because a
	// concurrent Take won the race. This is the replay guard: a state (and
	// the code that travels with it in the same redirect) can be consumed
	// exactly once, no matter how many times the callback URL is
	// delivered.
	Take(ctx context.Context, state string) (PendingAuthorization, bool, error)
}

// MemoryStateStore is the in-memory [StateStore] adapter this package
// ships. It is safe for concurrent use.
type MemoryStateStore struct {
	mu      sync.Mutex
	pending map[string]PendingAuthorization
}

// NewMemoryStateStore returns an empty in-memory state store.
func NewMemoryStateStore() *MemoryStateStore {
	return &MemoryStateStore{pending: make(map[string]PendingAuthorization)}
}

// Put implements [StateStore].
func (s *MemoryStateStore) Put(_ context.Context, pending PendingAuthorization) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.pending[pending.State]; exists {
		return fmt.Errorf("oidc: state %q is already pending", pending.State)
	}
	s.pending[pending.State] = pending
	return nil
}

// Take implements [StateStore].
func (s *MemoryStateStore) Take(_ context.Context, state string) (PendingAuthorization, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.pending[state]
	if !ok {
		return PendingAuthorization{}, false, nil
	}
	delete(s.pending, state)
	return pending, true, nil
}

var _ StateStore = (*MemoryStateStore)(nil)
