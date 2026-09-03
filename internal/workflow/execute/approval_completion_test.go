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
	intentapproval "github.com/monstercameron/hcm-next/internal/intent/approval"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/prototype"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
	stepapproval "github.com/monstercameron/hcm-next/internal/workflow/steps/approval"
	"github.com/monstercameron/hcm-next/internal/workflow/version"
)

type work006Authority struct {
	mu      sync.Mutex
	allowed bool
	reason  string
	calls   int
	tx      workitem.Executor
}

func (a *work006Authority) Recheck(_ context.Context, tx workitem.Executor, in CurrentApprovalAuthorityRequest) (CurrentApprovalAuthorityDecision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	a.tx = tx
	return CurrentApprovalAuthorityDecision{Allowed: a.allowed, DecisionRef: in.Decision.AuthorityDecisionRef, Reason: a.reason}, nil
}

func (a *work006Authority) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

type work006EndRunner struct{}

func (work006EndRunner) Run(_ context.Context, req StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if req.Node.Type != workflow.StepEnd {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.New("unexpected non-END successor")
	}
	return frontier.NodeOutcome{
		NodeID: req.Node.ID, Outcome: workflow.OutcomeSucceeded,
		OutputDigest: strings.Repeat("e", 64),
	}, runtime.GovernanceRefs{}, nil
}

type work006Terminal struct{}

func (work006Terminal) Write(context.Context, dbport.Tx, TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	return idempotency.ResultIdentity{ResultRef: "result:promotion:complete"}, nil
}

type work006Fixture struct {
	db              *pgtest.DB
	conn            *pgxadapter.Conn
	tenantID        uuid.UUID
	instanceID      uuid.UUID
	plan            *workflow.CompiledWorkflow
	proposal        intent.ProposalRevision
	requirement     humanwork.ApprovalRequirement
	item            workitem.WorkItem
	continuation    stepapproval.Continuation
	decision        intentapproval.ApprovalDecision
	instanceVersion int64
	at              time.Time
}

func newWork006Fixture(t *testing.T) work006Fixture {
	t.Helper()
	db := pgtest.New(t)
	tenantID := uuid.New()
	instanceID := uuid.New()
	at := time.Date(2026, 9, 3, 15, 0, 0, 0, time.UTC)
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'WORK-006 tenant', 'ACTIVE', $3)`,
		tenantID, "work-006-"+tenantID.String(), at.Add(-time.Hour))
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("SET ROLE: %v", err)
	}
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatalf("CompileApproval: %v", err)
	}

	intentID, revisionID := "intent:promotion:work-006", "proposal:promotion:work-006:1"
	proposal := intent.ProposalRevision{
		IntentID: intentID, ProposalRevisionID: revisionID, Revision: 1,
		MaterialDigest: digest.Reference{
			ProfileID: "PROPOSAL", ProfileVersion: 1,
			SchemaID: "hcmnext.intent.ProposalRevision", SchemaVersion: 1,
			AlgorithmID: "sha256", CanonicalLength: 42,
			Digest: strings.Repeat("a", 64), ScopeBindingDigest: strings.Repeat("b", 64),
			IntentID: &intentID, ProposalRevisionID: &revisionID,
		},
	}
	requirement := humanwork.ApprovalRequirement{
		RequirementID: prototype.ApprovalRequirementID, Revision: 1,
		Quorum: humanwork.Quorum{MinApprovals: 1, RequireDistinctPrincipals: true},
		Deadline: humanwork.Deadline{
			DecideBy: values.NewInstant(at.Add(time.Hour)), Expiry: values.NewInstant(at.Add(2 * time.Hour)),
		},
		ExpressionDigest: "sha256:" + strings.Repeat("c", 64),
		Source: humanwork.RequirementSource{
			TableID: "promotion.threshold", TableVersion: "1",
			TableDigest: "sha256:" + strings.Repeat("d", 64), MatchedRowID: "manager",
			GovernancePolicyRef: "policy:promotion-approval/v1",
		},
	}

	var item workitem.WorkItem
	var instanceVersion int64
	work006Tx(t, conn, tenantID, func(tx dbport.Tx) error {
		inst, err := runtime.NewInstance(tenantID, instanceID, "cell-local", plan, workflow.ModeExecute,
			"sha256:"+strings.Repeat("1", 64), "correlation:work-006", at.Add(-time.Hour))
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
			TenantID: tenantID, WorkType: "promotion.approval", CorrelationID: "correlation:work-006",
			WorkflowInstanceID: instanceID, NodeID: prototype.NodeApproval,
			ProposalRef: proposal.MaterialDigest.Digest, SubjectRefs: []string{"worker:jane"},
			PolicyRouteRef: "route:manager/v1", Visibility: workitem.VisibilityAssigneeOnly,
			OrganizationScopeID: "org:acme/people", DeadlineAt: at.Add(time.Hour), CreatedAt: at.Add(-time.Hour),
		}, requirement.RequirementID)
		if err != nil {
			return err
		}
		store := workitem.Store{}
		item, err = store.Create(context.Background(), tx, item, work006Meta("system:workflow", "created", at.Add(-time.Hour)))
		if err != nil {
			return err
		}
		resolution := humanwork.Resolution{
			RequirementID: requirement.RequirementID, RequirementRevision: requirement.Revision,
			Outcome:    humanwork.OutcomeResolved,
			Candidates: []humanwork.Candidate{{PrincipalID: "principal:manager", Via: humanwork.SourceDirect, TermRef: "role:manager"}},
			ResolvedAt: values.NewInstant(at.Add(-time.Hour)), EffectiveAt: values.NewInstant(at.Add(-time.Hour)),
			DirectoryVersion: "directory:1", ExpressionDigest: requirement.ExpressionDigest,
			RequirementDigest: requirement.Digest(), QuorumRequired: 1,
		}
		item, err = store.Route(context.Background(), tx, tenantID, item.WorkItemID, item.ItemVersion,
			workitem.Assignment{Resolution: resolution, GovernancePolicyRef: requirement.Source.GovernancePolicyRef, Trigger: workitem.TriggerInitialRouting},
			work006Meta("system:workflow", "routed", at.Add(-50*time.Minute)))
		if err != nil {
			return err
		}
		item, err = store.Claim(context.Background(), tx, workitem.ClaimInput{
			TenantID: tenantID, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
			ClaimantPrincipalID: "principal:manager", ClaimExpiresAt: at.Add(time.Hour), Now: at.Add(-10 * time.Minute),
			Meta: work006Meta("principal:manager", "claimed", at.Add(-10*time.Minute)),
		})
		if err != nil {
			return err
		}
		item, err = store.Start(context.Background(), tx, tenantID, item.WorkItemID, item.ItemVersion, at.Add(-5*time.Minute),
			work006Meta("principal:manager", "started", at.Add(-5*time.Minute)))
		return err
	})

	continuation, err := stepapproval.NewContinuation(instanceID, prototype.NodeApproval, proposal,
		humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{requirement}}, []workitem.WorkItem{item})
	if err != nil {
		t.Fatalf("NewContinuation: %v", err)
	}
	decision := intentapproval.ApprovalDecision{
		DecisionID: "decision:work-006:approved",
		Binding: intentapproval.DecisionBinding{
			RequirementID: requirement.RequirementID, RequirementRevision: requirement.Revision,
			IntentID: proposal.IntentID, ProposalRevisionID: proposal.ProposalRevisionID,
			ProposalDigest: proposal.MaterialDigest, TaskVersion: 1,
			RenderedProjectionDigest: "sha256:" + strings.Repeat("2", 64),
			RequirementDigest:        requirement.Digest(), ResolutionExpressionDigest: requirement.ExpressionDigest,
		},
		Outcome: intentapproval.OutcomeApproved,
		Approver: intentapproval.ApproverReference{
			PrincipalID: "principal:manager", IdentityAssuranceRef: "assurance:mfa:1",
			SessionRef: "session:active:1", Via: humanwork.SourceDirect,
		},
		AuthorityDecisionRef: "authz:current:work-006", Reason: "reason:promotion-approved",
		DecidedAt: values.NewInstant(at), VoteDigest: "sha256:" + strings.Repeat("3", 64),
	}
	return work006Fixture{
		db: db, conn: conn, tenantID: tenantID, instanceID: instanceID, plan: plan, proposal: proposal,
		requirement: requirement, item: item, continuation: continuation, decision: decision,
		instanceVersion: instanceVersion, at: at,
	}
}

func (f work006Fixture) request(authority CurrentApprovalAuthority) ApprovalCompletionRequest {
	selection := runtime.WorkflowSelection{
		WorkflowID: f.plan.WorkflowID, Pin: version.Pin{SemanticVersion: "1.0.0"}, Plan: f.plan,
	}
	record := version.CompiledVersion{
		WorkflowID: f.plan.WorkflowID, SemanticVersion: "1.0.0", CompiledPlanDigest: f.plan.Digest(), Status: version.StatusActive,
	}
	return ApprovalCompletionRequest{
		Start: runtime.StartRequest{
			TenantID: f.tenantID, CellID: "cell-local", StartIdempotencyKey: "start:work-006",
			Resolver: staticResolver{selection: selection}, Versions: staticVersions{record: record},
			Proposal:            runtime.ProposalBinding{Revision: f.proposal, Approved: true, ApprovalRef: "approval:proposal:1"},
			BusinessSubjectRefs: []string{"worker:jane"}, ExecutionMode: workflow.ModeExecute,
			CorrelationID: "correlation:work-006",
		},
		InstanceID: f.instanceID, ExpectedInstanceVersion: f.instanceVersion,
		WorkItemID: f.item.WorkItemID, ExpectedWorkItemVersion: f.item.ItemVersion,
		Continuation: f.continuation, Decision: f.decision, RecordedAt: f.at,
		Meta: work006Meta("principal:manager", "approved", f.at), Authority: authority,
	}
}

func (f work006Fixture) driver(t *testing.T, advance AdvanceFunc) *Driver {
	return f.driverOn(t, f.conn, advance)
}

func (f work006Fixture) driverOn(t *testing.T, conn *pgxadapter.Conn, advance AdvanceFunc) *Driver {
	t.Helper()
	d, err := New(Options{
		DB: conn, Steps: work006EndRunner{}, Advance: advance,
		Terminal: work006Terminal{}, Guard: idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 24 * time.Hour, RetryWindow: time.Hour},
		Clock:     func() time.Time { return f.at.Add(time.Minute) },
	})
	if err != nil {
		t.Fatalf("New driver: %v", err)
	}
	return d
}

func TestTodo_WORK_006(t *testing.T) {
	f := newWork006Fixture(t)
	authority := &work006Authority{allowed: true}
	result, err := f.driver(t, nil).CompleteApproval(context.Background(), f.request(authority))
	if err != nil {
		t.Fatalf("CompleteApproval: %v", err)
	}
	if result.Status != StatusComplete || result.Resolution.Outcome != "APPROVED" || result.Replay {
		t.Fatalf("result = %+v", result)
	}
	if result.CompletedItem.Status != workitem.StatusCompleted || result.CompletedItem.CompletedOutputDigest != f.decision.Digest() {
		t.Fatalf("completed item = %+v", result.CompletedItem)
	}
	if authority.callCount() != 1 || result.AuthorityRef != f.decision.AuthorityDecisionRef {
		t.Fatalf("authority calls=%d result ref=%q", authority.callCount(), result.AuthorityRef)
	}
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		node, err := (runtime.Store{}).LoadNodeExecution(context.Background(), tx, f.tenantID, f.instanceID, prototype.NodeApproval, 1)
		if err != nil {
			return err
		}
		if node.Status != runtime.NodeSucceeded || node.OutputArtifactRef != result.Resolution.Digest ||
			node.Refs.AuthorizationDecisionID != f.decision.AuthorityDecisionRef {
			t.Fatalf("approval node = %+v", node)
		}
		return nil
	})

	replayed, err := f.driver(t, nil).CompleteApproval(context.Background(), f.request(authority))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replayed.Replay || replayed.CompletedItem.WorkItemID != result.CompletedItem.WorkItemID || authority.callCount() != 1 {
		t.Fatalf("replay=%+v authority calls=%d", replayed, authority.callCount())
	}
}

func TestTodo_WORK_006_Race(t *testing.T) {
	f := newWork006Fixture(t)
	authority := &work006Authority{allowed: true}
	secondConn := f.db.NewConn(t)
	if _, err := secondConn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("SET ROLE second connection: %v", err)
	}
	drivers := []*Driver{f.driverOn(t, f.conn, nil), f.driverOn(t, secondConn, nil)}
	req := f.request(authority)
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, driver := range drivers {
		go func(driver *Driver) {
			<-start
			_, err := driver.CompleteApproval(context.Background(), req)
			errs <- err
		}(driver)
	}
	close(start)
	first, second := <-errs, <-errs
	if first != nil && second != nil {
		t.Fatalf("both racing completions failed: %v; %v", first, second)
	}
	var transitions int
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM work_item_transition WHERE tenant_id=$1 AND work_item_id=$2 AND to_status='COMPLETED'`,
			f.tenantID, f.item.WorkItemID).Scan(&transitions)
	})
	if transitions != 1 {
		t.Fatalf("COMPLETED transitions = %d, want 1", transitions)
	}
}

func TestTodo_WORK_006_Security(t *testing.T) {
	f := newWork006Fixture(t)
	authority := &work006Authority{allowed: false, reason: "session revoked or SoD changed"}
	_, err := f.driver(t, nil).CompleteApproval(context.Background(), f.request(authority))
	if !errors.Is(err, ErrApprovalAuthorityDenied) {
		t.Fatalf("error = %v, want authority denial", err)
	}
	work006AssertUnchanged(t, f)
}

func TestTodo_WORK_006_Mutation(t *testing.T) {
	t.Run("stale WorkItem version", func(t *testing.T) {
		f := newWork006Fixture(t)
		authority := &work006Authority{allowed: true}
		req := f.request(authority)
		req.ExpectedWorkItemVersion--
		_, err := f.driver(t, nil).CompleteApproval(context.Background(), req)
		if !errors.Is(err, ErrApprovalCompletionConflict) || authority.callCount() != 0 {
			t.Fatalf("error=%v authority calls=%d", err, authority.callCount())
		}
		work006AssertUnchanged(t, f)
	})

	t.Run("stale proposal", func(t *testing.T) {
		f := newWork006Fixture(t)
		authority := &work006Authority{allowed: true}
		req := f.request(authority)
		req.Start.Proposal.Revision.MaterialDigest.Digest = strings.Repeat("f", 64)
		_, err := f.driver(t, nil).CompleteApproval(context.Background(), req)
		if !errors.Is(err, ErrApprovalCompletionConflict) || authority.callCount() != 0 {
			t.Fatalf("error=%v authority calls=%d", err, authority.callCount())
		}
		work006AssertUnchanged(t, f)
	})

	t.Run("missing decision-time evidence", func(t *testing.T) {
		f := newWork006Fixture(t)
		authority := &work006Authority{allowed: true}
		req := f.request(authority)
		req.Decision.DecidedAt = values.Instant{}
		_, err := f.driver(t, nil).CompleteApproval(context.Background(), req)
		if !errors.Is(err, stepapproval.ErrInvalidEvidence) {
			t.Fatalf("error = %v, want invalid evidence", err)
		}
		work006AssertUnchanged(t, f)
	})

	t.Run("advance failure rolls completion back", func(t *testing.T) {
		f := newWork006Fixture(t)
		authority := &work006Authority{allowed: true}
		boom := errors.New("advance fault")
		_, err := f.driver(t, func(context.Context, runtime.Executor, runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
			return runtime.AdvanceReceipt{}, boom
		}).CompleteApproval(context.Background(), f.request(authority))
		if !errors.Is(err, boom) {
			t.Fatalf("error = %v, want injected fault", err)
		}
		work006AssertUnchanged(t, f)
	})

	t.Run("conflicting replay", func(t *testing.T) {
		f := newWork006Fixture(t)
		authority := &work006Authority{allowed: true}
		driver := f.driver(t, nil)
		if _, err := driver.CompleteApproval(context.Background(), f.request(authority)); err != nil {
			t.Fatalf("first completion: %v", err)
		}
		req := f.request(authority)
		req.Decision.Outcome = intentapproval.OutcomeRejected
		if _, err := driver.CompleteApproval(context.Background(), req); !errors.Is(err, ErrApprovalCompletionConflict) {
			t.Fatalf("conflicting replay error = %v", err)
		}
	})
}

func work006AssertUnchanged(t *testing.T, f work006Fixture) {
	t.Helper()
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		item, err := (workitem.Store{}).Load(context.Background(), tx, f.tenantID, f.item.WorkItemID)
		if err != nil {
			return err
		}
		if item.Status != workitem.StatusInProgress || item.ItemVersion != f.item.ItemVersion {
			t.Fatalf("item changed: %+v", item)
		}
		node, err := (runtime.Store{}).LoadNodeExecution(context.Background(), tx, f.tenantID, f.instanceID, prototype.NodeApproval, 1)
		if err != nil {
			return err
		}
		if node.Status != runtime.NodeWaiting {
			t.Fatalf("node status = %s, want WAITING", node.Status)
		}
		return nil
	})
}

func work006Tx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("tenant: %v", err)
	}
	if err := fn(tx); err != nil {
		t.Fatalf("transaction body: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func work006Meta(actor, reason string, at time.Time) workitem.TransitionMeta {
	return workitem.TransitionMeta{ActorPrincipalID: actor, Reason: reason, At: at}
}

func work006TimePtr(at time.Time) *time.Time { return &at }
