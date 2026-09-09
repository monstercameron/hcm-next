package frontier_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
)

// Node ids of the WAIT/SIGNAL activation fixture.
const (
	wsFrontierStart    = "start_transform"
	wsFrontierWait     = "wait_for_effective_date"
	wsFrontierSignal   = "signal_ack_received"
	wsFrontierComplete = "end_completed"
	wsFrontierDegraded = "end_degraded"
)

// waitSignalActivationDefinition is a small P1B chain — TRANSFORM -> WAIT ->
// SIGNAL -> END — built so a frontier walk can prove that activating a WAIT
// or SIGNAL successor emits exactly the scheduling intents step-types.md
// promises: TIMER_REQUIRED and SIGNAL_SUBSCRIPTION_REQUIRED, with no worker
// ever claiming or running either node first.
func waitSignalActivationDefinition() workflow.Definition {
	schema := func(name string) workflow.SchemaRef {
		return workflow.SchemaRef{SchemaID: name + "/v1", Version: 1, ProtobufFullName: "hcmnext.workflows.v1." + name}
	}
	stringField := func(path string) workflow.Field {
		return workflow.Field{Path: path, Type: workflow.ValueType{Kind: workflow.KindString}}
	}
	completion := func(request, execution, business, consistency, obligation string) map[string]string {
		return map[string]string{
			"RequestState": request, "ExecutionState": execution, "BusinessState": business,
			"ConsistencyState": consistency, "ObligationState": obligation,
		}
	}

	return workflow.Definition{
		WorkflowID:        "hcmnext.workflows.fixture_wait_signal_activation",
		Version:           1,
		Name:              "Wait/signal activation fixture",
		InputSchema:       schema("WaitSignalActivationInput"),
		OutputSchema:      schema("WaitSignalActivationResult"),
		VariablesSchema:   schema("WaitSignalActivationVariables"),
		TenantScope:       "acme",
		OrganizationScope: "acme/engineering",
		RiskClass:         "LOW",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeExecute},
		TerminalProfile:   workflow.TerminalProfileExecute,
		StartNodeID:       wsFrontierStart,
		Inputs:            []workflow.Field{stringField("worker_id")},
		Outputs:           []workflow.Field{stringField("worker_id"), stringField("terminal_code")},
		Limits:            workflow.Limits{MaxFanOut: 4, MaxDepth: 10, MaxNodes: 12},

		FailurePolicyRef:      "policy.workflow.failure.execute/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.execute/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.wait_signal_activation/v1",

		Nodes: []workflow.Node{
			{
				ID:           wsFrontierStart,
				Type:         workflow.StepTransform,
				InputSchema:  schema("StartInput"),
				OutputSchema: schema("StartResult"),
				Transform: &workflow.TransformSpec{
					TransformRef:         "fixture.transform.noop",
					Version:              1,
					NormalizationProfile: "normalization.fixture/v1",
					OutputTaint:          workflow.TaintTrusted,
					Limits:               workflow.TransformLimits{MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxSteps: 10},
				},
			},
			{
				ID:           wsFrontierWait,
				Type:         workflow.StepWait,
				InputSchema:  schema("WaitInput"),
				OutputSchema: schema("WaitResult"),
				Wait: &workflow.WaitSpec{
					WakeKind:              workflow.WaitWakeAtLocalDateTime,
					WakeLocalDate:         "2027-03-01",
					WakeLocalTime:         "09:00:00",
					Disambiguation:        "REJECT_GAP",
					ZoneID:                "America/New_York",
					ZoneTzdbVersion:       "2026a",
					CalendarRef:           "us-federal",
					CalendarVersion:       "2026.1",
					ReferenceUpdatePolicy: "REVIEW_REQUIRED",
				},
				FailureRoute: wsFrontierDegraded,
			},
			{
				ID:           wsFrontierSignal,
				Type:         workflow.StepSignal,
				InputSchema:  schema("SignalInput"),
				OutputSchema: schema("SignalResult"),
				Signal: &workflow.SignalSpec{
					EventType:                "hcmnext.events.promotion_ack",
					CorrelationKeyExpression: "worker_id",
					ExpectedSchemaRef:        schema("PromotionAckPayload"),
					AcceptedSources:          []string{"hcmnext.integrations.hris"},
					Ordering:                 workflow.SignalOrderingNone,
					CloseAfterSeconds:        86400,
				},
				FailureRoute: wsFrontierDegraded,
			},
			{
				ID:     wsFrontierComplete,
				Type:   workflow.StepEnd,
				Inputs: []workflow.Field{stringField("worker_id"), stringField("terminal_code")},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
					{Target: "terminal_code", Source: workflow.Source{Kind: workflow.SourceConstant, Constant: "COMPLETED", Type: workflow.ValueType{Kind: workflow.KindString}}},
				},
				End: &workflow.EndSpec{
					TerminalCode:      "COMPLETED",
					RuntimeStatus:     workflow.RuntimeCompleted,
					CompletionMapping: completion("APPROVED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"),
					CommitReceiptRef:  "receipt.fixture.wait_signal_activation/v1",
				},
			},
			{
				ID:     wsFrontierDegraded,
				Type:   workflow.StepEnd,
				Inputs: []workflow.Field{stringField("worker_id"), stringField("terminal_code")},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
					{Target: "terminal_code", Source: workflow.Source{Kind: workflow.SourceConstant, Constant: "COMMITTED_DEGRADED", Type: workflow.ValueType{Kind: workflow.KindString}}},
				},
				End: &workflow.EndSpec{
					TerminalCode:      "COMMITTED_DEGRADED",
					RuntimeStatus:     workflow.RuntimeCompleted,
					CompletionMapping: completion("APPROVED", "COMMITTED", "COMPLETED", "DEGRADED", "SATISFIED"),
					CommitReceiptRef:  "receipt.fixture.wait_signal_activation/v1",
					RepairRefs:        []string{"repair.fixture.wait_signal_activation/v1"},
				},
			},
		},
		Edges: []workflow.Edge{
			{From: wsFrontierStart, To: wsFrontierWait, RouteKey: "SUCCEEDED"},
			{From: wsFrontierStart, To: wsFrontierDegraded, RouteKey: "FAILED"},

			{From: wsFrontierWait, To: wsFrontierSignal, RouteKey: "SUCCEEDED"},
			{From: wsFrontierWait, To: wsFrontierDegraded, RouteKey: "LATE"},
			{From: wsFrontierWait, To: wsFrontierDegraded, RouteKey: "CANCELLED"},

			{From: wsFrontierSignal, To: wsFrontierComplete, RouteKey: "SUCCEEDED"},
			{From: wsFrontierSignal, To: wsFrontierDegraded, RouteKey: "TIMED_OUT"},
			{From: wsFrontierSignal, To: wsFrontierDegraded, RouteKey: "CANCELLED"},
		},
	}
}

// TestFrontierEmitsTimerAndSignalIntentsForCompiledNodes proves that
// activating a compiled WAIT or SIGNAL node — reached the ordinary way, by
// routing a predecessor's outcome through [frontier.Advance] — moves it
// straight to WAITING and emits exactly the scheduling intent step-types.md
// promises (§5 WAIT: "no executor sleeps"; §6 SIGNAL: a correlated
// subscription must be registered before the node can be woken), with no
// worker ever claiming or running either node first.
func TestFrontierEmitsTimerAndSignalIntentsForCompiledNodes(t *testing.T) {
	plan, err := workflow.Compile(waitSignalActivationDefinition(), workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	state, err := frontier.Seed(plan, "wfi-activation-0001")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if len(state.Frontier) != 1 || state.Frontier[0] != wsFrontierStart {
		t.Fatalf("seed frontier = %v, want [%s]", state.Frontier, wsFrontierStart)
	}

	// Complete the start node; its SUCCEEDED route activates the WAIT node.
	tr1, err := frontier.Advance(plan, state, frontier.NodeOutcome{NodeID: wsFrontierStart, Outcome: workflow.OutcomeSucceeded})
	if err != nil {
		t.Fatalf("advance start -> wait: %v", err)
	}
	waitIntent, ok := tr1.Intent(wsFrontierWait)
	if !ok {
		t.Fatalf("no intent recorded for the WAIT node; intents = %+v", tr1.Intents)
	}
	if waitIntent.Kind != frontier.IntentTimerRequired {
		t.Fatalf("wait intent kind = %s, want %s", waitIntent.Kind, frontier.IntentTimerRequired)
	}
	if waitIntent.Type != workflow.StepWait {
		t.Fatalf("wait intent type = %s, want %s", waitIntent.Type, workflow.StepWait)
	}
	waitStatus, ok := tr1.Next.Node(wsFrontierWait)
	if !ok || waitStatus.State != frontier.NodeWaiting {
		t.Fatalf("wait node state = %+v, want %s", waitStatus, frontier.NodeWaiting)
	}
	if len(tr1.Next.Frontier) != 1 || tr1.Next.Frontier[0] != wsFrontierWait {
		t.Fatalf("frontier after start completes = %v, want [%s]", tr1.Next.Frontier, wsFrontierWait)
	}

	// Complete the WAIT node itself (the FIRED resolution maps onto the
	// declared SUCCEEDED route — see wait.Resolution.ToNodeOutcome); its
	// route activates the SIGNAL node.
	tr2, err := frontier.Advance(plan, tr1.Next, frontier.NodeOutcome{NodeID: wsFrontierWait, Outcome: workflow.OutcomeSucceeded})
	if err != nil {
		t.Fatalf("advance wait -> signal: %v", err)
	}
	signalIntent, ok := tr2.Intent(wsFrontierSignal)
	if !ok {
		t.Fatalf("no intent recorded for the SIGNAL node; intents = %+v", tr2.Intents)
	}
	if signalIntent.Kind != frontier.IntentSignalSubscriptionRequired {
		t.Fatalf("signal intent kind = %s, want %s", signalIntent.Kind, frontier.IntentSignalSubscriptionRequired)
	}
	if signalIntent.Type != workflow.StepSignal {
		t.Fatalf("signal intent type = %s, want %s", signalIntent.Type, workflow.StepSignal)
	}
	signalStatus, ok := tr2.Next.Node(wsFrontierSignal)
	if !ok || signalStatus.State != frontier.NodeWaiting {
		t.Fatalf("signal node state = %+v, want %s", signalStatus, frontier.NodeWaiting)
	}
	if len(tr2.Next.Frontier) != 1 || tr2.Next.Frontier[0] != wsFrontierSignal {
		t.Fatalf("frontier after wait completes = %v, want [%s]", tr2.Next.Frontier, wsFrontierSignal)
	}

	// Complete the SIGNAL node (ACCEPTED maps onto the declared SUCCEEDED
	// route — see signal.Result.ToNodeOutcome); the workflow reaches its
	// terminal.
	tr3, err := frontier.Advance(plan, tr2.Next, frontier.NodeOutcome{NodeID: wsFrontierSignal, Outcome: workflow.OutcomeSucceeded})
	if err != nil {
		t.Fatalf("advance signal -> end: %v", err)
	}
	completeIntent, ok := tr3.Intent(wsFrontierComplete)
	if !ok || completeIntent.Kind != frontier.IntentReady {
		t.Fatalf("intent for the activated END node = %+v, want %s", completeIntent, frontier.IntentReady)
	}

	// The END node is ready work like any other; a worker claims it and mints
	// the terminal artifact.
	tr4, err := frontier.Advance(plan, tr3.Next, frontier.NodeOutcome{NodeID: wsFrontierComplete})
	if err != nil {
		t.Fatalf("advance end: %v", err)
	}
	if !tr4.Complete || tr4.Terminal.TerminalCode != "COMPLETED" {
		t.Fatalf("final transition = %+v, want a COMPLETED terminal", tr4)
	}
}
