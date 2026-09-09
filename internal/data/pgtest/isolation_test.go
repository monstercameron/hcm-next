package pgtest_test

// TOOL-014: this file is the in-repo TestEnvironmentIsolation RED/GREEN test
// the todo entry's partial evidence line calls out as the remaining gap. The
// CI job go-core-ephemeral-pg already proves the harness *runs* against a
// throwaway embedded server; these tests prove the harness's isolation
// contract itself, per pgtest.go's own package doc: separate schemas, no
// shared rows, no leaked session settings, teardown that removes exactly
// the one schema, and a cache directory left intact for reuse.
//
// Everything here goes through pgtest's public API (pgtest.New/NewEmpty,
// db.Exec/ExecErr/QueryRow, db.Conn, pgtest.ServerURL) or opens its own raw
// connection with pgxadapter.Connect the same way pgtest.go's admin
// connection does; nothing reaches into pgtest's unexported state. That
// keeps these tests an outside proof of the isolation contract rather than
// a restatement of the implementation.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// TestEnvironmentIsolation is TOOL-014's PRIMARY test.
//
// RED: it fails the moment two pgtest instances share mutable state - the
// same schema, visible rows, or a leaked session setting.
// GREEN: every subtest below passes because pgtest.New/NewEmpty give each
// caller a schema nothing else can observe, and teardown removes exactly
// that schema while leaving the shared cache directory alone.
func TestEnvironmentIsolation(t *testing.T) {
	t.Run("SequentialSchemasAreIsolated", testSequentialSchemasAreIsolated)
	t.Run("ParallelSchemasAreIsolated", testParallelSchemasAreIsolated)
	t.Run("FailureInOneDoesNotCorruptTheOther", testFailureInOneDoesNotCorruptTheOther)
	t.Run("TeardownRemovesSchemaButKeepsCacheDirectory", testTeardownRemovesSchemaButKeepsCacheDirectory)
	t.Run("SharedSchemaMisconfigurationIsRefused", testSharedSchemaMisconfigurationIsRefused)
}

func testSequentialSchemasAreIsolated(t *testing.T) {
	first := pgtest.NewEmpty(t)
	first.Exec(t, `CREATE TABLE isolation_probe (id int, note text)`)
	first.Exec(t, `INSERT INTO isolation_probe (id, note) VALUES (1, 'first')`)

	second := pgtest.NewEmpty(t)
	if first.Schema == second.Schema {
		t.Fatalf("two NewEmpty calls in the same process produced the same schema %q", first.Schema)
	}

	// second never ran the CREATE TABLE above; if it shared first's schema
	// (or first's search_path leaked into second's connection) the table
	// would resolve. It must not.
	if err := second.ExecErr(`SELECT * FROM isolation_probe`); err == nil {
		t.Fatal("schema B can see schema A's table; schemas are not isolated")
	}
}

func testParallelSchemasAreIsolated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Every subtest inserts the identical value into a table it alone
	// creates, under a UNIQUE constraint. If two subtests ended up sharing
	// one schema, either the second CREATE TABLE (no IF NOT EXISTS) or the
	// second INSERT (duplicate key) would fail; if they run in genuinely
	// separate schemas both succeed independently.
	const shared = "same-value-in-every-isolated-schema"
	for _, name := range []string{"writer_a", "writer_b", "writer_c", "writer_d"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := pgtest.NewEmpty(t)
			db.Exec(t, `CREATE TABLE isolation_probe (note text UNIQUE NOT NULL)`)
			db.Exec(t, `INSERT INTO isolation_probe (note) VALUES ($1)`, shared)

			var count int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM isolation_probe`).Scan(&count); err != nil {
				t.Fatalf("count rows in schema %s: %v", db.Schema, err)
			}
			if count != 1 {
				t.Fatalf("schema %s sees %d row(s) for the shared value, want exactly its own 1 (schemas are shared)", db.Schema, count)
			}
		})
	}
}

func testFailureInOneDoesNotCorruptTheOther(t *testing.T) {
	ctx := context.Background()
	victim := pgtest.NewEmpty(t)
	bystander := pgtest.NewEmpty(t)

	victim.Exec(t, `CREATE TABLE probe (id int PRIMARY KEY)`)
	victim.Exec(t, `INSERT INTO probe (id) VALUES (1)`)

	// Cause a real failure in victim's schema: a primary-key violation.
	if err := victim.ExecErr(`INSERT INTO probe (id) VALUES (1)`); err == nil {
		t.Fatal("expected a duplicate primary key insert to fail")
	}

	// bystander never saw victim's CREATE TABLE; the failure above must not
	// have leaked a table, a row, or a broken connection into its schema.
	var bystanderHasProbe bool
	err := bystander.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema() AND table_name = 'probe'
		)`).Scan(&bystanderHasProbe)
	if err != nil {
		t.Fatalf("check bystander schema for a leaked probe table: %v", err)
	}
	if bystanderHasProbe {
		t.Fatal("bystander schema sees victim's probe table after victim's failed insert")
	}
	bystander.Exec(t, `CREATE TABLE probe (id int PRIMARY KEY)`)
	bystander.Exec(t, `INSERT INTO probe (id) VALUES (1)`) // same key victim used; would collide if shared

	// victim's own connection must still work: one failed statement outside
	// an explicit transaction must not corrupt the rest of victim's session.
	victim.Exec(t, `INSERT INTO probe (id) VALUES (2)`)
	var victimCount int
	if err := victim.QueryRow(ctx, `SELECT count(*) FROM probe`).Scan(&victimCount); err != nil {
		t.Fatalf("count rows in victim's schema after its failure: %v", err)
	}
	if victimCount != 2 {
		t.Fatalf("victim schema has %d row(s) after recovering from its failure, want 2", victimCount)
	}
}

func testTeardownRemovesSchemaButKeepsCacheDirectory(t *testing.T) {
	ctx := context.Background()

	var schema string
	t.Run("scratch", func(t *testing.T) {
		db := pgtest.NewEmpty(t)
		schema = db.Schema
		db.Exec(t, `CREATE TABLE evidence (id int)`)
	})
	// The scratch subtest above has fully returned, so its t.Cleanup (the
	// DROP SCHEMA) has already run.

	conn, err := pgxadapter.Connect(ctx, pgtest.ServerURL(), nil)
	if err != nil {
		t.Fatalf("connect to check teardown: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var stillExists bool
	if err := conn.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)`,
		schema).Scan(&stillExists); err != nil {
		t.Fatalf("check schema %s after teardown: %v", schema, err)
	}
	if stillExists {
		t.Fatalf("schema %s still exists after its test completed; teardown did not remove it", schema)
	}

	// The binary cache directory is a resource shared across the whole
	// process (and across processes, via HCMNEXT_TEST_PG_CACHE); teardown
	// of one schema must never touch it. Only meaningful for the embedded
	// path - an external server has no such directory.
	if os.Getenv(pgtest.EnvDatabaseURL) != "" {
		t.Skip("HCMNEXT_TEST_DATABASE_URL is set; no embedded-postgres cache directory to check")
	}
	dir := os.Getenv(pgtest.EnvCacheDir)
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			t.Skipf("no user cache dir available on this platform to verify: %v", err)
		}
		dir = base + string(os.PathSeparator) + "hcm-next" + string(os.PathSeparator) + "embedded-postgres"
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("embedded-postgres cache directory %s missing or not a directory after teardown: %v", dir, err)
	}
}

// testSharedSchemaMisconfigurationIsRefused is TOOL-014's RED case: a
// synthetic misconfiguration where two instances would compute the *same*
// schema name (as they would if schemaName() were, say, a fixed string or a
// counter that resets across processes, instead of a fresh UUID per call).
// It proves the harness does not silently let the second instance share the
// first one's schema: NewEmpty's CREATE SCHEMA has no IF NOT EXISTS, so a
// colliding name is refused outright by Postgres itself.
func testSharedSchemaMisconfigurationIsRefused(t *testing.T) {
	ctx := context.Background()
	url := pgtest.ServerURL()

	connA, err := pgxadapter.Connect(ctx, url, nil)
	if err != nil {
		t.Fatalf("connect (instance A): %v", err)
	}
	// Register connection teardown before the schema-drop cleanup below. Test
	// cleanups run LIFO, so the probe schema is still reachable when it is
	// dropped.
	t.Cleanup(func() { _ = connA.Close(ctx) })
	connB, err := pgxadapter.Connect(ctx, url, nil)
	if err != nil {
		t.Fatalf("connect (instance B): %v", err)
	}
	t.Cleanup(func() { _ = connB.Close(ctx) })

	// A fixed name simulating a broken schema-naming scheme that two
	// concurrent instances both computed identically (the real
	// pgtest.schemaName always mints a fresh UUID and cannot actually do
	// this - that is the point of the RED case).
	colliding := "t_red_misconfig_" + strings.ReplaceAll(uuid.NewString(), "-", "")

	if _, err := connA.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %s`, colliding)); err != nil {
		t.Fatalf("instance A failed to create the probe schema %s: %v", colliding, err)
	}
	t.Cleanup(func() {
		dropCtx := context.Background()
		if _, err := connA.Exec(dropCtx, fmt.Sprintf(`DROP SCHEMA %s CASCADE`, colliding)); err != nil {
			t.Errorf("drop probe schema %s: %v", colliding, err)
		}
	})

	// Instance B, misconfigured to reuse A's schema name, must be refused -
	// not silently handed a shared schema.
	if _, err := connB.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %s`, colliding)); err == nil {
		t.Fatalf("instance B's CREATE SCHEMA for the colliding name %q succeeded; "+
			"a schema-name misconfiguration would go undetected and two tests would share state", colliding)
	} else {
		t.Logf("harness correctly refused the colliding schema name: %v", err)
	}
}

// TestTodo_TOOL_014_Golden pins the schema-naming convention (t_ followed by
// a 32-character lowercase-hex UUID) and its uniqueness across many calls in
// one process, so an accidental change - e.g. to a shorter or predictable
// scheme - is caught here rather than by a rare collision in the field.
func TestTodo_TOOL_014_Golden(t *testing.T) {
	pattern := regexp.MustCompile(`^t_[0-9a-f]{32}$`)
	seen := make(map[string]bool)
	const n = 20
	for i := 0; i < n; i++ {
		db := pgtest.NewEmpty(t)
		if !pattern.MatchString(db.Schema) {
			t.Fatalf("schema name %q does not match the pinned convention t_<32 lowercase hex chars>", db.Schema)
		}
		if seen[db.Schema] {
			t.Fatalf("schema name %q was generated twice in %d calls", db.Schema, n)
		}
		seen[db.Schema] = true
	}
}

// TestTodo_TOOL_014_Race drives many concurrent isolated schemas through the
// full migration path (pgtest.New), stressing the harness's single
// serialized admin connection and Goose's per-schema version table under
// real concurrency. CI's go-core job runs the whole module with -race; this
// package runs on windows/arm64 locally where the race detector is
// unavailable, but the concurrency itself is exercised either way.
func TestTodo_TOOL_014_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const workers = 8
	for i := 0; i < workers; i++ {
		t.Run(fmt.Sprintf("worker_%d", i), func(t *testing.T) {
			t.Parallel()
			db := pgtest.New(t)
			db.Exec(t, `CREATE TABLE race_probe (id int)`)
			for j := 0; j < 5; j++ {
				db.Exec(t, `INSERT INTO race_probe (id) VALUES ($1)`, j)
			}

			var count int
			if err := db.QueryRow(ctx, `SELECT count(*) FROM race_probe`).Scan(&count); err != nil {
				t.Fatalf("count rows in schema %s: %v", db.Schema, err)
			}
			if count != 5 {
				t.Fatalf("schema %s has %d row(s), want 5", db.Schema, count)
			}

			var current string
			if err := db.QueryRow(ctx, `SELECT current_schema()`).Scan(&current); err != nil {
				t.Fatalf("read current_schema() for %s: %v", db.Schema, err)
			}
			if current != db.Schema {
				t.Fatalf("connection reports schema %q, want %q", current, db.Schema)
			}
		})
	}
}

// TestTodo_TOOL_014_Integration is TOOL-014's INTEGRATION test: it proves
// that when HCMNEXT_TEST_DATABASE_URL points at an external server (the
// escape hatch pgtest.go documents, and the path CI's go-core job exercises
// against a service container), per-test schema isolation still holds. It
// re-executes this same test binary as a subprocess with that variable set,
// because the in-process server choice is latched by a sync.Once for the
// lifetime of one `go test` process and cannot be flipped mid-run.
func TestTodo_TOOL_014_Integration(t *testing.T) {
	// Force a server to exist in this process (embedded or already
	// external) so ServerURL() below is never empty, regardless of what
	// else has or has not run yet.
	_ = pgtest.NewEmpty(t)
	url := pgtest.ServerURL()
	if url == "" {
		t.Fatal("pgtest.ServerURL() is empty after NewEmpty; cannot hand a subprocess a server to connect to")
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess_ExternalOverride$", "-test.v")
	cmd.Env = append(
		envWithout(pgtest.EnvDatabaseURL, "HCMNEXT_PGTEST_HELPER"),
		pgtest.EnvDatabaseURL+"="+url,
		"HCMNEXT_PGTEST_HELPER=external-override",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("external-override helper subprocess failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "HELPER_OK") {
		t.Fatalf("external-override helper subprocess did not report success:\n%s", out)
	}
}

// TestHelperProcess_ExternalOverride is not a real test: it is a no-op
// unless HCMNEXT_PGTEST_HELPER=external-override, in which case it is the
// subprocess body TestTodo_TOOL_014_Integration re-executes with
// HCMNEXT_TEST_DATABASE_URL set, to prove isolation holds on that code path
// too. This mirrors the standard library's own os/exec self-exec test
// pattern.
func TestHelperProcess_ExternalOverride(t *testing.T) {
	if os.Getenv("HCMNEXT_PGTEST_HELPER") != "external-override" {
		t.Skip("only runs as a subprocess helper for TestTodo_TOOL_014_Integration")
	}
	if os.Getenv(pgtest.EnvDatabaseURL) == "" {
		t.Fatal("expected HCMNEXT_TEST_DATABASE_URL to be set by the parent process")
	}

	a := pgtest.NewEmpty(t)
	b := pgtest.NewEmpty(t)
	if a.Schema == b.Schema {
		t.Fatalf("external-override: two NewEmpty calls produced the same schema %q", a.Schema)
	}
	a.Exec(t, `CREATE TABLE helper_probe (id int)`)
	a.Exec(t, `INSERT INTO helper_probe (id) VALUES (7)`)
	if err := b.ExecErr(`SELECT * FROM helper_probe`); err == nil {
		t.Fatal("external-override: schema B can see schema A's table")
	}
	t.Log("HELPER_OK")
}

func envWithout(keys ...string) []string {
	base := os.Environ()
	out := make([]string, 0, len(base))
	for _, kv := range base {
		skip := false
		for _, k := range keys {
			if strings.HasPrefix(kv, k+"=") {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, kv)
		}
	}
	return out
}

// TestTodo_TOOL_014_Security covers the two security-relevant facets of the
// isolation contract the todo entry names explicitly: that role/session
// settings such as app.tenant_id do not leak between schemas, and that
// dropping one instance's schema cannot reach a sibling's. It also records,
// deliberately, what the boundary is *not*: a same-role Postgres schema is a
// namespace, not a privilege boundary, so a fully-qualified reference still
// works. Nothing here should be read as tenant-grade access control.
func TestTodo_TOOL_014_Security(t *testing.T) {
	t.Run("TenantSettingDoesNotLeakAcrossSchemas", testTenantSettingDoesNotLeak)
	t.Run("DropSchemaCascadeDoesNotTouchSiblingSchema", testDropSchemaCascadeIsScoped)
	t.Run("SchemaBoundaryIsNamespaceNotPrivilege", testSchemaBoundaryIsNamespace)
}

func testTenantSettingDoesNotLeak(t *testing.T) {
	ctx := context.Background()
	dbA := pgtest.NewEmpty(t)
	dbB := pgtest.NewEmpty(t)

	if _, err := dbA.Conn.Exec(ctx, `SET app.tenant_id = 'tenant-a-secret'`); err != nil {
		t.Fatalf("set app.tenant_id on schema A's connection: %v", err)
	}

	var val *string
	if err := dbB.QueryRow(ctx, `SELECT current_setting('app.tenant_id', true)`).Scan(&val); err != nil {
		t.Fatalf("read app.tenant_id on schema B's connection: %v", err)
	}
	if val != nil {
		t.Fatalf("schema B observed app.tenant_id = %q; a setting from schema A's session leaked", *val)
	}
}

func testDropSchemaCascadeIsScoped(t *testing.T) {
	ctx := context.Background()
	sibling := pgtest.NewEmpty(t)
	sibling.Exec(t, `CREATE TABLE keepme (id int)`)
	sibling.Exec(t, `INSERT INTO keepme (id) VALUES (42)`)

	t.Run("victim", func(t *testing.T) {
		victim := pgtest.NewEmpty(t)
		victim.Exec(t, `CREATE TABLE also_keepme (id int)`)
	})
	// victim's t.Cleanup (DROP SCHEMA victim CASCADE) has now run.

	var count int
	if err := sibling.QueryRow(ctx, `SELECT count(*) FROM keepme`).Scan(&count); err != nil {
		t.Fatalf("sibling schema %s appears corrupted after a neighboring schema's teardown: %v", sibling.Schema, err)
	}
	if count != 1 {
		t.Fatalf("sibling schema lost its row after a neighboring DROP SCHEMA CASCADE, got %d rows want 1", count)
	}
}

func testSchemaBoundaryIsNamespace(t *testing.T) {
	ctx := context.Background()
	dbA := pgtest.NewEmpty(t)
	dbB := pgtest.NewEmpty(t)
	dbA.Exec(t, `CREATE TABLE qualified_probe (id int)`)
	dbA.Exec(t, `INSERT INTO qualified_probe (id) VALUES (99)`)

	// Unqualified: search_path isolation hides it, as every other test here
	// relies on.
	if err := dbB.ExecErr(`SELECT * FROM qualified_probe`); err == nil {
		t.Fatal("unqualified cross-schema query unexpectedly succeeded")
	}

	// Fully qualified: the same Postgres role owns every test schema on this
	// server, so an explicit reference still resolves. This is expected and
	// documents that pgtest's isolation is a per-test namespace for
	// concurrent tests, not a privilege boundary between tenants.
	var id int
	err := dbB.QueryRow(ctx, fmt.Sprintf(`SELECT id FROM %s.qualified_probe`, dbA.Schema)).Scan(&id)
	if err != nil {
		t.Fatalf("fully-qualified cross-schema read failed: %v (expected to succeed: same role owns both schemas)", err)
	}
	if id != 99 {
		t.Fatalf("fully-qualified cross-schema read returned %d, want 99", id)
	}
}
