package runtimestate

import (
	"context"
	"slices"
	"sort"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// RuntimeTables is the closed, named set of tables DB-012's migrations
// materialize for durable workflow-runtime and human-work state:
//
//	00016_workflow_runtime.sql        workflow_instance, workflow_node_execution
//	00017_work_item.sql               work_item, work_item_transition
//	00018_workflow_continuation.sql   workflow_continuation
//	00019_idempotency_record.sql      idempotency_record
//	00020_workflow_advancement_receipt.sql   workflow_advancement_receipt
//	00026_workflow_scheduling_state.sql      workflow_frontier_entry,
//	                                  workflow_node_output, workflow_ready_work,
//	                                  workflow_variable, workflow_timer,
//	                                  workflow_signal_subscription,
//	                                  workflow_signal, workflow_signal_receipt,
//	                                  workflow_lease, workflow_checkpoint,
//	                                  workflow_child_link, work_queue,
//	                                  work_queue_item, work_item_claim,
//	                                  work_item_sla,
//	                                  workflow_approval_requirement
//
// [SchemaInventory] compares the live schema against exactly this list, not
// against a table count: naming every member is what makes a future migration
// accidentally adding (or a typo silently dropping) one of these tables show
// up as a one-line diff instead of a wrong integer.
var RuntimeTables = []string{
	"idempotency_record",
	"work_item",
	"work_item_claim",
	"work_item_sla",
	"work_item_transition",
	"work_queue",
	"work_queue_item",
	"workflow_advancement_receipt",
	"workflow_approval_requirement",
	"workflow_checkpoint",
	"workflow_child_link",
	"workflow_continuation",
	"workflow_frontier_entry",
	"workflow_instance",
	"workflow_lease",
	"workflow_node_execution",
	"workflow_node_output",
	"workflow_ready_work",
	"workflow_signal",
	"workflow_signal_receipt",
	"workflow_signal_subscription",
	"workflow_timer",
	"workflow_variable",
}

// GatedTables names the tables a SCHEDULER PROCESS would need for its own
// bookkeeping, which definitions/runtime/durable-runtime-decision.yaml
// (WF-RUN-000) still blocks.
//
// This list changed shape when migration 00026 landed, and the reason matters.
// Before 00026 it named the durable state itself -- leases, timers, signal
// subscriptions, checkpoints, child links, queues -- because none of it existed
// and DB-012's evidence had to prove the absence by name rather than infer it
// from silence. 00026 materialized that state, which is DB-012's own scope: a
// GATE_B data-plane todo about the rows a runtime persists, not a WF-RUN todo
// about the code that drives them. What the gate actually blocks is that code --
// "P1B implementation of WF-RUN-002 through WF-RUN-026 scheduler/timer/lease
// code", a background worker with its own clock, claim loop and retry driver --
// and a scheduler like that keeps its own operational bookkeeping in its own
// tables: a worker registry, a heartbeat table, a timer-wheel shard map, an
// admission/workload limit, a retry policy table, a poison quarantine.
//
// So the list below is that bookkeeping. Its members are still, correctly,
// absent, and [SchemaInventory] keeps checking their absence by name -- the same
// discipline as before, now pointed at the thing the gate is actually about.
// This package's own stores are caller-driven and hold no goroutine, ticker or
// background claim, so nothing here is the code the gate blocks.
var GatedTables = []string{
	// A scheduler's own worker fleet and liveness (WF-RUN-002/003 driver code,
	// distinct from workflow_lease, which is the durable lease row a caller
	// takes).
	"scheduler_worker",
	"scheduler_heartbeat",
	"scheduler_partition",
	// A timer wheel's shard/dispatch bookkeeping (WF-RUN-004 driver code,
	// distinct from workflow_timer, which is the durable promise).
	"timer_wheel_shard",
	"timer_dispatch_log",
	// Retry and poison-node policy the scheduler applies (WF-RUN-006/007).
	"retry_policy",
	"poison_node_quarantine",
	// Admission control and workload limits (WF-RUN-021).
	"workload_limit",
	"admission_control_bucket",
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
	return slices.Contains(set, name)
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
	// Unexpected is GatedTables intersect Present: a scheduler-bookkeeping
	// table that exists when the WF-RUN-000 gate says the scheduler has not
	// been built.
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
