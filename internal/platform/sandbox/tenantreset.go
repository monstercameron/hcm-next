package sandbox

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// tenantUUID is this package's own name for a PostgreSQL uuid value, spelled
// as its underlying representation rather than as github.com/google/uuid.UUID.
// definitions/architecture/dependency-roles.yaml confines that import to
// internal/kernel, internal/intent, internal/ledger, internal/data,
// internal/connectivity, internal/transaction, internal/humanwork,
// internal/workflow, internal/operations/explorer,
// internal/engines/wire/digest, cmd and test - internal/platform is not
// among them, the same reason internal/platform/execution's own comments
// give for never importing it directly. Go's assignability rule (identical
// underlying type, at least one side unnamed) makes this interchangeable
// with uuid.UUID at every call boundary without a cast: a caller holding a
// uuid.UUID passes it in directly, and this package hands values of this
// type to internal/data/tenancy.WithTenant and internal/data/seed.Seed the
// same way.
type tenantUUID = [16]byte

// tenantScopedTables returns every base table in the connection's current
// schema that carries a tenant_id column, ordered by name. It reads
// PostgreSQL's own catalog rather than a hand-maintained list, because a list
// this package kept itself would silently fall behind every other lane that
// adds a tenant-scoped table.
func tenantScopedTables(ctx context.Context, q dbport.Querier) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT c.table_name
		FROM information_schema.columns c
		JOIN information_schema.tables t
			ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = current_schema()
			AND c.column_name = 'tenant_id'
			AND t.table_type = 'BASE TABLE'
		ORDER BY c.table_name`)
	if err != nil {
		return nil, fmt.Errorf("sandbox: discover tenant-scoped tables: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("sandbox: scan tenant-scoped table name: %w", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sandbox: list tenant-scoped tables: %w", err)
	}
	return out, nil
}

// appendOnlyTables returns the set of tenant-scoped tables PostgreSQL's own
// catalog says carry the append-only guarantee: a row-level trigger that
// calls forbid_mutation(), the one function every append-only table
// migration 00001 onward wires an UPDATE/DELETE trigger to (DB-*'s own
// "RLS tenant_isolation, forbid_mutation trigger where append-only" pattern).
//
// [DeleteTenantRows] never attempts a DELETE against one of these: an
// evidence, ledger or registry table's immutability is a property of the
// schema every tenant gets, sandbox or not, and a reset that bypassed it
// would be fencing external effects while quietly breaking the one guarantee
// this platform makes about its own audit trail. Destroying that trail for a
// tenant that has genuinely expired is a different, governed operation
// (retention_disposition's own destruction-receipt flow), not a side effect
// of an ordinary Reset.
func appendOnlyTables(ctx context.Context, q dbport.Querier) (map[string]bool, error) {
	rows, err := q.Query(ctx, `
		SELECT DISTINCT c.relname
		FROM pg_trigger trg
		JOIN pg_proc p ON p.oid = trg.tgfoid
		JOIN pg_class c ON c.oid = trg.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE p.proname = 'forbid_mutation'
			AND n.nspname = current_schema()
			AND NOT trg.tgisinternal`)
	if err != nil {
		return nil, fmt.Errorf("sandbox: discover append-only tables: %w", err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("sandbox: scan append-only table name: %w", err)
		}
		out[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sandbox: list append-only tables: %w", err)
	}
	return out, nil
}

// preservedTables closes appendOnly over edges toward the tables it depends
// on: if a preserved table's row still exists and references a parent, that
// parent must be preserved too, or deleting it would violate the very
// foreign key the append-only row can never be updated or removed to
// release. The closure is what lets [DeleteTenantRows] compute a deletion
// plan over exactly the tables it may safely act on, rather than discovering
// a foreign-key violation from a table it should never have touched.
func preservedTables(appendOnly map[string]bool, edges []fkEdge) map[string]bool {
	preserved := make(map[string]bool, len(appendOnly))
	for t := range appendOnly {
		preserved[t] = true
	}
	for {
		grew := false
		for _, e := range edges {
			if preserved[e.child] && !preserved[e.parent] {
				preserved[e.parent] = true
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	return preserved
}

// fkEdge is one foreign key from child (the referencing table) to parent (the
// referenced table), both members of the tenant-scoped table set.
type fkEdge struct{ child, parent string }

// tenantForeignKeys returns every foreign key among tables, as (child,
// parent) pairs, read from pg_constraint. Self-references (a table whose
// foreign key names itself, e.g. a manager_id column) are dropped: a single
// DELETE against one table removes a self-referencing row set as one
// statement, so a self-edge carries no ordering information and would only
// make the topological sort below reject a graph that has no real cycle.
func tenantForeignKeys(ctx context.Context, q dbport.Querier, tables []string) ([]fkEdge, error) {
	member := make(map[string]bool, len(tables))
	for _, t := range tables {
		member[t] = true
	}

	rows, err := q.Query(ctx, `
		SELECT con.conrelid::regclass::text, con.confrelid::regclass::text
		FROM pg_constraint con
		WHERE con.contype = 'f'
			AND con.connamespace = current_schema()::regnamespace`)
	if err != nil {
		return nil, fmt.Errorf("sandbox: read foreign keys: %w", err)
	}
	defer rows.Close()

	var edges []fkEdge
	for rows.Next() {
		var child, parent string
		if err := rows.Scan(&child, &parent); err != nil {
			return nil, fmt.Errorf("sandbox: scan foreign key: %w", err)
		}
		child, parent = unqualify(child), unqualify(parent)
		if child == parent {
			continue
		}
		if !member[child] || !member[parent] {
			// A foreign key touching a table outside the tenant-scoped set
			// (a global reference table, say) carries no ordering
			// requirement this deletion needs to honour.
			continue
		}
		edges = append(edges, fkEdge{child: child, parent: parent})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sandbox: list foreign keys: %w", err)
	}
	return edges, nil
}

// unqualify strips a possible "schema"."name" quoting regclass::text can
// produce when a name needs quoting, keeping only the bare table name.
func unqualify(name string) string {
	name = strings.TrimSuffix(strings.TrimPrefix(name, `"`), `"`)
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return strings.Trim(name, `"`)
}

// dependencyOrder returns tables in an order safe to DELETE FROM, one at a
// time, under the foreign keys edges describes: every child appears before
// every parent it references, whenever the foreign keys among tables form a
// DAG. It is Kahn's algorithm over the graph whose edges are "child must
// precede parent" - the same partial order the deletion itself must respect.
//
// A schema is not always a DAG at the table level: two tables can each
// reference the other (through different columns), which is a real cycle
// with no total order at all. dependencyOrder reports that case rather than
// guessing; [dependencyGroups] is what a caller with a genuine cycle uses
// instead.
func dependencyOrder(tables []string, edges []fkEdge) ([]string, error) {
	order, cyclic := kahnOrder(tables, edges)
	if len(cyclic) > 0 {
		return nil, fmt.Errorf("sandbox: tenant-scoped tables have a foreign-key cycle among %v; cannot derive a single safe deletion order", cyclic)
	}
	return order, nil
}

// kahnOrder runs Kahn's algorithm over tables under edges (child->parent),
// peeling off tables with no remaining unprocessed child first. It returns
// the order it could resolve, plus every table it could not (the members of
// one or more real cycles), so a caller can decide what to do with each half
// rather than only being told a cycle exists somewhere.
func kahnOrder(tables []string, edges []fkEdge) (order []string, stuck []string) {
	indegree := make(map[string]int, len(tables))
	adj := make(map[string][]string, len(tables)) // child -> parents depending on it being gone first
	for _, t := range tables {
		indegree[t] = 0
	}
	for _, e := range edges {
		adj[e.child] = append(adj[e.child], e.parent)
		indegree[e.parent]++
	}

	// A stable starting order (sorted table names) makes the result
	// deterministic across runs when more than one table is simultaneously
	// eligible, which is what makes two independent Reset calls over the same
	// schema comparable.
	var ready []string
	for _, t := range tables {
		if indegree[t] == 0 {
			ready = append(ready, t)
		}
	}
	sort.Strings(ready)

	for len(ready) > 0 {
		sort.Strings(ready)
		next := ready[0]
		ready = ready[1:]
		order = append(order, next)
		for _, parent := range adj[next] {
			indegree[parent]--
			if indegree[parent] == 0 {
				ready = append(ready, parent)
			}
		}
	}
	if len(order) == len(tables) {
		return order, nil
	}
	resolved := make(map[string]bool, len(order))
	for _, t := range order {
		resolved[t] = true
	}
	for _, t := range tables {
		if !resolved[t] {
			stuck = append(stuck, t)
		}
	}
	sort.Strings(stuck)
	return order, stuck
}

// dependencyGroups partitions tables into an ordered list of groups, each
// safe to fully clear before the next group is touched: every edge
// child->parent has child's group at or before parent's group, and a group
// with more than one member is the one bundle of tables [kahnOrder] could not
// place in the acyclic prefix - a genuine foreign-key cycle, or a table
// reachable only through one, among those tables (handled by
// [deleteCyclicGroup]'s own retry loop rather than a plain per-table DELETE,
// since that loop tolerates internal structure this function does not need
// to further decompose).
func dependencyGroups(tables []string, edges []fkEdge) [][]string {
	order, stuck := kahnOrder(tables, edges)
	groups := make([][]string, 0, len(order)+1)
	for _, t := range order {
		groups = append(groups, []string{t})
	}
	if len(stuck) > 0 {
		groups = append(groups, stuck)
	}
	return groups
}

// DeleteTenantRows deletes every row belonging to tenantID from every
// mutable tenant-scoped table in the schema tx is bound to, in an order
// [dependencyGroups] derives from the schema's own foreign keys. Every
// statement it issues carries an exact `WHERE tenant_id = $1` predicate: it
// touches rows named by tenantID and nothing else, in any table, regardless
// of what other tenants' rows share those same tables.
//
// Append-only tables ([appendOnlyTables]) and anything [preservedTables]
// closes over because an append-only row still references it are never
// targeted at all: a tenant's evidence, ledger and registry history survives
// every Reset, exactly as it would in production.
//
// A group with more than one table (a genuine foreign-key cycle) is deleted
// through [deleteCyclicGroup] rather than a single plain statement per table.
//
// It returns the number of rows removed per table (zero-count tables
// included, so a caller can tell "nothing there" from "table not
// considered") and the names of the tables it left untouched because they
// are preserved.
func DeleteTenantRows(ctx context.Context, tx dbport.Tx, tenantID tenantUUID) (deleted map[string]int64, preserved []string, err error) {
	var zero tenantUUID
	if tenantID == zero {
		return nil, nil, fmt.Errorf("sandbox: delete tenant rows needs a tenant id")
	}
	tables, err := tenantScopedTables(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	appendOnly, err := appendOnlyTables(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	allEdges, err := tenantForeignKeys(ctx, tx, tables)
	if err != nil {
		return nil, nil, err
	}
	preservedSet := preservedTables(appendOnly, allEdges)

	var deletable []string
	for _, t := range tables {
		if preservedSet[t] {
			preserved = append(preserved, t)
			continue
		}
		deletable = append(deletable, t)
	}
	sort.Strings(preserved)

	var edges []fkEdge
	for _, e := range allEdges {
		if !preservedSet[e.child] && !preservedSet[e.parent] {
			edges = append(edges, e)
		}
	}
	groups := dependencyGroups(deletable, edges)

	deleted = make(map[string]int64, len(deletable))
	for _, group := range groups {
		if len(group) == 1 {
			table := group[0]
			n, execErr := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE tenant_id = $1`, quoteIdent(table)), tenantID)
			if execErr != nil {
				return nil, nil, fmt.Errorf("sandbox: delete tenant rows from %s: %w", table, execErr)
			}
			deleted[table] = n
			continue
		}
		if execErr := deleteCyclicGroup(ctx, tx, tenantID, group, deleted); execErr != nil {
			return nil, nil, execErr
		}
	}
	return deleted, preserved, nil
}

// deleteCyclicGroup deletes tenantID's rows from every table in a genuine
// foreign-key cycle (no per-table order can be safe for all of them at
// once). It repeatedly attempts a DELETE against each table still
// outstanding, under its own savepoint: a table another member of the cycle
// still points at fails with a foreign-key violation, which rolls back only
// that savepoint and leaves it for the next pass, while a table whose
// referencing rows (elsewhere in the cycle) are already gone succeeds and is
// removed from the outstanding set. Passes continue until nothing is left or
// a pass removes nothing, which can only mean two rows require each other's
// continued existence - a real, irreducible mutual dependency this generic
// deletion cannot resolve on its own.
func deleteCyclicGroup(ctx context.Context, tx dbport.Tx, tenantID tenantUUID, group []string, counts map[string]int64) error {
	remaining := append([]string(nil), group...)
	sort.Strings(remaining)

	for len(remaining) > 0 {
		var next []string
		progressed := false
		for _, table := range remaining {
			savepoint := quoteIdent("sandbox_reset_" + table)
			if _, err := tx.Exec(ctx, "SAVEPOINT "+savepoint); err != nil {
				return fmt.Errorf("sandbox: open savepoint for cyclic table %s: %w", table, err)
			}
			n, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE tenant_id = $1`, quoteIdent(table)), tenantID)
			if err != nil {
				if _, rerr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); rerr != nil {
					return fmt.Errorf("sandbox: roll back savepoint for cyclic table %s after %v: %w", table, err, rerr)
				}
				next = append(next, table)
				continue
			}
			if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
				return fmt.Errorf("sandbox: release savepoint for cyclic table %s: %w", table, err)
			}
			counts[table] = n
			progressed = true
		}
		if !progressed {
			return fmt.Errorf("sandbox: cyclic tenant-scoped tables %v could not be fully deleted; "+
				"a mutual foreign key requires each other's rows to still exist", remaining)
		}
		remaining = next
	}
	return nil
}

// quoteIdent double-quotes a catalog-derived identifier defensively. Table
// names in this schema are all plain lower-snake-case identifiers, but the
// names here come from a catalog query, not a literal in this file, so this
// keeps the interpolation into SQL text honest about being an identifier and
// not a value.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
