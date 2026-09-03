package workflow_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

// goldenBytes compares raw bytes against a checked-in golden file.
func goldenBytes(t *testing.T, name string, got []byte) {
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

// TestTodo_WF_COMP_001 proves planning/todos.md WF-COMP-001: a missing,
// retired or type-invalid reference is rejected with TYPE_MISMATCH or
// UNRESOLVED_REF, and a well-formed definition compiles to an immutable
// normalized IR carrying resolved definition, schema and capability digests.
func TestTodo_WF_COMP_001(t *testing.T) {
	t.Run("RED_missing_schema_reference", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		nodeRef(t, &def, workflow.PromotionNodeSnapshotWorker).InputSchema = workflow.SchemaRef{}
		d := mustReject(t, def, promotionOptions(t), workflow.CodeUnresolvedRef)
		if !d.HasAt(workflow.CodeUnresolvedRef, workflow.PromotionNodeSnapshotWorker) {
			t.Fatalf("diagnostic must name the node that lost its schema, got %v", d.Errors)
		}
	})

	t.Run("RED_retired_capability", func(t *testing.T) {
		registry := promotionRegistry(t)
		key := capability.Key{ID: "hcmnext.rewards.simulate_compensation", Version: 1}
		if err := registry.Retire(key); err != nil {
			t.Fatalf("retire %s: %v", key, err)
		}
		opts := workflow.Options{Phase: workflow.PhaseP1A, Capabilities: registry}
		d := mustReject(t, workflow.PromotionReferenceDefinition(), opts, workflow.CodeUnresolvedRef)
		if !d.HasAt(workflow.CodeUnresolvedRef, workflow.PromotionNodeSimulateComp) {
			t.Fatalf("diagnostic must name the node binding the retired version, got %v", d.Errors)
		}
	})

	t.Run("RED_unknown_capability_version", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		nodeRef(t, &def, workflow.PromotionNodeEvaluateBand).Capability.Version = 7
		mustReject(t, def, promotionOptions(t), workflow.CodeUnresolvedRef)
	})

	t.Run("RED_no_registry_supplied", func(t *testing.T) {
		mustReject(t, workflow.PromotionReferenceDefinition(),
			workflow.Options{Phase: workflow.PhaseP1A}, workflow.CodeUnresolvedRef)
	})

	t.Run("RED_invalid_mapping", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*workflow.Definition)
			code   string
		}{
			{"target_is_not_a_declared_input", func(def *workflow.Definition) {
				n := nodeRef(t, def, workflow.PromotionNodeSimulateComp)
				n.InputMappings[0].Target = "not_an_input"
			}, workflow.CodeUnresolvedRef},
			{"source_path_is_not_a_declared_output", func(def *workflow.Definition) {
				n := nodeRef(t, def, workflow.PromotionNodeSimulateComp)
				n.InputMappings[0].Source.Path = "not_an_output"
			}, workflow.CodeUnresolvedRef},
			{"source_node_is_unknown", func(def *workflow.Definition) {
				n := nodeRef(t, def, workflow.PromotionNodeSimulateComp)
				n.InputMappings[0].Source.NodeID = "no_such_node"
			}, workflow.CodeUnresolvedRef},
			{"source_node_does_not_run_first", func(def *workflow.Definition) {
				n := nodeRef(t, def, workflow.PromotionNodeSnapshotWorker)
				n.InputMappings[0].Source = nodeSource(workflow.PromotionNodeObserveDrift, "observed_job_id")
			}, workflow.CodeSourceNotPredecessor},
			{"input_field_has_no_mapping", func(def *workflow.Definition) {
				n := nodeRef(t, def, workflow.PromotionNodeEvaluateBand)
				n.InputMappings = n.InputMappings[:1]
			}, workflow.CodeUnresolvedRef},
			{"input_field_is_bound_twice", func(def *workflow.Definition) {
				n := nodeRef(t, def, workflow.PromotionNodeEvaluateBand)
				n.InputMappings = append(n.InputMappings, n.InputMappings[0])
			}, workflow.CodeDuplicateMapping},
			{"unrestricted_result_copy", func(def *workflow.Definition) {
				n := nodeRef(t, def, workflow.PromotionNodeSimulateComp)
				n.InputMappings[0].Source.Path = "*"
			}, workflow.CodeUnrestrictedResultCopy},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				def := workflow.PromotionReferenceDefinition()
				tc.mutate(&def)
				mustReject(t, def, promotionOptions(t), tc.code)
			})
		}
	})

	t.Run("RED_incompatible_types", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*workflow.Definition)
		}{
			{"different_kind", func(def *workflow.Definition) {
				n := nodeRef(t, def, workflow.PromotionNodeEvaluateBand)
				n.Inputs[1].Type = workflow.ValueType{Kind: workflow.KindBool}
			}},
			{"different_brand", func(def *workflow.Definition) {
				n := nodeRef(t, def, workflow.PromotionNodeSimulateComp)
				n.Inputs[0].Type = brandedString("PersonID")
			}},
			{"nullable_into_non_nullable", func(def *workflow.Definition) {
				n := nodeRef(t, def, workflow.PromotionNodeBuildProposal)
				n.Inputs[4].Type = workflow.ValueType{Kind: workflow.KindDecimal}
				simulate := nodeRef(t, def, workflow.PromotionNodeSimulateComp)
				simulate.Outputs[1].Type = workflow.ValueType{Kind: workflow.KindDecimal, Nullable: true}
			}},
			{"workflow_output_not_produced_by_terminal", func(def *workflow.Definition) {
				def.Outputs = append(def.Outputs, workflow.Field{Path: "unproduced", Type: stringType()})
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				def := workflow.PromotionReferenceDefinition()
				tc.mutate(&def)
				plan, err := workflow.Compile(def, promotionOptions(t))
				if plan != nil {
					t.Fatal("expected no plan for an incompatible mapping")
				}
				d := diagnostics(t, err)
				if !d.Has(workflow.CodeTypeMismatch) && !d.Has(workflow.CodeUnresolvedRef) {
					t.Fatalf("expected TYPE_MISMATCH or UNRESOLVED_REF, got %v", d.Codes())
				}
			})
		}
	})

	t.Run("RED_undeclared_context", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		n := nodeRef(t, &def, workflow.PromotionNodeBuildProposal)
		n.InputMappings[0].Source = workflow.Source{
			Kind:        workflow.SourceContext,
			ContextKind: "PrincipalContext",
			Path:        "principal_id",
			Type:        brandedString("WorkerID"),
		}
		mustReject(t, def, promotionOptions(t), workflow.CodeUnresolvedRef)
	})

	t.Run("RED_unknown_failure_route", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		nodeRef(t, &def, workflow.PromotionNodeSnapshotWorker).FailureRoute = "no_such_node"
		mustReject(t, def, promotionOptions(t), workflow.CodeUnresolvedRef)
	})

	t.Run("GREEN_immutable_normalized_ir_with_resolved_digests", func(t *testing.T) {
		plan := mustCompilePromotion(t)

		if plan.Digest() == "" {
			t.Fatal("a compiled plan carries a content digest")
		}
		if err := plan.Verify(); err != nil {
			t.Fatalf("a freshly compiled plan verifies against its own digest: %v", err)
		}

		second := mustCompilePromotion(t)
		if plan.Digest() != second.Digest() {
			t.Fatalf("compiling the same definition twice produced %s and %s", plan.Digest(), second.Digest())
		}

		for i := 1; i < len(plan.Nodes); i++ {
			if plan.Nodes[i-1].ID >= plan.Nodes[i].ID {
				t.Fatalf("normalized nodes are ordered; %q precedes %q", plan.Nodes[i-1].ID, plan.Nodes[i].ID)
			}
		}
		for _, n := range plan.Nodes {
			if n.Capability == nil {
				continue
			}
			if n.Capability.Digest == "" {
				t.Fatalf("node %s binds %s with no resolved capability digest", n.ID, n.Capability.ID)
			}
			if !n.Capability.RequestSchema.Valid() || !n.Capability.ResponseSchema.Valid() {
				t.Fatalf("node %s binds unresolved capability schemas", n.ID)
			}
			if n.Capability.Status != capability.StatusActive {
				t.Fatalf("node %s binds a %s capability version", n.ID, n.Capability.Status)
			}
		}
	})

	t.Run("GREEN_compiler_identity_is_material", func(t *testing.T) {
		base := mustCompilePromotion(t)
		other, err := workflow.Compile(workflow.PromotionReferenceDefinition(), workflow.Options{
			Phase:           workflow.PhaseP1A,
			Capabilities:    promotionRegistry(t),
			CompilerVersion: "hcmnext.workflow.compiler/v99",
		})
		if err != nil {
			t.Fatalf("compile with an alternate compiler version: %v", err)
		}
		if base.Digest() == other.Digest() {
			t.Fatal("the compiler that produced a plan is part of its identity")
		}
	})

	t.Run("REFACTOR_plan_never_aliases_the_draft", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		plan, err := workflow.Compile(def, promotionOptions(t))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		before := plan.Digest()

		// Editing the draft after publication must not reach into the plan:
		// the runtime never interprets arbitrary draft configuration.
		def.Nodes[0].InputMappings[0].Target = "mutated_after_publication"
		def.Inputs[0].Type = workflow.ValueType{Kind: workflow.KindBool}
		def.Limits.MaxFanOut = 99

		if err := plan.Verify(); err != nil {
			t.Fatalf("plan changed when the draft was edited: %v", err)
		}
		if plan.Digest() != before {
			t.Fatal("plan digest changed when the draft was edited")
		}
	})

	t.Run("REFACTOR_edited_plan_stops_verifying", func(t *testing.T) {
		plan := mustCompilePromotion(t)
		plan.Nodes[0].Governance.Purpose = "SOMETHING_ELSE"
		if err := plan.Verify(); err == nil {
			t.Fatal("a plan edited after compilation must stop verifying")
		}
	})
}

// TestTodo_WF_COMP_001_Golden pins the compiled promotion reference workflow:
// its normalized IR, its digest and the YAML shape the loader accepts.
func TestTodo_WF_COMP_001_Golden(t *testing.T) {
	plan := mustCompilePromotion(t)

	goldenJSON(t, "promotion_plan.json", struct {
		Digest string                     `json:"digest"`
		Plan   *workflow.CompiledWorkflow `json:"plan"`
	}{Digest: plan.Digest(), Plan: plan})

	document, err := workflow.Marshal(workflow.PromotionReferenceDefinition())
	if err != nil {
		t.Fatalf("marshal reference definition: %v", err)
	}
	goldenBytes(t, "promotion_reference.json", document)

	loaded, err := workflow.LoadFile(filepath.Join("testdata", "promotion_reference.json"))
	if err != nil {
		t.Fatalf("load reference definition: %v", err)
	}
	if !reflect.DeepEqual(loaded, workflow.PromotionReferenceDefinition()) {
		t.Fatal("the document shape and the Go values are not the same definition")
	}
	loadedPlan, err := workflow.Compile(loaded, promotionOptions(t))
	if err != nil {
		t.Fatalf("compile the loaded definition: %v", err)
	}
	if loadedPlan.Digest() != plan.Digest() {
		t.Fatalf("loaded definition compiled to %s, Go values to %s", loadedPlan.Digest(), plan.Digest())
	}
}

// TestTodo_WF_COMP_001_Mutation kills one mutant per WF-COMP-001 rule: each
// single-point edit of a compiling definition must be rejected with the code
// that rule owns.
func TestTodo_WF_COMP_001_Mutation(t *testing.T) {
	runMutations(t, workflow.PromotionReferenceDefinition, promotionOptions, []mutationCase{
		{"blank_workflow_id", func(t *testing.T, def *workflow.Definition) {
			def.WorkflowID = ""
		}, workflow.CodeInvalidDefinition},
		{"zero_version", func(t *testing.T, def *workflow.Definition) {
			def.Version = 0
		}, workflow.CodeInvalidDefinition},
		{"duplicate_node_id", func(t *testing.T, def *workflow.Definition) {
			def.Nodes = append(def.Nodes, *nodeRef(t, def, workflow.PromotionNodeEndUnknown))
		}, workflow.CodeInvalidDefinition},
		{"unversioned_workflow_input_schema", func(t *testing.T, def *workflow.Definition) {
			def.InputSchema.Version = 0
		}, workflow.CodeUnresolvedRef},
		{"capability_schema_drift", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeSnapshotWorker).OutputSchema.Version = 2
		}, workflow.CodeTypeMismatch},
		{"duplicate_field_path", func(t *testing.T, def *workflow.Definition) {
			n := nodeRef(t, def, workflow.PromotionNodeEvaluateBand)
			n.Outputs = append(n.Outputs, n.Outputs[0])
		}, workflow.CodeInvalidDefinition},
		{"unknown_mapping_source_kind", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeEvaluateBand).InputMappings[0].Source.Kind = "AMBIENT"
		}, workflow.CodeUnresolvedRef},
		{"nullable_constant", func(t *testing.T, def *workflow.Definition) {
			n := nodeRef(t, def, workflow.PromotionNodeEndUnknown)
			n.InputMappings[1].Source.Type = workflow.ValueType{Kind: workflow.KindString, Nullable: true}
		}, workflow.CodeTypeMismatch},
		{"terminal_drops_a_workflow_output", func(t *testing.T, def *workflow.Definition) {
			n := nodeRef(t, def, workflow.PromotionNodeEndRejected)
			n.Inputs = n.Inputs[:1]
			n.InputMappings = n.InputMappings[:1]
		}, workflow.CodeUnresolvedRef},
		{"phase_does_not_implement_step_type", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeBuildProposal).Type = workflow.StepApproval
		}, workflow.CodePhaseNotImplemented},
	})
}
