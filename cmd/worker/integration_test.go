package main

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

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

// TestWorkerIntegrationDispatchesOutboxViaBootstrap proves worker's real,
// production Spec - config, health, the outbox-consumer Workload, and
// ordered shutdown - works end to end against a real PostgreSQL server: one
// PENDING outbox message for one ACTIVE tenant is claimed, "dispatched"
// (logged) and acked to DELIVERED, and bootstrap.Run then shuts down
// cleanly (exit code 0) once its context is canceled.
//
// This is worker's one pgtest-backed integration test, complementing the
// no-database unit tests in sweep_test.go: SVC-010 will eventually give
// worker a real messaging-delivery role (not implemented here - this
// composition root keeps worker's current plain, logging outbox-consumer
// role), so there is no dedicated todo test ID for this refactor; the name
// below is descriptive rather than a TestTodo_SVC_* id.
func TestWorkerIntegrationDispatchesOutboxViaBootstrap(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	ctx := context.Background()

	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "acme-"+tenant.String())

	const schemaRef = "hcmnext.test.v1.Fixture@1"
	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.test.v1.Fixture', 1,
			'hcmnext.test.v1.Fixture', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, schemaRef)

	msgID := uuid.New()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin fixture tx: %v", err)
	}
	if _, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
		Tenant:         tenant,
		OutboxID:       msgID,
		EffectIdentity: "worker-integration-test",
		OrderingKey:    "worker-integration-test",
		SchemaRef:      schemaRef,
		Payload:        []byte("hello"),
	}); err != nil {
		t.Fatalf("enqueue fixture message: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit fixture tx: %v", err)
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
	var status string
	for time.Now().Before(deadline) {
		row := pool.QueryRow(ctx, `SELECT status FROM outbox WHERE tenant_id = $1 AND outbox_id = $2`, tenant, msgID)
		if err := row.Scan(&status); err != nil {
			t.Fatalf("read outbox row: %v", err)
		}
		if status == outbox.StatusDelivered {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != outbox.StatusDelivered {
		t.Fatalf("outbox message status = %q after deadline, want %q", status, outbox.StatusDelivered)
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
