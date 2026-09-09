package integrationmeta

import (
	"context"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// IntegrationTables is the exact set of base tables migration 00031 creates
// for the integration half of DB-014, in sorted order.
var IntegrationTables = []string{
	"connector_connection",
	"connector_definition",
	"connector_operation",
	"connector_operation_attempt",
	"external_operation_observation",
	"external_system",
	"integration_receipt",
	"integration_receipt_item",
	"mapping_execution",
	"mapping_profile",
	"reconciliation_job",
	"reconciliation_result",
}

// ForeignTables are tables DB-014 deliberately does NOT create because another
// migration owns them. A test asserting the boundary reads this list rather
// than restating it, so an accidental duplicate here is a failure, not a
// silent second definition.
var ForeignTables = []string{
	"integration_schema_snapshot",          // 00025
	"integration_schema_snapshot_evidence", // 00025
	"external_observation",                 // 00007
	"observation_checkpoint",               // 00007
	"config_object",                        // 00027
	"config_object_activation",             // 00027
	"tenant_bootstrap_receipt",             // 00029
	"ledger_checkpoint_epoch",              // 00028
}

// AppendOnlyTables carry migration 00031's forbid_mutation trigger.
var AppendOnlyTables = []string{
	"connector_operation_attempt",
	"delivery_attempt",
	"delivery_receipt",
	"document_artifact_reference",
	"document_version",
	"external_operation_observation",
	"integration_receipt",
	"integration_receipt_item",
	"mapping_execution",
	"reconciliation_result",
	"signature",
}

// LiveTables lists the schema's own base tables, sorted, excluding Goose's
// bookkeeping table and the physical partitions of a partitioned table.
func LiveTables(ctx context.Context, q dbport.Querier) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema()
		  AND c.relkind IN ('r', 'p')
		  AND NOT c.relispartition
		  AND c.relname <> 'goose_db_version'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// Contains reports whether set holds name.
func Contains(set []string, name string) bool {
	for _, s := range set {
		if s == name {
			return true
		}
	}
	return false
}

// Inventory is the result of comparing a wanted table set against the live
// schema.
type Inventory struct {
	Present []string
	Missing []string
}

// Exact reports whether every wanted table is present.
func (i Inventory) Exact() bool { return len(i.Missing) == 0 }

// Inspect compares want against the live schema.
func Inspect(ctx context.Context, q dbport.Querier, want []string) (Inventory, error) {
	live, err := LiveTables(ctx, q)
	if err != nil {
		return Inventory{}, err
	}
	var inv Inventory
	for _, name := range want {
		if Contains(live, name) {
			inv.Present = append(inv.Present, name)
		} else {
			inv.Missing = append(inv.Missing, name)
		}
	}
	sort.Strings(inv.Present)
	sort.Strings(inv.Missing)
	return inv, nil
}

// TriggerNames returns the names of the BEFORE UPDATE OR DELETE triggers
// attached to table, so a test can prove append-only enforcement is present
// rather than only observing that one write happened to fail.
func TriggerNames(ctx context.Context, q dbport.Querier, table string) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT t.tgname
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relname = $1 AND NOT t.tgisinternal
		ORDER BY t.tgname`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}
