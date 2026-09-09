package seed_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// insertTenant registers one active tenant, as the migration/admin role, and
// returns its identifier.
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// countDefinitionVersions counts the definition_version rows visible to db's
// admin connection for tenantID.
func countDefinitionVersions(t *testing.T, db *pgtest.DB, tenantID uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.Conn.QueryRow(context.Background(),
		`SELECT count(*) FROM definition_version WHERE tenant_id = $1`, tenantID).Scan(&n); err != nil {
		t.Fatalf("count definition_version: %v", err)
	}
	return n
}

// countRows counts table's rows for tenantID, visible to db's admin
// connection. table is always one of this package's own hard-coded
// constants, never request input, so building the query with fmt.Sprintf
// carries no injection risk.
func countRows(t *testing.T, db *pgtest.DB, table string, tenantID uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.Conn.QueryRow(context.Background(),
		fmt.Sprintf(`SELECT count(*) FROM %s WHERE tenant_id = $1`, table), tenantID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}
