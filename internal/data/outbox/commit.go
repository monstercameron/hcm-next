package outbox

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
)

// Appender is the narrow slice of internal/ledger.Appender Commit needs. It
// is declared locally (rather than importing internal/ledger) so this
// package's only ledger dependency is the adapter type it already needs for
// AppendRequest/AppendReceipt.
type Appender interface {
	Append(ctx context.Context, tx dbport.Tx, req datalogger.AppendRequest) (datalogger.AppendReceipt, error)
}

// ProjectionSpec names the projection a Commit call advances.
type ProjectionSpec struct {
	Name string
}

// OutboxSpec is the distribution message a Commit call enqueues alongside
// the ledger event it accompanies.
type OutboxSpec struct {
	EffectIdentity string
	OrderingKey    string
	Criticality    string
	SchemaRef      string
	Payload        []byte
}

// CommitRequest is one atomic write: one ledger event, one projection
// advance and one outbox message.
type CommitRequest struct {
	Append     datalogger.AppendRequest
	Projection ProjectionSpec
	Outbox     OutboxSpec
}

// CommitReceipt is everything Commit produced.
type CommitReceipt struct {
	Ledger     datalogger.AppendReceipt
	Projection projection.ApplyResult
	Outbox     Record
}

// Commit appends one ledger event, idempotently advances the named
// projection checkpoint to that event's sequence, and enqueues one outbox
// message - all inside the caller's transaction (DATA-007: "all
// correctness-bearing records commit once or all roll back").
//
// Replay is safe end to end: if Append returns an idempotent replay (the
// same IdempotencyKey with the same bytes was already recorded), the
// receipt's Sequence is the original one, so Projection.Apply below is a
// no-op (the sequence was already applied) and Enqueue is a no-op (the
// EffectIdentity was already recorded) - a retried Commit call never
// double-applies the projection or double-enqueues the message.
//
// The caller must have already registered the stream (ledger.EnsureStream)
// and the projection checkpoint (projection.EnsureProjection); Commit only
// advances existing registrations, matching Append's own "stream must
// already be registered" contract.
func Commit(ctx context.Context, tx dbport.Tx, appender Appender, req CommitRequest) (CommitReceipt, error) {
	receipt, err := appender.Append(ctx, tx, req.Append)
	if err != nil {
		return CommitReceipt{}, fmt.Errorf("outbox: commit: append: %w", err)
	}

	projResult, err := projection.Apply(ctx, tx, projection.ApplyRequest{
		Tenant:         req.Append.Tenant,
		ProjectionName: req.Projection.Name,
		StreamKey:      req.Append.StreamKey,
		Sequence:       receipt.Sequence,
		Digest:         receipt.Digest,
	})
	if err != nil {
		return CommitReceipt{}, fmt.Errorf("outbox: commit: projection: %w", err)
	}

	outboxRecord, err := Enqueue(ctx, tx, EnqueueRequest{
		Tenant:         req.Append.Tenant,
		OutboxID:       deterministicOutboxID(receipt),
		EffectIdentity: req.Outbox.EffectIdentity,
		OrderingKey:    req.Outbox.OrderingKey,
		Criticality:    req.Outbox.Criticality,
		SchemaRef:      req.Outbox.SchemaRef,
		Payload:        req.Outbox.Payload,
	})
	if err != nil {
		return CommitReceipt{}, fmt.Errorf("outbox: commit: enqueue: %w", err)
	}

	return CommitReceipt{Ledger: receipt, Projection: projResult, Outbox: outboxRecord}, nil
}

// deterministicOutboxID derives a stable outbox row ID from the ledger event
// ID, so a replayed Commit (same idempotency key, same event ID) reuses the
// same outbox row instead of racing EffectIdentity's own uniqueness
// constraint to decide who "won" the insert.
func deterministicOutboxID(receipt datalogger.AppendReceipt) uuid.UUID {
	return receipt.EventID
}
