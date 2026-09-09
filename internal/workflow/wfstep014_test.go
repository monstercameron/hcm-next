package workflow_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func observeNode(t *testing.T) workflow.Node {
	t.Helper()
	def := workflow.PromotionReferenceDefinition()
	return *nodeRef(t, &def, workflow.PromotionNodeObserveDrift)
}

// TestTodo_WF_STEP_014 proves planning/todos.md WF-STEP-014: a submission
// receipt or transport acknowledgement is never accepted as observed business
// state, a stale or unavailable result never becomes a pass, and retry
// exhaustion ends in a degraded or repair state rather than a false
// completion.
func TestTodo_WF_STEP_014(t *testing.T) {
	t.Run("RED_submission_receipt_is_not_an_observation", func(t *testing.T) {
		for _, kind := range []workflow.ObservationEvidenceKind{
			workflow.EvidenceSubmissionReceipt,
			workflow.EvidenceTransportAck,
		} {
			t.Run(string(kind), func(t *testing.T) {
				node := observeNode(t)
				node.Observe.EvidenceKind = kind
				requireCode(t, workflow.CheckStepConformance(node, nil), workflow.CodeReceiptIsNotObservation)
			})
		}
	})

	t.Run("RED_observation_without_a_source_authority", func(t *testing.T) {
		node := observeNode(t)
		node.Observe.SourceAuthority = ""
		requireCode(t, workflow.CheckStepConformance(node, nil), workflow.CodeReceiptIsNotObservation)
	})

	t.Run("RED_stale_result_can_become_a_pass", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*workflow.Node)
		}{
			{"no_freshness_bound", func(n *workflow.Node) { n.Observe.MaxAgeSeconds = 0 }},
			{"no_required_watermarks", func(n *workflow.Node) { n.Observe.RequiredWatermarks = nil }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				node := observeNode(t)
				tc.mutate(&node)
				requireCode(t, workflow.CheckStepConformance(node, nil), workflow.CodeStaleObservationAccepted)
			})
		}
	})

	t.Run("RED_no_expected_state_or_comparison_profile", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*workflow.Node)
		}{
			{"no_expected_state", func(n *workflow.Node) { n.Observe.ExpectedStateFields = nil }},
			{"expected_state_is_not_an_input", func(n *workflow.Node) {
				n.Observe.ExpectedStateFields = []string{"not_an_input"}
			}},
			{"no_comparison_profile", func(n *workflow.Node) { n.Observe.ComparisonProfile = "" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				node := observeNode(t)
				tc.mutate(&node)
				requireCode(t, workflow.CheckStepConformance(node, nil), workflow.CodeUnresolvedRef)
			})
		}
	})

	t.Run("RED_degraded_outcome_collapsed_into_pass", func(t *testing.T) {
		for _, route := range []string{"UNKNOWN", "PARTIAL", "FAIL"} {
			t.Run(route, func(t *testing.T) {
				def := workflow.PromotionReferenceDefinition()
				pass := edgeRef(t, &def, workflow.PromotionNodeObserveDrift, "PASS")
				edgeRef(t, &def, workflow.PromotionNodeObserveDrift, route).To = pass.To
				mustReject(t, def, promotionOptions(t), workflow.CodeDegradedCollapsedToPass)
			})
		}
	})

	t.Run("GREEN_four_outcomes_with_watermark_and_reconciliation", func(t *testing.T) {
		plan := mustCompilePromotion(t)
		node, ok := plan.Node(workflow.PromotionNodeObserveDrift)
		if !ok {
			t.Fatal("plan has no observe node")
		}
		want := []string{"FAIL", "PARTIAL", "PASS", "UNKNOWN"}
		if len(node.Routes) != len(want) {
			t.Fatalf("routes\n got %v\nwant %v", node.Routes, want)
		}
		for i := range want {
			if node.Routes[i] != want[i] {
				t.Fatalf("routes\n got %v\nwant %v", node.Routes, want)
			}
		}
		if node.Observe == nil {
			t.Fatal("compiled OBSERVE carries no observation binding")
		}
		if node.Observe.EvidenceKind != workflow.EvidenceAuthoritativeRead {
			t.Fatalf("evidence kind %q", node.Observe.EvidenceKind)
		}
		if len(node.Observe.RequiredWatermarks) == 0 || node.Observe.MaxAgeSeconds == 0 {
			t.Fatalf("a compiled observation carries watermarks and a freshness bound, got %+v", node.Observe)
		}
		if node.Observe.ComparisonProfile == "" {
			t.Fatal("a compiled observation names its reconciliation comparison profile")
		}
	})

	t.Run("REFACTOR_retry_exhaustion_creates_a_degraded_or_repair_state", func(t *testing.T) {
		t.Run("no_exhaustion_route_declared", func(t *testing.T) {
			def := workflow.PromotionReferenceDefinition()
			nodeRef(t, &def, workflow.PromotionNodeObserveDrift).Observe.RetryExhaustionRoute = ""
			mustReject(t, def, promotionOptions(t), workflow.CodeRetryExhaustionFalseCompletion)
		})
		t.Run("exhaustion_route_claims_a_completed_consistent_outcome", func(t *testing.T) {
			def := effectsDefinition()
			nodeRef(t, &def, fxObserve).Observe.RetryExhaustionRoute = fxEndCommit
			mustReject(t, def, effectsOptions(t), workflow.CodeRetryExhaustionFalseCompletion)
		})
		t.Run("exhaustion_route_reaches_a_degraded_terminal", func(t *testing.T) {
			plan, err := workflow.Compile(effectsDefinition(), effectsOptions(t))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			node, _ := plan.Node(fxObserve)
			if node.Observe.RetryExhaustionRoute != fxEndDegraded {
				t.Fatalf("exhaustion route %q", node.Observe.RetryExhaustionRoute)
			}
		})
	})
}

// TestTodo_WF_STEP_014_Conformance drives the OBSERVE contract through the
// step harness.
func TestTodo_WF_STEP_014_Conformance(t *testing.T) {
	conf, ok := workflow.ConformanceFor(workflow.StepObserve)
	if !ok {
		t.Fatal("OBSERVE has no conformance contract")
	}
	if !conf.RequiresCapability {
		t.Fatal("an OBSERVE binds an observer capability version")
	}
	if conf.MayCarryEffect {
		t.Fatal("an OBSERVE reads; it never carries a write effect")
	}
	want := map[workflow.Outcome]bool{
		workflow.OutcomePass:    false,
		workflow.OutcomeFail:    false,
		workflow.OutcomeUnknown: false,
		workflow.OutcomePartial: false,
	}
	for _, o := range conf.Outcomes {
		if _, ok := want[o]; !ok {
			t.Fatalf("unexpected OBSERVE outcome %q", o)
		}
		want[o] = true
	}
	for o, seen := range want {
		if !seen {
			t.Fatalf("OBSERVE must distinguish %s", o)
		}
	}

	cases := []struct {
		name   string
		mutate func(*workflow.Node)
		code   string
	}{
		{"conforming", func(*workflow.Node) {}, ""},
		{"receipt_as_observation", func(n *workflow.Node) {
			n.Observe.EvidenceKind = workflow.EvidenceSubmissionReceipt
		}, workflow.CodeReceiptIsNotObservation},
		{"unbounded_freshness", func(n *workflow.Node) { n.Observe.MaxAgeSeconds = 0 },
			workflow.CodeStaleObservationAccepted},
		{"retry_without_an_exhaustion_route", func(n *workflow.Node) {
			n.Observe.RetryExhaustionRoute = ""
		}, workflow.CodeRetryExhaustionFalseCompletion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node := observeNode(t)
			tc.mutate(&node)
			report := workflow.CheckStepConformance(node, nil)
			if tc.code == "" {
				requireOK(t, report)
				return
			}
			requireCode(t, report, tc.code)
		})
	}
}

// TestTodo_WF_STEP_014_Mutation kills one mutant per observation rule.
func TestTodo_WF_STEP_014_Mutation(t *testing.T) {
	runMutations(t, workflow.PromotionReferenceDefinition, promotionOptions, []mutationCase{
		{"accept_a_submission_receipt", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeObserveDrift).Observe.EvidenceKind =
				workflow.EvidenceSubmissionReceipt
		}, workflow.CodeReceiptIsNotObservation},
		{"drop_the_source_authority", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeObserveDrift).Observe.SourceAuthority = ""
		}, workflow.CodeReceiptIsNotObservation},
		{"drop_the_freshness_bound", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeObserveDrift).Observe.MaxAgeSeconds = 0
		}, workflow.CodeStaleObservationAccepted},
		{"drop_the_expected_state", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeObserveDrift).Observe.ExpectedStateFields = nil
		}, workflow.CodeUnresolvedRef},
		{"route_partial_like_pass", func(t *testing.T, def *workflow.Definition) {
			pass := edgeRef(t, def, workflow.PromotionNodeObserveDrift, "PASS")
			edgeRef(t, def, workflow.PromotionNodeObserveDrift, "PARTIAL").To = pass.To
		}, workflow.CodeDegradedCollapsedToPass},
		{"drop_the_retry_exhaustion_route", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeObserveDrift).Observe.RetryExhaustionRoute = ""
		}, workflow.CodeRetryExhaustionFalseCompletion},
	})
}
