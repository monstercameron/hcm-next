package uow_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/aggregates"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/data/uow"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// fixedInstant is the clock every fixture in this package stamps, mirroring
// internal/data/runtimestate's own fixtures_test.go convention.
var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// insertTenant registers one active tenant as the migration/admin role.
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// registerStandinEntity inserts a bare row into aggregate_entity for a
// synthetic id this package wants to use as a worker's person_ref without
// building a full Person through internal/data/aggregates. Every DB-018
// table's own entity_id, and every cross-entity reference column, foreign
// keys to aggregate_entity (migrations/00011), so a test that references an
// id it never actually appended a revision for needs this instead.
func registerStandinEntity(t *testing.T, db *pgtest.DB, tenant, entityID uuid.UUID, kind string) {
	t.Helper()
	db.Exec(t, `
		INSERT INTO aggregate_entity (tenant_id, entity_id, kind, canonical_id)
		VALUES ($1, $2, $3, $4)`,
		tenant, entityID, kind, "eid:v1:"+kind+":"+entityID.String())
}

// appConn opens a fresh connection on db's schema, assumes the
// least-privilege hcmnext_app role, and scopes the session (not merely one
// transaction -- set_config's third argument is false) to tenant, mirroring
// internal/data/aggregates' own asAppRole test helper. [uow.Begin] still binds
// its own transaction to whatever tenant its caller passes it (via
// [tenancy.WithTenant], which this session-level setting does not
// substitute for in production, where one pooled connection outlives many
// tenants' requests); the session-level scope here exists only so this
// package's tests can Load/ListRevisions directly on conn, outside any unit
// of work, to verify what a transaction actually committed.
func appConn(t *testing.T, db *pgtest.DB, tenant uuid.UUID) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	ctx := context.Background()
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, tenancy.SessionSetting, tenant.String()); err != nil {
		t.Fatalf("set %s: %v", tenancy.SessionSetting, err)
	}
	return conn
}

// inTenantTx runs fn inside its own transaction on conn, scoped to tenant as
// the transaction's first statement, and commits it.
func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// newPersonRef registers a standin Person entity a Worker fixture can
// reference, and returns its id.
func newPersonRef(t *testing.T, db *pgtest.DB, tenant uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	registerStandinEntity(t, db, tenant, id, "person")
	return id
}

// workerBuilder returns a [uow.ConformanceCase.Build] function that produces
// the seq'th Worker revision for entityID: effective from well before
// businessAt and open-ended (so it stays "current as of businessAt" for
// every seq), referencing personRef. recordedAt strictly increases with seq
// -- migrations/00011's worker_superseded_after_recorded CHECK requires the
// row a new revision supersedes to have been recorded strictly before the
// new revision's own RecordedAt (the value PostgresWorkerRepository.Save
// writes as that predecessor's superseded_at) -- so seq must be called in
// order, 1, 2, 3, ..., for one entityID.
func workerBuilder(t *testing.T, tenant, personRef uuid.UUID) func(entityID uuid.UUID, seq int, businessAt time.Time) aggregates.Worker {
	t.Helper()
	return func(entityID uuid.UUID, seq int, businessAt time.Time) aggregates.Worker {
		effectiveFrom := businessAt.Add(-30 * 24 * time.Hour)
		recordedAt := businessAt.Add(time.Duration(seq) * time.Hour)
		w, err := aggregates.NewWorker(tenant, entityID, personRef, effectiveFrom, nil, recordedAt,
			workerNumberFor(seq), "EMPLOYEE", workerLifecycleFor(seq))
		if err != nil {
			t.Fatalf("NewWorker (seq %d): %v", seq, err)
		}
		return w
	}
}

func workerNumberFor(seq int) string {
	switch seq {
	case 1:
		return "W-0001"
	case 2:
		return "W-0001-A"
	default:
		return "W-0001-B"
	}
}

func workerLifecycleFor(seq int) string {
	if seq == 1 {
		return "PENDING"
	}
	return "ACTIVE"
}

// beginWorkerUnit begins a UnitOfWork on conn for tenant and registers repo
// under kind "worker", failing the test on any error.
func beginWorkerUnit(t *testing.T, ctx context.Context, conn *pgxadapter.Conn, tenant uuid.UUID, repo uow.PostgresWorkerRepository) *uow.UnitOfWork {
	t.Helper()
	u, err := uow.Begin(ctx, conn, tenant)
	if err != nil {
		t.Fatalf("uow.Begin: %v", err)
	}
	if err := uow.Register(u, "worker", repo); err != nil {
		t.Fatalf("uow.Register: %v", err)
	}
	return u
}
