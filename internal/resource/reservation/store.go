package reservation

import (
	"github.com/google/uuid"
	"sort"
	"sync"
	"time"
)

// Store is a concurrency-safe reference implementation of the protocol.
// Production adapters may persist the same compare-and-swap decisions in an
// ACID store; remote providers must be reconciled, never described as atomic.
type Store struct {
	mu        sync.Mutex
	nextFence uint64
	seq       uint64
	items     map[uuid.UUID]*Reservation
	byKey     map[string]uuid.UUID
	events    map[uuid.UUID][]Event
}

func NewStore() *Store {
	return &Store{items: make(map[uuid.UUID]*Reservation), byKey: make(map[string]uuid.UUID), events: make(map[uuid.UUID][]Event)}
}
func key(r Request) string { return r.Resource + "\x00" + r.Owner + "\x00" + r.IdempotencyKey }

// Acquire atomically checks expiry, proposal identity and overlapping active
// quantity, then allocates a strictly increasing fence. Repeating the exact
// request returns the same hold; reusing its key for a changed proposal is a
// typed conflict and cannot consume the old hold.
func (s *Store) Acquire(req Request, capacity Quantity, now time.Time) (Reservation, error) {
	if err := req.Validate(now); err != nil {
		return Reservation{}, wrap(CodeInvalid, uuid.Nil, err)
	}
	if capacity.Scale != req.Quantity.Scale || capacity.Value <= 0 {
		return Reservation{}, wrap(CodeInvalid, uuid.Nil, ErrInvalidQuantity)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = make(map[uuid.UUID]*Reservation)
	}
	if s.byKey == nil {
		s.byKey = make(map[string]uuid.UUID)
	}
	if s.events == nil {
		s.events = make(map[uuid.UUID][]Event)
	}
	if oldID, ok := s.byKey[key(req)]; ok {
		old := *s.items[oldID]
		if old.Request.ProposalDigest != req.ProposalDigest || old.Request.AuthorityDigest != req.AuthorityDigest || old.Request.Quantity != req.Quantity || old.Request.Interval != req.Interval {
			return Reservation{}, wrap(CodeConflict, old.ID, ErrConflict)
		}
		return old, nil
	}
	used := Quantity{Scale: req.Quantity.Scale}
	for _, old := range s.items {
		if old.Status != Held || old.Request.Resource != req.Resource || old.Request.Version != req.Version || !old.Request.Interval.Overlaps(req.Interval) {
			continue
		}
		var err error
		used, err = used.Add(old.Request.Quantity)
		if err != nil {
			return Reservation{}, wrap(CodeCapacity, uuid.Nil, err)
		}
	}
	if used.Value > capacity.Value-req.Quantity.Value {
		return Reservation{}, wrap(CodeCapacity, uuid.Nil, ErrCapacity)
	}
	s.nextFence++
	s.seq++
	id := uuid.New()
	item := &Reservation{ID: id, Request: req, Status: Held, Fence: s.nextFence, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	s.items[id] = item
	s.byKey[key(req)] = id
	s.events[id] = append(s.events[id], Event{Sequence: s.seq, ReservationID: id, From: "", To: Held, Fence: item.Fence, At: now.UTC()})
	return *item, nil
}

func (s *Store) transition(id uuid.UUID, fence uint64, to Status, now time.Time) (Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Reservation{}, wrap(CodeInvalid, id, ErrNotFound)
	}
	if item.Fence != fence {
		return Reservation{}, wrap(CodeFence, id, ErrFence)
	}
	if item.Status == to {
		return *item, nil
	} // idempotent replay
	if item.Status != Held || !to.Terminal() {
		return Reservation{}, wrap(CodeTransition, id, ErrInvalidTransition)
	}
	item.Status, item.UpdatedAt = to, now.UTC()
	s.seq++
	s.events[id] = append(s.events[id], Event{Sequence: s.seq, ReservationID: id, From: Held, To: to, Fence: fence, At: now.UTC()})
	return *item, nil
}
func (s *Store) Consume(id uuid.UUID, fence uint64, now time.Time) (Reservation, error) {
	return s.transition(id, fence, Consumed, now)
}
func (s *Store) Release(id uuid.UUID, fence uint64, now time.Time) (Reservation, error) {
	return s.transition(id, fence, Released, now)
}

// Expire transitions eligible holds to EXPIRED; it is explicit and caller
// clocked, so tests and recovery do not depend on a hidden wall clock.
func (s *Store) Expire(now time.Time) []Reservation {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Reservation
	for _, item := range s.items {
		if item.Status == Held && !item.Request.ExpiresAt.After(now) {
			item.Status, item.UpdatedAt = Expired, now.UTC()
			s.seq++
			s.events[item.ID] = append(s.events[item.ID], Event{Sequence: s.seq, ReservationID: item.ID, From: Held, To: Expired, Fence: item.Fence, At: now.UTC()})
			out = append(out, *item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out
}
func (s *Store) Get(id uuid.UUID) (Reservation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return Reservation{}, false
	}
	return *item, true
}
func (s *Store) Events(id uuid.UUID) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Event(nil), s.events[id]...)
	return out
}
