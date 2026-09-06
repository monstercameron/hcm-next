package governance

import (
	"context"
	"sort"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

var GovernanceTables = []string{
	"authority_binding",
	"authority_source",
	"authentication_session",
	"authorization_decision",
	"authorization_policy_snapshot",
	"data_access_manifest",
	"delegation_grant",
	"evidence_artifact",
	"evidence_manifest",
	"governance_decision_bundle",
	"jurisdiction",
	"legal_rule_pack",
	"obligation",
	"obligation_binding",
	"principal",
	"rule_evaluation",
}

// GatedTables names scheduler-side bookkeeping that must stay absent until
// the WF-RUN-000 gate opens. The durable runtime state migration 00026
// materializes (leases, timers, signals, checkpoints, child links, queues,
// claims, SLAs) moved out of this list on 2026-09-05; see
// internal/data/runtimestate for their owner.
var GatedTables = []string{
	"execution_lease",
	"signal_subscription",
	"child_workflow_link",
	"work_item_queue",
	"workflow_queue",
	"sla_policy",
}

func Tables(ctx context.Context, ex dbport.Querier) ([]string, error) {
	rows, err := ex.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'`)
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

func Contains(set []string, name string) bool {
	for _, s := range set {
		if s == name {
			return true
		}
	}
	return false
}

type SchemaInventory struct {
	Present    []string
	Missing    []string
	Unexpected []string
}

func (s SchemaInventory) Exact() bool {
	return len(s.Missing) == 0 && len(s.Unexpected) == 0
}

func Inspect(ctx context.Context, ex dbport.Querier) (SchemaInventory, error) {
	live, err := Tables(ctx, ex)
	if err != nil {
		return SchemaInventory{}, err
	}
	universe := append(append([]string(nil), GovernanceTables...), GatedTables...)
	var inv SchemaInventory
	for _, name := range universe {
		if Contains(live, name) {
			inv.Present = append(inv.Present, name)
		}
	}
	sort.Strings(inv.Present)
	for _, want := range GovernanceTables {
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
