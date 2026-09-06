package replay

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// The fixture identities are fixed rather than random so that a golden trace
// digest is a contract. A replay's digest covers the tenant, the instance and
// the historical intent, which is exactly the point -- two runs of the same
// plan must not produce the same trace -- so they cannot be freshly generated
// per test run without making the golden meaningless.
var (
	fixtureTenant   = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	fixtureInstance = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

const (
	fixtureHistoricalIntent = "intent:promotion:jane-doe:1"
	fixtureReplayIntent     = "intent:replay:promotion:jane-doe:1"
	fixtureTerminal         = "SIMULATION_APPROVAL_REQUIRED"
)

// fixtureStart is the pinned instant every recorded attempt is stamped
// relative to. This package reads no wall clock, and neither does its test
// data.
var fixtureStart = time.Date(2026, 9, 3, 15, 0, 0, 0, time.UTC)

// promotionPlan compiles the Promotion reference fixture the whole ticket is
// specified against.
func promotionPlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	registry, err := newBootstrapRegistry()
	if err != nil {
		t.Fatalf("bootstrap capability registry: %v", err)
	}
	plan, err := workflow.CompilePromotionReference(registry)
	if err != nil {
		t.Fatalf("compile Promotion fixture: %v", err)
	}
	return plan
}

// newBootstrapRegistry is the capability registry the Promotion fixture
// compiles against.
func newBootstrapRegistry() (*capability.Registry, error) { return capability.NewBootstrapRegistry() }

// digestOf builds a plausible artifact digest for a node. Replay never reads a
// digest's content -- it carries it -- so the value only has to be stable.
func digestOf(nodeID string) string {
	return "sha256:" + strings.Repeat("0", 64-len(nodeID)) + nodeID
}

// promotionRecord is the recorded run every non-pgtest test in this package
// replays: Jane Doe's management promotion, which changes grade, escalates
// past the finance threshold and ends at the terminal that reports a pending
// Finance Partner approval. It is the same walk
// planning/reference-workflows/promote-into-management.md describes.
func promotionRecord(t *testing.T, plan *workflow.CompiledWorkflow) Record {
	t.Helper()
	steps := []struct {
		nodeID   string
		stepType workflow.StepType
		route    workflow.Outcome
	}{
		{workflow.PromotionNodeSnapshotWorker, workflow.StepCapability, workflow.OutcomeSucceeded},
		{workflow.PromotionNodeSimulateComp, workflow.StepCapability, workflow.OutcomeSucceeded},
		{workflow.PromotionNodeEvaluateBand, workflow.StepCapability, workflow.OutcomeSucceeded},
		{workflow.PromotionNodeBuildProposal, workflow.StepTransform, workflow.OutcomeSucceeded},
		{workflow.PromotionNodeRaiseThreshold, workflow.StepDecision, "EXCEEDS_THRESHOLD"},
		{workflow.PromotionNodeEndApproval, workflow.StepEnd, ""},
	}
	nodes := make([]NodeRecord, 0, len(steps))
	for i, s := range steps {
		nodes = append(nodes, NodeRecord{
			Sequence: i + 1, NodeID: s.nodeID, Attempt: 1, StepType: s.stepType,
			RouteKey: string(s.route), OutputDigest: digestOf(s.nodeID),
			RecordedAt: fixtureStart.Add(time.Duration(i) * time.Minute),
		})
	}
	return Record{
		TenantID: fixtureTenant, InstanceID: fixtureInstance,
		WorkflowID: plan.WorkflowID, WorkflowVersion: plan.Version,
		CompiledPlanDigest: plan.Digest(),
		CorrelationID:      "correlation:promotion:jane-doe",
		HistoricalIntentID: fixtureHistoricalIntent,
		ExecutionMode:      workflow.ModeExecute,
		FinalStatus:        runtime.InstanceCompleted,
		TerminalCode:       fixtureTerminal,
		InputRef:           "sha256:" + strings.Repeat("1", 64),
		Nodes:              nodes,
		Checkpoints: []Checkpoint{{
			Sequence: 1, Kind: "SAFE_POINT",
			StateDigest:     "sha256:" + strings.Repeat("2", 64),
			FrontierDigest:  "sha256:" + strings.Repeat("3", 64),
			VariableDigest:  "sha256:" + strings.Repeat("4", 64),
			InstanceVersion: 6, TakenAt: fixtureStart.Add(5 * time.Minute),
		}},
	}
}

// pausedPromotionRecord is the same run stopped after the band evaluation,
// with a standing pause and one open frontier node. It is what
// [StatusPausedAtFrontier] is proved against.
func pausedPromotionRecord(t *testing.T, plan *workflow.CompiledWorkflow) Record {
	t.Helper()
	rec := promotionRecord(t, plan)
	rec.Nodes = rec.Nodes[:3]
	rec.FinalStatus = runtime.InstancePaused
	rec.TerminalCode = ""
	rec.Frontier = []FrontierEntry{{
		NodeID: workflow.PromotionNodeBuildProposal, State: "READY", Sequence: 4,
		EnteredAt: fixtureStart.Add(3 * time.Minute),
	}}
	return rec
}

// replayContract is the REPLAY contract every admitted test runs under. TEST
// is the right environment for a package's own suite: its contract is the
// production REPLAY row with a pinned clock, not a bypass.
func replayContract(t *testing.T) intent.ModeContract {
	t.Helper()
	c, err := ContractFor(intent.EnvironmentProduction)
	if err != nil {
		t.Fatalf("REPLAY contract: %v", err)
	}
	return c
}

// replayDefinition is a zero-effect definition that allows every mode, so a
// refusal in a test is always the contract's doing rather than the
// definition's.
func replayDefinition() intent.Definition {
	return intent.Definition{
		Ref:          intent.Ref{TypeID: "hcmnext.people.promote_into_management", Version: 1},
		EffectClass:  intent.EffectClassZero,
		AllowedModes: intent.Modes(),
	}
}

// replayInstance is the replay's own identity: a new intent that names the
// historical one as its cause and is not that intent.
func replayInstance() intent.Instance {
	cause := fixtureHistoricalIntent
	return intent.Instance{
		IntentID:      fixtureReplayIntent,
		CausationID:   &cause,
		ExecutionMode: intent.ModeReplay,
	}
}

// newReplayer builds a replayer over a record, with every admission input at
// its ordinary value.
func newReplayer(t *testing.T, plan *workflow.CompiledWorkflow, rec Record) *Replayer {
	t.Helper()
	r, err := New(Options{
		Plan: plan, Source: NewMemorySource(rec),
		Contract: replayContract(t), Definition: replayDefinition(), Instance: replayInstance(),
	})
	if err != nil {
		t.Fatalf("New replayer: %v", err)
	}
	return r
}

// nodeIDs renders a trace's node ids, for a readable failure message.
func nodeIDs(tr Trace) []string {
	out := make([]string, 0, len(tr.Entries))
	for _, e := range tr.Entries {
		out = append(out, e.NodeID)
	}
	return out
}

// joinDeclarations is here so the fixture's use of the frontier package is
// explicit; the Promotion plan declares no JOIN, so it is always empty.
var _ = []frontier.JoinDeclaration(nil)
