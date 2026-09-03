package schema_test

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

// platformControlTables are the only base tables that are not tenant scoped.
// They describe the schema itself, not any tenant's business truth.
var platformControlTables = []string{"migration_journal", "schema_release"}

// tenantScopedTables must every one of them carry tenant_id NOT NULL.
var tenantScopedTables = []string{
	"authority_assignment",
	"definition_active_pointer",
	"definition_version",
	"intent_instance",
	"ledger_event",
	"ledger_stream",
	"outbox",
	"payload_schema",
	"projection_checkpoint",
	"proposal_revision",
	"stream_head",
	"tenant",
}

// appendOnlyTables reject UPDATE and DELETE outright.
var appendOnlyTables = []string{"definition_version", "ledger_event", "proposal_revision"}

// TestTodo_DATA_001 proves the authoritative storage boundaries: every
// correctness-bearing table declares its tenant, no foreign key crosses a tenant
// boundary or leaves the schema, ledger payloads are immutable, and the data
// plane performs no external call inside a transaction.
func TestTodo_DATA_001(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	t.Run("every authoritative table is classified", func(t *testing.T) {
		got := baseTables(t, db)
		want := append(slices.Clone(platformControlTables), tenantScopedTables...)
		sort.Strings(want)
		if !slices.Equal(got, want) {
			t.Fatalf("schema holds tables %v, want %v", got, want)
		}
	})

	t.Run("tenant scoped tables declare their tenant", func(t *testing.T) {
		for _, table := range tenantScopedTables {
			var nullable string
			err := db.QueryRow(ctx, `
				SELECT is_nullable FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = $2 AND column_name = 'tenant_id'`,
				db.Schema, table).Scan(&nullable)
			if err != nil {
				t.Fatalf("%s has no tenant_id column: %v", table, err)
			}
			if nullable != "NO" {
				t.Fatalf("%s.tenant_id is nullable; an authoritative row must never be tenantless", table)
			}
		}
	})

	t.Run("primary keys are tenant scoped", func(t *testing.T) {
		for _, table := range tenantScopedTables {
			cols := primaryKeyColumns(t, db, table)
			if len(cols) == 0 {
				t.Fatalf("%s has no primary key", table)
			}
			if !slices.Contains(cols, "tenant_id") {
				t.Fatalf("%s primary key %v does not include tenant_id", table, cols)
			}
		}
	})

	t.Run("no foreign key crosses a tenant or schema boundary", func(t *testing.T) {
		for _, fk := range foreignKeys(t, db) {
			if fk.parentSchema != db.Schema {
				t.Fatalf("foreign key %s on %s points outside the schema, at %s.%s",
					fk.name, fk.child, fk.parentSchema, fk.parent)
			}
			if !slices.Contains(tenantScopedTables, fk.child) {
				continue
			}
			if !slices.Contains(fk.childColumns, "tenant_id") {
				t.Fatalf("foreign key %s from %s to %s uses columns %v; a tenant-scoped reference must carry tenant_id",
					fk.name, fk.child, fk.parent, fk.childColumns)
			}
		}
	})

	t.Run("ledger and proposal rows are immutable", func(t *testing.T) {
		fixture := newLedgerFixture(t, db)
		fixture.appendEvent(t, 1, "TRANSACTION_FACT")
		// Row triggers can only refuse rows that exist, so every append-only
		// table must hold one before the refusal means anything.
		seedAppendOnly(t, db, fixture.tenant)

		for _, table := range appendOnlyTables {
			var rows int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&rows); err != nil {
				t.Fatalf("count %s: %v", table, err)
			}
			if rows == 0 {
				t.Fatalf("%s is empty; the immutability check would pass vacuously", table)
			}
			if err := db.ExecErr(`UPDATE ` + table + ` SET recorded_at = now()`); err == nil {
				t.Fatalf("UPDATE on %s succeeded; the table must be append-only", table)
			}
			if err := db.ExecErr(`DELETE FROM ` + table); err == nil {
				t.Fatalf("DELETE on %s succeeded; the table must be append-only", table)
			}
		}
	})

	t.Run("the data plane makes no external call", func(t *testing.T) {
		// An external effect inside a domain transaction is the failure this
		// boundary exists to prevent, so the packages that own authoritative
		// writes must not be able to reach the network at all.
		for _, pkg := range []string{"ledger", "schema"} {
			for _, imported := range packageImports(t, pkg) {
				switch imported {
				case "net", "net/http", "net/url", "os/exec":
					t.Fatalf("internal/data/%s imports %q; authoritative writers never perform external calls", pkg, imported)
				}
			}
		}
	})
}

// TestTodo_DATA_001_Security proves a tenantless or foreign-tenant authoritative
// row cannot be written at all.
func TestTodo_DATA_001_Security(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)

	if err := db.ExecErr(`
		INSERT INTO ledger_stream (tenant_id, stream_key, stream_kind, subject_ref)
		VALUES (NULL, 'worker-1', 'WORKER', 'worker:1')`); err == nil {
		t.Fatal("a tenantless ledger_stream row was accepted")
	}

	unknown := uuid.New()
	if unknown == tenant {
		t.Fatal("fixture generated a colliding tenant identifier")
	}
	if err := db.ExecErr(`
		INSERT INTO ledger_stream (tenant_id, stream_key, stream_kind, subject_ref)
		VALUES ($1, 'worker-1', 'WORKER', 'worker:1')`, unknown); err == nil {
		t.Fatal("a ledger_stream row for an unregistered tenant was accepted")
	}

	// A child row may not borrow a parent from another tenant.
	db.Exec(t, `
		INSERT INTO ledger_stream (tenant_id, stream_key, stream_kind, subject_ref)
		VALUES ($1, 'worker-1', 'WORKER', 'worker:1')`, tenant)
	other := insertNamedTenant(t, db, "other-tenant")
	if err := db.ExecErr(`
		INSERT INTO stream_head (tenant_id, stream_key, head_sequence)
		VALUES ($1, 'worker-1', 0)`, other); err == nil {
		t.Fatal("a stream_head row pointing at another tenant's stream was accepted")
	}
}

// TestTodo_DATA_001_Golden pins the authoritative table inventory.
func TestTodo_DATA_001_Golden(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)

	got := baseTables(t, db)
	want := []string{
		"authority_assignment",
		"definition_active_pointer",
		"definition_version",
		"intent_instance",
		"ledger_event",
		"ledger_stream",
		"migration_journal",
		"outbox",
		"payload_schema",
		"projection_checkpoint",
		"proposal_revision",
		"schema_release",
		"stream_head",
		"tenant",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("authoritative tables are %v, want %v", got, want)
	}
}

// baseTables returns the schema's own base tables, ordered, excluding Goose's
// version table and the physical partitions of a partitioned table.
func baseTables(t *testing.T, db *pgtest.DB) []string {
	t.Helper()
	rows, err := db.Conn.Query(context.Background(), `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema()
		  AND c.relkind IN ('r', 'p')
		  AND NOT c.relispartition
		  AND c.relname <> 'goose_db_version'
		ORDER BY c.relname`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list tables: %v", err)
	}
	return names
}

func primaryKeyColumns(t *testing.T, db *pgtest.DB, table string) []string {
	t.Helper()
	var cols []string
	err := db.QueryRow(context.Background(), `
		SELECT array_agg(att.attname ORDER BY x.ord)
		FROM pg_constraint con
		JOIN pg_class rel ON rel.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = rel.relnamespace
		CROSS JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS x(attnum, ord)
		JOIN pg_attribute att ON att.attrelid = con.conrelid AND att.attnum = x.attnum
		WHERE con.contype = 'p' AND n.nspname = current_schema() AND rel.relname = $1`,
		table).Scan(&cols)
	if err != nil {
		t.Fatalf("read primary key of %s: %v", table, err)
	}
	return cols
}

type foreignKey struct {
	name         string
	child        string
	parent       string
	parentSchema string
	childColumns []string
}

func foreignKeys(t *testing.T, db *pgtest.DB) []foreignKey {
	t.Helper()
	rows, err := db.Conn.Query(context.Background(), `
		SELECT con.conname,
		       rel.relname,
		       frel.relname,
		       fns.nspname,
		       (SELECT array_agg(att.attname ORDER BY x.ord)
		          FROM unnest(con.conkey) WITH ORDINALITY AS x(attnum, ord)
		          JOIN pg_attribute att
		            ON att.attrelid = con.conrelid AND att.attnum = x.attnum)
		FROM pg_constraint con
		JOIN pg_class rel ON rel.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = rel.relnamespace
		JOIN pg_class frel ON frel.oid = con.confrelid
		JOIN pg_namespace fns ON fns.oid = frel.relnamespace
		WHERE con.contype = 'f' AND n.nspname = current_schema()
		ORDER BY con.conname`)
	if err != nil {
		t.Fatalf("list foreign keys: %v", err)
	}
	defer rows.Close()

	var keys []foreignKey
	for rows.Next() {
		var fk foreignKey
		if err := rows.Scan(&fk.name, &fk.child, &fk.parent, &fk.parentSchema, &fk.childColumns); err != nil {
			t.Fatalf("scan foreign key: %v", err)
		}
		keys = append(keys, fk)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list foreign keys: %v", err)
	}
	if len(keys) == 0 {
		t.Fatal("schema declares no foreign keys at all")
	}
	return keys
}

// packageImports returns every import path of a sibling data-plane package.
func packageImports(t *testing.T, pkg string) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the test source")
	}
	dir := filepath.Join(filepath.Dir(filepath.Dir(thisFile)), pkg)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	var imports []string
	var parsed int
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed++
		for _, spec := range file.Imports {
			imports = append(imports, strings.Trim(spec.Path.Value, `"`))
		}
	}
	if parsed == 0 {
		t.Fatalf("no non-test Go files under %s", dir)
	}
	return imports
}
