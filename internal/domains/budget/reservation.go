package budget

// This file is the pure, in-memory reference protocol for a promotion
// compensation-pool reservation. Persistence and remote adapters may use the
// same decisions, but must not claim stronger atomicity than they provide.

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type ReservationState string

const (
	Requested              ReservationState = "REQUESTED"
	Held                   ReservationState = "HELD"
	Committed              ReservationState = "COMMITTED"
	Released               ReservationState = "RELEASED"
	Expired                ReservationState = "EXPIRED"
	ReconciliationRequired ReservationState = "RECONCILIATION_REQUIRED"
)

func (s ReservationState) Terminal() bool {
	return s == Committed || s == Released || s == Expired
}

type CompensationReservationRequest struct {
	TenantID        string
	BudgetID        string
	ProposalDigest  string
	Amount          values.Decimal
	Currency        string
	AuthorityDigest string
	IdempotencyKey  string
	ExpiresAt       time.Time
}

func (r CompensationReservationRequest) Validate(now time.Time) error {
	if strings.TrimSpace(r.TenantID) == "" || strings.TrimSpace(r.BudgetID) == "" ||
		strings.TrimSpace(r.ProposalDigest) == "" || strings.TrimSpace(r.AuthorityDigest) == "" ||
		strings.TrimSpace(r.IdempotencyKey) == "" {
		return ErrInvalidReservation
	}
	if err := r.Amount.Validate(); err != nil || r.Amount.Sign() <= 0 {
		return fmt.Errorf("%w: amount must be positive exact decimal", ErrInvalidReservation)
	}
	if !validCurrency(r.Currency) {
		return ErrCurrencyMismatch
	}
	if r.ExpiresAt.IsZero() || !r.ExpiresAt.After(now) {
		return ErrReservationExpired
	}
	return nil
}

type CompensationBudgetAuthority struct {
	BudgetID        string
	Currency        string
	Available       values.Decimal
	AuthorityDigest string
}

func (a CompensationBudgetAuthority) Validate() error {
	if strings.TrimSpace(a.BudgetID) == "" || !validCurrency(a.Currency) || strings.TrimSpace(a.AuthorityDigest) == "" {
		return ErrInvalidAuthority
	}
	if err := a.Available.Validate(); err != nil || a.Available.Sign() < 0 {
		return ErrInvalidAuthority
	}
	return nil
}

type CompensationReservation struct {
	ID        uuid.UUID
	Request   CompensationReservationRequest
	State     ReservationState
	Fence     uint64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ReservationEvent struct {
	Sequence      uint64
	ReservationID uuid.UUID
	From, To      ReservationState
	Fence         uint64
	At            time.Time
}

var (
	ErrInvalidReservation   = errors.New("budget: invalid reservation")
	ErrInvalidAuthority     = errors.New("budget: invalid authority")
	ErrCurrencyMismatch     = errors.New("budget: currency mismatch")
	ErrReservationExpired   = errors.New("budget: reservation expired")
	ErrInsufficientBudget   = errors.New("budget: insufficient available balance")
	ErrCompetingReservation = errors.New("budget: competing reservation")
	ErrReservationConflict  = errors.New("budget: idempotency key conflicts with another proposal")
	ErrStaleFence           = errors.New("budget: stale fencing token")
	ErrInvalidTransition    = errors.New("budget: invalid reservation transition")
	ErrReservationNotFound  = errors.New("budget: reservation not found")
	ErrExternalAmbiguous    = errors.New("budget: external reservation outcome is ambiguous")
	ErrAuthorityMismatch    = errors.New("budget: authority does not match reservation")
)

type ReservationStore struct {
	mu                  sync.Mutex
	nextFence, sequence uint64
	items               map[uuid.UUID]*CompensationReservation
	byKey               map[string]uuid.UUID
	events              map[uuid.UUID][]ReservationEvent
}

func NewReservationStore() *ReservationStore {
	return &ReservationStore{items: make(map[uuid.UUID]*CompensationReservation), byKey: make(map[string]uuid.UUID), events: make(map[uuid.UUID][]ReservationEvent)}
}

func reservationKey(r CompensationReservationRequest) string {
	return r.TenantID + "\x00" + r.BudgetID + "\x00" + r.IdempotencyKey
}

// Reserve performs the co-located atomic request-and-hold operation. Replaying
// the exact request and authority returns the original hold without consuming
// balance twice; changing any material input returns a typed conflict.
func (s *ReservationStore) Reserve(r CompensationReservationRequest, a CompensationBudgetAuthority, now time.Time) (CompensationReservation, error) {
	if err := r.Validate(now); err != nil {
		return CompensationReservation{}, err
	}
	if err := a.Validate(); err != nil {
		return CompensationReservation{}, err
	}
	if a.BudgetID != r.BudgetID || a.AuthorityDigest != r.AuthorityDigest {
		return CompensationReservation{}, ErrAuthorityMismatch
	}
	if a.Currency != r.Currency {
		return CompensationReservation{}, ErrCurrencyMismatch
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if oldID, ok := s.byKey[reservationKey(r)]; ok {
		old := *s.items[oldID]
		if old.Request.ProposalDigest != r.ProposalDigest || !old.Request.Amount.Equal(r.Amount) || old.Request.Currency != r.Currency || old.Request.ExpiresAt != r.ExpiresAt || old.Request.AuthorityDigest != r.AuthorityDigest {
			return CompensationReservation{}, ErrReservationConflict
		}
		return old, nil
	}
	used, err := zeroLike(r.Amount)
	if err != nil {
		return CompensationReservation{}, err
	}
	for _, old := range s.items {
		if old.Request.TenantID == r.TenantID && old.Request.BudgetID == r.BudgetID && (old.State == Requested || old.State == Held || old.State == ReconciliationRequired) {
			used, err = used.Add(old.Request.Amount)
			if err != nil {
				return CompensationReservation{}, err
			}
		}
	}
	total, err := used.Add(r.Amount)
	if err != nil {
		return CompensationReservation{}, err
	}
	if total.Cmp(a.Available) > 0 {
		return CompensationReservation{}, ErrInsufficientBudget
	}
	s.nextFence++
	s.sequence++
	now = now.UTC()
	id := uuid.New()
	item := &CompensationReservation{ID: id, Request: r, State: Held, Fence: s.nextFence, CreatedAt: now, UpdatedAt: now}
	s.items[id] = item
	s.byKey[reservationKey(r)] = id
	s.events[id] = append(s.events[id], ReservationEvent{Sequence: s.sequence, ReservationID: id, To: Held, Fence: item.Fence, At: now})
	return *item, nil
}

func (s *ReservationStore) transition(id uuid.UUID, fence uint64, to ReservationState, now time.Time) (CompensationReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return CompensationReservation{}, ErrReservationNotFound
	}
	if item.Fence != fence {
		return CompensationReservation{}, ErrStaleFence
	}
	if item.State == to {
		return *item, nil
	}
	if item.State != Held || (to != Committed && to != Released) {
		return CompensationReservation{}, ErrInvalidTransition
	}
	item.State, item.UpdatedAt = to, now.UTC()
	s.sequence++
	s.events[id] = append(s.events[id], ReservationEvent{Sequence: s.sequence, ReservationID: id, From: Held, To: to, Fence: fence, At: now.UTC()})
	return *item, nil
}

func (s *ReservationStore) Commit(id uuid.UUID, fence uint64, now time.Time) (CompensationReservation, error) {
	return s.transition(id, fence, Committed, now)
}
func (s *ReservationStore) Release(id uuid.UUID, fence uint64, now time.Time) (CompensationReservation, error) {
	return s.transition(id, fence, Released, now)
}

// MarkAmbiguous blocks all ordinary lifecycle transitions until an external
// observation resolves the provider outcome.
func (s *ReservationStore) MarkAmbiguous(id uuid.UUID, fence uint64, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return ErrReservationNotFound
	}
	if item.Fence != fence {
		return ErrStaleFence
	}
	if item.State != Held {
		return ErrInvalidTransition
	}
	item.State, item.UpdatedAt = ReconciliationRequired, now.UTC()
	s.sequence++
	s.events[id] = append(s.events[id], ReservationEvent{Sequence: s.sequence, ReservationID: id, From: Held, To: ReconciliationRequired, Fence: fence, At: now.UTC()})
	return ErrExternalAmbiguous
}

// Reconcile records the provider's observed terminal outcome. Until this is
// called an ambiguous hold remains blocked and therefore unavailable to new
// reservations.
func (s *ReservationStore) Reconcile(id uuid.UUID, fence uint64, outcome ReservationState, now time.Time) (CompensationReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return CompensationReservation{}, ErrReservationNotFound
	}
	if item.Fence != fence {
		return CompensationReservation{}, ErrStaleFence
	}
	if item.State == outcome {
		return *item, nil
	}
	if item.State != ReconciliationRequired || (outcome != Committed && outcome != Released) {
		return CompensationReservation{}, ErrInvalidTransition
	}
	item.State, item.UpdatedAt = outcome, now.UTC()
	s.sequence++
	s.events[id] = append(s.events[id], ReservationEvent{Sequence: s.sequence, ReservationID: id, From: ReconciliationRequired, To: outcome, Fence: fence, At: now.UTC()})
	return *item, nil
}

func (s *ReservationStore) Expire(now time.Time) []CompensationReservation {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []CompensationReservation
	for _, item := range s.items {
		if item.State == Held && !item.Request.ExpiresAt.After(now) {
			item.State, item.UpdatedAt = Expired, now.UTC()
			s.sequence++
			s.events[item.ID] = append(s.events[item.ID], ReservationEvent{Sequence: s.sequence, ReservationID: item.ID, From: Held, To: Expired, Fence: item.Fence, At: now.UTC()})
			out = append(out, *item)
		}
	}
	return out
}

func (s *ReservationStore) Get(id uuid.UUID) (CompensationReservation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return CompensationReservation{}, false
	}
	return *item, true
}
func (s *ReservationStore) Events(id uuid.UUID) []ReservationEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ReservationEvent(nil), s.events[id]...)
}

func validCurrency(s string) bool {
	return len(s) == 3 && s == strings.ToUpper(s) && strings.Trim(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == ""
}
func zeroLike(d values.Decimal) (values.Decimal, error) {
	return values.NewDecimal("0", d.Scale(), values.RoundingExactRequired)
}
