package cell

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/promotion"
	"github.com/monstercameron/hcm-next/internal/humanwork"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/intent/app"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/execute"
	"github.com/monstercameron/hcm-next/internal/workflow/execute/effects"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/prototype"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
	"github.com/monstercameron/hcm-next/internal/workflow/version"
)

// defaultRetention is the caller-driven driver's idempotency retention when
// PromotionExecutionConfig.Retention is the zero value.
var defaultRetention = idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour}

// defaultApproverPrincipalID is who the composed promotion approval workflow
// routes its one approval WorkItem to, when the composition root names no
// approver of its own. Relationship-based resolution (ManagerOf(worker),
// HRBPFor(worker.organization)) is a later P1B contract
// (planning/specs/workflow-runtime.md "Resolvers may target ..."); this
// release names one fixed operator instead, exactly like
// internal/workflow/prototype's own conformance fixture does.
const defaultApproverPrincipalID = "principal:promotion-approver"

// defaultRequiredRole is the principal role ExecuteIntent requires under a
// PromotionExecution's ExecutionAuthority, when the composition root names
// no role of its own.
const defaultRequiredRole = "promotion_operator"

// PromotionExecutionConfig configures [NewPromotionExecution]. DB and
// Terminal are required; every other field defaults to a bounded, named
// value.
type PromotionExecutionConfig struct {
	// DB opens the transactions Start and each Advance run inside.
	DB execute.Beginner
	// Terminal performs the one governed business write the workflow's END
	// node raises. This package never implements one itself — it is a port,
	// supplied by the composition root
	// (internal/workflow/execute/effects.LedgerTerminalWriter in production,
	// a recording fake in a test).
	Terminal execute.TerminalWriter
	// Guard is the idempotency store TX-006 requires. Nil means
	// idempotency.PostgresStore{}.
	Guard idempotency.Store
	// Retention is the idempotency retention policy. The zero value means
	// [defaultRetention].
	Retention idempotency.RetentionPolicy
	// Clock supplies the recording time. Nil means time.Now in UTC.
	Clock func() time.Time
	// CellID names the cell runtime.StartRequest.CellID records. Empty means
	// "cell-local".
	CellID string
	// ApproverPrincipalID is who the one approval WorkItem this workflow
	// raises is routed to. Empty means [defaultApproverPrincipalID].
	ApproverPrincipalID string
	// AuthorityDigest names the signed P1B authority amendment this
	// composition asserts. Carried through as evidence; never verified here.
	AuthorityDigest string
	// RequiredRole is the principal role ExecuteIntent additionally requires
	// under the returned ExecutionAuthority. Empty means [defaultRequiredRole].
	RequiredRole string
}

// PromotionExecution is the composed EXECUTE-mode wiring for
// internal/workflow/prototype's bounded, executable promote_worker approval
// graph, shaped to plug directly into app.CellConfig's Executor,
// ExecutionResolver, ExecutionVersions and ExecutionAuthority fields.
type PromotionExecution struct {
	Executor  app.ProposalExecutor
	Resolver  runtime.WorkflowResolver
	Versions  version.Store
	Authority *app.ExecutionAuthority
}

// NewPromotionExecution composes the caller-driven driver
// (internal/workflow/execute.Driver) for internal/workflow/prototype's
// bounded promote_worker approval graph: a data-driven resolver bound to the
// one compiled and activated plan, a step runner that parks on APPROVAL and
// completes on END, a work-item factory backed by the real
// internal/humanwork/workitem store, and cfg.Terminal for the END node's
// governed business write.
//
// This is deliberately the smallest executable shape next-steps.md's P1B
// item 1 names (promote_worker in EXECUTE mode): it performs no capability
// invocation and no domain mutation of its own. The workflow parks for one
// human approval and, once resumed with an APPROVED outcome, records the
// promotion outcome as a governed ledger fact through cfg.Terminal.
func NewPromotionExecution(cfg PromotionExecutionConfig) (*PromotionExecution, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("transport cell: promotion execution needs a database Beginner")
	}
	if cfg.Terminal == nil {
		return nil, fmt.Errorf("transport cell: promotion execution needs a TerminalWriter")
	}
	guard := cfg.Guard
	if guard == nil {
		guard = idempotency.PostgresStore{}
	}
	retention := cfg.Retention
	if retention == (idempotency.RetentionPolicy{}) {
		retention = defaultRetention
	}
	clock := cfg.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	approver := cfg.ApproverPrincipalID
	if approver == "" {
		approver = defaultApproverPrincipalID
	}
	role := cfg.RequiredRole
	if role == "" {
		role = defaultRequiredRole
	}

	plan, err := prototype.CompileApproval()
	if err != nil {
		return nil, fmt.Errorf("transport cell: compile the promotion approval workflow: %w", err)
	}
	versions := version.NewRegistry()
	at := clock()
	published, err := version.Publish(versions, prototype.ApprovalDefinition(), plan,
		workflow.Options{Phase: workflow.PhaseP1B}, version.PublishMeta{
			SemanticVersion: "1.0.0", PublishedAt: at, PublishedBy: "cmd/hcmnext:execution-authority",
		})
	if err != nil {
		return nil, fmt.Errorf("transport cell: publish the promotion approval workflow: %w", err)
	}
	if _, err := version.Activate(versions, published.CompiledPlanDigest, version.ActivationEvidence{
		Authorized: true, ApprovedBy: "cmd/hcmnext:execution-authority",
		Authority: "authority:execution-authority-flag", ApprovedAt: at,
		ReviewedPlanDigest: published.CompiledPlanDigest, TestsPassed: true,
	}); err != nil {
		return nil, fmt.Errorf("transport cell: activate the promotion approval workflow: %w", err)
	}

	resolver := effects.PolicyResolver{Entries: []effects.PolicyEntry{{
		WorkflowID: plan.WorkflowID,
		Pin:        version.Pin{CompiledPlanDigest: plan.Digest()},
		Plan:       plan,
	}}}

	driver, err := execute.New(execute.Options{
		DB:        cfg.DB,
		Steps:     promotionStepRunner{},
		WorkItems: promotionWorkItems{approver: approver},
		Terminal:  cfg.Terminal,
		Guard:     guard,
		Retention: retention,
		Clock:     clock,
	})
	if err != nil {
		return nil, fmt.Errorf("transport cell: build the promotion execution driver: %w", err)
	}

	return &PromotionExecution{
		Executor: executeDriverAdapter{driver: driver},
		Resolver: resolver,
		Versions: versions,
		Authority: &app.ExecutionAuthority{
			AuthorityDigest:     cfg.AuthorityDigest,
			AdmittedIntentTypes: map[string]bool{promotion.IntentType: true},
			RequiredRole:        role,
		},
	}, nil
}

// promotionStepRunner runs internal/workflow/prototype's two node types: it
// parks on APPROVAL and returns a bare outcome on END. It invokes no
// capability and performs no business mutation itself — the driver's own
// continuation sink is what runs the composed TerminalWriter at END.
type promotionStepRunner struct{}

var _ execute.StepRunner = promotionStepRunner{}

func (promotionStepRunner) Run(_ context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	switch req.Node.Type {
	case workflow.StepApproval:
		return frontier.NodeOutcome{
			NodeID: req.Node.ID, Await: frontier.AwaitWorkItem,
			AwaitRef: prototype.ApprovalRequirementID,
		}, runtime.GovernanceRefs{}, nil
	case workflow.StepEnd:
		return frontier.NodeOutcome{NodeID: req.Node.ID}, runtime.GovernanceRefs{}, nil
	default:
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{},
			fmt.Errorf("transport cell: promotion execution has no step for %s", req.Node.Type)
	}
}

// promotionWorkItems creates and routes the one approval WorkItem the bounded
// promotion graph raises, to a single fixed approver principal.
type promotionWorkItems struct{ approver string }

var _ execute.WorkItemFactory = promotionWorkItems{}

func (f promotionWorkItems) CreateAndRoute(ctx context.Context, ex workitem.Executor, req execute.WorkItemRequest) (workitem.WorkItem, error) {
	item, err := workitem.NewApprovalTask(workitem.NewWorkItemInput{
		TenantID: req.Continuation.TenantID, WorkItemID: req.WorkItemID,
		WorkType: prototype.ApprovalRequirementID, CorrelationID: req.CorrelationID,
		WorkflowInstanceID: req.Continuation.InstanceID, NodeID: req.Continuation.TargetNodeID,
		ProposalRef: req.Proposal.Revision.MaterialDigest.Digest, SubjectRefs: req.SubjectRefs,
		PolicyRouteRef: "route.promotion.execution-authority/v1", Visibility: workitem.VisibilityAssigneeOnly,
		OrganizationScopeID: req.Proposal.Revision.OrganizationScopeID,
		DeadlineAt:          req.CreatedAt.Add(48 * time.Hour), CreatedAt: req.CreatedAt,
	}, prototype.ApprovalRequirementID)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	store := workitem.Store{}
	meta := workitem.TransitionMeta{
		ActorPrincipalID: "system:promotion-execution", Reason: "execution_authority.work_item.created", At: req.CreatedAt,
	}
	created, err := store.Create(ctx, ex, item, meta)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	resolution := humanwork.Resolution{
		RequirementID: prototype.ApprovalRequirementID, RequirementRevision: 1,
		Outcome: humanwork.OutcomeResolved,
		Candidates: []humanwork.Candidate{
			{PrincipalID: f.approver, Via: humanwork.SourceDirect, TermRef: "term:execution-authority-approver"},
		},
		ResolvedAt: values.NewInstant(req.CreatedAt), EffectiveAt: values.NewInstant(req.CreatedAt),
		DirectoryVersion:  "directory.execution-authority/1",
		ExpressionDigest:  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RequirementDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		QuorumRequired:    1,
	}
	assignment := workitem.Assignment{
		Resolution: resolution, GovernancePolicyRef: "governance.execution-authority/1",
		Trigger: workitem.TriggerInitialRouting, ChosenOwner: f.approver,
	}
	return store.Route(ctx, ex, created.TenantID, created.WorkItemID, created.ItemVersion, assignment,
		workitem.TransitionMeta{
			ActorPrincipalID: "system:promotion-execution", Reason: "execution_authority.work_item.routed", At: req.CreatedAt,
		})
}

// executeDriverAdapter adapts *execute.Driver to app.ProposalExecutor. It is
// the one place this package's own request/result shapes and
// internal/intent/app's port-owned shapes convert into one another, so
// internal/intent/app never needs to import internal/workflow/execute
// itself.
type executeDriverAdapter struct{ driver *execute.Driver }

var _ app.ProposalExecutor = executeDriverAdapter{}

func (a executeDriverAdapter) Execute(ctx context.Context, start runtime.StartRequest) (app.ExecutionResult, error) {
	result, err := a.driver.Execute(ctx, execute.ExecuteRequest{Start: start})
	if err != nil {
		return app.ExecutionResult{}, err
	}
	return adaptExecutionResult(result, result.Start.InstanceID.String()), nil
}

func (a executeDriverAdapter) Resume(ctx context.Context, req app.ExecutionResumeRequest) (app.ExecutionResult, error) {
	result, err := a.driver.Resume(ctx, execute.ResumeRequest{
		Start: req.Start, InstanceID: req.InstanceID, ExpectedInstanceVersion: req.ExpectedInstanceVersion,
		WorkItem: req.WorkItem, Outcome: req.Outcome,
	})
	if err != nil {
		return app.ExecutionResult{}, err
	}
	return adaptExecutionResult(result, req.InstanceID.String()), nil
}

// adaptExecutionResult projects one execute.Result onto app.ExecutionResult.
// instanceID is passed as its already-rendered string form (rather than the
// package.google/uuid.UUID type itself, which internal/transport/cell must
// not import directly - LIB-002/003) because execute.Result.Start (which
// itself carries an instance id) is only populated by Execute, never by
// Resume.
func adaptExecutionResult(result execute.Result, instanceID string) app.ExecutionResult {
	visited := make([]string, 0, len(result.Advances))
	for _, adv := range result.Advances {
		visited = append(visited, adv.NodeID)
	}
	parked := make([]string, 0, len(result.WorkItems))
	for _, item := range result.WorkItems {
		parked = append(parked, item.WorkType+":"+item.WorkItemID.String())
	}
	return app.ExecutionResult{
		Parked:              result.Status == execute.StatusParked,
		InstanceID:          instanceID,
		InstanceVersion:     result.InstanceVersion,
		VisitedNodes:        visited,
		ParkedContinuations: parked,
	}
}
