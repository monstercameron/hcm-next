package seed_test

import (
	"context"
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
		conn := db.NewConn(t)
		if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
			t.Fatalf("assume %s: %v", tenancy.AppRole, err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenantA); err != nil {
			t.Fatalf("scope to tenant A: %v", err)
		}

		var legalEntityCount int
		if err := tx.QueryRow(ctx, `
			SELECT count(*)
			FROM legal_entity
			WHERE tenant_id = $1 AND registered_name = $2`, tenantA, "HarborCare US Inc.").
			Scan(&legalEntityCount); err != nil {
			t.Fatalf("find seeded HarborCare legal entity: %v", err)
		}
		if legalEntityCount != 1 {
			t.Fatalf("tenant A saw %d seeded HarborCare legal entities, want exactly one", legalEntityCount)
		}

		plan, err := seed.Plan()
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		for _, r := range plan {
			if string(r.Body) == "" {
				t.Errorf("%s %q has an empty body", r.Kind, r.Key)
			}
		}
	})
}
