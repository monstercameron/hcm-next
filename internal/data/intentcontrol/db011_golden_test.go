package intentcontrol_test

import (
	"context"
	"slices"
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestTodo_DB_011_Golden pins the closed vocabularies migration 00024 declares
// and the grant surface each table carries.
//
// These are goldens rather than assertions about behavior because they are the
// contract other packages compile against: an added abort reason or a widened
// grant is a decision someone should have to make deliberately, and a golden is
// what turns "someone edited the migration" into a failing diff instead of a
// silent capability change.
func TestTodo_DB_011_Golden(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)

	t.Run("closed vocabularies", func(t *testing.T) {
		for _, want := range []struct {
			table, constraint string
			values            []string
		}{
			{"intent_instance_context", "intent_instance_context_family_allowed",
				[]string{"ANALYTICAL_REQUEST", "CALCULATION_REQUEST", "CHANGE_REQUEST"}},
			{"intent_instance_context", "intent_instance_context_origin_trust_allowed",
				[]string{"ASSERTED", "TRUSTED", "UNVERIFIED"}},
			{"hcm_change_request", "hcm_change_request_status_allowed",
				[]string{"APPROVED", "CLOSED", "COMMITTED", "DRAFT", "PREFLIGHTED", "REJECTED",
					"SUBMITTED", "WITHDRAWN"}},
			{"intent_input_snapshot", "intent_input_snapshot_purpose_allowed",
				[]string{"PREFLIGHT", "REPAIR", "REVALIDATION", "SIMULATION"}},
			{"intent_simulation_result", "intent_simulation_result_status_allowed",
				[]string{"BLOCKED", "INCONCLUSIVE", "READY", "REJECTED"}},
			{"intent_relationship", "intent_relationship_type_allowed",
				[]string{"CHILD_OF", "CORRECTS", "REPAIRS", "SUPERSEDES"}},
			{"intent_result", "intent_result_kind_allowed",
				[]string{"ABANDONED", "COMMITTED", "CORRECTED", "REJECTED", "SIMULATED"}},
			{"intent_decision", "intent_decision_outcome_allowed",
				[]string{"ABSTAINED", "APPROVED", "DELEGATED", "REJECTED"}},
			{"intent_certificate", "intent_certificate_kind_allowed",
				[]string{"APPROVAL", "COMMIT", "GOVERNANCE", "PREFLIGHT", "SIMULATION"}},
			{"transaction_plan", "transaction_plan_status_allowed",
				[]string{"BLOCKED", "GOVERNANCE_VALIDATED"}},
			{"transaction_plan_binding", "transaction_plan_binding_state_allowed",
				[]string{"ABORTED", "AMBIGUOUS", "APPROVAL_BOUND", "COMMITTED", "COMMITTING",
					"DOMAIN_VALIDATED", "DRAFT", "GOVERNANCE_VALIDATED", "READY",
					"REPAIR_REQUIRED", "RESERVED", "STALE"}},
			{"transaction_abort_receipt", "transaction_abort_receipt_reason_allowed",
				[]string{"APPROVAL_MISMATCH", "AUTHORITY_CHANGED", "CANCELLED", "DATABASE_ABORT",
					"DIGEST_MISMATCH", "DOMAIN_REJECTED", "GOVERNANCE_REJECTED",
					"IDEMPOTENCY_CONFLICT", "PLAN_EXPIRED", "PLAN_STALE", "RESERVATION_LOST",
					"SEQUENCE_CONFLICT"}},
			{"repair_plan", "repair_plan_status_allowed",
				[]string{"ABANDONED", "IN_PROGRESS", "OPEN", "REPAIRED"}},
			{"intent_closure", "intent_closure_kind_allowed",
				[]string{"CANCELLED", "COMMITTED", "CORRECTED", "EXPIRED", "REJECTED",
					"SUPERSEDED", "WITHDRAWN"}},
		} {
			got := checkVocabulary(t, db, want.table, want.constraint)
			if !slices.Equal(got, want.values) {
				t.Errorf("%s.%s admits %v, want %v", want.table, want.constraint, got, want.values)
			}
		}
	})

	t.Run("grant surface: append-only tables get SELECT/INSERT, live ones also UPDATE, none gets DELETE", func(t *testing.T) {
		for _, table := range appendOnlyControlTables {
			got := grantsFor(t, db, table)
			if !slices.Equal(got, []string{"INSERT", "SELECT"}) {
				t.Errorf("hcmnext_app holds %v on append-only %s, want [INSERT SELECT]", got, table)
			}
		}
		for _, table := range liveControlTables {
			got := grantsFor(t, db, table)
			if !slices.Equal(got, []string{"INSERT", "SELECT", "UPDATE"}) {
				t.Errorf("hcmnext_app holds %v on live %s, want [INSERT SELECT UPDATE]", got, table)
			}
		}
	})

	t.Run("every control table is tenant scoped in its primary key", func(t *testing.T) {
		for _, table := range controlTables {
			cols := primaryKeyColumns(t, db, table)
			if len(cols) == 0 {
				t.Errorf("%s has no primary key", table)
				continue
			}
			if !slices.Contains(cols, "tenant_id") {
				t.Errorf("%s primary key %v does not include tenant_id", table, cols)
			}
		}
	})
}

// checkVocabulary reads the literal string values a CHECK constraint admits,
// sorted. It parses the constraint's own source text rather than probing with
// inserts, so an added token shows up even for a table the test never writes to.
func checkVocabulary(t *testing.T, db *pgtest.DB, table, constraint string) []string {
	t.Helper()
	var expr string
	if err := db.QueryRow(context.Background(), `
		SELECT pg_get_constraintdef(con.oid)
		FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		JOIN pg_namespace ns ON ns.oid = c.relnamespace
		WHERE ns.nspname = current_schema() AND c.relname = $1 AND con.conname = $2`,
		table, constraint).Scan(&expr); err != nil {
		t.Fatalf("read constraint %s on %s: %v", constraint, table, err)
	}
	values := literalsIn(expr)
	sort.Strings(values)
	return values
}

// literalsIn extracts the single-quoted literals from a constraint definition.
// PostgreSQL renders a CHECK ... IN (...) as an ANY(ARRAY[...]) expression with
// every token as a quoted literal, so scanning for quotes is enough and needs no
// SQL parser.
func literalsIn(expr string) []string {
	var (
		out     []string
		current []rune
		inside  bool
	)
	for _, r := range expr {
		switch {
		case r == '\'' && !inside:
			inside = true
			current = current[:0]
		case r == '\'' && inside:
			inside = false
			if token := string(current); token != "" && isVocabularyToken(token) {
				out = append(out, token)
			}
		case inside:
			current = append(current, r)
		}
	}
	return dedupe(out)
}

// isVocabularyToken keeps the upper-case tokens and drops the type names
// PostgreSQL renders alongside them (::text and friends never appear quoted, but
// a cast target can).
func isVocabularyToken(token string) bool {
	for _, r := range token {
		if (r < 'A' || r > 'Z') && r != '_' {
			return false
		}
	}
	return true
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// grantsFor returns the privileges hcmnext_app holds on table, sorted.
func grantsFor(t *testing.T, db *pgtest.DB, table string) []string {
	t.Helper()
	rows, err := db.Conn.Query(context.Background(), `
		SELECT privilege_type FROM information_schema.role_table_grants
		WHERE table_schema = current_schema() AND table_name = $1 AND grantee = 'hcmnext_app'`, table)
	if err != nil {
		t.Fatalf("read grants on %s: %v", table, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var privilege string
		if err := rows.Scan(&privilege); err != nil {
			t.Fatalf("scan grant: %v", err)
		}
		out = append(out, privilege)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read grants on %s: %v", table, err)
	}
	sort.Strings(out)
	return out
}

// primaryKeyColumns returns table's primary key columns in key order.
func primaryKeyColumns(t *testing.T, db *pgtest.DB, table string) []string {
	t.Helper()
	var cols []string
	if err := db.QueryRow(context.Background(), `
		SELECT array_agg(att.attname ORDER BY x.ord)
		FROM pg_constraint con
		JOIN pg_class rel ON rel.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = rel.relnamespace
		CROSS JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS x(attnum, ord)
		JOIN pg_attribute att ON att.attrelid = con.conrelid AND att.attnum = x.attnum
		WHERE con.contype = 'p' AND n.nspname = current_schema() AND rel.relname = $1`,
		table).Scan(&cols); err != nil {
		t.Fatalf("read primary key of %s: %v", table, err)
	}
	return cols
}
