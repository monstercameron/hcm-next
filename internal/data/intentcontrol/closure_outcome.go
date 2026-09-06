package intentcontrol

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrClosureConflict reports a second closure whose immutable evidence does
// not match the already-recorded closure row.
var ErrClosureConflict = errors.New("intentcontrol: conflicting closure")

// CloseIdempotent records a closure once and treats an exact replay as a
// successful no-op. It is the closure operation used by completion
// re-evaluation; the append-only row remains the source of truth.
func (s ClosureStore) CloseIdempotent(ctx context.Context, ex Executor, in Closure) error {
	if err := in.Validate(); err != nil {
		return err
	}
	var receipt any
	if in.ExecutionReceiptID != uuid.Nil {
		receipt = in.ExecutionReceiptID
	}
	var stored uuid.UUID
	err := ex.QueryRow(ctx, `
		INSERT INTO intent_closure (
			tenant_id, intent_id, closure_kind, closure_reason,
			intent_digest, proposal_digest, control_digest,
			execution_receipt_id, closed_by, closed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (tenant_id, intent_id) DO NOTHING
		RETURNING intent_id`, in.TenantID, in.IntentID, in.Kind, in.Reason,
		in.IntentDigest, in.ProposalDigest, in.ControlDigest, receipt, in.ClosedBy, in.ClosedAt.UTC()).Scan(&stored)
	if err == nil {
		return nil
	}
	if !isNoRows(err) {
		return fmt.Errorf("intentcontrol: close intent %s: %w", in.IntentID, err)
	}
	existing, loadErr := s.Load(ctx, ex, in.TenantID, in.IntentID)
	if loadErr != nil {
		return loadErr
	}
	if existing.Kind == in.Kind && existing.Reason == in.Reason &&
		existing.IntentDigest == in.IntentDigest && existing.ProposalDigest == in.ProposalDigest &&
		existing.ControlDigest == in.ControlDigest && existing.ExecutionReceiptID == in.ExecutionReceiptID {
		return nil
	}
	return fmt.Errorf("%w: intent_closure %s already records %s", ErrClosureConflict, in.IntentID, existing.Kind)
}
