package runtimestate

import (
	"context"
	"sort"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// RuntimeTables is the closed, named set of tables DB-012's five migrations
// materialize for durable workflow-runtime and human-work state:
//
//	00016_workflow_runtime.sql        workflow_instance, workflow_node_execution
//	00017_work_item.sql               work_item, work_item_transition
//	00018_workflow_continuation.sql   workflow_continuation
//	00019_idempotency_record.sql      idempotency_record
//	00020_workflow_advancement_receipt.sql   workflow_advancement_receipt
//
// [SchemaInventory] compares the live schema against exactly this list, not
// against a table count: naming every member is what makes a future migration
// accidentally adding (or a typo silently dropping) one of these tables show
// up as a one-line diff instead of a wrong integer.
var RuntimeTables = []string{
	"idempotency_record",
	"work_item",
	"work_item_transition",
	"workflow_advancement_receipt",
	"workflow_continuation",
	"workflow_instance",
	"workflow_node_execution",
}

// GatedTables names lease, timer, signal-subscription, checkpoint,
// child-link, queue and SLA table shapes that GREEN's clause for DB-012
// mentions but definitions/runtime/durable-runtime-decision.yaml (WF-RUN-000)
// blocks behind its P1B re-evaluation gate. This is a representative
// candidate set, not a claim that no other table name could ever exist: the
// point [SchemaInventory] proves is that within this exact candidate
// universe, only [RuntimeTables]' members are present -- the gated ones are
// named here so their absence is checked by name rather than inferred from
// silence.
var GatedTables = []string{
	// Leases and fencing (WF-RUN-002, explicitly blocked).
	"execution_lease",
	"workflow_lease",
	// Timers (WF-RUN-004).
	"workflow_timer",
	// Signal subscriptions (WF-RUN-005).
	"signal_subscription",
	"workflow_signal_subscription",
	// Checkpoints (safe-point pause, WF-RUN-008/009 territory).
	"workflow_checkpoint",
	// Child-workflow links (WF-RUN-011, PHASE_2).
	"workflow_child_link",
	"child_workflow_link",
	// Queues/SLA (a scheduler's own work distribution, not work_item's claim
	// columns, which migration 00017 already carries on the item row itself).
	"work_queue",
	"work_item_queue",
	"workflow_queue",
	"work_item_claim",
	"sla_policy",
	"work_item_sla",
}

// Tables returns the base table names that exist in ex's current schema,
// sorted. It reads information_schema rather than pg_catalog directly so the
// query stays portable to any Postgres-compatible dbport.Querier, and it
// scopes to current_schema() because internal/data/pgtest gives every test
// its own private schema on a shared server: qualifying by name here is what
// keeps one test's inventory from seeing another's.
func Tables(ctx context.Context, ex dbport.Querier) ([]string, error) {
	rows, err := ex.Query(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'`)
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

// Contains reports whether name is present in set.
func Contains(set []string, name string) bool {
	for _, s := range set {
		if s == name {
			return true
		}
	}
	return false
}

// SchemaInventory is the live schema's answer, restricted to the candidate
// universe [RuntimeTables] union [GatedTables]: which of those names actually
// exist right now, and which are (correctly) absent.
type SchemaInventory struct {
	// Present is the subset of RuntimeTables union GatedTables that exists in
	// the live schema, sorted.
	Present []string
	// Missing is RuntimeTables minus Present: a required table that did not
	// materialize.
	Missing []string
	// Unexpected is GatedTables intersect Present: a gated table that exists
	// when the WF-RUN-000 gate says it should not.
	Unexpected []string
}

// Exact reports whether Present is exactly RuntimeTables: every required
// table exists, and no gated one does.
func (s SchemaInventory) Exact() bool {
	return len(s.Missing) == 0 && len(s.Unexpected) == 0
}

// Inspect builds a [SchemaInventory] from ex's live schema.
func Inspect(ctx context.Context, ex dbport.Querier) (SchemaInventory, error) {
	live, err := Tables(ctx, ex)
	if err != nil {
		return SchemaInventory{}, err
	}
	universe := append(append([]string(nil), RuntimeTables...), GatedTables...)

	var inv SchemaInventory
	for _, name := range universe {
		if Contains(live, name) {
			inv.Present = append(inv.Present, name)
		}
	}
	sort.Strings(inv.Present)
	for _, want := range RuntimeTables {
		if !Contains(inv.Present, want) {
			inv.Missing = append(inv.Missing, want)
		}
	}
	for _, gated := range GatedTables {
		if Contains(inv.Present, gated) {
			inv.Unexpected = append(inv.Unexpected, gated)
		}
	}
	return inv, nil
}
