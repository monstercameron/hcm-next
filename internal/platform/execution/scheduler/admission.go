package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/runtimestate"
	"github.com/monstercameron/hcm-next/internal/workflow/lease"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// admissibleStatuses is the declared admission rule: the instance runtime
// statuses whose ready work a scheduler replica may publish.
//
// It is a positive list rather than a list of exclusions on purpose. An
// exclusion list silently admits every status added later, and the statuses
// most costly to admit by accident are exactly the ones an operator adds --
// PAUSED and QUARANTINED are governed interventions, and dispatching work for
// a paused or quarantined instance would be the scheduler overriding a human
// decision. PAUSE_REQUESTED is excluded for the same reason: a pause that has
// been asked for and not yet applied is not an invitation to start more work.
// BLOCKED, REPAIR_REQUIRED, CANCELLING and every terminal status are excluded
// because none of them is a state an advancement may proceed from.
var admissibleStatuses = []string{
	string(runtime.InstanceCreated),
	string(runtime.InstanceRunning),
	string(runtime.InstanceWaiting),
}

// AdmissibleStatuses returns the declared admission rule as a fresh slice, so
// a caller (or a test) can read it without being able to widen it.
func AdmissibleStatuses() []string {
	return append([]string(nil), admissibleStatuses...)
}

// Admissible reports whether an instance in this runtime status may have its
// ready work published.
func Admissible(status string) bool {
	for _, known := range admissibleStatuses {
		if status == known {
			return true
		}
	}
	return false
}

// readyColumns is the ready-work projection both statements below select, in
// the order [scanReadyWork] reads it.
const readyColumns = `rw.tenant_id, rw.ready_work_id, rw.instance_id, rw.node_id, rw.attempt,
		rw.ready_state, rw.priority, rw.eligible_at, rw.ready_version, rw.enqueued_at`

// readyOrder is the declared publication order suffix, and it is total:
// priority first (the admission/priority rule ready work carries), then
// eligibility, then arrival, then the row's own identity as the final
// tiebreak. Because it is total, two replicas reading the same instant see the
// same sequence, which is what makes publication deterministic rather than
// merely concurrent-safe.
const readyOrder = `rw.priority ASC, rw.eligible_at ASC, rw.enqueued_at ASC, rw.ready_work_id ASC`

// eligibleOrder places live EXECUTE work ahead of REPLAY work before applying
// the ordinary priority order. The instance mode is durable, while ready-work
// priority is the caller's criticality signal; the two dimensions must not be
// conflated because a replay carrying an accidentally high priority still
// cannot pre-empt a live pilot execution.
const eligibleOrder = `ORDER BY CASE WHEN wi.execution_mode = 'REPLAY' THEN 1 ELSE 0 END ASC, ` + readyOrder

// selectEligible reads the ready work this replica may claim.
//
// FOR UPDATE OF rw takes a row lock on the ready-work rows only (never on the
// joined instance), and SKIP LOCKED makes a second replica scanning at the
// same instant step over whatever this one already holds instead of blocking
// on it. The lock lives and dies with the caller's transaction, which is the
// same transaction the claim is written in.
const selectEligible = `
	SELECT ` + readyColumns + `, wi.execution_mode
	FROM workflow_ready_work rw
	JOIN workflow_instance wi
	  ON wi.tenant_id = rw.tenant_id AND wi.instance_id = rw.instance_id
	WHERE rw.tenant_id = $1
	  AND rw.ready_state = $2
	  AND rw.eligible_at <= $3
	  AND wi.runtime_status = ANY ($4)
	` + eligibleOrder + `
	LIMIT $5
	FOR UPDATE OF rw SKIP LOCKED`

// selectAbandoned reads DISPATCHED ready work so the caller can check whether
// the replica that claimed it still holds the instance lease. It carries no
// admission join: a row already claimed has to be returned to the pool whatever
// the instance's status now is, or it would be stranded forever.
const selectAbandoned = `
	SELECT ` + readyColumns + `
	FROM workflow_ready_work rw
	WHERE rw.tenant_id = $1
	  AND rw.ready_state = $2
	ORDER BY ` + readyOrder + `
	LIMIT $3
	FOR UPDATE OF rw SKIP LOCKED`

// scanReadyWork reads one ready-work row in [readyColumns] order. The
// identifiers land straight in the store's own struct fields, which is how
// this package handles them without ever naming their type.
func scanReadyWork(scan func(dest ...any) error) (runtimestate.ReadyWork, error) {
	var (
		out     runtimestate.ReadyWork
		version int64
	)
	if err := scan(&out.TenantID, &out.ReadyWorkID, &out.InstanceID, &out.NodeID, &out.Attempt,
		&out.State, &out.Priority, &out.EligibleAt, &version, &out.EnqueuedAt); err != nil {
		return runtimestate.ReadyWork{}, err
	}
	out.Version = uint64(version)
	out.EligibleAt = out.EligibleAt.UTC()
	out.EnqueuedAt = out.EnqueuedAt.UTC()
	return out, nil
}

type eligibleWork struct {
	row  runtimestate.ReadyWork
	mode string
}

func scanEligibleWork(scan func(dest ...any) error) (eligibleWork, error) {
	row, err := scanReadyWorkWithMode(scan)
	if err != nil {
		return eligibleWork{}, err
	}
	return row, nil
}

func scanReadyWorkWithMode(scan func(dest ...any) error) (eligibleWork, error) {
	var (
		out     runtimestate.ReadyWork
		version int64
		mode    string
	)
	if err := scan(&out.TenantID, &out.ReadyWorkID, &out.InstanceID, &out.NodeID, &out.Attempt,
		&out.State, &out.Priority, &out.EligibleAt, &version, &out.EnqueuedAt, &mode); err != nil {
		return eligibleWork{}, err
	}
	out.Version = uint64(version)
	out.EligibleAt = out.EligibleAt.UTC()
	out.EnqueuedAt = out.EnqueuedAt.UTC()
	return eligibleWork{row: out, mode: mode}, nil
}

// eligible returns the admissible ready work of one tenant that is due at now,
// bounded by limit, row-locked in the caller's transaction.
func eligible(ctx context.Context, ex runtimestate.Executor, claim lease.AcquireRequest,
	now time.Time, limit int,
) ([]eligibleWork, error) {
	rows, err := ex.Query(ctx, selectEligible,
		claim.TenantID, runtimestate.ReadyReady, now.UTC(), admissibleStatuses, limit)
	if err != nil {
		return nil, fmt.Errorf("scheduler: select eligible ready work: %w", err)
	}
	defer rows.Close()
	var out []eligibleWork
	for rows.Next() {
		row, err := scanEligibleWork(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scheduler: read eligible ready work row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scheduler: iterate eligible ready work: %w", err)
	}
	return out, nil
}

// abandoned returns the tenant's DISPATCHED ready work, row-locked in the
// caller's transaction.
func abandoned(ctx context.Context, ex runtimestate.Executor, claim lease.AcquireRequest,
	limit int,
) ([]runtimestate.ReadyWork, error) {
	rows, err := ex.Query(ctx, selectAbandoned, claim.TenantID, runtimestate.ReadyDispatched, limit)
	if err != nil {
		return nil, fmt.Errorf("scheduler: select dispatched ready work: %w", err)
	}
	return collectReadyWork(rows)
}

func collectReadyWork(rows dbport.Rows) ([]runtimestate.ReadyWork, error) {
	defer rows.Close()
	var out []runtimestate.ReadyWork
	for rows.Next() {
		row, err := scanReadyWork(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scheduler: read ready work row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scheduler: iterate ready work: %w", err)
	}
	return out, nil
}

// instanceResource names the lease a claim takes: the workflow instance the
// ready work belongs to. Leasing the instance rather than the ready-work row
// is deliberate -- two units of work for one instance must not be advanced
// concurrently, because they would race on the same instance version.
func instanceResource(row runtimestate.ReadyWork) lease.Resource {
	return lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: row.InstanceID.String()}
}
