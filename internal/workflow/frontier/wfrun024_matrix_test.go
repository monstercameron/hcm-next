package frontier_test

import (
	"encoding/json"
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
)

// TestTodo_WF_RUN_024_Property generates random outcome sequences over the
// promotion plan and asserts the invariants that make advancement a function
// rather than a policy: the same three arguments always produce the same
// transition, the arguments are never mutated, and no successor, intent or
// route appears that the plan did not declare.
func TestTodo_WF_RUN_024_Property(t *testing.T) {
	plan := promotionPlan(t)

	for iter := uint64(1); iter <= 64; iter++ {
		rng := rand.New(rand.NewPCG(iter, 0x5741524e))
		state := seed(t, plan)

		for steps := 0; ; steps++ {
			if steps > 4*len(plan.Nodes) {
				t.Fatalf("iteration %d: walk did not settle in %d steps (frontier %v)", iter, steps, state.Frontier)
			}
			if len(state.Frontier) == 0 {
				t.Fatalf("iteration %d: frontier emptied without completing", iter)
			}
			nodeID := state.Frontier[rng.IntN(len(state.Frontier))]
			node, ok := plan.Node(nodeID)
			if !ok {
				t.Fatalf("iteration %d: frontier carries %q, which the plan does not declare", iter, nodeID)
			}

			out := frontier.NodeOutcome{NodeID: nodeID, OutputDigest: "sha256:property"}
			if node.Type != workflow.StepEnd {
				out.Outcome = workflow.Outcome(node.Routes[rng.IntN(len(node.Routes))])
			}

			before := state.Digest()
			tr, err := frontier.Advance(plan, state, out)
			if err != nil {
				t.Fatalf("iteration %d: Advance(%s, %s): %v", iter, nodeID, out.Outcome, err)
			}
			// Determinism: the identical call reproduces the identical value.
			again, err := frontier.Advance(plan, state, out)
			if err != nil {
				t.Fatalf("iteration %d: repeated Advance refused: %v", iter, err)
			}
			if tr.Digest() != again.Digest() {
				t.Fatalf("iteration %d: repeated evaluation of %s digests to %s then %s",
					iter, nodeID, tr.Digest(), again.Digest())
			}
			// Purity: the caller's snapshot is untouched.
			if after := state.Digest(); after != before {
				t.Fatalf("iteration %d: Advance mutated the state it was given (%s -> %s)", iter, before, after)
			}

			assertNoImplicitRoute(t, plan, node, tr)
			assertSkippedActivatesNothing(t, tr)
			assertIntentsMatchStepTypes(t, plan, tr)

			state = tr.Next
			if tr.Complete {
				if len(state.Frontier) != 0 {
					t.Fatalf("iteration %d: completed with frontier %v", iter, state.Frontier)
				}
				if state.Terminal.TerminalCode == "" {
					t.Fatalf("iteration %d: completed with no terminal code", iter)
				}
				break
			}
		}
	}

	t.Run("an undeclared outcome is never routed implicitly", func(t *testing.T) {
		state := seed(t, plan)
		// Every non-DECISION node refuses a route key it cannot produce.
		_ = refuses(t, plan, state, frontier.NodeOutcome{
			NodeID:  plan.StartNodeID,
			Outcome: "NOT_A_DECLARED_OUTCOME",
		}, frontier.CodeUnknownOutcome)

		// The DECISION declares a default, so the same undeclared key takes it
		// — explicitly, and marked as having taken it.
		steps := walk(t, plan, state, promotionRoutes[:4])
		tr := step(t, plan, steps[3].Next, frontier.NodeOutcome{
			NodeID:  workflow.PromotionNodeRaiseThreshold,
			Outcome: "NOT_A_DECLARED_OUTCOME",
		})
		if !tr.Successors[0].ViaDefault || tr.RouteKey != "WITHIN_THRESHOLD" {
			t.Fatalf("undeclared decision route produced %+v, want the declared default", tr.Successors)
		}
	})
}

// assertNoImplicitRoute checks that every successor arrived over an edge the
// plan declares, taken by an outcome the node declares, a declared default or
// a declared failure route.
func assertNoImplicitRoute(t *testing.T, plan *workflow.CompiledWorkflow, node workflow.CompiledNode, tr frontier.Transition) {
	t.Helper()
	for _, s := range tr.Successors {
		if s.ViaFailure {
			if node.FailureRoute != s.NodeID {
				t.Fatalf("%s reached via a failure route the node does not declare", s.NodeID)
			}
			continue
		}
		if s.ViaDefault {
			if node.Decision == nil || node.Decision.DefaultRoute != s.RouteKey {
				t.Fatalf("%s reached via a default route the node does not declare", s.NodeID)
			}
		} else if !contains(node.Routes, s.RouteKey) {
			t.Fatalf("%s reached over route %q, which %s does not declare (%v)",
				s.NodeID, s.RouteKey, node.ID, node.Routes)
		}
		found := false
		for _, e := range plan.Edges {
			if e.From == node.ID && e.RouteKey == s.RouteKey && e.To == s.NodeID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("no declared edge %s --%s--> %s", node.ID, s.RouteKey, s.NodeID)
		}
	}
}

// assertSkippedActivatesNothing checks the RED clause directly: a skipped path
// never activates work.
func assertSkippedActivatesNothing(t *testing.T, tr frontier.Transition) {
	t.Helper()
	for _, id := range tr.Skipped {
		if _, ok := tr.Intent(id); ok {
			t.Fatalf("skipped node %s carries a scheduling intent", id)
		}
		if contains(tr.Frontier, id) {
			t.Fatalf("skipped node %s stayed on the frontier", id)
		}
		status, ok := tr.Next.Node(id)
		if !ok || status.State != frontier.NodeSkipped {
			t.Fatalf("skipped node %s is %+v", id, status)
		}
	}
}

// assertIntentsMatchStepTypes checks that the scheduling intent a successor
// carries is the one its compiled step type calls for, and nothing else.
func assertIntentsMatchStepTypes(t *testing.T, plan *workflow.CompiledWorkflow, tr frontier.Transition) {
	t.Helper()
	want := map[workflow.StepType]frontier.IntentKind{
		workflow.StepApproval: frontier.IntentWorkItemRequired,
		workflow.StepTask:     frontier.IntentWorkItemRequired,
		workflow.StepSignal:   frontier.IntentSignalSubscriptionRequired,
		workflow.StepWait:     frontier.IntentTimerRequired,
	}
	for _, i := range tr.Intents {
		if !i.Kind.Valid() {
			t.Fatalf("intent %+v names no declared kind", i)
		}
		if i.Kind == frontier.IntentComplete {
			if !tr.Complete {
				t.Fatalf("COMPLETE intent on a transition that did not complete")
			}
			continue
		}
		node, ok := plan.Node(i.NodeID)
		if !ok {
			t.Fatalf("intent for %q, which the plan does not declare", i.NodeID)
		}
		expected, special := want[node.Type]
		if !special {
			expected = frontier.IntentReady
		}
		if i.Kind != expected {
			t.Fatalf("intent for %s (%s) is %s, want %s", i.NodeID, node.Type, i.Kind, expected)
		}
	}
}

// TestTodo_WF_RUN_024_Golden pins the whole promotion advancement sequence,
// byte for byte, with the digest each step minted. The checked-in vector is
// the contract: a change to it is a change to every transition digest this
// package has produced.
func TestTodo_WF_RUN_024_Golden(t *testing.T) {
	plan := promotionPlan(t)
	steps := walk(t, plan, seed(t, plan), promotionRoutes)

	type rendered struct {
		Step        int             `json:"step"`
		Digest      string          `json:"digest"`
		StateDigest string          `json:"state_digest"`
		Transition  json.RawMessage `json:"transition"`
	}
	out := make([]rendered, 0, len(steps))
	for i, tr := range steps {
		b, err := tr.JSON()
		if err != nil {
			t.Fatalf("render step %d: %v", i, err)
		}
		out = append(out, rendered{
			Step:        i + 1,
			Digest:      tr.Digest(),
			StateDigest: tr.Next.Digest(),
			Transition:  json.RawMessage(b),
		})
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	golden(t, "promotion_advancement.json", append(b, '\n'))

	// Recomputing the same walk reproduces the same digests, which is the
	// property the golden bytes stand for.
	repeat := walk(t, plan, seed(t, plan), promotionRoutes)
	for i := range steps {
		if steps[i].Digest() != repeat[i].Digest() {
			t.Fatalf("step %d digests to %s then %s", i+1, steps[i].Digest(), repeat[i].Digest())
		}
	}
}

// TestTodo_WF_RUN_024_Race proves the function owns no shared mutable state:
// many goroutines advancing the same plan and the same snapshot concurrently
// all derive the identical transition, and none of them disturbs the snapshot
// the others are reading.
func TestTodo_WF_RUN_024_Race(t *testing.T) {
	plan := promotionPlan(t)
	state := seed(t, plan)
	want := step(t, plan, state, promotionRoutes[0])
	before := state.Digest()

	const workers = 64
	digests := make([]string, workers)
	walks := make([]string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tr, err := frontier.Advance(plan, state, promotionRoutes[0])
			if err != nil {
				t.Errorf("worker %d: Advance: %v", i, err)
				return
			}
			digests[i] = tr.Digest()

			// A whole independent walk from the same seed state, so the
			// workers exercise more than one call each.
			local := state
			for _, out := range promotionRoutes {
				next, err := frontier.Advance(plan, local, out)
				if err != nil {
					t.Errorf("worker %d: walk %s: %v", i, out.NodeID, err)
					return
				}
				local = next.Next
			}
			walks[i] = local.Digest()
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		if digests[i] != want.Digest() {
			t.Fatalf("worker %d derived %s, want %s", i, digests[i], want.Digest())
		}
		if walks[i] != walks[0] {
			t.Fatalf("worker %d finished at state %s, worker 0 finished at %s", i, walks[i], walks[0])
		}
	}
	if after := state.Digest(); after != before {
		t.Fatalf("the shared snapshot changed under concurrent advancement (%s -> %s)", before, after)
	}
}

// TestTodo_WF_RUN_024_Fault drives every way an advancement can be asked for
// something the plan does not license. Each case must be a typed refusal
// naming its own code — never a guessed route, a silent no-op or a panic.
func TestTodo_WF_RUN_024_Fault(t *testing.T) {
	plan := promotionPlan(t)

	t.Run("no plan", func(t *testing.T) {
		_, err := frontier.Advance(nil, frontier.InstanceState{}, frontier.NodeOutcome{})
		if frontier.CodeOf(err) != frontier.CodeInvalidPlan {
			t.Fatalf("code = %q, want INVALID_PLAN (%v)", frontier.CodeOf(err), err)
		}
		errIsFrontier(t, err)
	})

	t.Run("state pinned to another plan", func(t *testing.T) {
		state := seed(t, plan)
		state.PlanDigest = "sha256:some-other-plan"
		err := refuses(t, plan, state, promotionRoutes[0], frontier.CodePlanMismatch)
		errIsFrontier(t, err)
	})

	t.Run("outcome for a node the plan does not declare", func(t *testing.T) {
		state := seed(t, plan)
		refuses(t, plan, state, frontier.NodeOutcome{NodeID: "no_such_node", Outcome: workflow.OutcomeSucceeded},
			frontier.CodeUnknownNode)
	})

	t.Run("outcome for a node that is not on the frontier", func(t *testing.T) {
		state := seed(t, plan)
		refuses(t, plan, state, frontier.NodeOutcome{
			NodeID: workflow.PromotionNodeObserveDrift, Outcome: workflow.OutcomePass,
		}, frontier.CodeNodeNotActive)
	})

	t.Run("the same completion delivered twice", func(t *testing.T) {
		state := seed(t, plan)
		tr := step(t, plan, state, promotionRoutes[0])
		refuses(t, plan, tr.Next, promotionRoutes[0], frontier.CodeNodeNotActive)
	})

	t.Run("advancing a completed instance", func(t *testing.T) {
		steps := walk(t, plan, seed(t, plan), promotionRoutes)
		refuses(t, plan, steps[len(steps)-1].Next, promotionRoutes[0], frontier.CodeAlreadyComplete)
	})

	t.Run("an outcome the step type cannot produce", func(t *testing.T) {
		refuses(t, plan, seed(t, plan), frontier.NodeOutcome{
			NodeID: plan.StartNodeID, Outcome: "TOTALLY_MADE_UP",
		}, frontier.CodeUnknownOutcome)
	})

	t.Run("a decision route nobody declared, with no default", func(t *testing.T) {
		joins := joinPlan()
		state, err := frontier.Seed(joins, "wf-no-default")
		if err != nil {
			t.Fatalf("Seed: %v", err)
		}
		refuses(t, joins, state, frontier.NodeOutcome{NodeID: "fan", Outcome: "TO_C"},
			frontier.CodeNoMatchingRoute)
	})

	t.Run("a declared outcome whose edge was removed", func(t *testing.T) {
		joins := joinPlan()
		kept := joins.Edges[:0:0]
		for _, e := range joins.Edges {
			if e.From == "fan" && e.RouteKey == "TO_A" {
				continue
			}
			kept = append(kept, e)
		}
		joins.Edges = kept
		state, err := frontier.Seed(joins, "wf-missing-edge")
		if err != nil {
			t.Fatalf("Seed: %v", err)
		}
		refuses(t, joins, state, frontier.NodeOutcome{NodeID: "fan", Outcome: "TO_A"},
			frontier.CodeMissingRoute)
	})

	t.Run("a failed attempt with no declared failure route", func(t *testing.T) {
		refuses(t, plan, seed(t, plan), frontier.NodeOutcome{
			NodeID: plan.StartNodeID, Failed: true, ErrorClass: "PROVIDER_TIMEOUT",
		}, frontier.CodeNoFailureRoute)
	})

	t.Run("an await a step type cannot raise", func(t *testing.T) {
		refuses(t, plan, seed(t, plan), frontier.NodeOutcome{
			NodeID: plan.StartNodeID, Await: frontier.AwaitTimer,
		}, frontier.CodeAwaitNotAdmitted)
	})

	t.Run("an outcome that both completes and awaits", func(t *testing.T) {
		refuses(t, plan, seed(t, plan), frontier.NodeOutcome{
			NodeID: plan.StartNodeID, Outcome: workflow.OutcomeSucceeded, Await: frontier.AwaitWorkItem,
		}, frontier.CodeAwaitNotAdmitted)
	})

	t.Run("an outcome that both awaits and fails", func(t *testing.T) {
		refuses(t, plan, seed(t, plan), frontier.NodeOutcome{
			NodeID: plan.StartNodeID, Await: frontier.AwaitWorkItem, Failed: true,
		}, frontier.CodeAwaitNotAdmitted)
	})

	t.Run("a join with no declared strategy", func(t *testing.T) {
		joins := joinPlan()
		state := bothBranchesReady(t, joins)
		state.Joins = nil
		refuses(t, joins, state, frontier.NodeOutcome{NodeID: "branch_a", Outcome: workflow.OutcomeSucceeded},
			frontier.CodeJoinNotDeclared)
	})

	t.Run("a quorum count its branch set cannot meet", func(t *testing.T) {
		_, err := frontier.Seed(joinPlan(), "wf-bad-quorum", frontier.JoinDeclaration{
			NodeID: "gate", Strategy: frontier.JoinQuorum, RequiredCount: 9,
		})
		if frontier.CodeOf(err) != frontier.CodeInvalidJoinDeclaration {
			t.Fatalf("code = %q, want INVALID_JOIN_DECLARATION (%v)", frontier.CodeOf(err), err)
		}
	})

	t.Run("an END with no compiled terminal", func(t *testing.T) {
		joins := joinPlan()
		for i := range joins.Nodes {
			if joins.Nodes[i].Type == workflow.StepEnd {
				joins.Nodes[i].Terminal = nil
			}
		}
		state, err := frontier.Seed(joins, "wf-no-terminal")
		if err != nil {
			t.Fatalf("Seed: %v", err)
		}
		state.Nodes = []frontier.NodeStatus{{NodeID: "end_join", State: frontier.NodeReady}}
		state.Frontier = []string{"end_join"}
		refuses(t, joins, state, frontier.NodeOutcome{NodeID: "end_join"}, frontier.CodeMissingTerminal)
	})

	t.Run("a handler that restates the terminal differently", func(t *testing.T) {
		steps := walk(t, plan, seed(t, plan), promotionRoutes[:5])
		refuses(t, plan, steps[4].Next, frontier.NodeOutcome{
			NodeID:   workflow.PromotionNodeEndApproval,
			Terminal: frontier.TerminalResult{TerminalCode: "SIMULATION_COMPLETE"},
		}, frontier.CodeTerminalMismatch)
	})

	t.Run("a terminal that would leave work on the frontier", func(t *testing.T) {
		state := seed(t, plan)
		state.Nodes = []frontier.NodeStatus{
			{NodeID: workflow.PromotionNodeEndApproval, State: frontier.NodeReady},
			{NodeID: workflow.PromotionNodeObserveDrift, State: frontier.NodeReady},
		}
		state.Frontier = []string{workflow.PromotionNodeEndApproval, workflow.PromotionNodeObserveDrift}
		err := refuses(t, plan, state, frontier.NodeOutcome{NodeID: workflow.PromotionNodeEndApproval},
			frontier.CodeTerminalFrontierRemains)
		// The refusal explains itself by naming what is still open.
		var typed *frontier.Error
		if !asFrontierError(err, &typed) {
			t.Fatalf("refusal is not a *frontier.Error: %v", err)
		}
		if !containsSubstring(typed.Detail, workflow.PromotionNodeObserveDrift) {
			t.Fatalf("refusal detail %q does not name the node still on the frontier", typed.Detail)
		}
	})
}

// TestTodo_WF_RUN_024_Mutation kills the mutants the RED clause names: a
// transition whose content changed but whose digest did not, a state whose
// frontier no longer matches its node states, and routing decisions that would
// still "work" if the declared route were ignored.
func TestTodo_WF_RUN_024_Mutation(t *testing.T) {
	plan := promotionPlan(t)

	t.Run("a tampered transition no longer verifies", func(t *testing.T) {
		tr := step(t, plan, seed(t, plan), promotionRoutes[0])
		if err := tr.Verify(); err != nil {
			t.Fatalf("the transition as minted does not verify: %v", err)
		}
		mutants := []func(*frontier.Transition){
			func(m *frontier.Transition) { m.RouteKey = "REJECTED" },
			func(m *frontier.Transition) { m.Successors[0].NodeID = workflow.PromotionNodeEndUnknown },
			func(m *frontier.Transition) { m.Successors[0].State = frontier.NodeWaiting },
			func(m *frontier.Transition) { m.Intents[0].Kind = frontier.IntentWorkItemRequired },
			func(m *frontier.Transition) { m.Frontier = append(m.Frontier, "smuggled_node") },
			func(m *frontier.Transition) { m.Complete = true },
			func(m *frontier.Transition) { m.Sequence++ },
			func(m *frontier.Transition) { m.OutputDigest = "sha256:different" },
		}
		for i, mutate := range mutants {
			mutant := tr
			mutant.Successors = append([]frontier.Successor(nil), tr.Successors...)
			mutant.Intents = append([]frontier.Intent(nil), tr.Intents...)
			mutant.Frontier = append([]string(nil), tr.Frontier...)
			mutate(&mutant)
			if err := mutant.Verify(); err == nil {
				t.Fatalf("mutant %d verified against the original digest", i)
			}
		}
	})

	t.Run("a frontier edited apart from its node states is refused", func(t *testing.T) {
		state := seed(t, plan)
		state.Frontier = append(state.Frontier, workflow.PromotionNodeObserveDrift)
		refuses(t, plan, state, promotionRoutes[0], frontier.CodeInvalidState)

		dropped := seed(t, plan)
		dropped.Frontier = nil
		refuses(t, plan, dropped, promotionRoutes[0], frontier.CodeInvalidState)
	})

	t.Run("a state carrying a node the plan does not declare is refused", func(t *testing.T) {
		state := seed(t, plan)
		state.Nodes = append(state.Nodes, frontier.NodeStatus{NodeID: "zz_smuggled", State: frontier.NodeSucceeded})
		refuses(t, plan, state, promotionRoutes[0], frontier.CodeInvalidState)
	})

	t.Run("an undeclared node state is refused", func(t *testing.T) {
		state := seed(t, plan)
		state.Nodes[0].State = "ALMOST_DONE"
		state.Frontier = nil
		refuses(t, plan, state, promotionRoutes[0], frontier.CodeInvalidState)
	})

	t.Run("different routes never collapse to the same transition", func(t *testing.T) {
		state := seed(t, plan)
		succeeded := step(t, plan, state, frontier.NodeOutcome{
			NodeID: plan.StartNodeID, Outcome: workflow.OutcomeSucceeded,
		})
		rejected := step(t, plan, state, frontier.NodeOutcome{
			NodeID: plan.StartNodeID, Outcome: workflow.OutcomeRejected,
		})
		if succeeded.Digest() == rejected.Digest() {
			t.Fatal("two different routes produced one digest")
		}
		if equal(succeeded.SuccessorIDs(), rejected.SuccessorIDs()) {
			t.Fatalf("both routes led to %v", succeeded.SuccessorIDs())
		}
	})

	t.Run("removing a declared default turns the undeclared route into a refusal", func(t *testing.T) {
		steps := walk(t, plan, seed(t, plan), promotionRoutes[:4])
		state := steps[3].Next

		stripped := promotionPlan(t)
		for i := range stripped.Nodes {
			if stripped.Nodes[i].ID == workflow.PromotionNodeRaiseThreshold && stripped.Nodes[i].Decision != nil {
				stripped.Nodes[i].Decision.DefaultRoute = ""
			}
		}
		out := frontier.NodeOutcome{NodeID: workflow.PromotionNodeRaiseThreshold, Outcome: "UNDECLARED"}
		step(t, plan, state, out) // with the default declared, this routes
		refuses(t, stripped, state, out, frontier.CodeNoMatchingRoute)
	})

	t.Run("the output digest is carried into the state, not dropped", func(t *testing.T) {
		tr := step(t, plan, seed(t, plan), promotionRoutes[0])
		status, ok := tr.Next.Node(plan.StartNodeID)
		if !ok {
			t.Fatal("the completed node left no status")
		}
		if status.OutputDigest != promotionRoutes[0].OutputDigest {
			t.Fatalf("output digest = %q, want %q", status.OutputDigest, promotionRoutes[0].OutputDigest)
		}
		if status.RouteKey != string(promotionRoutes[0].Outcome) {
			t.Fatalf("route key = %q, want %q", status.RouteKey, promotionRoutes[0].Outcome)
		}
	})
}
