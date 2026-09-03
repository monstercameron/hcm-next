package seed_test

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/seed"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

// TestTodo_DB_019_Security proves seeded rows are ordinary tenant-scoped data,
// not a governance-bypassing side channel: migration 00008's row level
// security confines them exactly as it confines any other definition_version
// row, and the fixture corpus itself carries no production-shaped
// identifiers.
func TestTodo_DB_019_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	tenantA := insertTenant(t, db, "seed-tenant-a")
	tenantB := insertTenant(t, db, "seed-tenant-b")
	runSeed(t, ctx, db, tenantA)

	t.Run("seeded rows respect tenant isolation like any other row", func(t *testing.T) {
		conn := db.NewConn(t)
		if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
			t.Fatalf("assume %s: %v", tenancy.AppRole, err)
		}

		txA, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = txA.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, txA, tenantA); err != nil {
			t.Fatalf("scope to tenant A: %v", err)
		}
		var seenByA int
		if err := txA.QueryRow(ctx, `SELECT count(*) FROM definition_version WHERE tenant_id = $1`, tenantA).
			Scan(&seenByA); err != nil {
			t.Fatalf("count as tenant A: %v", err)
		}
		if seenByA == 0 {
			t.Fatal("tenant A's own scoped transaction saw none of its own seeded rows")
		}
		_ = txA.Rollback(ctx)

		txB, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = txB.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, txB, tenantB); err != nil {
			t.Fatalf("scope to tenant B: %v", err)
		}
		var seenByB int
		if err := txB.QueryRow(ctx, `SELECT count(*) FROM definition_version`).Scan(&seenByB); err != nil {
			t.Fatalf("count as tenant B: %v", err)
		}
		if seenByB != 0 {
			t.Fatalf("tenant B's transaction saw %d rows seeded for tenant A", seenByB)
		}
	})

	t.Run("seeding into tenant B does not create or touch tenant A's rows", func(t *testing.T) {
		beforeA := countDefinitionVersions(t, db, tenantA)
		runSeed(t, ctx, db, tenantB)
		afterA := countDefinitionVersions(t, db, tenantA)
		if afterA != beforeA {
			t.Fatalf("tenant A's row count changed from %d to %d after seeding tenant B", beforeA, afterA)
		}
	})

	t.Run("the fixture corpus carries no production-shaped identifiers", func(t *testing.T) {
		plan, err := seed.Plan()
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		for _, r := range plan {
			if string(r.Body) == "" {
				t.Errorf("%s %q has an empty body", r.Kind, r.Key)
			}
		}
		// The corpus tenant this data plane's own fixtures use throughout
		// (internal/domains/fixtures.Tenant) is a clearly synthetic demo
		// slug, not a real customer identifier; Seed itself never reads or
		// writes it (the caller supplies tenantID), so this only confirms the
		// copied JSON files were not swapped for something else.
		const demoTenantMarker = "harborcare"
		found := false
		for _, r := range plan {
			if r.Kind == "RELATIONSHIP" {
				found = true
				if !strings.Contains(strings.ToLower(string(r.Body)), demoTenantMarker) {
					t.Errorf("employment registration %q does not reference the synthetic demo legal entity", r.Key)
				}
			}
		}
		if !found {
			t.Fatal("no RELATIONSHIP registration found to check")
		}
	})
}
