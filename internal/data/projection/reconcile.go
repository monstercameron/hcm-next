package projection

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
)

// StreamProjection names one (tenant, projection, stream) triple a
// [Reconciler] catches up.
type StreamProjection struct {
	Tenant         uuid.UUID
	ProjectionName string
	StreamKey      string
}

// StreamHead returns a stream's current head sequence, read directly rather
// than through the ledger port: a reconciler's only interest in stream_head
// is "how far should this projection catch up", not appending.
func StreamHead(ctx context.Context, q datalogger.Querier, tenant uuid.UUID, streamKey string) (int64, error) {
	var head int64
	err := q.QueryRow(ctx, `SELECT head_sequence FROM stream_head WHERE tenant_id = $1 AND stream_key = $2`,
		tenant, streamKey).Scan(&head)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("projection: stream %s is not registered", streamKey)
	}
	if err != nil {
		return 0, fmt.Errorf("projection: read stream head %s: %w", streamKey, err)
	}
	return head, nil
}

// Beginner opens transactions. *pgxpool.Pool and *pgx.Conn both implement it.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Reconciler catches a projection checkpoint up to its stream's current head
// by replaying and applying whatever events it has not yet applied
// (DATA-010: "rebuild a projection from canonical sources"). It is the
// out-of-band complement to outbox.Commit's synchronous, same-transaction
// advance: a projection that ever falls behind - a missed synchronous commit
// path, or a projection registered after events already existed - catches up
// here instead of staying stuck, and cmd/projector is the process that runs
// it on a schedule.
type Reconciler struct {
	db     Beginner
	reader *datalogger.Reader
}

// NewReconciler builds a Reconciler over a connection or pool and a ledger
// reader.
func NewReconciler(db Beginner, reader *datalogger.Reader) *Reconciler {
	return &Reconciler{db: db, reader: reader}
}

// ReconcileOne catches up one (tenant, projection, stream) to the stream's
// current head, applying whatever events are missing, in sequence order,
// inside one transaction. It returns how many events it applied - zero when
// the projection was already current.
func (r *Reconciler) ReconcileOne(ctx context.Context, target StreamProjection) (int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("projection: reconcile: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	cp, err := Read(ctx, tx, target.Tenant, target.ProjectionName, target.StreamKey)
	if err != nil {
		return 0, err
	}
	head, err := StreamHead(ctx, tx, target.Tenant, target.StreamKey)
	if err != nil {
		return 0, err
	}
	if cp.LastAppliedSequence >= head {
		if err := tx.Commit(ctx); err != nil {
			return 0, fmt.Errorf("projection: reconcile: commit (current): %w", err)
		}
		committed = true
		return 0, nil
	}

	events, err := r.reader.ReadStream(ctx, tx, target.Tenant, target.StreamKey)
	if err != nil {
		return 0, fmt.Errorf("projection: reconcile: read stream: %w", err)
	}

	applied := 0
	for _, ev := range events {
		if ev.Sequence <= cp.LastAppliedSequence {
			continue
		}
		result, err := Apply(ctx, tx, ApplyRequest{
			Tenant:         target.Tenant,
			ProjectionName: target.ProjectionName,
			StreamKey:      target.StreamKey,
			Sequence:       ev.Sequence,
			Digest:         ev.Digest,
		})
		if err != nil {
			return applied, fmt.Errorf("projection: reconcile: apply sequence %d: %w", ev.Sequence, err)
		}
		if result.Applied {
			applied++
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return applied, fmt.Errorf("projection: reconcile: commit: %w", err)
	}
	committed = true
	return applied, nil
}

// ReconcileDue lists every checkpoint at least one event behind its stream
// head, across every tenant and projection - the sweep a projector process
// runs each cycle.
func ReconcileDue(ctx context.Context, q datalogger.Querier) ([]StreamProjection, error) {
	rows, err := q.Query(ctx, `
		SELECT pc.tenant_id, pc.projection_name, pc.stream_key
		FROM projection_checkpoint pc
		JOIN stream_head sh ON sh.tenant_id = pc.tenant_id AND sh.stream_key = pc.stream_key
		WHERE pc.last_applied_sequence < sh.head_sequence`)
	if err != nil {
		return nil, fmt.Errorf("projection: list due: %w", err)
	}
	defer rows.Close()

	var out []StreamProjection
	for rows.Next() {
		var sp StreamProjection
		if err := rows.Scan(&sp.Tenant, &sp.ProjectionName, &sp.StreamKey); err != nil {
			return nil, fmt.Errorf("projection: list due: scan: %w", err)
		}
		out = append(out, sp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("projection: list due: %w", err)
	}
	return out, nil
}
