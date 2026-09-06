package deferredschema

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

// TestTodo_DB_016_Integration proves every generated migration preview is
// valid DDL against the real house schema: pgtest.New gives an isolated,
// fully migrated throwaway schema (tenant table, the tenant_ref/semantic_key
// domains and the forbid_mutation() function every preview references all
// exist), the whole preview set is applied inside one explicit transaction,
// and that transaction is rolled back rather than committed -- so nothing
// this test creates outlives the test, and pgtest's own t.Cleanup drops the
// whole schema regardless.
func TestTodo_DB_016_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()

	set, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	rolledBack := false
	defer func() {
		if !rolledBack {
			_ = tx.Rollback(ctx)
		}
	}()

	for _, d := range Domains() {
		content, ok := set.Lookup(d.PreviewFileName())
		if !ok {
			t.Fatalf("preview set has no file for domain %s", d.Slug)
		}
		up := UpSQL(content)
		for _, stmt := range Statements(up) {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				t.Fatalf("domain %s: applying statement failed: %v\nstatement:\n%s", d.Slug, err, stmt)
			}
		}
	}

	// Inside the transaction, every table this run created must now be
	// visible.
	for _, row := range DispositionRows(Domains()) {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", row.Table).Scan(&exists); err != nil {
			t.Fatalf("checking %s exists mid-transaction: %v", row.Table, err)
		}
		if !exists {
			t.Fatalf("table %s was not created by its preview's DDL", row.Table)
		}
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	rolledBack = true

	// After rollback, on a fresh connection against the same schema, none of
	// the preview tables exist: the previews are proven-valid DDL, not
	// production authority.
	for _, row := range DispositionRows(Domains()) {
		var exists bool
		if err := db.Conn.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", row.Table).Scan(&exists); err != nil {
			t.Fatalf("checking %s absent after rollback: %v", row.Table, err)
		}
		if exists {
			t.Fatalf("table %s survived rollback; preview leaked production authority", row.Table)
		}
	}
}
