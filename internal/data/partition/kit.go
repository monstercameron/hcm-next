package partition

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// identPattern is the whole set of table/role names this package ever builds
// SQL text with. Every name is either a Go-source literal (LedgerEventPlan's
// Table, a Kit's ShadowTable) or derived from one with a fixed suffix, never
// attacker- or caller-supplied at runtime, but validateIdent is still checked
// before any name is spliced into a statement: a typo that produced an empty
// or punctuation-laden identifier fails loudly here instead of becoming a
// second, harder-to-read SQL syntax error from the server.
var identPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateIdent(kind, name string) {
	if !identPattern.MatchString(name) {
		panic(fmt.Sprintf("partition: %s %q is not a plain SQL identifier", kind, name))
	}
}

// Kit compares a partitioned parent table's declared behavior against a
// hand-built, non-partitioned shadow copy loaded with identical rows, so
// that DATA-017's requirement -- partitioning is invisible to every caller
// that is not the migration itself -- is a property that gets checked
// against a real database rather than merely a design claim.
type Kit struct {
	// Table is the partitioned parent's relation name.
	Table string
	// ShadowTable is the name Build creates and every comparison method
	// reads from. It must not already exist in the test schema.
	ShadowTable string
}

// Build creates k.ShadowTable as an ordinary, non-partitioned table with
// k.Table's own columns, defaults and CHECK constraints ("LIKE ...
// INCLUDING ALL"), then reproduces -- LIKE copies neither -- every row level
// security policy and every user-defined trigger k.Table itself declares,
// plus k.Table's own SELECT/INSERT/UPDATE/DELETE grants to role. Build must
// run on a connection that can create objects and alter privileges (an
// admin/superuser connection such as pgtest.DB.Conn): the least-privilege
// application role is never granted CREATE.
func (k Kit) Build(t *testing.T, ctx context.Context, admin dbport.Conn, role string) {
	t.Helper()
	validateIdent("table", k.Table)
	validateIdent("shadow table", k.ShadowTable)
	validateIdent("role", role)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("partition.Kit.Build: %s: %v", firstLine(sql), err)
		}
	}

	exec(fmt.Sprintf(`CREATE TABLE %s (LIKE %s INCLUDING ALL)`, k.ShadowTable, k.Table))
	k.clonePolicies(t, ctx, admin)
	k.cloneTriggers(t, ctx, admin)
	k.cloneGrants(t, ctx, admin, role)
}

// clonePolicies reproduces every row level security policy k.Table declares
// onto k.ShadowTable, by reading pg_policies rather than relying on any
// inheritance PostgreSQL might apply automatically (it applies none: RLS
// policies are never copied by LIKE, and a partition -- unlike a plain
// table -- gets ENABLE/FORCE and a policy from the parent's own DDL, not
// from any general PostgreSQL cloning mechanism the shadow could ride along
// on).
func (k Kit) clonePolicies(t *testing.T, ctx context.Context, admin dbport.Conn) {
	t.Helper()
	rows, err := admin.Query(ctx, `
		SELECT policyname, cmd, array_to_string(roles, ','), coalesce(qual, ''), coalesce(with_check, '')
		FROM pg_policies
		WHERE schemaname = current_schema() AND tablename = $1
		ORDER BY policyname`, k.Table)
	if err != nil {
		t.Fatalf("partition.Kit.Build: read policies on %s: %v", k.Table, err)
	}
	defer rows.Close()

	type policy struct{ name, cmd, roles, qual, withCheck string }
	var policies []policy
	for rows.Next() {
		var p policy
		if err := rows.Scan(&p.name, &p.cmd, &p.roles, &p.qual, &p.withCheck); err != nil {
			t.Fatalf("partition.Kit.Build: scan policy on %s: %v", k.Table, err)
		}
		policies = append(policies, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("partition.Kit.Build: iterate policies on %s: %v", k.Table, err)
	}
	if len(policies) == 0 {
		// Nothing to clone is a legitimate answer for a table with no RLS at
		// all; Build must not fabricate a policy that was never there.
		return
	}

	if _, err := admin.Exec(ctx, fmt.Sprintf(`ALTER TABLE %s ENABLE ROW LEVEL SECURITY`, k.ShadowTable)); err != nil {
		t.Fatalf("partition.Kit.Build: enable RLS on %s: %v", k.ShadowTable, err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf(`ALTER TABLE %s FORCE ROW LEVEL SECURITY`, k.ShadowTable)); err != nil {
		t.Fatalf("partition.Kit.Build: force RLS on %s: %v", k.ShadowTable, err)
	}

	for _, p := range policies {
		var b strings.Builder
		fmt.Fprintf(&b, `CREATE POLICY %s ON %s`, quoteIdent(p.name), k.ShadowTable)
		if p.cmd != "ALL" {
			fmt.Fprintf(&b, ` FOR %s`, p.cmd)
		}
		if p.roles != "public" {
			fmt.Fprintf(&b, ` TO %s`, p.roles)
		}
		if p.qual != "" {
			fmt.Fprintf(&b, ` USING (%s)`, p.qual)
		}
		if p.withCheck != "" {
			fmt.Fprintf(&b, ` WITH CHECK (%s)`, p.withCheck)
		}
		if _, err := admin.Exec(ctx, b.String()); err != nil {
			t.Fatalf("partition.Kit.Build: create policy %s on %s: %v", p.name, k.ShadowTable, err)
		}
	}
}

// cloneTriggers reproduces every non-internal trigger k.Table declares onto
// k.ShadowTable. It reads back pg_get_triggerdef's own rendering of each
// trigger and substitutes the shadow's name for the parent's wherever it
// appears as a whole word -- the only place a plain "CREATE TRIGGER ...
// FOR EACH ROW EXECUTE FUNCTION ..." statement names a table at all is its
// "ON <table>" clause, so this is exact for the triggers this data plane
// actually declares (forbid_mutation, no arguments, no WHEN clause naming
// the table).
func (k Kit) cloneTriggers(t *testing.T, ctx context.Context, admin dbport.Conn) {
	t.Helper()
	rows, err := admin.Query(ctx, `
		SELECT pg_get_triggerdef(oid, true)
		FROM pg_trigger
		WHERE tgrelid = $1::regclass AND NOT tgisinternal
		ORDER BY tgname`, k.Table)
	if err != nil {
		t.Fatalf("partition.Kit.Build: read triggers on %s: %v", k.Table, err)
	}
	defer rows.Close()

	// Table names come from the runtime schema, so the pattern is per call.
	renamer := regexp.MustCompile(`\b` + regexp.QuoteMeta(k.Table) + `\b`) // regexhoist:dynamic
	var defs []string
	for rows.Next() {
		var def string
		if err := rows.Scan(&def); err != nil {
			t.Fatalf("partition.Kit.Build: scan trigger def on %s: %v", k.Table, err)
		}
		defs = append(defs, def)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("partition.Kit.Build: iterate triggers on %s: %v", k.Table, err)
	}

	for _, def := range defs {
		shadowDef := renamer.ReplaceAllString(def, k.ShadowTable)
		if shadowDef == def {
			t.Fatalf("partition.Kit.Build: trigger def on %s does not mention the table by name, cannot retarget: %s", k.Table, def)
		}
		if _, err := admin.Exec(ctx, shadowDef); err != nil {
			t.Fatalf("partition.Kit.Build: create trigger on %s from %q: %v", k.ShadowTable, shadowDef, err)
		}
	}
}

// cloneGrants reproduces k.Table's own table-level grants to role onto
// k.ShadowTable, read back from information_schema.role_table_grants. LIKE
// carries no privileges, and a table's real grants (SELECT and INSERT only,
// for an append-only table) are exactly what makes a refusal at the SQL
// level a row-level-security or a trigger refusal and not merely
// permission-denied noise.
func (k Kit) cloneGrants(t *testing.T, ctx context.Context, admin dbport.Conn, role string) {
	t.Helper()
	rows, err := admin.Query(ctx, `
		SELECT privilege_type
		FROM information_schema.role_table_grants
		WHERE table_schema = current_schema() AND table_name = $1 AND grantee = $2
		ORDER BY privilege_type`, k.Table, role)
	if err != nil {
		t.Fatalf("partition.Kit.Build: read grants on %s: %v", k.Table, err)
	}
	defer rows.Close()

	var privileges []string
	for rows.Next() {
		var priv string
		if err := rows.Scan(&priv); err != nil {
			t.Fatalf("partition.Kit.Build: scan grant on %s: %v", k.Table, err)
		}
		privileges = append(privileges, priv)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("partition.Kit.Build: iterate grants on %s: %v", k.Table, err)
	}
	if len(privileges) == 0 {
		return
	}
	sql := fmt.Sprintf(`GRANT %s ON %s TO %s`, strings.Join(privileges, ", "), k.ShadowTable, role)
	if _, err := admin.Exec(ctx, sql); err != nil {
		t.Fatalf("partition.Kit.Build: %s: %v", sql, err)
	}
}

// Load copies k.Table's current rows into k.ShadowTable, verbatim and in one
// statement, on a connection that bypasses row level security (an
// admin/superuser connection): the shadow's whole purpose is to hold
// identical rows, not a re-derived approximation of them.
func (k Kit) Load(t *testing.T, ctx context.Context, admin dbport.Conn) {
	t.Helper()
	sql := fmt.Sprintf(`INSERT INTO %s SELECT * FROM %s`, k.ShadowTable, k.Table)
	if _, err := admin.Exec(ctx, sql); err != nil {
		t.Fatalf("partition.Kit.Load: %s: %v", sql, err)
	}
}

// fill substitutes the literal token "{table}" in sqlText with tableName.
func (k Kit) fill(tableName, sqlText string) string {
	return strings.ReplaceAll(sqlText, "{table}", tableName)
}

// CompareRows runs query (which must contain the literal token "{table}",
// and should ORDER BY something deterministic) against both k.Table and
// k.ShadowTable through conn, and fails t unless the two result sets are
// identical row for row and column for column. Comparison goes through
// to_jsonb so this needs no column list or Go struct describing either
// table's shape: any two tables with the same columns compare correctly.
func (k Kit) CompareRows(t *testing.T, ctx context.Context, conn dbport.Conn, name, query string, args ...any) {
	t.Helper()
	parentRows := k.fetchJSON(t, ctx, conn, k.Table, name, query, args...)
	shadowRows := k.fetchJSON(t, ctx, conn, k.ShadowTable, name, query, args...)

	if len(parentRows) != len(shadowRows) {
		t.Fatalf("query %q: partitioned %s returned %d rows, shadow %s returned %d\nparent: %v\nshadow: %v",
			name, k.Table, len(parentRows), k.ShadowTable, len(shadowRows), parentRows, shadowRows)
	}
	for i := range parentRows {
		if parentRows[i] != shadowRows[i] {
			t.Fatalf("query %q: row %d disagrees\npartitioned %s: %s\nshadow %s: %s",
				name, i, k.Table, parentRows[i], k.ShadowTable, shadowRows[i])
		}
	}
}

func (k Kit) fetchJSON(t *testing.T, ctx context.Context, conn dbport.Conn, tableName, name, query string, args ...any) []string {
	t.Helper()
	wrapped := fmt.Sprintf(`SELECT to_jsonb(t) FROM (%s) t`, k.fill(tableName, query))
	rows, err := conn.Query(ctx, wrapped, args...)
	if err != nil {
		t.Fatalf("query %q against %s: %v", name, tableName, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var js string
		if err := rows.Scan(&js); err != nil {
			t.Fatalf("query %q: scan a row from %s: %v", name, tableName, err)
		}
		out = append(out, js)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("query %q: iterate %s: %v", name, tableName, err)
	}
	return out
}

// CompareRefusal runs statement (containing "{table}") against k.Table
// through parentConn and against k.ShadowTable through shadowConn, and fails
// t unless both are refused and both refusals' text mentions wantSubstring
// (case-insensitive) -- the wording a specific constraint (the append-only
// trigger's RAISE EXCEPTION, or a row-level-security WITH CHECK failure) is
// documented to produce. Requiring the same wording, not merely "both
// errored", is what rules out the two tables refusing the same statement for
// two different, coincidental reasons.
//
// parentConn and shadowConn are two separate parameters, not one shared
// connection, because a refusal inside an explicit transaction aborts it:
// once the first Exec fails, every later statement on that same transaction
// fails with "current transaction is aborted" regardless of what it says,
// which would prove nothing about the second table. A caller comparing two
// bare (non-transactional) connections -- where each Exec is its own
// implicit transaction -- may safely pass the same connection for both.
func (k Kit) CompareRefusal(t *testing.T, ctx context.Context, parentConn, shadowConn dbport.Conn, name, wantSubstring, statement string, args ...any) {
	t.Helper()
	_, parentErr := parentConn.Exec(ctx, k.fill(k.Table, statement), args...)
	_, shadowErr := shadowConn.Exec(ctx, k.fill(k.ShadowTable, statement), args...)

	if parentErr == nil {
		t.Fatalf("%s: partitioned %s accepted a statement expected to be refused (%s)", name, k.Table, wantSubstring)
	}
	if shadowErr == nil {
		t.Fatalf("%s: shadow %s accepted a statement expected to be refused (%s)", name, k.ShadowTable, wantSubstring)
	}
	want := strings.ToLower(wantSubstring)
	if !strings.Contains(strings.ToLower(parentErr.Error()), want) {
		t.Fatalf("%s: partitioned %s refusal %q does not mention %q", name, k.Table, parentErr, wantSubstring)
	}
	if !strings.Contains(strings.ToLower(shadowErr.Error()), want) {
		t.Fatalf("%s: shadow %s refusal %q does not mention %q", name, k.ShadowTable, shadowErr, wantSubstring)
	}
}

// Partitions returns k.Table's direct leaf partitions, in name order, via
// pg_partition_tree -- the catalog function PostgreSQL itself provides for
// walking a partition hierarchy, rather than this package guessing a naming
// convention such as "_p0.._p3".
func (k Kit) Partitions(t *testing.T, ctx context.Context, admin dbport.Conn) []string {
	t.Helper()
	rows, err := admin.Query(ctx, `
		SELECT c.relname
		FROM pg_partition_tree($1::regclass) pt
		JOIN pg_class c ON c.oid = pt.relid
		WHERE pt.isleaf
		ORDER BY c.relname`, k.Table)
	if err != nil {
		t.Fatalf("partition.Kit.Partitions: list partitions of %s: %v", k.Table, err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("partition.Kit.Partitions: scan a partition name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("partition.Kit.Partitions: iterate partitions of %s: %v", k.Table, err)
	}
	return names
}

// policySignature is one row level security policy, reduced to the fields
// that determine its behavior, for equality comparison between a parent
// table and one of its partitions.
type policySignature struct{ name, cmd, roles, qual, withCheck string }

func (k Kit) policySignatures(t *testing.T, ctx context.Context, admin dbport.Conn, tableName string) []policySignature {
	t.Helper()
	rows, err := admin.Query(ctx, `
		SELECT policyname, cmd, array_to_string(roles, ','), coalesce(qual, ''), coalesce(with_check, '')
		FROM pg_policies
		WHERE schemaname = current_schema() AND tablename = $1
		ORDER BY policyname`, tableName)
	if err != nil {
		t.Fatalf("partition.Kit: read policies on %s: %v", tableName, err)
	}
	defer rows.Close()

	var out []policySignature
	for rows.Next() {
		var p policySignature
		if err := rows.Scan(&p.name, &p.cmd, &p.roles, &p.qual, &p.withCheck); err != nil {
			t.Fatalf("partition.Kit: scan policy on %s: %v", tableName, err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("partition.Kit: iterate policies on %s: %v", tableName, err)
	}
	return out
}

// CheckPartitionPolicies fails t unless every one of k.Table's own row level
// security policies -- name, command, role list and USING/WITH CHECK
// expression text, verbatim -- is present on every leaf partition
// [Kit.Partitions] finds, and unless every partition also has row level
// security itself ENABLE'd and FORCE'd (relrowsecurity, relforcerowsecurity
// in pg_class). PostgreSQL clones a partitioned parent's triggers onto every
// partition automatically; it does not do the same for RLS policies, so a
// partition that only inherited ENABLE/FORCE from being "part of" the
// parent, without its own CREATE POLICY, would be enabled-but-policy-less --
// which denies everything, a fail-closed availability bug rather than a
// leak, but still a real gap this check must catch rather than assume away
// (migrations/00008_tenant_isolation.sql section on ledger_event explains
// why each partition needs its own copy).
func (k Kit) CheckPartitionPolicies(t *testing.T, ctx context.Context, admin dbport.Conn) {
	t.Helper()
	parent := k.policySignatures(t, ctx, admin, k.Table)
	if len(parent) == 0 {
		t.Fatalf("partition.Kit.CheckPartitionPolicies: %s declares no row level security policy to check", k.Table)
	}

	for _, part := range k.Partitions(t, ctx, admin) {
		var rowSecurity, forceRowSecurity bool
		if err := admin.QueryRow(ctx, `
			SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = $1::regclass`,
			part).Scan(&rowSecurity, &forceRowSecurity); err != nil {
			t.Fatalf("partition.Kit.CheckPartitionPolicies: read pg_class for %s: %v", part, err)
		}
		if !rowSecurity {
			t.Fatalf("partition.Kit.CheckPartitionPolicies: partition %s does not have row level security enabled", part)
		}
		if !forceRowSecurity {
			t.Fatalf("partition.Kit.CheckPartitionPolicies: partition %s does not FORCE row level security", part)
		}

		got := k.policySignatures(t, ctx, admin, part)
		if !policySetsEqual(parent, got) {
			t.Fatalf("partition.Kit.CheckPartitionPolicies: partition %s's policies %+v do not match parent %s's %+v",
				part, got, k.Table, parent)
		}
	}
}

func policySetsEqual(a, b []policySignature) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// triggerSignature is one non-internal trigger, reduced to its name and its
// definition text with the owning table's own name blanked out (so a parent
// and a partition, which necessarily differ only in that one name, compare
// equal).
type triggerSignature struct{ name, normalizedDef string }

func (k Kit) triggerSignatures(t *testing.T, ctx context.Context, admin dbport.Conn, tableName string) []triggerSignature {
	t.Helper()
	rows, err := admin.Query(ctx, `
		SELECT tgname, pg_get_triggerdef(oid, true)
		FROM pg_trigger
		WHERE tgrelid = $1::regclass AND NOT tgisinternal
		ORDER BY tgname`, tableName)
	if err != nil {
		t.Fatalf("partition.Kit: read triggers on %s: %v", tableName, err)
	}
	defer rows.Close()

	// Table names come from the runtime schema, so the pattern is per call.
	renamer := regexp.MustCompile(`\b` + regexp.QuoteMeta(tableName) + `\b`) // regexhoist:dynamic
	var out []triggerSignature
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			t.Fatalf("partition.Kit: scan trigger on %s: %v", tableName, err)
		}
		out = append(out, triggerSignature{name: name, normalizedDef: renamer.ReplaceAllString(def, "{self}")})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("partition.Kit: iterate triggers on %s: %v", tableName, err)
	}
	return out
}

// CheckPartitionTriggers fails t unless every one of k.Table's own
// non-internal triggers is present, with an equivalent definition, on every
// leaf partition [Kit.Partitions] finds. PostgreSQL does clone a partitioned
// parent's own triggers onto every partition automatically (unlike RLS
// policies), so this check is expected to pass by construction; it stays
// here because "expected to" is exactly the claim DATA-017 exists to verify
// against a real database rather than take on faith.
func (k Kit) CheckPartitionTriggers(t *testing.T, ctx context.Context, admin dbport.Conn) {
	t.Helper()
	parent := k.triggerSignatures(t, ctx, admin, k.Table)
	if len(parent) == 0 {
		t.Fatalf("partition.Kit.CheckPartitionTriggers: %s declares no trigger to check", k.Table)
	}

	for _, part := range k.Partitions(t, ctx, admin) {
		got := k.triggerSignatures(t, ctx, admin, part)
		if len(got) != len(parent) {
			t.Fatalf("partition.Kit.CheckPartitionTriggers: partition %s has %d triggers, parent %s has %d",
				part, len(got), k.Table, len(parent))
		}
		for i := range parent {
			if got[i] != parent[i] {
				t.Fatalf("partition.Kit.CheckPartitionTriggers: partition %s trigger %+v does not match parent %s trigger %+v",
					part, got[i], k.Table, parent[i])
			}
		}
	}
}

// quoteIdent double-quotes name for use as an SQL identifier. It is used only
// for names already checked by validateIdent or read back verbatim from the
// catalog (policy names), never for untrusted input.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
