package signal_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/steps/signal"
)

const (
	fsNodeSignal   = "signal_ack_received"
	fsEndCompleted = "end_completed"
	fsEndReview    = "end_review"
)

// signalOnlyDefinition is a minimal, self-contained P1B definition with
// exactly one SIGNAL node, compiled here (rather than in internal/workflow's
// own test package) so this package can prove [signal.FromCompiled] against a
// genuine [*workflow.CompiledWorkflow] plan without importing an unexported
// fixture.
func signalOnlyDefinition() workflow.Definition {
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
		WorkflowID:        "hcmnext.workflows.fixture_signal_only",
		Version:           1,
		Name:              "Signal-only binding fixture",
		InputSchema:       schema("SignalOnlyInput"),
		OutputSchema:      schema("SignalOnlyResult"),
		VariablesSchema:   schema("SignalOnlyVariables"),
		TenantScope:       "acme",
		OrganizationScope: "acme/engineering",
		RiskClass:         "LOW",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeExecute},
		TerminalProfile:   workflow.TerminalProfileExecute,
		StartNodeID:       fsNodeSignal,
		Inputs:            []workflow.Field{stringField("worker_id")},
		Outputs:           []workflow.Field{stringField("worker_id"), stringField("terminal_code")},
		Limits:            workflow.Limits{MaxFanOut: 4, MaxDepth: 10, MaxNodes: 12},

		FailurePolicyRef:      "policy.workflow.failure.execute/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.execute/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.signal_only/v1",

		Nodes: []workflow.Node{
			{
				ID:           fsNodeSignal,
				Type:         workflow.StepSignal,
				InputSchema:  schema("SignalInput"),
				OutputSchema: schema("SignalResult"),
				Signal: &workflow.SignalSpec{
					EventType:                "hcmnext.events.promotion_ack",
					CorrelationKeyExpression: "worker_id",
					ExpectedSchemaRef:        schema("PromotionAckPayload"),
					AcceptedSources:          []string{"hcmnext.integrations.hris"},
					Ordering:                 workflow.SignalOrderingMonotonicSequence,
					CloseAfterSeconds:        86400,
				},
				FailureRoute: fsEndReview,
			},
			{
				ID:     fsEndCompleted,
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
					CommitReceiptRef:  "receipt.fixture.signal_only/v1",
				},
			},
			{
				ID:     fsEndReview,
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
					CommitReceiptRef:  "receipt.fixture.signal_only/v1",
					RepairRefs:        []string{"repair.fixture.signal_only/v1"},
				},
			},
		},
		Edges: []workflow.Edge{
			{From: fsNodeSignal, To: fsEndCompleted, RouteKey: "SUCCEEDED"},
			{From: fsNodeSignal, To: fsEndReview, RouteKey: "TIMED_OUT"},
			{From: fsNodeSignal, To: fsEndReview, RouteKey: "CANCELLED"},
		},
	}
}

func mustCompileSignalOnly(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := workflow.Compile(signalOnlyDefinition(), workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatalf("compile signal-only fixture: %v", err)
	}
	return plan
}

// TestSignalBindingFromCompiledPlan proves [signal.FromCompiled] turns a
// genuine compiled SIGNAL node — internal/workflow/compile.go's
// CompiledSignal binding, reached the same way a runtime would, through
// [workflow.Compile] and [workflow.CompiledWorkflow.Node] — combined with
// per-instance context, into a [signal.SignalSubscription] this package's
// pure [signal.Accept] can evaluate signals against, and refuses every input
// that is not a well-formed SIGNAL node or a complete instance context.
func TestSignalBindingFromCompiledPlan(t *testing.T) {
	t.Run("GREEN_translates_a_compiled_signal_node", func(t *testing.T) {
		plan := mustCompileSignalOnly(t)
		node, ok := plan.Node(fsNodeSignal)
		if !ok {
			t.Fatal("plan has no SIGNAL node")
		}

		instanceCtx := signal.InstanceContext{
			Tenant:             "acme",
			WorkflowInstanceID: "wfi-0001",
			CorrelationValue:   "worker-42",
			ClosesAt:           mustInstant(t, "2026-06-18T12:00:00Z"),
		}
		sub, err := signal.FromCompiled(&node, instanceCtx)
		if err != nil {
			t.Fatalf("FromCompiled: %v", err)
		}
		if sub.NodeID != fsNodeSignal {
			t.Fatalf("node id = %q, want %q", sub.NodeID, fsNodeSignal)
		}
		if sub.Tenant != "acme" || sub.WorkflowInstanceID != "wfi-0001" {
			t.Fatalf("instance identity lost: %+v", sub)
		}
		if sub.EventType != "hcmnext.events.promotion_ack" {
			t.Fatalf("event type = %q", sub.EventType)
		}
		if sub.CorrelationKey != "worker_id" || sub.CorrelationValue != "worker-42" {
			t.Fatalf("correlation = %q/%q", sub.CorrelationKey, sub.CorrelationValue)
		}
		if sub.ExpectedSchemaRef != "PromotionAckPayload/v1/v1" {
			t.Fatalf("expected schema ref = %q", sub.ExpectedSchemaRef)
		}
		if len(sub.AcceptedSources) != 1 || sub.AcceptedSources[0] != "hcmnext.integrations.hris" {
			t.Fatalf("accepted sources = %v", sub.AcceptedSources)
		}
		if sub.Ordering != signal.OrderingMonotonicSequence {
			t.Fatalf("ordering = %s, want %s", sub.Ordering, signal.OrderingMonotonicSequence)
		}
		if !sub.ClosesAt.IsSet() || sub.ClosesAtText() != "2026-06-18T12:00:00Z" {
			t.Fatalf("closes at = %v", sub.ClosesAt)
		}
		if err := sub.Validate(); err != nil {
			t.Fatalf("translated subscription does not validate: %v", err)
		}

		// The translated subscription drives Accept exactly like any other:
		// an in-window, correctly-sourced, schema-matching, signed signal is
		// accepted.
		sig := signal.Signal{
			Tenant: "acme", Source: "hcmnext.integrations.hris",
			EventType: "hcmnext.events.promotion_ack", SchemaRef: "PromotionAckPayload/v1/v1",
			CorrelationKey: "worker_id", CorrelationValue: "worker-42",
			SequenceNumber: 1, Payload: []byte(`{"ack":true}`),
			ReceivedAt: mustInstant(t, "2026-06-17T12:00:00Z"),
		}
		sig.Signature = testVerifier.sign(sig.Payload)
		result, err := signal.Accept(sub, sig, nil, testVerifier, mustInstant(t, "2026-06-17T12:00:00Z"))
		if err != nil {
			t.Fatalf("Accept: %v", err)
		}
		if result.Status != signal.StatusAccepted {
			t.Fatalf("status = %s, want %s: %s", result.Status, signal.StatusAccepted, result.Reason)
		}
	})

	t.Run("RED_refuses_malformed_or_mistyped_input", func(t *testing.T) {
		plan := mustCompileSignalOnly(t)
		signalNode, _ := plan.Node(fsNodeSignal)
		endNode, _ := plan.Node(fsEndCompleted)
		completeCtx := signal.InstanceContext{Tenant: "acme", WorkflowInstanceID: "wfi-0001", CorrelationValue: "worker-42"}

		cases := []struct {
			name string
			node *workflow.CompiledNode
			ctx  signal.InstanceContext
		}{
			{"nil_node", nil, completeCtx},
			{"wrong_step_type", &endNode, completeCtx},
			{"no_signal_binding", &workflow.CompiledNode{ID: "x", Type: workflow.StepSignal}, completeCtx},
			{"no_tenant", &signalNode, signal.InstanceContext{WorkflowInstanceID: "wfi-0001", CorrelationValue: "worker-42"}},
			{"no_workflow_instance_id", &signalNode, signal.InstanceContext{Tenant: "acme", CorrelationValue: "worker-42"}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := signal.FromCompiled(tc.node, tc.ctx); err == nil {
					t.Fatal("expected an error, got nil")
				}
			})
		}

		t.Run("unknown_ordering", func(t *testing.T) {
			bad := signalNode
			cp := *signalNode.Signal
			cp.Ordering = "NOT_AN_ORDERING"
			bad.Signal = &cp
			if _, err := signal.FromCompiled(&bad, completeCtx); err == nil {
				t.Fatal("expected an error for an unknown ordering expectation")
			}
		})

		t.Run("no_accepted_sources", func(t *testing.T) {
			bad := signalNode
			cp := *signalNode.Signal
			cp.AcceptedSources = nil
			bad.Signal = &cp
			if _, err := signal.FromCompiled(&bad, completeCtx); err == nil {
				t.Fatal("expected an error for no accepted sources")
			}
		})
	})
}
