package inspect_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var update = flag.Bool("update", false, "rewrite the checked-in golden view")

// Fixed identities and instants. A projection whose rendered bytes are pinned
// cannot draw any value from a clock or a random source, so every one of them
// is a constant here rather than a uuid.New() call.
var (
	fixtureTenant     = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	fixtureInstance   = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	fixtureTransation = uuid.MustParse("33333333-3333-4333-8333-333333333333")

	fixtureCreated = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	fixtureStarted = time.Date(2026, 9, 3, 12, 0, 1, 0, time.UTC)
	fixtureEnded   = time.Date(2026, 9, 3, 12, 0, 2, 0, time.UTC)
)

// fixturePlanHash is a real sha256 hex digest: migration 00002's
// content_digest domain accepts nothing else, so a readable stand-in like
// "sha256:88718f2d" would pass every in-memory test and fail the one that
// writes to PostgreSQL.
const fixturePlanHash = "88718f2d5c1a4e0b93f6c27a1d8e45b0a3f79c62e410d85b7a2c93f14e60d8b2"

const (
	nodePreflight = "preflight"
	nodeApproval  = "finance_approval"
	nodeSync      = "payroll_sync"
)

// fixtureInstanceState is a RUNNING instance stopped on one node, with a
// business transaction, a pinned context and a checkpoint. It is the shape the
// spec's own inspector sketch shows: a promotion sitting on a node whose
// latest attempt failed.
func fixtureInstanceState() runtime.Instance {
	return runtime.Instance{
		TenantID:              fixtureTenant,
		InstanceID:            fixtureInstance,
		CellID:                "cell-local",
		WorkflowID:            "people.workflows.promote_into_management",
		WorkflowVersion:       17,
		CompiledPlanHash:      fixturePlanHash,
		BusinessSubjectRefs:   []string{"person:jane", "position:POS-MGR-204"},
		BusinessTransactionID: &fixtureTransation,
		ExecutionMode:         workflow.ModeExecute,
		RuntimeStatus:         runtime.InstanceRunning,
		CompletionDimensions: runtime.Dimensions{
			RequestState:     "APPROVED",
			ExecutionState:   "IN_PROGRESS",
			BusinessState:    "IN_PROGRESS",
			ConsistencyState: "CONSISTENT",
			ObligationState:  "PENDING",
		},
		InputRef:             "artifact:input/promotion-88191",
		VariableRevisionHead: 4,
		CurrentNodeIDs:       []string{nodeSync},
		EffectiveContextRef:  "ctx:legal.us-ca/2026.1",
		LastCheckpointRef:    "checkpoint:3",
		InstanceVersion:      12,
		CorrelationID:        "corr-88191",
		CreatedAt:            fixtureCreated,
		StartedAt:            &fixtureStarted,
	}
}

// fixtureNodes are the recorded attempts: two finished nodes and the failed
// third attempt of the node the instance is stopped on.
func fixtureNodes() []runtime.NodeExecution {
	node := func(id string, attempt int, step workflow.StepType, status runtime.NodeStatus) runtime.NodeExecution {
		n := runtime.NewNodeExecution(fixtureTenant, fixtureInstance, id, attempt, step, status)
		n.RecordedAt = fixtureCreated
		return n
	}

	preflight := node(nodePreflight, 1, workflow.StepTransform, runtime.NodeSucceeded)
	preflight.InputSnapshotRef = "artifact:input/preflight"
	preflight.OutputArtifactRef = "artifact:output/preflight"
	preflight.StartedAt = &fixtureStarted
	preflight.CompletedAt = &fixtureEnded
	preflight.TraceID = "trace:preflight"
	preflight.Refs = runtime.GovernanceRefs{
		AuthorizationDecisionID: "authz:preflight",
		PolicyRef:               "people.promotion.policy/1.0.0",
		ProposalRef:             "proposal:88191",
		BaselineRef:             "baseline:comp/2026-09-01",
	}

	approval := node(nodeApproval, 1, workflow.StepApproval, runtime.NodeSucceeded)
	approval.InputSnapshotRef = "artifact:input/approval"
	approval.OutputArtifactRef = "artifact:output/approval"
	approval.StartedAt = &fixtureStarted
	approval.CompletedAt = &fixtureEnded
	approval.TraceID = "trace:approval"
	approval.Refs = runtime.GovernanceRefs{
		AuthorizationDecisionID: "authz:approval",
		DecisionID:              "decision:finance-tier-2",
		HumanTaskID:             "task:finance-partner-7",
		PolicyRef:               "people.promotion.policy/1.0.0",
		ProposalRef:             "proposal:88191",
	}

	sync := node(nodeSync, 3, workflow.StepCapability, runtime.NodeFailed)
	sync.InputSnapshotRef = "artifact:input/payroll-sync"
	sync.StartedAt = &fixtureStarted
	sync.CompletedAt = &fixtureEnded
	sync.ErrorClass = "ADP_TIMEOUT"
	sync.TraceID = "trace:payroll-sync"
	sync.Refs = runtime.GovernanceRefs{
		AuthorizationDecisionID: "authz:payroll-sync",
		CapabilityExecutionID:   "capexec:payroll-sync/3",
		PolicyRef:               "payroll.sync.policy/2.1.0",
		ProposalRef:             "proposal:88191",
		BaselineRef:             "baseline:comp/2026-09-01",
		RepairRef:               "repair:payroll-88191",
		EffectRefs:              []string{"effect:payroll.worker_sync/88191"},
		RetryPolicyRef:          "policy.retry.effect.bounded/v1",
	}

	return []runtime.NodeExecution{preflight, approval, sync}
}

// operatorAuth is a fully authorized operator view.
func operatorAuth() inspect.Authorization {
	return inspect.AllowAll("policy.inspect.operator/1.0.0", "OPERATIONS", "principal:ops-7")
}

// golden compares rendered bytes against a checked-in file, or rewrites it
// under -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden %s drifted\n got:\n%s\nwant:\n%s", path, got, want)
	}
}
