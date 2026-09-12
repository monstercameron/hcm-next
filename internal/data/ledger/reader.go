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
	// PayloadState reports what Payload means (LEDGER-011). It is always one
	// of the four declared values below, never the empty string: every read
	// through ReadStream/ReadEvent sets it explicitly by consulting
	// ledger_payload_disposition, so a caller cannot mistake a nil Payload
	// for "never had inline bytes" (PayloadReferenced) when it might instead
	// mean "destroyed under a retention or hold decision"
	// (PayloadErased/PayloadRestricted). When PayloadState is
	// PayloadErased or PayloadRestricted, Payload is always nil regardless
	// of what internal/data/ledger/disposition's write path could still
	// physically leave in the ledger_event row: withholding it here is the
	// default every caller gets, not something a caller must opt into.
	PayloadState PayloadState
}

// PayloadState is the disposition of an EventRecord's Payload field. It
// mirrors internal/data/ledger/disposition.PayloadState's PRESENT,
// REFERENCED, RESTRICTED and PAYLOAD_ERASED values byte-for-byte (that
// package's own tests assert the two vocabularies cannot drift), but is
// declared here rather than imported from there: disposition already
// imports ledger for internal/data/ledger.Reader and internal/data/ledger.
// EventRecord, so ledger importing disposition back would be a cycle.
// disposition.ReadView additionally reports StateHeld, a live legal-hold
// check this package does not perform -- EventRecord only ever reports
// whether a payload is present, referenced, or has undergone disposition.
type PayloadState string

const (
	// PayloadPresent is an ordinary event that still carries its original
	// inline payload.
	PayloadPresent PayloadState = "PRESENT"
	// PayloadReferenced is an event that never carried an inline payload at
	// all: its content lives at ArtifactRef.
	PayloadReferenced PayloadState = "REFERENCED"
	// PayloadRestricted is a crypto-erased payload (LEDGER-011): Payload is
	// nil, and internal/data/ledger/disposition names the classification and
	// mechanism.
	PayloadRestricted PayloadState = "RESTRICTED"
	// PayloadErased is an overwritten payload (LEDGER-011): Payload is nil,
	// and internal/data/ledger/disposition names the classification and
	// mechanism.
	PayloadErased PayloadState = "PAYLOAD_ERASED"
)

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

// selectEventColumns is qualified against the "e" alias eventFromDisposition
// binds to ledger_event, because both are always used together: withholding
// an erased or crypto-erased payload is not an opt-in a caller requests, it
// is what ReadStream/ReadEvent always do (LEDGER-011).
const selectEventColumns = `
	e.tenant_id, e.stream_key, e.sequence, e.event_id, e.assertion_class, e.authority_ref,
	e.source_ref, e.schema_ref, e.payload, e.artifact_ref, e.canonical_length, e.digest,
	e.digest_algorithm, e.occurred_at, e.effective_at, e.recorded_at, e.correlation_id,
	e.idempotency_key, d.state`

// eventFromDisposition LEFT JOINs ledger_payload_disposition so every read
// carries its own event's disposition state alongside it. The join key is
// ledger_payload_disposition's primary key (tenant_id, stream_key,
// sequence), so it can add at most one row per event -- no fan-out.
const eventFromDisposition = `
	FROM ledger_event e
	LEFT JOIN ledger_payload_disposition d
		ON d.tenant_id = e.tenant_id AND d.stream_key = e.stream_key AND d.sequence = e.sequence`

func scanEvent(row interface {
	Scan(dest ...any) error
}) (EventRecord, error) {
	var (
		rec              EventRecord
		authority        *string
		artifact         *string
		dispositionState *string
	)
	if err := row.Scan(
		&rec.Tenant, &rec.StreamKey, &rec.Sequence, &rec.EventID, &rec.AssertionClass, &authority,
		&rec.SourceRef, &rec.SchemaRef, &rec.Payload, &artifact, &rec.CanonicalLength, &rec.Digest,
		&rec.DigestAlgorithm, &rec.OccurredAt, &rec.EffectiveAt, &rec.RecordedAt, &rec.CorrelationID,
		&rec.IdempotencyKey, &dispositionState,
	); err != nil {
		return EventRecord{}, err
	}
	if authority != nil {
		rec.Authority = *authority
	}
	if artifact != nil {
		rec.ArtifactRef = *artifact
	}
	switch {
	case dispositionState != nil:
		// A disposition row names this exact event: withhold Payload
		// regardless of what the (still physically immutable) ledger_event
		// row itself scanned into it. This is the default every caller
		// gets, never an opt-in.
		rec.PayloadState = PayloadState(*dispositionState)
		rec.Payload = nil
	case rec.Payload != nil:
		rec.PayloadState = PayloadPresent
	default:
		rec.PayloadState = PayloadReferenced
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
		`+eventFromDisposition+`
		WHERE e.tenant_id = $1 AND e.stream_key = $2
		ORDER BY e.sequence ASC`, tenant, streamKey)
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
		`+eventFromDisposition+`
		WHERE e.tenant_id = $1 AND e.stream_key = $2 AND e.sequence = $3`, tenant, streamKey, sequence)
	rec, err := scanEvent(row)
	if errors.Is(err, dbport.ErrNoRows) {
		return EventRecord{}, ErrEventNotFound{Tenant: tenant, StreamKey: streamKey, Sequence: sequence}
	}
	if err != nil {
		return EventRecord{}, fmt.Errorf("read event %s@%d: %w", streamKey, sequence, err)
	}
	return rec, nil
}
