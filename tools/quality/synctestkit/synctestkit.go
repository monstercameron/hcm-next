// Package synctestkit is the TOOL-021 testing/synctest qualification kit.
//
// It is a pure fixture package: every type here is a minimal, in-memory
// stand-in shaped like the outbox consumer's lease-acquire/renew/release
// loop, built only from the standard library. It never imports any real
// Human Capital Management Suite package, so its tests prove testing/synctest's own behavior
// (deterministic virtual-time timer advancement, goroutine-quiescence
// detection, and race-free concurrent access) rather than exercising any
// production code.
//
// Qualification scope: testing/synctest is qualified here ONLY for
// deterministic in-process timer, retry-backoff and goroutine-quiescence
// assertions against fixtures like these. It is explicitly NOT qualified
// to stand in for PostgreSQL, network, or external-process fault
// behavior - those remain the responsibility of the TOOL-014 ephemeral
// integration environment and the TOOL-022 Toxiproxy fault harness. See
// definitions/toolchain/synctest-qualification.yaml for the recorded
// verdict.
package synctestkit

import (
	"context"
	"sync"
	"time"
)

// LeaseStore is the minimal interface RunLeaseLoop needs: try to acquire a
// named lease, renew a held one, and release it. A real outbox consumer's
// lease store (backed by PostgreSQL row locks, in production) implements
// the same shape; this package never imports that implementation.
type LeaseStore interface {
	TryAcquire(id string) bool
	Renew(id string) bool
	Release(id string)
}

// FakeLeaseStore is an in-memory, mutex-guarded LeaseStore fixture. At
// most one lease ID may be held at a time (id is a single shared "slot",
// matching the tests' use of one contended lease). ForceExpireNext makes
// the next N Renew calls fail and immediately clears the held slot, which
// tests use to simulate a lease being lost out from under its holder
// (e.g. because a heartbeat missed its deadline).
type FakeLeaseStore struct {
	mu           sync.Mutex
	holder       string
	forceExpireN int
}

// TryAcquire reports whether id now holds the lease: true if the slot was
// free or already held by id, false if another id holds it.
func (s *FakeLeaseStore) TryAcquire(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.holder == "" || s.holder == id {
		s.holder = id
		return true
	}
	return false
}

// Renew reports whether id still holds the lease after renewal. It fails
// (and clears the slot) if id is not the current holder, or if a forced
// expiry set by ForceExpireNext is still pending.
func (s *FakeLeaseStore) Renew(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.holder != id {
		return false
	}
	if s.forceExpireN > 0 {
		s.forceExpireN--
		s.holder = ""
		return false
	}
	return true
}

// Release clears the slot if id is the current holder.
func (s *FakeLeaseStore) Release(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.holder == id {
		s.holder = ""
	}
}

// ForceExpireNext arranges for the next n calls to Renew(id) by whoever
// currently holds the lease to fail, simulating the lease being lost.
func (s *FakeLeaseStore) ForceExpireNext(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forceExpireN = n
}

// EventKind identifies one step RunLeaseLoop reports on its events
// channel.
type EventKind int

const (
	// EventAcquired: the loop just took the lease.
	EventAcquired EventKind = iota
	// EventRenewed: a periodic renewal of a held lease succeeded.
	EventRenewed
	// EventLost: a periodic renewal failed; the lease is no longer held.
	EventLost
	// EventRetry: acquisition failed because another holder has the
	// lease; the loop is about to back off before trying again.
	EventRetry
	// EventReleased: the loop released the lease because its context was
	// canceled while it was held.
	EventReleased
)

func (k EventKind) String() string {
	switch k {
	case EventAcquired:
		return "Acquired"
	case EventRenewed:
		return "Renewed"
	case EventLost:
		return "Lost"
	case EventRetry:
		return "Retry"
	case EventReleased:
		return "Released"
	default:
		return "Unknown"
	}
}

// Event is one RunLeaseLoop step, timestamped by the clock RunLeaseLoop
// observes (the bubble's virtual clock, when run inside synctest.Test).
type Event struct {
	Kind EventKind
	At   time.Time
}

// RunLeaseLoop models the shape of an outbox consumer's lease loop: try to
// acquire a lease, renew it on a fixed interval while held, back off on a
// fixed retry interval when another holder has it, and release cleanly
// when ctx is canceled. It sends one Event per step to events (which must
// have a receiver keeping pace; the loop's send blocks like any real
// consumer's would) and returns only after fully exiting - a goroutine
// running it can never outlive ctx's cancellation, so callers can prove
// "no goroutine leak" by observing the call returns.
//
// This is a pure fixture: store is the only production-shaped dependency,
// injected as the minimal LeaseStore interface, so the loop's actual
// timing and retry logic can be exercised deterministically under
// testing/synctest without a real lease store or database.
func RunLeaseLoop(ctx context.Context, store LeaseStore, leaseID string, renewEvery, retryEvery time.Duration, events chan<- Event) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !store.TryAcquire(leaseID) {
			events <- Event{Kind: EventRetry, At: time.Now()}
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryEvery):
			}
			continue
		}

		events <- Event{Kind: EventAcquired, At: time.Now()}

		held := true
		for held {
			select {
			case <-ctx.Done():
				store.Release(leaseID)
				events <- Event{Kind: EventReleased, At: time.Now()}
				return
			case <-time.After(renewEvery):
				if store.Renew(leaseID) {
					events <- Event{Kind: EventRenewed, At: time.Now()}
				} else {
					events <- Event{Kind: EventLost, At: time.Now()}
					held = false
				}
			}
		}
	}
}
