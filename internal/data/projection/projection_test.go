package projection_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/projection"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

const (
	projectionName = "worker_state"
	streamKey      = "worker:1"
)

func newFixture(t *testing.T) (*pgtest.DB, uuid.UUID) {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()

	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'acme', 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant)

	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := ledger.EnsureStream(ctx, tx, tenant, streamKey, "WORKER", streamKey); err != nil {
		t.Fatalf("ensure stream: %v", err)
	}
	if err := projection.EnsureProjection(ctx, tx, tenant, projectionName, streamKey); err != nil {
		t.Fatalf("ensure projection: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit fixture: %v", err)
	}
	return db, tenant
}

func inTx(t *testing.T, db *pgtest.DB, fn func(dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("tx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// TestApplyAdvancesIdempotentlyAndRejectsGaps proves DATA-006's contract for
// the critical projection checkpoint: applying the next sequence advances
// the watermark exactly once, re-applying an already-applied sequence is a
// no-op rather than a second effect, and skipping ahead is refused rather
// than silently advancing past a missing event.
func TestApplyAdvancesIdempotentlyAndRejectsGaps(t *testing.T) {
	db, tenant := newFixture(t)
	digest1 := strings.Repeat("1", 64)
	digest2 := strings.Repeat("2", 64)

	var result projection.ApplyResult
	inTx(t, db, func(tx dbport.Tx) error {
		var err error
		result, err = projection.Apply(context.Background(), tx, projection.ApplyRequest{
			Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey, Sequence: 1, Digest: digest1,
		})
		return err
	})
	if !result.Applied || result.Checkpoint.LastAppliedSequence != 1 {
		t.Fatalf("first apply: %+v", result)
	}

	// Re-applying the same sequence is idempotent: no error, Applied=false,
	// and the watermark does not move.
	inTx(t, db, func(tx dbport.Tx) error {
		var err error
		result, err = projection.Apply(context.Background(), tx, projection.ApplyRequest{
			Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey, Sequence: 1, Digest: digest1,
		})
		return err
	})
	if result.Applied {
		t.Fatal("expected re-applying sequence 1 to be a no-op")
	}
	if result.Checkpoint.LastAppliedSequence != 1 {
		t.Fatalf("watermark moved on a duplicate apply: %d", result.Checkpoint.LastAppliedSequence)
	}

	// Skipping ahead (sequence 3 when the watermark is at 1) is refused.
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	_, gapErr := projection.Apply(ctx, tx, projection.ApplyRequest{
		Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey, Sequence: 3, Digest: digest2,
	})
	_ = tx.Rollback(ctx)
	if gapErr == nil {
		t.Fatal("expected a sequence gap to be refused")
	}
	var gapTyped projection.ErrSequenceGap
	if !isErrSequenceGap(gapErr, &gapTyped) {
		t.Fatalf("expected ErrSequenceGap, got %T: %v", gapErr, gapErr)
	}

	// The correct next sequence (2) still advances cleanly after the refused
	// gap attempt rolled back.
	inTx(t, db, func(tx dbport.Tx) error {
		var err error
		result, err = projection.Apply(context.Background(), tx, projection.ApplyRequest{
			Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey, Sequence: 2, Digest: digest2,
		})
		return err
	})
	if !result.Applied || result.Checkpoint.LastAppliedSequence != 2 {
		t.Fatalf("apply sequence 2: %+v", result)
	}

	cp, err := projection.Read(context.Background(), db.Conn, tenant, projectionName, streamKey)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if cp.LastAppliedSequence != 2 || cp.LastAppliedDigest != digest2 {
		t.Fatalf("final checkpoint = %+v", cp)
	}
}

func isErrSequenceGap(err error, target *projection.ErrSequenceGap) bool {
	if e, ok := err.(projection.ErrSequenceGap); ok {
		*target = e
		return true
	}
	return false
}

// TestReconcilerCatchesUpFromLedger proves DATA-010's rebuild contract for
// this slice: a projection that never advanced synchronously (no
// outbox.Commit ever ran for it - the stand-in for "a projection registered
// after events already existed") is caught up to the stream head by
// replaying the ledger directly, and a projection already current is a
// no-op.
func TestReconcilerCatchesUpFromLedger(t *testing.T) {
	db, tenant := newFixture(t)
	ctx := context.Background()

	const schemaRef = "hcmnext.intents.v1.BusinessIntent@1"
	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.BusinessIntent', 1,
			'hcmnext.intents.v1.BusinessIntent', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, schemaRef)

	// Append three events directly through the ledger, bypassing
	// outbox.Commit entirely - the projection checkpoint never moves off
	// zero on its own.
	for i := 0; i < 3; i++ {
		inTx(t, db, func(tx dbport.Tx) error {
			_, err := ledger.Append(ctx, tx, ledger.AppendRequest{
				Tenant:         tenant,
				StreamKey:      streamKey,
				ExpectedHead:   int64(i),
				AssertionClass: ledger.TransactionFact,
				SourceRef:      "hcmnext:test",
				SchemaRef:      schemaRef,
				Payload:        []byte("event"),
				OccurredAt:     time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC),
				EffectiveAt:    time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
				CorrelationID:  uuid.New(),
				IdempotencyKey: uuid.NewString(),
			})
			return err
		})
	}

	cpBefore, err := projection.Read(ctx, db.Conn, tenant, projectionName, streamKey)
	if err != nil {
		t.Fatalf("read before reconcile: %v", err)
	}
	if cpBefore.LastAppliedSequence != 0 {
		t.Fatalf("checkpoint moved without a reconciler: %d", cpBefore.LastAppliedSequence)
	}

	due, err := projection.ReconcileDue(ctx, db.Conn)
	if err != nil {
		t.Fatalf("reconcile due: %v", err)
	}
	if len(due) != 1 || due[0].StreamKey != streamKey || due[0].ProjectionName != projectionName {
		t.Fatalf("due = %+v, want exactly this one checkpoint", due)
	}

	reconciler := projection.NewReconciler(db.Conn, ledger.NewReader())
	applied, err := reconciler.ReconcileOne(ctx, due[0])
	if err != nil {
		t.Fatalf("reconcile one: %v", err)
	}
	if applied != 3 {
		t.Fatalf("applied %d events, want 3", applied)
	}

	cpAfter, err := projection.Read(ctx, db.Conn, tenant, projectionName, streamKey)
	if err != nil {
		t.Fatalf("read after reconcile: %v", err)
	}
	if cpAfter.LastAppliedSequence != 3 {
		t.Fatalf("checkpoint after reconcile = %d, want 3", cpAfter.LastAppliedSequence)
	}

	// A projection already current reconciles to zero applied events and
	// drops off the due list.
	dueAfter, err := projection.ReconcileDue(ctx, db.Conn)
	if err != nil {
		t.Fatalf("reconcile due (after): %v", err)
	}
	if len(dueAfter) != 0 {
		t.Fatalf("due after reconcile = %+v, want none", dueAfter)
	}
	appliedAgain, err := reconciler.ReconcileOne(ctx, projection.StreamProjection{Tenant: tenant, ProjectionName: projectionName, StreamKey: streamKey})
	if err != nil {
		t.Fatalf("reconcile one (already current): %v", err)
	}
	if appliedAgain != 0 {
		t.Fatalf("reconciling an already-current projection applied %d events, want 0", appliedAgain)
	}
}
