package critical

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/data/outbox"
)

// CommitResult is everything one Commit call produced: the ledger/outbox
// envelope internal/data/outbox.Commit already returns, plus this package's
// own critical-projection result for the same event.
type CommitResult struct {
	Outbox   outbox.CommitReceipt
	Critical ApplyResult
}

// Commit appends one ledger event and enqueues its outbox message through
// internal/data/outbox.Commit - unchanged, so this package adds no second
// opinion about append or enqueue semantics - and then, inside the same
// transaction, applies the same event to this package's critical projection
// via [Apply]. That composition is DATA-006's "commit atomically with the
// same transaction as the append": the caller supplies its own
// outbox.CommitRequest exactly as it would for a plain outbox.Commit call,
// and gets the critical-projection application for free, in the one
// transaction the caller owns.
//
// mapper decodes req.Append.SchemaRef/Payload the same way [Apply] does;
// pass [ProtoMapper]{} for the schemas this package knows.
func Commit(ctx context.Context, tx pgx.Tx, appender outbox.Appender, mapper Mapper, req outbox.CommitRequest) (CommitResult, error) {
	receipt, err := outbox.Commit(ctx, tx, appender, req)
	if err != nil {
		return CommitResult{}, fmt.Errorf("critical: commit: %w", err)
	}

	applied, err := Apply(ctx, tx, mapper, ApplyRequest{
		Tenant:    req.Append.Tenant,
		StreamKey: req.Append.StreamKey,
		Sequence:  receipt.Ledger.Sequence,
		Digest:    receipt.Ledger.Digest,
		SchemaRef: req.Append.SchemaRef,
		Payload:   req.Append.Payload,
	})
	if err != nil {
		return CommitResult{}, fmt.Errorf("critical: commit: apply: %w", err)
	}

	return CommitResult{Outbox: receipt, Critical: applied}, nil
}
