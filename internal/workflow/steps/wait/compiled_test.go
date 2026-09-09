package wait_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
)

const (
	fwNodeWait     = "wait_for_effective_date"
	fwEndCompleted = "end_completed"
	fwEndReview    = "end_review"
)

// waitOnlyDefinition is a minimal, self-contained P1B definition with exactly
// one WAIT node, compiled here (rather than in internal/workflow's own test
// package) so this package can prove [wait.FromCompiled] against a genuine
// [*workflow.CompiledWorkflow] plan without importing an unexported fixture.
func waitOnlyDefinition() workflow.Definition {
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
		WorkflowID:        "hcmnext.workflows.fixture_wait_only",
		Version:           1,
		Name:              "Wait-only binding fixture",
		InputSchema:       schema("WaitOnlyInput"),
		OutputSchema:      schema("WaitOnlyResult"),
		VariablesSchema:   schema("WaitOnlyVariables"),
		TenantScope:       "acme",
		OrganizationScope: "acme/engineering",
		RiskClass:         "LOW",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeExecute},
		TerminalProfile:   workflow.TerminalProfileExecute,
		StartNodeID:       fwNodeWait,
		Inputs:            []workflow.Field{stringField("worker_id")},
		Outputs:           []workflow.Field{stringField("worker_id"), stringField("terminal_code")},
		Limits:            workflow.Limits{MaxFanOut: 4, MaxDepth: 10, MaxNodes: 12},

		FailurePolicyRef:      "policy.workflow.failure.execute/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.execute/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.wait_only/v1",

		Nodes: []workflow.Node{
			{
				ID:           fwNodeWait,
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
				FailureRoute: fwEndReview,
			},
			{
				ID:     fwEndCompleted,
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
					CommitReceiptRef:  "receipt.fixture.wait_only/v1",
				},
			},
			{
				ID:     fwEndReview,
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
					CommitReceiptRef:  "receipt.fixture.wait_only/v1",
					RepairRefs:        []string{"repair.fixture.wait_only/v1"},
				},
			},
		},
		Edges: []workflow.Edge{
			{From: fwNodeWait, To: fwEndCompleted, RouteKey: "SUCCEEDED"},
			{From: fwNodeWait, To: fwEndReview, RouteKey: "LATE"},
			{From: fwNodeWait, To: fwEndReview, RouteKey: "CANCELLED"},
		},
	}
}

func mustCompileWaitOnly(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := workflow.Compile(waitOnlyDefinition(), workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatalf("compile wait-only fixture: %v", err)
	}
	return plan
}

// TestWaitBindingFromCompiledPlan proves [wait.FromCompiled] turns a genuine
// compiled WAIT node — internal/workflow/compile.go's CompiledWait binding,
// reached the same way a runtime would, through [workflow.Compile] and
// [workflow.CompiledWorkflow.Node] — into a [wait.CompiledWaitNode] this
// package's pure functions can drive end to end, and refuses every input
// that is not a well-formed WAIT node.
func TestWaitBindingFromCompiledPlan(t *testing.T) {
	t.Run("GREEN_translates_a_local_datetime_wake_condition", func(t *testing.T) {
		plan := mustCompileWaitOnly(t)
		node, ok := plan.Node(fwNodeWait)
		if !ok {
			t.Fatal("plan has no WAIT node")
		}

		cwn, err := wait.FromCompiled(&node)
		if err != nil {
			t.Fatalf("FromCompiled: %v", err)
		}
		if cwn.NodeID != fwNodeWait {
			t.Fatalf("node id = %q, want %q", cwn.NodeID, fwNodeWait)
		}
		if cwn.Kind() != wait.WakeAtLocalDateTime {
			t.Fatalf("kind = %s, want %s", cwn.Kind(), wait.WakeAtLocalDateTime)
		}
		if cwn.Zone.ID != "America/New_York" || cwn.Zone.TzdbVersion != "2026a" {
			t.Fatalf("zone = %+v", cwn.Zone)
		}
		if cwn.Calendar.Ref != "us-federal" || cwn.Calendar.Version != "2026.1" {
			t.Fatalf("calendar = %+v", cwn.Calendar)
		}
		if cwn.Policy != values.ReferenceUpdateReviewRequired {
			t.Fatalf("policy = %s, want %s", cwn.Policy, values.ReferenceUpdateReviewRequired)
		}
		if cwn.Disambiguation != values.DisambiguationRejectGap {
			t.Fatalf("disambiguation = %v", cwn.Disambiguation)
		}
		if !cwn.WakeLocalDate.IsSet() || cwn.WakeLocalDate.String() != "2027-03-01" {
			t.Fatalf("wake local date = %v", cwn.WakeLocalDate)
		}
		if !cwn.WakeLocalTime.IsSet() || cwn.WakeLocalTime.String() != "09:00:00" {
			t.Fatalf("wake local time = %v", cwn.WakeLocalTime)
		}

		// The plan's own identity is not carried on a lone CompiledNode; a
		// caller that has the plan sets it before the node is used to compute
		// a timer requirement.
		cwn.WorkflowID = plan.WorkflowID
		cwn.WorkflowVersion = plan.Version
		if err := cwn.Validate(); err != nil {
			t.Fatalf("translated node does not validate: %v", err)
		}

		req, err := wait.ComputeTimerRequirement(cwn, values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"})
		if err != nil {
			t.Fatalf("ComputeTimerRequirement: %v", err)
		}
		if req.ReviewRequired {
			t.Fatalf("an ordinary local time far from a DST transition must not require review: %s", req.ReviewReason)
		}
		if req.Digest == "" {
			t.Fatal("a computed requirement always carries a digest")
		}
	})

	t.Run("GREEN_translates_a_fixed_instant_wake_condition", func(t *testing.T) {
		def := waitOnlyDefinition()
		for i := range def.Nodes {
			if def.Nodes[i].ID != fwNodeWait {
				continue
			}
			def.Nodes[i].Wait = &workflow.WaitSpec{
				WakeKind:              workflow.WaitWakeAtInstant,
				WakeInstant:           "2027-03-01T14:00:00Z",
				ZoneID:                "America/New_York",
				ZoneTzdbVersion:       "2026a",
				CalendarRef:           "us-federal",
				CalendarVersion:       "2026.1",
				ReferenceUpdatePolicy: "PIN",
			}
		}
		plan, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1B})
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		node, _ := plan.Node(fwNodeWait)
		cwn, err := wait.FromCompiled(&node)
		if err != nil {
			t.Fatalf("FromCompiled: %v", err)
		}
		if cwn.Kind() != wait.WakeAtInstant {
			t.Fatalf("kind = %s, want %s", cwn.Kind(), wait.WakeAtInstant)
		}
		if !cwn.WakeInstant.IsSet() || cwn.WakeInstant.String() != "2027-03-01T14:00:00Z" {
			t.Fatalf("wake instant = %v", cwn.WakeInstant)
		}
	})

	t.Run("RED_refuses_malformed_or_mistyped_input", func(t *testing.T) {
		plan := mustCompileWaitOnly(t)
		waitNode, _ := plan.Node(fwNodeWait)
		endNode, _ := plan.Node(fwEndCompleted)

		cases := []struct {
			name string
			node *workflow.CompiledNode
		}{
			{"nil_node", nil},
			{"wrong_step_type", &endNode},
			{"no_wait_binding", &workflow.CompiledNode{ID: "x", Type: workflow.StepWait}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := wait.FromCompiled(tc.node); err == nil {
					t.Fatal("expected an error, got nil")
				}
			})
		}

		t.Run("unknown_zone", func(t *testing.T) {
			bad := waitNode
			cp := *waitNode.Wait
			cp.ZoneID = "Not/A_Real_Zone"
			bad.Wait = &cp
			if _, err := wait.FromCompiled(&bad); err == nil {
				t.Fatal("expected an error for an unloadable zone")
			}
		})

		t.Run("unknown_reference_update_policy", func(t *testing.T) {
			bad := waitNode
			cp := *waitNode.Wait
			cp.ReferenceUpdatePolicy = "NOT_A_POLICY"
			bad.Wait = &cp
			if _, err := wait.FromCompiled(&bad); err == nil {
				t.Fatal("expected an error for an unknown reference-update policy")
			}
		})

		t.Run("unknown_wake_kind", func(t *testing.T) {
			bad := waitNode
			cp := *waitNode.Wait
			cp.WakeKind = "NOT_A_KIND"
			bad.Wait = &cp
			if _, err := wait.FromCompiled(&bad); err == nil {
				t.Fatal("expected an error for an unknown wake_kind")
			}
		})
	})
}
