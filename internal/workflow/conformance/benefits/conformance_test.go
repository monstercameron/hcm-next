package benefits

import (
	"context"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
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

// TestTodo_CONF_010 is the PRIMARY conformance claim: the compiled benefits
// eligibility/election/carrier-reconciliation reference workflow walks,
// through the real SIMULATE-mode interpreter, along the golden path
// CONF-010's GREEN clause names - eligibility facts read, an eligibility
// trace built, an immutable election revision proposed, a routine
// eligibility decision, and independent carrier and payroll-deduction
// reconciliation observations - and ends at a terminal that reports exact,
// separately-tracked lifecycle dimensions with the eligibility-trace,
// election-revision, carrier-reconciliation and deduction-reconciliation
// obligations outstanding rather than collapsed into a false success.
func TestTodo_CONF_010(t *testing.T) {
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
		NodeReadEligibilityFacts,
		NodeBuildEligibilityTrace,
		NodeBuildElectionProposal,
		NodeEligibilityDecision,
		NodeObserveCarrierReconciliation,
		NodeObserveDeductionReconciliation,
		NodeEndPendingObligations,
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

	if receipt.Terminal.TerminalCode != "BENEFITS_SIMULATION_PENDING_OBLIGATIONS" {
		t.Errorf("terminal code = %q, want BENEFITS_SIMULATION_PENDING_OBLIGATIONS", receipt.Terminal.TerminalCode)
	}
	wantObligations := []string{
		ObligationEligibilityTrace, ObligationElectionRevision,
		ObligationCarrierReconciliation, ObligationDeductionReconciliation, ObligationRecordsRetention,
	}
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

	if len(receipt.WorkItems) != 3 {
		t.Fatalf("work items = %d, want 3 (one per declared approval requirement)", len(receipt.WorkItems))
	}
	wantApprovals := []string{ApprovalBenefitsAdmin, ApprovalHRPartner, ApprovalPayrollDeduction}
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

// TestTodo_CONF_010_Conformance proves the whole terminal lattice at once,
// including that carrier and payroll-deduction reconciliation obligations
// are independently tracked: a degraded carrier terminal never names the
// deduction obligation it never examined, and vice versa.
func TestTodo_CONF_010_Conformance(t *testing.T) {
	cases := []struct {
		name        string
		env         *Environment
		terminal    string
		want        simulate.LifecycleState
		obligations []string
	}{
		{
			"pending_obligations", GoldenEnvironment(), NodeEndPendingObligations,
			simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"},
			[]string{ObligationEligibilityTrace, ObligationElectionRevision, ObligationCarrierReconciliation, ObligationDeductionReconciliation, ObligationRecordsRetention},
		},
		{
			"disputed_fact_blocked", DisputedFactEnvironment(), NodeEndDisputedFactBlocked,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"},
			nil,
		},
		{
			"expired_window_blocked", ExpiredWindowEnvironment(), NodeEndExpiredWindowBlocked,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"},
			nil,
		},
		{
			"duplicate_life_event_blocked", DuplicateLifeEventEnvironment(), NodeEndDuplicateLifeEventBlocked,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"},
			nil,
		},
		{
			"overlapping_election_blocked", OverlappingElectionEnvironment(), NodeEndOverlappingElectionBlocked,
			simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"},
			nil,
		},
		{
			"carrier_degraded_repair", DegradedCarrierEnvironment(), NodeEndCarrierDegradedRepair,
			simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "BLOCKED", BusinessState: "UNKNOWN", ConsistencyState: "UNKNOWN", ObligationState: "PENDING"},
			[]string{ObligationCarrierReconciliation, ObligationRecordsRetention},
		},
		{
			"deduction_degraded_repair", DegradedDeductionEnvironment(), NodeEndDeductionDegradedRepair,
			simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "BLOCKED", BusinessState: "UNKNOWN", ConsistencyState: "UNKNOWN", ObligationState: "PENDING"},
			[]string{ObligationDeductionReconciliation, ObligationRecordsRetention},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			receipt := mustRun(t, mustSetup(t, c.env, Params{}))
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

	// A carrier failure must never widen to name the deduction obligation it
	// never touched, and a deduction failure must never widen to name the
	// carrier obligation - that is what "independent" reconciliation means
	// in practice, and what CONF-010's "partial carrier/deduction outcome
	// becomes full success" RED case is a special case of (an outcome that
	// widens or narrows the wrong obligation set is not honestly reported
	// either).
	t.Run("carrier_degraded_never_names_deduction_obligation", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, DegradedCarrierEnvironment(), Params{}))
		for _, got := range receipt.Terminal.OutstandingObligationRefs {
			if got == ObligationDeductionReconciliation {
				t.Fatalf("carrier-degraded terminal names the deduction-reconciliation obligation it never examined")
			}
		}
	})
	t.Run("deduction_degraded_never_names_carrier_obligation", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, DegradedDeductionEnvironment(), Params{}))
		for _, got := range receipt.Terminal.OutstandingObligationRefs {
			if got == ObligationCarrierReconciliation {
				t.Fatalf("deduction-degraded terminal names the carrier-reconciliation obligation a prior PASS already reconciled")
			}
		}
	})
}

// TestTodo_CONF_010_Property proves two invariants hold across every input,
// not merely the named scenarios above: the eligibility DECISION's routing
// is a total, deterministic function of its four booleans with a fixed
// precedence (disputed > expired > duplicate > overlap > routine), and the
// election-proposal TRANSFORM always produces a distinct digest when the
// prior election digest changes - the runtime content of "immutable
// election revisions" (CONF-010's GREEN clause).
func TestTodo_CONF_010_Property(t *testing.T) {
	expect := func(disputed, expired, duplicate, overlap bool) string {
		switch {
		case disputed:
			return RouteDisputedFactBlocked
		case expired:
			return RouteExpiredWindowBlocked
		case duplicate:
			return RouteDuplicateLifeEventBlocked
		case overlap:
			return RouteOverlappingElectionBlocked
		default:
			return RouteRoutineApprovalRequired
		}
	}
	for bits := 0; bits < 16; bits++ {
		disputed := bits&1 != 0
		expired := bits&2 != 0
		duplicate := bits&4 != 0
		overlap := bits&8 != 0
		want := expect(disputed, expired, duplicate, overlap)

		result, err := Decisions{}.Decide(context.Background(), simulate.DecisionRequest{
			Decision: workflow.CompiledDecision{RuleRef: RuleEligibilityAndElection},
			Inputs: simulate.Bag{
				"fact_disputed":             simulate.NewBool(disputed),
				"enrollment_window_expired": simulate.NewBool(expired),
				"life_event_duplicate":      simulate.NewBool(duplicate),
				"election_overlap_detected": simulate.NewBool(overlap),
			},
		})
		if err != nil {
			t.Fatalf("bits=%04b: Decide: %v", bits, err)
		}
		if result.RouteKey != want {
			t.Fatalf("bits=%04b (disputed=%v expired=%v duplicate=%v overlap=%v): route = %q, want %q",
				bits, disputed, expired, duplicate, overlap, result.RouteKey, want)
		}
	}

	effective, err := values.ParseLocalDate(EffectiveDateText)
	if err != nil {
		t.Fatalf("ParseLocalDate: %v", err)
	}
	baseInputs := func(prior string) simulate.Bag {
		return simulate.Bag{
			"worker_id":                simulate.NewBranded("WorkerID", "worker-1"),
			"plan_id":                  simulate.NewBranded("BenefitPlanID", "PLAN-1"),
			"prior_election_digest":    simulate.NewString(prior),
			"eligibility_trace_digest": simulate.NewString("trace-1"),
			"effective_date":           simulate.NewLocalDate(effective),
		}
	}
	first, err := Transforms{}.Transform(context.Background(), simulate.TransformRequest{
		Transform: workflow.CompiledTransform{TransformRef: TransformBuildElectionProposal},
		Inputs:    baseInputs(""),
	})
	if err != nil {
		t.Fatalf("Transform (no prior): %v", err)
	}
	second, err := Transforms{}.Transform(context.Background(), simulate.TransformRequest{
		Transform: workflow.CompiledTransform{TransformRef: TransformBuildElectionProposal},
		Inputs:    baseInputs(first.Outputs["election_digest"].Text),
	})
	if err != nil {
		t.Fatalf("Transform (with prior): %v", err)
	}
	if first.Outputs["election_digest"].Text == second.Outputs["election_digest"].Text {
		t.Fatal("an election revision naming a prior digest produced the same digest as the one with no prior; revisions are not distinguishable from overwrites")
	}
	// But the same prior digest and the same facts must always reproduce the
	// exact same revision digest: immutability is about superseding, not
	// about being unstably random.
	third, err := Transforms{}.Transform(context.Background(), simulate.TransformRequest{
		Transform: workflow.CompiledTransform{TransformRef: TransformBuildElectionProposal},
		Inputs:    baseInputs(first.Outputs["election_digest"].Text),
	})
	if err != nil {
		t.Fatalf("Transform (repeat with prior): %v", err)
	}
	if second.Outputs["election_digest"].Text != third.Outputs["election_digest"].Text {
		t.Fatal("the same prior digest and facts produced two different revision digests")
	}
}

// TestTodo_CONF_010_Race proves the walk is deterministic and safe to run
// concurrently: N independent goroutines, each with its own compiled plan
// and environment, produce byte-identical receipts.
func TestTodo_CONF_010_Race(t *testing.T) {
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

// TestTodo_CONF_010_HiddenEffect proves the simulator refuses a plan whose
// otherwise admitted node has acquired a hidden write effect.
func TestTodo_CONF_010_HiddenEffect(t *testing.T) {
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
