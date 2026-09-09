package migrate_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/migrate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// fixedInstant is the clock every fixture stamps. Neither this package nor
// internal/workflow/runtime reads a wall clock, so a test that wants a time
// has to say which one.
var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

const promotionSubjectID = "employment:jane-doe-9001"

// --- database plumbing (mirrors internal/workflow/runtime's own
// fixtures_test.go; those helpers are unexported to that package and cannot
// be reused across a package boundary) ---------------------------------------

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTx(t *testing.T, conn *pgxadapter.Conn, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTxErr(conn, fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

func inTxErr(conn *pgxadapter.Conn, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	return inTxErr(conn, func(tx dbport.Tx) error {
		if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
			return err
		}
		return fn(tx)
	})
}

// --- compiled plans -----------------------------------------------------

func bootstrapRegistry(t *testing.T) *capability.Registry {
	t.Helper()
	reg, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("bootstrap capability registry: %v", err)
	}
	return reg
}

func promotionSourcePlan(t *testing.T, registry workflow.CapabilityResolver) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := workflow.CompilePromotionReference(registry)
	if err != nil {
		t.Fatalf("compile Promotion fixture: %v", err)
	}
	return plan
}

// promotionTargetPlanEvaluateChanged bumps the definition version and changes
// evaluate_band's governance purpose only. Every node on the start frontier
// (snapshot_worker) stays byte-identical between source and target, so a
// migrationpreview.Preview classifies an instance paused there SAFE/CONTINUE.
func promotionTargetPlanEvaluateChanged(t *testing.T, registry workflow.CapabilityResolver) *workflow.CompiledWorkflow {
	t.Helper()
	def := workflow.PromotionReferenceDefinition()
	def.Version++
	for i := range def.Nodes {
		if def.Nodes[i].ID == workflow.PromotionNodeEvaluateBand {
			def.Nodes[i].Governance.Purpose = "SIMULATE_MANAGEMENT_PROMOTION_REVISED"
		}
	}
	plan, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1A, Capabilities: registry})
	if err != nil {
		t.Fatalf("compile changed Promotion fixture: %v", err)
	}
	return plan
}

// promotionTargetPlanWithoutBuildProposal removes build_proposal and rewires
// raise_threshold/end_requires_finance_approval's input mappings accordingly.
// It mirrors internal/workflow/migrationpreview's own WF-RUN-017 fixture: the
// smallest real change that removes a live step and forces a cross-node
// bridge.
func promotionTargetPlanWithoutBuildProposal(t *testing.T, registry workflow.CapabilityResolver) *workflow.CompiledWorkflow {
	t.Helper()
	def := workflow.PromotionReferenceDefinition()
	def.Version++
	nodes := make([]workflow.Node, 0, len(def.Nodes)-1)
	for _, node := range def.Nodes {
		if node.ID == workflow.PromotionNodeBuildProposal {
			continue
		}
		nodes = append(nodes, node)
	}
	def.Nodes = nodes
	var edges []workflow.Edge
	for _, edge := range def.Edges {
		if edge.From == workflow.PromotionNodeBuildProposal {
			continue
		}
		if edge.To == workflow.PromotionNodeBuildProposal {
			edge.To = workflow.PromotionNodeRaiseThreshold
		}
		edges = append(edges, edge)
	}
	def.Edges = edges
	for i := range def.Nodes {
		if def.Nodes[i].ID != workflow.PromotionNodeRaiseThreshold {
			continue
		}
		for j := range def.Nodes[i].InputMappings {
			switch def.Nodes[i].InputMappings[j].Target {
			case "raise_ratio":
				def.Nodes[i].InputMappings[j].Source.NodeID = workflow.PromotionNodeSimulateComp
			case "band_position":
				def.Nodes[i].InputMappings[j].Source.NodeID = workflow.PromotionNodeEvaluateBand
			}
		}
	}
	for i := range def.Nodes {
		if def.Nodes[i].ID != workflow.PromotionNodeEndApproval {
			continue
		}
		for j := range def.Nodes[i].InputMappings {
			if def.Nodes[i].InputMappings[j].Target == "proposal_digest" {
				def.Nodes[i].InputMappings[j].Source = workflow.Source{
					Kind:     workflow.SourceConstant,
					Constant: "migrated-proposal",
					Type:     workflow.ValueType{Kind: workflow.KindString},
				}
			}
		}
	}
	plan, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1A, Capabilities: registry})
	if err != nil {
		t.Fatalf("compile modified Promotion fixture: %v", err)
	}
	return plan
}

// --- workflow start fixture (mirrors internal/workflow/runtime's own
// promotionFixture, trimmed to what this package's tests need) --------------

func testControlSnapshots() intent.ControlSnapshots {
	return intent.ControlSnapshots{
		CapabilityRegistryDigest:     "sha256:capability-registry-fixture",
		PolicyBundleDigest:           "sha256:policy-bundle-fixture",
		LegalContextDigest:           "sha256:legal-context-fixture",
		EntitlementDigest:            "sha256:entitlement-fixture",
		ReferenceDataDigest:          "sha256:reference-data-fixture",
		ClassificationTaxonomyDigest: "sha256:classification-taxonomy-fixture",
		DLPDecisionDigest:            "sha256:dlp-decision-fixture",
	}
}

func promotionResolvedContext() map[string]string {
	return map[string]string{"LegalContext": "sha256:legal-context-resolved"}
}

type stubResolver struct {
	sel runtime.WorkflowSelection
	err error
}

func (s stubResolver) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	return s.sel, s.err
}

type promotionFixture struct {
	Plan     *workflow.CompiledWorkflow
	Versions *version.Registry
	Resolver runtime.WorkflowResolver
	Proposal intent.ProposalRevision
	Binding  runtime.ProposalBinding
}

func newPromotionFixture(t *testing.T, tenant values.TenantId, intentID string) promotionFixture {
	t.Helper()
	registry := bootstrapRegistry(t)
	plan := promotionSourcePlan(t, registry)

	store := version.NewRegistry()
	cv, err := version.Publish(store, workflow.PromotionReferenceDefinition(), plan,
		workflow.Options{Phase: workflow.PhaseP1A, Capabilities: registry}, version.PublishMeta{
			SemanticVersion: "1.0.0", PublishedAt: fixedInstant, PublishedBy: "test-publisher",
		})
	if err != nil {
		t.Fatalf("version.Publish: %v", err)
	}
	if _, err := version.Activate(store, cv.CompiledPlanDigest, version.ActivationEvidence{
		Authorized: true, ApprovedBy: "qa-lead", Authority: "authority:release-management",
		ApprovedAt: fixedInstant, ReviewedPlanDigest: cv.CompiledPlanDigest, TestsPassed: true,
	}); err != nil {
		t.Fatalf("version.Activate: %v", err)
	}

	digester, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("protomap.NewDefaultDigester: %v", err)
	}
	def := intent.Definition{
		Ref:    intent.Ref{TypeID: "hcmnext.test.workflow_migrate", Version: 1},
		Family: intent.FamilyChangeRequest,
	}
	interval, err := values.NewOpenInstantInterval(values.NewInstant(fixedInstant))
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	spec := intent.ProposalSpec{
		IntentID: intentID, Revision: 1, Tenant: tenant, OrganizationScopeID: "org:acme-test:eng",
		Subjects: []intent.SubjectReference{
			{Kind: "EMPLOYMENT", SubjectID: promotionSubjectID, AuthorityDomain: "PEOPLE"},
		},
		EffectiveTime:    interval,
		ControlSnapshots: testControlSnapshots(),
		CreatedBy: intent.PrincipalReference{
			PrincipalID: "principal:hr-partner-7", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "assurance:1",
		},
	}
	rev, err := intent.NewProposalRevision(spec, def, digester, nil, func() values.Instant { return values.NewInstant(fixedInstant) })
	if err != nil {
		t.Fatalf("NewProposalRevision: %v", err)
	}

	return promotionFixture{
		Plan:     plan,
		Versions: store,
		Resolver: stubResolver{sel: runtime.WorkflowSelection{
			WorkflowID: plan.WorkflowID,
			Pin:        version.Pin{CompiledPlanDigest: plan.Digest()},
			Plan:       plan,
		}},
		Proposal: rev,
		Binding:  runtime.ProposalBinding{Revision: rev},
	}
}

func (pf promotionFixture) baseStartRequest(tenantID uuid.UUID, key string) runtime.StartRequest {
	approvalFacts := runtime.MemoryApprovalFacts{ByRevisionID: map[string][]runtime.ApprovalDecisionFact{
		pf.Proposal.ProposalRevisionID: {{
			DecisionID: "decision:finance-partner-1", Outcome: runtime.ApprovalOutcomeApproved,
			ProposalDigest: pf.Proposal.MaterialDigest.Digest,
		}},
	}}
	return runtime.StartRequest{
		TenantID:            tenantID,
		CellID:              "cell-local",
		StartIdempotencyKey: key,
		Resolver:            pf.Resolver,
		Versions:            pf.Versions,
		Proposal:            pf.Binding,
		ProposalFacts:       runtime.MemoryProposalFacts{},
		ApprovalFacts:       approvalFacts,
		ExpectedIntentID:    pf.Proposal.IntentID,
		ExpectedTenant:      pf.Proposal.Tenant,
		BusinessSubjectRefs: []string{promotionSubjectID},
		ExecutionMode:       workflow.ModeSimulate,
		CorrelationID:       "corr-" + key,
		ResolvedContext:     promotionResolvedContext(),
		CreatedAt:           fixedInstant,
	}
}

// --- runtime driving helpers ---------------------------------------------

func startPromotionInstance(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, req runtime.StartRequest) runtime.StartReceipt {
	t.Helper()
	var receipt runtime.StartReceipt
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		receipt, err = runtime.Start(context.Background(), tx, req)
		return err
	})
	return receipt
}

func advanceOnce(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, req runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
	t.Helper()
	var receipt runtime.AdvanceReceipt
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		var advErr error
		receipt, advErr = runtime.Advance(context.Background(), tx, req)
		return advErr
	})
	return receipt, err
}

func advanceOutcome(tenantID, instanceID uuid.UUID, plan *workflow.CompiledWorkflow, expectedVersion int64, sink runtime.ContinuationSink, nodeID string, outcome workflow.Outcome, digest string) runtime.AdvanceRequest {
	return runtime.AdvanceRequest{
		TenantID: tenantID, InstanceID: instanceID, ExpectedInstanceVersion: expectedVersion, Attempt: 1,
		Plan:       plan,
		Outcome:    frontier.NodeOutcome{NodeID: nodeID, Outcome: outcome, OutputDigest: digest},
		RecordedAt: fixedInstant,
		Sink:       sink,
	}
}

func requestPause(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, req runtime.PauseRequest) (runtime.PauseReceipt, error) {
	t.Helper()
	var receipt runtime.PauseReceipt
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		var pauseErr error
		receipt, pauseErr = runtime.RequestPause(context.Background(), tx, req)
		return pauseErr
	})
	return receipt, err
}

func resumeFromPause(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, req runtime.ResumeRequest) (runtime.PauseReceipt, error) {
	t.Helper()
	var receipt runtime.PauseReceipt
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		var resumeErr error
		receipt, resumeErr = runtime.ResumeFromPause(context.Background(), tx, req)
		return resumeErr
	})
	return receipt, err
}

func loadInstanceForTest(t *testing.T, conn *pgxadapter.Conn, tenantID, instanceID uuid.UUID) runtime.Instance {
	t.Helper()
	var inst runtime.Instance
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		inst, err = (runtime.Store{}).LoadInstance(context.Background(), tx, tenantID, instanceID)
		return err
	})
	return inst
}

func loadNodeExecutionForTest(t *testing.T, conn *pgxadapter.Conn, tenantID, instanceID uuid.UUID, nodeID string, attempt int) runtime.NodeExecution {
	t.Helper()
	var ne runtime.NodeExecution
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		ne, err = (runtime.Store{}).LoadNodeExecution(context.Background(), tx, tenantID, instanceID, nodeID, attempt)
		return err
	})
	return ne
}

// takeCheckpoint records one workflow_checkpoint row describing inst's
// current version and frontier, using [migrate.FrontierDigest] so a later
// [migrate.Migrate] call recognizes it. variableDigest and stateDigest are
// any 64-hex-character placeholder: this fixture never touches business
// variable content, so its exact value is not under test.
func takeCheckpoint(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, inst runtime.Instance, sequence uint64, kind, stateDigest, variableDigest string) {
	t.Helper()
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		return (runtimestate.CheckpointStore{}).Take(context.Background(), tx, runtimestate.Checkpoint{
			TenantID: tenantID, InstanceID: inst.InstanceID, Sequence: sequence,
			Kind:            kind,
			StateDigest:     stateDigest,
			FrontierDigest:  migrate.FrontierDigest(inst.CurrentNodeIDs),
			VariableDigest:  variableDigest,
			InstanceVersion: uint64(inst.InstanceVersion),
			TakenAt:         fixedInstant,
		})
	})
}

// placeholderDigest is a syntactically valid 64-hex-character content digest
// used wherever a fixture must supply one but its exact value is not under
// test (this package never touches business variable content).
const placeholderDigest = "0000000000000000000000000000000000000000000000000000000000000000"
