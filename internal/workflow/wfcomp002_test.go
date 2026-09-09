package workflow_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

const (
	synGoRoute   = "GO"
	synExitRoute = "EXIT"
	synEndOK     = "e_ok"
	synEndUnknwn = "e_unknown"
)

func synNodeID(i int) string { return fmt.Sprintf("d%d", i) }

func synDecisionNode(id string, routes []workflow.DecisionRoute) workflow.Node {
	return workflow.Node{
		ID:           id,
		Type:         workflow.StepDecision,
		InputSchema:  fixtureSchemaRef("SyntheticDecisionInput"),
		OutputSchema: fixtureSchemaRef("SyntheticDecisionResult"),
		Inputs:       []workflow.Field{{Path: "raise_ratio", Type: workflow.ValueType{Kind: workflow.KindDecimal}}},
		Outputs:      []workflow.Field{{Path: "route_key", Type: stringType()}},
		InputMappings: []workflow.Mapping{
			{Target: "raise_ratio", Source: inputSource("raise_ratio")},
		},
		Decision: &workflow.DecisionSpec{
			EvaluatorRef:       "engines.decisiontable",
			EvaluatorVersion:   1,
			InputDigestProfile: "hcmnext.workflow.InputMappingSet/v1",
			Routes:             routes,
			DefaultRoute:       routes[0].Key,
		},
		Governance: workflow.NodeGovernance{
			Purpose:               "SYNTHETIC",
			Classification:        "INTERNAL",
			RevalidationBoundary:  workflow.RevalidateNone,
			DataAccessManifestRef: "dam.synthetic/v1",
		},
	}
}

func synTerminal(id, code string) workflow.Node {
	return workflow.Node{
		ID:            id,
		Type:          workflow.StepEnd,
		Inputs:        fixtureTerminalInputs(),
		InputMappings: fixtureTerminalMappings(code),
		End: &workflow.EndSpec{
			TerminalCode:      code,
			RuntimeStatus:     workflow.RuntimeCompleted,
			CompletionMapping: fixtureCompletion("SIMULATED", "NOT_PLANNED", "NOT_STARTED", "NOT_APPLICABLE", "NOT_APPLICABLE"),
		},
		Governance: workflow.NodeGovernance{
			Purpose:               "SYNTHETIC",
			Classification:        "INTERNAL",
			RevalidationBoundary:  workflow.RevalidatePreClosure,
			DataAccessManifestRef: "dam.synthetic/v1",
		},
	}
}

// syntheticDefinition builds a k-node DECISION graph whose GO edges point
// wherever targets says. The shape is deliberately free enough to produce
// unreachable nodes and cycles, which is exactly what the graph-safety
// property has to be tested against.
func syntheticDefinition(targets []string) workflow.Definition {
	def := workflow.Definition{
		WorkflowID:        "fixture.workflows.synthetic",
		Version:           1,
		Name:              "Synthetic graph",
		InputSchema:       fixtureSchemaRef("SyntheticInput"),
		OutputSchema:      fixtureSchemaRef("SyntheticResult"),
		VariablesSchema:   fixtureSchemaRef("SyntheticVariables"),
		TenantScope:       "acme",
		OrganizationScope: "acme/engineering",
		RiskClass:         "LOW",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeSimulate},
		TerminalProfile:   workflow.TerminalProfileSimulateOnly,
		StartNodeID:       synNodeID(0),
		Inputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
			{Path: "raise_ratio", Type: workflow.ValueType{Kind: workflow.KindDecimal}},
		},
		Outputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
			{Path: "terminal_code", Type: stringType()},
		},
		Limits:                workflow.Limits{MaxFanOut: 4, MaxDepth: 64, MaxNodes: 64},
		FailurePolicyRef:      "policy.workflow.failure.simulation/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.simulation/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.synthetic/v1",
	}
	for i := range targets {
		def.Nodes = append(def.Nodes, synDecisionNode(synNodeID(i), []workflow.DecisionRoute{
			{Key: synGoRoute, Predicate: fmt.Sprintf("pred_%d", i), Precedence: 10},
		}))
		def.Edges = append(def.Edges,
			workflow.Edge{From: synNodeID(i), To: targets[i], RouteKey: synGoRoute},
			workflow.Edge{From: synNodeID(i), To: synEndUnknwn, RouteKey: "UNKNOWN"},
		)
	}
	def.Nodes = append(def.Nodes,
		synTerminal(synEndOK, "SYNTHETIC_OK"),
		synTerminal(synEndUnknwn, "SYNTHETIC_UNKNOWN"),
	)
	return def
}

// syntheticOptions compiles synthetic graphs; they bind no capability, so an
// empty registry is enough.
func syntheticOptions(*testing.T) workflow.Options {
	return workflow.Options{Phase: workflow.PhaseP1A}
}

// graphOracle recomputes reachability and acyclicity independently of the
// compiler, so the property test compares two implementations rather than the
// compiler with itself.
func graphOracle(targets []string) (allReachable, endReachable, acyclic bool) {
	next := map[string][]string{}
	for i, to := range targets {
		next[synNodeID(i)] = []string{to, synEndUnknwn}
	}
	reached := map[string]bool{}
	stack := []string{synNodeID(0)}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if reached[id] {
			continue
		}
		reached[id] = true
		stack = append(stack, next[id]...)
	}
	allReachable = true
	for i := range targets {
		if !reached[synNodeID(i)] {
			allReachable = false
		}
	}
	if !reached[synEndUnknwn] {
		allReachable = false
	}
	endReachable = reached[synEndOK]

	state := map[string]int{}
	acyclic = true
	var walk func(string)
	walk = func(id string) {
		state[id] = 1
		for _, to := range next[id] {
			switch state[to] {
			case 0:
				walk(to)
			case 1:
				acyclic = false
			}
		}
		state[id] = 2
	}
	walk(synNodeID(0))
	return allReachable, endReachable, acyclic
}

// TestTodo_WF_COMP_002 proves planning/todos.md WF-COMP-002: unreachable
// nodes, implicit first edges, missing routes, invalid terminal paths,
// undeclared cycles and unbounded fan-out or depth are all compile errors, and
// a sound graph compiles with a reachability proof and explicit routes.
func TestTodo_WF_COMP_002(t *testing.T) {
	t.Run("RED_unreachable_node", func(t *testing.T) {
		def := syntheticDefinition([]string{synEndOK, synEndOK})
		mustReject(t, def, syntheticOptions(t), workflow.CodeUnreachableNode)
	})

	t.Run("RED_implicit_first_edge", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		edgeRef(t, &def, workflow.PromotionNodeBuildProposal, "SUCCEEDED").RouteKey = ""
		mustReject(t, def, promotionOptions(t), workflow.CodeImplicitFirstEdge)
	})

	t.Run("RED_missing_unknown_route", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		kept := def.Edges[:0]
		for _, e := range def.Edges {
			if e.From == workflow.PromotionNodeRaiseThreshold && e.RouteKey == "UNKNOWN" {
				continue
			}
			kept = append(kept, e)
		}
		def.Edges = kept
		d := mustReject(t, def, promotionOptions(t), workflow.CodeMissingRoute)
		if !d.HasAt(workflow.CodeMissingRoute, workflow.PromotionNodeRaiseThreshold) {
			t.Fatalf("the missing UNKNOWN route must be reported at the DECISION, got %v", d.Errors)
		}
	})

	t.Run("RED_invalid_terminal_path", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		def.Edges = append(def.Edges, workflow.Edge{
			From:     workflow.PromotionNodeEndUnknown,
			To:       workflow.PromotionNodeSnapshotWorker,
			RouteKey: "SUCCEEDED",
		})
		mustReject(t, def, promotionOptions(t), workflow.CodeInvalidTerminalPath)
	})

	t.Run("RED_undeclared_cycle", func(t *testing.T) {
		def := syntheticDefinition([]string{synNodeID(1), synNodeID(0)})
		mustReject(t, def, syntheticOptions(t), workflow.CodeUndeclaredCycle)
	})

	t.Run("RED_unbounded_fanout_and_depth", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*workflow.Definition)
		}{
			{"no_fan_out_limit", func(def *workflow.Definition) { def.Limits.MaxFanOut = 0 }},
			{"no_depth_limit", func(def *workflow.Definition) { def.Limits.MaxDepth = 0 }},
			{"no_node_limit", func(def *workflow.Definition) { def.Limits.MaxNodes = 0 }},
			{"fan_out_exceeded", func(def *workflow.Definition) { def.Limits.MaxFanOut = 1 }},
			{"depth_exceeded", func(def *workflow.Definition) { def.Limits.MaxDepth = 2 }},
			{"node_count_exceeded", func(def *workflow.Definition) { def.Limits.MaxNodes = 2 }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				def := workflow.PromotionReferenceDefinition()
				tc.mutate(&def)
				mustReject(t, def, promotionOptions(t), workflow.CodeUnboundedFanout)
			})
		}
	})

	t.Run("GREEN_reachability_proof_and_explicit_routes", func(t *testing.T) {
		plan := mustCompilePromotion(t)

		if plan.Reachability.StartNodeID != workflow.PromotionNodeSnapshotWorker {
			t.Fatalf("reachability proof names start %q", plan.Reachability.StartNodeID)
		}
		if len(plan.Reachability.Order) != len(plan.Nodes) {
			t.Fatalf("reachability order covers %d of %d nodes", len(plan.Reachability.Order), len(plan.Nodes))
		}
		if len(plan.Reachability.BackEdges) != 0 {
			t.Fatalf("the reference workflow is acyclic; got back edges %v", plan.Reachability.BackEdges)
		}
		if len(plan.Reachability.Terminals) != 5 {
			t.Fatalf("expected five terminals, got %v", plan.Reachability.Terminals)
		}
		for _, n := range plan.Nodes {
			if _, ok := plan.Reachability.Depth[n.ID]; !ok {
				t.Fatalf("node %s has no recorded depth", n.ID)
			}
			if n.Type == workflow.StepEnd {
				if len(n.Routes) != 0 {
					t.Fatalf("terminal %s carries outgoing routes %v", n.ID, n.Routes)
				}
				continue
			}
			if len(n.Routes) == 0 {
				t.Fatalf("node %s carries no explicit outcome routes", n.ID)
			}
			for _, key := range n.Routes {
				if key == "" {
					t.Fatalf("node %s carries an unnamed route", n.ID)
				}
			}
		}
		if plan.Limits.MaxFanOut == 0 || plan.Limits.MaxDepth == 0 || plan.Limits.MaxNodes == 0 {
			t.Fatalf("compiled limits must be bounded, got %+v", plan.Limits)
		}
	})

	t.Run("GREEN_declared_guarded_cycle_compiles", func(t *testing.T) {
		def := cyclicDefinition(true)
		plan, err := workflow.Compile(def, syntheticOptions(t))
		if err != nil {
			t.Fatalf("a declared, guarded, bounded cycle compiles: %v", err)
		}
		if len(plan.Reachability.BackEdges) != 1 {
			t.Fatalf("expected exactly one recorded back edge, got %v", plan.Reachability.BackEdges)
		}
	})

	t.Run("REFACTOR_diagnostics_locate_node_edge_and_field", func(t *testing.T) {
		def := workflow.PromotionReferenceDefinition()
		def.Edges = append(def.Edges, workflow.Edge{
			From:     workflow.PromotionNodeObserveDrift,
			To:       workflow.PromotionNodeEndUnknown,
			RouteKey: "PASS",
		})
		nodeRef(t, &def, workflow.PromotionNodeEvaluateBand).InputMappings[0].Target = "not_an_input"

		_, err := workflow.Compile(def, promotionOptions(t))
		d := diagnostics(t, err)

		var sawEdge, sawField bool
		for _, e := range d.Errors {
			if e.Location.EdgeFrom == workflow.PromotionNodeObserveDrift &&
				e.Location.EdgeTo == workflow.PromotionNodeEndUnknown && e.Location.RouteKey == "PASS" {
				sawEdge = true
			}
			if e.Location.NodeID == workflow.PromotionNodeEvaluateBand && e.Location.Field == "not_an_input" {
				sawField = true
			}
		}
		if !sawEdge {
			t.Fatalf("an edge diagnostic names its source, target and route: %v", d.Errors)
		}
		if !sawField {
			t.Fatalf("a mapping diagnostic names its node and field: %v", d.Errors)
		}
	})
}

// cyclicDefinition builds d0 -> d1 -> d2 -> d0 with an explicit exit from d2,
// optionally declaring the cycle with a bound and a DECISION guard.
func cyclicDefinition(declare bool) workflow.Definition {
	def := syntheticDefinition([]string{synNodeID(1), synNodeID(2), synNodeID(0)})
	// d2 additionally routes EXIT to the success terminal so the graph still
	// terminates and e_ok is reachable.
	for i := range def.Nodes {
		if def.Nodes[i].ID != synNodeID(2) {
			continue
		}
		def.Nodes[i].Decision.Routes = append(def.Nodes[i].Decision.Routes,
			workflow.DecisionRoute{Key: synExitRoute, Predicate: "pred_exit", Precedence: 20})
	}
	def.Edges = append(def.Edges,
		workflow.Edge{From: synNodeID(2), To: synEndOK, RouteKey: synExitRoute})
	if declare {
		def.Limits.DeclaredCycles = []workflow.CycleDeclaration{{
			EntryNodeID:   synNodeID(0),
			GuardNodeID:   synNodeID(2),
			MaxIterations: 3,
		}}
	}
	return def
}

// TestTodo_WF_COMP_002_Property compiles randomly wired graphs and checks the
// compiler's verdict against an independent reachability and cycle oracle. A
// graph compiles exactly when every node is reachable, the success terminal is
// reachable and no undeclared cycle exists.
func TestTodo_WF_COMP_002_Property(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x5eed, 0xf00d))
	const iterations = 300

	var compiled, rejected int
	for i := 0; i < iterations; i++ {
		k := 2 + rng.IntN(5)
		targets := make([]string, k)
		for j := range targets {
			if rng.IntN(3) == 0 {
				targets[j] = synEndOK
				continue
			}
			targets[j] = synNodeID(rng.IntN(k))
		}

		allReachable, endReachable, acyclic := graphOracle(targets)
		wantOK := allReachable && endReachable && acyclic

		def := syntheticDefinition(targets)
		plan, err := workflow.Compile(def, syntheticOptions(t))
		gotOK := err == nil

		if gotOK != wantOK {
			t.Fatalf("targets=%v: oracle says ok=%v (reachable=%v end=%v acyclic=%v), compiler says ok=%v (%v)",
				targets, wantOK, allReachable, endReachable, acyclic, gotOK, err)
		}
		if gotOK {
			compiled++
			if len(plan.Reachability.Order) != len(plan.Nodes) {
				t.Fatalf("targets=%v: a compiled plan orders every node", targets)
			}
			if len(plan.Reachability.BackEdges) != 0 {
				t.Fatalf("targets=%v: a compiled acyclic plan records no back edges", targets)
			}
			again, err := workflow.Compile(syntheticDefinition(targets), syntheticOptions(t))
			if err != nil {
				t.Fatalf("targets=%v: recompilation failed: %v", targets, err)
			}
			if again.Digest() != plan.Digest() {
				t.Fatalf("targets=%v: recompilation produced a different digest", targets)
			}
			continue
		}
		rejected++
		d := diagnostics(t, err)
		if len(d.Errors) == 0 {
			t.Fatalf("targets=%v: rejection carried no diagnostic", targets)
		}
	}
	if compiled == 0 || rejected == 0 {
		t.Fatalf("the generator must produce both sound and unsound graphs; got %d compiled, %d rejected",
			compiled, rejected)
	}
}

// TestTodo_WF_COMP_002_Fault drives each malformed graph shape individually.
func TestTodo_WF_COMP_002_Fault(t *testing.T) {
	cases := []struct {
		name   string
		build  func(t *testing.T) workflow.Definition
		opts   func(*testing.T) workflow.Options
		code   string
		nodeID string
	}{
		{
			name: "start_node_is_not_declared",
			build: func(*testing.T) workflow.Definition {
				def := workflow.PromotionReferenceDefinition()
				def.StartNodeID = "no_such_node"
				return def
			},
			opts: promotionOptions,
			code: workflow.CodeUnresolvedRef,
		},
		{
			name: "edge_target_does_not_exist",
			build: func(t *testing.T) workflow.Definition {
				def := workflow.PromotionReferenceDefinition()
				edgeRef(t, &def, workflow.PromotionNodeSnapshotWorker, "REJECTED").To = "ghost"
				return def
			},
			opts: promotionOptions,
			code: workflow.CodeUnresolvedRef,
		},
		{
			name: "duplicate_route_key",
			build: func(t *testing.T) workflow.Definition {
				def := workflow.PromotionReferenceDefinition()
				def.Edges = append(def.Edges, workflow.Edge{
					From:     workflow.PromotionNodeObserveDrift,
					To:       workflow.PromotionNodeEndUnknown,
					RouteKey: "PASS",
				})
				return def
			},
			opts: promotionOptions,
			code: workflow.CodeDuplicateRoute,
		},
		{
			name: "route_the_step_type_cannot_produce",
			build: func(t *testing.T) workflow.Definition {
				def := workflow.PromotionReferenceDefinition()
				def.Edges = append(def.Edges, workflow.Edge{
					From:     workflow.PromotionNodeObserveDrift,
					To:       workflow.PromotionNodeEndUnknown,
					RouteKey: "AMBIGUOUS",
				})
				return def
			},
			opts: promotionOptions,
			code: workflow.CodeUnknownRoute,
		},
		{
			name: "self_loop_without_a_declaration",
			build: func(t *testing.T) workflow.Definition {
				def := syntheticDefinition([]string{synNodeID(0), synEndOK})
				return def
			},
			opts: syntheticOptions,
			code: workflow.CodeUndeclaredCycle,
		},
		{
			name: "declared_cycle_without_a_decision_guard",
			build: func(t *testing.T) workflow.Definition {
				def := cyclicDefinition(true)
				def.Limits.DeclaredCycles[0].GuardNodeID = synEndOK
				return def
			},
			opts: syntheticOptions,
			code: workflow.CodeUndeclaredCycle,
		},
		{
			name: "declared_cycle_without_an_iteration_bound",
			build: func(t *testing.T) workflow.Definition {
				def := cyclicDefinition(true)
				def.Limits.DeclaredCycles[0].MaxIterations = 0
				return def
			},
			opts: syntheticOptions,
			code: workflow.CodeUndeclaredCycle,
		},
		{
			name: "terminal_with_an_outgoing_edge",
			build: func(t *testing.T) workflow.Definition {
				def := syntheticDefinition([]string{synEndOK})
				def.Edges = append(def.Edges, workflow.Edge{
					From: synEndOK, To: synEndUnknwn, RouteKey: "SUCCEEDED",
				})
				return def
			},
			opts: syntheticOptions,
			code: workflow.CodeInvalidTerminalPath,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mustReject(t, tc.build(t), tc.opts(t), tc.code)
		})
	}
}

// TestTodo_WF_COMP_002_Mutation kills one mutant per graph-safety rule.
func TestTodo_WF_COMP_002_Mutation(t *testing.T) {
	runMutations(t, workflow.PromotionReferenceDefinition, promotionOptions, []mutationCase{
		{"drop_a_capability_ambiguous_route", func(t *testing.T, def *workflow.Definition) {
			kept := def.Edges[:0]
			for _, e := range def.Edges {
				if e.From == workflow.PromotionNodeSnapshotWorker && e.RouteKey == "AMBIGUOUS" {
					continue
				}
				kept = append(kept, e)
			}
			def.Edges = kept
		}, workflow.CodeMissingRoute},
		{"drop_an_observe_partial_route", func(t *testing.T, def *workflow.Definition) {
			kept := def.Edges[:0]
			for _, e := range def.Edges {
				if e.From == workflow.PromotionNodeObserveDrift && e.RouteKey == "PARTIAL" {
					continue
				}
				kept = append(kept, e)
			}
			def.Edges = kept
		}, workflow.CodeMissingRoute},
		{"orphan_a_terminal", func(t *testing.T, def *workflow.Definition) {
			edgeRef(t, def, workflow.PromotionNodeRaiseThreshold, "EXCEEDS_THRESHOLD").To =
				workflow.PromotionNodeEndUnknown
		}, workflow.CodeUnreachableNode},
		{"redirect_a_route_into_a_cycle", func(t *testing.T, def *workflow.Definition) {
			edgeRef(t, def, workflow.PromotionNodeObserveDrift, "UNKNOWN").To =
				workflow.PromotionNodeSnapshotWorker
		}, workflow.CodeUndeclaredCycle},
		{"blank_a_route_key", func(t *testing.T, def *workflow.Definition) {
			edgeRef(t, def, workflow.PromotionNodeRaiseThreshold, "WITHIN_THRESHOLD").RouteKey = ""
		}, workflow.CodeImplicitFirstEdge},
	})
}
