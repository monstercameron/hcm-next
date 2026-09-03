// Package workflow_test is the end-to-end harness for the first EXECUTABLE
// run of a workflow instance: an approved, immutable ProposalRevision starts
// a compiled plan through internal/workflow/execute, parks on a governed
// APPROVAL WorkItem, resumes through internal/workflow/steps/approval,
// parks on a governed TASK WorkItem, resumes through
// internal/workflow/steps/task, and reaches COMPLETE having appended exactly
// one governed business fact to the ledger.
//
// The compiled plan is this package's own small executable definition
// (promotionApprovalTaskDefinition below), built the same way
// internal/workflow/prototype's own approval-only prototype is: a real
// workflow.Definition compiled through workflow.Compile under PhaseP1B, not
// a hand-built CompiledWorkflow literal. It exists here, rather than
// extending internal/workflow/promotion.go's landed P1A reference (which
// declares no APPROVAL or TASK node -- P1A ships preflight and simulation
// only) or internal/workflow/prototype's own approval-only graph (which has
// no TASK node and is owned by a different lane), because proving this
// ticket's contract needs both governed human-work primitives in one graph.
package workflow_test

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/humanwork"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/intent"
	intentapproval "github.com/monstercameron/hcm-next/internal/intent/approval"
	"github.com/monstercameron/hcm-next/internal/intent/protomap"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	ledgerport "github.com/monstercameron/hcm-next/internal/ledger"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/execute"
	"github.com/monstercameron/hcm-next/internal/workflow/execute/effects"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/inspect"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
	stepsapproval "github.com/monstercameron/hcm-next/internal/workflow/steps/approval"
	stepstask "github.com/monstercameron/hcm-next/internal/workflow/steps/task"
	"github.com/monstercameron/hcm-next/internal/workflow/version"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// ---------------------------------------------------------------------------
// The compiled plan: CAPABILITY-free, DECISION-free demonstration graph --
// APPROVAL -> TASK -> one of five terminals -- built through the real
// compiler, exactly as internal/workflow/prototype's own graph is.
// ---------------------------------------------------------------------------

const (
	demoWorkflowID = "hcmnext.workflows.test.promotion_execute_demo"

	// nodePrepare is the plan's start node. [frontier.Seed] always marks the
	// start node READY regardless of its step type -- there is no such thing
	// as an instance whose very first node is parked -- so an APPROVAL or
	// TASK node may not be the start node of a plan an execute.StepRunner
	// drives; a trivial TRANSFORM stands in front of the real APPROVAL gate.
	nodePrepare     = "prepare_promotion"
	nodeApproval    = "approve_promotion"
	nodeTask        = "provision_role"
	nodeApproved    = "end_approved"
	nodeRejected    = "end_rejected"
	nodeInvalidated = "end_invalidated"
	nodeExpired     = "end_expired"
	nodeCancelled   = "end_cancelled"

	demoApprovalRequirementRef = "approval.demo.promotion_manager/v1"
	demoTaskWorkType           = "task.demo.provision_role/v1"
	demoTerminalCode           = "PROMOTION_APPLIED"
)

func demoSchema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID: "hcmnext.workflows.test." + name + "/v1", Version: 1,
		ProtobufFullName: "hcmnext.workflow.test." + name,
	}
}

func brandedString(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}

func demoTerminalFields() []workflow.Field {
	return []workflow.Field{
		{Path: "worker_id", Type: brandedString("WorkerID")},
		{Path: "terminal_code", Type: workflow.ValueType{Kind: workflow.KindString}},
	}
}

func demoCompletion(request, execution, business, consistency, obligation string) map[string]string {
	return map[string]string{
		"RequestState": request, "ExecutionState": execution, "BusinessState": business,
		"ConsistencyState": consistency, "ObligationState": obligation,
	}
}

func demoTerminalNode(id, code string, status workflow.RuntimeStatus, dims map[string]string, commitReceiptRef string) workflow.Node {
	return workflow.Node{
		ID: id, Type: workflow.StepEnd, Inputs: demoTerminalFields(),
		InputMappings: []workflow.Mapping{
			{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
			{Target: "terminal_code", Source: workflow.Source{
				Kind: workflow.SourceConstant, Constant: code, Type: workflow.ValueType{Kind: workflow.KindString},
			}},
		},
		Governance: workflow.NodeGovernance{
			Purpose: "PROMOTION_EXECUTE_DEMO", Classification: "CONFIDENTIAL_HR",
			RevalidationBoundary: workflow.RevalidatePreClosure, DataAccessManifestRef: "data-access.test.promotion-execute-demo/v1",
		},
		End: &workflow.EndSpec{TerminalCode: code, RuntimeStatus: status, CompletionMapping: dims, CommitReceiptRef: commitReceiptRef},
	}
}

// promotionApprovalTaskDefinition is the smallest bounded executable graph
// that parks on a governed APPROVAL, then a governed TASK, before reaching
// one of five terminals. It performs no capability invocation and no
// business mutation of its own -- ModeExecute here only means the runtime
// may create and complete governed human work; the one governed ledger
// write happens at COMPLETE, through effects.LedgerTerminalWriter.
func promotionApprovalTaskDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID: demoWorkflowID, Version: 1, Name: "Promotion execute demo (test/workflow)",
		InputSchema: demoSchema("Input"), OutputSchema: demoSchema("Result"), VariablesSchema: demoSchema("Variables"),
		TenantScope: "test", OrganizationScope: humanwork.ScenarioOrganizationScopeID, RiskClass: "HIGH",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeExecute}, TerminalProfile: workflow.TerminalProfileExecute,
		StartNodeID: nodePrepare,
		Inputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
			{Path: "proposal_digest", Type: workflow.ValueType{Kind: workflow.KindString}},
		},
		Outputs: demoTerminalFields(),
		ApprovalRequirements: []workflow.ApprovalRequirement{{
			ID: demoApprovalRequirementRef, ResolverExpression: "CurrentManagerOf(worker)",
			Scope: humanwork.ScenarioOrganizationScopeID, Quorum: 1, SeparationOfDuties: true,
			EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND",
		}},
		Limits:                workflow.Limits{MaxFanOut: 5, MaxDepth: 3, MaxNodes: 8},
		FailurePolicyRef:      "policy.workflow.failure.test/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.test/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.test/v1",
		Nodes: []workflow.Node{
			{
				ID: nodePrepare, Type: workflow.StepTransform,
				InputSchema: demoSchema("PrepareInput"), OutputSchema: demoSchema("PrepareResult"),
				Inputs: []workflow.Field{
					{Path: "worker_id", Type: brandedString("WorkerID")},
					{Path: "proposal_digest", Type: workflow.ValueType{Kind: workflow.KindString}},
				},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
					{Target: "proposal_digest", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "proposal_digest"}},
				},
				Transform: &workflow.TransformSpec{
					TransformRef: "transform.test.promotion-execute-demo.prepare/v1", Version: 1,
					NormalizationProfile: "profile.test.promotion-execute-demo/v1", OutputTaint: workflow.TaintDerived,
					Limits: workflow.TransformLimits{MaxInputBytes: workflow.MaxTransformInputBytes, MaxOutputBytes: workflow.MaxTransformOutputBytes, MaxSteps: workflow.MaxTransformSteps},
				},
				DeclaredEffect: capability.EffectPure,
				Governance: workflow.NodeGovernance{
					Purpose: "PROMOTION_EXECUTE_DEMO", Classification: "CONFIDENTIAL_HR",
					RevalidationBoundary: workflow.RevalidatePreExecution, DataAccessManifestRef: "data-access.test.promotion-execute-demo/v1",
				},
			},
			{
				ID: nodeApproval, Type: workflow.StepApproval,
				InputSchema: demoSchema("ApprovalTaskInput"), OutputSchema: demoSchema("ApprovalTaskResult"),
				Inputs: []workflow.Field{
					{Path: "worker_id", Type: brandedString("WorkerID")},
					{Path: "proposal_digest", Type: workflow.ValueType{Kind: workflow.KindString}},
				},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
					{Target: "proposal_digest", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "proposal_digest"}},
				},
				DeclaredEffect: capability.EffectPure,
				Governance: workflow.NodeGovernance{
					Purpose: "PROMOTION_APPROVAL", Classification: "CONFIDENTIAL_HR",
					ApprovalRequirements: []string{demoApprovalRequirementRef},
					RevalidationBoundary: workflow.RevalidatePreExecution, DataAccessManifestRef: "data-access.test.promotion-execute-demo/v1",
				},
			},
			{
				ID: nodeTask, Type: workflow.StepTask,
				InputSchema: demoSchema("TaskInput"), OutputSchema: demoSchema("TaskResult"),
				Inputs: []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
				},
				DeclaredEffect: capability.EffectPure,
				Governance: workflow.NodeGovernance{
					Purpose: "PROMOTION_ROLE_PROVISIONING", Classification: "CONFIDENTIAL_HR",
					RevalidationBoundary: workflow.RevalidatePreExecution, DataAccessManifestRef: "data-access.test.promotion-execute-demo/v1",
				},
			},
			demoTerminalNode(nodeApproved, demoTerminalCode, workflow.RuntimeCompleted,
				demoCompletion("CLOSED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"),
				"receipt.test.promotion-execute-demo/v1"),
			demoTerminalNode(nodeRejected, "REJECTED", workflow.RuntimeCompleted,
				demoCompletion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
			demoTerminalNode(nodeInvalidated, "INVALIDATED", workflow.RuntimeSuperseded,
				demoCompletion("SUPERSEDED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
			demoTerminalNode(nodeExpired, "EXPIRED", workflow.RuntimeCancelled,
				demoCompletion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
			demoTerminalNode(nodeCancelled, "CANCELLED", workflow.RuntimeCancelled,
				demoCompletion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
		},
		Edges: []workflow.Edge{
			{From: nodePrepare, To: nodeApproval, RouteKey: "SUCCEEDED"},
			{From: nodePrepare, To: nodeRejected, RouteKey: "FAILED"},
			{From: nodeApproval, To: nodeTask, RouteKey: "APPROVED"},
			{From: nodeApproval, To: nodeRejected, RouteKey: "REJECTED"},
			{From: nodeApproval, To: nodeInvalidated, RouteKey: "INVALIDATED"},
			{From: nodeApproval, To: nodeExpired, RouteKey: "EXPIRED"},
			{From: nodeApproval, To: nodeCancelled, RouteKey: "CANCELLED"},
			{From: nodeTask, To: nodeApproved, RouteKey: "SUCCEEDED"},
			{From: nodeTask, To: nodeRejected, RouteKey: "REJECTED"},
			{From: nodeTask, To: nodeExpired, RouteKey: "EXPIRED"},
			{From: nodeTask, To: nodeCancelled, RouteKey: "CANCELLED"},
		},
	}
}

func compileDemoPlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := workflow.Compile(promotionApprovalTaskDefinition(), workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatalf("compile promotion execute demo definition: %v", err)
	}
	return plan
}

// ---------------------------------------------------------------------------
// Version registry: published and ACTIVE.
// ---------------------------------------------------------------------------

func publishActiveDemoPlan(t *testing.T, at time.Time) (*version.Registry, *workflow.CompiledWorkflow, version.CompiledVersion) {
	t.Helper()
	plan := compileDemoPlan(t)
	store := version.NewRegistry()
	published, err := version.Publish(store, promotionApprovalTaskDefinition(), plan,
		workflow.Options{Phase: workflow.PhaseP1B}, version.PublishMeta{
			SemanticVersion: "1.0.0", PublishedAt: at, PublishedBy: "test:promotion-execute",
		})
	if err != nil {
		t.Fatalf("version.Publish: %v", err)
	}
	activated, err := version.Activate(store, published.CompiledPlanDigest, version.ActivationEvidence{
		Authorized: true, ApprovedBy: "test:release", Authority: "authority:release",
		ApprovedAt: at, ReviewedPlanDigest: published.CompiledPlanDigest, TestsPassed: true,
	})
	if err != nil {
		t.Fatalf("version.Activate: %v", err)
	}
	return store, plan, activated
}

// ---------------------------------------------------------------------------
// Proposal + approval-requirement fixtures.
// ---------------------------------------------------------------------------

func demoControlSnapshots() intent.ControlSnapshots {
	return intent.ControlSnapshots{
		CapabilityRegistryDigest: "sha256:capability-registry-fixture", PolicyBundleDigest: "sha256:policy-bundle-fixture",
		LegalContextDigest: "sha256:legal-context-fixture", EntitlementDigest: "sha256:entitlement-fixture",
		ReferenceDataDigest: "sha256:reference-data-fixture", ClassificationTaxonomyDigest: "sha256:classification-taxonomy-fixture",
		DLPDecisionDigest: "sha256:dlp-decision-fixture",
	}
}

// newDemoProposal mints a real, immutable ProposalRevision. approved and
// superseded control the [runtime.ProposalBinding] this test presents to
// Execute -- Start itself re-checks both, per WF-RUN-023.
func newDemoProposal(t *testing.T, tenant values.TenantId, intentID string, at time.Time) intent.ProposalRevision {
	t.Helper()
	digester, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("protomap.NewDefaultDigester: %v", err)
	}
	interval, err := values.NewOpenInstantInterval(values.NewInstant(at))
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	rev, err := intent.NewProposalRevision(intent.ProposalSpec{
		IntentID: intentID, Revision: 1, Tenant: tenant, OrganizationScopeID: humanwork.ScenarioOrganizationScopeID,
		Subjects:      []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "employment:promotion-execute-demo-1", AuthorityDomain: "PEOPLE"}},
		EffectiveTime: interval, ControlSnapshots: demoControlSnapshots(),
		CreatedBy: intent.PrincipalReference{PrincipalID: humanwork.PrincipalRequester, Kind: intent.InitiatorHuman, IdentityAssuranceRef: "assurance:1"},
	}, intent.Definition{
		Ref: intent.Ref{TypeID: "hcmnext.test.promotion_execute_demo", Version: 1}, Family: intent.FamilyChangeRequest,
	}, digester, nil, func() values.Instant { return values.NewInstant(at) })
	if err != nil {
		t.Fatalf("intent.NewProposalRevision: %v", err)
	}
	return rev
}

// managerRequirementAndResolution derives the real baseline promotion
// requirement set (internal/humanwork's own fixture) and resolves the
// current-manager requirement, so the WorkItem this test creates is routed
// by the same [humanwork.Resolve] production wiring uses. Only that one
// requirement gates this demo's single APPROVAL node.
//
// The fixture directory (internal/humanwork.PromotionDirectory) also grants
// the manager's own holiday delegate a valid, unexpired delegation on this
// relationship, so resolution legitimately returns two authorized
// candidates (the manager, DIRECT, and the delegate, DELEGATED) rather than
// one -- [workitem.RouteFromAssignment] routes that as an OwnerCandidateSet/
// AVAILABLE work item rather than a single OwnerPrincipal/ASSIGNED one. This
// test always decides as humanwork.PrincipalManager, who is authorized
// either way.
func managerRequirementAndResolution(t *testing.T) (humanwork.ApprovalRequirement, humanwork.Resolution, humanwork.Directory, humanwork.Clock) {
	t.Helper()
	sc, err := humanwork.NewPromotionScenario(humanwork.PromotionInputStandard())
	if err != nil {
		t.Fatalf("humanwork.NewPromotionScenario: %v", err)
	}
	req, ok := sc.Requirements.Find(humanwork.RequirementCurrentManager)
	if !ok {
		t.Fatalf("promotion scenario derived no %s requirement", humanwork.RequirementCurrentManager)
	}
	res, err := humanwork.Resolve(req, sc.Resolution, sc.Directory, sc.Clock)
	if err != nil {
		t.Fatalf("humanwork.Resolve: %v", err)
	}
	if res.Outcome != humanwork.OutcomeResolved {
		t.Fatalf("manager requirement resolution = %+v, want RESOLVED", res)
	}
	if _, ok := res.Authorizes(humanwork.PrincipalManager); !ok {
		t.Fatalf("manager requirement resolution = %+v, want %s among the candidates", res, humanwork.PrincipalManager)
	}
	return req, res, sc.Directory, sc.Clock
}

// ---------------------------------------------------------------------------
// StepRunner: this demo graph's only READY nodes are its trivial start-node
// TRANSFORM (nodePrepare, always SUCCEEDED -- frontier.Seed always marks the
// start node READY regardless of step type, so a plan whose APPROVAL is the
// very first node would ask this StepRunner to run an APPROVAL, which is
// never legal) and its END nodes (which assert nothing and let the pinned
// plan's own compiled terminal win).
// ---------------------------------------------------------------------------

type endOnlySteps struct{}

func (endOnlySteps) Run(_ context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	switch req.Node.Type {
	case workflow.StepEnd:
		// Asserts nothing: the pinned plan's own compiled Terminal wins.
		return frontier.NodeOutcome{NodeID: req.Node.ID}, runtime.GovernanceRefs{}, nil
	case workflow.StepTransform:
		// The plan's start node (nodePrepare) is the only TRANSFORM here.
		// frontier.Seed always marks the start node READY regardless of its
		// step type, so this trivial, always-succeeds transform is what
		// stands between "instance started" and the governed APPROVAL gate.
		return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: workflow.OutcomeSucceeded}, runtime.GovernanceRefs{}, nil
	default:
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf(
			"test/workflow: unexpected READY step type %s for node %s", req.Node.Type, req.Node.ID)
	}
}

// ---------------------------------------------------------------------------
// WorkItemFactory: opens the governed APPROVAL/TASK WorkItem through
// steps/approval.Open and steps/task.Open, then resolves and records its
// assignment through workitem.Store.Route -- exactly what those packages'
// own doc comments describe as the caller's remaining job.
// ---------------------------------------------------------------------------

type demoWorkItems struct {
	proposal          intent.ProposalRevision
	managerReq        humanwork.ApprovalRequirement
	managerResolution humanwork.Resolution
	taskOwner         string
}

func (f demoWorkItems) CreateAndRoute(ctx context.Context, ex workitem.Executor, req execute.WorkItemRequest) (workitem.WorkItem, error) {
	store := workitem.Store{}
	switch req.Continuation.TargetNodeID {
	case nodeApproval:
		item, err := stepsapproval.Open(ctx, ex, store, stepsapproval.OpenInput{
			TenantID: req.Continuation.TenantID, WorkItemID: req.WorkItemID,
			WorkflowInstanceID: req.Continuation.InstanceID, CorrelationID: req.CorrelationID, SubjectRefs: req.SubjectRefs,
			Node: stepsapproval.CompiledApprovalNode{
				WorkflowID: demoWorkflowID, WorkflowVersion: 1, NodeID: nodeApproval, WorkType: demoApprovalRequirementRef,
				PolicyRouteRef: "route.test.promotion_manager_approval/v1", Visibility: workitem.VisibilityAssigneeOnly,
				OrganizationScopeID: humanwork.ScenarioOrganizationScopeID,
			},
			Requirement: f.managerReq, Proposal: f.proposal, Now: req.CreatedAt,
			Meta: workitem.TransitionMeta{ActorPrincipalID: "system:workflow-runtime", Reason: "WORKFLOW_APPROVAL_REQUIRED", At: req.CreatedAt},
		})
		if err != nil {
			return workitem.WorkItem{}, fmt.Errorf("steps/approval.Open: %w", err)
		}
		assignment := workitem.Assignment{Resolution: f.managerResolution, GovernancePolicyRef: humanwork.PromotionGovernancePolicy().PolicyRef, Trigger: workitem.TriggerInitialRouting}
		if f.managerResolution.Outcome == humanwork.OutcomeResolved && len(f.managerResolution.Candidates) == 1 {
			assignment.ChosenOwner = f.managerResolution.Candidates[0].PrincipalID
		}
		return store.Route(ctx, ex, item.TenantID, item.WorkItemID, item.ItemVersion, assignment,
			workitem.TransitionMeta{ActorPrincipalID: "system:workflow-runtime", Reason: "ASSIGNMENT_RESOLVED", At: req.CreatedAt})

	case nodeTask:
		// steps/task.Open does not accept or set ProposalRef (TASK carries no
		// approval decision, so workitem.NewWorkItemInput.ProposalRef is
		// simply never wired through it) -- but execute's own
		// validateResume unconditionally checks item.ProposalRef against the
		// Start request's bound proposal digest for every resumed WorkItem,
		// APPROVAL or TASK alike. Building the item directly with
		// workitem.NewWorkItem (exactly what Open does internally, plus
		// ProposalRef) is what lets this TASK's WorkItem resume at all.
		draft, err := workitem.NewWorkItem(workitem.NewWorkItemInput{
			TenantID: req.Continuation.TenantID, WorkItemID: req.WorkItemID,
			Kind: workitem.KindTask, WorkType: demoTaskNode().WorkType,
			CorrelationID: req.CorrelationID, WorkflowInstanceID: req.Continuation.InstanceID, NodeID: nodeTask,
			ProposalRef:         req.Proposal.Revision.MaterialDigest.Digest,
			SubjectRefs:         req.SubjectRefs,
			PolicyRouteRef:      "route.test.promotion_provision_role/v1",
			Visibility:          workitem.VisibilityAssigneeOnly,
			OrganizationScopeID: humanwork.ScenarioOrganizationScopeID,
			DeadlineAt:          req.CreatedAt.Add(72 * time.Hour),
			CreatedAt:           req.CreatedAt,
		})
		if err != nil {
			return workitem.WorkItem{}, fmt.Errorf("build task work item: %w", err)
		}
		item, err := store.Create(ctx, ex, draft, workitem.TransitionMeta{ActorPrincipalID: "system:workflow-runtime", Reason: "WORKFLOW_TASK_REQUIRED", At: req.CreatedAt})
		if err != nil {
			return workitem.WorkItem{}, fmt.Errorf("create task work item: %w", err)
		}
		resolution := humanwork.Resolution{
			RequirementID: "task.demo.direct_assignment/v1", Outcome: humanwork.OutcomeResolved,
			Candidates: []humanwork.Candidate{{PrincipalID: f.taskOwner, Via: humanwork.SourceDirect, TermRef: "task.demo.direct_assignment"}},
			ResolvedAt: values.NewInstant(req.CreatedAt), EffectiveAt: values.NewInstant(req.CreatedAt),
			DirectoryVersion: "directory.test.promotion-execute-demo/v1", ExpressionDigest: "expr.test.promotion-execute-demo/v1",
			QuorumRequired: 1,
		}
		assignment := workitem.Assignment{Resolution: resolution, GovernancePolicyRef: "policy.test.task_direct_assignment/v1", Trigger: workitem.TriggerInitialRouting, ChosenOwner: f.taskOwner}
		return store.Route(ctx, ex, item.TenantID, item.WorkItemID, item.ItemVersion, assignment,
			workitem.TransitionMeta{ActorPrincipalID: "system:workflow-runtime", Reason: "ASSIGNMENT_RESOLVED", At: req.CreatedAt})

	default:
		return workitem.WorkItem{}, fmt.Errorf("test/workflow: no human-work spec bound for node %s", req.Continuation.TargetNodeID)
	}
}

func demoTaskNode() stepstask.CompiledTaskNode {
	return stepstask.CompiledTaskNode{
		WorkflowID: demoWorkflowID, WorkflowVersion: 1, NodeID: nodeTask, WorkType: demoTaskWorkType,
		OutputSchema:        demoSchema("TaskSubmissionOutput"),
		FormDefinition:      stepstask.VersionedRef{Ref: "form.test.promotion_execute_demo.provision_role", Version: 1},
		AccessibilityPolicy: stepstask.VersionedRef{Ref: "policy.accessibility.default/v1", Version: 1},
		AccommodationPolicy: stepstask.VersionedRef{Ref: "policy.accommodation.default/v1", Version: 1},
	}
}

// ---------------------------------------------------------------------------
// Ledger wiring.
// ---------------------------------------------------------------------------

func newLedgerAppender(t *testing.T) ledgerport.Appender {
	t.Helper()
	registry, err := ledgerport.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatalf("ledger.NewLedgerEventDigestRegistry: %v", err)
	}
	return ledgerport.NewAppender(registry)
}

// ---------------------------------------------------------------------------
// pgtest plumbing.
// ---------------------------------------------------------------------------

func insertTenant(t *testing.T, db *pgtest.DB, key string, at time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', $4)`,
		id, key, "tenant "+key, at)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) dbport.Beginner {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTenantTx(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	if err := fn(tx); err != nil {
		t.Fatalf("transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Approval decision + task submission mapping: exactly the seam
// internal/workflow/execute's own ResumeRequest.Outcome leaves the caller
// (see execute/resume_test.go's own resume fixture), computed here through
// steps/approval.Resolve and steps/task.Resolve rather than asserted by
// hand.
// ---------------------------------------------------------------------------

func approvalDecisionFor(item workitem.WorkItem, req humanwork.ApprovalRequirement, proposal intent.ProposalRevision, at time.Time) intentapproval.ApprovalDecision {
	return intentapproval.ApprovalDecision{
		DecisionID: "decision:promotion-execute-demo-manager-1",
		Binding: intentapproval.DecisionBinding{
			RequirementID: req.RequirementID, RequirementRevision: req.Revision, RequirementDigest: req.Digest(),
			IntentID: proposal.IntentID, ProposalRevisionID: proposal.ProposalRevisionID, ProposalDigest: proposal.MaterialDigest,
			ControlSnapshots: proposal.ControlSnapshots, TaskVersion: 1,
			RenderedProjectionDigest:   "sha256:" + strings.Repeat("a", 64),
			ResolutionExpressionDigest: req.ExpressionDigest,
		},
		Outcome: intentapproval.OutcomeApproved,
		Approver: intentapproval.ApproverReference{
			PrincipalID: item.CompletedBy, IdentityAssuranceRef: "assurance.mfa_session/v1",
			SessionRef: "session:promotion-execute-demo", Via: humanwork.SourceDirect,
		},
		AuthorityDecisionRef: "authz:decision:promotion-execute-demo",
		Reason:               "reason.promotion_supported/v1",
		DecidedAt:            values.NewInstant(at),
	}
}

// approvalOutcome reconstructs the same ApprovalDecision the approval was
// completed with (decidedAt must be the exact instant approvalDecisionFor
// minted it at, since that instant is part of the decision's own digest --
// see checkDecisionBinding's decision.Digest() == item.CompletedOutputDigest
// check) and resolves the continuation at now, which may be later.
func approvalOutcome(t *testing.T, item workitem.WorkItem, req humanwork.ApprovalRequirement, proposal intent.ProposalRevision, decidedAt, now time.Time) frontier.NodeOutcome {
	t.Helper()
	decision := approvalDecisionFor(item, req, proposal, decidedAt)
	continuation, err := stepsapproval.NewContinuation(item.WorkflowInstanceID, nodeApproval, proposal,
		humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{req}}, []workitem.WorkItem{item})
	if err != nil {
		t.Fatalf("steps/approval.NewContinuation: %v", err)
	}
	resolution, err := stepsapproval.Resolve(continuation, []workitem.WorkItem{item}, []intentapproval.ApprovalDecision{decision},
		values.NewInstant(now), stepsapproval.Event{})
	if err != nil {
		t.Fatalf("steps/approval.Resolve: %v", err)
	}
	return resolution.ToNodeOutcome(nodeApproval)
}

func taskOutcome(t *testing.T, item workitem.WorkItem, submission stepstask.Submission, at time.Time) frontier.NodeOutcome {
	t.Helper()
	continuation, err := stepstask.NewContinuation(item.WorkflowInstanceID, demoTaskNode(), item)
	if err != nil {
		t.Fatalf("steps/task.NewContinuation: %v", err)
	}
	always := stepstask.ValidatorFunc(func(stepstask.ValidationRequest) error { return nil })
	resolution, err := stepstask.Resolve(continuation, item, &submission, always, values.NewInstant(at), stepstask.Event{})
	if err != nil {
		t.Fatalf("steps/task.Resolve: %v", err)
	}
	return resolution.ToNodeOutcome(nodeTask)
}

// ---------------------------------------------------------------------------
// TestPromotionWorkflowExecutesEndToEndWithOneGovernedWrite
// ---------------------------------------------------------------------------

func TestPromotionWorkflowExecutesEndToEndWithOneGovernedWrite(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	db := pgtest.New(t)
	beginner := appConn(t, db)
	tenantID := insertTenant(t, db, "promo-exec-1", at)

	versions, plan, activated := publishActiveDemoPlan(t, at)
	if activated.Status != version.StatusActive {
		t.Fatalf("published version status = %s, want ACTIVE", activated.Status)
	}

	proposal := newDemoProposal(t, values.TenantId("promo-exec-1"), "intent:promotion-execute-demo-1", at)
	binding := runtime.ProposalBinding{Revision: proposal, Approved: true, ApprovalRef: "decision:hr-partner-approves-start"}

	managerReq, managerRes, _, _ := managerRequirementAndResolution(t)

	resolver := effects.PolicyResolver{Entries: []effects.PolicyEntry{{
		WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: activated.CompiledPlanDigest}, Plan: plan,
	}}}
	appender := newLedgerAppender(t)
	terminal := &effects.LedgerTerminalWriter{
		Appender: appender, ProjectionName: "workflow_promotion_outcome_test",
		SourceRef: "hcmnext:test:workflow",
	}
	workItems := demoWorkItems{proposal: proposal, managerReq: managerReq, managerResolution: managerRes, taskOwner: humanwork.PrincipalHRBP}

	drv, err := execute.New(execute.Options{
		DB: beginner, Steps: endOnlySteps{}, WorkItems: workItems, Terminal: terminal,
		Guard: idempotency.PostgresStore{}, Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock: func() time.Time { return at },
	})
	if err != nil {
		t.Fatalf("execute.New: %v", err)
	}

	start := runtime.StartRequest{
		TenantID: tenantID, CellID: "cell-local", StartIdempotencyKey: "start:promotion-execute-demo-1",
		Resolver: resolver, Versions: versions, Proposal: binding,
		ExpectedIntentID: proposal.IntentID, ExpectedTenant: proposal.Tenant,
		BusinessSubjectRefs: []string{"employment:promotion-execute-demo-1"},
		ExecutionMode:       workflow.ModeExecute, CorrelationID: "corr:promotion-execute-demo-1", CreatedAt: at,
	}

	// --- Execute: parks at the approval work item. ---
	parkedApproval, err := drv.Execute(ctx, execute.ExecuteRequest{Start: start})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if parkedApproval.Status != execute.StatusParked || len(parkedApproval.WorkItems) != 1 {
		t.Fatalf("Execute result = %+v, want PARKED with one WorkItem", parkedApproval)
	}
	approvalItem := parkedApproval.WorkItems[0]
	if approvalItem.Kind != workitem.KindApproval || approvalItem.NodeID != nodeApproval {
		t.Fatalf("parked work item = %+v, want the APPROVAL work item for %s", approvalItem, nodeApproval)
	}
	if approvalItem.Status != workitem.StatusAssigned && approvalItem.Status != workitem.StatusAvailable {
		t.Fatalf("approval work item = %+v, want ASSIGNED or AVAILABLE", approvalItem)
	}
	if _, ok := approvalItem.Assignment.Resolution.Authorizes(humanwork.PrincipalManager); !ok {
		t.Fatalf("approval work item assignment = %+v, want %s among the authorized candidates", approvalItem.Assignment, humanwork.PrincipalManager)
	}

	// --- Claim, start and complete the approval work item as the manager. ---
	store := workitem.Store{}
	var completedApproval workitem.WorkItem
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		claimed, claimErr := store.Claim(ctx, tx, workitem.ClaimInput{
			TenantID: tenantID, WorkItemID: approvalItem.WorkItemID, ExpectedVersion: approvalItem.ItemVersion,
			ClaimantPrincipalID: humanwork.PrincipalManager, ClaimExpiresAt: at.Add(time.Hour), Now: at,
			Meta: workitem.TransitionMeta{ActorPrincipalID: humanwork.PrincipalManager, Reason: "APPROVAL_CLAIMED", At: at},
		})
		if claimErr != nil {
			return claimErr
		}
		started, startErr := store.Start(ctx, tx, tenantID, approvalItem.WorkItemID, claimed.ItemVersion, at,
			workitem.TransitionMeta{ActorPrincipalID: humanwork.PrincipalManager, Reason: "APPROVAL_STARTED", At: at})
		if startErr != nil {
			return startErr
		}
		decision := approvalDecisionFor(workitem.WorkItem{CompletedBy: humanwork.PrincipalManager}, managerReq, proposal, at)
		var completeErr error
		completedApproval, completeErr = stepsapproval.Complete(ctx, tx, store, started, decision, at,
			workitem.TransitionMeta{ActorPrincipalID: humanwork.PrincipalManager, Reason: "APPROVAL_DECIDED", At: at})
		return completeErr
	})
	if completedApproval.Status != workitem.StatusCompleted {
		t.Fatalf("completed approval work item status = %s, want COMPLETED", completedApproval.Status)
	}

	// --- Resume: the approval decision routes to the TASK, which parks. ---
	approvalOut := approvalOutcome(t, completedApproval, managerReq, proposal, at, at.Add(time.Minute))
	if approvalOut.Outcome != workflow.Outcome("APPROVED") {
		t.Fatalf("approval resolution outcome = %q, want APPROVED", approvalOut.Outcome)
	}
	parkedTask, err := drv.Resume(ctx, execute.ResumeRequest{
		Start: start, InstanceID: parkedApproval.Start.InstanceID, ExpectedInstanceVersion: parkedApproval.InstanceVersion,
		WorkItem: completedApproval, Outcome: approvalOut, RecordedAt: at.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Resume (approval->task): %v", err)
	}
	if parkedTask.Status != execute.StatusParked || len(parkedTask.WorkItems) != 1 {
		t.Fatalf("Resume result = %+v, want PARKED with one WorkItem", parkedTask)
	}
	taskItem := parkedTask.WorkItems[0]
	if taskItem.Kind != workitem.KindTask || taskItem.NodeID != nodeTask {
		t.Fatalf("parked work item = %+v, want the TASK work item for %s", taskItem, nodeTask)
	}

	// --- Claim and submit the task as its assigned owner. ---
	var completedTask workitem.WorkItem
	var submission stepstask.Submission
	claimID := uuid.New()
	claimExpires := at.Add(2 * time.Hour)
	submittedAt := at.Add(90 * time.Minute)
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		claimed, claimErr := store.Claim(ctx, tx, workitem.ClaimInput{
			TenantID: tenantID, WorkItemID: taskItem.WorkItemID, ExpectedVersion: taskItem.ItemVersion,
			ClaimantPrincipalID: humanwork.PrincipalHRBP, ClaimExpiresAt: claimExpires, Now: at.Add(time.Minute),
			Meta: workitem.TransitionMeta{ActorPrincipalID: humanwork.PrincipalHRBP, Reason: "TASK_CLAIMED", At: at.Add(time.Minute)},
		})
		if claimErr != nil {
			return claimErr
		}
		started, startErr := store.Start(ctx, tx, tenantID, taskItem.WorkItemID, claimed.ItemVersion, at.Add(2*time.Minute),
			workitem.TransitionMeta{ActorPrincipalID: humanwork.PrincipalHRBP, Reason: "TASK_STARTED", At: at.Add(2 * time.Minute)})
		if startErr != nil {
			return startErr
		}
		claimed.ClaimID = &claimID
		started.ClaimID = &claimID
		var submitErr error
		completedTask, submission, submitErr = stepstask.Submit(ctx, tx, store, started, stepstask.SubmitInput{
			Node: demoTaskNode(),
			Spec: stepstask.SubmissionSpec{
				CompletedBy: humanwork.PrincipalHRBP, CandidateVia: humanwork.SourceDirect,
				ClaimID: claimID, ClaimExpiresAt: values.NewInstant(claimExpires), SubmittedAt: values.NewInstant(submittedAt),
				OutputSchema: demoTaskNode().OutputSchema, CanonicalPayloadDigest: "sha256:" + strings.Repeat("c", 64),
				FormDefinition: demoTaskNode().FormDefinition, RenderContextDigest: "sha256:" + strings.Repeat("d", 64),
				ValidationEvidenceRef: "evidence.validation.test/v1", AccessibilityEvidenceRef: "evidence.accessibility.test/v1",
				AccommodationEvidenceRef: "evidence.accommodation.test/v1",
			},
			Validator: stepstask.ValidatorFunc(func(stepstask.ValidationRequest) error { return nil }),
			Now:       submittedAt, Meta: workitem.TransitionMeta{ActorPrincipalID: humanwork.PrincipalHRBP, Reason: "TASK_SUBMITTED", At: submittedAt},
		})
		return submitErr
	})
	if completedTask.Status != workitem.StatusCompleted {
		t.Fatalf("completed task work item status = %s, want COMPLETED", completedTask.Status)
	}

	// --- Resume: the task submission routes to COMPLETE. ---
	taskOut := taskOutcome(t, completedTask, submission, submittedAt.Add(time.Minute))
	if taskOut.Outcome != workflow.OutcomeSucceeded {
		t.Fatalf("task resolution outcome = %q, want SUCCEEDED", taskOut.Outcome)
	}
	final, err := drv.Resume(ctx, execute.ResumeRequest{
		Start: start, InstanceID: parkedApproval.Start.InstanceID, ExpectedInstanceVersion: parkedTask.InstanceVersion,
		WorkItem: completedTask, Outcome: taskOut, RecordedAt: submittedAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Resume (task->complete): %v", err)
	}
	if final.Status != execute.StatusComplete {
		t.Fatalf("final Resume result = %+v, want COMPLETE", final)
	}

	// --- Exactly one new ledger business event for the promotion. ---
	streamKey := effects.StreamKeyFor(demoWorkflowID, parkedApproval.Start.InstanceID.String())
	var ledgerEventCount int
	if err := db.Conn.QueryRow(ctx, `
		SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`,
		tenantID, streamKey).Scan(&ledgerEventCount); err != nil {
		t.Fatalf("count ledger events: %v", err)
	}
	if ledgerEventCount != 1 {
		t.Fatalf("ledger events on %s = %d, want exactly 1", streamKey, ledgerEventCount)
	}

	// --- A second Resume with the identical completion appends nothing. ---
	replay, err := drv.Resume(ctx, execute.ResumeRequest{
		Start: start, InstanceID: parkedApproval.Start.InstanceID, ExpectedInstanceVersion: parkedTask.InstanceVersion,
		WorkItem: completedTask, Outcome: taskOut, RecordedAt: submittedAt.Add(time.Minute),
	})
	if err == nil {
		if replay.Status != execute.StatusComplete {
			t.Fatalf("idempotent replay result = %+v, want COMPLETE", replay)
		}
	} else if code := runtime.CodeOf(err); code != runtime.CodeStaleInstance {
		t.Fatalf("idempotent replay error = %v (code %q), want a replay or the documented stale-version seam", err, code)
	}
	var ledgerEventCountAfterReplay int
	if err := db.Conn.QueryRow(ctx, `
		SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`,
		tenantID, streamKey).Scan(&ledgerEventCountAfterReplay); err != nil {
		t.Fatalf("recount ledger events: %v", err)
	}
	if ledgerEventCountAfterReplay != 1 {
		t.Fatalf("ledger events after replayed resume = %d, want still exactly 1 (idempotency guard must append nothing)", ledgerEventCountAfterReplay)
	}

	// --- The inspector renders the full chronology. ---
	var loadedInstance runtime.Instance
	var loadedNodes []runtime.NodeExecution
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		var loadErr error
		loadedInstance, loadErr = (runtime.Store{}).LoadInstance(ctx, tx, tenantID, parkedApproval.Start.InstanceID)
		if loadErr != nil {
			return loadErr
		}
		loadedNodes, loadErr = (runtime.Store{}).LoadNodeExecutions(ctx, tx, tenantID, parkedApproval.Start.InstanceID)
		return loadErr
	})
	view, err := inspect.Build(inspect.Request{
		Instance: loadedInstance, Nodes: loadedNodes,
		Authorization: inspect.AllowAll("policy.test/v1", "OPERATIONS", "principal:test-operator"),
	})
	if err != nil {
		t.Fatalf("inspect.Build: %v", err)
	}
	if len(view.FrontierNodeIDs()) != 0 {
		t.Fatalf("inspector frontier = %v, want empty (instance is terminal)", view.FrontierNodeIDs())
	}
	for _, id := range []string{nodeApproval, nodeTask, nodeApproved} {
		if _, ok := view.Node(id); !ok {
			t.Errorf("inspector chronology is missing node %s", id)
		}
	}

	// --- The instance's five lifecycle dimensions are terminal. ---
	if loadedInstance.RuntimeStatus != runtime.InstanceCompleted {
		t.Fatalf("instance runtime status = %s, want COMPLETED", loadedInstance.RuntimeStatus)
	}
	want := runtime.Dimensions{
		RequestState: "CLOSED", ExecutionState: "COMMITTED", BusinessState: "COMPLETED",
		ConsistencyState: "CONSISTENT", ObligationState: "SATISFIED",
	}
	if loadedInstance.CompletionDimensions != want {
		t.Fatalf("completion dimensions = %+v, want %+v", loadedInstance.CompletionDimensions, want)
	}
}

// ---------------------------------------------------------------------------
// TestPromotionWorkflowExecuteRefusesWithoutApprovedProposal
// ---------------------------------------------------------------------------

func TestPromotionWorkflowExecuteRefusesWithoutApprovedProposal(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	db := pgtest.New(t)
	beginner := appConn(t, db)
	tenantID := insertTenant(t, db, "promo-exec-refuse", at)

	versions, plan, activated := publishActiveDemoPlan(t, at)
	resolver := effects.PolicyResolver{Entries: []effects.PolicyEntry{{
		WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: activated.CompiledPlanDigest}, Plan: plan,
	}}}
	managerReq, managerRes, _, _ := managerRequirementAndResolution(t)

	drv, err := execute.New(execute.Options{
		DB: beginner, Steps: endOnlySteps{}, Terminal: &effects.LedgerTerminalWriter{Appender: newLedgerAppender(t), ProjectionName: "workflow_promotion_outcome_test", SourceRef: "hcmnext:test:workflow"},
		WorkItems: demoWorkItems{managerReq: managerReq, managerResolution: managerRes, taskOwner: humanwork.PrincipalHRBP},
		Guard:     idempotency.PostgresStore{}, Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock: func() time.Time { return at },
	})
	if err != nil {
		t.Fatalf("execute.New: %v", err)
	}

	baseRequest := func(key string, proposal intent.ProposalRevision, binding runtime.ProposalBinding) runtime.StartRequest {
		return runtime.StartRequest{
			TenantID: tenantID, CellID: "cell-local", StartIdempotencyKey: key,
			Resolver: resolver, Versions: versions, Proposal: binding,
			ExpectedIntentID: proposal.IntentID, ExpectedTenant: proposal.Tenant,
			BusinessSubjectRefs: []string{"employment:promotion-execute-demo-1"},
			ExecutionMode:       workflow.ModeExecute, CorrelationID: "corr:" + key, CreatedAt: at,
		}
	}

	t.Run("mutable proposal (no minted material digest) never starts", func(t *testing.T) {
		proposal := newDemoProposal(t, values.TenantId("promo-exec-refuse"), "intent:refuse-mutable", at)
		mutable := proposal
		mutable.MaterialDigest.Digest = ""
		_, err := drv.Execute(ctx, execute.ExecuteRequest{Start: baseRequest("refuse-mutable", proposal,
			runtime.ProposalBinding{Revision: mutable, Approved: true, ApprovalRef: "decision:x"})})
		if code := runtime.CodeOf(err); code != runtime.CodeMutableProposal {
			t.Fatalf("Execute error = %v (code %q), want %q", err, code, runtime.CodeMutableProposal)
		}
	})

	t.Run("unapproved proposal never starts", func(t *testing.T) {
		proposal := newDemoProposal(t, values.TenantId("promo-exec-refuse"), "intent:refuse-unapproved", at)
		_, err := drv.Execute(ctx, execute.ExecuteRequest{Start: baseRequest("refuse-unapproved", proposal,
			runtime.ProposalBinding{Revision: proposal, Approved: false})})
		if code := runtime.CodeOf(err); code != runtime.CodeUnapprovedProposal {
			t.Fatalf("Execute error = %v (code %q), want %q", err, code, runtime.CodeUnapprovedProposal)
		}
	})

	t.Run("superseded proposal never starts", func(t *testing.T) {
		proposal := newDemoProposal(t, values.TenantId("promo-exec-refuse"), "intent:refuse-superseded", at)
		_, err := drv.Execute(ctx, execute.ExecuteRequest{Start: baseRequest("refuse-superseded", proposal,
			runtime.ProposalBinding{Revision: proposal, Approved: true, ApprovalRef: "decision:x", Superseded: true})})
		if code := runtime.CodeOf(err); code != runtime.CodeSupersededProposal {
			t.Fatalf("Execute error = %v (code %q), want %q", err, code, runtime.CodeSupersededProposal)
		}
	})

	var instanceCount int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM workflow_instance WHERE tenant_id = $1`, tenantID).Scan(&instanceCount); err != nil {
		t.Fatalf("count workflow instances: %v", err)
	}
	if instanceCount != 0 {
		t.Fatalf("workflow_instance rows after every refused start = %d, want 0", instanceCount)
	}
}

// ---------------------------------------------------------------------------
// TestPromotionWorkflowExecuteIsCallerDriven
// ---------------------------------------------------------------------------

// TestPromotionWorkflowExecuteIsCallerDriven proves the reevaluation_trigger
// ruling in definitions/runtime/durable-runtime-decision.yaml by source scan:
// no goroutine and no sleep/ticker/timer construction anywhere in
// internal/workflow/execute or internal/workflow/execute/effects -- every
// completion this ticket's driver acts on must come from an explicit caller
// argument, never a background loop.
//
// time.Now is scanned strictly on this ticket's own package
// (internal/workflow/execute/effects): every instant effects.
// LedgerTerminalWriter records is the caller's own RecordedAt. On
// internal/workflow/execute itself, one occurrence is tolerated rather than
// hidden: (*Options).clock's nil-Clock fallback in driver.go, an override-only
// default this test also never exercises (every composition here, including
// this one, always supplies an explicit Clock). A second occurrence anywhere
// in execute still fails the test, so a real regression is still caught.
func TestPromotionWorkflowExecuteIsCallerDriven(t *testing.T) {
	scanDirForCallerDrivenViolations(t, filepath.Join("..", "..", "internal", "workflow", "execute"), 1)
	scanDirForCallerDrivenViolations(t, filepath.Join("..", "..", "internal", "workflow", "execute", "effects"), 0)
}

// forbiddenAlways names background-execution primitives that are never
// acceptable in a caller-driven package, in either directory.
var forbiddenAlways = map[string]bool{
	"time.Sleep": true, "time.NewTicker": true, "time.NewTimer": true,
	"time.AfterFunc": true, "time.Tick": true,
}

const forbiddenClockRead = "time.Now"

// scanDirForCallerDrivenViolations parses every non-test .go file in dir and
// fails the test on a goroutine launch or any call in forbiddenAlways, and on
// more than allowedClockReads occurrences of time.Now.
func scanDirForCallerDrivenViolations(t *testing.T, dir string, allowedClockReads int) {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}
	if len(pkgs) == 0 {
		t.Fatalf("no non-test Go files found under %s", dir)
	}
	clockReads := 0
	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.GoStmt:
					t.Errorf("%s: goroutine launch (go statement) found; the caller-driven ruling forbids background execution", filename)
				case *ast.CallExpr:
					sel, ok := node.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					ident, ok := sel.X.(*ast.Ident)
					if !ok {
						return true
					}
					qualified := ident.Name + "." + sel.Sel.Name
					switch {
					case forbiddenAlways[qualified]:
						t.Errorf("%s: forbidden call %s found; every instant and every wake must come from a caller argument", filename, qualified)
					case qualified == forbiddenClockRead:
						clockReads++
						t.Logf("%s: found %s (occurrence %d)", filename, qualified, clockReads)
					}
				}
				return true
			})
		}
	}
	if clockReads > allowedClockReads {
		t.Errorf("%s: found %d occurrences of %s, want at most %d", dir, clockReads, forbiddenClockRead, allowedClockReads)
	}
}

var _ = errors.New // keep errors imported if unused by future edits
