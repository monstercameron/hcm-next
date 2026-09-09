package workflow_test

import (
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func transformNode(t *testing.T) workflow.Node {
	t.Helper()
	def := workflow.PromotionReferenceDefinition()
	return *nodeRef(t, &def, workflow.PromotionNodeBuildProposal)
}

// TestTodo_WF_STEP_010 proves planning/todos.md WF-STEP-010: arbitrary code,
// clock/randomness/network access, an unpinned lookup, unbounded resources or
// an unsatisfied taint sanitizer all fail; a conforming TRANSFORM binds a
// versioned deterministic mapping and records its input digest and taint
// lineage.
func TestTodo_WF_STEP_010(t *testing.T) {
	t.Run("RED_arbitrary_code", func(t *testing.T) {
		node := transformNode(t)
		node.Transform.InlineCode = "return input.raise_ratio * 2"
		requireCode(t, workflow.CheckStepConformance(node, nil), workflow.CodeArbitraryCode)
	})

	t.Run("RED_network_time_or_randomness", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*workflow.Node)
		}{
			{"clock", func(n *workflow.Node) { n.Transform.UsesClock = true }},
			{"randomness", func(n *workflow.Node) { n.Transform.UsesRandom = true }},
			{"network", func(n *workflow.Node) { n.Transform.UsesNetwork = true }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				node := transformNode(t)
				tc.mutate(&node)
				requireCode(t, workflow.CheckStepConformance(node, nil), workflow.CodeNondeterministicTransform)
			})
		}
	})

	t.Run("RED_unpinned_lookup", func(t *testing.T) {
		node := transformNode(t)
		node.Transform.Lookups = []workflow.TransformLookup{{Ref: "reference.currency_rates"}}
		requireCode(t, workflow.CheckStepConformance(node, nil), workflow.CodeUnpinnedLookup)
	})

	t.Run("RED_resource_excess", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*workflow.Node)
		}{
			{"no_input_bound", func(n *workflow.Node) { n.Transform.Limits.MaxInputBytes = 0 }},
			{"no_output_bound", func(n *workflow.Node) { n.Transform.Limits.MaxOutputBytes = 0 }},
			{"no_step_bound", func(n *workflow.Node) { n.Transform.Limits.MaxSteps = 0 }},
			{"above_input_ceiling", func(n *workflow.Node) {
				n.Transform.Limits.MaxInputBytes = workflow.MaxTransformInputBytes + 1
			}},
			{"above_output_ceiling", func(n *workflow.Node) {
				n.Transform.Limits.MaxOutputBytes = workflow.MaxTransformOutputBytes + 1
			}},
			{"above_step_ceiling", func(n *workflow.Node) {
				n.Transform.Limits.MaxSteps = workflow.MaxTransformSteps + 1
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				node := transformNode(t)
				tc.mutate(&node)
				requireCode(t, workflow.CheckStepConformance(node, nil), workflow.CodeResourceLimitExceeded)
			})
		}
	})

	t.Run("RED_unsatisfied_taint_sanitizer", func(t *testing.T) {
		node := transformNode(t)
		node.Transform.InputTaint[0].Level = workflow.TaintTainted
		node.Transform.OutputTaint = workflow.TaintTrusted
		requireCode(t, workflow.CheckStepConformance(node, nil), workflow.CodeUnsatisfiedSanitizer)
	})

	t.Run("GREEN_sanitizer_receipt_authorizes_the_downgrade", func(t *testing.T) {
		node := transformNode(t)
		node.Transform.InputTaint[0].Level = workflow.TaintTainted
		node.Transform.OutputTaint = workflow.TaintTrusted
		node.Transform.SanitizerReceiptRef = "sanitizer.receipt.promotion/v1"
		requireOK(t, workflow.CheckStepConformance(node, nil))
	})

	t.Run("RED_taint_manifest_names_an_undeclared_input", func(t *testing.T) {
		node := transformNode(t)
		node.Transform.InputTaint = append(node.Transform.InputTaint,
			workflow.TaintInput{Source: "not_an_input", Level: workflow.TaintDerived})
		requireCode(t, workflow.CheckStepConformance(node, nil), workflow.CodeUnresolvedRef)
	})

	t.Run("GREEN_byte_stable_typed_output_with_digest_and_lineage", func(t *testing.T) {
		first := mustCompilePromotion(t)
		second := mustCompilePromotion(t)

		a, ok := first.Node(workflow.PromotionNodeBuildProposal)
		if !ok {
			t.Fatal("plan has no transform node")
		}
		b, _ := second.Node(workflow.PromotionNodeBuildProposal)
		if !reflect.DeepEqual(a.Transform, b.Transform) {
			t.Fatal("the same transform binding must compile byte-identically")
		}
		if a.Transform.InputDigest == "" {
			t.Fatal("a compiled TRANSFORM records the digest of its pinned inputs")
		}
		if a.Transform.TransformRef == "" || a.Transform.Version == 0 {
			t.Fatalf("a compiled TRANSFORM binds an exact version, got %+v", a.Transform)
		}
		if a.EffectClass != capability.EffectPure {
			t.Fatalf("a TRANSFORM is pure, got %s", a.EffectClass)
		}
		wantLineage := []string{
			"input:band_position=TRUSTED",
			"input:current_job_id=TRUSTED",
			"input:raise_ratio=TRUSTED",
			"input:worker_id=TRUSTED",
			"output=TRUSTED",
		}
		if !reflect.DeepEqual(a.Transform.Lineage, wantLineage) {
			t.Fatalf("taint lineage\n got %v\nwant %v", a.Transform.Lineage, wantLineage)
		}
	})

	t.Run("GREEN_lineage_and_digest_move_with_the_declaration", func(t *testing.T) {
		base := mustCompilePromotion(t)
		baseNode, _ := base.Node(workflow.PromotionNodeBuildProposal)

		def := workflow.PromotionReferenceDefinition()
		n := nodeRef(t, &def, workflow.PromotionNodeBuildProposal)
		n.Transform.InputTaint[0].Level = workflow.TaintDerived
		n.Transform.OutputTaint = workflow.TaintDerived
		changed, err := workflow.Compile(def, promotionOptions(t))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		changedNode, _ := changed.Node(workflow.PromotionNodeBuildProposal)
		if reflect.DeepEqual(baseNode.Transform.Lineage, changedNode.Transform.Lineage) {
			t.Fatal("a changed taint declaration must change the recorded lineage")
		}
		if base.Digest() == changed.Digest() {
			t.Fatal("a changed taint declaration must change the plan digest")
		}
	})

	t.Run("REFACTOR_binds_the_constrained_engine_rather_than_carrying_code", func(t *testing.T) {
		plan := mustCompilePromotion(t)
		node, _ := plan.Node(workflow.PromotionNodeBuildProposal)
		if node.Transform.NormalizationProfile == "" {
			t.Fatal("a compiled TRANSFORM names the normalization profile the engine applies")
		}
		compiled := reflect.TypeOf(*node.Transform)
		for i := 0; i < compiled.NumField(); i++ {
			if compiled.Field(i).Name == "InlineCode" {
				t.Fatal("a compiled TRANSFORM must not be able to carry inline code")
			}
		}
		for _, l := range node.Transform.Lookups {
			if l.SnapshotDigest == "" {
				t.Fatalf("lookup %q is not pinned", l.Ref)
			}
		}
	})
}

// TestTodo_WF_STEP_010_Golden pins the compiled TRANSFORM node and a table of
// deterministic taint-lineage vectors.
func TestTodo_WF_STEP_010_Golden(t *testing.T) {
	plan := mustCompilePromotion(t)
	node, ok := plan.Node(workflow.PromotionNodeBuildProposal)
	if !ok {
		t.Fatal("plan has no transform node")
	}
	goldenJSON(t, "step_transform_node.json", node)

	type vector struct {
		Name        string                      `json:"name"`
		InputTaint  []workflow.TaintInput       `json:"input_taint"`
		Lookups     []workflow.TransformLookup  `json:"lookups,omitempty"`
		Sanitizer   string                      `json:"sanitizer,omitempty"`
		OutputTaint workflow.TaintLevel         `json:"output_taint"`
		Compiled    *workflow.CompiledTransform `json:"compiled"`
	}
	vectors := []struct {
		name      string
		taint     []workflow.TaintInput
		lookups   []workflow.TransformLookup
		sanitizer string
		output    workflow.TaintLevel
	}{
		{
			name:   "all_trusted",
			taint:  []workflow.TaintInput{{Source: "worker_id", Level: workflow.TaintTrusted}},
			output: workflow.TaintTrusted,
		},
		{
			name: "derived_with_pinned_lookup",
			taint: []workflow.TaintInput{
				{Source: "worker_id", Level: workflow.TaintTrusted},
				{Source: "raise_ratio", Level: workflow.TaintDerived},
			},
			lookups: []workflow.TransformLookup{
				{Ref: "reference.pay_bands", SnapshotDigest: "sha256:pinned-pay-bands"},
			},
			output: workflow.TaintDerived,
		},
		{
			name:      "tainted_downgraded_by_receipt",
			taint:     []workflow.TaintInput{{Source: "band_position", Level: workflow.TaintTainted}},
			sanitizer: "sanitizer.receipt.promotion/v1",
			output:    workflow.TaintTrusted,
		},
	}

	var out []vector
	for _, v := range vectors {
		def := workflow.PromotionReferenceDefinition()
		n := nodeRef(t, &def, workflow.PromotionNodeBuildProposal)
		n.Transform.InputTaint = v.taint
		n.Transform.Lookups = v.lookups
		n.Transform.SanitizerReceiptRef = v.sanitizer
		n.Transform.OutputTaint = v.output
		plan, err := workflow.Compile(def, promotionOptions(t))
		if err != nil {
			t.Fatalf("vector %s: %v", v.name, err)
		}
		compiled, _ := plan.Node(workflow.PromotionNodeBuildProposal)
		out = append(out, vector{
			Name:        v.name,
			InputTaint:  v.taint,
			Lookups:     v.lookups,
			Sanitizer:   v.sanitizer,
			OutputTaint: v.output,
			Compiled:    compiled.Transform,
		})
	}
	goldenJSON(t, "step_transform_vectors.json", out)
}
