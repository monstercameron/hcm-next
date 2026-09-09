package seed_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/seed"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

// TestTodo_DB_019 proves the two halves of DB-019 together: seeding a tenant
// once writes the full plan -- both the definition_version registrations and,
// exactly once, the person/worker/.../compensation_band aggregate rows -- and
// seeding the same tenant again -- a separate transaction, modelling a
// redeployment or a rerun of a seed command -- is a no-op that neither
// duplicates rows (in either substrate) nor changes the reported digest.
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
	if first.Aggregates == nil {
		t.Fatal("first seed did not report the aggregate fixtures it loaded")
	}
	if len(first.Aggregates.WorkerID) == 0 {
		t.Fatal("first seed's aggregate fixtures loaded no workers")
	}

	if got := countDefinitionVersions(t, db, tenantID); got != len(plan) {
		t.Fatalf("definition_version holds %d rows after one seed, want %d", got, len(plan))
	}
	workersAfterFirst := countRows(t, db, "worker", tenantID)
	bandsAfterFirst := countRows(t, db, "compensation_band", tenantID)
	if workersAfterFirst == 0 {
		t.Fatal("worker holds no rows after one seed")
	}
	if bandsAfterFirst == 0 {
		t.Fatal("compensation_band holds no rows after one seed")
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
	if second.Aggregates != nil {
		t.Fatal("second seed reported aggregate fixtures again; the aggregateCorpusKey marker should have skipped it")
	}

	if got := countDefinitionVersions(t, db, tenantID); got != len(plan) {
		t.Fatalf("definition_version holds %d rows after two seeds, want %d (a rerun must not duplicate rows)",
			got, len(plan))
	}
	if got := countRows(t, db, "worker", tenantID); got != workersAfterFirst {
		t.Fatalf("worker holds %d rows after two seeds, want %d (a rerun must not duplicate aggregate rows)",
			got, workersAfterFirst)
	}
	if got := countRows(t, db, "compensation_band", tenantID); got != bandsAfterFirst {
		t.Fatalf("compensation_band holds %d rows after two seeds, want %d (a rerun must not duplicate aggregate rows)",
			got, bandsAfterFirst)
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
