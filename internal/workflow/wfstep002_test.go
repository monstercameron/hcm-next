package workflow_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

func decisionNode(t *testing.T) workflow.Node {
	t.Helper()
	def := workflow.PromotionReferenceDefinition()
	return *nodeRef(t, &def, workflow.PromotionNodeRaiseThreshold)
}

// unpinnedContext is a context requirement a DECISION must not read: it is a
// live read of current state dressed up as an input.
func unpinnedContext() workflow.ContextRequirement {
	return workflow.ContextRequirement{
		Kind:                  "OrganizationContext",
		FieldPaths:            []string{"manager_id"},
		Purpose:               "SIMULATE_MANAGEMENT_PROMOTION",
		MaximumClassification: "CONFIDENTIAL_HR",
		MaxAgeSeconds:         60,
		Pinned:                false,
		MissingBehavior:       workflow.MissingUnknown,
	}
}

// TestTodo_WF_STEP_002 proves planning/todos.md WF-STEP-002: a DECISION with a
// mutable or unpinned input, overlapping routes without declared precedence or
// no UNKNOWN route fails deterministically; a conforming DECISION selects
// exactly one explicit route and records evaluator, version and input digest.
func TestTodo_WF_STEP_002(t *testing.T) {
	t.Run("RED_mutable_unpinned_input", func(t *testing.T) {
		t.Run("unpinned_context_requirement", func(t *testing.T) {
			def := workflow.PromotionReferenceDefinition()
			n := nodeRef(t, &def, workflow.PromotionNodeRaiseThreshold)
			n.RequiredContext = []workflow.ContextRequirement{unpinnedContext()}
			mustReject(t, def, promotionOptions(t), workflow.CodeMutableDecisionInput)
		})
		t.Run("mapping_reads_unpinned_context", func(t *testing.T) {
			node := decisionNode(t)
			node.RequiredContext = []workflow.ContextRequirement{unpinnedContext()}
			node.Inputs = append(node.Inputs, workflow.Field{Path: "manager_id", Type: brandedString("WorkerID")})
			node.InputMappings = append(node.InputMappings, workflow.Mapping{
				Target: "manager_id",
				Source: workflow.Source{
					Kind:        workflow.SourceContext,
					ContextKind: "OrganizationContext",
					Path:        "manager_id",
					Type:        brandedString("WorkerID"),
				},
			})
			requireCode(t, workflow.CheckStepConformance(node, nil), workflow.CodeMutableDecisionInput)
		})
	})

	t.Run("RED_nonexclusive_routes", func(t *testing.T) {
		node := decisionNode(t)
		node.Decision.Routes = []workflow.DecisionRoute{
			{Key: "WITHIN_THRESHOLD", Predicate: "same_predicate", Precedence: 10},
			{Key: "EXCEEDS_THRESHOLD", Predicate: "same_predicate", Precedence: 10},
		}
		requireCode(t, workflow.CheckStepConformance(node, nil), workflow.CodeNonExclusiveRoutes)
	})

	t.Run("GREEN_overlapping_routes_with_declared_precedence_are_allowed", func(t *testing.T) {
		node := decisionNode(t)
		node.Decision.Routes = []workflow.DecisionRoute{
			{Key: "WITHIN_THRESHOLD", Predicate: "same_predicate", Precedence: 10},
			{Key: "EXCEEDS_THRESHOLD", Predicate: "same_predicate", Precedence: 20},
		}
		requireOK(t, workflow.CheckStepConformance(node, nil))
	})

	t.Run("RED_absent_unknown_route", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		kept := def.Edges[:0]
		for _, e := range def.Edges {
			if e.From == workflow.PromotionNodeRaiseThreshold && e.RouteKey == "UNKNOWN" {
				continue
			}
			kept = append(kept, e)
		}
		def.Edges = kept
		mustReject(t, def, promotionOptions(t), workflow.CodeMissingRoute)
	})

	t.Run("RED_unrecorded_evaluator_or_digest_profile", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*workflow.Node)
		}{
			{"no_evaluator_ref", func(n *workflow.Node) { n.Decision.EvaluatorRef = "" }},
			{"no_evaluator_version", func(n *workflow.Node) { n.Decision.EvaluatorVersion = 0 }},
			{"no_input_digest_profile", func(n *workflow.Node) { n.Decision.InputDigestProfile = "" }},
			{"route_without_a_predicate", func(n *workflow.Node) { n.Decision.Routes[0].Predicate = "" }},
			{"default_route_is_not_declared", func(n *workflow.Node) { n.Decision.DefaultRoute = "NOWHERE" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				node := decisionNode(t)
				tc.mutate(&node)
				requireCode(t, workflow.CheckStepConformance(node, nil), workflow.CodeUnresolvedRef)
			})
		}
	})

	t.Run("GREEN_pinned_snapshot_selects_one_route_and_records_its_evaluation", func(t *testing.T) {
		plan := mustCompilePromotion(t)
		node, ok := plan.Node(workflow.PromotionNodeRaiseThreshold)
		if !ok {
			t.Fatal("plan has no decision node")
		}
		if node.Decision == nil {
			t.Fatal("compiled DECISION carries no decision binding")
		}
		if node.Decision.EvaluatorRef == "" || node.Decision.EvaluatorVersion == 0 {
			t.Fatalf("compiled DECISION records evaluator identity, got %+v", node.Decision)
		}
		if node.Decision.RuleRef == "" {
			t.Fatal("RULE is a DECISION carrying a rule reference; none is recorded")
		}
		if node.Decision.InputDigest == "" {
			t.Fatal("compiled DECISION records the digest of its pinned input snapshot")
		}
		wantRoutes := map[string]bool{"WITHIN_THRESHOLD": false, "EXCEEDS_THRESHOLD": false, "UNKNOWN": false}
		for _, key := range node.Routes {
			if _, ok := wantRoutes[key]; !ok {
				t.Fatalf("unexpected route %q", key)
			}
			wantRoutes[key] = true
		}
		for key, seen := range wantRoutes {
			if !seen {
				t.Fatalf("route %q has no explicit edge", key)
			}
		}
		for _, m := range node.Mappings {
			if !m.PinnedInput {
				t.Fatalf("DECISION input %q is not pinned", m.Target)
			}
		}
	})

	t.Run("GREEN_input_digest_tracks_the_pinned_snapshot", func(t *testing.T) {
		base := mustCompilePromotion(t)
		baseNode, _ := base.Node(workflow.PromotionNodeRaiseThreshold)

		same := mustCompilePromotion(t)
		sameNode, _ := same.Node(workflow.PromotionNodeRaiseThreshold)
		if baseNode.Decision.InputDigest != sameNode.Decision.InputDigest {
			t.Fatal("the same pinned inputs must digest identically")
		}

		def := workflow.PromotionReferenceDefinition()
		n := nodeRef(t, &def, workflow.PromotionNodeRaiseThreshold)
		n.InputMappings[1].Source = nodeSource(workflow.PromotionNodeEvaluateBand, "band_position")
		changed, err := workflow.Compile(def, promotionOptions(t))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		changedNode, _ := changed.Node(workflow.PromotionNodeRaiseThreshold)
		if changedNode.Decision.InputDigest == baseNode.Decision.InputDigest {
			t.Fatal("a different pinned input snapshot must produce a different input digest")
		}
	})

	t.Run("REFACTOR_no_side_effect_and_no_hidden_current_state_read", func(t *testing.T) {
		plan := mustCompilePromotion(t)
		node, _ := plan.Node(workflow.PromotionNodeRaiseThreshold)
		if node.EffectClass != capability.EffectPure {
			t.Fatalf("a DECISION is pure, got %s", node.EffectClass)
		}
		if len(node.RequiredContext) != 0 {
			t.Fatalf("this DECISION reads no context, got %v", node.RequiredContext)
		}

		def := workflow.PromotionReferenceDefinition()
		nodeRef(t, &def, workflow.PromotionNodeRaiseThreshold).DeclaredEffect = capability.EffectInternalMutation
		mustReject(t, def, promotionOptions(t), workflow.CodeEffectDeclarationConflict)

		withCapability := workflow.PromotionReferenceDefinition()
		nodeRef(t, &withCapability, workflow.PromotionNodeRaiseThreshold).Capability =
			&workflow.CapabilityRef{ID: "hcmnext.people.explain_worker_state", Version: 1}
		mustReject(t, withCapability, promotionOptions(t), workflow.CodeInvalidDefinition)
	})
}

// TestTodo_WF_STEP_002_Golden pins the compiled shape of a DECISION node,
// including its rule reference and pinned-input digest.
func TestTodo_WF_STEP_002_Golden(t *testing.T) {
	plan := mustCompilePromotion(t)
	node, ok := plan.Node(workflow.PromotionNodeRaiseThreshold)
	if !ok {
		t.Fatal("plan has no decision node")
	}
	goldenJSON(t, "step_decision_node.json", node)
}

// TestTodo_WF_STEP_002_Conformance drives the DECISION contract through the
// step harness, variant by variant.
func TestTodo_WF_STEP_002_Conformance(t *testing.T) {
	conf, ok := workflow.ConformanceFor(workflow.StepDecision)
	if !ok {
		t.Fatal("DECISION has no conformance contract")
	}
	if !conf.AuthorRoutes {
		t.Fatal("a DECISION's route keys are author-declared")
	}
	if len(conf.Outcomes) != 1 || conf.Outcomes[0] != workflow.OutcomeUnknown {
		t.Fatalf("a DECISION always carries an UNKNOWN outcome, got %v", conf.Outcomes)
	}
	if conf.MayCarryEffect {
		t.Fatal("a DECISION never carries an effect")
	}
	if conf.RequiresCapability {
		t.Fatal("a DECISION calls no capability")
	}

	cases := []struct {
		name   string
		mutate func(*workflow.Node)
		code   string
	}{
		{"conforming", func(*workflow.Node) {}, ""},
		{"no_routes", func(n *workflow.Node) { n.Decision.Routes = nil }, workflow.CodeMissingRoute},
		{"duplicate_route_key", func(n *workflow.Node) {
			n.Decision.Routes = append(n.Decision.Routes, n.Decision.Routes[0])
		}, workflow.CodeDuplicateRoute},
		{"route_without_a_key", func(n *workflow.Node) { n.Decision.Routes[0].Key = "" }, workflow.CodeInvalidDefinition},
		{"overlapping_without_precedence", func(n *workflow.Node) {
			n.Decision.Routes[1].Predicate = n.Decision.Routes[0].Predicate
			n.Decision.Routes[1].Precedence = n.Decision.Routes[0].Precedence
		}, workflow.CodeNonExclusiveRoutes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node := decisionNode(t)
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
