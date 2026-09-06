// Package outbox owns the transactional outbox (owner: data plane; phase:
// P1A; DATA-007, DATA-008, part of the NEXT-004 slice).
//
// Enqueue writes one PENDING message inside the same transaction as the
// ledger append and projection update it accompanies (Commit, in commit.go),
// so an authoritative write and the intent to distribute it about that write
// commit or roll back together (DATA-007). Consumer then dispatches queued
// messages at least once, with a lease so a crash mid-batch never loses a
// message and a completed delivery never re-applies (DATA-008).
package outbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// Status values for one outbox row.
const (
	StatusPending   = "PENDING"
	StatusInFlight  = "IN_FLIGHT"
	StatusDelivered = "DELIVERED"
	StatusFailed    = "FAILED"
	StatusAbandoned = "ABANDONED"
)

// Criticality is the durable priority attached to a delivery. It follows the
// platform-wide P0 (highest) through P4 (lowest) vocabulary without importing
// the operations admission package into the data plane.
const (
	CriticalityP0 = "P0"
	CriticalityP1 = "P1"
	CriticalityP2 = "P2"
	CriticalityP3 = "P3"
	CriticalityP4 = "P4"
)

var (
	// ErrIdentityConflict means an idempotency key was reused for different
	// immutable delivery content. Silently returning the first row would hide
	// a logical duplicate or a caller bug.
	ErrIdentityConflict   = errors.New("outbox: effect identity conflicts with existing message")
	ErrInvalidCriticality = errors.New("outbox: invalid criticality")
	ErrLeaseFence         = errors.New("outbox: lease fence refused")
)

// EnqueueRequest is one message to distribute after the authoritative commit.
type EnqueueRequest struct {
	Tenant uuid.UUID
	// OutboxID is the row's own identity. Callers that want a deterministic,
	// replay-stable ID (e.g. derived from the ledger event ID) may supply
	// one; the zero UUID means "generate one".
	OutboxID uuid.UUID
	// EffectIdentity is the idempotent identity of the external effect this
	// message carries. A duplicate Enqueue with the same EffectIdentity
	// under the same tenant is a no-op that returns the original row: one
	// record per idempotent effect (migrations/00006, outbox_effect_identity_unique).
	EffectIdentity string
	OrderingKey    string
	Criticality    string
	SchemaRef      string
	Payload        []byte
}

// Record is one outbox row.
type Record struct {
	Tenant         uuid.UUID
	OutboxID       uuid.UUID
	EffectIdentity string
	OrderingKey    string
	Criticality    string
	SchemaRef      string
	Payload        []byte
	Status         string
	Attempts       int
	AvailableAt    time.Time
	UpdatedAt      time.Time
	LeaseToken     uuid.UUID
	LeaseUntil     time.Time
	LeaseVersion   int64
	LastError      *string
}

// Enqueue inserts one PENDING message inside the caller's transaction. A
// duplicate EffectIdentity returns the row already recorded rather than a
// second logical message.
func Enqueue(ctx context.Context, tx dbport.Tx, req EnqueueRequest) (Record, error) {
	if req.Tenant == uuid.Nil {
		return Record{}, fmt.Errorf("outbox: enqueue requires a tenant")
	}
	if req.EffectIdentity == "" {
		return Record{}, fmt.Errorf("outbox: enqueue requires an effect identity")
	}
	if req.OrderingKey == "" {
		return Record{}, fmt.Errorf("outbox: enqueue requires an ordering key")
	}
	criticality := req.Criticality
	if criticality == "" {
		criticality = CriticalityP2
	}
	if !validCriticality(criticality) {
		return Record{}, fmt.Errorf("%w: %q", ErrInvalidCriticality, criticality)
	}
	if req.SchemaRef == "" {
		return Record{}, fmt.Errorf("outbox: enqueue requires a schema reference")
	}
	id := req.OutboxID
	if id == uuid.Nil {
		id = uuid.New()
	}

	affected, err := tx.Exec(ctx, `
		INSERT INTO outbox (tenant_id, outbox_id, effect_identity, ordering_key, criticality, schema_ref, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (tenant_id, effect_identity) DO NOTHING`,
		req.Tenant, id, req.EffectIdentity, req.OrderingKey, criticality, req.SchemaRef, req.Payload)
	if err != nil {
		return Record{}, fmt.Errorf("outbox: enqueue %s: %w", req.EffectIdentity, err)
	}
	if affected == 1 {
		return Read(ctx, tx, req.Tenant, id)
	}

	// Already enqueued under this effect identity: return the existing row.
	existing, err := readByEffectIdentity(ctx, tx, req.Tenant, req.EffectIdentity)
	if err != nil {
		return Record{}, err
	}
	if !sameImmutableMessage(existing, req, criticality) {
		return Record{}, fmt.Errorf("%w: %s", ErrIdentityConflict, req.EffectIdentity)
	}
	return existing, nil
}

const selectRecordColumns = `tenant_id, outbox_id, effect_identity, ordering_key, criticality, schema_ref, payload, status, attempts, available_at, updated_at, lease_token, lease_until, lease_version, last_error`

func scanRecord(row interface{ Scan(dest ...any) error }) (Record, error) {
	var (
		rec        Record
		leaseToken *uuid.UUID
		leaseUntil *time.Time
	)
	if err := row.Scan(
		&rec.Tenant, &rec.OutboxID, &rec.EffectIdentity, &rec.OrderingKey, &rec.Criticality,
		&rec.SchemaRef, &rec.Payload, &rec.Status, &rec.Attempts, &rec.AvailableAt,
		&rec.UpdatedAt, &leaseToken, &leaseUntil, &rec.LeaseVersion, &rec.LastError,
	); err != nil {
		return Record{}, err
	}
	if leaseToken != nil {
		rec.LeaseToken = *leaseToken
	}
	if leaseUntil != nil {
		rec.LeaseUntil = leaseUntil.UTC()
	}
	return rec, nil
}

func validCriticality(value string) bool {
	return value == CriticalityP0 || value == CriticalityP1 || value == CriticalityP2 || value == CriticalityP3 || value == CriticalityP4
}

func sameImmutableMessage(existing Record, req EnqueueRequest, criticality string) bool {
	return existing.EffectIdentity == req.EffectIdentity &&
		existing.OrderingKey == req.OrderingKey &&
		existing.Criticality == criticality &&
		existing.SchemaRef == req.SchemaRef &&
		bytes.Equal(existing.Payload, req.Payload)
}

// Querier is the minimal database capability Read needs.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) dbport.Row
}

// Read returns one outbox row by its own ID.
func Read(ctx context.Context, q Querier, tenant uuid.UUID, outboxID uuid.UUID) (Record, error) {
	row := q.QueryRow(ctx, `SELECT `+selectRecordColumns+` FROM outbox WHERE tenant_id = $1 AND outbox_id = $2`, tenant, outboxID)
	rec, err := scanRecord(row)
	if errors.Is(err, dbport.ErrNoRows) {
		return Record{}, fmt.Errorf("outbox: %s not found for tenant %s", outboxID, tenant)
	}
	if err != nil {
		return Record{}, fmt.Errorf("outbox: read %s: %w", outboxID, err)
	}
	return rec, nil
}

func readByEffectIdentity(ctx context.Context, q Querier, tenant uuid.UUID, effectIdentity string) (Record, error) {
	row := q.QueryRow(ctx, `SELECT `+selectRecordColumns+` FROM outbox WHERE tenant_id = $1 AND effect_identity = $2`, tenant, effectIdentity)
	rec, err := scanRecord(row)
	if err != nil {
		return Record{}, fmt.Errorf("outbox: read effect %s: %w", effectIdentity, err)
	}
	return rec, nil
}
