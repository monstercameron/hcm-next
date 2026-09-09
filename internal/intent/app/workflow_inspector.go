package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// The durable execution record of one workflow instance, as an application
// port.
//
// Two surfaces read the same four tables to answer "what happened to this
// run": the operator's AdminService.GetWorkflowInstance (ADMIN-008) and the
// Promotion journey's Inspect. Both used to open the stores themselves, and
// for the operator surface that meant internal/transport importing the data
// port directly, which package-dependency-policy.yaml forbids
// (transport-must-not-import-store: transport adapts protocol, it does not
// read tables). This file is where that read now lives: one loader over the
// runtime and work-item stores, one port a transport is handed, and the
// journey engine's own read reuses the loader so the two surfaces cannot
// drift in what they consider the record to be.

// WorkflowInstanceRecord is everything durable one workflow instance has:
// the instance row, its node executions, its work items and each item's
// transitions. It carries the stores' own types unprojected; rendering and
// redaction are the caller's (internal/workflow/inspect for the operator
// surface, the journey's own projection for the page).
type WorkflowInstanceRecord struct {
	Instance  runtime.Instance
	Nodes     []runtime.NodeExecution
	WorkItems []workitem.WorkItem
	// Transitions is keyed by the work item id's string form, in the order
	// the store returns them.
	Transitions map[string][]workitem.TransitionRecord
}

// WorkflowInstanceReader loads one instance's durable record for a caller
// acting in one tenant.
//
// An instance that does not exist, or is not this tenant's, is reported with
// the runtime store's own [runtime.CodeInstanceNotFound] refusal (readable
// through runtime.CodeOf), so a transport can project NOT_FOUND without this
// package inventing a second sentinel for the same fact.
type WorkflowInstanceReader interface {
	ReadWorkflowInstance(ctx context.Context, tenant values.TenantId, instanceID uuid.UUID) (WorkflowInstanceRecord, error)
}

// workflowInstanceReader implements [WorkflowInstanceReader] over a pool.
type workflowInstanceReader struct {
	db         dbport.Beginner
	tenantUUID func(values.TenantId) uuid.UUID
}

// NewWorkflowInstanceReader builds the reader a composition root hands the
// operator surface. db is the pool the workflow runtime writes through
// (internal/data/pgxadapter.Pool in every real composition) and tenantUUID
// the same tenant-key-to-uuid mapping [CellConfig.TenantUUID] carries.
// Either nil yields a reader that refuses every read, which is what an
// unconfigured port should do rather than answering from nowhere.
func NewWorkflowInstanceReader(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID) WorkflowInstanceReader {
	return workflowInstanceReader{db: db, tenantUUID: tenantUUID}
}

// ErrWorkflowInstanceReaderUnconfigured reports a reader built with no
// database or no tenant mapping.
var ErrWorkflowInstanceReaderUnconfigured = errors.New("app: the workflow instance reader is not configured")

// ReadWorkflowInstance implements [WorkflowInstanceReader].
//
// The read runs inside one tenant-scoped transaction that is always rolled
// back: every table it touches is row-level-security protected, so the tenant
// has to be established on the session (internal/data/tenancy.WithTenant)
// before the first SELECT, and a read that committed would be claiming to
// have changed something.
func (r workflowInstanceReader) ReadWorkflowInstance(
	ctx context.Context, tenant values.TenantId, instanceID uuid.UUID,
) (WorkflowInstanceRecord, error) {
	if r.db == nil || r.tenantUUID == nil {
		return WorkflowInstanceRecord{}, ErrWorkflowInstanceReaderUnconfigured
	}
	tenantID := r.tenantUUID(tenant)
	if tenantID == uuid.Nil {
		return WorkflowInstanceRecord{}, fmt.Errorf("app: workflow instance read: tenant %q maps to no id", tenant)
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return WorkflowInstanceRecord{}, fmt.Errorf("app: workflow instance read: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return WorkflowInstanceRecord{}, fmt.Errorf("app: workflow instance read: scope tenant: %w", err)
	}
	return loadWorkflowInstanceRecord(ctx, tx, tenantID, instanceID)
}

// loadWorkflowInstanceRecord performs the four reads over an executor the
// caller has already scoped to the tenant. Store refusals are wrapped, never
// replaced, so runtime.CodeOf still reads them.
func loadWorkflowInstanceRecord(
	ctx context.Context, ex workitem.Executor, tenantID, instanceID uuid.UUID,
) (WorkflowInstanceRecord, error) {
	runtimeStore, itemStore := runtime.Store{}, workitem.Store{}

	instance, err := runtimeStore.LoadInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return WorkflowInstanceRecord{}, fmt.Errorf("load the workflow instance: %w", err)
	}
	nodes, err := runtimeStore.LoadNodeExecutions(ctx, ex, tenantID, instanceID)
	if err != nil {
		return WorkflowInstanceRecord{}, fmt.Errorf("load the node executions: %w", err)
	}
	items, err := itemStore.ListForInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return WorkflowInstanceRecord{}, fmt.Errorf("list the work items: %w", err)
	}
	transitions := make(map[string][]workitem.TransitionRecord, len(items))
	for _, item := range items {
		rows, err := itemStore.LoadTransitions(ctx, ex, tenantID, item.WorkItemID)
		if err != nil {
			return WorkflowInstanceRecord{}, fmt.Errorf("load work item transitions: %w", err)
		}
		transitions[item.WorkItemID.String()] = rows
	}
	return WorkflowInstanceRecord{
		Instance: instance, Nodes: nodes, WorkItems: items, Transitions: transitions,
	}, nil
}
