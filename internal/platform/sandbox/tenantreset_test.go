package sandbox

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
)

// TestDependencyOrderPlacesChildrenBeforeParents is a pure unit test (no
// database) of the topological sort [DeleteTenantRows] relies on: for every
// foreign key child->parent, child must appear before parent in the order.
func TestDependencyOrderPlacesChildrenBeforeParents(t *testing.T) {
	tables := []string{"c", "a", "b"}
	edges := []fkEdge{{child: "a", parent: "b"}, {child: "b", parent: "c"}}

	order, err := dependencyOrder(tables, edges)
	if err != nil {
		t.Fatalf("dependencyOrder: %v", err)
	}
	pos := map[string]int{}
	for i, name := range order {
		pos[name] = i
	}
	if pos["a"] >= pos["b"] {
		t.Fatalf("order %v places child a after parent b", order)
	}
	if pos["b"] >= pos["c"] {
		t.Fatalf("order %v places child b after parent c", order)
	}
	if len(order) != len(tables) {
		t.Fatalf("order has %d entries, want %d", len(order), len(tables))
	}
}

// TestDependencyOrderIsDeterministic proves two independent calls over the
// same unordered input produce byte-identical output: a Reset run twice
// deletes in the same order both times.
func TestDependencyOrderIsDeterministic(t *testing.T) {
	tables := []string{"zeta", "alpha", "mid", "leaf"}
	edges := []fkEdge{{child: "leaf", parent: "mid"}, {child: "mid", parent: "alpha"}}

	first, err := dependencyOrder(tables, edges)
	if err != nil {
		t.Fatalf("dependencyOrder: %v", err)
	}
	second, err := dependencyOrder(append([]string(nil), tables...), append([]fkEdge(nil), edges...))
	if err != nil {
		t.Fatalf("dependencyOrder: %v", err)
	}
	if strings.Join(first, ",") != strings.Join(second, ",") {
		t.Fatalf("two calls disagreed: %v vs %v", first, second)
	}
}

// TestDependencyOrderDiamondIsSafe proves a table referenced by two other
// tenant-scoped tables (a diamond, not a simple chain) is still placed after
// both of its children.
func TestDependencyOrderDiamondIsSafe(t *testing.T) {
	tables := []string{"root", "left", "right", "leaf"}
	edges := []fkEdge{
		{child: "leaf", parent: "left"},
		{child: "leaf", parent: "right"},
		{child: "left", parent: "root"},
		{child: "right", parent: "root"},
	}
	order, err := dependencyOrder(tables, edges)
	if err != nil {
		t.Fatalf("dependencyOrder: %v", err)
	}
	pos := map[string]int{}
	for i, name := range order {
		pos[name] = i
	}
	for _, e := range edges {
		if pos[e.child] >= pos[e.parent] {
			t.Fatalf("order %v places child %s after parent %s", order, e.child, e.parent)
		}
	}
}

// TestDependencyOrderRejectsACycle proves a foreign-key cycle among
// tenant-scoped tables is refused rather than silently mis-ordered.
func TestDependencyOrderRejectsACycle(t *testing.T) {
	tables := []string{"a", "b"}
	edges := []fkEdge{{child: "a", parent: "b"}, {child: "b", parent: "a"}}
	if _, err := dependencyOrder(tables, edges); err == nil {
		t.Fatal("dependencyOrder admitted a cycle")
	}
}

// TestDependencyGroupsBundlesAGenuineCycle proves that where dependencyOrder
// refuses outright, dependencyGroups instead reports every acyclic table
// resolved individually and the cyclic remainder as one group naming exactly
// its members - covering every table, dropping none.
func TestDependencyGroupsBundlesAGenuineCycle(t *testing.T) {
	tables := []string{"leaf", "a", "b"}
	edges := []fkEdge{
		{child: "leaf", parent: "a"},
		{child: "a", parent: "b"},
		{child: "b", parent: "a"},
	}
	groups := dependencyGroups(tables, edges)

	seen := map[string]int{}
	for _, g := range groups {
		for _, t := range g {
			seen[t]++
		}
	}
	for _, table := range tables {
		if seen[table] != 1 {
			t.Fatalf("table %s appears %d times, want 1", table, seen[table])
		}
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %v, want the acyclic leaf and one cyclic bundle of {a,b}", groups)
	}
	if len(groups[0]) != 1 || groups[0][0] != "leaf" {
		t.Fatalf("first group = %v, want [leaf] (the only acyclic table)", groups[0])
	}
	last := groups[len(groups)-1]
	if len(last) != 2 {
		t.Fatalf("cyclic group = %v, want exactly {a,b}", last)
	}
}

// TestUnqualifyStripsSchemaAndQuoting covers the defensive parsing of
// regclass::text output.
func TestUnqualifyStripsSchemaAndQuoting(t *testing.T) {
	cases := map[string]string{
		"plain_table":         "plain_table",
		"schema1.table_two":   "table_two",
		`"Weird Name"`:        "Weird Name",
		`schema1."Weird One"`: "Weird One",
	}
	for in, want := range cases {
		if got := unqualify(in); got != want {
			t.Errorf("unqualify(%q) = %q, want %q", in, got, want)
		}
	}
}

// newResetPool opens a fresh pgtest schema (every real migration applied)
// bound through pgxadapter, the same connection shape [Sandbox] runs on.
func newResetPool(t *testing.T) *pgxadapter.Pool {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestTenantScopedTablesAgainstRealSchemaResolveToACompleteDeletionPlan is a
// regression guard for the real, migrated schema: every table PostgreSQL's
// catalog says carries a tenant_id column must appear exactly once across
// [dependencyGroups]' output, whether or not the schema's own foreign keys
// happen to be a strict DAG at the table level. A schema with a genuine
// cross-table foreign-key cycle is real (bitemporal, aggregate-supertype
// schemas commonly have one) and is not itself a defect; what this guards
// against is a table [dependencyGroups] silently drops or double-counts,
// which would be.
func TestTenantScopedTablesAgainstRealSchemaResolveToACompleteDeletionPlan(t *testing.T) {
	pool := newResetPool(t)
	ctx := context.Background()

	tables, err := tenantScopedTables(ctx, pool)
	if err != nil {
		t.Fatalf("tenantScopedTables: %v", err)
	}
	if len(tables) == 0 {
		t.Fatal("the migrated schema names no tenant-scoped table at all")
	}
	edges, err := tenantForeignKeys(ctx, pool, tables)
	if err != nil {
		t.Fatalf("tenantForeignKeys: %v", err)
	}
	groups := dependencyGroups(tables, edges)

	seen := map[string]int{}
	for _, group := range groups {
		for _, table := range group {
			seen[table]++
		}
	}
	for _, table := range tables {
		if seen[table] != 1 {
			t.Errorf("table %s appears %d times across dependencyGroups' output, want exactly 1", table, seen[table])
		}
	}
	if len(seen) != len(tables) {
		t.Fatalf("dependencyGroups named %d distinct tables, want %d", len(seen), len(tables))
	}

	for _, group := range groups {
		if len(group) > 1 {
			t.Logf("cyclic deletion group of %d tables (handled by deleteCyclicGroup's retry loop): %v", len(group), group)
		}
	}
}

// TestDeleteTenantRowsOnlyTouchesItsOwnTenant is the generic correctness
// proof behind SANDBOX-001's Reset guarantee, exercised against two purpose-
// built tables carrying a real foreign key between them (a shape the
// production schema also has): deleting tenant A's rows removes exactly
// tenant A's rows, in child-before-parent order, and never touches tenant B's
// rows in the same tables.
func TestDeleteTenantRowsOnlyTouchesItsOwnTenant(t *testing.T) {
	pool := newResetPool(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		CREATE TABLE reset_probe_parent (
			tenant_id uuid NOT NULL,
			id        uuid PRIMARY KEY
		)`); err != nil {
		t.Fatalf("create reset_probe_parent: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE TABLE reset_probe_child (
			tenant_id uuid NOT NULL,
			id        uuid PRIMARY KEY,
			parent_id uuid NOT NULL REFERENCES reset_probe_parent(id)
		)`); err != nil {
		t.Fatalf("create reset_probe_child: %v", err)
	}

	tenantA, tenantB := uuid.New(), uuid.New()
	seedProbeRows(t, pool, tenantA)
	seedProbeRows(t, pool, tenantB)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	deleted, _, err := DeleteTenantRows(ctx, tx, tenantA)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatalf("DeleteTenantRows: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if deleted["reset_probe_parent"] != 1 || deleted["reset_probe_child"] != 1 {
		t.Fatalf("deleted counts = %+v, want 1 row from each probe table", deleted)
	}

	remainingA := countProbeRows(t, pool, tenantA)
	if remainingA != 0 {
		t.Fatalf("tenant A still has %d probe rows after its own reset", remainingA)
	}
	remainingB := countProbeRows(t, pool, tenantB)
	if remainingB != 2 {
		t.Fatalf("tenant B's probe rows changed: has %d, want 2 (untouched)", remainingB)
	}
}

// TestDeleteTenantRowsResolvesATableLevelCycle proves [DeleteTenantRows]
// clears a tenant's rows even when the two tables' own foreign keys form a
// cycle at the table level - reset_probe_x references reset_probe_y and
// reset_probe_y references reset_probe_x, the same "optional back-reference"
// shape a bitemporal/aggregate-supertype schema produces - as long as no two
// specific rows require each other's continued existence. That is exactly
// [deleteCyclicGroup]'s retry loop earning its keep: a naive single-pass
// per-table DELETE would fail here no matter which table it tried first.
func TestDeleteTenantRowsResolvesATableLevelCycle(t *testing.T) {
	pool := newResetPool(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		CREATE TABLE reset_probe_x (
			tenant_id uuid NOT NULL,
			id        uuid PRIMARY KEY,
			y_id      uuid NULL
		)`); err != nil {
		t.Fatalf("create reset_probe_x: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE TABLE reset_probe_y (
			tenant_id uuid NOT NULL,
			id        uuid PRIMARY KEY,
			x_id      uuid NULL REFERENCES reset_probe_x(id)
		)`); err != nil {
		t.Fatalf("create reset_probe_y: %v", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE reset_probe_x ADD CONSTRAINT reset_probe_x_y_fk FOREIGN KEY (y_id) REFERENCES reset_probe_y(id)`); err != nil {
		t.Fatalf("add reset_probe_x -> reset_probe_y foreign key: %v", err)
	}

	tenant := uuid.New()
	xID, yID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO reset_probe_x (tenant_id, id, y_id) VALUES ($1, $2, NULL)`, tenant, xID); err != nil {
		t.Fatalf("insert x: %v", err)
	}
	// y references x (this direction is real); x's own reference to y is left
	// null, so no single pair of rows requires the other to still exist -
	// the cycle is between the two tables, not between these two rows.
	if _, err := pool.Exec(ctx, `INSERT INTO reset_probe_y (tenant_id, id, x_id) VALUES ($1, $2, $3)`, tenant, yID, xID); err != nil {
		t.Fatalf("insert y: %v", err)
	}

	tables, err := tenantScopedTables(ctx, pool)
	if err != nil {
		t.Fatalf("tenantScopedTables: %v", err)
	}
	edges, err := tenantForeignKeys(ctx, pool, tables)
	if err != nil {
		t.Fatalf("tenantForeignKeys: %v", err)
	}
	foundCycle := false
	for _, group := range dependencyGroups(tables, edges) {
		if len(group) > 1 {
			foundCycle = true
		}
	}
	if !foundCycle {
		t.Fatal("the probe tables' mutual foreign keys did not surface as a cyclic group; the test no longer exercises deleteCyclicGroup")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	deleted, _, err := DeleteTenantRows(ctx, tx, tenant)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatalf("DeleteTenantRows over a table-level cycle: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if deleted["reset_probe_x"] != 1 || deleted["reset_probe_y"] != 1 {
		t.Fatalf("deleted counts = %+v, want 1 row from each probe table", deleted)
	}
	var remaining int64
	if err := pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM reset_probe_x WHERE tenant_id = $1) + (SELECT count(*) FROM reset_probe_y WHERE tenant_id = $1)`,
		tenant).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("remaining probe rows = %d, want 0", remaining)
	}
}

func seedProbeRows(t *testing.T, pool *pgxadapter.Pool, tenant uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	parentID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO reset_probe_parent (tenant_id, id) VALUES ($1, $2)`, tenant, parentID); err != nil {
		t.Fatalf("seed parent for %s: %v", tenant, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO reset_probe_child (tenant_id, id, parent_id) VALUES ($1, $2, $3)`,
		tenant, uuid.New(), parentID); err != nil {
		t.Fatalf("seed child for %s: %v", tenant, err)
	}
}

func countProbeRows(t *testing.T, pool *pgxadapter.Pool, tenant uuid.UUID) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(context.Background(),
		`SELECT (SELECT count(*) FROM reset_probe_parent WHERE tenant_id = $1) + (SELECT count(*) FROM reset_probe_child WHERE tenant_id = $1)`,
		tenant).Scan(&n); err != nil {
		t.Fatalf("count probe rows for %s: %v", tenant, err)
	}
	return n
}
