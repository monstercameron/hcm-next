package schema_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestTodo_DB_007 proves the canonical registry contract in its minimal form:
// definition versions are immutable and carry source and supersession lineage,
// and the active pointer is a convenience that can never invent a version.
func TestTodo_DB_007(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)

	publish := func(version int64, digest string, supersedes any) error {
		return db.ExecErr(`
			INSERT INTO definition_version (
				tenant_id, definition_kind, definition_key, version, definition_digest,
				source_ref, body, supersedes_version, published_by, published_at)
			VALUES ($1, 'INTENT', 'promotion', $2, $3, 'git://specs/promotion.yaml',
				'\x0102', $4, 'publisher', timestamptz '2026-03-01T00:00:00Z')`,
			tenant, version, digest, supersedes)
	}

	if err := publish(1, fixtureDigestA, nil); err != nil {
		t.Fatalf("publish version 1: %v", err)
	}

	t.Run("a published definition is immutable", func(t *testing.T) {
		if err := db.ExecErr(`UPDATE definition_version SET source_ref = 'forged'`); err == nil {
			t.Fatal("a definition version was updated in place")
		}
		if err := db.ExecErr(`DELETE FROM definition_version`); err == nil {
			t.Fatal("a definition version was deleted")
		}
	})

	t.Run("supersession lineage must exist", func(t *testing.T) {
		if err := publish(2, fixtureDigestB, int64(99)); err == nil {
			t.Fatal("a definition superseding a version that does not exist was accepted")
		}
		if err := publish(2, fixtureDigestB, int64(2)); err == nil {
			t.Fatal("a definition superseding itself was accepted")
		}
		if err := publish(2, fixtureDigestB, int64(1)); err != nil {
			t.Fatalf("publishing version 2 superseding version 1: %v", err)
		}
	})

	t.Run("versions begin at one", func(t *testing.T) {
		if err := publish(0, fixtureDigestA, nil); err == nil {
			t.Fatal("definition version 0 was accepted")
		}
	})

	t.Run("the active pointer cannot invent a version", func(t *testing.T) {
		if err := db.ExecErr(`
			INSERT INTO definition_active_pointer (
				tenant_id, definition_kind, definition_key, active_version, effective_from)
			VALUES ($1, 'INTENT', 'promotion', 7, timestamptz '2026-03-01T00:00:00Z')`,
			tenant); err == nil {
			t.Fatal("an active pointer to a version that was never published was accepted")
		}
		db.Exec(t, `
			INSERT INTO definition_active_pointer (
				tenant_id, definition_kind, definition_key, active_version, effective_from)
			VALUES ($1, 'INTENT', 'promotion', 1, timestamptz '2026-03-01T00:00:00Z')`,
			tenant)

		// Publication moves the pointer; history stays where it is.
		db.Exec(t, `
			UPDATE definition_active_pointer
			SET active_version = 2, pointer_version = pointer_version + 1, updated_at = now()
			WHERE tenant_id = $1 AND definition_kind = 'INTENT' AND definition_key = 'promotion'`,
			tenant)

		var active, pointerVersion int64
		if err := db.QueryRow(ctx, `
			SELECT active_version, pointer_version FROM definition_active_pointer
			WHERE tenant_id = $1 AND definition_kind = 'INTENT' AND definition_key = 'promotion'`,
			tenant).Scan(&active, &pointerVersion); err != nil {
			t.Fatalf("read active pointer: %v", err)
		}
		if active != 2 || pointerVersion != 2 {
			t.Fatalf("pointer is at version %d (pointer_version %d), want 2 and 2", active, pointerVersion)
		}

		var history int64
		if err := db.QueryRow(ctx, `
			SELECT count(*) FROM definition_version
			WHERE tenant_id = $1 AND definition_kind = 'INTENT' AND definition_key = 'promotion'`,
			tenant).Scan(&history); err != nil {
			t.Fatalf("count definition versions: %v", err)
		}
		if history != 2 {
			t.Fatalf("definition history holds %d versions, want 2", history)
		}
	})

	t.Run("definition kinds are constrained", func(t *testing.T) {
		if err := db.ExecErr(`
			INSERT INTO definition_version (
				tenant_id, definition_kind, definition_key, version, definition_digest,
				source_ref, body, published_by, published_at)
			VALUES ($1, 'VIBES', 'promotion', 1, $2, 'git://x', '\x01', 'publisher', now())`,
			tenant, fixtureDigestA); err == nil {
			t.Fatal("an undeclared definition kind was accepted")
		}
	})
}

// TestTodo_DB_007_Property drives every declared definition kind through the
// registry and confirms each keeps its own independent lineage.
func TestTodo_DB_007_Property(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)

	kinds := []string{
		"ENTITY", "PROPERTY", "RELATIONSHIP", "INTENT", "SCHEMA",
		"CAPABILITY", "WORKFLOW", "RULE", "CONFIG",
	}
	for _, kind := range kinds {
		for version := int64(1); version <= 3; version++ {
			var supersedes any
			if version > 1 {
				supersedes = version - 1
			}
			if err := db.ExecErr(`
				INSERT INTO definition_version (
					tenant_id, definition_kind, definition_key, version, definition_digest,
					source_ref, body, supersedes_version, published_by, published_at)
				VALUES ($1, $2, 'k', $3, $4, 'git://x', '\x01', $5, 'publisher', now())`,
				tenant, kind, version, fixtureDigestA, supersedes); err != nil {
				t.Fatalf("publish %s version %d: %v", kind, version, err)
			}
		}
	}

	var count int64
	if err := db.QueryRow(ctx,
		`SELECT count(*) FROM definition_version WHERE tenant_id = $1`, tenant).Scan(&count); err != nil {
		t.Fatalf("count definitions: %v", err)
	}
	if want := int64(len(kinds) * 3); count != want {
		t.Fatalf("registry holds %d versions, want %d", count, want)
	}
}

// TestTodo_DB_007_Golden proves lineage stays inside its tenant.
func TestTodo_DB_007_Golden(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	first := insertNamedTenant(t, db, "first")
	second := insertNamedTenant(t, db, "second")

	db.Exec(t, `
		INSERT INTO definition_version (
			tenant_id, definition_kind, definition_key, version, definition_digest,
			source_ref, body, published_by, published_at)
		VALUES ($1, 'RULE', 'shared-key', 1, $2, 'git://x', '\x01', 'publisher', now())`,
		first, fixtureDigestA)

	// The same key in another tenant is a separate lineage that cannot supersede
	// the first tenant's version.
	if err := db.ExecErr(`
		INSERT INTO definition_version (
			tenant_id, definition_kind, definition_key, version, definition_digest,
			source_ref, body, supersedes_version, published_by, published_at)
		VALUES ($1, 'RULE', 'shared-key', 2, $2, 'git://x', '\x01', 1, 'publisher', now())`,
		second, fixtureDigestB); err == nil {
		t.Fatal("a definition superseded another tenant's version")
	}

	if err := db.ExecErr(`
		INSERT INTO definition_version (
			tenant_id, definition_kind, definition_key, version, definition_digest,
			source_ref, body, published_by, published_at)
		VALUES ($1, 'RULE', 'shared-key', 1, $2, 'git://x', '\x01', 'publisher', now())`,
		second, fixtureDigestB); err != nil {
		t.Fatalf("the same key in another tenant was rejected: %v", err)
	}

}
