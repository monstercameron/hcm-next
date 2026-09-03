package seed_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
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
