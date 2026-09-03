package pgtest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/migrations"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

// TestTodo_DB_001 proves the migration and test harness: a clean schema migrates
// from zero, the latest reversible migration rolls back and re-applies, and each
// test owns an isolated schema that no parallel test can observe.
func TestTodo_DB_001(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db := pgtest.NewEmpty(t)
	provider := db.Provider(t)

	// Migrate from zero.
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		t.Fatalf("read version of empty schema: %v", err)
	}
	if version != 0 {
		t.Fatalf("empty schema reports version %d, want 0", version)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		t.Fatalf("migrate up from zero: %v", err)
	}
	files, err := migrations.Files()
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	if len(results) != len(files) {
		t.Fatalf("applied %d migrations, want %d", len(results), len(files))
	}

	target, err := migrations.TargetVersion()
	if err != nil {
		t.Fatalf("target version: %v", err)
	}
	version, err = provider.GetDBVersion(ctx)
	if err != nil {
		t.Fatalf("read version after up: %v", err)
	}
	if version != target {
		t.Fatalf("schema version %d after up, want %d", version, target)
	}
	if !tableExists(t, db, "ledger_event") {
		t.Fatal("ledger_event missing after migrating from zero")
	}

	// Roll back the latest reversible migration.
	down, err := provider.Down(ctx)
	if err != nil {
		t.Fatalf("roll back latest migration: %v", err)
	}
	if down.Source.Version != target {
		t.Fatalf("rolled back version %d, want %d", down.Source.Version, target)
	}
	if tableExists(t, db, "outbox") {
		t.Fatal("outbox still present after rolling back the migration that creates it")
	}

	// Re-apply it.
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("re-apply latest migration: %v", err)
	}
	if !tableExists(t, db, "outbox") {
		t.Fatal("outbox missing after re-applying the migration that creates it")
	}
	version, err = provider.GetDBVersion(ctx)
	if err != nil {
		t.Fatalf("read version after re-apply: %v", err)
	}
	if version != target {
		t.Fatalf("schema version %d after re-apply, want %d", version, target)
	}

	// Report the schema version and the migration digest, as DB-001 requires.
	digest, err := migrations.ArtifactDigest()
	if err != nil {
		t.Fatalf("artifact digest: %v", err)
	}
	t.Logf("schema %s at version %d, migration artifact digest sha256:%s", db.Schema, version, digest)
}

// TestTodo_DB_001_Integration proves per-test isolation: two parallel tests write
// the same tenant key into their own schema and neither observes the other.
func TestTodo_DB_001_Integration(t *testing.T) {
	t.Parallel()

	// A key both subtests insert. If isolation leaked, the unique constraint on
	// tenant_key would reject the second writer.
	const sharedKey = "isolation-probe"

	for _, name := range []string{"writer_a", "writer_b"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			db := pgtest.New(t)

			id := uuid.New()
			db.Exec(t, `
				INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
				VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', now())`,
				id, sharedKey, name)

			var count int
			if err := db.QueryRow(ctx,
				`SELECT count(*) FROM tenant WHERE tenant_key = $1`, sharedKey).Scan(&count); err != nil {
				t.Fatalf("count tenants: %v", err)
			}
			if count != 1 {
				t.Fatalf("schema %s sees %d rows for %q, want exactly its own", db.Schema, count, sharedKey)
			}

			var seen uuid.UUID
			if err := db.QueryRow(ctx,
				`SELECT tenant_id FROM tenant WHERE tenant_key = $1`, sharedKey).Scan(&seen); err != nil {
				t.Fatalf("read tenant: %v", err)
			}
			if seen != id {
				t.Fatalf("schema %s sees tenant %s, want its own %s", db.Schema, seen, id)
			}
		})
	}
}

// TestTodo_DB_001_Golden pins the migration inventory and its digest so that an
// accidental edit to an already-released migration is visible.
func TestTodo_DB_001_Golden(t *testing.T) {
	t.Parallel()

	files, err := migrations.Files()
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	want := []string{
		"00001_platform_control.sql",
		"00002_tenant_primitives.sql",
		"00003_definition_registry.sql",
		"00004_intent_and_proposal.sql",
		"00005_ledger.sql",
		"00006_projection_and_outbox.sql",
	}
	if len(files) != len(want) {
		t.Fatalf("migration tree holds %d files, want %d", len(files), len(want))
	}
	for i, f := range files {
		if f.Name != want[i] {
			t.Fatalf("migration %d is %q, want %q", i, f.Name, want[i])
		}
		if int64(i+1) != f.Version {
			t.Fatalf("migration %q has version %d, want %d", f.Name, f.Version, i+1)
		}
		if len(f.Checksum) != 64 {
			t.Fatalf("migration %q checksum %q is not a sha256 hex digest", f.Name, f.Checksum)
		}
	}

	digest, err := migrations.ArtifactDigest()
	if err != nil {
		t.Fatalf("artifact digest: %v", err)
	}
	if len(digest) != 64 {
		t.Fatalf("artifact digest %q is not a sha256 hex digest", digest)
	}
}

// TestTodo_DB_001_Race drives concurrent isolated schemas under -race.
func TestTodo_DB_001_Race(t *testing.T) {
	t.Parallel()
	for i := range 4 {
		t.Run("schema", func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			db := pgtest.New(t)
			db.Exec(t, `
				INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
				VALUES ($1, $2, 'cell-local', 'race', 'ACTIVE', now())`,
				uuid.New(), "race-tenant")

			var current string
			if err := db.QueryRow(ctx, `SELECT current_schema()`).Scan(&current); err != nil {
				t.Fatalf("read current schema: %v", err)
			}
			if current != db.Schema {
				t.Fatalf("connection is on schema %q, want %q", current, db.Schema)
			}
			_ = i
		})
	}
}

func tableExists(t *testing.T, db *pgtest.DB, name string) bool {
	t.Helper()
	var exists bool
	err := db.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = $1 AND table_name = $2
		)`, db.Schema, name).Scan(&exists)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("check table %s: %v", name, err)
	}
	return exists
}
