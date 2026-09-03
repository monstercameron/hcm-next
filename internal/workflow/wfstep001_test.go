package workflow_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

// record resolves one capability version out of a registry for the direct
// step-conformance harness.
func record(t *testing.T, r *capability.Registry, id string) *capability.Record {
	t.Helper()
	rec, ok := r.Lookup(capability.Key{ID: id, Version: 1})
	if !ok {
		t.Fatalf("fixture registry does not publish %s/v1", id)
	}
	return &rec
}

// capabilityNode returns the promotion reference's first CAPABILITY node,
// detached from its definition, for isolated conformance checks.
func capabilityNode(t *testing.T) workflow.Node {
	t.Helper()
	def := workflow.PromotionReferenceDefinition()
	return *nodeRef(t, &def, workflow.PromotionNodeSnapshotWorker)
}

// TestTodo_WF_STEP_001 proves planning/todos.md WF-STEP-001: a CAPABILITY node
// with the wrong schema or version, an authority that does not cover the
// capability's scope, a mutation claimed as simulatable or an undeclared retry
// is rejected; a conforming node routes SUCCEEDED, REJECTED, UNKNOWN and
// AMBIGUOUS explicitly and persists an invocation reference rather than a copy
// of the result.
func TestTodo_WF_STEP_001(t *testing.T) {
	registry := promotionRegistry(t)

	t.Run("RED_wrong_schema_version", func(t *testing.T) {
		node := capabilityNode(t)
		node.InputSchema.Version = 9
		report := workflow.CheckStepConformance(node, record(t, registry, "hcmnext.people.explain_worker_state"))
		requireCode(t, report, workflow.CodeTypeMismatch)
	})

	t.Run("RED_wrong_schema_identity", func(t *testing.T) {
		node := capabilityNode(t)
		node.OutputSchema.SchemaID = "some.other.schema/v1"
		report := workflow.CheckStepConformance(node, record(t, registry, "hcmnext.people.explain_worker_state"))
		requireCode(t, report, workflow.CodeTypeMismatch)
	})

	t.Run("RED_unauthorized_scope", func(t *testing.T) {
		node := capabilityNode(t)
		node.Capability.AuthorityScopes = []string{"scope:payroll.read"}
		report := workflow.CheckStepConformance(node, record(t, registry, "hcmnext.people.explain_worker_state"))
		requireCode(t, report, workflow.CodeUnauthorizedScope)
	})

	t.Run("RED_mutation_in_simulate_mode", func(t *testing.T) {
		effects := effectsRegistry(t)
		def := effectsDefinition()
		node := *nodeRef(t, &def, fxSync)
		node.Capability.OperationMode = workflow.ModeSimulate
		report := workflow.CheckStepConformance(node, record(t, effects, capSyncPayroll))
		requireCode(t, report, workflow.CodeMutationInSimulation)
	})

	t.Run("RED_undeclared_retry_policy", func(t *testing.T) {
		node := capabilityNode(t)
		node.Retry = &workflow.RetryPolicy{MaxAttempts: 3}
		report := workflow.CheckStepConformance(node, record(t, registry, "hcmnext.people.explain_worker_state"))
		requireCode(t, report, workflow.CodeInvalidDefinition)
	})

	t.Run("RED_mutation_without_a_logical_effect_identity", func(t *testing.T) {
		effects := effectsRegistry(t)
		def := effectsDefinition()
		node := *nodeRef(t, &def, fxSync)
		node.Capability.EffectBinding = ""
		report := workflow.CheckStepConformance(node, record(t, effects, capSyncPayroll))
		requireCode(t, report, workflow.CodeNonIdempotentRetry)
	})

	t.Run("GREEN_routes_all_four_outcomes", func(t *testing.T) {
		plan := mustCompilePromotion(t)
		node, ok := plan.Node(workflow.PromotionNodeSnapshotWorker)
		if !ok {
			t.Fatal("plan has no snapshot node")
		}
		want := []string{"AMBIGUOUS", "REJECTED", "SUCCEEDED", "UNKNOWN"}
		if len(node.Routes) != len(want) {
			t.Fatalf("routes\n got %v\nwant %v", node.Routes, want)
		}
		for i := range want {
			if node.Routes[i] != want[i] {
				t.Fatalf("routes\n got %v\nwant %v", node.Routes, want)
			}
		}
		if node.Capability == nil || node.Capability.Digest == "" {
			t.Fatal("a compiled CAPABILITY node binds a resolved, digested capability version")
		}
		if node.Capability.OperationMode != workflow.ModeSimulate {
			t.Fatalf("operation mode %q", node.Capability.OperationMode)
		}
	})

	t.Run("REFACTOR_persists_an_invocation_reference_not_the_result", func(t *testing.T) {
		plan := mustCompilePromotion(t)
		node, _ := plan.Node(workflow.PromotionNodeSnapshotWorker)

		wantEvidence := map[string]bool{
			"capability_execution_id": false,
			"governance_decision_id":  false,
			"request_digest":          false,
			"output_digest":           false,
		}
		for _, ref := range node.EvidenceRefs {
			if _, ok := wantEvidence[ref]; ok {
				wantEvidence[ref] = true
			}
		}
		for ref, seen := range wantEvidence {
			if !seen {
				t.Fatalf("a CAPABILITY execution records %q", ref)
			}
		}
		if len(node.Outputs) == 0 {
			t.Fatal("a CAPABILITY node declares the typed output fields it binds")
		}

		// Binding the whole result instead of declared fields is refused.
		def := workflow.PromotionReferenceDefinition()
		nodeRef(t, &def, workflow.PromotionNodeSimulateComp).InputMappings[0].Source.Path = "*"
		mustReject(t, def, promotionOptions(t), workflow.CodeUnrestrictedResultCopy)
	})
}

func requireCode(t *testing.T, report workflow.StepReport, code string) {
	t.Helper()
	if report.OK() {
		t.Fatalf("expected %s, but the node conformed", code)
	}
	for _, e := range report.Errors {
		if e.Code == code {
			return
		}
	}
	t.Fatalf("expected %s, got %v", code, report.Errors)
}

func requireOK(t *testing.T, report workflow.StepReport) {
	t.Helper()
	if !report.OK() {
		t.Fatalf("expected a conforming node, got %v", report.Errors)
	}
}

// TestTodo_WF_STEP_001_Golden pins the compiled shape of a CAPABILITY node.
func TestTodo_WF_STEP_001_Golden(t *testing.T) {
	plan := mustCompilePromotion(t)
	node, ok := plan.Node(workflow.PromotionNodeSnapshotWorker)
	if !ok {
		t.Fatal("plan has no snapshot node")
	}
	goldenJSON(t, "step_capability_node.json", node)
}

// TestTodo_WF_STEP_001_Fault drives the reference-resolution faults a
// CAPABILITY node can hit.
func TestTodo_WF_STEP_001_Fault(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T) (workflow.Definition, workflow.Options)
		code  string
	}{
		{
			name: "capability_version_is_not_published",
			build: func(t *testing.T) (workflow.Definition, workflow.Options) {
				def := workflow.PromotionReferenceDefinition()
				nodeRef(t, &def, workflow.PromotionNodeSnapshotWorker).Capability.Version = 2
				return def, promotionOptions(t)
			},
			code: workflow.CodeUnresolvedRef,
		},
		{
			name: "capability_version_is_retired",
			build: func(t *testing.T) (workflow.Definition, workflow.Options) {
				registry := promotionRegistry(t)
				if err := registry.Retire(capability.Key{ID: "hcmnext.operations.detect_drift", Version: 1}); err != nil {
					t.Fatalf("retire: %v", err)
				}
				return workflow.PromotionReferenceDefinition(),
					workflow.Options{Phase: workflow.PhaseP1A, Capabilities: registry}
			},
			code: workflow.CodeUnresolvedRef,
		},
		{
			name: "capability_reference_names_no_version",
			build: func(t *testing.T) (workflow.Definition, workflow.Options) {
				def := workflow.PromotionReferenceDefinition()
				nodeRef(t, &def, workflow.PromotionNodeEvaluateBand).Capability.Version = 0
				return def, promotionOptions(t)
			},
			code: workflow.CodeUnresolvedRef,
		},
		{
			name: "capability_node_binds_no_capability",
			build: func(t *testing.T) (workflow.Definition, workflow.Options) {
				def := workflow.PromotionReferenceDefinition()
				nodeRef(t, &def, workflow.PromotionNodeEvaluateBand).Capability = nil
				return def, promotionOptions(t)
			},
			code: workflow.CodeUnresolvedRef,
		},
		{
			name: "operation_mode_is_not_a_declared_mode",
			build: func(t *testing.T) (workflow.Definition, workflow.Options) {
				def := workflow.PromotionReferenceDefinition()
				nodeRef(t, &def, workflow.PromotionNodeEvaluateBand).Capability.OperationMode = "DRY_RUN"
				return def, promotionOptions(t)
			},
			code: workflow.CodeInvalidDefinition,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def, opts := tc.build(t)
			mustReject(t, def, opts, tc.code)
		})
	}
}

// TestTodo_WF_STEP_001_Conformance walks the whole step-type vocabulary
// through the harness, so a new or edited primitive cannot quietly lose its
// contract.
func TestTodo_WF_STEP_001_Conformance(t *testing.T) {
	registry := promotionRegistry(t)

	t.Run("step_type_table_is_complete", func(t *testing.T) {
		types := workflow.StepTypes()
		if len(types) != 13 {
			t.Fatalf("the kernel has ten core primitives and three structural ones, got %d: %v",
				len(types), types)
		}
		retired := []workflow.StepType{"CHECKPOINT", "RULE", "AGENT", "DOCUMENT"}
		for _, r := range retired {
			if r.Valid() {
				t.Fatalf("%s is not a step type: it is an attribute or a capability", r)
			}
		}
		for _, typ := range types {
			conf, ok := workflow.ConformanceFor(typ)
			if !ok {
				t.Fatalf("%s has no conformance contract", typ)
			}
			if conf.Terminal {
				if len(conf.Outcomes) != 0 {
					t.Fatalf("%s is terminal and produces no outcome routes", typ)
				}
				continue
			}
			if len(conf.Outcomes) == 0 {
				t.Fatalf("%s declares no outcome routes", typ)
			}
		}
	})

	t.Run("pure_step_types_never_carry_an_effect", func(t *testing.T) {
		for _, typ := range []workflow.StepType{workflow.StepDecision, workflow.StepTransform, workflow.StepEnd} {
			conf, _ := workflow.ConformanceFor(typ)
			if conf.MayCarryEffect {
				t.Fatalf("%s must never carry a write effect", typ)
			}
		}
	})

	t.Run("capability_binding_step_types", func(t *testing.T) {
		for _, typ := range []workflow.StepType{workflow.StepCapability, workflow.StepObserve, workflow.StepCompensate} {
			conf, _ := workflow.ConformanceFor(typ)
			if !conf.RequiresCapability {
				t.Fatalf("%s binds a capability version", typ)
			}
		}
	})

	t.Run("conforming_capability_node_passes_the_harness", func(t *testing.T) {
		report := workflow.CheckStepConformance(capabilityNode(t),
			record(t, registry, "hcmnext.people.explain_worker_state"))
		requireOK(t, report)
		if report.Conformance.Type != workflow.StepCapability {
			t.Fatalf("report names step type %q", report.Conformance.Type)
		}
	})

	t.Run("unknown_step_type_is_reported_not_ignored", func(t *testing.T) {
		node := capabilityNode(t)
		node.Type = "MAGIC"
		report := workflow.CheckStepConformance(node, nil)
		requireCode(t, report, workflow.CodeInvalidDefinition)
	})
}

// TestTodo_WF_STEP_001_Mutation kills one mutant per CAPABILITY rule.
func TestTodo_WF_STEP_001_Mutation(t *testing.T) {
	runMutations(t, workflow.PromotionReferenceDefinition, promotionOptions, []mutationCase{
		{"drift_the_request_schema", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeSimulateComp).InputSchema.SchemaID = "other/v1"
		}, workflow.CodeTypeMismatch},
		{"drift_the_response_schema", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeObserveDrift).OutputSchema.ProtobufFullName = "other.Message"
		}, workflow.CodeTypeMismatch},
		{"drop_the_required_authority_scope", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeEvaluateBand).Capability.AuthorityScopes = nil
		}, workflow.CodeUnauthorizedScope},
		{"drop_the_declared_outputs", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeEvaluateBand).Outputs = nil
		}, workflow.CodeUnresolvedRef},
		{"retry_without_a_backoff_policy", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, workflow.PromotionNodeObserveDrift).Retry.BackoffRef = ""
		}, workflow.CodeInvalidDefinition},
	})
}

// TestTodo_WF_STEP_001_Security proves the paths where a CAPABILITY node would
// otherwise obtain authority or data it was never granted.
func TestTodo_WF_STEP_001_Security(t *testing.T) {
	t.Run("authority_scope_must_cover_the_capability", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		nodeRef(t, &def, workflow.PromotionNodeObserveDrift).Capability.AuthorityScopes =
			[]string{"scope:operations.admin"}
		mustReject(t, def, promotionOptions(t), workflow.CodeUnauthorizedScope)
	})

	t.Run("simulate_mode_cannot_carry_a_mutation", func(t *testing.T) {
		def := effectsDefinition()
		nodeRef(t, &def, fxSync).Capability.OperationMode = workflow.ModeSimulate
		mustReject(t, def, effectsOptions(t), workflow.CodeMutationInSimulation)
	})

	t.Run("result_is_bound_field_by_field", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		nodeRef(t, &def, workflow.PromotionNodeBuildProposal).InputMappings[1].Source.Path = "*"
		mustReject(t, def, promotionOptions(t), workflow.CodeUnrestrictedResultCopy)
	})

	t.Run("p1a_refuses_every_write_capability", func(t *testing.T) {
		opts := workflow.Options{Phase: workflow.PhaseP1A, Capabilities: effectsRegistry(t)}
		mustReject(t, effectsDefinition(), opts, workflow.CodeWriteEffectRefusedP1A)
	})
}
