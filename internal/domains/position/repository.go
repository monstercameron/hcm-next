package position

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var (
	// ErrReservationDuplicate identifies a reservation identity or event that
	// has already been recorded.
	ErrReservationDuplicate = errors.New("position: duplicate reservation")
	// ErrReservationVersionConflict identifies a stale repository fence.
	ErrReservationVersionConflict = errors.New("position: reservation version conflict")
)

// ReservationRepository is the persistence-neutral port for the reservation
// aggregate. Put records the current row and its first ledger fact together;
// Transition advances the current row under its expected fence and appends the
// corresponding event atomically. The PostgreSQL adapter owns transaction and
// tenant-session details; the domain owns the value and lifecycle vocabulary.
type ReservationRepository interface {
	Put(context.Context, PositionReservation, PositionReservationEvent) error
	Load(context.Context, values.TenantId, string) (PositionReservation, error)
	Transition(context.Context, values.TenantId, string, uint64, PositionReservationState, string, time.Time) (PositionReservation, error)
	Events(context.Context, values.TenantId, string) ([]PositionReservationEvent, error)
}

// RepositoryExplanation is the stable, reference-only shape used when a
// caller needs to explain which storage port it used for a reservation.
type RepositoryExplanation struct {
	ReservationID string
	Tenant        values.TenantId
}

// ExplainRepository identifies the reservation repository without exposing
// its backing database or any request payload.
func ExplainRepository(tenant values.TenantId, reservationID string) RepositoryExplanation {
	return RepositoryExplanation{Tenant: tenant, ReservationID: reservationID}
}

// Put implements ReservationRepository for the in-memory semantic store.
func (s *PositionReservationStore) Put(ctx context.Context, r PositionReservation, event PositionReservationEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || r.ID == "" || r.Request.Tenant == "" {
		return ErrInvalidPositionReservation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.items[r.ID]; ok {
		if prior.Request.ProposalDigest == r.Request.ProposalDigest && prior.Fence == r.Fence {
			return ErrReservationConflict
		}
		return ErrReservationConflict
	}
	if s.items == nil {
		s.items = make(map[string]*PositionReservation)
	}
	if s.byKey == nil {
		s.byKey = make(map[string]string)
	}
	if s.bySlot == nil {
		s.bySlot = make(map[string][]string)
	}
	if s.events == nil {
		s.events = make(map[string][]PositionReservationEvent)
	}
	copyR := r
	s.items[r.ID] = &copyR
	s.byKey[r.Request.NaturalKey()] = r.ID
	s.bySlot[slotKey(r.Request)] = append(s.bySlot[slotKey(r.Request)], r.ID)
	s.events[r.ID] = append(s.events[r.ID], event)
	return nil
}

// Load implements ReservationRepository for the in-memory semantic store.
func (s *PositionReservationStore) Load(ctx context.Context, tenant values.TenantId, id string) (PositionReservation, error) {
	if err := ctx.Err(); err != nil {
		return PositionReservation{}, err
	}
	r, ok := s.Get(id)
	if !ok || r.Request.Tenant != tenant {
		return PositionReservation{}, ErrReservationNotFound
	}
	return r, nil
}

// Transition implements ReservationRepository for the in-memory semantic
// store. It mirrors the durable fence rule even though the original pure
// Reserve/Release API predates the repository port.
func (s *PositionReservationStore) Transition(ctx context.Context, tenant values.TenantId, id string, expected uint64, next PositionReservationState, reason string, at time.Time) (PositionReservation, error) {
	if err := ctx.Err(); err != nil {
		return PositionReservation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[id]
	if !ok || r.Request.Tenant != tenant {
		return PositionReservation{}, ErrReservationNotFound
	}
	if r.Fence != expected {
		return PositionReservation{}, fmt.Errorf("%w: %w", ErrReservationVersionConflict, ErrReservationFence)
	}
	if r.State != PositionReservationHeld || (next != PositionReservationReleased && next != PositionReservationExpired) {
		return PositionReservation{}, ErrReservationTransition
	}
	from := r.State
	r.State, r.Fence, r.UpdatedAt = next, r.Fence+1, at.UTC()
	sequence := uint64(len(s.events[id]) + 1)
	s.events[id] = append(s.events[id], PositionReservationEvent{Sequence: sequence, ReservationID: id, From: from, To: next, Reason: reason, Fence: r.Fence, At: at.UTC()})
	return *r, nil
}

// Events implements ReservationRepository for the in-memory semantic store.
func (s *PositionReservationStore) Events(ctx context.Context, tenant values.TenantId, id string) ([]PositionReservationEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[id]
	if !ok || r.Request.Tenant != tenant {
		return nil, ErrReservationNotFound
	}
	return append([]PositionReservationEvent(nil), s.events[id]...), nil
}

var _ ReservationRepository = (*PositionReservationStore)(nil)
