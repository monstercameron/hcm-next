package tenancy_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

// insertTenant registers one active tenant, as the migration/admin role (the
// pgtest connection, which is a PostgreSQL superuser and so is never subject
// to row level security itself), and returns its identifier.
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// insertAuthorityAssignment writes one minimal tenant-scoped row, as the
// admin role. authority_assignment is used across this package's fixtures
// because it needs nothing beyond a registered tenant: no ledger stream,
// schema or stream head to stand up first.
func insertAuthorityAssignment(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, authorityRef string) {
	t.Helper()
	db.Exec(t, `
		INSERT INTO authority_assignment (tenant_id, authority_ref, authority_kind, domain_scope, effective_from)
		VALUES ($1, $2, 'INTERNAL', 'workforce.compensation', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, authorityRef)
}

// appRoleConn opens a fresh connection on db's schema and assumes the
// hcmnext_app role on it. The pgtest connection URL authenticates as the
// PostgreSQL superuser (embedded-postgres provides no other user), so tests
// reach the least-privilege role the same way a production bootstrap
// connection would: by asking to become it, which any role a superuser holds
// membership in (or, as here, any role at all, since the session started as
// superuser) may always do.
func appRoleConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// scopedTx begins a transaction on conn and scopes it to tenantID.
func scopedTx(t *testing.T, ctx context.Context, conn *pgxadapter.Conn, tenantID uuid.UUID) dbport.Tx {
	t.Helper()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("scope transaction to tenant %s: %v", tenantID, err)
	}
	return tx
}

// insertLedgerEventFixture stands up the minimum a ledger_event row needs
// (schema, authority, stream, head) and appends one event for tenantID, as
// the admin role. It exists so RLS partition-routing tests have a row that
// physically lands in whichever of ledger_event's four hash partitions its
// tenant_id happens to hash to, without needing to predict which one.
func insertLedgerEventFixture(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, idempotencyKey string) {
	t.Helper()
	schemaRef := "hcmnext.intents.v1.BusinessIntent@1"
	authorityRef := "authority:ledger-fixture"
	streamKey := "worker:" + idempotencyKey

	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.BusinessIntent', 1,
			'hcmnext.intents.v1.BusinessIntent', 'PROTOBUF', 'LEDGER_EVENT')
		ON CONFLICT DO NOTHING`,
		tenantID, schemaRef)

	db.Exec(t, `
		INSERT INTO authority_assignment (tenant_id, authority_ref, authority_kind, domain_scope, effective_from)
		VALUES ($1, $2, 'INTERNAL', 'workforce.compensation', timestamptz '2026-01-01T00:00:00Z')
		ON CONFLICT DO NOTHING`,
		tenantID, authorityRef)

	db.Exec(t, `
		INSERT INTO ledger_stream (tenant_id, stream_key, stream_kind, subject_ref)
		VALUES ($1, $2, 'WORKER', $2)`, tenantID, streamKey)

	db.Exec(t, `
		INSERT INTO stream_head (tenant_id, stream_key, head_sequence)
		VALUES ($1, $2, 0)`, tenantID, streamKey)

	db.Exec(t, `
		INSERT INTO ledger_event (
			tenant_id, stream_key, sequence, event_id, assertion_class, authority_ref,
			source_ref, schema_ref, payload, canonical_length, digest, digest_algorithm,
			occurred_at, effective_at, correlation_id, idempotency_key)
		VALUES ($1, $2, 1, $3, 'DOMAIN_FACT', $4, 'test', $5, $6, $7,
			'1111111111111111111111111111111111111111111111111111111111111111', 'sha256',
			timestamptz '2026-02-01T00:00:00Z', timestamptz '2026-02-01T00:00:00Z', $3, $8)`,
		tenantID, streamKey, uuid.New(), authorityRef, schemaRef,
		[]byte("body"), len("body"), idempotencyKey)
}

// countAuthorityAssignments counts the rows visible to q (a *pgxadapter.Conn or a
// dbport.Tx) in authority_assignment.
func countAuthorityAssignments(t *testing.T, ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) dbport.Row
}) int {
	t.Helper()
	var n int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM authority_assignment`).Scan(&n); err != nil {
		t.Fatalf("count authority_assignment: %v", err)
	}
	return n
}
