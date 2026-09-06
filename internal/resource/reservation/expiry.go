package reservation

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const CodeExpired = "RESERVATION_EXPIRED"

var (
	ErrRenewalAuthority = errors.New("reservation: renewal authority does not match reservation")
	ErrInvalidRenewal   = errors.New("reservation: renewal expiry must extend the current expiry")
)

// Renew extends a held reservation before its current expiry. The reservation
// authority is the authority digest bound to the original request; proposal,
// capacity and ownership identity remain unchanged.
func (s *Store) Renew(id uuid.UUID, fence uint64, authorityDigest string, expiresAt, now time.Time) (Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[id]
	if !ok {
		return Reservation{}, wrap(CodeInvalid, id, ErrNotFound)
	}
	if item.Fence != fence {
		return Reservation{}, wrap(CodeFence, id, ErrFence)
	}
	if item.Status == Expired || (item.Status == Held && !item.Request.ExpiresAt.After(now)) {
		if item.Status == Held {
			item.Status, item.UpdatedAt = Expired, now.UTC()
			s.seq++
			s.events[id] = append(s.events[id], Event{Sequence: s.seq, ReservationID: id, From: Held, To: Expired, Fence: fence, At: now.UTC()})
		}
		return *item, wrap(CodeExpired, id, ErrExpired)
	}
	if item.Status != Held {
		return Reservation{}, wrap(CodeTransition, id, ErrInvalidTransition)
	}
	if authorityDigest != item.Request.AuthorityDigest {
		return Reservation{}, wrap(CodeConflict, id, ErrRenewalAuthority)
	}
	if !ValidDigest(authorityDigest) {
		return Reservation{}, wrap(CodeInvalid, id, ErrInvalidDigest)
	}
	if expiresAt.IsZero() || !expiresAt.After(now) || !expiresAt.After(item.Request.ExpiresAt) {
		return Reservation{}, wrap(CodeInvalid, id, ErrInvalidRenewal)
	}

	item.Request.ExpiresAt = expiresAt.UTC()
	item.UpdatedAt = now.UTC()
	return *item, nil
}
