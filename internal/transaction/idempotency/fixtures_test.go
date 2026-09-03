package idempotency_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// fixedInstant is the clock every fixture stamps. This package never reads a
// wall clock, so a test that wants a time has to say which one.
var fixedInstant = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// digestOf returns a well-formed canonical request digest over seed, purely
// as a test fixture -- this package never computes its own digests, it only
// ever stores and compares the ones a caller supplies.
func digestOf(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// insertTenant registers one active tenant as the migration/admin role (the
// pgtest connection is a superuser and is therefore never itself subject to
// row level security) and returns its identifier.
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// appConn opens a fresh connection on db's schema and assumes the
// least-privilege hcmnext_app role, which is the only way a test observes the
// row level security policy migration 00019 declares: the pgtest URL
// authenticates as a superuser, and a superuser bypasses every policy.
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// inTx runs fn inside its own transaction on conn and commits it.
func inTx(t *testing.T, conn *pgxadapter.Conn, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTxErr(conn, fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

// inTxErr is inTx for a call whose own error the test wants to inspect. A
// failing fn rolls the transaction back, so a refused write leaves nothing
// behind.
func inTxErr(conn *pgxadapter.Conn, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// inTenantTx is inTx with the tenant scope set as its first statement.
func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	return inTxErr(conn, func(tx dbport.Tx) error {
		if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
			return err
		}
		return fn(tx)
	})
}

// defaultScope builds one Scope for tenant with distinguishing suffix, so
// tests running in parallel never collide on the table's primary key.
func defaultScope(tenant uuid.UUID, suffix string) idempotency.Scope {
	return idempotency.Scope{
		Tenant:      tenant,
		Capability:  "promotion.approve.v1",
		EffectScope: "worker:employment_change",
		Key:         "key-" + suffix,
	}
}

// defaultPolicy is a retention policy every test that does not specifically
// exercise the RED "retention shorter than retry window" case can reuse.
var defaultPolicy = idempotency.RetentionPolicy{
	Retention:   30 * 24 * time.Hour,
	RetryWindow: 24 * time.Hour,
}
