package frontier_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

// TestTodo_WF_RUN_024 is the primary claim: one pure transition function
// derives the exact successor identities, node states, join counters, terminal
// dimensions and scheduling intents that follow from a pinned plan, a pinned
// state and a typed outcome — and derives them the same way every time.
func TestTodo_WF_RUN_024(t *testing.T) {
	plan := promotionPlan(t)

	t.Run("seed starts one node and nothing else", func(t *testing.T) {
		state := seed(t, plan)
		if got := state.Frontier; len(got) != 1 || got[0] != plan.StartNodeID {
			t.Fatalf("seed frontier = %v, want [%s]", got, plan.StartNodeID)
		}
		status, ok := state.Node(plan.StartNodeID)
		if !ok || status.State != frontier.NodeReady {
			t.Fatalf("start node status = %+v, want READY", status)
		}
		if state.PlanDigest != plan.Digest() {
			t.Errorf("seed pins plan %q, plan digests to %q", state.PlanDigest, plan.Digest())
		}
	})

	t.Run("advancement names exact successors and intents", func(t *testing.T) {
		state := seed(t, plan)
		tr := step(t, plan, state, promotionRoutes[0])

		if want := []string{workflow.PromotionNodeSimulateComp}; !equal(tr.SuccessorIDs(), want) {
			t.Fatalf("successors = %v, want %v", tr.SuccessorIDs(), want)
		}
		if tr.CompletedState != frontier.NodeSucceeded {
			t.Errorf("completed state = %s, want SUCCEEDED", tr.CompletedState)
		}
		if tr.RouteKey != string(workflow.OutcomeSucceeded) {
			t.Errorf("route key = %q, want SUCCEEDED", tr.RouteKey)
		}
		intent, ok := tr.Intent(workflow.PromotionNodeSimulateComp)
		if !ok || intent.Kind != frontier.IntentReady {
			t.Fatalf("intent for the successor = %+v, want READY", intent)
		}
		if len(tr.Intents) != 1 {
			t.Errorf("intents = %+v, want exactly one", tr.Intents)
		}
		if !equal(tr.Frontier, []string{workflow.PromotionNodeSimulateComp}) {
			t.Errorf("frontier = %v, want only the successor", tr.Frontier)
		}
		if tr.Next.Sequence != state.Sequence+1 {
			t.Errorf("sequence = %d, want %d", tr.Next.Sequence, state.Sequence+1)
		}
	})

	t.Run("the whole promotion walk reaches a stated terminal", func(t *testing.T) {
		state := seed(t, plan)
		steps := walk(t, plan, state, promotionRoutes)

		var visited []string
		for i, tr := range steps {
			visited = append(visited, tr.CompletedNodeID)
			if tr.Digest() == "" {
				t.Fatalf("step %d minted no digest", i)
			}
			if err := tr.Verify(); err != nil {
				t.Fatalf("step %d does not verify: %v", i, err)
			}
		}
		want := []string{
			workflow.PromotionNodeSnapshotWorker,
			workflow.PromotionNodeSimulateComp,
			workflow.PromotionNodeEvaluateBand,
			workflow.PromotionNodeBuildProposal,
			workflow.PromotionNodeRaiseThreshold,
			workflow.PromotionNodeEndApproval,
		}
		if !equal(visited, want) {
			t.Fatalf("visited %v, want %v", visited, want)
		}

		last := steps[len(steps)-1]
		if !last.Complete {
			t.Fatal("the final advancement did not complete the instance")
		}
		if last.Terminal.TerminalCode != "SIMULATION_APPROVAL_REQUIRED" {
			t.Errorf("terminal code = %q, want SIMULATION_APPROVAL_REQUIRED", last.Terminal.TerminalCode)
		}
		if last.Terminal.RuntimeStatus != workflow.RuntimeCompleted {
			t.Errorf("runtime status = %q, want COMPLETED", last.Terminal.RuntimeStatus)
		}
		// All five dimensions are carried, never collapsed into one status.
		lc := last.Terminal.Lifecycle
		for name, got := range map[string]string{
			"request":     lc.RequestState,
			"execution":   lc.ExecutionState,
			"business":    lc.BusinessState,
			"consistency": lc.ConsistencyState,
			"obligation":  lc.ObligationState,
		} {
			if got == "" {
				t.Errorf("terminal records no %s state", name)
			}
		}
		if len(last.Terminal.OutstandingObligationRefs) == 0 {
			t.Error("the approval terminal carries no outstanding obligation")
		}
		if len(last.Frontier) != 0 {
			t.Errorf("frontier after completion = %v, want empty", last.Frontier)
		}
		intent, ok := last.Intent(workflow.PromotionNodeEndApproval)
		if !ok || intent.Kind != frontier.IntentComplete {
			t.Fatalf("terminal intent = %+v, want COMPLETE", intent)
		}
	})

	t.Run("an excluded branch is skipped and activates nothing", func(t *testing.T) {
		state := seed(t, plan)
		steps := walk(t, plan, state, promotionRoutes[:5])
		threshold := steps[4]

		if !contains(threshold.Skipped, workflow.PromotionNodeObserveDrift) {
			t.Fatalf("skipped = %v, want the WITHIN_THRESHOLD branch %s",
				threshold.Skipped, workflow.PromotionNodeObserveDrift)
		}
		for _, id := range threshold.Skipped {
			if _, ok := threshold.Intent(id); ok {
				t.Errorf("skipped node %s carries a scheduling intent", id)
			}
			status, ok := threshold.Next.Node(id)
			if !ok || status.State != frontier.NodeSkipped {
				t.Errorf("skipped node %s is %+v, want SKIPPED", id, status)
			}
			if contains(threshold.Frontier, id) {
				t.Errorf("skipped node %s is still on the frontier", id)
			}
		}
	})

	t.Run("a shared terminal a live node can still reach is not skipped", func(t *testing.T) {
		state := seed(t, plan)
		tr := step(t, plan, state, promotionRoutes[0])
		// end_unknown is the UNKNOWN route of the node that just completed,
		// but the successor can still reach it, so it was not excluded.
		if contains(tr.Skipped, workflow.PromotionNodeEndUnknown) {
			t.Errorf("skipped %v marks a terminal the live successor can still reach", tr.Skipped)
		}
	})

	t.Run("a declared default route is applied explicitly", func(t *testing.T) {
		state := seed(t, plan)
		steps := walk(t, plan, state, promotionRoutes[:4])
		before := steps[3].Next

		tr := step(t, plan, before, frontier.NodeOutcome{
			NodeID:  workflow.PromotionNodeRaiseThreshold,
			Outcome: "A_ROUTE_THE_EVALUATOR_INVENTED",
		})
		if tr.RouteKey != "WITHIN_THRESHOLD" {
			t.Fatalf("route key = %q, want the declared default WITHIN_THRESHOLD", tr.RouteKey)
		}
		if len(tr.Successors) != 1 || !tr.Successors[0].ViaDefault {
			t.Fatalf("successors = %+v, want one marked via_default", tr.Successors)
		}
		if tr.Successors[0].NodeID != workflow.PromotionNodeObserveDrift {
			t.Errorf("default led to %q, want %q", tr.Successors[0].NodeID, workflow.PromotionNodeObserveDrift)
		}
	})

	t.Run("awaiting work suspends in place and schedules the right record", func(t *testing.T) {
		joins := joinPlan()
		approval := workflow.CompiledNode{ID: "approve", Type: workflow.StepApproval,
			Routes: []string{"APPROVED", "CANCELLED", "EXPIRED", "INVALIDATED", "REJECTED"}}
		joins.Nodes = append(joins.Nodes, approval)
		state, err := frontier.Seed(joins, "wf-await")
		if err != nil {
			t.Fatalf("Seed: %v", err)
		}
		state.Nodes = []frontier.NodeStatus{{NodeID: "approve", State: frontier.NodeReady}}
		state.Frontier = []string{"approve"}

		tr := step(t, joins, state, frontier.NodeOutcome{
			NodeID:   "approve",
			Await:    frontier.AwaitWorkItem,
			AwaitRef: "approval.finance_partner",
		})
		if tr.CompletedState != frontier.NodeWaiting {
			t.Fatalf("completed state = %s, want WAITING", tr.CompletedState)
		}
		if len(tr.Successors) != 0 {
			t.Errorf("an awaiting node activated successors %+v", tr.Successors)
		}
		intent, ok := tr.Intent("approve")
		if !ok || intent.Kind != frontier.IntentWorkItemRequired {
			t.Fatalf("intent = %+v, want WORK_ITEM_REQUIRED", intent)
		}
		if intent.Ref != "approval.finance_partner" {
			t.Errorf("intent ref = %q, want the requirement the handler named", intent.Ref)
		}
		if !contains(tr.Frontier, "approve") {
			t.Errorf("frontier = %v, want the waiting node still on it", tr.Frontier)
		}
	})

	t.Run("a join activates only when its declared count is met", func(t *testing.T) {
		joins := joinPlan()
		state := bothBranchesReady(t, joins)

		first := step(t, joins, state, frontier.NodeOutcome{NodeID: "branch_a", Outcome: workflow.OutcomeSucceeded})
		if len(first.Successors) != 1 || first.Successors[0].NodeID != "gate" {
			t.Fatalf("successors = %+v, want the gate", first.Successors)
		}
		if !first.Successors[0].JoinPending || first.Successors[0].State != frontier.NodeWaiting {
			t.Fatalf("gate = %+v, want WAITING and pending after one of two branches", first.Successors[0])
		}
		if _, ok := first.Intent("gate"); ok {
			t.Error("a join short of its declared count scheduled work")
		}
		counter, ok := first.Next.Join("gate")
		if !ok {
			t.Fatal("the state carries no counter for the gate")
		}
		if counter.Activated {
			t.Errorf("counter = %+v, want not yet activated", counter)
		}
		if !equal(counter.Satisfied, []string{"branch_a"}) {
			t.Errorf("satisfied = %v, want [branch_a]", counter.Satisfied)
		}

		second := step(t, joins, first.Next, frontier.NodeOutcome{NodeID: "branch_b", Outcome: workflow.OutcomeSucceeded})
		if len(second.Successors) != 1 || second.Successors[0].State != frontier.NodeReady {
			t.Fatalf("gate = %+v, want READY once both branches arrived", second.Successors)
		}
		intent, ok := second.Intent("gate")
		if !ok || intent.Kind != frontier.IntentReady {
			t.Fatalf("intent = %+v, want READY", intent)
		}
	})

	t.Run("a quorum join activates at its declared count, not at all of them", func(t *testing.T) {
		joins := joinPlan()
		state := bothBranchesReady(t, joins, frontier.JoinDeclaration{
			NodeID: "gate", Strategy: frontier.JoinQuorum, RequiredCount: 1,
		})
		tr := step(t, joins, state, frontier.NodeOutcome{NodeID: "branch_a", Outcome: workflow.OutcomeSucceeded})
		if _, ok := tr.Intent("gate"); !ok {
			t.Fatalf("QUORUM(1) did not activate on its first arrival: %+v", tr.Successors)
		}
	})

	t.Run("a skipped branch blocks a join rather than satisfying it", func(t *testing.T) {
		joins := joinPlan()
		state, err := frontier.Seed(joins, "wf-join-skip")
		if err != nil {
			t.Fatalf("Seed: %v", err)
		}
		fan := step(t, joins, state, frontier.NodeOutcome{NodeID: "fan", Outcome: "TO_A"})
		if !contains(fan.Skipped, "branch_b") {
			t.Fatalf("skipped = %v, want branch_b", fan.Skipped)
		}
		counter, _ := fan.Next.Join("gate")
		if !equal(counter.Skipped, []string{"branch_b"}) {
			t.Fatalf("counter.Skipped = %v, want [branch_b]", counter.Skipped)
		}

		arrived := step(t, joins, fan.Next, frontier.NodeOutcome{NodeID: "branch_a", Outcome: workflow.OutcomeSucceeded})
		counter, _ = arrived.Next.Join("gate")
		if counter.Activated {
			t.Error("ALL activated with a branch that will never arrive")
		}
		if !counter.Blocked {
			t.Error("counter does not report that its strategy can no longer be met")
		}
		if _, ok := arrived.Intent("gate"); ok {
			t.Error("a blocked join scheduled work")
		}
	})

	t.Run("the frontier walk matches the simulator over the same routes", func(t *testing.T) {
		setup, err := simulate.NewPromotionSetup(simulate.PromotionWithinThresholdPay)
		if err != nil {
			t.Fatalf("promotion setup: %v", err)
		}
		receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
		if err != nil {
			t.Fatalf("simulate.Run: %v", err)
		}

		state, err := frontier.Seed(setup.Plan, "wf-parity")
		if err != nil {
			t.Fatalf("Seed: %v", err)
		}
		var visited []string
		for _, entry := range receipt.Trace {
			out := frontier.NodeOutcome{NodeID: entry.NodeID, OutputDigest: entry.OutputDigest}
			if entry.Type != workflow.StepEnd {
				out.Outcome = entry.Outcome
			}
			tr, err := frontier.Advance(setup.Plan, state, out)
			if err != nil {
				t.Fatalf("Advance(%s): %v", entry.NodeID, err)
			}
			visited = append(visited, tr.CompletedNodeID)
			if entry.NextNodeID != "" {
				if ids := tr.SuccessorIDs(); !equal(ids, []string{entry.NextNodeID}) {
					t.Fatalf("after %s the frontier advanced to %v, the simulator went to %q",
						entry.NodeID, ids, entry.NextNodeID)
				}
			}
			state = tr.Next
		}
		if !equal(visited, receipt.NodeIDs()) {
			t.Fatalf("frontier visited %v, the simulator visited %v", visited, receipt.NodeIDs())
		}
		if !state.Completed {
			t.Fatal("the frontier walk did not complete where the simulator terminated")
		}
		if state.Terminal.TerminalCode != receipt.Terminal.TerminalCode {
			t.Errorf("terminal %q, simulator reached %q", state.Terminal.TerminalCode, receipt.Terminal.TerminalCode)
		}
	})
}

// equal compares two string slices.
func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// errIsFrontier asserts a refusal carries the package sentinel, so a caller
// can classify it without matching message text.
func errIsFrontier(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, frontier.ErrFrontier) {
		t.Fatalf("error %v does not unwrap to frontier.ErrFrontier", err)
	}
}

// asFrontierError extracts the typed refusal so a test can read its detail.
func asFrontierError(err error, target **frontier.Error) bool { return errors.As(err, target) }

func containsSubstring(s, sub string) bool { return strings.Contains(s, sub) }
