package seed_test

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/seed"
)

// TestTodo_DB_019_Integration proves Seed's actual database effects, not just
// its returned Summary: every registration lands as a real definition_version
// row at version 1 with the exact kind/key/digest/source_ref/published_by the
// plan declares, and the active-pointer table (which Seed never touches) is
// correctly left empty -- publishing a definition and activating it are
// deliberately separate steps.
func TestTodo_DB_019_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "seed-tenant")

	plan, err := seed.Plan()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	runSeed(t, ctx, db, tenantID)

	for _, r := range plan {
		r := r
		t.Run(r.Kind+"/"+r.Key, func(t *testing.T) {
			var version int64
			var digest, sourceRef, publishedBy string
			var body []byte
			err := db.Conn.QueryRow(ctx, `
				SELECT version, definition_digest, source_ref, published_by, body
				FROM definition_version
				WHERE tenant_id = $1 AND definition_kind = $2 AND definition_key = $3`,
				tenantID, r.Kind, r.Key).Scan(&version, &digest, &sourceRef, &publishedBy, &body)
			if err != nil {
				t.Fatalf("read back %s/%s: %v", r.Kind, r.Key, err)
			}
			if version != 1 {
				t.Fatalf("%s/%s landed at version %d, want 1", r.Kind, r.Key, version)
			}
			if sourceRef != r.SourceRef {
				t.Fatalf("%s/%s source_ref is %q, want %q", r.Kind, r.Key, sourceRef, r.SourceRef)
			}
			if publishedBy != seed.PublishedBy {
				t.Fatalf("%s/%s published_by is %q, want %q", r.Kind, r.Key, publishedBy, seed.PublishedBy)
			}
			if string(body) != string(r.Body) {
				t.Fatalf("%s/%s body does not match the plan's body", r.Kind, r.Key)
			}
			if len(digest) != 64 {
				t.Fatalf("%s/%s digest %q is not a 64-character sha256 hex digest", r.Kind, r.Key, digest)
			}
		})
	}

	t.Run("no active pointer is created", func(t *testing.T) {
		var count int
		if err := db.Conn.QueryRow(ctx,
			`SELECT count(*) FROM definition_active_pointer WHERE tenant_id = $1`, tenantID).Scan(&count); err != nil {
			t.Fatalf("count definition_active_pointer: %v", err)
		}
		if count != 0 {
			t.Fatalf("Seed left %d active pointer rows; publishing and activating are separate steps", count)
		}
	})

	t.Run("content drift under the same key is refused, not silently reported as success", func(t *testing.T) {
		// A different writer publishing version 1 of a key Seed also plans to
		// use, with different content, is exactly the RED case Seed's own
		// verify-on-conflict step exists to catch: ON CONFLICT DO NOTHING
		// alone would silently report success while leaving the foreign
		// content in place.
		other := insertTenant(t, db, "seed-tenant-drift")
		db.Exec(t, `
			INSERT INTO definition_version (
				tenant_id, definition_kind, definition_key, version, definition_digest,
				source_ref, body, published_by, published_at)
			VALUES ($1, 'SCHEMA', 'hcmnext.fixtures.corpus', 1, $2, 'someone-else', $3, 'someone-else', now())`,
			other, strings.Repeat("0", 64), []byte("not the plan's body"))

		tx, err := db.Conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := seed.Seed(ctx, tx, other); err == nil {
			t.Fatal("Seed reported success over a key that already held different content")
		}
	})
}
