package workflow_test

import (
	"reflect"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// withoutObservation removes the observation stage, leaving an external effect
// nobody ever confirms actually happened.
func withoutObservation(def workflow.Definition) workflow.Definition {
	nodes := def.Nodes[:0:0]
	for _, n := range def.Nodes {
		if n.ID == fxObserve || n.ID == fxEndDegraded {
			continue
		}
		nodes = append(nodes, n)
	}
	def.Nodes = nodes

	edges := def.Edges[:0:0]
	for _, e := range def.Edges {
		if e.From == fxObserve || e.To == fxObserve || e.To == fxEndDegraded {
			continue
		}
		if e.From == fxSync && e.RouteKey == "SUCCEEDED" {
			e.To = fxEndCommit
		}
		edges = append(edges, e)
	}
	def.Edges = edges
	return def
}

// TestTodo_WF_COMP_003 proves planning/todos.md WF-COMP-003: retried mutations
// without compatible idempotency, unobserved irreversible effects and
// mutations claimed as simulatable are rejected, and a compiled plan declares
// its effect classes, logical effect keys and allowed execution modes.
func TestTodo_WF_COMP_003(t *testing.T) {
	t.Run("RED_retried_mutation_without_compatible_idempotency", func(t *testing.T) {
		t.Run("no_idempotency_key_mapping", func(t *testing.T) {
			def := effectsDefinition()
			nodeRef(t, &def, fxSync).Capability.IdempotencyKeyMapping = ""
			mustReject(t, def, effectsOptions(t), workflow.CodeNonIdempotentRetry)
		})
		t.Run("capability_declares_no_idempotency_policy", func(t *testing.T) {
			registry := capability.NewRegistry()
			for _, d := range []capability.Definition{
				fixtureCapability(capReadWorker, "people", capability.EffectReadOnly),
				fixtureCapability(capObservePayrol, "payroll", capability.EffectReadOnly),
			} {
				if err := registry.Register(d, echoHandler); err != nil {
					t.Fatalf("publish %s: %v", d.Key(), err)
				}
			}
			// A capability with a write effect but no idempotency policy is
			// exactly what a retried mutation must not be allowed to bind.
			loose := fixtureCapability(capSyncPayroll, "payroll", capability.EffectExternalMutation)
			loose.IdempotencyPolicyRef = "idempotency.none.v1"
			if err := registry.Register(loose, echoHandler); err != nil {
				t.Fatalf("publish %s: %v", loose.Key(), err)
			}
			opts := workflow.Options{Phase: workflow.PhaseP1B, Capabilities: registry}
			def := effectsDefinition()
			nodeRef(t, &def, fxSync).Capability.IdempotencyKeyMapping = ""
			mustReject(t, def, opts, workflow.CodeNonIdempotentRetry)
		})
		t.Run("irreversible_effect_is_never_auto_retried", func(t *testing.T) {
			def := effectsDefinition()
			nodeRef(t, &def, fxSync).Capability.ID = capBurnLetter
			nodeRef(t, &def, fxSync).InputSchema = capSchema(capBurnLetter, "request")
			nodeRef(t, &def, fxSync).OutputSchema = capSchema(capBurnLetter, "response")
			nodeRef(t, &def, fxSync).Capability.AuthorityScopes = []string{"scope:documents.write"}
			mustReject(t, def, effectsOptions(t), workflow.CodeNonIdempotentRetry)
		})
	})

	t.Run("RED_irreversible_effect_without_observe_or_repair_route", func(t *testing.T) {
		t.Run("no_observation_downstream", func(t *testing.T) {
			def := withoutObservation(effectsDefinition())
			mustReject(t, def, effectsOptions(t), workflow.CodeUnobservedEffect)
		})
		t.Run("no_failure_route", func(t *testing.T) {
			def := effectsDefinition()
			nodeRef(t, &def, fxSync).FailureRoute = ""
			mustReject(t, def, effectsOptions(t), workflow.CodeUnobservedEffect)
		})
	})

	t.Run("RED_mutation_in_simulation", func(t *testing.T) {
		t.Run("definition_claims_simulate_support", func(t *testing.T) {
			def := effectsDefinition()
			def.DeclaredModes = []workflow.ExecutionMode{workflow.ModeSimulate, workflow.ModeExecute}
			mustReject(t, def, effectsOptions(t), workflow.CodeMutationInSimulation)
		})
		t.Run("invocation_declares_simulate_mode", func(t *testing.T) {
			def := effectsDefinition()
			nodeRef(t, &def, fxSync).Capability.OperationMode = workflow.ModeSimulate
			mustReject(t, def, effectsOptions(t), workflow.CodeMutationInSimulation)
		})
	})

	t.Run("RED_write_effect_refused_in_p1a", func(t *testing.T) {
		opts := workflow.Options{Phase: workflow.PhaseP1A, Capabilities: effectsRegistry(t)}
		mustReject(t, effectsDefinition(), opts, workflow.CodeWriteEffectRefusedP1A)
	})

	t.Run("GREEN_plan_declares_effects_keys_and_modes", func(t *testing.T) {
		plan, err := workflow.Compile(effectsDefinition(), effectsOptions(t))
		if err != nil {
			t.Fatalf("the effect fixture must compile under P1B: %v", err)
		}
		if plan.Effects.ZeroEffect {
			t.Fatal("a plan that syncs payroll is not zero-effect")
		}
		wantClasses := map[string][]string{
			string(capability.EffectPure):             {fxEndCommit, fxEndDegraded, fxEndRepair},
			string(capability.EffectReadOnly):         {fxObserve, fxRead},
			string(capability.EffectExternalMutation): {fxSync},
		}
		if !reflect.DeepEqual(plan.Effects.NodesByClass, wantClasses) {
			t.Fatalf("effect classification\n got %v\nwant %v", plan.Effects.NodesByClass, wantClasses)
		}
		wantKeys := []string{"fixture.payroll.sync_worker/v1#payroll.worker_sync@effect_key"}
		if !reflect.DeepEqual(plan.Effects.EffectKeys, wantKeys) {
			t.Fatalf("logical effect keys\n got %v\nwant %v", plan.Effects.EffectKeys, wantKeys)
		}
		wantModes := []workflow.ExecutionMode{workflow.ModeExecute, workflow.ModeRepair}
		if !reflect.DeepEqual(plan.Effects.AllowedModes, wantModes) {
			t.Fatalf("allowed modes\n got %v\nwant %v", plan.Effects.AllowedModes, wantModes)
		}
		if len(plan.Effects.IrreversibleNodes) != 0 {
			t.Fatalf("no node is irreversible here, got %v", plan.Effects.IrreversibleNodes)
		}
		sync, ok := plan.Node(fxSync)
		if !ok {
			t.Fatalf("plan has no node %q", fxSync)
		}
		if !sync.SafePoint {
			t.Fatal("the compiler places a safe point at an external-effect node")
		}
	})

	t.Run("GREEN_p1a_reference_compiles_to_zero_effect", func(t *testing.T) {
		plan := mustCompilePromotion(t)
		if !plan.Effects.ZeroEffect {
			t.Fatalf("the P1A promotion reference must compile to zero effect, got %v", plan.Effects.NodesByClass)
		}
		if len(plan.Effects.EffectKeys) != 0 {
			t.Fatalf("a zero-effect plan has no effect keys, got %v", plan.Effects.EffectKeys)
		}
		var simulatable bool
		for _, m := range plan.Effects.AllowedModes {
			if m == workflow.ModeSimulate {
				simulatable = true
			}
		}
		if !simulatable {
			t.Fatalf("a zero-effect plan supports SIMULATE, got %v", plan.Effects.AllowedModes)
		}
		for _, n := range plan.Nodes {
			if n.EffectClass.IsWrite() {
				t.Fatalf("node %s carries write effect %s", n.ID, n.EffectClass)
			}
		}
	})

	t.Run("REFACTOR_capability_manifest_is_the_source_of_effect_truth", func(t *testing.T) {
		def := effectsDefinition()
		nodeRef(t, &def, fxSync).DeclaredEffect = capability.EffectPure
		mustReject(t, def, effectsOptions(t), workflow.CodeEffectDeclarationConflict)

		agreeing := effectsDefinition()
		nodeRef(t, &agreeing, fxSync).DeclaredEffect = capability.EffectExternalMutation
		plan, err := workflow.Compile(agreeing, effectsOptions(t))
		if err != nil {
			t.Fatalf("an author declaration matching the manifest compiles: %v", err)
		}
		sync, _ := plan.Node(fxSync)
		if sync.EffectClass != capability.EffectExternalMutation {
			t.Fatalf("compiled effect class %s does not come from the manifest", sync.EffectClass)
		}
	})

	t.Run("REFACTOR_pure_step_types_never_carry_an_effect", func(t *testing.T) {
		def := effectsDefinition()
		nodeRef(t, &def, fxEndCommit).DeclaredEffect = capability.EffectExternalMutation
		mustReject(t, def, effectsOptions(t), workflow.CodeEffectDeclarationConflict)
	})
}

// TestTodo_WF_COMP_003_Race compiles one definition from many goroutines at
// once. Compilation reads shared registry state and must produce byte-identical
// plans regardless of interleaving; a plan whose digest depended on map
// iteration order would show up here.
func TestTodo_WF_COMP_003_Race(t *testing.T) {
	const goroutines = 32
	registry := effectsRegistry(t)
	opts := workflow.Options{Phase: workflow.PhaseP1B, Capabilities: registry}

	want, err := workflow.Compile(effectsDefinition(), opts)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	plans := make([]*workflow.CompiledWorkflow, goroutines)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			plans[i], errs[i] = workflow.Compile(effectsDefinition(), opts)
		}(i)
	}
	wg.Wait()

	for i := 0; i < goroutines; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if plans[i].Digest() != want.Digest() {
			t.Fatalf("goroutine %d produced digest %s, want %s", i, plans[i].Digest(), want.Digest())
		}
		if !reflect.DeepEqual(plans[i], want) {
			t.Fatalf("goroutine %d produced a structurally different plan", i)
		}
		if err := plans[i].Verify(); err != nil {
			t.Fatalf("goroutine %d produced an unverifiable plan: %v", i, err)
		}
	}
}

// TestTodo_WF_COMP_003_Mutation kills one mutant per effect and idempotency
// rule.
func TestTodo_WF_COMP_003_Mutation(t *testing.T) {
	runMutations(t, effectsDefinition, effectsOptions, []mutationCase{
		{"drop_the_effect_binding", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, fxSync).Capability.EffectBinding = ""
		}, workflow.CodeNonIdempotentRetry},
		{"drop_the_idempotency_key_mapping", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, fxSync).Capability.IdempotencyKeyMapping = ""
		}, workflow.CodeNonIdempotentRetry},
		{"drop_the_failure_route", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, fxSync).FailureRoute = ""
		}, workflow.CodeUnobservedEffect},
		{"claim_simulate_support", func(t *testing.T, def *workflow.Definition) {
			def.DeclaredModes = append(def.DeclaredModes, workflow.ModeSimulate)
		}, workflow.CodeMutationInSimulation},
		{"contradict_the_manifest", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, fxSync).DeclaredEffect = capability.EffectReadOnly
		}, workflow.CodeEffectDeclarationConflict},
		{"unbounded_retry_policy", func(t *testing.T, def *workflow.Definition) {
			nodeRef(t, def, fxSync).Retry.MaxAttempts = 0
		}, workflow.CodeInvalidDefinition},
	})
}
