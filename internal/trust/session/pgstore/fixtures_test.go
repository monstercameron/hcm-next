package pgstore_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// fixedInstant is the clock every fixture in this package stamps.
var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// newTenantID mints a fresh, random, canonical-uuid-shaped
// [values.TenantId] -- valid both as [values.TenantId.Validate]'s slug
// character class and as pgstore's own canonicalUUID check -- without this
// package importing github.com/google/uuid (internal/trust is not among
// that module's allowed_import_roots).
func newTenantID() values.TenantId {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" +
		hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:16])
	return values.TenantId(s)
}

// appConn opens a fresh connection on db's schema and assumes the
// least-privilege hcmnext_app role, matching every other data-plane test
// package's own appConn helper (see internal/data/runtimestate/fixtures_test.go).
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// insertTenant registers one active tenant, as the migration/admin role,
// keyed by tenantID's own uuid string as both tenant_id and tenant_key --
// this package's [values.TenantId] convention (matching
// internal/authn/issuerregistry's identical choice) is that the storage
// uuid is the tenant identity callers pass around, not a separate slug.
func insertTenant(t *testing.T, db *pgtest.DB, tenantID values.TenantId) {
	t.Helper()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1::uuid, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID.String(), tenantID.String(), "tenant "+tenantID.String())
}
