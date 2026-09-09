// Package positionstore persists the pre-commit position reservation
// aggregate from migrations/00043_position_reservation.sql.
//
// The store owns transaction boundaries because a reservation transition must
// update the fenced current row and append its ledger event as one fact. It
// still accepts a tenant-key-to-UUID resolver at construction time: domain
// values use the logical tenant key, while PostgreSQL's tenant_ref is the
// physical tenant identity.
package positionstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrInvalid identifies an unusable reservation or store configuration.
	ErrInvalid = errors.New("positionstore: invalid reservation")
	// ErrDuplicate identifies an already-owned reservation identity or event.
	ErrDuplicate = position.ErrReservationDuplicate
	// ErrNotFound identifies a reservation hidden or absent in the scoped tenant.
	ErrNotFound = errors.New("positionstore: reservation not found")
	// ErrVersionConflict identifies a stale expected fence.
	ErrVersionConflict = position.ErrReservationVersionConflict
)

// Store is the PostgreSQL adapter for position.ReservationRepository.
type Store struct {
	DB         dbport.Beginner
	TenantUUID func(values.TenantId) uuid.UUID
}

// New constructs a store. TenantUUID is required because the domain's
// canonical tenant key is not the database tenant UUID.
func New(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID) Store {
	return Store{DB: db, TenantUUID: tenantUUID}
}

var _ position.ReservationRepository = Store{}

type reservationPayload struct {
	Tenant             string    `json:"tenant"`
	Position           string    `json:"position"`
	AsOfEffectiveOn    string    `json:"as_of_effective_on"`
	AsOfKnownAt        string    `json:"as_of_known_at"`
	ProposalRevisionID string    `json:"proposal_revision_id"`
	ProposalDigest     string    `json:"proposal_digest"`
	EffectiveDate      string    `json:"effective_date"`
	Effective          string    `json:"effective,omitempty"`
	FTE                string    `json:"fte"`
	Heads              int64     `json:"heads"`
	DesiredJobCode     string    `json:"desired_job_code"`
	DesiredOrgUnit     string    `json:"desired_org_unit"`
	DesiredLegalEntity string    `json:"desired_legal_entity"`
	MinRevision        string    `json:"min_revision"`
	AuthorityDigest    string    `json:"authority_digest"`
	IdempotencyKey     string    `json:"idempotency_key"`
	ExpiresAt          time.Time `json:"expires_at"`
}

func encodeRequest(r position.PositionReservationRequest) ([]byte, error) {
	fte, err := r.FTE.MarshalText()
	if err != nil {
		return nil, fmt.Errorf("fte: %w", err)
	}
	minRevision, err := r.MinRevision.MarshalText()
	if err != nil {
		return nil, fmt.Errorf("min_revision: %w", err)
	}
	p := reservationPayload{
		Tenant: string(r.Tenant), Position: r.Position.String(),
		AsOfEffectiveOn: r.AsOf.EffectiveOn.String(), AsOfKnownAt: r.AsOf.KnownAt.String(),
		ProposalRevisionID: r.ProposalRevisionID, ProposalDigest: r.ProposalDigest,
		EffectiveDate: r.EffectiveDate.String(), Effective: r.Effective.String(),
		FTE: string(fte), Heads: r.Heads,
		DesiredJobCode: r.DesiredJobCode, DesiredOrgUnit: r.DesiredOrgUnit,
		DesiredLegalEntity: r.DesiredLegalEntity, MinRevision: string(minRevision),
		AuthorityDigest: r.AuthorityDigest, IdempotencyKey: r.IdempotencyKey,
		ExpiresAt: r.ExpiresAt.UTC(),
	}
	return json.Marshal(p)
}

func decodeRequest(raw string) (position.PositionReservationRequest, error) {
	var p reservationPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return position.PositionReservationRequest{}, fmt.Errorf("decode request: %w", err)
	}
	var out position.PositionReservationRequest
	out.Tenant = values.TenantId(p.Tenant)
	if err := out.Position.UnmarshalText([]byte(p.Position)); err != nil {
		return position.PositionReservationRequest{}, fmt.Errorf("position: %w", err)
	}
	var err error
	if out.AsOf.EffectiveOn, err = values.ParseLocalDate(p.AsOfEffectiveOn); err != nil {
		return position.PositionReservationRequest{}, fmt.Errorf("as_of_effective_on: %w", err)
	}
	knownAt, err := time.Parse(time.RFC3339Nano, p.AsOfKnownAt)
	if err != nil {
		return position.PositionReservationRequest{}, fmt.Errorf("as_of_known_at: %w", err)
	}
	out.AsOf.KnownAt, err = values.NewKnownAt(values.NewInstant(knownAt))
	if err != nil {
		return position.PositionReservationRequest{}, fmt.Errorf("as_of_known_at: %w", err)
	}
	out.ProposalRevisionID, out.ProposalDigest = p.ProposalRevisionID, p.ProposalDigest
	if out.EffectiveDate, err = values.ParseLocalDate(p.EffectiveDate); err != nil {
		return position.PositionReservationRequest{}, fmt.Errorf("effective_date: %w", err)
	}
	if out.FTE, err = parseDecimalText(p.FTE); err != nil {
		return position.PositionReservationRequest{}, fmt.Errorf("fte: %w", err)
	}
	out.Heads = p.Heads
	out.DesiredJobCode, out.DesiredOrgUnit, out.DesiredLegalEntity = p.DesiredJobCode, p.DesiredOrgUnit, p.DesiredLegalEntity
	if p.MinRevision != "" {
		if err := out.MinRevision.UnmarshalText([]byte(p.MinRevision)); err != nil {
			return position.PositionReservationRequest{}, fmt.Errorf("min_revision: %w", err)
		}
	}
	out.AuthorityDigest, out.IdempotencyKey, out.ExpiresAt = p.AuthorityDigest, p.IdempotencyKey, p.ExpiresAt.UTC()
	return out, nil
}

func parseDecimalText(text string) (values.Decimal, error) {
	if text == "" {
		return values.Decimal{}, fmt.Errorf("decimal is empty")
	}
	number, mode, ok := strings.Cut(text, "/")
	if !ok {
		return values.Decimal{}, fmt.Errorf("decimal has no rounding mode")
	}
	scale := int32(0)
	if _, fraction, hasPoint := strings.Cut(number, "."); hasPoint {
		scale = int32(len(fraction))
	}
	rounding, err := values.ParseRoundingMode(mode)
	if err != nil {
		return values.Decimal{}, err
	}
	return values.NewDecimal(number, scale, rounding)
}

func (s Store) tenant(tenant values.TenantId) (uuid.UUID, error) {
	if s.DB == nil || s.TenantUUID == nil {
		return uuid.Nil, fmt.Errorf("%w: database and tenant resolver are required", ErrInvalid)
	}
	if err := tenant.Validate(); err != nil {
		return uuid.Nil, fmt.Errorf("%w: tenant: %v", ErrInvalid, err)
	}
	id := s.TenantUUID(tenant)
	if id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: tenant %s has no UUID", ErrInvalid, tenant)
	}
	return id, nil
}

func beginTenant(ctx context.Context, db dbport.Beginner, tenantID uuid.UUID) (dbport.Tx, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("scope tenant: %w", err)
	}
	return tx, nil
}

// Put writes one current reservation and its first transition event.
func (s Store) Put(ctx context.Context, r position.PositionReservation, event position.PositionReservationEvent) error {
	tenantID, err := s.tenant(r.Request.Tenant)
	if err != nil {
		return err
	}
	if r.ID == "" || event.ReservationID != r.ID || event.Sequence < 1 || event.At.IsZero() {
		return fmt.Errorf("%w: reservation or initial event identity is incomplete", ErrInvalid)
	}
	if err := r.Request.Validate(r.CreatedAt); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	payload, err := encodeRequest(r.Request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	tx, err := beginTenant(ctx, s.DB, tenantID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rowID := uuid.New()
	count, err := tx.Exec(ctx, `
		INSERT INTO position_reservation
			(row_id, tenant_id, reservation_id, request, position_revision_ref, state, fence, created_at, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, NULL, $5, $6, $7, $8)
		ON CONFLICT (tenant_id, reservation_id) DO NOTHING`,
		rowID, tenantID, r.ID, string(payload), r.State, int64(r.Fence), r.CreatedAt.UTC(), r.UpdatedAt.UTC())
	if err != nil {
		return fmt.Errorf("insert reservation %s: %w", r.ID, err)
	}
	if count == 0 {
		return fmt.Errorf("%w: %s", ErrDuplicate, r.ID)
	}
	if err := insertEvent(ctx, tx, tenantID, event); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit reservation %s: %w", r.ID, err)
	}
	return nil
}

type storedRow struct {
	Request string
	State   string
	Fence   int64
	Created time.Time
	Updated time.Time
}

func loadRow(ctx context.Context, ex dbport.Querier, tenantID uuid.UUID, reservationID string, lock bool) (storedRow, error) {
	query := `SELECT request::text, state, fence, created_at, updated_at FROM position_reservation WHERE tenant_id=$1 AND reservation_id=$2`
	if lock {
		query += ` FOR UPDATE`
	}
	var row storedRow
	if err := ex.QueryRow(ctx, query, tenantID, reservationID).Scan(&row.Request, &row.State, &row.Fence, &row.Created, &row.Updated); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return storedRow{}, ErrNotFound
		}
		return storedRow{}, err
	}
	return row, nil
}

func materialize(id string, row storedRow) (position.PositionReservation, error) {
	request, err := decodeRequest(row.Request)
	if err != nil {
		return position.PositionReservation{}, err
	}
	return position.PositionReservation{ID: id, Request: request, State: position.PositionReservationState(row.State), Fence: uint64(row.Fence), CreatedAt: row.Created.UTC(), UpdatedAt: row.Updated.UTC()}, nil
}

// Load returns one tenant-scoped reservation.
func (s Store) Load(ctx context.Context, tenant values.TenantId, reservationID string) (position.PositionReservation, error) {
	tenantID, err := s.tenant(tenant)
	if err != nil {
		return position.PositionReservation{}, err
	}
	tx, err := beginTenant(ctx, s.DB, tenantID)
	if err != nil {
		return position.PositionReservation{}, err
	}
	defer tx.Rollback(ctx)
	row, err := loadRow(ctx, tx, tenantID, reservationID, false)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return position.PositionReservation{}, fmt.Errorf("%w: %s", ErrNotFound, reservationID)
		}
		return position.PositionReservation{}, fmt.Errorf("load reservation %s: %w", reservationID, err)
	}
	out, err := materialize(reservationID, row)
	if err != nil {
		return position.PositionReservation{}, fmt.Errorf("decode reservation %s: %w", reservationID, err)
	}
	return out, nil
}

// Transition advances state and fence, then appends the matching event in one
// transaction. The row lock plus the UPDATE predicate make concurrent stale
// writers return ErrVersionConflict without an ambiguous partial event.
func (s Store) Transition(ctx context.Context, tenant values.TenantId, reservationID string, expected uint64, next position.PositionReservationState, reason string, at time.Time) (position.PositionReservation, error) {
	tenantID, err := s.tenant(tenant)
	if err != nil {
		return position.PositionReservation{}, err
	}
	if expected == 0 || at.IsZero() {
		return position.PositionReservation{}, fmt.Errorf("%w: expected fence and transition time are required", ErrInvalid)
	}
	tx, err := beginTenant(ctx, s.DB, tenantID)
	if err != nil {
		return position.PositionReservation{}, err
	}
	defer tx.Rollback(ctx)
	row, err := loadRow(ctx, tx, tenantID, reservationID, true)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return position.PositionReservation{}, fmt.Errorf("%w: %s", ErrNotFound, reservationID)
		}
		return position.PositionReservation{}, err
	}
	if uint64(row.Fence) != expected {
		return position.PositionReservation{}, fmt.Errorf("%w: %s expected %d, current %d", ErrVersionConflict, reservationID, expected, row.Fence)
	}
	if row.State != string(position.PositionReservationHeld) || (next != position.PositionReservationReleased && next != position.PositionReservationExpired) {
		return position.PositionReservation{}, position.ErrReservationTransition
	}
	updated := tx.QueryRow(ctx, `
		UPDATE position_reservation
		SET state=$4, fence=fence+1, updated_at=$5
		WHERE tenant_id=$1 AND reservation_id=$2 AND fence=$3
		RETURNING request::text, state, fence, created_at, updated_at`,
		tenantID, reservationID, int64(expected), next, at.UTC())
	var stored storedRow
	if err := updated.Scan(&stored.Request, &stored.State, &stored.Fence, &stored.Created, &stored.Updated); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return position.PositionReservation{}, fmt.Errorf("%w: %s", ErrVersionConflict, reservationID)
		}
		return position.PositionReservation{}, fmt.Errorf("update reservation %s: %w", reservationID, err)
	}
	sequence := int64(0)
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(sequence), 0) + 1 FROM position_reservation_event WHERE tenant_id=$1 AND reservation_id=$2`, tenantID, reservationID).Scan(&sequence); err != nil {
		return position.PositionReservation{}, fmt.Errorf("next event sequence %s: %w", reservationID, err)
	}
	event := position.PositionReservationEvent{Sequence: uint64(sequence), ReservationID: reservationID, From: position.PositionReservationState(row.State), To: next, Reason: reason, Fence: uint64(stored.Fence), At: at.UTC()}
	if err := insertEvent(ctx, tx, tenantID, event); err != nil {
		return position.PositionReservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return position.PositionReservation{}, fmt.Errorf("commit transition %s: %w", reservationID, err)
	}
	return materialize(reservationID, stored)
}

func insertEvent(ctx context.Context, ex dbport.Execer, tenantID uuid.UUID, event position.PositionReservationEvent) error {
	from := any(nil)
	if event.From != "" {
		from = string(event.From)
	}
	count, err := ex.Exec(ctx, `
		INSERT INTO position_reservation_event
			(row_id, tenant_id, sequence, reservation_id, from_state, to_state, reason, fence, at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (tenant_id, reservation_id, sequence) DO NOTHING`,
		uuid.New(), tenantID, int64(event.Sequence), event.ReservationID, from, event.To, event.Reason, int64(event.Fence), event.At.UTC())
	if err != nil {
		return fmt.Errorf("insert reservation event %s/%d: %w", event.ReservationID, event.Sequence, err)
	}
	if count == 0 {
		return fmt.Errorf("%w: event %s/%d", ErrDuplicate, event.ReservationID, event.Sequence)
	}
	return nil
}

// Events loads the append-only transition history in sequence order.
func (s Store) Events(ctx context.Context, tenant values.TenantId, reservationID string) ([]position.PositionReservationEvent, error) {
	tenantID, err := s.tenant(tenant)
	if err != nil {
		return nil, err
	}
	tx, err := beginTenant(ctx, s.DB, tenantID)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := loadRow(ctx, tx, tenantID, reservationID, false); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, reservationID)
		}
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT sequence, reservation_id, from_state, to_state, reason, fence, at
		FROM position_reservation_event
		WHERE tenant_id=$1 AND reservation_id=$2
		ORDER BY sequence`, tenantID, reservationID)
	if err != nil {
		return nil, fmt.Errorf("list reservation events %s: %w", reservationID, err)
	}
	defer rows.Close()
	var out []position.PositionReservationEvent
	for rows.Next() {
		var e position.PositionReservationEvent
		var from, reason *string
		var sequence, fence int64
		if err := rows.Scan(&sequence, &e.ReservationID, &from, &e.To, &reason, &fence, &e.At); err != nil {
			return nil, fmt.Errorf("scan reservation event %s: %w", reservationID, err)
		}
		e.Sequence, e.Fence = uint64(sequence), uint64(fence)
		if from != nil {
			e.From = position.PositionReservationState(*from)
		}
		if reason != nil {
			e.Reason = *reason
		}
		e.At = e.At.UTC()
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read reservation events %s: %w", reservationID, err)
	}
	return out, nil
}
