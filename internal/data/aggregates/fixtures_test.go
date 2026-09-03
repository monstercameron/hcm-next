package aggregates_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

// insertTenant registers one active tenant and returns its identifier. It
// mirrors internal/data/schema's own helper of the same shape; this package
// does not import internal/data/schema (see doc.go's import-boundary note),
// so the handful of lines are duplicated rather than shared.
func insertTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, "tenant-"+id.String()[:8], "Tenant "+id.String()[:8])
	return id
}

// asAppRole opens a fresh connection scoped to hcmnext_app under tenant, the
// same way a request-scoped connection would reach these tables in
// production (migrations/00008_tenant_isolation.sql's row level security
// policies are enforced for hcmnext_app, never for the migration-running
// superuser db.Conn otherwise uses). set_config's third argument is false
// (session-scoped, not is_local) because the test issues several separate
// statements on this connection rather than one explicit transaction.
func asAppRole(t *testing.T, db *pgtest.DB, tenant uuid.UUID) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	ctx := context.Background()
	if _, err := conn.Exec(ctx, "SET ROLE hcmnext_app"); err != nil {
		t.Fatalf("SET ROLE hcmnext_app: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('app.tenant_id', $1, false)`, tenant.String()); err != nil {
		t.Fatalf("set app.tenant_id: %v", err)
	}
	return conn
}

func mustParse(t *testing.T, layout, value string) time.Time {
	t.Helper()
	ts, err := time.Parse(layout, value)
	if err != nil {
		t.Fatalf("parse %q as %q: %v", value, layout, err)
	}
	return ts.UTC()
}

func date(t *testing.T, s string) time.Time    { return mustParse(t, "2006-01-02", s) }
func instant(t *testing.T, s string) time.Time { return mustParse(t, time.RFC3339, s) }

// registerStandinEntity inserts a bare row into aggregate_entity for a
// synthetic id a test wants to use as a cross-entity reference (a worker_ref,
// budget_ref, ...) without building the full entity that would ordinarily
// register it via Put. Every table's own entity_id and every reference
// column now foreign-keys to aggregate_entity (migrations/00011), so a test
// that references an id it never actually Put needs this instead.
func registerStandinEntity(t *testing.T, db *pgtest.DB, tenant, entityID uuid.UUID, kind string) {
	t.Helper()
	db.Exec(t, `
		INSERT INTO aggregate_entity (tenant_id, entity_id, kind, canonical_id)
		VALUES ($1, $2, $3, $4)`,
		tenant, entityID, kind, "eid:v1:"+kind+":"+entityID.String())
}

// inTx runs fn inside its own transaction on db.Conn and commits it,
// failing the test on any error. Every Put call that might supersede an
// existing row needs its transaction's atomicity (see store.go's Executor
// doc), so tests route those calls through inTx rather than db.Conn
// directly.
func inTx(t *testing.T, db *pgtest.DB, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("tx body: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
}

// inTxErr is inTx for a call whose own error the test wants to inspect
// rather than fail on: fn's error is returned, and the transaction is rolled
// back on any error (including one from Commit, which is then folded into
// the returned error rather than failing the test outright).
func inTxErr(t *testing.T, db *pgtest.DB, fn func(tx dbport.Tx) error) error {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}
