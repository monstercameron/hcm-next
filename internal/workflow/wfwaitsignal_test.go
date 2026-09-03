package workflow_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

// Node ids of the WAIT/SIGNAL prototype fixture.
const (
	wsWorkflowID   = "hcmnext.workflows.fixture_wait_signal_prototype"
	wsNodeWait     = "wait_for_effective_date"
	wsNodeSignal   = "signal_ack_received"
	wsEndCompleted = "end_completed"
	wsEndDegraded  = "end_degraded"
)

// waitSignalDefinition is a small P1B definition exercising exactly one WAIT
// node and one SIGNAL node: WAIT suspends until a local business
// date/time, then a SIGNAL waits for a correlated external acknowledgement.
// It exists so the compiler's WAIT/SIGNAL binding (definition.go's
// WaitSpec/SignalSpec, compile.go's CompiledWait/CompiledSignal) has a
// fixture beyond the zero-effect, WAIT/SIGNAL-free promotion reference.
func waitSignalDefinition() workflow.Definition {
	return workflow.Definition{
		WorkflowID:        wsWorkflowID,
		Version:           1,
		Name:              "Wait and signal prototype fixture",
		InputSchema:       fixtureSchemaRef("WaitSignalInput"),
		OutputSchema:      fixtureSchemaRef("WaitSignalResult"),
		VariablesSchema:   fixtureSchemaRef("WaitSignalVariables"),
		TenantScope:       "acme",
		OrganizationScope: "acme/engineering",
		RiskClass:         "LOW",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeExecute},
		TerminalProfile:   workflow.TerminalProfileExecute,
		StartNodeID:       wsNodeWait,
		Inputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
		},
		Outputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
			{Path: "terminal_code", Type: stringType()},
		},
		Limits:                workflow.Limits{MaxFanOut: 4, MaxDepth: 10, MaxNodes: 12},
		FailurePolicyRef:      "policy.workflow.failure.execute/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.execute/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.wait_signal/v1",
		Nodes: []workflow.Node{
			{
				ID:             wsNodeWait,
				Type:           workflow.StepWait,
				InputSchema:    fixtureSchemaRef("WaitForEffectiveDateInput"),
				OutputSchema:   fixtureSchemaRef("WaitForEffectiveDateResult"),
				DeclaredEffect: capability.EffectPure,
				Wait: &workflow.WaitSpec{
					WakeKind:              workflow.WaitWakeAtLocalDateTime,
					WakeLocalDate:         "2027-03-01",
					WakeLocalTime:         "09:00:00",
					Disambiguation:        "REJECT_GAP",
					ZoneID:                "America/New_York",
					ZoneTzdbVersion:       "2025b",
					CalendarRef:           "us_federal",
					CalendarVersion:       "2025.1",
					ReferenceUpdatePolicy: "REVIEW_REQUIRED",
				},
				FailureRoute: wsEndDegraded,
			},
			{
				ID:             wsNodeSignal,
				Type:           workflow.StepSignal,
				InputSchema:    fixtureSchemaRef("SignalAckInput"),
				OutputSchema:   fixtureSchemaRef("SignalAckResult"),
				DeclaredEffect: capability.EffectPure,
				Signal: &workflow.SignalSpec{
					EventType:                "hcmnext.events.promotion_ack",
					CorrelationKeyExpression: "worker_id",
					ExpectedSchemaRef:        fixtureSchemaRef("PromotionAckPayload"),
					AcceptedSources:          []string{"hcmnext.integrations.hris"},
					Ordering:                 workflow.SignalOrderingNone,
					CloseAfterSeconds:        86400,
				},
				FailureRoute: wsEndDegraded,
			},
			{
				ID:            wsEndCompleted,
				Type:          workflow.StepEnd,
				Inputs:        fixtureTerminalInputs(),
				InputMappings: fixtureTerminalMappings("COMPLETED"),
				End: &workflow.EndSpec{
					TerminalCode:      "COMPLETED",
					RuntimeStatus:     workflow.RuntimeCompleted,
					CompletionMapping: fixtureCompletion("APPROVED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"),
					CommitReceiptRef:  "receipt.fixture.wait_signal/v1",
				},
			},
			{
				ID:            wsEndDegraded,
				Type:          workflow.StepEnd,
				Inputs:        fixtureTerminalInputs(),
				InputMappings: fixtureTerminalMappings("COMMITTED_DEGRADED"),
				End: &workflow.EndSpec{
					TerminalCode:      "COMMITTED_DEGRADED",
					RuntimeStatus:     workflow.RuntimeCompleted,
					CompletionMapping: fixtureCompletion("APPROVED", "COMMITTED", "COMPLETED", "DEGRADED", "SATISFIED"),
					CommitReceiptRef:  "receipt.fixture.wait_signal/v1",
					RepairRefs:        []string{"repair.fixture.wait_signal/v1"},
				},
			},
		},
		Edges: []workflow.Edge{
			{From: wsNodeWait, To: wsNodeSignal, RouteKey: "SUCCEEDED"},
			{From: wsNodeWait, To: wsEndDegraded, RouteKey: "LATE"},
			{From: wsNodeWait, To: wsEndDegraded, RouteKey: "CANCELLED"},

			{From: wsNodeSignal, To: wsEndCompleted, RouteKey: "SUCCEEDED"},
			{From: wsNodeSignal, To: wsEndDegraded, RouteKey: "TIMED_OUT"},
			{From: wsNodeSignal, To: wsEndDegraded, RouteKey: "CANCELLED"},
		},
	}
}

func waitSignalOptions() workflow.Options {
	return workflow.Options{Phase: workflow.PhaseP1B}
}

// TestWaitAndSignalNodesCompileWithExplicitRoutes proves WAIT and SIGNAL are
// first-class in the compiler: a plan containing one of each compiles,
// carries a fully populated CompiledWait/CompiledSignal binding, digests
// deterministically, round-trips through the document loader, and still
// enforces WF-RUN-024's "no implicit route" — a missing or unrecognized
// outcome edge on either node type is a compile error — exactly as it
// already does for every other step type.
func TestWaitAndSignalNodesCompileWithExplicitRoutes(t *testing.T) {
	t.Run("GREEN_compiles_with_populated_bindings", func(t *testing.T) {
		plan, err := workflow.Compile(waitSignalDefinition(), waitSignalOptions())
		if err != nil {
			t.Fatalf("compile: %v", err)
		}

		waitNode, ok := plan.Node(wsNodeWait)
		if !ok {
			t.Fatal("plan has no WAIT node")
		}
		if waitNode.Wait == nil {
			t.Fatal("compiled WAIT node carries no Wait binding")
		}
		wantRoutes := []string{"CANCELLED", "LATE", "SUCCEEDED"}
		if !reflect.DeepEqual(waitNode.Routes, wantRoutes) {
			t.Fatalf("wait routes = %v, want %v", waitNode.Routes, wantRoutes)
		}
		if waitNode.Wait.WakeKind != workflow.WaitWakeAtLocalDateTime ||
			waitNode.Wait.WakeLocalDate != "2027-03-01" ||
			waitNode.Wait.WakeLocalTime != "09:00:00" ||
			waitNode.Wait.Disambiguation != "REJECT_GAP" ||
			waitNode.Wait.ZoneID != "America/New_York" ||
			waitNode.Wait.ZoneTzdbVersion != "2025b" ||
			waitNode.Wait.CalendarRef != "us_federal" ||
			waitNode.Wait.CalendarVersion != "2025.1" ||
			waitNode.Wait.ReferenceUpdatePolicy != "REVIEW_REQUIRED" {
			t.Fatalf("compiled WAIT binding lost fields: %+v", waitNode.Wait)
		}
		if waitNode.EffectClass != capability.EffectPure {
			t.Fatalf("a WAIT node never carries a write effect, got %s", waitNode.EffectClass)
		}

		signalNode, ok := plan.Node(wsNodeSignal)
		if !ok {
			t.Fatal("plan has no SIGNAL node")
		}
		if signalNode.Signal == nil {
			t.Fatal("compiled SIGNAL node carries no Signal binding")
		}
		wantSignalRoutes := []string{"CANCELLED", "SUCCEEDED", "TIMED_OUT"}
		if !reflect.DeepEqual(signalNode.Routes, wantSignalRoutes) {
			t.Fatalf("signal routes = %v, want %v", signalNode.Routes, wantSignalRoutes)
		}
		if signalNode.Signal.EventType != "hcmnext.events.promotion_ack" ||
			signalNode.Signal.CorrelationKeyExpression != "worker_id" ||
			signalNode.Signal.ExpectedSchemaRef.SchemaID != fixtureSchemaRef("PromotionAckPayload").SchemaID ||
			len(signalNode.Signal.AcceptedSources) != 1 ||
			signalNode.Signal.AcceptedSources[0] != "hcmnext.integrations.hris" ||
			signalNode.Signal.Ordering != string(workflow.SignalOrderingNone) ||
			signalNode.Signal.CloseAfterSeconds != 86400 {
			t.Fatalf("compiled SIGNAL binding lost fields: %+v", signalNode.Signal)
		}
		if signalNode.EffectClass != capability.EffectPure {
			t.Fatalf("a SIGNAL node never carries a write effect, got %s", signalNode.EffectClass)
		}
	})

	t.Run("GREEN_digest_is_deterministic_and_survives_the_document_round_trip", func(t *testing.T) {
		planA, err := workflow.Compile(waitSignalDefinition(), waitSignalOptions())
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		planB, err := workflow.Compile(waitSignalDefinition(), waitSignalOptions())
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if planA.Digest() == "" {
			t.Fatal("a compiled plan always carries a digest")
		}
		if planA.Digest() != planB.Digest() {
			t.Fatal("compiling the same definition twice must produce the same digest")
		}

		document, err := workflow.Marshal(waitSignalDefinition())
		if err != nil {
			t.Fatalf("marshal fixture definition: %v", err)
		}
		goldenBytes(t, "wait_signal_fixture.json", document)

		loaded, err := workflow.LoadFile(filepath.Join("testdata", "wait_signal_fixture.json"))
		if err != nil {
			t.Fatalf("load fixture definition: %v", err)
		}
		if !reflect.DeepEqual(loaded, waitSignalDefinition()) {
			t.Fatal("the document shape and the Go values are not the same definition")
		}
		loadedPlan, err := workflow.Compile(loaded, waitSignalOptions())
		if err != nil {
			t.Fatalf("compile loaded fixture: %v", err)
		}
		if loadedPlan.Digest() != planA.Digest() {
			t.Fatal("loading the fixture from its document form must not change its compiled digest")
		}
	})

	t.Run("RED_wait_route_with_no_declared_edge_is_a_compile_error", func(t *testing.T) {
		def := waitSignalDefinition()
		edgeRef(t, &def, wsNodeWait, "LATE").RouteKey = "BOGUS_ROUTE"
		d := mustReject(t, def, waitSignalOptions(), workflow.CodeMissingRoute)
		if !d.Has(workflow.CodeUnknownRoute) {
			t.Fatalf("expected the renamed edge to also be flagged as an unknown route, got %v", d.Codes())
		}
	})

	t.Run("RED_signal_route_with_no_declared_edge_is_a_compile_error", func(t *testing.T) {
		def := waitSignalDefinition()
		edgeRef(t, &def, wsNodeSignal, "TIMED_OUT").RouteKey = "BOGUS_ROUTE"
		mustReject(t, def, waitSignalOptions(), workflow.CodeMissingRoute)
	})

	t.Run("RED_wait_spec_missing_dataset_identity", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*workflow.Node)
		}{
			{"no_zone", func(n *workflow.Node) { n.Wait.ZoneID = "" }},
			{"no_calendar", func(n *workflow.Node) { n.Wait.CalendarRef = "" }},
			{"no_reference_update_policy", func(n *workflow.Node) { n.Wait.ReferenceUpdatePolicy = "" }},
			{"no_disambiguation", func(n *workflow.Node) { n.Wait.Disambiguation = "" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				def := waitSignalDefinition()
				tc.mutate(nodeRef(t, &def, wsNodeWait))
				mustReject(t, def, waitSignalOptions(), workflow.CodeUnresolvedRef)
			})
		}
	})

	t.Run("RED_signal_spec_missing_correlation_identity", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*workflow.Node)
		}{
			{"no_event_type", func(n *workflow.Node) { n.Signal.EventType = "" }},
			{"no_correlation_key", func(n *workflow.Node) { n.Signal.CorrelationKeyExpression = "" }},
			{"no_accepted_sources", func(n *workflow.Node) { n.Signal.AcceptedSources = nil }},
			{"no_ordering", func(n *workflow.Node) { n.Signal.Ordering = "" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				def := waitSignalDefinition()
				tc.mutate(nodeRef(t, &def, wsNodeSignal))
				mustReject(t, def, waitSignalOptions(), workflow.CodeUnresolvedRef)
			})
		}
	})
}
