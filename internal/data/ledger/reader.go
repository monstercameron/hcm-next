package ledger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Querier is the minimal database capability a read against the ledger needs.
// A pooled handle, a single connection and an open transaction all satisfy it
// through [dbport], so callers read through whichever shape they already hold.
type Querier = dbport.Querier

// EventRecord is one durable ledger event as read back for replay,
// verification, and outbox/projection composition. Unlike AppendRequest it
// carries the fields the ledger itself assigned: EventID, Digest,
// DigestAlgorithm, CanonicalLength and RecordedAt.
type EventRecord struct {
	Tenant          uuid.UUID
	StreamKey       string
	Sequence        int64
	EventID         uuid.UUID
	AssertionClass  AssertionClass
	Authority       string
	SourceRef       string
	SchemaRef       string
	Payload         []byte
	ArtifactRef     string
	CanonicalLength int
	Digest          string
	DigestAlgorithm string
	OccurredAt      time.Time
	EffectiveAt     time.Time
	RecordedAt      time.Time
	CorrelationID   uuid.UUID
	IdempotencyKey  string
}

// ErrEventNotFound reports a read for a sequence that does not exist on the
// stream.
type ErrEventNotFound struct {
	Tenant    uuid.UUID
	StreamKey string
	Sequence  int64
}

func (ErrEventNotFound) Code() string { return "LEDGER_EVENT_NOT_FOUND" }

func (e ErrEventNotFound) Error() string {
	return fmt.Sprintf("%s: stream %s has no event at sequence %d", e.Code(), e.StreamKey, e.Sequence)
}

const selectEventColumns = `
	tenant_id, stream_key, sequence, event_id, assertion_class, authority_ref,
	source_ref, schema_ref, payload, artifact_ref, canonical_length, digest,
	digest_algorithm, occurred_at, effective_at, recorded_at, correlation_id,
	idempotency_key`

func scanEvent(row interface {
	Scan(dest ...any) error
}) (EventRecord, error) {
	var (
		rec       EventRecord
		authority *string
		artifact  *string
	)
	if err := row.Scan(
		&rec.Tenant, &rec.StreamKey, &rec.Sequence, &rec.EventID, &rec.AssertionClass, &authority,
		&rec.SourceRef, &rec.SchemaRef, &rec.Payload, &artifact, &rec.CanonicalLength, &rec.Digest,
		&rec.DigestAlgorithm, &rec.OccurredAt, &rec.EffectiveAt, &rec.RecordedAt, &rec.CorrelationID,
		&rec.IdempotencyKey,
	); err != nil {
		return EventRecord{}, err
	}
	if authority != nil {
		rec.Authority = *authority
	}
	if artifact != nil {
		rec.ArtifactRef = *artifact
	}
	return rec, nil
}

// Reader reads back committed ledger events. It performs no writes and no
// external calls; it is safe to use against a read replica in the future.
type Reader struct{}

// NewReader returns a Reader. It holds no state; the value exists so the
// method set can satisfy the internal/ledger.Reader port.
func NewReader() *Reader { return &Reader{} }

// ReadStream returns every event on a stream, ordered by sequence ascending.
func (r *Reader) ReadStream(ctx context.Context, q Querier, tenant uuid.UUID, streamKey string) ([]EventRecord, error) {
	rows, err := q.Query(ctx, `
		SELECT `+selectEventColumns+`
		FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = $2
		ORDER BY sequence ASC`, tenant, streamKey)
	if err != nil {
		return nil, fmt.Errorf("read stream %s: %w", streamKey, err)
	}
	defer rows.Close()

	var out []EventRecord
	for rows.Next() {
		rec, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan event on stream %s: %w", streamKey, err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stream %s: %w", streamKey, err)
	}
	return out, nil
}

// ReadEvent returns one exact event by (tenant, stream, sequence).
func (r *Reader) ReadEvent(ctx context.Context, q Querier, tenant uuid.UUID, streamKey string, sequence int64) (EventRecord, error) {
	row := q.QueryRow(ctx, `
		SELECT `+selectEventColumns+`
		FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = $2 AND sequence = $3`, tenant, streamKey, sequence)
	rec, err := scanEvent(row)
	if errors.Is(err, dbport.ErrNoRows) {
		return EventRecord{}, ErrEventNotFound{Tenant: tenant, StreamKey: streamKey, Sequence: sequence}
	}
	if err != nil {
		return EventRecord{}, fmt.Errorf("read event %s@%d: %w", streamKey, sequence, err)
	}
	return rec, nil
}
