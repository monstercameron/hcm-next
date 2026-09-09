// Package projection owns the critical-projection subplane's checkpoint
// (owner: data plane; phase: P1A; DATA-006, part of the NEXT-004 slice).
//
// A projection is rebuildable read state derived from the ledger; it is
// never authoritative (platform-plane-model.md, plan.md 5.15 "one canonical
// history, many rebuildable read planes"). What this package makes
// authoritative-adjacent is the checkpoint itself: the exact source sequence
// a named projection has applied for a stream, advanced by compare-and-swap
// so that applying the same event twice is a no-op and applying events out of
// order is refused rather than silently corrupting the watermark.
package projection

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Checkpoint is one projection's watermark on one stream.
type Checkpoint struct {
	Tenant              uuid.UUID
	ProjectionName      string
	StreamKey           string
	LastAppliedSequence int64
	LastAppliedDigest   string
	Status              string
}

// Status values. CURRENT is the only status this package's Apply ever
// writes; the others describe operator/repair states set elsewhere
// (DATA-006, DATA-010).
const (
	StatusCurrent = "CURRENT"
)

// ApplyRequest advances a projection checkpoint by exactly one source event.
type ApplyRequest struct {
	Tenant         uuid.UUID
	ProjectionName string
	StreamKey      string
	// Sequence is the source ledger event's sequence this application
	// advances the checkpoint to.
	Sequence int64
	// Digest is the source ledger event's digest, recorded as the
	// checkpoint's LastAppliedDigest once applied.
	Digest string
}

// ApplyResult reports what Apply did.
type ApplyResult struct {
	Checkpoint Checkpoint
	// Applied is false when Sequence was already applied (an idempotent
	// no-op: duplicate delivery must never change the result twice).
	Applied bool
}

// ErrCheckpointNotFound reports an Apply against a checkpoint EnsureProjection
// never created.
type ErrCheckpointNotFound struct {
	ProjectionName string
	StreamKey      string
}

func (ErrCheckpointNotFound) Code() string { return "PROJECTION_CHECKPOINT_NOT_FOUND" }

func (e ErrCheckpointNotFound) Error() string {
	return fmt.Sprintf("%s: projection %s has no checkpoint on stream %s", e.Code(), e.ProjectionName, e.StreamKey)
}

// ErrSequenceGap reports an Apply whose Sequence is more than one past the
// checkpoint's current watermark - a missing event that must never silently
// advance the watermark (DATA-006 RED clause).
type ErrSequenceGap struct {
	ProjectionName string
	StreamKey      string
	Current        int64
	Requested      int64
}

func (ErrSequenceGap) Code() string { return "PROJECTION_SEQUENCE_GAP" }

func (e ErrSequenceGap) Error() string {
	return fmt.Sprintf("%s: projection %s on stream %s is at %d; sequence %d would skip an event",
		e.Code(), e.ProjectionName, e.StreamKey, e.Current, e.Requested)
}

// EnsureProjection registers a projection checkpoint at sequence zero if one
// does not already exist. It is idempotent, like ledger.EnsureStream.
func EnsureProjection(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, projectionName, streamKey string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO projection_checkpoint (tenant_id, projection_name, stream_key, last_applied_sequence, status)
		VALUES ($1, $2, $3, 0, $4)
		ON CONFLICT (tenant_id, projection_name, stream_key) DO NOTHING`,
		tenant, projectionName, streamKey, StatusCurrent); err != nil {
		return fmt.Errorf("register projection checkpoint %s/%s: %w", projectionName, streamKey, err)
	}
	return nil
}

// Apply idempotently advances one projection checkpoint by exactly one
// event, inside the caller's transaction. It locks the checkpoint row (the
// same pattern internal/data/ledger.Append uses to lock the stream head), so
// concurrent appliers of the same projection serialize here.
//
// Semantics (DATA-006 GREEN):
//   - Sequence == current+1: advances the watermark; Applied=true.
//   - Sequence <= current: the event was already applied; a no-op that
//     returns the existing checkpoint with Applied=false. Duplicate or
//     reordered redelivery never changes the result twice.
//   - Sequence > current+1: a missing event would be skipped; returns
//     ErrSequenceGap and leaves the watermark untouched.
func Apply(ctx context.Context, tx dbport.Tx, req ApplyRequest) (ApplyResult, error) {
	if req.Sequence < 1 {
		return ApplyResult{}, fmt.Errorf("projection: apply requires a positive sequence, got %d", req.Sequence)
	}

	var (
		current       int64
		currentDigest *string
		status        string
	)
	err := tx.QueryRow(ctx, `
		SELECT last_applied_sequence, last_applied_digest, status
		FROM projection_checkpoint
		WHERE tenant_id = $1 AND projection_name = $2 AND stream_key = $3
		FOR UPDATE`, req.Tenant, req.ProjectionName, req.StreamKey).Scan(&current, &currentDigest, &status)
	if errors.Is(err, dbport.ErrNoRows) {
		return ApplyResult{}, ErrCheckpointNotFound{ProjectionName: req.ProjectionName, StreamKey: req.StreamKey}
	}
	if err != nil {
		return ApplyResult{}, fmt.Errorf("lock projection checkpoint %s/%s: %w", req.ProjectionName, req.StreamKey, err)
	}

	if req.Sequence <= current {
		cp := Checkpoint{Tenant: req.Tenant, ProjectionName: req.ProjectionName, StreamKey: req.StreamKey, LastAppliedSequence: current, Status: status}
		if currentDigest != nil {
			cp.LastAppliedDigest = *currentDigest
		}
		return ApplyResult{Checkpoint: cp, Applied: false}, nil
	}
	if req.Sequence > current+1 {
		return ApplyResult{}, ErrSequenceGap{ProjectionName: req.ProjectionName, StreamKey: req.StreamKey, Current: current, Requested: req.Sequence}
	}

	affected, err := tx.Exec(ctx, `
		UPDATE projection_checkpoint
		SET last_applied_sequence = $4, last_applied_digest = $5, status = $6, updated_at = now()
		WHERE tenant_id = $1 AND projection_name = $2 AND stream_key = $3`,
		req.Tenant, req.ProjectionName, req.StreamKey, req.Sequence, req.Digest, StatusCurrent)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("advance projection checkpoint %s/%s: %w", req.ProjectionName, req.StreamKey, err)
	}
	if affected != 1 {
		return ApplyResult{}, fmt.Errorf("advance projection checkpoint %s/%s: %d rows updated", req.ProjectionName, req.StreamKey, affected)
	}

	return ApplyResult{
		Checkpoint: Checkpoint{
			Tenant:              req.Tenant,
			ProjectionName:      req.ProjectionName,
			StreamKey:           req.StreamKey,
			LastAppliedSequence: req.Sequence,
			LastAppliedDigest:   req.Digest,
			Status:              StatusCurrent,
		},
		Applied: true,
	}, nil
}

// Read returns the current checkpoint for one projection on one stream.
func Read(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) dbport.Row
}, tenant uuid.UUID, projectionName, streamKey string) (Checkpoint, error) {
	var (
		cp     Checkpoint
		digest *string
	)
	cp.Tenant, cp.ProjectionName, cp.StreamKey = tenant, projectionName, streamKey
	err := q.QueryRow(ctx, `
		SELECT last_applied_sequence, last_applied_digest, status
		FROM projection_checkpoint
		WHERE tenant_id = $1 AND projection_name = $2 AND stream_key = $3`,
		tenant, projectionName, streamKey).Scan(&cp.LastAppliedSequence, &digest, &cp.Status)
	if errors.Is(err, dbport.ErrNoRows) {
		return Checkpoint{}, ErrCheckpointNotFound{ProjectionName: projectionName, StreamKey: streamKey}
	}
	if err != nil {
		return Checkpoint{}, fmt.Errorf("read projection checkpoint %s/%s: %w", projectionName, streamKey, err)
	}
	if digest != nil {
		cp.LastAppliedDigest = *digest
	}
	return cp, nil
}
