package app

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// TestWorkItemQueueReaderRefusesWhenTheReaderIsNotConfigured mirrors the
// inspector's pin: an unconfigured reader refuses before touching any
// database.
func TestWorkItemQueueReaderRefusesWhenTheReaderIsNotConfigured(t *testing.T) {
	always := func(values.TenantId) uuid.UUID { return uuid.New() }
	cases := map[string]WorkItemQueueReader{
		"no database and no tenant map": NewWorkItemQueueReader(nil, nil),
		"no database":                   NewWorkItemQueueReader(nil, always),
		"no tenant map":                 NewWorkItemQueueReader(fakeUnconfiguredBeginner{}, nil),
	}
	for name, reader := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := reader.ListWorkItemQueue(t.Context(), values.TenantId("tenant-x"), "user-x", time.Now()); !errors.Is(err, ErrWorkItemQueueReaderUnconfigured) {
				t.Fatalf("ListWorkItemQueue = %v, want ErrWorkItemQueueReaderUnconfigured", err)
			}
			if _, err := reader.ReadWorkItem(t.Context(), values.TenantId("tenant-x"), uuid.New()); !errors.Is(err, ErrWorkItemQueueReaderUnconfigured) {
				t.Fatalf("ReadWorkItem = %v, want ErrWorkItemQueueReaderUnconfigured", err)
			}
		})
	}
}

// TestTodo_EP_WORK_001_Reader proves the port over the least-privilege app
// connection: the queue lists the calling principal's live items only, the
// detail read answers the store's own not-found refusal for absent and
// other-tenant ids, and a second tenant's queue never bleeds through.
func TestTodo_EP_WORK_001_Reader(t *testing.T) {
	db := pgtest.New(t)
	tenantA := wfInspectorTenant(t, db, "work-queue-a")
	tenantB := wfInspectorTenant(t, db, "work-queue-b")
	conn := wfInspectorConn(t, db)
	ctx := t.Context()
	now := time.Date(2026, 9, 11, 11, 0, 0, 0, time.UTC)

	instanceID := uuid.New()
	db.Exec(t, `
		INSERT INTO workflow_instance (
			tenant_id, instance_id, cell_id, workflow_id, workflow_version,
			compiled_plan_hash, execution_mode, runtime_status, input_ref,
			current_node_ids, correlation_id, created_at)
		VALUES ($1, $2, 'cell-local', 'wf.inspector-test', 1,
			'`+wfInspectorHex64("a")+`', '`+string(workflow.ModeSimulate)+`', 'RUNNING',
			'input-snapshot-ref', ARRAY['approval_node'], $4, $3)`,
		tenantA, instanceID, now, "corr-"+instanceID.String())

	store := workitem.Store{}
	meta := workitem.TransitionMeta{ActorPrincipalID: "principal:seed", Reason: "seed", At: now}
	seed := func(tenant, instance uuid.UUID, owner string) workitem.WorkItem {
		item, err := workitem.NewWorkItem(workitem.NewWorkItemInput{
			TenantID: tenant, Kind: workitem.KindTask, WorkType: "worktype.reader/v1",
			WorkflowInstanceID: instance, CorrelationID: "corr-" + instance.String(),
			NodeID:         "approval_node",
			SubjectRefs:    []string{"worker:jane"},
			PolicyRouteRef: "route.reader/v1", Visibility: workitem.VisibilityAssigneeOnly,
			OrganizationScopeID: "org-reader", DeadlineAt: now.Add(24 * time.Hour), CreatedAt: now,
		})
		if err != nil {
			t.Fatalf("NewWorkItem: %v", err)
		}
		res := humanwork.Resolution{
			RequirementID: "req.reader/v1", Outcome: humanwork.OutcomeResolved,
			Candidates: []humanwork.Candidate{{PrincipalID: owner, Via: humanwork.SourceDirect, TermRef: "term.reader"}},
			ResolvedAt: values.NewInstant(now), EffectiveAt: values.NewInstant(now),
			DirectoryVersion: "directory.reader/1", QuorumRequired: 1,
		}
		wfInspectorTx(t, conn, tenant, func(tx dbport.Tx) error {
			stored, err := store.Create(ctx, tx, item, meta)
			if err != nil {
				return err
			}
			_, err = store.Route(ctx, tx, tenant, stored.WorkItemID, stored.ItemVersion,
				workitem.Assignment{Resolution: res}, meta)
			return err
		})
		return item
	}
	instanceB := uuid.New()
	db.Exec(t, `
		INSERT INTO workflow_instance (
			tenant_id, instance_id, cell_id, workflow_id, workflow_version,
			compiled_plan_hash, execution_mode, runtime_status, input_ref,
			current_node_ids, correlation_id, created_at)
		VALUES ($1, $2, 'cell-local', 'wf.inspector-test', 1,
			'`+wfInspectorHex64("a")+`', '`+string(workflow.ModeSimulate)+`', 'RUNNING',
			'input-snapshot-ref', ARRAY['approval_node'], $4, $3)`,
		tenantB, instanceB, now, "corr-"+instanceB.String())

	mine := seed(tenantA, instanceID, "user-amy")
	other := seed(tenantA, instanceID, "user-bob")
	foreign := seed(tenantB, instanceB, "user-amy") // same principal, other tenant

	tenants := map[values.TenantId]uuid.UUID{
		values.TenantId("queue-a"): tenantA,
		values.TenantId("queue-b"): tenantB,
	}
	reader := NewWorkItemQueueReader(conn, func(key values.TenantId) uuid.UUID { return tenants[key] })

	items, err := reader.ListWorkItemQueue(ctx, values.TenantId("queue-a"), "user-amy", now)
	if err != nil {
		t.Fatalf("ListWorkItemQueue: %v", err)
	}
	if len(items) != 1 || items[0].WorkItemID != mine.WorkItemID {
		t.Fatalf("queue-a amy items = %+v, want only %s", items, mine.WorkItemID)
	}
	items, err = reader.ListWorkItemQueue(ctx, values.TenantId("queue-b"), "user-amy", now)
	if err != nil {
		t.Fatalf("ListWorkItemQueue tenant-b: %v", err)
	}
	if len(items) != 1 || items[0].WorkItemID != foreign.WorkItemID {
		t.Fatalf("queue-b amy items = %+v, want only her tenant-b item", items)
	}

	got, err := reader.ReadWorkItem(ctx, values.TenantId("queue-a"), mine.WorkItemID)
	if err != nil {
		t.Fatalf("ReadWorkItem: %v", err)
	}
	if got.WorkItemID != mine.WorkItemID || got.OwnerRef != "user-amy" {
		t.Fatalf("ReadWorkItem = %+v, want %s owned by user-amy", got, mine.WorkItemID)
	}
	for _, id := range []uuid.UUID{uuid.New(), other.WorkItemID, uuid.New()} {
		if _, err := reader.ReadWorkItem(ctx, values.TenantId("queue-b"), id); workitem.CodeOf(err) != workitem.CodeWorkItemNotFound {
			t.Fatalf("ReadWorkItem tenant-b %s = %v, want not-found refusal", id, err)
		}
	}
	if _, err := reader.ReadWorkItem(ctx, values.TenantId("queue-a"), uuid.New()); workitem.CodeOf(err) != workitem.CodeWorkItemNotFound {
		t.Fatalf("ReadWorkItem absent = %v, want not-found refusal", err)
	}
}
