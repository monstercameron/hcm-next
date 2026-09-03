package seed_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/seed"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

// TestTodo_DB_019 proves the two halves of DB-019 together: seeding a tenant
// once writes the full plan, and seeding the same tenant again -- a separate
// transaction, modelling a redeployment or a rerun of a seed command -- is a
// no-op that neither duplicates rows nor changes the reported digest.
func TestTodo_DB_019(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "seed-tenant")

	plan, err := seed.Plan()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	wantDigest := seed.ArtifactDigest(plan)

	first := runSeed(t, ctx, db, tenantID)
	if first.Inserted != len(plan) {
		t.Fatalf("first seed inserted %d, want %d", first.Inserted, len(plan))
	}
	if first.Skipped != 0 {
		t.Fatalf("first seed skipped %d, want 0", first.Skipped)
	}
	if first.Digest != wantDigest {
		t.Fatalf("first seed digest %s, want %s", first.Digest, wantDigest)
	}

	if got := countDefinitionVersions(t, db, tenantID); got != len(plan) {
		t.Fatalf("definition_version holds %d rows after one seed, want %d", got, len(plan))
	}

	second := runSeed(t, ctx, db, tenantID)
	if second.Inserted != 0 {
		t.Fatalf("second seed inserted %d, want 0 (a rerun must be a no-op)", second.Inserted)
	}
	if second.Skipped != len(plan) {
		t.Fatalf("second seed skipped %d, want %d", second.Skipped, len(plan))
	}
	if second.Digest != wantDigest {
		t.Fatalf("second seed digest %s, want %s (the digest must not drift between runs)", second.Digest, wantDigest)
	}

	if got := countDefinitionVersions(t, db, tenantID); got != len(plan) {
		t.Fatalf("definition_version holds %d rows after two seeds, want %d (a rerun must not duplicate rows)",
			got, len(plan))
	}
}

// runSeed opens its own transaction, seeds tenantID, and commits.
func runSeed(t *testing.T, ctx context.Context, db *pgtest.DB, tenantID uuid.UUID) seed.Summary {
	t.Helper()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	summary, err := seed.Seed(ctx, tx, tenantID)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("seed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return summary
}
