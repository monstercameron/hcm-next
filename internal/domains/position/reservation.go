package position

// This file is the pure position-owned reservation protocol. It deliberately
// keeps persistence and transaction coordination outside the domain while
// making the reservation's proposal binding, capacity decision, fencing and
// release evidence explicit.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidPositionReservation = errors.New("position: invalid position reservation")
	ErrReservationConflict        = errors.New("position: reservation natural key conflicts with another proposal")
	ErrCompetingReservation       = errors.New("position: competing reservation")
	ErrNoVacancy                  = errors.New("position: no vacancy at effective date")
	ErrReservationFence           = errors.New("position: stale reservation fence")
	ErrReservationNotFound        = errors.New("position: reservation not found")
	ErrReservationTransition      = errors.New("position: invalid reservation transition")
	ErrReservationExpired         = errors.New("position: reservation expiry is not in the future")
)

// PositionReservationState is the append-only lifecycle of a position hold.
type PositionReservationState string

const (
	PositionReservationHeld     PositionReservationState = "HELD"
	PositionReservationReleased PositionReservationState = "RELEASED"
	PositionReservationExpired  PositionReservationState = "EXPIRED"
)

// PositionReservationRequest binds one capacity hold to one exact proposal
// revision and one local effective date. Occupants and pending proposals are
// the POSITION-002 snapshot supplied by the transaction preflight; the store
// adds its own live holds while fencing competing acquisitions.
type PositionReservationRequest struct {
	Tenant   values.TenantId
	Position values.EntityRef
	AsOf     AsOf

	ProposalRevisionID string
	ProposalDigest     string
	EffectiveDate      values.LocalDate
	Effective          values.EffectiveInterval

	FTE   values.Decimal
	Heads int64

	DesiredJobCode     string
	DesiredOrgUnit     string
	DesiredLegalEntity string
	MinRevision        values.RevisionToken

	Occupants []Occupant
	Pending   []PendingProposal

	AuthorityDigest string
	IdempotencyKey  string
	ExpiresAt       time.Time
}

func (r PositionReservationRequest) Validate(now time.Time) error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalidPositionReservation, err)
	}
	if err := r.Position.Validate(); err != nil || r.Position.Kind != KindPosition || r.Position.Tenant != r.Tenant {
		return fmt.Errorf("%w: position is invalid or outside tenant", ErrInvalidPositionReservation)
	}
	if err := r.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %v", ErrInvalidPositionReservation, err)
	}
	if !r.EffectiveDate.IsSet() || r.EffectiveDate.Validate() != nil || r.EffectiveDate.Compare(r.AsOf.EffectiveOn) != 0 {
		return fmt.Errorf("%w: effective date must equal the as-of effective date", ErrInvalidPositionReservation)
	}
	if r.Effective.Validate() == nil {
		if r.Effective.Kind() != values.IntervalKindLocalDate {
			return fmt.Errorf("%w: effective interval must be LOCAL_DATE", ErrInvalidPositionReservation)
		}
		covered, err := r.Effective.ContainsDate(r.EffectiveDate)
		if err != nil || !covered {
			return fmt.Errorf("%w: effective interval does not cover effective date", ErrInvalidPositionReservation)
		}
	}
	if strings.TrimSpace(r.ProposalRevisionID) == "" || strings.TrimSpace(r.ProposalDigest) == "" {
		return fmt.Errorf("%w: exact proposal revision id and digest are required", ErrInvalidPositionReservation)
	}
	if err := r.FTE.Validate(); err != nil || r.FTE.Sign() <= 0 {
		return fmt.Errorf("%w: fte must be a positive exact decimal", ErrInvalidPositionReservation)
	}
	if r.Heads <= 0 {
		return fmt.Errorf("%w: heads must be positive", ErrInvalidPositionReservation)
	}
	if strings.TrimSpace(r.AuthorityDigest) == "" || strings.TrimSpace(r.IdempotencyKey) == "" {
		return fmt.Errorf("%w: authority digest and idempotency key are required", ErrInvalidPositionReservation)
	}
	if r.ExpiresAt.IsZero() || !r.ExpiresAt.After(now) {
		return ErrReservationExpired
	}
	for i, occupant := range r.Occupants {
		if err := occupant.Validate(); err != nil {
			return fmt.Errorf("%w: occupant %d: %v", ErrInvalidPositionReservation, i, err)
		}
	}
	for i, pending := range r.Pending {
		if err := pending.Validate(); err != nil {
			return fmt.Errorf("%w: pending proposal %d: %v", ErrInvalidPositionReservation, i, err)
		}
	}
	return nil
}

// NaturalKey is the idempotency identity of a proposal reservation. The
// digest is checked against the stored request separately, so reusing a
// revision id with changed material cannot silently reuse a hold.
func (r PositionReservationRequest) NaturalKey() string {
	return string(r.Tenant) + "\x00" + r.Position.Id + "\x00" + r.ProposalRevisionID + "\x00" + r.EffectiveDate.String()
}

func (r PositionReservationRequest) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.position.PositionReservationRequest", positionSchemaVer).
		String("tenant", string(r.Tenant)).
		Value("position", r.Position).
		Value("as_of.effective_on", r.AsOf.EffectiveOn).
		Value("as_of.known_at", r.AsOf.KnownAt).
		String("proposal_revision_id", r.ProposalRevisionID).
		String("proposal_digest", r.ProposalDigest).
		Value("effective_date", r.EffectiveDate).
		Field("fte", r.FTE.Canonical()).
		Count("heads", int(r.Heads)).
		String("authority_digest", r.AuthorityDigest).
		String("idempotency_key", r.IdempotencyKey).
		String("expires_at", r.ExpiresAt.UTC().Format(time.RFC3339Nano))
	if r.Effective.Validate() == nil {
		w.Field("effective", r.Effective.Canonical())
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// PositionReservation is the fenced hold and its immutable request identity.
type PositionReservation struct {
	ID               string
	Request          PositionReservationRequest
	PositionRevision values.RevisionToken
	State            PositionReservationState
	Fence            uint64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type PositionReservationEvent struct {
	Sequence      uint64
	ReservationID string
	From, To      PositionReservationState
	Reason        string
	Fence         uint64
	At            time.Time
}

// PositionReservationEvidence is safe to retain as audit evidence: it names
// references, digests, bounded quantities and lifecycle transitions only.
type PositionReservationEvidence struct {
	ReservationID      string
	Tenant             values.TenantId
	Position           values.EntityRef
	ProposalRevisionID string
	ProposalDigest     string
	EffectiveDate      values.LocalDate
	FTE                values.Decimal
	Heads              int64
	AuthorityDigest    string
	PositionRevision   values.RevisionToken
	State              PositionReservationState
	Fence              uint64
	Events             []PositionReservationEvent
}

type PositionReservationStore struct {
	mu        sync.Mutex
	nextFence uint64
	sequence  uint64
	items     map[string]*PositionReservation
	byKey     map[string]string
	bySlot    map[string][]string
	events    map[string][]PositionReservationEvent
}

func NewPositionReservationStore() *PositionReservationStore {
	return &PositionReservationStore{
		items: make(map[string]*PositionReservation), byKey: make(map[string]string),
		bySlot: make(map[string][]string), events: make(map[string][]PositionReservationEvent),
	}
}

func reservationID(key, digest string, fence uint64) string {
	h := sha256.Sum256([]byte(key + "\x00" + digest + "\x00" + fmt.Sprint(fence)))
	return hex.EncodeToString(h[:])
}

func slotKey(r PositionReservationRequest) string {
	return string(r.Tenant) + "\x00" + r.Position.Id + "\x00" + r.EffectiveDate.String()
}

// Reserve validates compatibility and current capacity before atomically
// acquiring a fenced hold. A replay of the same natural key is idempotent;
// another proposal for the same position/date receives a typed conflict.
func (s *PositionReservationStore) Reserve(ctx context.Context, reader PositionFacts, req PositionReservationRequest, now time.Time) (PositionReservation, error) {
	if err := req.Validate(now); err != nil {
		return PositionReservation{}, err
	}
	compat, err := CheckCompatibility(ctx, reader, CompatibilityRequest{
		Tenant: req.Tenant, Position: req.Position, AsOf: req.AsOf,
		DesiredJobCode: req.DesiredJobCode, DesiredOrgUnit: req.DesiredOrgUnit,
		DesiredLegalEntity: req.DesiredLegalEntity, MinRevision: req.MinRevision,
	})
	if err != nil {
		return PositionReservation{}, err
	}
	if !compat.Exists || !compat.Compatible {
		return PositionReservation{}, fmt.Errorf("%w: %v", ErrReservationConflict, compat.Findings)
	}
	capacity, err := CalculateCapacity(ctx, reader, CapacityRequest{
		Tenant: req.Tenant, Position: req.Position, AsOf: req.AsOf,
		Occupants: req.Occupants, Pending: req.Pending,
	})
	if err != nil {
		return PositionReservation{}, err
	}
	if capacity.AvailableHeads <= 0 || capacity.AvailableFTE.Sign() <= 0 || capacity.AvailableHeads < req.Heads || capacity.AvailableFTE.Cmp(req.FTE) < 0 {
		return PositionReservation{}, ErrNoVacancy
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if oldID, ok := s.byKey[req.NaturalKey()]; ok {
		old := *s.items[oldID]
		if old.Request.ProposalDigest != req.ProposalDigest || old.Request.AuthorityDigest != req.AuthorityDigest || !old.Request.FTE.Equal(req.FTE) || old.Request.Heads != req.Heads || old.Request.ExpiresAt != req.ExpiresAt {
			return PositionReservation{}, ErrReservationConflict
		}
		return old, nil
	}
	for _, id := range s.bySlot[slotKey(req)] {
		old := s.items[id]
		if old != nil && old.State == PositionReservationHeld {
			return PositionReservation{}, ErrCompetingReservation
		}
	}
	s.nextFence++
	s.sequence++
	now = now.UTC()
	id := reservationID(req.NaturalKey(), req.ProposalDigest, s.nextFence)
	item := &PositionReservation{ID: id, Request: req, PositionRevision: compat.Revision, State: PositionReservationHeld, Fence: s.nextFence, CreatedAt: now, UpdatedAt: now}
	s.items[id] = item
	s.byKey[req.NaturalKey()] = id
	s.bySlot[slotKey(req)] = append(s.bySlot[slotKey(req)], id)
	s.events[id] = append(s.events[id], PositionReservationEvent{Sequence: s.sequence, ReservationID: id, To: PositionReservationHeld, Reason: "ACQUIRED", Fence: item.Fence, At: now})
	return *item, nil
}

func (s *PositionReservationStore) transition(id string, fence uint64, to PositionReservationState, reason string, now time.Time) (PositionReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return PositionReservation{}, ErrReservationNotFound
	}
	if item.Fence != fence {
		return PositionReservation{}, ErrReservationFence
	}
	if item.State == to {
		return *item, nil
	}
	if item.State != PositionReservationHeld || (to != PositionReservationReleased && to != PositionReservationExpired) {
		return PositionReservation{}, ErrReservationTransition
	}
	item.State, item.UpdatedAt = to, now.UTC()
	s.sequence++
	s.events[id] = append(s.events[id], PositionReservationEvent{Sequence: s.sequence, ReservationID: id, From: PositionReservationHeld, To: to, Reason: reason, Fence: fence, At: now.UTC()})
	return *item, nil
}

func (s *PositionReservationStore) Release(id string, fence uint64, now time.Time) (PositionReservation, error) {
	return s.transition(id, fence, PositionReservationReleased, "RELEASED", now)
}

func (s *PositionReservationStore) ReleaseOnProposalRejected(id string, fence uint64, now time.Time) (PositionReservation, error) {
	return s.transition(id, fence, PositionReservationReleased, "PROPOSAL_REJECTED", now)
}

func (s *PositionReservationStore) ReleaseOnProposalSuperseded(id string, fence uint64, now time.Time) (PositionReservation, error) {
	return s.transition(id, fence, PositionReservationReleased, "PROPOSAL_SUPERSEDED", now)
}

func (s *PositionReservationStore) Expire(now time.Time) []PositionReservation {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []PositionReservation
	for _, item := range s.items {
		if item.State == PositionReservationHeld && !item.Request.ExpiresAt.After(now) {
			item.State, item.UpdatedAt = PositionReservationExpired, now.UTC()
			s.sequence++
			s.events[item.ID] = append(s.events[item.ID], PositionReservationEvent{Sequence: s.sequence, ReservationID: item.ID, From: PositionReservationHeld, To: PositionReservationExpired, Reason: "EXPIRED", Fence: item.Fence, At: now.UTC()})
			out = append(out, *item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *PositionReservationStore) Get(id string) (PositionReservation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return PositionReservation{}, false
	}
	return *item, true
}

func (s *PositionReservationStore) Evidence(id string) (PositionReservationEvidence, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return PositionReservationEvidence{}, false
	}
	return PositionReservationEvidence{
		ReservationID: item.ID, Tenant: item.Request.Tenant, Position: item.Request.Position,
		ProposalRevisionID: item.Request.ProposalRevisionID, ProposalDigest: item.Request.ProposalDigest,
		EffectiveDate: item.Request.EffectiveDate, FTE: item.Request.FTE, Heads: item.Request.Heads,
		AuthorityDigest: item.Request.AuthorityDigest, State: item.State, Fence: item.Fence,
		PositionRevision: item.PositionRevision,
		Events:           append([]PositionReservationEvent(nil), s.events[id]...),
	}, true
}

// ReservationExplanation names the declared inputs and lifecycle facts that
// explain a hold. It carries no proposal content or protected position data.
type ReservationExplanation struct {
	ReservationID    string
	State            PositionReservationState
	Reason           string
	Inputs           []string
	ProposalDigest   string
	EffectiveDate    values.LocalDate
	PositionRevision values.RevisionToken
	Fence            uint64
}

// Explain returns a stable, reference-only explanation of a reservation.
func Explain(r PositionReservation) ReservationExplanation {
	reason := "HELD"
	if r.State != PositionReservationHeld {
		reason = string(r.State)
	}
	return ReservationExplanation{
		ReservationID: r.ID, State: r.State, Reason: reason,
		Inputs:         []string{"position_revision", "proposal_revision_digest", "effective_date", "capacity", "expiry", "fencing_token"},
		ProposalDigest: r.Request.ProposalDigest, EffectiveDate: r.Request.EffectiveDate,
		PositionRevision: r.PositionRevision, Fence: r.Fence,
	}
}
