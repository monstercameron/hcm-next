package main

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/projection"
	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

const (
	integrationProjectionName = "worker_state"
	integrationStreamKey      = "worker:1"
)

// schemaScopedPool opens a pool against db's isolated test schema: db.URL
// alone connects to the server's default search_path, so the schema
// pgtest.New created (and applied every migration to) has to be pinned
// explicitly the same way pgtest.NewEmpty pins it for db.SQL.
func schemaScopedPool(t *testing.T, db *pgtest.DB) *pgxadapter.Pool {
	t.Helper()
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestProjectorIntegrationCatchesUpViaBootstrap proves projector's real,
// production Spec - config, health, the projection-reconciler Workload,
// and ordered shutdown - works end to end against a real PostgreSQL
// server: three ledger events appended without ever going through
// outbox.Commit (the checkpoint never advances synchronously, the same
// setup internal/data/projection's own TestReconcilerCatchesUpFromLedger
// uses) are caught up by the running process, advancing the checkpoint from
// 0 to 3, and bootstrap.Run then shuts down cleanly (exit code 0) once its
// context is canceled.
//
// This is projector's pgtest-backed TestTodo_SVC_007_Integration,
// complementing the no-database TestTodo_SVC_007 unit tests in
// reconcile_test.go.
func TestTodo_SVC_007_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	ctx := context.Background()

	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'acme', 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant)

	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin fixture tx: %v", err)
	}
	if err := datalogger.EnsureStream(ctx, tx, tenant, integrationStreamKey, "WORKER", integrationStreamKey); err != nil {
		t.Fatalf("ensure stream: %v", err)
	}
	if err := projection.EnsureProjection(ctx, tx, tenant, integrationProjectionName, integrationStreamKey); err != nil {
		t.Fatalf("ensure projection: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit fixture tx: %v", err)
	}

	const schemaRef = "hcmnext.intents.v1.BusinessIntent@1"
	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.BusinessIntent', 1,
			'hcmnext.intents.v1.BusinessIntent', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, schemaRef)

	// Append three events directly through the ledger, bypassing
	// outbox.Commit entirely, so the projection checkpoint never moves off
	// zero on its own - projector's reconcile loop is the only thing that
	// can catch it up.
	for i := 0; i < 3; i++ {
		itx, err := db.Conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin append tx: %v", err)
		}
		if _, err := datalogger.Append(ctx, itx, datalogger.AppendRequest{
			Tenant:         tenant,
			StreamKey:      integrationStreamKey,
			ExpectedHead:   int64(i),
			AssertionClass: datalogger.TransactionFact,
			SourceRef:      "hcmnext:test",
			SchemaRef:      schemaRef,
			Payload:        []byte("event"),
			OccurredAt:     time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC),
			EffectiveAt:    time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			CorrelationID:  uuid.New(),
			IdempotencyKey: uuid.NewString(),
		}); err != nil {
			itx.Rollback(ctx)
			t.Fatalf("append event %d: %v", i, err)
		}
		if err := itx.Commit(ctx); err != nil {
			t.Fatalf("commit append tx %d: %v", i, err)
		}
	}

	cpBefore, err := projection.Read(ctx, db.Conn, tenant, integrationProjectionName, integrationStreamKey)
	if err != nil {
		t.Fatalf("read checkpoint before reconcile: %v", err)
	}
	if cpBefore.LastAppliedSequence != 0 {
		t.Fatalf("checkpoint moved without a reconciler: %d", cpBefore.LastAppliedSequence)
	}

	pool := schemaScopedPool(t, db)

	s := spec([]string{"-database-url=postgres://ignored/db", "-poll-interval=25ms"})
	s.Getenv = func(string) (string, bool) { return "", false }
	s.DBPoolFactory = func(context.Context, string) (bootstrap.DBPool, error) {
		return pool, nil
	}
	s.Logger = discardLogger()
	s.Stdout = io.Discard
	s.Stderr = io.Discard

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- bootstrap.Run(runCtx, s) }()

	deadline := time.Now().Add(20 * time.Second)
	var applied int64
	for time.Now().Before(deadline) {
		cp, err := projection.Read(ctx, pool, tenant, integrationProjectionName, integrationStreamKey)
		if err != nil {
			t.Fatalf("read checkpoint: %v", err)
		}
		applied = cp.LastAppliedSequence
		if applied == 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if applied != 3 {
		t.Fatalf("checkpoint after deadline = %d, want 3", applied)
	}

	cancel()
	select {
	case code := <-done:
		if code != bootstrap.ExitOK {
			t.Fatalf("bootstrap.Run exit code = %d, want ExitOK", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("bootstrap.Run did not return after its context was canceled")
	}
}
