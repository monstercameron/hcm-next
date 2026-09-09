package payroll

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// mustSetup wires env/params against the compiled reference workflow,
// failing the test with the full diagnostic set if the reference stops
// compiling.
func mustSetup(t *testing.T, env *Environment, params Params) *Setup {
	t.Helper()
	setup, err := NewSetupWithParams(env, params)
	if err != nil {
		t.Fatalf("NewSetupWithParams: %v", err)
	}
	return setup
}

// mustRun walks the plan and fails on any refusal.
func mustRun(t *testing.T, setup *Setup) simulate.Receipt {
	t.Helper()
	receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return receipt
}

func traceFor(receipt simulate.Receipt, nodeID string) (simulate.NodeTrace, bool) {
	for _, tr := range receipt.Trace {
		if tr.NodeID == nodeID {
			return tr, true
		}
	}
	return simulate.NodeTrace{}, false
}

// TestTodo_CONF_009 is the PRIMARY conformance claim: the compiled payroll
// run/calculation/release/settlement reference workflow walks, through the
// real SIMULATE-mode interpreter, along the golden path CONF-009's GREEN
// clause names - population/cutoff read, an exact decimal calculation bound
// to a trace, a proposal, a release decision and a provider-settlement
// observation - and ends at a terminal that reports exact, separately-
// tracked lifecycle dimensions with the run's calculation-trace and
// settlement-reconciliation obligations outstanding rather than collapsed
// into a false success.
func TestTodo_CONF_009(t *testing.T) {
	setup := mustSetup(t, GoldenEnvironment(), Params{})
	receipt := mustRun(t, setup)

	if receipt.Mode != workflow.ModeSimulate {
		t.Errorf("mode = %s, want %s", receipt.Mode, workflow.ModeSimulate)
	}
	if receipt.WorkflowID != WorkflowID {
		t.Errorf("workflow id = %q, want %q", receipt.WorkflowID, WorkflowID)
	}
	if receipt.PlanDigest != setup.Plan.Digest() {
		t.Errorf("receipt pins plan %q, plan digest is %q", receipt.PlanDigest, setup.Plan.Digest())
	}

	wantPath := []string{
		NodeReadPopulationAndCutoff,
		NodeComputeCalculation,
		NodeBuildRunProposal,
		NodeReleaseDecision,
		NodeObserveProviderSettlement,
		NodeEndPendingSettlementObligations,
	}
	got := receipt.NodeIDs()
	if len(got) != len(wantPath) {
		t.Fatalf("walked %d nodes %v, want %d %v", len(got), got, len(wantPath), wantPath)
	}
	for i := range wantPath {
		if got[i] != wantPath[i] {
			t.Fatalf("node %d = %q, want %q (whole path %v)", i, got[i], wantPath[i], got)
		}
	}

	if receipt.Terminal.TerminalCode != "PAYROLL_SIMULATION_PENDING_SETTLEMENT_OBLIGATIONS" {
		t.Errorf("terminal code = %q, want PAYROLL_SIMULATION_PENDING_SETTLEMENT_OBLIGATIONS", receipt.Terminal.TerminalCode)
	}
	wantObligations := []string{ObligationCalculationTrace, ObligationSettlementReconciliation, ObligationRecordsRetention}
	if !equalSets(receipt.Terminal.OutstandingObligationRefs, wantObligations) {
		t.Errorf("outstanding obligations = %v, want %v", receipt.Terminal.OutstandingObligationRefs, wantObligations)
	}

	wantLifecycle := simulate.LifecycleState{
		RequestState:     "SIMULATED",
		ExecutionState:   "NOT_PLANNED",
		BusinessState:    "NOT_STARTED",
		ConsistencyState: "PENDING_OBSERVATION",
		ObligationState:  "PENDING",
	}
	if receipt.Lifecycle != wantLifecycle {
		t.Errorf("lifecycle = %+v, want %+v", receipt.Lifecycle, wantLifecycle)
	}

	counters := receipt.EffectCounters()
	if !counters.IsZero() {
		t.Fatalf("simulation counted effects: %v", counters.NonZero())
	}
	if err := receipt.ZeroEffect.Validate(); err != nil {
		t.Fatalf("zero-effect receipt: %v", err)
	}

	// The exact decimal calculation - 22% flat tax on 4000.00 USD - must
	// appear in the calculation node's own trace, never a floating-point
	// approximation.
	calc, ok := traceFor(receipt, NodeComputeCalculation)
	if !ok {
		t.Fatalf("no trace entry for %s", NodeComputeCalculation)
	}
	for _, want := range []string{"4000.00 USD", "880.00 USD", "3120.00 USD"} {
		if !strings.Contains(calc.Detail, want) {
			t.Errorf("calculation detail %q does not name exact amount %q", calc.Detail, want)
		}
	}

	if len(receipt.WorkItems) != 3 {
		t.Fatalf("work items = %d, want 3 (one per declared approval requirement)", len(receipt.WorkItems))
	}
	wantApprovals := []string{ApprovalPayrollController, ApprovalFinanceRelease, ApprovalTaxCompliance}
	var gotApprovals []string
	for _, item := range receipt.WorkItems {
		if item.State != simulate.WouldAwait {
			t.Errorf("work item %s state = %q, want %q", item.RequirementID, item.State, simulate.WouldAwait)
		}
		if len(item.Candidates) == 0 {
			t.Errorf("work item %s resolved no candidate approver", item.RequirementID)
		}
		gotApprovals = append(gotApprovals, item.RequirementID)
	}
	if !equalSets(gotApprovals, wantApprovals) {
		t.Errorf("approval requirement refs = %v, want %v", gotApprovals, wantApprovals)
	}

	second := mustRun(t, mustSetup(t, GoldenEnvironment(), Params{}))
	if receipt.Digest() != second.Digest() {
		t.Fatalf("receipt digest drifted between runs:\n first  %s\n second %s", receipt.Digest(), second.Digest())
	}
	if err := receipt.Verify(); err != nil {
		t.Errorf("receipt does not verify: %v", err)
	}
}

// TestTodo_CONF_009_Fault exercises the RED cases CONF-009 names: a stale
// cutoff, a duplicate run, a partial/ambiguous settlement observation folded
// into success and unbounded/blind retry.
func TestTodo_CONF_009_Fault(t *testing.T) {
	t.Run("stale_cutoff_blocks_rather_than_releases", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, StaleCutoffEnvironment(), Params{}))
		if receipt.Terminal.NodeID != NodeEndStaleCutoffBlocked {
			t.Fatalf("terminal = %q, want %q; a stale population/cutoff snapshot must never release", receipt.Terminal.NodeID, NodeEndStaleCutoffBlocked)
		}
		if receipt.Lifecycle.RequestState != "REJECTED" {
			t.Fatalf("lifecycle request state = %q, want REJECTED", receipt.Lifecycle.RequestState)
		}
	})

	t.Run("duplicate_run_blocks_rather_than_releases_twice", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, DuplicateRunEnvironment(), Params{}))
		if receipt.Terminal.NodeID != NodeEndDuplicateRunBlocked {
			t.Fatalf("terminal = %q, want %q; a duplicate run must never be released again", receipt.Terminal.NodeID, NodeEndDuplicateRunBlocked)
		}
	})

	t.Run("rejected_settlement_routes_to_repair_not_success", func(t *testing.T) {
		setup := mustSetup(t, RejectedSettlementEnvironment(), Params{})
		receipt := mustRun(t, setup)
		if receipt.Terminal.NodeID != NodeEndSettlementRejectedRepair {
			t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndSettlementRejectedRepair, receipt.NodeIDs())
		}
		if receipt.Lifecycle.ConsistencyState == "CONSISTENT" || receipt.Lifecycle.BusinessState == "COMPLETED" {
			t.Fatalf("a rejected settlement must never be reported as consistent, completed: %+v", receipt.Lifecycle)
		}
		node, ok := setup.Plan.Node(NodeEndSettlementRejectedRepair)
		if !ok || node.Terminal == nil || len(node.Terminal.RepairRefs) == 0 {
			t.Fatalf("rejected-settlement terminal carries no RepairRefs; a degraded dimension must create bounded repair evidence")
		}
	})

	t.Run("ambiguous_settlement_is_a_distinct_repair_not_the_same_as_rejected", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, AmbiguousSettlementEnvironment(), Params{}))
		if receipt.Terminal.NodeID != NodeEndSettlementAmbiguousRepair {
			t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndSettlementAmbiguousRepair, receipt.NodeIDs())
		}
		if receipt.Terminal.NodeID == NodeEndSettlementRejectedRepair {
			t.Fatal("an ambiguous/partial settlement must never collapse to the same terminal as an outright rejected settlement")
		}
	})

	t.Run("settlement_retry_is_bounded_not_blind", func(t *testing.T) {
		setup := mustSetup(t, RejectedSettlementEnvironment(), Params{})
		node, ok := setup.Plan.Node(NodeObserveProviderSettlement)
		if !ok {
			t.Fatalf("plan declares no %s node", NodeObserveProviderSettlement)
		}
		if node.Retry == nil || node.Retry.MaxAttempts == 0 {
			t.Fatalf("provider-settlement observation declares no bounded retry policy: %+v", node.Retry)
		}
		if node.Observe == nil || node.Observe.RetryExhaustionRoute == "" {
			t.Fatalf("provider-settlement observation declares no retry exhaustion route")
		}
	})
}

// TestTodo_CONF_009_Conformance proves the whole terminal lattice at once,
// including that the calculation-trace and settlement-reconciliation
// obligations are independently tracked: a degraded settlement terminal
// names only the obligations this particular failure touched.
func TestTodo_CONF_009_Conformance(t *testing.T) {
	cases := []struct {
		name        string
		env         *Environment
		params      Params
		terminal    string
		want        simulate.LifecycleState
		obligations []string
	}{
		{
			"pending_settlement_obligations", GoldenEnvironment(), Params{}, NodeEndPendingSettlementObligations,
			simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"},
			[]string{ObligationCalculationTrace, ObligationSettlementReconciliation, ObligationRecordsRetention},
		},
		{
			"stale_cutoff_blocked", StaleCutoffEnvironment(), Params{}, NodeEndStaleCutoffBlocked,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"},
			nil,
		},
		{
			"duplicate_run_blocked", DuplicateRunEnvironment(), Params{}, NodeEndDuplicateRunBlocked,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"},
			nil,
		},
		{
			"requires_reversal_intent", AlreadySettledEnvironment(), Params{ReversalRequested: true}, NodeEndReversalIntent,
			simulate.LifecycleState{RequestState: "SUPERSEDED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"},
			nil,
		},
		{
			"already_settled_invalid", AlreadySettledEnvironment(), Params{}, NodeEndAlreadySettledInvalid,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"},
			nil,
		},
		{
			"settlement_rejected_repair", RejectedSettlementEnvironment(), Params{}, NodeEndSettlementRejectedRepair,
			simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "BLOCKED", BusinessState: "UNKNOWN", ConsistencyState: "UNKNOWN", ObligationState: "PENDING"},
			[]string{ObligationSettlementReconciliation, ObligationRecordsRetention},
		},
		{
			"settlement_ambiguous_repair", AmbiguousSettlementEnvironment(), Params{}, NodeEndSettlementAmbiguousRepair,
			simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "BLOCKED", BusinessState: "UNKNOWN", ConsistencyState: "UNKNOWN", ObligationState: "PENDING"},
			[]string{ObligationSettlementReconciliation, ObligationRecordsRetention},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			receipt := mustRun(t, mustSetup(t, c.env, c.params))
			if receipt.Terminal.NodeID != c.terminal {
				t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, c.terminal, receipt.NodeIDs())
			}
			if receipt.Lifecycle != c.want {
				t.Fatalf("lifecycle = %+v, want %+v", receipt.Lifecycle, c.want)
			}
			if c.obligations != nil && !equalSets(receipt.Terminal.OutstandingObligationRefs, c.obligations) {
				t.Fatalf("outstanding obligations = %v, want %v", receipt.Terminal.OutstandingObligationRefs, c.obligations)
			}
		})
	}
}

// TestTodo_CONF_009_Integration proves the release-to-settlement bridge end
// to end: the calculation, the proposal and the provider-settlement
// observation are one connected pipeline through the real interpreter, and
// each of the three reachable settlement outcomes (pass/fail/partial) is
// distinctly reachable through that same pipeline without re-deriving the
// calculation or the proposal.
func TestTodo_CONF_009_Integration(t *testing.T) {
	scenarios := []struct {
		name     string
		env      *Environment
		terminal string
	}{
		{"settled_pending_obligations", GoldenEnvironment(), NodeEndPendingSettlementObligations},
		{"rejected", RejectedSettlementEnvironment(), NodeEndSettlementRejectedRepair},
		{"ambiguous", AmbiguousSettlementEnvironment(), NodeEndSettlementAmbiguousRepair},
	}
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			receipt := mustRun(t, mustSetup(t, s.env, Params{}))
			if receipt.Terminal.NodeID != s.terminal {
				t.Fatalf("terminal = %q, want %q", receipt.Terminal.NodeID, s.terminal)
			}
			// Every scenario shares the same upstream calculation and
			// proposal nodes: the bridge to settlement never re-derives them.
			calc, ok := traceFor(receipt, NodeComputeCalculation)
			if !ok || !strings.Contains(calc.Detail, "3120.00 USD") {
				t.Fatalf("scenario %s: calculation node trace = %+v, want exact net pay 3120.00 USD", s.name, calc)
			}
			proposal, ok := traceFor(receipt, NodeBuildRunProposal)
			if !ok || proposal.OutputDigest == "" {
				t.Fatalf("scenario %s: proposal node produced no output digest", s.name)
			}
			observe, ok := traceFor(receipt, NodeObserveProviderSettlement)
			if !ok {
				t.Fatalf("scenario %s: no trace entry for %s; the bridge observation never ran", s.name, NodeObserveProviderSettlement)
			}
			if observe.NodeID != NodeObserveProviderSettlement {
				t.Fatalf("unexpected trace entry: %+v", observe)
			}
		})
	}
}

// TestTodo_CONF_009_Property proves the release DECISION's routing is a
// total, deterministic function of its four declared inputs, with a fixed
// precedence (reversal-intent > already-settled > duplicate-run >
// stale-cutoff > routine) that holds across every one of the sixteen
// combinations of the four booleans, not merely the handful of named
// scenarios above.
func TestTodo_CONF_009_Property(t *testing.T) {
	expect := func(cutoffActive, duplicate, alreadySettled, reversal bool) string {
		switch {
		case reversal && alreadySettled:
			return RouteReversalIntent
		case alreadySettled:
			return RouteAlreadySettledInvalid
		case duplicate:
			return RouteDuplicateRunBlocked
		case !cutoffActive:
			return RouteStaleCutoffBlocked
		default:
			return RouteRoutineReleaseRequired
		}
	}
	for bits := 0; bits < 16; bits++ {
		cutoffActive := bits&1 != 0
		duplicate := bits&2 != 0
		alreadySettled := bits&4 != 0
		reversal := bits&8 != 0
		want := expect(cutoffActive, duplicate, alreadySettled, reversal)

		result, err := Decisions{}.Decide(context.Background(), simulate.DecisionRequest{
			Decision: workflow.CompiledDecision{RuleRef: RuleReleaseAndIntegrity},
			Inputs: simulate.Bag{
				"cutoff_active":          simulate.NewBool(cutoffActive),
				"duplicate_run_detected": simulate.NewBool(duplicate),
				"already_settled":        simulate.NewBool(alreadySettled),
				"reversal_requested":     simulate.NewBool(reversal),
			},
		})
		if err != nil {
			t.Fatalf("bits=%04b: Decide: %v", bits, err)
		}
		if result.RouteKey != want {
			t.Fatalf("bits=%04b (cutoffActive=%v duplicate=%v alreadySettled=%v reversal=%v): route = %q, want %q",
				bits, cutoffActive, duplicate, alreadySettled, reversal, result.RouteKey, want)
		}

		// The trace ref is a pure function of the same four inputs: running
		// twice with identical inputs must reproduce it exactly.
		again, err := Decisions{}.Decide(context.Background(), simulate.DecisionRequest{
			Decision: workflow.CompiledDecision{RuleRef: RuleReleaseAndIntegrity},
			Inputs: simulate.Bag{
				"cutoff_active":          simulate.NewBool(cutoffActive),
				"duplicate_run_detected": simulate.NewBool(duplicate),
				"already_settled":        simulate.NewBool(alreadySettled),
				"reversal_requested":     simulate.NewBool(reversal),
			},
		})
		if err != nil {
			t.Fatalf("bits=%04b: second Decide: %v", bits, err)
		}
		if again.TraceRef != result.TraceRef {
			t.Fatalf("bits=%04b: trace ref drifted: %q vs %q", bits, result.TraceRef, again.TraceRef)
		}
	}
}

// TestTodo_CONF_009_Race proves the walk is deterministic and safe to run
// concurrently: N independent goroutines, each with its own compiled plan
// and environment, produce byte-identical receipts.
func TestTodo_CONF_009_Race(t *testing.T) {
	const n = 16
	digests := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			setup, err := NewSetup(GoldenEnvironment())
			if err != nil {
				errs[i] = err
				return
			}
			receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = receipt.Digest()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	for i := 1; i < n; i++ {
		if digests[i] != digests[0] {
			t.Fatalf("run %d digest %q differs from run 0 digest %q", i, digests[i], digests[0])
		}
	}
}

// TestTodo_CONF_009_HiddenEffect proves the simulator refuses a plan whose
// otherwise admitted node has acquired a hidden write effect.
func TestTodo_CONF_009_HiddenEffect(t *testing.T) {
	setup := mustSetup(t, GoldenEnvironment(), Params{})
	setup.Plan.Nodes[0].EffectClass = capability.EffectExternalMutation
	if err := simulate.Admit(setup.Plan); err == nil {
		t.Fatal("Admit accepted a plan containing a hidden write effect")
	} else if code := simulate.CodeOf(err); code != simulate.CodeWriteEffectInSimulate {
		t.Fatalf("Admit refusal code = %q, want %q (%v)", code, simulate.CodeWriteEffectInSimulate, err)
	}
}

func equalSets(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(want))
	for _, w := range want {
		seen[w] = true
	}
	for _, g := range got {
		if !seen[g] {
			return false
		}
		delete(seen, g)
	}
	return len(seen) == 0
}
