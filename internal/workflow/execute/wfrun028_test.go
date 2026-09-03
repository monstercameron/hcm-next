package execute

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
	"github.com/monstercameron/hcm-next/internal/humanwork"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/prototype"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
	"github.com/monstercameron/hcm-next/internal/workflow/version"
)

// --- WF-RUN-028 fixture -----------------------------------------------------
//
// Every test in this file starts an instance WAITING on prototype's own
// approval->END graph (the same one work006Fixture in
// approval_completion_test.go compiles), with a real, durable,
// COMPLETED WorkItem behind it -- built directly through
// internal/humanwork/workitem.Store, never asserted. [Driver.Resume] is the
// only thing under test: it must reload that row itself and refuse
// [ErrWorkItemDrift] the instant a request disagrees with it.

// wfrun028Fixture bundles one durable, COMPLETED APPROVAL work item parked on
// a WAITING instance, real in Postgres.
type wfrun028Fixture struct {
	db              *pgtest.DB
	conn            *pgxadapter.Conn
	tenantID        uuid.UUID
	instanceID      uuid.UUID
	plan            *workflow.CompiledWorkflow
	proposal        intent.ProposalRevision
	item            workitem.WorkItem
	instanceVersion int64
	at              time.Time
	start           runtime.StartRequest
}

// newWfrun028Fixture builds one tenant, one WAITING instance on prototype's
// APPROVAL->END graph and one durable, COMPLETED approval WorkItem behind it.
// key must be unique per test so tenants and instance ids never collide.
func newWfrun028Fixture(t *testing.T, key string) wfrun028Fixture {
	t.Helper()
	db := pgtest.New(t)
	tenantID := uuid.New()
	instanceID := uuid.New()
	at := time.Date(2026, 9, 3, 15, 0, 0, 0, time.UTC)
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', $4)`,
		tenantID, "wfrun028-"+key+"-"+tenantID.String(), "WF-RUN-028 tenant "+key, at.Add(-time.Hour))
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}

	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatalf("CompileApproval: %v", err)
	}
	intentID, revisionID := "intent:wfrun028:"+key, "proposal:wfrun028:"+key+":1"
	proposal := intent.ProposalRevision{
		IntentID: intentID, ProposalRevisionID: revisionID, Revision: 1,
		MaterialDigest: digest.Reference{
			ProfileID: "PROPOSAL", ProfileVersion: 1,
			SchemaID: "hcmnext.intent.ProposalRevision", SchemaVersion: 1,
			AlgorithmID: "sha256", CanonicalLength: 42,
			Digest:             "sha256:" + strings.Repeat("a", 64),
			ScopeBindingDigest: "sha256:" + strings.Repeat("b", 64),
			IntentID:           &intentID, ProposalRevisionID: &revisionID,
		},
	}

	var item workitem.WorkItem
	var instanceVersion int64
	work006Tx(t, conn, tenantID, func(tx dbport.Tx) error {
		inst, err := runtime.NewInstance(tenantID, instanceID, "cell-local", plan, workflow.ModeExecute,
			"sha256:"+strings.Repeat("1", 64), "correlation:wfrun028:"+key, at.Add(-time.Hour))
		if err != nil {
			return err
		}
		inst.RuntimeStatus = runtime.InstanceWaiting
		inst.StartedAt = work006TimePtr(at.Add(-time.Hour))
		if _, err = (runtime.Store{}).CreateInstance(context.Background(), tx, inst); err != nil {
			return err
		}
		node := runtime.NewNodeExecution(tenantID, instanceID, prototype.NodeApproval, 1, workflow.StepApproval, runtime.NodeWaiting)
		_, instanceVersion, err = (runtime.Store{}).RecordNodeExecution(context.Background(), tx, node, 1)
		if err != nil {
			return err
		}
		item, err = workitem.NewApprovalTask(workitem.NewWorkItemInput{
			TenantID: tenantID, WorkType: "wfrun028.approval", CorrelationID: "correlation:wfrun028:" + key,
			WorkflowInstanceID: instanceID, NodeID: prototype.NodeApproval,
			ProposalRef: proposal.MaterialDigest.Digest, SubjectRefs: []string{"worker:wfrun028-" + key},
			PolicyRouteRef: "route:wfrun028/v1", Visibility: workitem.VisibilityAssigneeOnly,
			OrganizationScopeID: "org:acme/people", DeadlineAt: at.Add(time.Hour), CreatedAt: at.Add(-time.Hour),
		}, prototype.ApprovalRequirementID)
		if err != nil {
			return err
		}
		store := workitem.Store{}
		item, err = store.Create(context.Background(), tx, item, work006Meta("system:workflow", "created", at.Add(-time.Hour)))
		if err != nil {
			return err
		}
		resolution := humanwork.Resolution{
			RequirementID: prototype.ApprovalRequirementID, RequirementRevision: 1,
			Outcome:    humanwork.OutcomeResolved,
			Candidates: []humanwork.Candidate{{PrincipalID: "principal:approver", Via: humanwork.SourceDirect, TermRef: "role:approver"}},
			ResolvedAt: values.NewInstant(at.Add(-time.Hour)), EffectiveAt: values.NewInstant(at.Add(-time.Hour)),
			DirectoryVersion: "directory:1", ExpressionDigest: "sha256:" + strings.Repeat("c", 64),
			QuorumRequired: 1,
		}
		item, err = store.Route(context.Background(), tx, tenantID, item.WorkItemID, item.ItemVersion,
			workitem.Assignment{Resolution: resolution, GovernancePolicyRef: "policy:wfrun028/v1", Trigger: workitem.TriggerInitialRouting, ChosenOwner: "principal:approver"},
			work006Meta("system:workflow", "routed", at.Add(-50*time.Minute)))
		if err != nil {
			return err
		}
		item, err = store.Claim(context.Background(), tx, workitem.ClaimInput{
			TenantID: tenantID, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
			ClaimantPrincipalID: "principal:approver", ClaimExpiresAt: at.Add(time.Hour), Now: at.Add(-10 * time.Minute),
			Meta: work006Meta("principal:approver", "claimed", at.Add(-10*time.Minute)),
		})
		if err != nil {
			return err
		}
		item, err = store.Start(context.Background(), tx, tenantID, item.WorkItemID, item.ItemVersion, at.Add(-5*time.Minute),
			work006Meta("principal:approver", "started", at.Add(-5*time.Minute)))
		if err != nil {
			return err
		}
		item, err = store.Complete(context.Background(), tx, workitem.CompleteInput{
			TenantID: tenantID, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
			CompletedBy: "principal:approver", CompletedOutputDigest: "sha256:" + strings.Repeat("d", 64),
			Now: at, Meta: work006Meta("principal:approver", "completed", at),
		})
		return err
	})

	start := runtime.StartRequest{
		TenantID: tenantID, CellID: "cell-local", StartIdempotencyKey: "start:wfrun028:" + key,
		Resolver: staticResolver{selection: runtime.WorkflowSelection{
			WorkflowID: plan.WorkflowID, Pin: version.Pin{SemanticVersion: "1.0.0"}, Plan: plan,
		}},
		Versions: staticVersions{record: version.CompiledVersion{
			WorkflowID: plan.WorkflowID, SemanticVersion: "1.0.0", CompiledPlanDigest: plan.Digest(), Status: version.StatusActive,
		}},
		Proposal:            runtime.ProposalBinding{Revision: proposal},
		BusinessSubjectRefs: []string{"worker:wfrun028-" + key},
		ExecutionMode:       workflow.ModeExecute, CorrelationID: "correlation:wfrun028:" + key, CreatedAt: at.Add(-time.Hour),
	}

	return wfrun028Fixture{
		db: db, conn: conn, tenantID: tenantID, instanceID: instanceID, plan: plan, proposal: proposal,
		item: item, instanceVersion: instanceVersion, at: at, start: start,
	}
}

func (f wfrun028Fixture) driver(t *testing.T) *Driver {
	t.Helper()
	return f.driverOn(t, f.conn)
}

// appConn opens a fresh connection scoped to this fixture's tenant-app role,
// exactly as the one built in [newWfrun028Fixture].
func (f wfrun028Fixture) appConn(t *testing.T) *pgxadapter.Conn {
	t.Helper()
	conn := f.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// driverOn builds a driver over an explicit connection -- WF-RUN-028_Race
// needs one real, independent connection per concurrent caller, since a
// single pgx connection is not safe for concurrent use (the same reasoning
// TestTodo_WF_RUN_023_Race documents).
func (f wfrun028Fixture) driverOn(t *testing.T, conn *pgxadapter.Conn) *Driver {
	t.Helper()
	d, err := New(Options{
		DB: conn, Steps: work006EndRunner{}, Terminal: work006Terminal{},
		Items: workitem.Store{}, Guard: idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 24 * time.Hour, RetryWindow: time.Hour},
		Clock:     func() time.Time { return f.at.Add(time.Minute) },
	})
	if err != nil {
		t.Fatalf("New driver: %v", err)
	}
	return d
}

// driverWithCurrency builds the same fixture driver with a [CurrencyGuard]
// attached -- WF-RUN-029's tests reuse this fixture's WAITING instance and
// COMPLETED work item, adding only the currency check under test.
func (f wfrun028Fixture) driverWithCurrency(t *testing.T, guard *CurrencyGuard) *Driver {
	t.Helper()
	d, err := New(Options{
		DB: f.conn, Steps: work006EndRunner{}, Terminal: work006Terminal{},
		Items: workitem.Store{}, Guard: idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 24 * time.Hour, RetryWindow: time.Hour},
		Clock:     func() time.Time { return f.at.Add(time.Minute) },
		Currency:  guard,
	})
	if err != nil {
		t.Fatalf("New driver: %v", err)
	}
	return d
}

func (f wfrun028Fixture) resumeRequest() ResumeRequest {
	return ResumeRequest{
		Start: f.start, InstanceID: f.instanceID, ExpectedInstanceVersion: f.instanceVersion,
		WorkItemID: f.item.WorkItemID, ExpectedWorkItemVersion: f.item.ItemVersion,
		Outcome: frontier.NodeOutcome{
			NodeID: prototype.NodeApproval, Outcome: workflow.Outcome("APPROVED"),
			OutputDigest: "sha256:" + strings.Repeat("e", 64),
		},
		RecordedAt: f.at.Add(time.Minute),
	}
}

// --- TestTodo_WF_RUN_028 ----------------------------------------------------

// TestTodo_WF_RUN_028 is the PRIMARY test: Resume loads the persisted
// WorkItem itself and advances only from that stored row.
func TestTodo_WF_RUN_028(t *testing.T) {
	f := newWfrun028Fixture(t, "primary")
	result, err := f.driver(t).Resume(context.Background(), f.resumeRequest())
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if result.Status != StatusComplete {
		t.Fatalf("result = %+v, want COMPLETE", result)
	}
}

// TestTodo_WF_RUN_028_Race resumes the SAME WorkItem/instance concurrently.
// Exactly one caller may advance the instance; every other caller is refused
// a typed error, and the node this Resume advances is recorded exactly once
// -- WF-RUN-028's own load-and-bind never lets two callers apply the same
// completed evidence twice.
func TestTodo_WF_RUN_028_Race(t *testing.T) {
	f := newWfrun028Fixture(t, "race")
	const workers = 6
	type outcome struct {
		status Status
		err    error
	}
	results := make([]outcome, workers)
	// Each worker gets its own connection, created before any goroutine
	// starts: a pgx connection is not safe for concurrent use, and
	// testing.T is not safe to call from a goroutine other than the test's
	// own, so every t-touching call happens before Add/Wait or after it,
	// never inside a worker -- the same shape TestTodo_WF_RUN_023_Race uses.
	drivers := make([]*Driver, workers)
	for i := range drivers {
		drivers[i] = f.driverOn(t, f.appConn(t))
	}
	var start, done sync.WaitGroup
	start.Add(1)
	for i := 0; i < workers; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			res, err := drivers[i].Resume(context.Background(), f.resumeRequest())
			results[i] = outcome{status: res.Status, err: err}
		}(i)
	}
	start.Done()
	done.Wait()

	successes := 0
	for _, r := range results {
		if r.err == nil {
			successes++
			if r.status != StatusComplete {
				t.Fatalf("a successful concurrent resume reported status %s, want COMPLETE", r.status)
			}
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent resumes against the same work item succeeded %d times, want exactly 1", successes)
	}

	// db.Conn bypasses row-level security (it is not scoped to any
	// app.tenant_id), unlike every app-role connection above -- exactly what
	// a cross-tenant count assertion needs here.
	var nodeCount int
	if err := f.db.Conn.QueryRow(context.Background(),
		`SELECT count(*) FROM workflow_node_execution WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3`,
		f.tenantID, f.instanceID, prototype.NodeApproval).Scan(&nodeCount); err != nil {
		t.Fatalf("count node executions: %v", err)
	}
	if nodeCount != 1 {
		t.Fatalf("approval node execution rows = %d, want exactly 1 (no double-advance)", nodeCount)
	}
}

// TestTodo_WF_RUN_028_Fault proves a WorkItem-drift refusal leaves the
// instance completely untouched: a caller whose ExpectedWorkItemVersion
// disagrees with the stored row's is refused, and neither the instance nor
// any node execution advances.
func TestTodo_WF_RUN_028_Fault(t *testing.T) {
	f := newWfrun028Fixture(t, "fault")
	req := f.resumeRequest()
	req.ExpectedWorkItemVersion = f.item.ItemVersion + 1

	_, err := f.driver(t).Resume(context.Background(), req)
	if !errors.Is(err, ErrWorkItemDrift) {
		t.Fatalf("Resume error = %v, want work item drift", err)
	}

	var inst runtime.Instance
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		var loadErr error
		inst, loadErr = (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenantID, f.instanceID)
		return loadErr
	})
	if inst.RuntimeStatus != runtime.InstanceWaiting || inst.InstanceVersion != f.instanceVersion {
		t.Fatalf("instance changed after a refused resume: status=%s version=%d, want WAITING/%d",
			inst.RuntimeStatus, inst.InstanceVersion, f.instanceVersion)
	}
}

// TestTodo_WF_RUN_028_Security proves the WorkItemReader is tenant-scoped:
// a Resume whose Start.TenantID names a different tenant than the one the
// WorkItem and instance actually belong to is refused, never silently
// re-scoped or leaked across tenants.
func TestTodo_WF_RUN_028_Security(t *testing.T) {
	f := newWfrun028Fixture(t, "security")
	otherTenant := uuid.New()
	f.db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'other tenant', 'ACTIVE', $3)`,
		otherTenant, "wfrun028-security-other-"+otherTenant.String(), f.at.Add(-time.Hour))

	req := f.resumeRequest()
	req.Start.TenantID = otherTenant
	if _, err := f.driver(t).Resume(context.Background(), req); err == nil {
		t.Fatal("expected a refusal resuming under a tenant that does not own the work item")
	}
}

// TestTodo_WF_RUN_028_Mutation proves every binding checkWorkItemDrift claims
// to check actually gates the call: a request whose BusinessSubjectRefs no
// longer match the stored WorkItem's own subjects is refused, not silently
// accepted because the id and version alone matched.
func TestTodo_WF_RUN_028_Mutation(t *testing.T) {
	f := newWfrun028Fixture(t, "mutation")
	req := f.resumeRequest()
	req.Start.BusinessSubjectRefs = []string{"worker:someone-else-entirely"}
	if _, err := f.driver(t).Resume(context.Background(), req); !errors.Is(err, ErrWorkItemDrift) {
		t.Fatalf("Resume error = %v, want work item drift on subject mismatch", err)
	}
}
